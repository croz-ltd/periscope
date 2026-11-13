package extract

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/croz-ltd/periscope/internal/model"
)

func rl(cpu, mem string) corev1.ResourceList {
	return corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse(mem)}
}

func sizedNode(name, role string, capCPU, capMem, allocCPU, allocMem string, vm bool) *corev1.Node {
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"node-role.kubernetes.io/" + role: ""}},
		Status:     corev1.NodeStatus{Capacity: rl(capCPU, capMem), Allocatable: rl(allocCPU, allocMem)},
	}
	if vm {
		n.Labels[labelVMSchedulable] = "true"
	}
	return n
}

func requestingPod(name, nodeName string, phase corev1.PodPhase, cpu, mem string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: name},
		Spec: corev1.PodSpec{
			NodeName:   nodeName,
			Containers: []corev1.Container{{Name: "c", Resources: corev1.ResourceRequirements{Requests: rl(cpu, mem)}}},
		},
		Status: corev1.PodStatus{Phase: phase},
	}
}

// Nodes of different size are summed one by one. Free is allocatable minus the
// requests of the placed, unfinished pods.
func TestCapacitySumsNodesOfDifferentSize(t *testing.T) {
	objs := []runtime.Object{
		sizedNode("master-0", "master", "8", "32Gi", "7500m", "30Gi", false),
		sizedNode("worker-0", "worker", "32", "128Gi", "31500m", "124Gi", true),
		sizedNode("worker-1", "worker", "64", "256Gi", "63500m", "252Gi", true),
		requestingPod("api", "master-0", corev1.PodRunning, "2", "8Gi"),
		requestingPod("web", "worker-0", corev1.PodRunning, "4", "16Gi"),
		requestingPod("virt-launcher", "worker-1", corev1.PodRunning, "1500m", "16Gi"),
		requestingPod("done", "worker-1", corev1.PodSucceeded, "10", "10Gi"),
		requestingPod("waiting", "", corev1.PodPending, "10", "10Gi"),
	}
	c := &Clients{Typed: k8sfake.NewClientset(objs...)}

	comps, err := Capacity{}.Extract(context.Background(), c)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	got := byKey(comps)
	want := map[string]string{
		"cpu-capacity": "104", "cpu-requested": "8", "cpu-free": "95", // 102.5 - 7.5 = 95
		"mem-capacity": "416", "mem-requested": "40", "mem-free": "366", // 406 - 40
	}
	for key, w := range want {
		if got[key].Version != w {
			t.Errorf("%s = %q, want %q", key, got[key].Version, w)
		}
		if got[key].Compare != model.CompareInfo || got[key].Group != model.GroupCapacity {
			t.Errorf("%s is %s/%s, want an info row in %s", key, got[key].Compare, got[key].Group, model.GroupCapacity)
		}
	}
	if ex := got["cpu-capacity"].Extra; ex["role worker"] != "96 cores" || ex["reserved"] != "1.5 cores" {
		t.Errorf("cpu-capacity tooltip = %v", ex)
	}
	if _, ok := got["vm-vcpu-total"]; ok {
		t.Error("VM rows without OpenShift Virtualization")
	}
}

// The scheduler reserves the larger of the app containers plus sidecars and the
// largest plain init container, plus the pod overhead.
func TestPodRequestFollowsTheSchedulerRule(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	p := &corev1.Pod{Spec: corev1.PodSpec{
		InitContainers: []corev1.Container{
			{Name: "sidecar", RestartPolicy: &always, Resources: corev1.ResourceRequirements{Requests: rl("100m", "64Mi")}},
			{Name: "migrate", Resources: corev1.ResourceRequirements{Requests: rl("2", "128Mi")}},
		},
		Containers: []corev1.Container{
			{Name: "a", Resources: corev1.ResourceRequirements{Requests: rl("500m", "1Gi")}},
			{Name: "b", Resources: corev1.ResourceRequirements{Requests: rl("500m", "1Gi")}},
		},
		Overhead: rl("10m", "1Mi"),
	}}
	r := podRequest(p)
	// CPU: init 2 + sidecar 0.1 beats 1 + 0.1, then overhead. Memory: 2Gi + 64Mi beats 192Mi.
	if r.cpu != 2110 || r.mem != (2<<30)+(65<<20) {
		t.Errorf("podRequest = %+v", r)
	}
}

func hyperConverged(spec map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
	u.SetAPIVersion("hco.kubevirt.io/v1beta1")
	u.SetKind("HyperConverged")
	u.SetNamespace("openshift-cnv")
	u.SetName("kubevirt-hyperconverged")
	return u
}

func virtClients(hco *unstructured.Unstructured, objs ...runtime.Object) *Clients {
	scheme := runtime.NewScheme()
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{hyperConvergedGVR: "HyperConvergedList"}, hco)
	c := &Clients{Typed: k8sfake.NewClientset(objs...), Dynamic: dyn}
	c.served = map[string]map[string]bool{"hco.kubevirt.io/v1beta1": {"hyperconvergeds": true}}
	return c
}

// The VM view counts only the nodes that can run a VM, multiplies cores by the
// allocation ratio and memory by the overcommit percentage.
func TestCapacityVMView(t *testing.T) {
	cordoned := sizedNode("worker-2", "worker", "64", "256Gi", "64", "256Gi", true)
	cordoned.Spec.Unschedulable = true
	objs := []runtime.Object{
		sizedNode("master-0", "master", "8", "32Gi", "8", "32Gi", false),
		sizedNode("worker-0", "worker", "32", "128Gi", "30", "120Gi", true),
		sizedNode("worker-1", "worker", "32", "128Gi", "30", "120Gi", true),
		cordoned,
		requestingPod("on-master", "master-0", corev1.PodRunning, "4", "8Gi"),
		requestingPod("vm-a", "worker-0", corev1.PodRunning, "1600m", "16Gi"),
		requestingPod("vm-b", "worker-1", corev1.PodRunning, "400m", "24Gi"),
	}

	cases := []struct {
		name string
		spec map[string]any
		want map[string]string
	}{
		{"defaults", map[string]any{}, map[string]string{
			"vm-vcpu-total": "600", "vm-vcpu-used": "20", "vm-vcpu-free": "580",
			"vm-memory-total": "240", "vm-memory-used": "40", "vm-memory-free": "200",
		}},
		{"set", map[string]any{
			"resourceRequirements":  map[string]any{"vmiCPUAllocationRatio": int64(4)},
			"higherWorkloadDensity": map[string]any{"memoryOvercommitPercentage": int64(150)},
		}, map[string]string{
			"vm-vcpu-total": "240", "vm-vcpu-used": "8", "vm-vcpu-free": "232",
			"vm-memory-total": "360", "vm-memory-used": "60", "vm-memory-free": "300",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comps, err := Capacity{}.Extract(context.Background(), virtClients(hyperConverged(tc.spec), objs...))
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			got := byKey(comps)
			for key, w := range tc.want {
				if got[key].Version != w {
					t.Errorf("%s = %q, want %q", key, got[key].Version, w)
				}
			}
			if got["vm-vcpu-total"].Extra["VM nodes"] != "2" {
				t.Errorf("vm-vcpu-total tooltip = %v", got["vm-vcpu-total"].Extra)
			}
		})
	}
}

// A cluster with no nodes reports nothing rather than a row of zeros.
func TestCapacityNoNodes(t *testing.T) {
	comps, err := Capacity{}.Extract(context.Background(), &Clients{Typed: k8sfake.NewClientset()})
	if err != nil || comps != nil {
		t.Errorf("got %v, %v, want no rows", comps, err)
	}
}
