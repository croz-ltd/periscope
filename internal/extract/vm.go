package extract

import (
	"context"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/croz-ltd/periscope/internal/model"
)

// VirtualMachines counts OpenShift Virtualization / KubeVirt VirtualMachines by
// their printable status. Counts are informational (they differ per cluster by
// design), with the full per-status breakdown in the total's tooltip.
type VirtualMachines struct{}

func (VirtualMachines) Key() string { return "virtualmachines" }

var vmGVR = schema.GroupVersionResource{
	Group: "kubevirt.io", Version: "v1", Resource: "virtualmachines",
}

func (VirtualMachines) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	list, err := c.Dynamic.Resource(vmGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil // KubeVirt/CNV not installed
		}
		return nil, err
	}

	byStatus := map[string]int{}
	for _, item := range list.Items {
		st, _, _ := unstructured.NestedString(item.Object, "status", "printableStatus")
		if st == "" {
			st = "Unknown"
		}
		byStatus[st]++
	}

	// Full breakdown on the total row's tooltip.
	extra := make(map[string]string, len(byStatus))
	for st, n := range byStatus {
		extra[st] = strconv.Itoa(n)
	}

	info := func(key, name string, count int, ex map[string]string) model.Component {
		return model.Component{
			Key: key, Name: name, Group: model.GroupVirt,
			Compare: model.CompareInfo, Kind: "virt",
			Version: strconv.Itoa(count), Extra: ex,
		}
	}
	return []model.Component{
		info("vm-total", "Virtual machines", len(list.Items), extra),
		info("vm-running", "VMs running", byStatus["Running"], nil),
		info("vm-stopped", "VMs stopped", byStatus["Stopped"], nil),
	}, nil
}
