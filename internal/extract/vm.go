package extract

import (
	"context"
	"sort"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/croz-ltd/periscope/internal/model"
)

// virtInfo builds an informational OpenShift Virtualization statistic row.
func virtInfo(key, name string, count int) model.Component {
	return model.Component{
		Key: key, Name: name, Group: model.GroupVirt,
		Compare: model.CompareInfo, Kind: "virt", Version: strconv.Itoa(count),
	}
}

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

// VMSnapshots counts VirtualMachineSnapshots, plus a per-storage-class breakdown
// derived from each snapshot content's volume backups (where the storage class
// actually lives). All informational.
type VMSnapshots struct{}

func (VMSnapshots) Key() string { return "vm-snapshots" }

var (
	vmSnapshotGVR        = schema.GroupVersionResource{Group: "snapshot.kubevirt.io", Version: "v1alpha1", Resource: "virtualmachinesnapshots"}
	vmSnapshotContentGVR = schema.GroupVersionResource{Group: "snapshot.kubevirt.io", Version: "v1alpha1", Resource: "virtualmachinesnapshotcontents"}
)

func (VMSnapshots) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	snaps, err := c.Dynamic.Resource(vmSnapshotGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil // VM snapshots CRD not installed
		}
		return nil, err
	}
	// "VM snapshots" is the count of VirtualMachineSnapshot objects.
	out := []model.Component{virtInfo("vmsnapshot-total", "VM snapshots", len(snaps.Items))}

	// A VM snapshot can span several disks on different storage classes, so a
	// per-class breakdown of VM snapshots would overlap (a multi-disk snapshot
	// belongs to several classes) and not sum to the total. Instead we count the
	// individual volume snapshots (one per backed disk) per class: these
	// partition cleanly and sum to the "Snapshot volumes" total.
	if contents, err := c.Dynamic.Resource(vmSnapshotContentGVR).List(ctx, metav1.ListOptions{}); err == nil {
		byClass := map[string]int{}
		totalVolumes := 0
		for _, item := range contents.Items {
			vbs, ok, _ := unstructured.NestedSlice(item.Object, "spec", "volumeBackups")
			if !ok {
				continue
			}
			for _, v := range vbs {
				vb, ok := v.(map[string]interface{})
				if !ok {
					continue
				}
				sc, _, _ := unstructured.NestedString(vb, "persistentVolumeClaim", "spec", "storageClassName")
				if sc == "" {
					sc = "(none)"
				}
				byClass[sc]++
				totalVolumes++
			}
		}
		out = append(out, virtInfo("vmsnapshot-volumes", "Snapshot volumes", totalVolumes))
		classes := make([]string, 0, len(byClass))
		for sc := range byClass {
			classes = append(classes, sc)
		}
		sort.Strings(classes)
		for _, sc := range classes {
			out = append(out, virtInfo("vmsnapshot-vol-"+sc, "Snapshot volumes: "+sc, byClass[sc]))
		}
	}
	return out, nil
}

// VMTemplates counts OpenShift Templates labelled as KubeVirt VM templates.
type VMTemplates struct{}

func (VMTemplates) Key() string { return "vm-templates" }

var templateGVR = schema.GroupVersionResource{Group: "template.openshift.io", Version: "v1", Resource: "templates"}

func (VMTemplates) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	list, err := c.Dynamic.Resource(templateGVR).List(ctx, metav1.ListOptions{
		LabelSelector: "template.kubevirt.io/type=vm",
	})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil // Templates API not present
		}
		return nil, err
	}
	return []model.Component{virtInfo("vm-templates", "VM templates", len(list.Items))}, nil
}
