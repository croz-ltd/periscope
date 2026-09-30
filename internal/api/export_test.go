package api

import (
	"encoding/csv"
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

func TestExportFollowsThePage(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	now := time.Now()
	for cluster, v := range map[string]string{"a": "4.14.9", "b": "4.12.0"} {
		if err := st.SaveSnapshot(model.Snapshot{Cluster: cluster, Time: now, OK: true, Components: []model.Component{
			{Key: "openshift", Name: "OpenShift", Kind: "openshift", Group: model.GroupOpenShift, Version: v},
			{Key: "nodes", Name: "Nodes", Kind: "count", Group: model.GroupNode, Compare: model.CompareInfo, Version: "3"},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	ts := httptest.NewServer((&Server{Store: st, StaleAfter: time.Hour}).Handler())
	defer ts.Close()

	readCSV := func(url string) [][]string {
		t.Helper()
		res, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %s", url, res.Status)
		}
		recs, err := csv.NewReader(res.Body).ReadAll()
		if err != nil {
			t.Fatalf("GET %s: not CSV: %v", url, err)
		}
		return recs
	}

	all := readCSV(ts.URL + "/api/export.csv")
	if got := strings.Join(all[0], ","); got != "component,kind,leader,a,b" {
		t.Errorf("header = %s", got)
	}
	if len(all) != 3 {
		t.Errorf("the whole fleet has 2 rows, got %d", len(all)-1)
	}

	stats := readCSV(ts.URL + "/api/export.csv?page=statistics&cluster=b")
	if len(stats) != 2 || stats[1][0] != "Nodes" {
		t.Fatalf("statistics export = %v, want the nodes row only", stats)
	}
	if got := strings.Join(stats[0][3:], ","); got != "b" {
		t.Errorf("columns = %s, want b only", got)
	}
	if stats[1][3] != "3 (info)" {
		t.Errorf("count cell = %q", stats[1][3])
	}

	compare := readCSV(ts.URL + "/api/export.csv?page=compare")
	if len(compare) != 2 || compare[1][0] != "OpenShift" || compare[1][4] != "4.12.0 (behind)" {
		t.Errorf("compare export = %v", compare)
	}

	var m drift.Matrix
	getJSON(t, ts.URL+"/api/export.json?page=statistics", &m)
	if len(m.Pages) != 1 || m.Pages[0].ID != "statistics" || len(m.Rows) != 1 {
		t.Errorf("statistics JSON has %d pages and %d rows", len(m.Pages), len(m.Rows))
	}

	for _, bad := range []string{"?page=nope", "?cluster=nope"} {
		res, err := http.Get(ts.URL + "/api/export.csv" + bad)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s answered %s, want 400", bad, res.Status)
		}
	}
}
