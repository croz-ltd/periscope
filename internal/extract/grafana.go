package extract

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/croz-ltd/periscope/internal/model"
	"github.com/croz-ltd/periscope/internal/version"
)

// Grafana reads the version of the Grafana the grafana-operator runs, which is
// not the same fact as the operator's own version. OLM already reports the
// operator, but the instance it manages is pinned separately in the Grafana CR,
// so two clusters on the identical operator can serve Grafana versions a year
// apart and nothing in the operator row shows it.
type Grafana struct{}

func (Grafana) Key() string { return "grafana" }

var grafanaGVR = schema.GroupVersionResource{
	Group: "grafana.integreatly.org", Version: "v1beta1", Resource: "grafanas",
}

func (Grafana) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	if !c.HasResource(grafanaGVR) {
		return nil, nil // grafana-operator's Grafana CRD is not installed here
	}
	list, err := c.Dynamic.Resource(grafanaGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(list.Items) == 0 {
		return nil, nil // operator installed, no instance declared
	}

	// One cluster can run several Grafana instances (one per tenant is a common
	// pattern), and they all share one row. Keying rows by instance name lines up
	// only for clusters that name their instances the same way, and leaves everyone
	// else comparing against "not installed". So the row shows the oldest instance,
	// because that is the one behind the fleet, and the tooltip lists every instance
	// so the number leads back to a CR.
	detail := make(map[string]string, len(list.Items))
	var pick, pickNS string
	for i, item := range list.Items {
		ver := grafanaVersion(item.Object)
		detail[item.GetNamespace()+"/"+item.GetName()] = versionOrDefault(ver)
		if i == 0 || olderVersion(ver, pick) {
			pick, pickNS = ver, item.GetNamespace()
		}
	}

	comp := model.Component{
		Key:       "grafana",
		Name:      "Grafana",
		Group:     model.GroupOperators,
		Compare:   model.CompareVersion,
		Kind:      "managed",
		Version:   pick,
		Namespace: pickNS,
	}
	if len(detail) > 1 {
		comp.Extra = detail
	}
	return []model.Component{comp}, nil
}

// grafanaVersion reads the version a Grafana CR declares. spec.version is what
// pins the image. When it is left out, the operator installs a default version
// of its own, which recent operator releases report back in status.version.
func grafanaVersion(obj map[string]any) string {
	if v, _, _ := unstructured.NestedString(obj, "spec", "version"); v != "" {
		return v
	}
	v, _, _ := unstructured.NestedString(obj, "status", "version")
	return v
}

// olderVersion reports whether a takes the place of b. A parseable version
// always beats an unparseable or missing one, because an instance that states
// its version is the fact worth comparing. Between two parseable versions, the
// lower one wins.
func olderVersion(a, b string) bool {
	pa, pb := version.Parse(a), version.Parse(b)
	if pa.OK != pb.OK {
		return pa.OK
	}
	if pa.OK {
		return version.Compare(pa, pb) < 0
	}
	return a != "" && b == ""
}

// versionOrDefault labels an instance that pins no version, so the tooltip says
// "the operator chose this one" rather than showing an empty line.
func versionOrDefault(v string) string {
	if v == "" {
		return "(operator default)"
	}
	return v
}
