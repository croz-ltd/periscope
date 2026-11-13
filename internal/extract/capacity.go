package extract

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/croz-ltd/periscope/internal/model"
)

// Capacity sums the CPU and memory of all nodes of a cluster, and tells how much
// of it the pods request and how much is free. Nodes differ in size, so each
// node is read on its own and the values are added.
//
//   - total is the sum of status.capacity of every node.
//   - used is the sum of the requests of the pods that run on a node. The
//     scheduler places pods by requests, not by live usage, so requests tell
//     what a new pod or VM can still get.
//   - free is the sum of status.allocatable minus used. Allocatable already
//     leaves out the system reservation of each node, so the reservation is
//     not subtracted again. total minus allocatable is in the tooltip.
//
// When OpenShift Virtualization is installed, the extractor also reports the VM
// view of the same nodes. It counts only the nodes that can run a VM (label
// kubevirt.io/schedulable=true, not cordoned):
//
//   - vCPUs = cores x vmiCPUAllocationRatio of the HyperConverged CR (default
//     10). A VM with N vCPUs requests N / ratio cores, so each core of the
//     node gives "ratio" vCPUs. Total uses allocatable cores, used uses the
//     requested cores, and free = total - used.
//   - VM memory = memory x memoryOvercommitPercentage / 100 (default 100). VM
//     memory is not overcommitted unless that setting is above 100.
//
// The values are whole cores and whole GiB, so the Charts view can draw them.
// The tooltip carries the exact values.
type Capacity struct{}

func (Capacity) Key() string { return "capacity" }

const (
	// defaultCPUAllocationRatio is the vCPU to core ratio that OpenShift
	// Virtualization uses when the HyperConverged CR does not set one.
	defaultCPUAllocationRatio = 10
	// defaultMemoryOvercommit is 100 %: VM memory is not overcommitted.
	defaultMemoryOvercommit = 100

	labelVMSchedulable = "kubevirt.io/schedulable"
	gib                = 1 << 30
)

// resources holds CPU in millicores and memory in bytes.
type resources struct {
	cpu, mem int64
}

func (r *resources) add(o resources) {
	r.cpu += o.cpu
	r.mem += o.mem
}

func resourcesOf(list corev1.ResourceList) resources {
	return resources{cpu: list.Cpu().MilliValue(), mem: list.Memory().Value()}
}

func (Capacity) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	nodes, err := c.Typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	if len(nodes.Items) == 0 {
		return nil, nil
	}

	var capacity, allocatable, vmAllocatable resources
	byRole := map[string]resources{}
	vmNode := map[string]bool{}
	for i := range nodes.Items {
		n := &nodes.Items[i]
		capacity.add(resourcesOf(n.Status.Capacity))
		allocatable.add(resourcesOf(n.Status.Allocatable))
		for label := range n.Labels {
			if role, ok := strings.CutPrefix(label, "node-role.kubernetes.io/"); ok && role != "" {
				r := byRole[role]
				r.add(resourcesOf(n.Status.Capacity))
				byRole[role] = r
			}
		}
		if n.Labels[labelVMSchedulable] == "true" && !n.Spec.Unschedulable {
			vmNode[n.Name] = true
			vmAllocatable.add(resourcesOf(n.Status.Allocatable))
		}
	}

	requested, vmRequested, err := podRequests(ctx, c, vmNode)
	if err != nil {
		return nil, err
	}

	cpuTotal := capacityInfo("cpu-capacity", "CPU cores: total", cores(capacity.cpu), map[string]string{
		"allocatable": coresText(allocatable.cpu),
		"reserved":    coresText(capacity.cpu - allocatable.cpu),
	})
	memTotal := capacityInfo("mem-capacity", "Memory GiB: total", gibs(capacity.mem), map[string]string{
		"allocatable": gibText(allocatable.mem),
		"reserved":    gibText(capacity.mem - allocatable.mem),
	})
	for role, r := range byRole {
		cpuTotal.Extra["role "+role] = coresText(r.cpu)
		memTotal.Extra["role "+role] = gibText(r.mem)
	}
	cpuFree := nonNegative(allocatable.cpu - requested.cpu)
	memFree := nonNegative(allocatable.mem - requested.mem)
	out := []model.Component{
		cpuTotal,
		capacityInfo("cpu-requested", "CPU cores: used (requested)", cores(requested.cpu),
			map[string]string{"exact": coresText(requested.cpu), "of allocatable": percent(requested.cpu, allocatable.cpu)}),
		capacityInfo("cpu-free", "CPU cores: free", cores(cpuFree), map[string]string{"exact": coresText(cpuFree)}),
		memTotal,
		capacityInfo("mem-requested", "Memory GiB: used (requested)", gibs(requested.mem),
			map[string]string{"exact": gibText(requested.mem), "of allocatable": percent(requested.mem, allocatable.mem)}),
		capacityInfo("mem-free", "Memory GiB: free", gibs(memFree), map[string]string{"exact": gibText(memFree)}),
	}

	if !c.HasResource(hyperConvergedGVR) {
		return out, nil // OpenShift Virtualization not installed, no VM view
	}
	hcos, err := c.Dynamic.Resource(hyperConvergedGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	if len(hcos.Items) == 0 {
		return out, nil
	}
	ratio, overcommit := virtRatios(hcos.Items[0].Object)
	return append(out, vmCapacity(vmAllocatable, vmRequested, len(vmNode), ratio, overcommit)...), nil
}

