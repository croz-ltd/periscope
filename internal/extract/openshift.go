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
	extra := map[string]string{}
	if channel != "" {
		extra["channel"] = channel
	}
	return []model.Component{{
		Key:     "openshift",
		Name:    "OpenShift",
		Kind:    "openshift",
		Version: ver,
		Extra:   extra,
	}}, nil
}
