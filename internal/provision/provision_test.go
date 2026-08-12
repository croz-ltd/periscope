package provision

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

// clusterReader is the built-in role every step depends on.
func clusterReader() *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: ClusterRole}}
}

// fakeCluster returns a factory serving a cluster that already holds objs. The
// token controller is simulated: a created token Secret is filled in at once,
// which is what a healthy cluster does.
func fakeCluster(t *testing.T, objs ...runtime.Object) (ClientFactory, *fake.Clientset) {
	t.Helper()
	cs := fake.NewClientset(objs...)
	// The token controller is simulated by filling the Secret in as it is created,
	// and putting that version in the tracker so the following Get sees it. A
	// reactor that returns the object without storing it leaves the Get empty.
	cs.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		secret, ok := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret)
		if !ok || secret.Type != corev1.SecretTypeServiceAccountToken {
			return false, nil, nil
		}
		filled := secret.DeepCopy()
		filled.Data = map[string][]byte{corev1.ServiceAccountTokenKey: []byte("reader-token-abc")}
		if err := cs.Tracker().Add(filled); err != nil {
			return true, nil, err
		}
		return true, filled, nil
	})
	return func(*rest.Config) (kubernetes.Interface, error) { return cs, nil }, cs
}

func creds() Credentials {
	return Credentials{APIURL: "https://api.example.com:6443", Token: "admin-token", InsecureTLS: true}
}

func TestReaderCreatesEverythingAndReturnsTheToken(t *testing.T) {
	factory, cs := fakeCluster(t, clusterReader())

	res, err := Reader(context.Background(), creds(), factory)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if res.Token != "reader-token-abc" {
		t.Errorf("token is %q, want the one the cluster minted", res.Token)
	}
	if len(res.Actions) != 5 {
		t.Errorf("actions are %v, want four objects and the token read", res.Actions)
	}

	if _, err := cs.CoreV1().Namespaces().Get(context.Background(), Namespace, metav1.GetOptions{}); err != nil {
		t.Errorf("namespace missing: %v", err)
	}
	if _, err := cs.CoreV1().ServiceAccounts(Namespace).Get(context.Background(), ServiceAccount, metav1.GetOptions{}); err != nil {
		t.Errorf("service account missing: %v", err)
	}
	crb, err := cs.RbacV1().ClusterRoleBindings().Get(context.Background(), ServiceAccount, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("cluster role binding missing: %v", err)
	}
	if crb.RoleRef.Name != ClusterRole {
		t.Errorf("bound to %q, want %q", crb.RoleRef.Name, ClusterRole)
	}
	if len(crb.Subjects) != 1 || crb.Subjects[0].Namespace != Namespace {
		t.Errorf("binding subjects are %+v, want the reader in %s", crb.Subjects, Namespace)
	}
	// An unverified connection is a fact worth repeating back.
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "TLS") {
		t.Errorf("warnings are %v, want one about TLS", res.Warnings)
	}
}

// Re-importing a cluster has to do the one thing that is missing rather than
// failing on the first object that exists.
func TestReaderIsIdempotent(t *testing.T) {
	factory, _ := fakeCluster(t,
		clusterReader(),
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: Namespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: ServiceAccount, Namespace: Namespace}},
	)

	res, err := Reader(context.Background(), creds(), factory)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	var already int
	for _, a := range res.Actions {
		if strings.Contains(a, "was already there") {
			already++
		}
	}
	if already != 2 {
		t.Errorf("actions are %v, want two reported as already present", res.Actions)
	}
	if res.Token == "" {
		t.Error("no token returned")
	}
}

// The pasted token decides what can be created. A refusal has to name the object,
// because that is what tells an operator which right is missing.
func TestReaderReportsWhatTheTokenCannotDo(t *testing.T) {
	factory, cs := fakeCluster(t, clusterReader())
	cs.PrependReactor("create", "clusterrolebindings", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Group: "rbac.authorization.k8s.io", Resource: "clusterrolebindings"},
			ServiceAccount, nil)
	})

	_, err := Reader(context.Background(), creds(), factory)
	if err == nil {
		t.Fatal("a forbidden create was reported as success")
	}
	if !strings.Contains(err.Error(), "cluster role binding") {
		t.Errorf("error is %q, want it to name the object", err)
	}
}

// Binding to a role that does not exist succeeds and grants nothing, which shows
// up later as a fleet of forbidden errors on every scrape.
func TestReaderRefusesWithoutClusterReader(t *testing.T) {
	factory, _ := fakeCluster(t) // no ClusterRole in the cluster

	_, err := Reader(context.Background(), creds(), factory)
	if err == nil {
		t.Fatal("a cluster without cluster-reader was accepted")
	}
	if !strings.Contains(err.Error(), ClusterRole) {
		t.Errorf("error is %q, want it to name the missing role", err)
	}
}

// A cluster that never fills the token in has something else wrong, and the
// request must say so rather than hang.
func TestReaderGivesUpWaitingForTheToken(t *testing.T) {
	original := tokenWait
	tokenWait = 200 * time.Millisecond
	defer func() { tokenWait = original }()

	cs := fake.NewClientset(clusterReader()) // no reactor, so the Secret stays empty
	factory := func(*rest.Config) (kubernetes.Interface, error) { return cs, nil }

	_, err := Reader(context.Background(), creds(), factory)
	if err == nil {
		t.Fatal("an empty token Secret was accepted")
	}
	if !strings.Contains(err.Error(), "did not fill it in") {
		t.Errorf("error is %q, want it to name the token controller", err)
	}
}

// Pasting a privileged token down an unverified connection must be a decision,
// not a default.
func TestCredentialsRequireACABundleOrAnExplicitChoice(t *testing.T) {
	c := Credentials{APIURL: "https://api.example.com:6443", Token: "t"}
	if _, err := c.Config(); err == nil {
		t.Error("a connection with neither a CA bundle nor a decision was accepted")
	}

	c.CABundle = []byte("-----BEGIN CERTIFICATE-----")
	cfg, err := c.Config()
	if err != nil {
		t.Fatalf("a CA bundle was rejected: %v", err)
	}
	if cfg.TLSClientConfig.Insecure {
		t.Error("a CA bundle still produced an insecure config")
	}

	if _, err := (Credentials{APIURL: "https://x", InsecureTLS: true}).Config(); err == nil {
		t.Error("a missing token was accepted")
	}
}
