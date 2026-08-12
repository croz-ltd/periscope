// Package cluster discovers the clusters to scrape. Every cluster, including
// the hub's own, is represented by a labeled credential Secret in the hub
// namespace. The Secret's name is the cluster's display name. The hub's own
// in-cluster credentials are used only to read those Secrets, never as an
// implicit scrape target.
package cluster

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/croz-ltd/periscope/internal/logging"
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

// NewRegistryWithClient builds a registry around a client the caller supplies. It
// exists so the API layer and its tests can run against a fake hub.
func NewRegistryWithClient(namespace, labelKey, labelVal string, hub kubernetes.Interface) *Registry {
	return &Registry{Namespace: namespace, LabelKey: labelKey, LabelVal: labelVal, hub: hub}
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

// SaveCluster writes the labeled credential Secret that joins a cluster, and
// reports whether it was created or updated. An update is the honest answer to
// re-importing a cluster whose token was rotated.
//
// This is the only write the hub itself performs. It needs create and update on
// secrets in its own namespace, which the chart grants and CanJoinClusters
// checks.
func (r *Registry) SaveCluster(ctx context.Context, name, apiURL, token string, caBundle []byte, order int) (created bool, err error) {
	data := map[string][]byte{"apiURL": []byte(apiURL), "token": []byte(token)}
	if len(caBundle) > 0 {
		data["caBundle"] = caBundle
	}
	labels := map[string]string{r.LabelKey: r.LabelVal}
	if order != DefaultOrder {
		labels[OrderLabel] = strconv.Itoa(order)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: r.Namespace, Labels: labels},
		Data:       data,
	}

	log := logging.For("cluster")
	_, err = r.hub.CoreV1().Secrets(r.Namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err == nil {
		log.Info("cluster joined", "cluster", name, "namespace", r.Namespace, "host", apiURL)
		return true, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return false, err
	}
	// Replacing the whole Secret drops labels somebody else set on it, so the
	// credentials are updated in place and the join label is made sure of.
	existing, err := r.hub.CoreV1().Secrets(r.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if existing.Data == nil {
		existing.Data = map[string][]byte{}
	}
	for k, v := range data {
		existing.Data[k] = v
	}
	if len(caBundle) == 0 {
		delete(existing.Data, "caBundle")
	}
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels[r.LabelKey] = r.LabelVal
	if order != DefaultOrder {
		existing.Labels[OrderLabel] = strconv.Itoa(order)
	}
	if _, err := r.hub.CoreV1().Secrets(r.Namespace).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return false, err
	}
	log.Info("cluster credentials replaced", "cluster", name, "namespace", r.Namespace, "host", apiURL)
	return false, nil
}

// CanJoinClusters reports whether this hub can write its own cluster Secrets. The
// UI asks before offering to do it, because a hub whose Role was narrowed to
// read-only can still serve the manifests for an operator to apply by hand.
func (r *Registry) CanJoinClusters(ctx context.Context) bool {
	review := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: r.Namespace, Verb: "create", Resource: "secrets", Version: "v1",
			},
		},
	}
	res, err := r.hub.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		logging.For("cluster").Debug("cannot review my own access, assuming no", "error", err)
		return false
	}
	return res.Status.Allowed
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
	log := logging.For("cluster")
	sel := fmt.Sprintf("%s=%s", r.LabelKey, r.LabelVal)
	log.Debug("listing cluster Secrets", "namespace", r.Namespace, "selector", sel)

	secrets, err := r.hub.CoreV1().Secrets(r.Namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		return nil, err
	}

	var targets []Target
	var skipped, insecure []string
	for _, s := range secrets.Items {
		apiURL := string(s.Data["apiURL"])
		token := string(s.Data["token"])
		if apiURL == "" || token == "" {
			// A Secret that carries the label but not the credentials is a join
			// half-done. It used to be skipped in silence, which looks exactly
			// like the cluster never having been joined at all.
			log.Warn("ignoring labeled Secret with incomplete credentials",
				"secret", s.Name, "namespace", s.Namespace,
				"hasApiURL", apiURL != "", "hasToken", token != "")
			skipped = append(skipped, s.Name)
			continue
		}
		cfg := &rest.Config{Host: apiURL, BearerToken: token}
		if ca := s.Data["caBundle"]; len(ca) > 0 {
			cfg.TLSClientConfig = rest.TLSClientConfig{CAData: ca}
		} else {
			cfg.TLSClientConfig = rest.TLSClientConfig{Insecure: true}
			insecure = append(insecure, s.Name)
		}
		order := DefaultOrder
		if raw, set := s.Labels[OrderLabel]; set {
			if v, err := strconv.Atoi(raw); err == nil {
				order = v
			} else {
				log.Warn("ignoring unparseable order label", "secret", s.Name,
					"label", OrderLabel, "value", raw)
			}
		}
		log.Debug("cluster discovered", "cluster", s.Name, "host", apiURL, "order", order,
			"tls", tlsMode(cfg))
		targets = append(targets, Target{Name: s.Name, Config: cfg, Order: order})
	}

	if len(insecure) > 0 {
		// Worth saying out loud once per cycle: these connections are not verified.
		log.Warn("scraping without a CA bundle, TLS verification disabled",
			"clusters", strings.Join(insecure, ","))
	}
	log.Info("cluster discovery finished", "found", len(targets), "skipped", len(skipped))
	return targets, nil
}

func tlsMode(cfg *rest.Config) string {
	if cfg.TLSClientConfig.Insecure {
		return "insecure"
	}
	return "verified"
}
