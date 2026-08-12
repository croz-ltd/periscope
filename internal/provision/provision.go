// Package provision creates the read-only credentials Periscope needs on a
// cluster, from a token an operator pastes once.
//
// It is the only place Periscope writes anything, and it writes on the joined
// cluster with the operator's own token, never with the hub's. What an operator
// can do by applying the manifests by hand is exactly what this does for them, so
// the app gains no privilege of its own. The pasted token is used for these calls
// and then dropped: what the hub stores is the read-only token the cluster mints.
package provision

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/croz-ltd/periscope/internal/logging"
)

// Names of the objects created on the joined cluster. They match
// charts/periscope-join and the document served at /yaml/new-cluster, so a
// cluster onboarded any of the three ways looks the same afterwards.
const (
	Namespace      = "periscope"
	ServiceAccount = "periscope-reader"
	TokenSecret    = "periscope-reader-token"
	ClusterRole    = "cluster-reader"
)

// tokenWait bounds how long the ServiceAccount token controller is given to fill
// in the Secret. It is normally immediate. A cluster that never fills it in has
// something else wrong, and saying so beats a request that hangs. It is a
// variable so a test can wait a moment instead of half a minute.
var tokenWait = 30 * time.Second

// Credentials is what an operator pastes: where the cluster is, and a token with
// enough rights to create a ServiceAccount and a ClusterRoleBinding.
type Credentials struct {
	APIURL      string
	Token       string
	CABundle    []byte
	InsecureTLS bool
}

// Result reports what happened, so the UI can list it rather than claiming
// success in one word.
type Result struct {
	Actions  []string `json:"actions"`
	Warnings []string `json:"warnings,omitempty"`
	// Token is the read-only ServiceAccount token to store on the hub. It is never
	// the token the operator pasted.
	Token string `json:"-"`
}

// ClientFactory builds a client for the target cluster. Tests replace it.
type ClientFactory func(*rest.Config) (kubernetes.Interface, error)

// DefaultClientFactory is the real one.
func DefaultClientFactory(cfg *rest.Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(cfg)
}

// ErrNoCABundle is returned when a caller offers neither a CA bundle nor an
// explicit decision to skip verification. Pasting a privileged token down an
// unverified connection must be a choice somebody made on purpose.
var ErrNoCABundle = errors.New("provide a CA bundle, or accept an unverified connection explicitly")

// Config turns credentials into a client config for the target cluster.
func (c Credentials) Config() (*rest.Config, error) {
	if c.APIURL == "" || c.Token == "" {
		return nil, errors.New("both the API URL and a token are required")
	}
	cfg := &rest.Config{Host: c.APIURL, BearerToken: c.Token, Timeout: 20 * time.Second}
	switch {
	case len(c.CABundle) > 0:
		cfg.TLSClientConfig = rest.TLSClientConfig{CAData: c.CABundle}
	case c.InsecureTLS:
		cfg.TLSClientConfig = rest.TLSClientConfig{Insecure: true}
	default:
		return nil, ErrNoCABundle
	}
	return cfg, nil
}

