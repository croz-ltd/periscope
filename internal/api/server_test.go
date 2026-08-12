package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/croz-ltd/periscope/internal/drift"
	"github.com/croz-ltd/periscope/internal/model"
	"github.com/croz-ltd/periscope/internal/store"
	"github.com/croz-ltd/periscope/pkg/version"
)

func TestServerEndpoints(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	now := time.Now()
	if err := st.SaveSnapshot(model.Snapshot{Cluster: "a", Time: now, OK: true, Components: []model.Component{
		{Key: "openshift", Name: "OpenShift", Kind: "openshift", Version: "4.14.9"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSnapshot(model.Snapshot{Cluster: "b", Time: now, OK: true, Components: []model.Component{
		{Key: "openshift", Name: "OpenShift", Kind: "openshift", Version: "4.12.0"},
	}}); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer((&Server{Store: st, StaleAfter: time.Hour}).Handler())
	defer ts.Close()

	// /api/matrix returns a well-formed matrix with the expected drift.
	var m drift.Matrix
	getJSON(t, ts.URL+"/api/matrix", &m)
	if len(m.Clusters) != 2 || len(m.Rows) != 1 {
		t.Fatalf("matrix shape: %d clusters, %d rows", len(m.Clusters), len(m.Rows))
	}
	row := m.Rows[0]
	if row.Leader != "4.14.9" {
		t.Errorf("leader = %q, want 4.14.9", row.Leader)
	}
	if row.Cells["a"].State != drift.StateLeader {
		t.Errorf("a is %s, want leader", row.Cells["a"].State)
	}
	if row.Cells["b"].State != drift.StateBehind {
		t.Errorf("b is %s, want behind", row.Cells["b"].State)
	}

	// Embedded UI serves at /.
	if body, ct := get(t, ts.URL+"/"); !strings.Contains(body, "Periscope") {
		t.Errorf("index.html not served (content-type %q): %.80q", ct, body)
	}

	// Metrics and CSV export respond.
	if body, _ := get(t, ts.URL+"/metrics"); !strings.Contains(body, "periscope_component_drift_severity") {
		t.Errorf("metrics missing drift gauge: %.120q", body)
	}
	if body, _ := get(t, ts.URL+"/api/export.csv"); !strings.Contains(body, "component,kind,leader") {
		t.Errorf("csv header missing: %.80q", body)
	}

	// /api/changes serves the feed. Both clusters just joined.
	var feed struct {
		Changes []model.Change `json:"changes"`
	}
	getJSON(t, ts.URL+"/api/changes", &feed)
	if len(feed.Changes) != 2 {
		t.Errorf("want a join per cluster, got %+v", feed.Changes)
	}

	// /api/changes/calendar marks the days something happened.
	var cal struct {
		Days []store.ChangeDay `json:"days"`
	}
	getJSON(t, ts.URL+"/api/changes/calendar", &cal)
	if len(cal.Days) != 1 || cal.Days[0].Count != 2 {
		t.Errorf("calendar = %+v, want one day with both joins", cal.Days)
	}

	// /api/version reports the version stamped into the binary, plus the namespace
	// and label the Docs page quotes back in the commands that join a cluster.
	var v map[string]string
	getJSON(t, ts.URL+"/api/version", &v)
	if v["version"] != version.Raw {
		t.Errorf("version = %q, want %q", v["version"], version.Raw)
	}
	if v["namespace"] == "" || v["clusterLabel"] == "" {
		t.Errorf("version payload is %+v, want a namespace and a cluster label", v)
	}
}

func get(t *testing.T, url string) (string, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	// Read the whole body: a single Read returns whatever happens to have
	// arrived, which is not the whole response. That passed for as long as the
	// bodies fit in one chunk and failed the moment index.html grew past it.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: read body: %v", url, err)
	}
	return string(body), resp.Header.Get("Content-Type")
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

// Time travel rebuilds the matrix from what each cluster last reported at that
// moment, so a cluster not yet scraped is not there.
func TestMatrixTimeTravel(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	base := time.Now().Add(-72 * time.Hour)
	openshift := func(v string) []model.Component {
		return []model.Component{{Key: "openshift", Name: "OpenShift", Kind: "openshift", Version: v}}
	}
	for _, s := range []model.Snapshot{
		{Cluster: "a", Time: base, OK: true, Components: openshift("4.14.9")},
		{Cluster: "a", Time: base.Add(48 * time.Hour), OK: true, Components: openshift("4.15.0")},
		{Cluster: "b", Time: base.Add(60 * time.Hour), OK: true, Components: openshift("4.15.0")},
	} {
		if err := st.SaveSnapshot(s); err != nil {
			t.Fatal(err)
		}
	}

	ts := httptest.NewServer((&Server{Store: st, StaleAfter: time.Hour}).Handler())
	defer ts.Close()

	var past drift.Matrix
	at := base.Add(time.Minute).UTC().Format(time.RFC3339)
	getJSON(t, ts.URL+"/api/matrix?at="+at, &past)

	if len(past.Clusters) != 1 || past.Clusters[0].Name != "a" {
		t.Fatalf("want only cluster a back then, got %+v", past.Clusters)
	}
	if past.Rows[0].Cells["a"].Version != "4.14.9" {
		t.Errorf("cell = %q, want the version a was on then", past.Rows[0].Cells["a"].Version)
	}
	if past.At == nil {
		t.Error("a historical matrix must say so, or the UI cannot warn that it is not live")
	}
	// Staleness is judged against the moment being viewed: a snapshot that was
	// fresh then must not be branded stale by today's clock.
	if past.Clusters[0].Stale {
		t.Error("a snapshot taken at the viewed moment is not stale")
	}

	var now drift.Matrix
	getJSON(t, ts.URL+"/api/matrix", &now)
	if len(now.Clusters) != 2 {
		t.Errorf("live matrix has %d clusters, want both", len(now.Clusters))
	}
	if now.At != nil {
		t.Error("the live matrix must not claim to be history")
	}
}
