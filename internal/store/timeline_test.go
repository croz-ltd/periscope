package store

import (
	"testing"
	"time"

	"github.com/croz-ltd/periscope/internal/model"
)

// counts builds a snapshot carrying one info component, the shape a statistics
// row has.
func counts(cluster string, at time.Time, nodes string) model.Snapshot {
	return model.Snapshot{
		Cluster: cluster,
		Time:    at,
		OK:      true,
		Components: []model.Component{{
			Key: "node-count", Name: "Total nodes", Group: model.GroupNode,
			Compare: model.CompareInfo, Kind: "nodes", Version: nodes,
			Extra: map[string]string{"workers": nodes},
		}},
	}
}

func valuesOf(t *testing.T, rows []TimelineRow, key, cluster string) []string {
	t.Helper()
	for _, row := range rows {
		if row.Key != key {
			continue
		}
		for _, s := range row.Series {
			if s.Cluster != cluster {
				continue
			}
			out := make([]string, 0, len(s.Points))
			for _, p := range s.Points {
				out = append(out, p.Version)
			}
			return out
		}
	}
	return nil
}

// A value is recorded on every scrape, and a scrape that found no change records
// the same number again. The series has to hold one value per step whatever the
// scrape interval was.
func TestTimelineCarriesValuesForward(t *testing.T) {
	st := openTest(t)
	now := time.Now().UTC().Truncate(time.Hour)

	// Six hours of scrapes, twice an hour, with the count moving twice.
	for i := 12; i >= 0; i-- {
		at := now.Add(-time.Duration(i) * 30 * time.Minute)
		nodes := "6"
		if i <= 8 {
			nodes = "9"
		}
		if i <= 2 {
			nodes = "12"
		}
		if err := st.SaveSnapshot(counts("prod", at, nodes)); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := st.Timeline([]string{"node-count"}, now.Add(-6*time.Hour), now, time.Hour)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	got := valuesOf(t, rows, "node-count", "prod")
	want := []string{"6", "9", "9", "9", "12", "12"}
	if len(got) != len(want) {
		t.Fatalf("got %d points %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("point %d is %q, want %q (series %v)", i, got[i], want[i], got)
		}
	}
	if rows[0].Name != "Total nodes" {
		t.Errorf("row name is %q, want the component name", rows[0].Name)
	}
}

// A component nobody touched inside the window still has a value, taken from the
// last scrape before the window opened. Without that seed a quiet fortnight draws
// an empty chart.
func TestTimelineSeedsFromBeforeTheWindow(t *testing.T) {
	st := openTest(t)
	now := time.Now().UTC().Truncate(time.Hour)

	if err := st.SaveSnapshot(counts("prod", now.Add(-72*time.Hour), "21")); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSnapshot(counts("prod", now.Add(-30*time.Minute), "21")); err != nil {
		t.Fatal(err)
	}

	rows, err := st.Timeline([]string{"node-count"}, now.Add(-6*time.Hour), now, 2*time.Hour)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	got := valuesOf(t, rows, "node-count", "prod")
	if len(got) != 3 {
		t.Fatalf("got %v, want three points", got)
	}
	for i, v := range got {
		if v != "21" {
			t.Errorf("point %d is %q, want 21 carried from before the window", i, v)
		}
	}
}

// A cluster joined during the window has no history before it. A line that starts
// at zero reports a shrinking fleet that never happened.
func TestTimelineStartsWhenTheClusterJoined(t *testing.T) {
	st := openTest(t)
	now := time.Now().UTC().Truncate(time.Hour)

	if err := st.SaveSnapshot(counts("old", now.Add(-10*time.Hour), "6")); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSnapshot(counts("new", now.Add(-2*time.Hour), "3")); err != nil {
		t.Fatal(err)
	}

	rows, err := st.Timeline([]string{"node-count"}, now.Add(-6*time.Hour), now, time.Hour)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if old := valuesOf(t, rows, "node-count", "old"); len(old) != 6 {
		t.Errorf("the cluster present all along has %d points, want 6: %v", len(old), old)
	}
	// Three boundaries fall at or after the moment it joined, and none before it.
	fresh := valuesOf(t, rows, "node-count", "new")
	if len(fresh) != 3 {
		t.Fatalf("the cluster joined two hours ago has %d points, want 3: %v", len(fresh), fresh)
	}
	for _, v := range fresh {
		if v != "3" {
			t.Errorf("new cluster point is %q, want 3", v)
		}
	}
}

// A failed scrape records nothing about components. Reading it as a value would
// draw a drop to zero for every cluster that was briefly unreachable.
func TestTimelineIgnoresFailedScrapes(t *testing.T) {
	st := openTest(t)
	now := time.Now().UTC().Truncate(time.Hour)

	if err := st.SaveSnapshot(counts("prod", now.Add(-4*time.Hour), "9")); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSnapshot(model.Snapshot{
		Cluster: "prod", Time: now.Add(-2 * time.Hour), OK: false, Error: "timeout",
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := st.Timeline([]string{"node-count"}, now.Add(-6*time.Hour), now, 2*time.Hour)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	got := valuesOf(t, rows, "node-count", "prod")
	for _, v := range got {
		if v != "9" {
			t.Errorf("point is %q, want 9 held across the failed scrape (series %v)", v, got)
		}
	}
}

// The extra map travels with the value, because volume rows carry their PVC and
// PV counts there and the chart reads them from it.
func TestTimelineKeepsExtra(t *testing.T) {
	st := openTest(t)
	now := time.Now().UTC().Truncate(time.Hour)
	if err := st.SaveSnapshot(counts("prod", now.Add(-time.Hour), "9")); err != nil {
		t.Fatal(err)
	}

	rows, err := st.Timeline([]string{"node-count"}, now.Add(-2*time.Hour), now, time.Hour)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	for _, row := range rows {
		for _, s := range row.Series {
			for _, p := range s.Points {
				if p.Extra["workers"] != "9" {
					t.Errorf("point %+v lost its extra fields", p)
				}
			}
		}
	}
}

func TestTimelineRejectsABadWindow(t *testing.T) {
	st := openTest(t)
	now := time.Now().UTC()
	if _, err := st.Timeline([]string{"node-count"}, now, now, time.Hour); err == nil {
		t.Error("an empty window was accepted")
	}
	if _, err := st.Timeline([]string{"node-count"}, now.Add(-time.Hour), now, 0); err == nil {
		t.Error("a zero step was accepted")
	}
}