// podRequests sums the requests of the pods placed on a node, for the whole
// cluster and for the nodes that can run a VM. Finished pods hold no resources,
// so the list leaves them out. It is paged like the Pods extractor.
func podRequests(ctx context.Context, c *Clients, vmNode map[string]bool) (all, vm resources, err error) {
	opts := metav1.ListOptions{
		Limit:         podPageSize,
		FieldSelector: "status.phase!=Succeeded,status.phase!=Failed",
	}
	for {
		page, err := c.Typed.CoreV1().Pods(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return all, vm, err
		}
		for i := range page.Items {
			p := &page.Items[i]
			if p.Spec.NodeName == "" || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
				continue // not placed yet, or finished
			}
			r := podRequest(p)
			all.add(r)
			if vmNode[p.Spec.NodeName] {
				vm.add(r)
			}
		}
		if page.Continue == "" {
			return all, vm, nil
		}
		opts.Continue = page.Continue
	}
}

// podRequest is the amount the scheduler reserves for a pod, by the Kubernetes
// rule: the app containers plus the sidecars (init containers that keep
// running), or the largest plain init container plus the sidecars started
// before it, whichever is larger, plus the pod overhead.
func podRequest(p *corev1.Pod) resources {
	var sum, sidecars, initMax resources
	for _, ctr := range p.Spec.Containers {
		sum.add(resourcesOf(ctr.Resources.Requests))
	}
	for _, ctr := range p.Spec.InitContainers {
		r := resourcesOf(ctr.Resources.Requests)
		if ctr.RestartPolicy != nil && *ctr.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			sidecars.add(r)
			continue
		}
		initMax.cpu = max(initMax.cpu, sidecars.cpu+r.cpu)
		initMax.mem = max(initMax.mem, sidecars.mem+r.mem)
	}
	sum.add(sidecars)
	out := resources{cpu: max(sum.cpu, initMax.cpu), mem: max(sum.mem, initMax.mem)}
	out.add(resourcesOf(p.Spec.Overhead))
	return out
}

// virtRatios reads the vCPU allocation ratio and the memory overcommit
// percentage of the HyperConverged CR, with the defaults for unset values.
func virtRatios(hco map[string]any) (ratio, overcommit int64) {
	ratio, overcommit = defaultCPUAllocationRatio, defaultMemoryOvercommit
	if v, ok := positiveInt(hco, "spec", "resourceRequirements", "vmiCPUAllocationRatio"); ok {
		ratio = v
	}
	if v, ok := positiveInt(hco, "spec", "higherWorkloadDensity", "memoryOvercommitPercentage"); ok {
		overcommit = v
	}
	return ratio, overcommit
}

func positiveInt(obj map[string]any, path ...string) (int64, bool) {
	if v, ok, _ := unstructured.NestedInt64(obj, path...); ok && v > 0 {
		return v, true
	}
	if f, ok, _ := unstructured.NestedFloat64(obj, path...); ok && f > 0 {
		return int64(f), true
	}
	return 0, false
}

// vmCapacity is the VM view of the nodes that can run a VM.
func vmCapacity(alloc, req resources, nodes int, ratio, overcommit int64) []model.Component {
	vcpuTotal := alloc.cpu * ratio / 1000
	vcpuUsed := req.cpu * ratio / 1000
	vcpuFree := nonNegative(vcpuTotal - vcpuUsed)
	memTotal := alloc.mem * overcommit / 100
	memUsed := req.mem * overcommit / 100
	memFree := nonNegative(memTotal - memUsed)

	basis := map[string]string{
		"VM nodes":          strconv.Itoa(nodes),
		"allocatable cores": coresText(alloc.cpu),
		"allocation ratio":  fmt.Sprintf("%d:1", ratio),
	}
	memBasis := map[string]string{
		"VM nodes":           strconv.Itoa(nodes),
		"allocatable memory": gibText(alloc.mem),
		"memory overcommit":  fmt.Sprintf("%d %%", overcommit),
	}
	return []model.Component{
		capacityInfo("vm-vcpu-total", "VM vCPUs: total", int(vcpuTotal), basis),
		capacityInfo("vm-vcpu-used", "VM vCPUs: used (requested)", int(vcpuUsed),
			map[string]string{"requested cores": coresText(req.cpu), "of total": percent(vcpuUsed, vcpuTotal)}),
		capacityInfo("vm-vcpu-free", "VM vCPUs: free", int(vcpuFree), nil),
		capacityInfo("vm-memory-total", "VM memory GiB: total", gibs(memTotal), memBasis),
		capacityInfo("vm-memory-used", "VM memory GiB: used (requested)", gibs(memUsed),
			map[string]string{"exact": gibText(memUsed), "of total": percent(memUsed, memTotal)}),
		capacityInfo("vm-memory-free", "VM memory GiB: free", gibs(memFree), map[string]string{"exact": gibText(memFree)}),
	}
}

func capacityInfo(key, name string, value int, extra map[string]string) model.Component {
	return model.Component{
		Key: key, Name: name, Group: model.GroupCapacity,
		Compare: model.CompareInfo, Kind: "capacity",
		Version: strconv.Itoa(value), Extra: extra,
	}
}

func nonNegative(v int64) int64 { return max(v, 0) }

// cores rounds millicores to whole cores, and gibs rounds bytes to whole GiB.
func cores(milli int64) int { return int(math.Round(float64(milli) / 1000)) }
func gibs(bytes int64) int  { return int(math.Round(float64(bytes) / gib)) }

func coresText(milli int64) string {
	return strconv.FormatFloat(float64(milli)/1000, 'f', -1, 64) + " cores"
}

func gibText(bytes int64) string {
	return strconv.FormatFloat(float64(bytes)/gib, 'f', 1, 64) + " GiB"
}

func percent(part, whole int64) string {
	if whole <= 0 {
		return "0 %"
	}
	return strconv.FormatFloat(float64(part)*100/float64(whole), 'f', 0, 64) + " %"
}
