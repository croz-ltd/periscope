package extract

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/croz-ltd/periscope/internal/model"
)

// MachineConfigPools lists the cluster's MachineConfigPools and surfaces each
// pool's maxUnavailable (rollout batch size) for cross-cluster comparison.
type MachineConfigPools struct{}

func (MachineConfigPools) Key() string { return "machineconfigpools" }

var mcpGVR = schema.GroupVersionResource{
	Group: "machineconfiguration.openshift.io", Version: "v1", Resource: "machineconfigpools",
}

func (MachineConfigPools) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	if !c.HasResource(mcpGVR) {
		return nil, nil // not an OpenShift/MCO cluster
	}
	list, err := c.Dynamic.Resource(mcpGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []model.Component
	for _, item := range list.Items {
		name := item.GetName()
		// spec.maxUnavailable is an IntOrString; default is 1 when unset.
		maxUnavail := "1"
		if v, ok, _ := unstructured.NestedFieldNoCopy(item.Object, "spec", "maxUnavailable"); ok && v != nil {
			maxUnavail = fmt.Sprintf("%v", v)
		}

		extra := map[string]string{}
		if n, ok, _ := unstructured.NestedInt64(item.Object, "status", "machineCount"); ok {
			extra["machineCount"] = fmt.Sprintf("%d", n)
		}
		if n, ok, _ := unstructured.NestedInt64(item.Object, "status", "readyMachineCount"); ok {
			extra["readyMachineCount"] = fmt.Sprintf("%d", n)
		}
		if paused, ok, _ := unstructured.NestedBool(item.Object, "spec", "paused"); ok && paused {
			extra["paused"] = "true"
		}

		out = append(out, model.Component{
			Key:     "mcp-" + name,
			Name:    "MCP: " + name,
			Group:   model.GroupMCP,
			Compare: model.CompareMatch,
			Kind:    "mcp",
			Version: maxUnavail,
			Extra:   extra,
		})
	}
	return out, nil
}
