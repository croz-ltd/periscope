package extract

import (
	"context"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/croz-ltd/periscope/internal/model"
)

// OLM enumerates every installed operator via its ClusterServiceVersion. Rows
// are keyed by the stable OLM package name (resolved from Subscriptions) so the
// same operator lines up across clusters even as its version changes.
type OLM struct{}

func (OLM) Key() string { return "olm" }

var (
	csvGVR = schema.GroupVersionResource{Group: "operators.coreos.com", Version: "v1alpha1", Resource: "clusterserviceversions"}
	subGVR = schema.GroupVersionResource{Group: "operators.coreos.com", Version: "v1alpha1", Resource: "subscriptions"}
)

func (OLM) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	// Map installedCSV -> package name for stable row keys (best-effort).
	pkgByCSV := map[string]string{}
	if subs, err := c.Dynamic.Resource(subGVR).List(ctx, metav1.ListOptions{}); err == nil {
		for _, s := range subs.Items {
			pkg, _, _ := unstructured.NestedString(s.Object, "spec", "name")
			installed, _, _ := unstructured.NestedString(s.Object, "status", "installedCSV")
			if pkg != "" && installed != "" {
				pkgByCSV[installed] = pkg
			}
		}
	}

	// OLM copies each CSV into every watched namespace; copies carry the
	// "olm.copiedFrom" label and CSV objects are large (embedded install strategy
	// + icons). Excluding copies keeps us to one object per actual install and
	// avoids listing thousands of duplicates (which OOMs the pod on real clusters).
	list, err := c.Dynamic.Resource(csvGVR).List(ctx, metav1.ListOptions{LabelSelector: "!olm.copiedFrom"})
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{} // an operator installed in several namespaces => dedupe originals by CSV name
	var out []model.Component
	for _, item := range list.Items {
		name := item.GetName()
		if seen[name] {
			continue
		}
		seen[name] = true

		ver, _, _ := unstructured.NestedString(item.Object, "spec", "version")
		display, _, _ := unstructured.NestedString(item.Object, "spec", "displayName")
		key := pkgByCSV[name]
		if key == "" {
			key = stripVersionSuffix(name)
		}
		if display == "" {
			display = key
		}
		out = append(out, model.Component{
			Key:       key,
			Name:      display,
			Group:     model.GroupOperators,
			Compare:   model.CompareVersion,
			Kind:      "operator",
			Version:   ver,
			Namespace: item.GetNamespace(),
		})
	}
	return out, nil
}

// stripVersionSuffix turns "portworx-operator.v3.1.0" into "portworx-operator".
func stripVersionSuffix(csvName string) string {
	if i := strings.Index(csvName, ".v"); i > 0 {
		return csvName[:i]
	}
	return csvName
}
