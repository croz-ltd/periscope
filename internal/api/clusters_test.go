package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"

	"github.com/croz-ltd/periscope/internal/cluster"
	"github.com/croz-ltd/periscope/internal/provision"
	"github.com/croz-ltd/periscope/internal/scrape"
)

const hubNS = "periscope"

// hubClient is a fake hub. allow decides how the SelfSubjectAccessReview answers,
// which is how the endpoint knows whether it can write cluster Secrets.
func hubClient(allow bool, objs ...runtime.Object) *fake.Clientset {
	cs := fake.NewClientset(objs...)
	cs.PrependReactor("create", "selfsubjectaccessreviews",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, &authv1.SelfSubjectAccessReview{
				Status: authv1.SubjectAccessReviewStatus{Allowed: allow},
			}, nil
		})
	return cs
}

// targetCluster is the cluster being joined: it has cluster-reader, and its token
// controller fills the Secret in.
func targetCluster() provision.ClientFactory {
	cs := fake.NewClientset(&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: provision.ClusterRole}})
	cs.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		secret, ok := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret)
		if !ok || secret.Type != corev1.SecretTypeServiceAccountToken {
			return false, nil, nil
		}
		filled := secret.DeepCopy()
		filled.Data = map[string][]byte{corev1.ServiceAccountTokenKey: []byte("minted-reader-token")}
		if err := cs.Tracker().Add(filled); err != nil {
			return true, nil, err
		}
		return true, filled, nil
	})
	return func(*rest.Config) (kubernetes.Interface, error) { return cs, nil }
}

func joinServer(t *testing.T, hub *fake.Clientset) *Server {
	t.Helper()
	reg := cluster.NewRegistryWithClient(hubNS, "periscope.io/cluster", "true", hub)
	return &Server{
		Scheduler: &scrape.Scheduler{Registry: reg},
		Provision: targetCluster(),
	}
}

func postJoin(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/clusters", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

const goodBody = `{"name":"prod-emea","apiURL":"https://api.prod.example.com:6443",` +
	`"token":"admin-token","insecureTLS":true}`

// The whole point: paste a URL and a token, and the cluster is joined.
func TestJoinClusterProvisionsAndStoresTheSecret(t *testing.T) {
	hub := hubClient(true)
	rec := postJoin(t, joinServer(t, hub), goodBody)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var res joinResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("response does not parse: %v", err)
	}
	if !res.Created || res.Name != "prod-emea" {
		t.Errorf("response is %+v, want prod-emea created", res)
	}
	if len(res.Actions) < 5 {
		t.Errorf("actions are %v, want the provisioning steps and the hub Secret", res.Actions)
	}

	secret, err := hub.CoreV1().Secrets(hubNS).Get(t.Context(), "prod-emea", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("no Secret on the hub: %v", err)
	}
	// The stored token is the one the target cluster minted, never the one pasted.
	if got := string(secret.Data["token"]); got != "minted-reader-token" {
		t.Errorf("stored token is %q, want the minted read-only one", got)
	}
	if got := string(secret.Data["apiURL"]); got != "https://api.prod.example.com:6443" {
		t.Errorf("stored apiURL is %q", got)
	}
	if secret.Labels["periscope.io/cluster"] != "true" {
		t.Errorf("Secret labels are %v, want the join label", secret.Labels)
	}
}

// The token an operator pastes must not survive the request, in the response or
// anywhere near a log line.
func TestJoinClusterNeverEchoesThePastedToken(t *testing.T) {
	hub := hubClient(true)
	rec := postJoin(t, joinServer(t, hub), goodBody)
	if strings.Contains(rec.Body.String(), "admin-token") {
		t.Error("the response repeats the pasted token")
	}
	secret, err := hub.CoreV1().Secrets(hubNS).Get(t.Context(), "prod-emea", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range secret.Data {
		if strings.Contains(string(value), "admin-token") {
			t.Errorf("the stored Secret carries the pasted token in %q", key)
		}
	}
}

// Re-importing replaces the credentials, which is how a rotated token is fixed.
func TestJoinClusterReplacesExistingCredentials(t *testing.T) {
	hub := hubClient(true, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prod-emea", Namespace: hubNS,
			Labels: map[string]string{"periscope.io/cluster": "true", "keep-me": "yes"},
		},
		Data: map[string][]byte{"apiURL": []byte("https://old"), "token": []byte("stale")},
	})

	rec := postJoin(t, joinServer(t, hub), goodBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var res joinResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Created {
		t.Error("an existing cluster was reported as created")
	}

	secret, err := hub.CoreV1().Secrets(hubNS).Get(t.Context(), "prod-emea", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(secret.Data["token"]); got != "minted-reader-token" {
		t.Errorf("token is %q, want the freshly minted one", got)
	}
	// Someone else's label on that Secret is not ours to drop.
	if secret.Labels["keep-me"] != "yes" {
		t.Errorf("labels are %v, want the unrelated one kept", secret.Labels)
	}
}

// A hub narrowed to read-only can still serve the manifests, and the refusal has
// to say which of the two applies.
func TestJoinClusterRefusedWhenTheHubMayNotWrite(t *testing.T) {
	rec := postJoin(t, joinServer(t, hubClient(false)), goodBody)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/yaml/new-cluster") {
		t.Errorf("the refusal is %q, want it to point at the manifests", rec.Body.String())
	}
}

func TestJoinClusterValidatesTheRequest(t *testing.T) {
	srv := joinServer(t, hubClient(true))
	cases := map[string]string{
		"bad name":      `{"name":"Prod EMEA","apiURL":"https://x","token":"t","insecureTLS":true}`,
		"no name":       `{"apiURL":"https://x","token":"t","insecureTLS":true}`,
		"no token":      `{"name":"prod","apiURL":"https://x","insecureTLS":true}`,
		"no url":        `{"name":"prod","token":"t","insecureTLS":true}`,
		"plain http":    `{"name":"prod","apiURL":"http://x","token":"t","insecureTLS":true}`,
		"no tls choice": `{"name":"prod","apiURL":"https://x","token":"t"}`,
		"broken json":   `{"name":`,
	}
	for what, body := range cases {
		if rec := postJoin(t, srv, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s returned %d, want 400: %s", what, rec.Code, rec.Body.String())
		}
	}
}

func TestJoinClusterRejectsOtherMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	joinServer(t, hubClient(true)).Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/clusters", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET returned %d, want 405", rec.Code)
	}
}
