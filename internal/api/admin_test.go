package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/croz-ltd/periscope/internal/cluster"
	"github.com/croz-ltd/periscope/internal/model"
	"github.com/croz-ltd/periscope/internal/scrape"
	"github.com/croz-ltd/periscope/internal/store"
)

// adminToken is the token an elevated reader arrives with. The fake hub below
// answers the access review by whether this exact token was used, which is what
// tells "the caller may administer" apart from "this pod may".
const adminToken = "elevated-user-token"

// adminServer holds two clusters in the store, one of which still has a join
// Secret on the hub. Only the other one is purgeable.
func adminServer(t *testing.T) *Server {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, name := range []string{"decommissioned", "live"} {
		snap := model.Snapshot{Cluster: name, Time: time.Unix(1000, 0), OK: true,
			Components: []model.Component{{Key: "openshift", Name: "OpenShift",
				Group: model.GroupOpenShift, Compare: model.CompareVersion, Version: "4.16.1"}}}
		if err := st.SaveSnapshot(snap); err != nil {
			t.Fatal(err)
		}
	}

	joined := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: "live", Namespace: hubNS, Labels: map[string]string{"periscope.io/cluster": "true"}}}
	hub := hubClient(false, joined)

	reg := cluster.NewRegistryWithClient(hubNS, "periscope.io/cluster", "true", hub)
	// The hub's own client says no to everything (hubClient(false)); a client
	// built for the admin token says yes. That is the real split: the pod cannot
	// create services, an administrator can.
	reg.AsUser = func(token string) (kubernetes.Interface, error) {
		return hubClient(token == adminToken, joined), nil
	}
	return &Server{Store: st, Scheduler: &scrape.Scheduler{Registry: reg, Store: st}}
}

func adminRequest(t *testing.T, srv *Server, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("X-Forwarded-Access-Token", token)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// A reader who can see the dashboard but not administer the namespace gets a
// 403, which is the signal the UI reads to leave purging out entirely.
func TestAdminAPIRefusesAnOrdinaryReader(t *testing.T) {
	srv := adminServer(t)

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/admin/clusters", ""},
		{http.MethodPost, "/api/admin/purge", `{"clusters":["decommissioned"]}`},
	} {
		rec := adminRequest(t, srv, tc.method, tc.path, tc.body, "some-readers-token")
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}

	// And nothing was removed on the way to being refused.
	stored, err := srv.Store.StoredClusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Errorf("the store holds %d clusters, want both untouched", len(stored))
	}
}

// A request with no token at all is the one that arrives when the proxy is not
// in front of the server. In the cluster the hub's own rights answer it, and
// they do not include creating a Service.
func TestAdminAPIRefusesARequestWithNoToken(t *testing.T) {
	rec := adminRequest(t, adminServer(t), http.MethodGet, "/api/admin/clusters", "", "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestAdminClustersMarksWhatIsStillJoined(t *testing.T) {
	rec := adminRequest(t, adminServer(t), http.MethodGet, "/api/admin/clusters", "", adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}

	var body struct {
		Clusters []struct {
			Name      string `json:"name"`
			Joined    bool   `json:"joined"`
			Snapshots int    `json:"snapshots"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Clusters) != 2 {
		t.Fatalf("got %d clusters, want both", len(body.Clusters))
	}
	got := map[string]bool{}
	for _, c := range body.Clusters {
		got[c.Name] = c.Joined
		if c.Snapshots != 1 {
			t.Errorf("%s reports %d snapshots, want 1", c.Name, c.Snapshots)
		}
	}
	if got["decommissioned"] {
		t.Error("decommissioned has no Secret, so it must not read as joined")
	}
	if !got["live"] {
		t.Error("live still has its Secret, so it must read as joined")
	}
}

func TestPurgeRemovesTheClusterThatIsNoLongerJoined(t *testing.T) {
	srv := adminServer(t)

	rec := adminRequest(t, srv, http.MethodPost, "/api/admin/purge",
		`{"clusters":["decommissioned"]}`, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}

	var body purgeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Purged) != 1 || body.Purged[0].Cluster != "decommissioned" || body.Purged[0].Snapshots != 1 {
		t.Fatalf("purged = %+v, want the one cluster and its snapshot", body.Purged)
	}
	if len(body.Refused) != 0 {
		t.Errorf("refused = %+v, want nothing refused", body.Refused)
	}

	snaps, err := srv.Store.LatestSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || snaps[0].Cluster != "live" {
		t.Fatalf("the matrix still holds %+v", snaps)
	}
}

// The guard that makes the endpoint safe to expose: configuration decides which
// clusters exist, and a purge only clears up after it.
func TestPurgeRefusesAClusterThatIsStillJoined(t *testing.T) {
	srv := adminServer(t)

	rec := adminRequest(t, srv, http.MethodPost, "/api/admin/purge",
		`{"clusters":["live","never-heard-of-it"]}`, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}

	var body purgeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Purged) != 0 {
		t.Errorf("purged = %+v, want nothing purged", body.Purged)
	}
	if len(body.Refused) != 2 {
		t.Fatalf("refused = %+v, want both names refused", body.Refused)
	}
	if !strings.Contains(body.Refused[0].Reason, "still joined") {
		t.Errorf("live was refused as %q, want it to say the cluster is still joined", body.Refused[0].Reason)
	}

	stored, err := srv.Store.StoredClusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Errorf("the store holds %d clusters, want both untouched", len(stored))
	}
}

func TestPurgeNeedsAClusterName(t *testing.T) {
	rec := adminRequest(t, adminServer(t), http.MethodPost, "/api/admin/purge", `{"clusters":[]}`, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// A hub that cannot reach a cluster registry cannot tell an administrator from
// a reader, so it must not serve the admin API at all.
func TestAdminAPIWithoutARegistryIsUnavailable(t *testing.T) {
	srv := &Server{Store: nil, Scheduler: nil}
	rec := adminRequest(t, srv, http.MethodGet, "/api/admin/clusters", "", adminToken)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestCallerTokenPrefersTheProxyHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/clusters", nil)
	req.Header.Set("Authorization", "Bearer from-the-client")
	if got := callerToken(req); got != "from-the-client" {
		t.Errorf("callerToken = %q, want the bearer token", got)
	}
	req.Header.Set("X-Forwarded-Access-Token", "from-the-proxy")
	if got := callerToken(req); got != "from-the-proxy" {
		t.Errorf("callerToken = %q, want the proxy header to win", got)
	}
	// Basic auth is what oauth-proxy puts there by default, and it is not a
	// token. Reading it as one would send rubbish to the access review.
	plain := httptest.NewRequest(http.MethodGet, "/api/admin/clusters", nil)
	plain.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	if got := callerToken(plain); got != "" {
		t.Errorf("callerToken = %q, want empty for a non-bearer header", got)
	}
}