// Reader creates, or completes, the read-only credentials on the target cluster
// and returns the token the hub must store.
//
// Every step is idempotent: an object that exists already is left alone and
// reported as such, so re-importing a cluster whose token was rotated does the
// one thing that is missing rather than failing on the first object.
func Reader(ctx context.Context, creds Credentials, newClient ClientFactory) (Result, error) {
	var res Result

	cfg, err := creds.Config()
	if err != nil {
		return res, err
	}
	if creds.InsecureTLS && len(creds.CABundle) == 0 {
		res.Warnings = append(res.Warnings,
			"TLS verification is off for this cluster, both for this import and for every scrape")
	}
	cs, err := newClient(cfg)
	if err != nil {
		return res, fmt.Errorf("cannot build a client for %s: %w", creds.APIURL, err)
	}
	log := logging.For("provision")

	// Reach the cluster before writing to it, so an unreachable host or a bad
	// certificate is reported as that rather than as a failed create.
	if _, err := cs.Discovery().ServerVersion(); err != nil {
		return res, fmt.Errorf("cannot reach %s: %w", creds.APIURL, err)
	}
	// cluster-reader is an OpenShift built-in. Binding to a role that does not
	// exist succeeds and grants nothing, which shows up later as a fleet of
	// forbidden errors.
	if _, err := cs.RbacV1().ClusterRoles().Get(ctx, ClusterRole, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return res, fmt.Errorf("this cluster has no %q ClusterRole, which OpenShift provides", ClusterRole)
		}
		return res, fmt.Errorf("cannot read the %q ClusterRole: %w", ClusterRole, err)
	}

	steps := []struct {
		what   string
		create func() error
	}{
		{fmt.Sprintf("namespace %s", Namespace), func() error {
			_, err := cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: Namespace, Labels: labels()},
			}, metav1.CreateOptions{})
			return err
		}},
		{fmt.Sprintf("service account %s/%s", Namespace, ServiceAccount), func() error {
			_, err := cs.CoreV1().ServiceAccounts(Namespace).Create(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: ServiceAccount, Namespace: Namespace, Labels: labels()},
			}, metav1.CreateOptions{})
			return err
		}},
		{fmt.Sprintf("cluster role binding %s to %s", ServiceAccount, ClusterRole), func() error {
			_, err := cs.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: ServiceAccount, Labels: labels()},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: ClusterRole,
				},
				Subjects: []rbacv1.Subject{{
					Kind: "ServiceAccount", Name: ServiceAccount, Namespace: Namespace,
				}},
			}, metav1.CreateOptions{})
			return err
		}},
		{fmt.Sprintf("token secret %s/%s", Namespace, TokenSecret), func() error {
			_, err := cs.CoreV1().Secrets(Namespace).Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: TokenSecret, Namespace: Namespace, Labels: labels(),
					Annotations: map[string]string{corev1.ServiceAccountNameKey: ServiceAccount},
				},
				Type: corev1.SecretTypeServiceAccountToken,
			}, metav1.CreateOptions{})
			return err
		}},
	}

	for _, step := range steps {
		err := step.create()
		switch {
		case err == nil:
			log.Info("created", "object", step.what, "host", creds.APIURL)
			res.Actions = append(res.Actions, "created "+step.what)
		case apierrors.IsAlreadyExists(err):
			log.Debug("already present", "object", step.what, "host", creds.APIURL)
			res.Actions = append(res.Actions, step.what+" was already there")
		case apierrors.IsForbidden(err):
			// The pasted token decides what can be created, so name the object and
			// the verb rather than reporting a generic failure.
			return res, fmt.Errorf("this token cannot create the %s: %w", step.what, err)
		default:
			return res, fmt.Errorf("cannot create the %s: %w", step.what, err)
		}
	}

	token, err := waitForToken(ctx, cs)
	if err != nil {
		return res, err
	}
	res.Token = token
	res.Actions = append(res.Actions, "read the read-only token")
	return res, nil
}

// waitForToken polls the token Secret until the ServiceAccount token controller
// fills it in.
func waitForToken(ctx context.Context, cs kubernetes.Interface) (string, error) {
	deadline := time.Now().Add(tokenWait)
	for {
		secret, err := cs.CoreV1().Secrets(Namespace).Get(ctx, TokenSecret, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return "", fmt.Errorf("cannot read %s/%s: %w", Namespace, TokenSecret, err)
		}
		if err == nil {
			if token := secret.Data[corev1.ServiceAccountTokenKey]; len(token) > 0 {
				return string(token), nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf(
				"%s/%s has no token after %s: the ServiceAccount token controller did not fill it in",
				Namespace, TokenSecret, tokenWait)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func labels() map[string]string {
	return map[string]string{"app.kubernetes.io/name": "periscope-join"}
}
