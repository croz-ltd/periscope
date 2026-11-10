package extract

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/croz-ltd/periscope/internal/model"
)

func pod(ns, name string, phase corev1.PodPhase, waiting ...string) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Status:     corev1.PodStatus{Phase: phase},
	}
	for _, reason := range waiting {
		p.Status.ContainerStatuses = append(p.Status.ContainerStatuses, corev1.ContainerStatus{
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}},
		})
	}
	return p
}

// Pods are counted across all namespaces. A crash-looping pod stays in phase
// Running, so it counts as running and as crash-looping.
func TestPodsCountsTheWholeCluster(t *testing.T) {
	initCrash := pod("batch", "init-crash", corev1.PodPending)
	initCrash.Status.InitContainerStatuses = []corev1.ContainerStatus{{
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
	}}
	objs := []runtime.Object{
		pod("app", "web-1", corev1.PodRunning),
		pod("app", "web-2", corev1.PodRunning),
		pod("app", "worker", corev1.PodRunning, "CrashLoopBackOff"),
		pod("openshift-monitoring", "pull", corev1.PodPending, "ImagePullBackOff"),
		pod("batch", "job-done", corev1.PodSucceeded),
		initCrash,
	}
	c := &Clients{Typed: k8sfake.NewClientset(objs...)}

	comps, err := Pods{}.Extract(context.Background(), c)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	got := byKey(comps)
	for key, want := range map[string]string{"pod-total": "6", "pod-running": "3", "pod-crashloop": "2"} {
		if got[key].Version != want {
			t.Errorf("%s = %q, want %q", key, got[key].Version, want)
		}
		if got[key].Compare != model.CompareInfo || got[key].Group != model.GroupWorkloads {
			t.Errorf("%s is %s/%s, want an info row in %s", key, got[key].Compare, got[key].Group, model.GroupWorkloads)
		}
	}
	phases := got["pod-total"].Extra
	if phases["Running"] != "3" || phases["Pending"] != "2" || phases["Succeeded"] != "1" {
		t.Errorf("phase breakdown = %v", phases)
	}
}

// A cluster with no pods reports zeros, not an absent row: zero crash-looping
// pods is the good answer and has to show as one.
func TestPodsReportsZeros(t *testing.T) {
	comps, err := Pods{}.Extract(context.Background(), &Clients{Typed: k8sfake.NewClientset()})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	got := byKey(comps)
	if len(got) != 3 || got["pod-crashloop"].Version != "0" {
		t.Errorf("components = %+v, want three zero rows", comps)
	}
}
