package extract

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/croz-ltd/cluster-comparator/internal/model"
)

// OpenShift reads the cluster release from the ClusterVersion "version" object.
type OpenShift struct{}

func (OpenShift) Key() string { return "openshift" }

var clusterVersionGVR = schema.GroupVersionResource{
	Group: "config.openshift.io", Version: "v1", Resource: "clusterversions",
}

func (OpenShift) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	obj, err := c.Dynamic.Resource(clusterVersionGVR).Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	ver, _, _ := unstructured.NestedString(obj.Object, "status", "desired", "version")
	channel, _, _ := unstructured.NestedString(obj.Object, "spec", "channel")

	comps := []model.Component{{
		Key:     "openshift",
		Name:    "OpenShift",
		Group:   model.GroupOpenShift,
		Compare: model.CompareVersion,
		Kind:    "openshift",
		Version: ver,
	}}
	// Update channel: a config value compared for consistency across the fleet.
	comps = append(comps, model.Component{
		Key:     "openshift-channel",
		Name:    "Update channel",
		Group:   model.GroupOpenShift,
		Compare: model.CompareMatch,
		Kind:    "openshift",
		Version: channel,
	})
	return comps, nil
}
