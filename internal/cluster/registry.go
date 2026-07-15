// Package cluster discovers the clusters to scrape. Every cluster — including
// the hub's own — is represented by a labeled credential Secret in the hub
// namespace; the Secret's name is the cluster's display name. The hub's own
// in-cluster credentials are used only to read those Secrets, never as an
// implicit scrape target.
package cluster

import (
	"context"
	"fmt"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// OrderLabel, when set on a cluster Secret to an integer, controls column order
// in the matrix (lower = further left). Unlabeled clusters sort after labeled
// ones, then alphabetically.
const OrderLabel = "periscope.io/order"

// DefaultOrder is used for clusters whose Secret has no (valid) order label.
const DefaultOrder = 1_000_000

// Target is a cluster to scrape.
type Target struct {
	Name   string
	Config *rest.Config
	Order  int
}

// Registry finds targets from labeled Secrets in the hub namespace.
type Registry struct {
	Namespace string
	LabelKey  string
	LabelVal  string

	hub kubernetes.Interface
}

// NewRegistry builds a registry. The hub client uses in-cluster credentials
// (falling back to the default kubeconfig for local development) purely to list
// the labeled cluster Secrets.
func NewRegistry(namespace, labelKey, labelVal string) (*Registry, error) {
	cfg, err := hubConfig()
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Registry{Namespace: namespace, LabelKey: labelKey, LabelVal: labelVal, hub: cs}, nil
}

// ConfigMapData returns a ConfigMap's data from the hub namespace. Returns
// (nil, nil) when the ConfigMap does not exist.
func (r *Registry) ConfigMapData(ctx context.Context, name string) (map[string]string, error) {
	cm, err := r.hub.CoreV1().ConfigMaps(r.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return cm.Data, nil
}

func hubConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
}

// Discover returns one target per labeled credential Secret. Secret data keys:
// apiURL, token, and optional caBundle (PEM). A Secret without caBundle falls
// back to an insecure TLS connection. The cluster name is the Secret name.
func (r *Registry) Discover(ctx context.Context) ([]Target, error) {
	sel := fmt.Sprintf("%s=%s", r.LabelKey, r.LabelVal)
	secrets, err := r.hub.CoreV1().Secrets(r.Namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		return nil, err
	}

	var targets []Target
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
		order := DefaultOrder
		if v, err := strconv.Atoi(s.Labels[OrderLabel]); err == nil {
			order = v
		}
		targets = append(targets, Target{Name: s.Name, Config: cfg, Order: order})
	}
	return targets, nil
}
