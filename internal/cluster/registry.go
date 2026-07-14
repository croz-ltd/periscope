// Package cluster discovers the clusters to scrape: the local (in-cluster) one
// plus every labeled credential Secret in the hub namespace.
package cluster

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Target is a cluster to scrape.
type Target struct {
	Name    string
	Config  *rest.Config
	IsLocal bool
}

// Registry finds targets from labeled Secrets in the hub namespace.
type Registry struct {
	Namespace string
	LabelKey  string
	LabelVal  string
	LocalName string

	local *rest.Config
	hub   kubernetes.Interface
}

// NewRegistry builds a registry using in-cluster credentials (falling back to
// the default kubeconfig for local development).
func NewRegistry(namespace, labelKey, labelVal, localName string) (*Registry, error) {
	cfg, err := localConfig()
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	if localName == "" {
		localName = "local"
	}
	return &Registry{
		Namespace: namespace, LabelKey: labelKey, LabelVal: labelVal, LocalName: localName,
		local: cfg, hub: cs,
	}, nil
}

func localConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
}

// Discover returns the local target plus one per labeled credential Secret.
// Secret data keys: apiURL, token, and optional caBundle (PEM). A Secret with
// caBundle absent falls back to an insecure TLS connection.
func (r *Registry) Discover(ctx context.Context) ([]Target, error) {
	targets := []Target{{Name: r.LocalName, Config: r.local, IsLocal: true}}

	sel := fmt.Sprintf("%s=%s", r.LabelKey, r.LabelVal)
	secrets, err := r.hub.CoreV1().Secrets(r.Namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		return targets, err
	}
	for _, s := range secrets.Items {
		apiURL := string(s.Data["apiURL"])
		token := string(s.Data["token"])
		if apiURL == "" || token == "" {
			continue
		}
		cfg := &rest.Config{Host: apiURL, BearerToken: token}
		if ca := s.Data["caBundle"]; len(ca) > 0 {
			cfg.TLSClientConfig = rest.TLSClientConfig{CAData: ca}
		} else {
			cfg.TLSClientConfig = rest.TLSClientConfig{Insecure: true}
		}
		targets = append(targets, Target{Name: s.Name, Config: cfg})
	}
	return targets, nil
}
