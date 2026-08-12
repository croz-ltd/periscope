package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/croz-ltd/periscope/internal/model"
	"github.com/croz-ltd/periscope/internal/store"
)

func timelineServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "timeline.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := time.Now().UTC()
	for i := 48; i >= 0; i-- {
		at := now.Add(-time.Duration(i) * time.Hour)
		nodes := "6"
		if i < 24 {
			nodes = "9"
		}
		snap := model.Snapshot{
			Cluster: "prod", Time: at, OK: true,
			Components: []model.Component{
				{Key: "node-count", Name: "Total nodes", Compare: model.CompareInfo, Version: nodes},
				{Key: "openshift", Name: "OpenShift", Compare: model.CompareVersion, Version: "4.17.9"},
			},
		}
		if err := st.SaveSnapshot(snap); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	return &Server{Store: st}
}

func getTimeline(t *testing.T, srv *Server, query string) (*httptest.ResponseRecorder, timelineResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/timeline"+query, nil))
	var body timelineResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("response does not parse: %v\n%s", err, rec.Body.String())
		}
	}
	return rec, body
}

func TestTimelineServesOneSeriesPerCluster(t *testing.T) {
	srv := timelineServer(t)
	rec, body := getTimeline(t, srv, "?key=node-count&days=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if body.Days != 2 || body.Step != "2h0m0s" {
		t.Errorf("window is %d days at %s, want 2 days at 2h", body.Days, body.Step)
	}
	if len(body.Rows) != 1 || body.Rows[0].Key != "node-count" {
		t.Fatalf("rows are %+v, want one node-count row", body.Rows)
	}
	if body.Rows[0].Name != "Total nodes" {
		t.Errorf("row name is %q, want the component name", body.Rows[0].Name)
	}
	series := body.Rows[0].Series
	if len(series) != 1 || series[0].Cluster != "prod" {
		t.Fatalf("series are %+v, want one for prod", series)
	}
	// Two days at two-hour steps, and the count moves once inside the window.
	points := series[0].Points
	if len(points) != 24 {
		t.Errorf("got %d points, want 24", len(points))
	}
	if points[0].Version != "6" || points[len(points)-1].Version != "9" {
		t.Errorf("series runs %q to %q, want 6 to 9", points[0].Version, points[len(points)-1].Version)
	}
	// Points arrive oldest first, or a line chart draws backwards.
	for i := 1; i < len(points); i++ {
		if !points[i].Time.After(points[i-1].Time) {
			t.Fatalf("point %d is not after its predecessor: %s then %s", i, points[i-1].Time, points[i].Time)
		}
	}
}

// The page asks for every countable row at once, so one request has to carry
// several keys.
func TestTimelineAcceptsSeveralKeys(t *testing.T) {
	srv := timelineServer(t)
	_, body := getTimeline(t, srv, "?key=node-count,openshift&days=1")
	if len(body.Rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(body.Rows), body.Rows)
	}
	keys := map[string]bool{}
	for _, row := range body.Rows {
		keys[row.Key] = true
	}
	if !keys["node-count"] || !keys["openshift"] {
		t.Errorf("rows are %v, want both keys", keys)
	}
}

// The timeframes are a fixed set, because the step is chosen with the window.
func TestTimelineRejectsAnUnsupportedWindow(t *testing.T) {
	srv := timelineServer(t)
	for _, days := range []string{"3", "0", "-7", "90", "abc"} {
		rec, _ := getTimeline(t, srv, "?key=node-count&days="+days)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("days=%s returned %d, want 400", days, rec.Code)
		}
	}
	for _, days := range []string{"1", "2", "5", "7", "14", "30"} {
		rec, _ := getTimeline(t, srv, "?key=node-count&days="+days)
		if rec.Code != http.StatusOK {
			t.Errorf("days=%s returned %d, want 200", days, rec.Code)
		}
	}
}

func TestTimelineRequiresAKey(t *testing.T) {
	srv := timelineServer(t)
	rec, _ := getTimeline(t, srv, "?days=7")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d with no key, want 400", rec.Code)
	}
}

// A window longer than the stored history is answered, and flagged, so the UI can
// say the lines start late rather than leaving a reader to guess.
func TestTimelineFlagsAShortHistory(t *testing.T) {
	srv := timelineServer(t)
	_, body := getTimeline(t, srv, "?key=node-count&days=30")
	if !body.Stale {
		t.Error("a 30 day window over two days of history is not flagged")
	}
	_, short := getTimeline(t, srv, "?key=node-count&days=1")
	if short.Stale {
		t.Error("a one day window over two days of history is flagged")
	}
}

// Time travel bounds the end of the window, so the chart matches the matrix the
// reader is looking at.
func TestTimelineHonoursTimeTravel(t *testing.T) {
	srv := timelineServer(t)
	at := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)
	_, body := getTimeline(t, srv, "?key=node-count&days=1&at="+at.Format(time.RFC3339))
	if !body.To.Equal(at) {
		t.Errorf("window ends at %s, want %s", body.To, at)
	}
	last := body.Rows[0].Series[0].Points
	if got := last[len(last)-1].Version; got != "6" {
		t.Errorf("last point is %q, want the value as of a day ago (6)", got)
	}

	rec, _ := getTimeline(t, srv, "?key=node-count&days=1&at=yesterday")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("an unparseable at returned %d, want 400", rec.Code)
	}
}
