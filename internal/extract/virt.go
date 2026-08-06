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

// Virtualization reads the OpenShift Virtualization HyperConverged CR and
// surfaces a few key settings for cross-cluster consistency comparison.
type Virtualization struct{}

func (Virtualization) Key() string { return "virtualization" }

var hyperConvergedGVR = schema.GroupVersionResource{
	Group: "hco.kubevirt.io", Version: "v1beta1", Resource: "hyperconvergeds",
}

func (Virtualization) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	if !c.HasResource(hyperConvergedGVR) {
		return nil, nil // OpenShift Virtualization (HCO CRD) not installed
	}
	list, err := c.Dynamic.Resource(hyperConvergedGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(list.Items) == 0 {
		return nil, nil // OpenShift Virtualization not configured
	}
	hco := list.Items[0].Object // typically a single kubevirt-hyperconverged CR

	fields := []struct {
		key, name string
		path      []string
	}{
		{"hco-vm-state-sc", "VM state storage class", []string{"spec", "vmStateStorageClass"}},
		{"hco-scratch-sc", "Scratch space storage class", []string{"spec", "scratchSpaceStorageClass"}},
		{"hco-mem-overcommit", "Memory overcommit %", []string{"spec", "higherWorkloadDensity", "memoryOvercommitPercentage"}},
		{"hco-cpu-ratio", "vCPU allocation ratio", []string{"spec", "resourceRequirements", "vmiCPUAllocationRatio"}},
	}

	out := make([]model.Component, 0, len(fields))
	for _, f := range fields {
		out = append(out, model.Component{
			Key:     f.key,
			Name:    f.name,
			Group:   model.GroupVirt,
			Compare: model.CompareMatch,
			Kind:    "virt",
			Version: nestedScalar(hco, f.path...),
		})
	}
	return out, nil
}

// nestedScalar reads a nested field as a display string, handling int/float/string.
func nestedScalar(obj map[string]any, path ...string) string {
	if i, ok, _ := unstructured.NestedInt64(obj, path...); ok {
		return strconv.FormatInt(i, 10)
	}
	if f, ok, _ := unstructured.NestedFloat64(obj, path...); ok {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	if s, ok, _ := unstructured.NestedString(obj, path...); ok {
		return s
	}
	return ""
}
