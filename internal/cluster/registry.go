// Package cluster discovers the clusters to scrape. Every cluster, including
// the hub's own, is represented by a labeled credential Secret in the hub
// namespace. The Secret's name is the cluster's display name. The hub's own
// in-cluster credentials are used only to read those Secrets, never as an
// implicit scrape target.
package cluster

import (
	"context"
	"errors"
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

	// AsUser builds a hub client that acts as the bearer of a token, so an
	// access review can ask what the signed-in reader may do rather than what
	// this pod may do. Nil means the real one, built from the hub's own
	// connection settings with the token swapped in; a test supplies a fake.
	AsUser func(token string) (kubernetes.Interface, error)

	hub kubernetes.Interface
	// cfg is how the hub itself connects, kept so AsUser can reuse the host and
	// the CA and change only the credential.
	cfg *rest.Config
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
	return &Registry{Namespace: namespace, LabelKey: labelKey, LabelVal: labelVal, hub: cs, cfg: cfg}, nil
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
	ok, _ := r.allowed(ctx, r.hub, "create", "secrets")
	return ok
}

// CanAdminister reports whether the bearer of token may use the admin API.
//
// The right asked for is create on services in the hub namespace. Reading the
// dashboard already needs get on services, which is the check the oauth-proxy
// sidecar makes, and every reader passes it. Creating one is a right only
// somebody who administers the namespace holds, which is the line the admin API
// wants: purging history is not undoable, and a reader should not be able to do
// it by knowing the URL.
//
// An empty token asks with the hub's own credentials instead. In the cluster
// that answers no, because the hub's Role covers secrets and configmaps and
// nothing else, so a request that arrives without the proxy in front of it is
// refused. Off-cluster it answers with the developer's own kubeconfig rights,
// which is what makes the admin API usable in local development.
// The error is returned rather than swallowed because the two ways this says no
// need different fixes, and a bare 403 cannot tell them apart: an error means
// the review never happened (an expired or rubbish token, an unreachable API),
// while a nil error with false means the review happened and the answer was no.
func (r *Registry) CanAdminister(ctx context.Context, token string) (bool, error) {
	client, err := r.clientFor(token)
	if err != nil {
		return false, err
	}
	return r.allowed(ctx, client, "create", "services")
}

// clientFor returns a hub client acting as the bearer of token, or the hub's
// own client when the token is empty.
func (r *Registry) clientFor(token string) (kubernetes.Interface, error) {
	if token == "" {
		return r.hub, nil
	}
	if r.AsUser != nil {
		return r.AsUser(token)
	}
	if r.cfg == nil {
		return nil, errors.New("this hub was built without connection settings, so it cannot act on a token")
	}
	// Copy the host and the CA, and drop every other credential the hub has, so
	// the review is answered for the caller alone.
	cfg := rest.AnonymousClientConfig(r.cfg)
	cfg.BearerToken = token
	return kubernetes.NewForConfig(cfg)
}

// allowed answers one SelfSubjectAccessReview in the hub namespace. A review
// that cannot be made is a no: an access check that fails open is not a check.
// The error says why it could not be made, so a refusal can be explained.
func (r *Registry) allowed(ctx context.Context, client kubernetes.Interface, verb, resource string) (bool, error) {
	review := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: r.Namespace, Verb: verb, Resource: resource, Version: "v1",
			},
		},
	}
	res, err := client.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		logging.For("cluster").Debug("cannot review access, assuming no",
			"verb", verb, "resource", resource, "error", err)
		return false, err
	}
	return res.Status.Allowed, nil
}

// JoinedNames returns the name of every cluster Secret carrying the join label
// in the hub namespace, whether its credentials are complete or not.
//
// Discover skips a Secret it cannot scrape from, which is right for scraping and
// wrong here: a join written half way still means somebody joined that cluster,
// and purging its history because the token is missing would delete the data of
// a cluster that is about to come back.
func (r *Registry) JoinedNames(ctx context.Context) ([]string, error) {
	sel := fmt.Sprintf("%s=%s", r.LabelKey, r.LabelVal)
	secrets, err := r.hub.CoreV1().Secrets(r.Namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(secrets.Items))
	for _, s := range secrets.Items {
		names = append(names, s.Name)
	}
	return names, nil
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
