package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/croz-ltd/periscope/internal/drift"
	"github.com/croz-ltd/periscope/internal/model"
	"github.com/croz-ltd/periscope/internal/store"
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
		t.Errorf("a should be leader, got %s", row.Cells["a"].State)
	}
	if row.Cells["b"].State != drift.StateBehind {
		t.Errorf("b should be behind, got %s", row.Cells["b"].State)
	}

	// Embedded UI serves at /.
	if body, ct := get(t, ts.URL+"/"); !strings.Contains(body, "Periscope") {
		t.Errorf("index.html not served (content-type %q): %.80q", ct, body)
	}

	// Metrics and CSV export respond.
	if body, _ := get(t, ts.URL+"/metrics"); !strings.Contains(body, "cluster_comparator_component_drift_severity") {
		t.Errorf("metrics missing drift gauge: %.120q", body)
	}
	if body, _ := get(t, ts.URL+"/api/export.csv"); !strings.Contains(body, "component,kind,leader") {
		t.Errorf("csv header missing: %.80q", body)
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
	buf := make([]byte, 1<<16)
	n, _ := resp.Body.Read(buf)
	return string(buf[:n]), resp.Header.Get("Content-Type")
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
