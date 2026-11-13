package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/croz-ltd/periscope/internal/auth"
	"github.com/croz-ltd/periscope/internal/cluster"
	"github.com/croz-ltd/periscope/internal/model"
	"github.com/croz-ltd/periscope/internal/scrape"
	"github.com/croz-ltd/periscope/internal/store"
)

// adminToken is the token an elevated reader arrives with. Any other token in
// readerTokens belongs to a reader who can see the dashboard and nothing more.
const adminToken = "elevated-user-token"

var readerTokens = []string{"some-readers-token", "a-readers-token"}

// fakeReviewer stands in for the API server: it knows which user a token is,
// and which rights each user holds.
type fakeReviewer struct {
	users    map[string]string // token -> user
	rights   map[string]bool   // "user verb resource"
	allowErr error             // returned by every Allowed call when set
}

func newFakeReviewer() *fakeReviewer {
	f := &fakeReviewer{
		users: map[string]string{adminToken: "admin"},
		rights: map[string]bool{
			"admin get services": true, "admin create services": true,
			"reader get services": true,
		},
	}
	for _, t := range readerTokens {
		f.users[t] = "reader"
	}
	return f
}

func (f *fakeReviewer) Authenticate(_ context.Context, token string) (*auth.Identity, error) {
	user, ok := f.users[token]
	if !ok {
		return nil, auth.ErrInvalidToken
	}
	return &auth.Identity{User: user, Token: token}, nil
}

func (f *fakeReviewer) Allowed(_ context.Context, id *auth.Identity, verb, resource string) (bool, error) {
	if f.allowErr != nil && verb != auth.ReadVerb {
		return false, f.allowErr
	}
	return f.rights[id.User+" "+verb+" "+resource], nil
}

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
	reg := cluster.NewRegistryWithClient(hubNS, "periscope.io/cluster", "true", hubClient(false, joined))
	return &Server{Store: st, Scheduler: &scrape.Scheduler{Registry: reg, Store: st}, Access: newFakeReviewer()}
}

// guarded serves srv the way the hub does in a cluster: behind the auth guard.
func guarded(t *testing.T, srv *Server) http.Handler {
	t.Helper()
	sealer, err := auth.NewSealer([]byte("a test session secret of some length"))
	if err != nil {
		t.Fatal(err)
	}
	if srv.Access == nil {
		srv.Access = newFakeReviewer()
	}
	g := &auth.Guard{Reviewer: srv.Access, Sealer: sealer, Namespace: hubNS}
	return srv.Wrap(g.Wrap)
}

func adminRequest(t *testing.T, srv *Server, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	guarded(t, srv).ServeHTTP(rec, req)
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

// The ways a request is refused need different fixes, so each must say which
// it is.
func TestRefusalSaysWhichFailureItWas(t *testing.T) {
	t.Run("no token at all", func(t *testing.T) {
		rec := adminRequest(t, adminServer(t), http.MethodGet, "/api/admin/clusters", "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Error("a 401 must say how to authenticate")
		}
	})

	t.Run("a token the API server does not accept", func(t *testing.T) {
		rec := adminRequest(t, adminServer(t), http.MethodGet, "/api/admin/clusters", "", "rubbish")
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "expired, revoked") {
			t.Fatalf("status = %d (%q), want 401 that names the token", rec.Code, rec.Body.String())
		}
	})

	t.Run("the review said no", func(t *testing.T) {
		rec := adminRequest(t, adminServer(t), http.MethodGet, "/api/admin/clusters", "", "a-readers-token")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "create on services in namespace "+hubNS) || !strings.Contains(body, "reader") {
			t.Errorf("body = %q, want it to name the right, the namespace and the user", body)
		}
	})

	t.Run("the review failed", func(t *testing.T) {
		srv := adminServer(t)
		srv.Access.(*fakeReviewer).allowErr = errors.New("the API server is unreachable")
		rec := adminRequest(t, srv, http.MethodGet, "/api/admin/clusters", "", adminToken)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "the API server is unreachable") {
			t.Errorf("body = %q, want it to carry the reason the review failed", rec.Body.String())
		}
	})
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
	srv := &Server{Store: nil, Scheduler: nil, Access: newFakeReviewer()}
	rec := adminRequest(t, srv, http.MethodGet, "/api/admin/clusters", "", adminToken)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
