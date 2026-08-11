package extract

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// grafanaCluster builds clients that serve the Grafana CRD and the given instances.
func grafanaCluster(objs ...runtime.Object) *Clients {
	typed := k8sfake.NewClientset()
	typed.Resources = []*metav1.APIResourceList{{
		GroupVersion: grafanaGVR.GroupVersion().String(),
		APIResources: []metav1.APIResource{{Name: grafanaGVR.Resource, Namespaced: true, Kind: "Grafana"}},
	}}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{grafanaGVR: "GrafanaList"}, objs...)
	return &Clients{Typed: typed, Dynamic: dyn}
}

func grafanaCR(namespace, name string, spec, status map[string]any) *unstructured.Unstructured {
	obj := map[string]any{
		"apiVersion": grafanaGVR.GroupVersion().String(),
		"kind":       "Grafana",
		"metadata":   map[string]any{"name": name, "namespace": namespace},
	}
	if spec != nil {
		obj["spec"] = spec
	}
	if status != nil {
		obj["status"] = status
	}
	return &unstructured.Unstructured{Object: obj}
}

func TestGrafanaReadsSpecVersion(t *testing.T) {
	c := grafanaCluster(grafanaCR("monitoring", "grafana", map[string]any{"version": "13.1.3"}, nil))

	comps, err := Grafana{}.Extract(context.Background(), c)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(comps) != 1 {
		t.Fatalf("want 1 component, got %d", len(comps))
	}
	got := comps[0]
	if got.Key != "grafana" || got.Version != "13.1.3" || got.Namespace != "monitoring" {
		t.Errorf("got %+v, want grafana 13.1.3 in monitoring", got)
	}
	if got.Extra != nil {
		t.Errorf("a single instance needs no per-instance detail, got %v", got.Extra)
	}
}

// An instance that pins no version runs whatever the operator defaults to, and
// recent operator releases report that back on the status.
func TestGrafanaFallsBackToStatusVersion(t *testing.T) {
	c := grafanaCluster(grafanaCR("monitoring", "grafana", nil, map[string]any{"version": "12.0.1"}))

	comps, err := Grafana{}.Extract(context.Background(), c)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(comps) != 1 || comps[0].Version != "12.0.1" {
		t.Fatalf("got %+v, want version 12.0.1 from the status", comps)
	}
}

// Several instances on one cluster share one row, showing the one that is behind
// and listing all of them for the tooltip.
func TestGrafanaCollapsesInstancesToTheOldest(t *testing.T) {
	c := grafanaCluster(
		grafanaCR("monitoring", "central", map[string]any{"version": "13.1.3"}, nil),
		grafanaCR("team-a", "tenant", map[string]any{"version": "11.6.0"}, nil),
	)

	comps, err := Grafana{}.Extract(context.Background(), c)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(comps) != 1 {
		t.Fatalf("want 1 component, got %d", len(comps))
	}
	got := comps[0]
	if got.Version != "11.6.0" || got.Namespace != "team-a" {
		t.Errorf("got %+v, want the oldest instance (11.6.0 in team-a)", got)
	}
	want := map[string]string{"monitoring/central": "13.1.3", "team-a/tenant": "11.6.0"}
	for k, v := range want {
		if got.Extra[k] != v {
			t.Errorf("extra[%q] = %q, want %q (got %v)", k, got.Extra[k], v, got.Extra)
		}
	}
}

// A version nobody stated must not win over one that was, or a cluster running a
// pinned Grafana next to a defaulted one would compare as unknown.
func TestGrafanaPrefersAStatedVersion(t *testing.T) {
	c := grafanaCluster(
		grafanaCR("team-b", "unpinned", nil, nil),
		grafanaCR("monitoring", "central", map[string]any{"version": "13.1.3"}, nil),
	)

	comps, err := Grafana{}.Extract(context.Background(), c)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(comps) != 1 || comps[0].Version != "13.1.3" {
		t.Fatalf("got %+v, want 13.1.3", comps)
	}
	if comps[0].Extra["team-b/unpinned"] != "(operator default)" {
		t.Errorf("unpinned instance should be labelled, got %v", comps[0].Extra)
	}
}

// The CRD is absent on every cluster that does not run grafana-operator, which
// must stay silent rather than erroring or inventing an empty row.
func TestGrafanaSkipsAbsentCRD(t *testing.T) {
	typed := k8sfake.NewClientset() // discovery serves no groups
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{grafanaGVR: "GrafanaList"})

	comps, err := Grafana{}.Extract(context.Background(), &Clients{Typed: typed, Dynamic: dyn})
	if err != nil {
		t.Fatalf("absent CRD should not error, got %v", err)
	}
	if len(comps) != 0 {
		t.Errorf("absent CRD should yield no components, got %d", len(comps))
	}
}

// The operator can be installed with no instance declared, which is not drift
// and must not produce a row full of empty cells.
func TestGrafanaSkipsWhenNoInstance(t *testing.T) {
	comps, err := Grafana{}.Extract(context.Background(), grafanaCluster())
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(comps) != 0 {
		t.Errorf("no instances should yield no components, got %d", len(comps))
	}
}
