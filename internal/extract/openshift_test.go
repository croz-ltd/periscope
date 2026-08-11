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

	"github.com/croz-ltd/periscope/internal/model"
)

func byKey(comps []model.Component) map[string]model.Component {
	out := map[string]model.Component{}
	for _, c := range comps {
		out[c.Key] = c
	}
	return out
}

func clusterVersion(status map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "config.openshift.io/v1",
		"kind":       "ClusterVersion",
		"metadata":   map[string]any{"name": "version"},
		"spec":       map[string]any{"channel": "stable-4.14"},
		"status":     status,
	}}
}

func openShiftClients(cv *unstructured.Unstructured) *Clients {
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{clusterVersionGVR: "ClusterVersionList"}, cv)
	return &Clients{Typed: k8sfake.NewClientset(), Dynamic: dyn}
}

// The cluster has already asked its update service, so what it can move to is
// read from its own status rather than fetched from the hub.
func TestOpenShiftReportsAvailableUpdate(t *testing.T) {
	cv := clusterVersion(map[string]any{
		"desired": map[string]any{"version": "4.14.9"},
		"availableUpdates": []any{
			map[string]any{"version": "4.14.10"},
			map[string]any{"version": "4.14.12"}, // newest, whatever the order
			map[string]any{"version": "4.14.11"},
		},
	})

	comps, err := OpenShift{}.Extract(context.Background(), openShiftClients(cv))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	got := byKey(comps)
	if got["openshift"].Version != "4.14.9" {
		t.Errorf("release = %q, want 4.14.9", got["openshift"].Version)
	}
	if got["openshift-update-available"].Version != "4.14.12" {
		t.Errorf("update available = %q, want the newest offer 4.14.12", got["openshift-update-available"].Version)
	}
}

// A cluster at the head of its channel has nowhere to go, and an empty row
// reads as "no data" rather than "nothing to do".
func TestOpenShiftOmitsUpdateRowWhenAtHead(t *testing.T) {
	cv := clusterVersion(map[string]any{"desired": map[string]any{"version": "4.14.12"}})

	comps, err := OpenShift{}.Extract(context.Background(), openShiftClients(cv))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, ok := byKey(comps)["openshift-update-available"]; ok {
		t.Error("no available updates produced a row, want none")
	}
}

// Upgradeable=False is the difference between "behind" and "stuck".
func TestOpenShiftReportsUpgradeBlocked(t *testing.T) {
	cv := clusterVersion(map[string]any{
		"desired": map[string]any{"version": "4.14.9"},
		"conditions": []any{
			map[string]any{"type": "Available", "status": "True"},
			map[string]any{
				"type": "Upgradeable", "status": "False",
				"reason":  "AdminAckRequired",
				"message": "Kubernetes 1.29 API removals require admin acknowledgement",
			},
			map[string]any{"type": "Progressing", "status": "True", "message": "Working towards 4.14.12"},
		},
	})

	got := byKey(mustExtract(t, cv))
	blocked, ok := got["openshift-upgradeable"]
	if !ok {
		t.Fatal("Upgradeable=False must surface")
	}
	if blocked.Version != "AdminAckRequired" {
		t.Errorf("reason = %q", blocked.Version)
	}
	if blocked.Extra["message"] == "" {
		t.Error("the message is the actual to-do item, keep it")
	}
	if _, ok := got["openshift-updating"]; !ok {
		t.Error("an upgrade in flight explains a version that disagrees with the fleet")
	}
}

func TestOpenShiftQuietWhenUpgradeable(t *testing.T) {
	cv := clusterVersion(map[string]any{
		"desired":    map[string]any{"version": "4.14.9"},
		"conditions": []any{map[string]any{"type": "Upgradeable", "status": "True"}},
	})
	got := byKey(mustExtract(t, cv))
	if _, ok := got["openshift-upgradeable"]; ok {
		t.Error("a healthy cluster carries a blocked row, want none")
	}
	if _, ok := got["openshift-updating"]; ok {
		t.Error("no upgrade running, no row")
	}
}

func mustExtract(t *testing.T, cv *unstructured.Unstructured) []model.Component {
	t.Helper()
	comps, err := OpenShift{}.Extract(context.Background(), openShiftClients(cv))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return comps
}

func TestConsoleBannerNamesTheColumn(t *testing.T) {
	notification := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "console.openshift.io/v1",
		"kind":       "ConsoleNotification",
		"metadata":   map[string]any{"name": bannerName},
		"spec": map[string]any{
			"text":            "PRODUCTION - Frankfurt",
			"color":           "#FFFFFF",
			"backgroundColor": "#C9190B",
		},
	}}

	typed := k8sfake.NewClientset()
	typed.Resources = []*metav1.APIResourceList{{
		GroupVersion: consoleNotificationGVR.GroupVersion().String(),
		APIResources: []metav1.APIResource{{Name: consoleNotificationGVR.Resource, Kind: "ConsoleNotification"}},
	}}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{consoleNotificationGVR: "ConsoleNotificationList"}, notification)

	comps, err := ConsoleBanner{}.Extract(context.Background(), &Clients{Typed: typed, Dynamic: dyn})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(comps) != 1 {
		t.Fatalf("want the banner, got %d components", len(comps))
	}
	c := comps[0]
	if c.Key != model.KeyClusterBanner {
		t.Errorf("key = %q, must be the well-known banner key so the matrix lifts it into the header", c.Key)
	}
	if c.Version != "PRODUCTION - Frankfurt" {
		t.Errorf("label = %q", c.Version)
	}
	if c.Extra["backgroundColor"] != "#C9190B" || c.Extra["color"] != "#FFFFFF" {
		t.Errorf("colours lost: %+v", c.Extra)
	}
}

// Most clusters have no such banner, and that is not a failure.
func TestConsoleBannerAbsentIsSilent(t *testing.T) {
	typed := k8sfake.NewClientset()
	typed.Resources = []*metav1.APIResourceList{{
		GroupVersion: consoleNotificationGVR.GroupVersion().String(),
		APIResources: []metav1.APIResource{{Name: consoleNotificationGVR.Resource, Kind: "ConsoleNotification"}},
	}}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{consoleNotificationGVR: "ConsoleNotificationList"})

	comps, err := ConsoleBanner{}.Extract(context.Background(), &Clients{Typed: typed, Dynamic: dyn})
	if err != nil {
		t.Fatalf("missing banner must not error: %v", err)
	}
	if len(comps) != 0 {
		t.Errorf("want nothing, got %+v", comps)
	}
}
