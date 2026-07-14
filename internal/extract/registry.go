// Package extract pulls version information from a cluster. A generic OLM path
// covers all installed operators; hand-written extractors handle special cases
// (OpenShift release, node fleet, and CSI driver versions buried in vendor CRs).
package extract

import (
	"context"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/croz-ltd/cluster-comparator/internal/model"
)

// Clients bundles the per-cluster clients an extractor may use.
type Clients struct {
	Typed   kubernetes.Interface
	Dynamic dynamic.Interface
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
		OLM{},
		Nodes{},
		Portworx(),
		DellCSM(),
	}
}
