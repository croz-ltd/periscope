// Package extract pulls version information from a cluster. A generic OLM path
// covers all installed operators; hand-written extractors handle special cases
// (OpenShift release, node fleet, and CSI driver versions buried in vendor CRs).
package extract

import (
	"context"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/croz-ltd/periscope/internal/logging"
	"github.com/croz-ltd/periscope/internal/model"
)

// Clients bundles the per-cluster clients an extractor may use.
type Clients struct {
	Typed   kubernetes.Interface
	Dynamic dynamic.Interface
	Host    string // API server URL, for TLS cert inspection

	mu     sync.Mutex
	served map[string]map[string]bool // group/version -> resource -> served
}

// HasResource reports whether the cluster serves the given resource.
//
// Optional CRs (Portworx, Dell, KubeVirt) are absent on most clusters, and
// listing a resource whose CRD is not installed does not come back as 404:
// authorization runs first, and a read-only service account has no rule for an
// unknown resource, so the API server answers Forbidden. That looked like a
// scrape failure and put an error on every cluster not running that vendor.
// Asking discovery first tells absence and misconfiguration apart.
//
// Results are cached per Clients (one scrape of one cluster), so each group is
// discovered once no matter how many extractors ask. When discovery itself
// fails the answer is true, leaving the extractor to report the real error.
func (c *Clients) HasResource(gvr schema.GroupVersionResource) bool {
	gv := gvr.GroupVersion().String()

	c.mu.Lock()
	defer c.mu.Unlock()

	res, cached := c.served[gv]
	if !cached {
		res = c.discover(gv)
		if c.served == nil {
			c.served = make(map[string]map[string]bool)
		}
		c.served[gv] = res
	}
	if res == nil {
		return true // discovery unavailable, so do not suppress anything
	}
	served := res[gvr.Resource]
	if !served {
		// Skipping is the normal case for a vendor CRD nobody installed, but it
		// is also the answer to "why is this row missing on that cluster".
		logging.For("extract").Debug("resource not served, skipping",
			"group", gvr.Group, "version", gvr.Version, "resource", gvr.Resource)
	}
	return served
}

// discover lists the resources served for one group/version. It returns an
// empty (non-nil) map when the group/version is not served at all, and nil when
// discovery failed for any other reason.
func (c *Clients) discover(groupVersion string) map[string]bool {
	list, err := c.Typed.Discovery().ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return map[string]bool{} // group/version not installed on this cluster
		}
		return nil
	}
	res := make(map[string]bool, len(list.APIResources))
	for _, r := range list.APIResources {
		res[r.Name] = true
	}
	return res
}

// Extractor pulls one class of version information from a cluster. Key is used
// for error attribution and logging.
type Extractor interface {
	Key() string
	Extract(ctx context.Context, c *Clients) ([]model.Component, error)
}

// Default returns the built-in extractor set (see DESIGN.md v1 scope).
func Default() []Extractor {
	return []Extractor{
		OpenShift{},
		ConsoleBanner{},
		DefaultStorageClass{},
		StorageVolumes{},
		Certificates{},
		Virtualization{},
		VirtualMachines{},
		VMSnapshots{},
		VMTemplates{},
		Nodes{},
		MachineConfigPools{},
		OLM{},
		Portworx(),
		DellCSM(),
		Grafana{},
	}
}

// AlwaysOn returns the extractors a config file cannot replace: everything in
// Default() except the CR-field ones.
//
// A config file redefines the CR-field extractors (that is its whole purpose),
// but it has no way to express the hand-written ones, so listing them by hand
// meant every extractor added since went missing the moment anyone passed
// --config. Deriving the set keeps that from happening again.
func AlwaysOn() []Extractor {
	var out []Extractor
	for _, e := range Default() {
		if _, isCRField := e.(crFieldExtractor); isCRField {
			continue
		}
		out = append(out, e)
	}
	return out
}
