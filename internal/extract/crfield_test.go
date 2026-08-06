package extract

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var testGVR = schema.GroupVersionResource{
	Group: "core.libopenstorage.org", Version: "v1", Resource: "storageclusters",
}

func testExtractor() Extractor {
	return NewCRFieldExtractor("portworx-csi", "Portworx (CSI)", "csi",
		testGVR.Group, testGVR.Version, testGVR.Resource, []string{"status", "version"}, false)
}

// A cluster without the vendor CRD answers Forbidden rather than 404, because
// authorization runs before the unknown path is resolved. That must be silent,
// not a scrape error on every cluster that does not run the vendor.
func TestCRFieldExtractorSkipsAbsentCRD(t *testing.T) {
	typed := k8sfake.NewClientset() // discovery serves no groups
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{testGVR: "StorageClusterList"})

	listed := false
	dyn.PrependReactor("list", testGVR.Resource, func(k8stesting.Action) (bool, runtime.Object, error) {
		listed = true
		return true, nil, apierrors.NewForbidden(testGVR.GroupResource(), "", nil)
	})

	comps, err := testExtractor().Extract(context.Background(), &Clients{Typed: typed, Dynamic: dyn})
	if err != nil {
		t.Fatalf("absent CRD should not error, got %v", err)
	}
	if len(comps) != 0 {
		t.Errorf("absent CRD should yield no components, got %d", len(comps))
	}
	if listed {
		t.Error("absent CRD should not be listed at all")
	}
}

func TestCRFieldExtractorReadsVersion(t *testing.T) {
	typed := k8sfake.NewClientset()
	typed.Resources = []*metav1.APIResourceList{{
		GroupVersion: testGVR.GroupVersion().String(),
		APIResources: []metav1.APIResource{{Name: testGVR.Resource, Namespaced: true, Kind: "StorageCluster"}},
	}}

	cr := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": testGVR.GroupVersion().String(),
		"kind":       "StorageCluster",
		"metadata":   map[string]any{"name": "px", "namespace": "portworx"},
		"status":     map[string]any{"version": "3.1.0"},
	}}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{testGVR: "StorageClusterList"}, cr)

	comps, err := testExtractor().Extract(context.Background(), &Clients{Typed: typed, Dynamic: dyn})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(comps) != 1 {
		t.Fatalf("want 1 component, got %d", len(comps))
	}
	if comps[0].Version != "3.1.0" || comps[0].Namespace != "portworx" {
		t.Errorf("got %+v, want version 3.1.0 in namespace portworx", comps[0])
	}
}

// A resource that discovery does serve but that fails for a real reason still
// reports the error, so a broken read-only account is not silently ignored.
func TestCRFieldExtractorReportsRealErrors(t *testing.T) {
	typed := k8sfake.NewClientset()
	typed.Resources = []*metav1.APIResourceList{{
		GroupVersion: testGVR.GroupVersion().String(),
		APIResources: []metav1.APIResource{{Name: testGVR.Resource, Namespaced: true, Kind: "StorageCluster"}},
	}}

	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{testGVR: "StorageClusterList"})
	dyn.PrependReactor("list", testGVR.Resource, func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(testGVR.GroupResource(), "", nil)
	})

	if _, err := testExtractor().Extract(context.Background(), &Clients{Typed: typed, Dynamic: dyn}); err == nil {
		t.Error("forbidden on an installed CRD should be reported")
	}
}

// Discovery is asked once per group/version, however many extractors ask.
func TestHasResourceCachesDiscovery(t *testing.T) {
	typed := k8sfake.NewClientset()
	calls := 0
	typed.PrependReactor("get", "resource", func(k8stesting.Action) (bool, runtime.Object, error) {
		calls++
		return false, nil, nil
	})

	c := &Clients{Typed: typed}
	for range 3 {
		if c.HasResource(testGVR) {
			t.Fatal("resource should not be served")
		}
	}
	if calls != 1 {
		t.Errorf("discovery called %d times, want 1", calls)
	}
}
