package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/croz-ltd/periscope/internal/model"
)

// twoClusterStore holds two clusters with two snapshots each, so a purge has
// something to leave behind as well as something to remove.
func twoClusterStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.waitRebuild()

	for _, c := range []string{"gone", "live"} {
		for i, v := range []string{"4.16.1", "4.16.4"} {
			snap := model.Snapshot{
				Cluster: c,
				Time:    time.Unix(int64(1000+i*600), 0),
				OK:      true,
				Components: []model.Component{
					{Key: "openshift", Name: "OpenShift", Group: model.GroupOpenShift,
						Compare: model.CompareVersion, Version: v},
				},
			}
			if err := st.SaveSnapshot(snap); err != nil {
				t.Fatal(err)
			}
		}
	}
	return st
}

func TestStoredClustersReportsWhatIsHeld(t *testing.T) {
	st := twoClusterStore(t)

	got, err := st.StoredClusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "gone" || got[1].Name != "live" {
		t.Fatalf("want the two clusters in name order, got %+v", got)
	}
	if got[0].Snapshots != 2 {
		t.Errorf("snapshots = %d, want 2", got[0].Snapshots)
	}
	if !got[0].First.Equal(time.Unix(1000, 0)) || !got[0].Last.Equal(time.Unix(1600, 0)) {
		t.Errorf("span = %v..%v, want the first and last snapshot", got[0].First, got[0].Last)
	}
	// The second scrape moved the version, so the feed holds that update. A
	// purge that left it behind would keep the cluster in the Changes page.
	if got[0].Changes == 0 {
		t.Error("changes = 0, want the version update recorded for this cluster")
	}
}

// The point of the whole feature: after a purge the cluster is gone from every
// read path, and the cluster next to it is untouched.
func TestPurgeClusterRemovesOnlyThatCluster(t *testing.T) {
	st := twoClusterStore(t)

	purged, err := st.PurgeCluster("gone")
	if err != nil {
		t.Fatal(err)
	}
	if purged.Snapshots != 2 || purged.Components != 2 || purged.Changes == 0 {
		t.Errorf("purge counted %+v, want 2 snapshots, 2 components and at least one change", purged)
	}

	stored, err := st.StoredClusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Name != "live" {
		t.Fatalf("after purging gone, the store holds %+v", stored)
	}

	snaps, err := st.LatestSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || snaps[0].Cluster != "live" || len(snaps[0].Components) != 1 {
		t.Fatalf("the matrix still sees %+v", snaps)
	}

	changes, err := st.Changes(ChangeQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.Cluster == "gone" {
			t.Fatalf("the change feed still holds %q", c.Cluster)
		}
	}

	// No orphaned component rows: they are deleted explicitly, because the
	// foreign key that would cascade is not enforced on this connection.
	var orphans int
	if err := st.db.QueryRow(
		`SELECT COUNT(*) FROM components WHERE snapshot_id NOT IN (SELECT id FROM snapshots)`,
	).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Errorf("%d component rows point at snapshots that are gone", orphans)
	}
}

func TestPurgeUnknownClusterRemovesNothing(t *testing.T) {
	st := twoClusterStore(t)

	purged, err := st.PurgeCluster("never-joined")
	if err != nil {
		t.Fatal(err)
	}
	if purged.Snapshots != 0 || purged.Components != 0 || purged.Changes != 0 {
		t.Errorf("purging an unknown cluster counted %+v, want nothing", purged)
	}
	stored, err := st.StoredClusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Errorf("the store holds %d clusters, want both still there", len(stored))
	}
}
