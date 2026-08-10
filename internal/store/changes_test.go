package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/croz-ltd/periscope/internal/model"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func comp(key, ver string) model.Component {
	return model.Component{Key: key, Name: key, Version: ver, Group: "OpenShift", Compare: model.CompareVersion}
}

func save(t *testing.T, st *Store, snap model.Snapshot) {
	t.Helper()
	if err := st.SaveSnapshot(snap); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
}

// The feed must only ever hold real events: a scrape that found the same thing
// again is what happens almost every time, and recording it would bury the
// changes that matter.
func TestChangesRecordsOnlyRealChanges(t *testing.T) {
	st := openTest(t)
	t0 := time.Now().Add(-3 * time.Hour)

	save(t, st, model.Snapshot{Cluster: "a", Time: t0, OK: true, Components: []model.Component{
		comp("openshift", "4.14.9"), comp("cert-manager", "1.13.0"),
	}})
	// Identical scrape: nothing new to say.
	save(t, st, model.Snapshot{Cluster: "a", Time: t0.Add(time.Hour), OK: true, Components: []model.Component{
		comp("openshift", "4.14.9"), comp("cert-manager", "1.13.0"),
	}})
	// An upgrade, an install and an uninstall, all at once.
	save(t, st, model.Snapshot{Cluster: "a", Time: t0.Add(2 * time.Hour), OK: true, Components: []model.Component{
		comp("openshift", "4.14.12"), comp("loki", "5.9.0"),
	}})

	changes, err := st.Changes(ChangeQuery{})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}

	got := map[string]model.Change{}
	for _, c := range changes {
		got[c.Kind+":"+c.Key] = c
	}
	if len(changes) != 4 { // joined + updated + added + removed
		t.Fatalf("want 4 changes, got %d: %+v", len(changes), changes)
	}
	if _, ok := got[model.ChangeJoined+":"]; !ok {
		t.Error("first scrape should record a join, not one addition per component")
	}
	if c := got[model.ChangeUpdated+":openshift"]; c.From != "4.14.9" || c.To != "4.14.12" {
		t.Errorf("openshift update recorded as %q -> %q", c.From, c.To)
	}
	if _, ok := got[model.ChangeAdded+":loki"]; !ok {
		t.Error("loki install not recorded")
	}
	if _, ok := got[model.ChangeRemoved+":cert-manager"]; !ok {
		t.Error("cert-manager removal not recorded")
	}

	// Newest first, so the feed reads as a feed.
	for i := 1; i < len(changes); i++ {
		if changes[i-1].Time.Before(changes[i].Time) {
			t.Fatalf("changes not newest-first: %v before %v", changes[i-1].Time, changes[i].Time)
		}
	}
}

// An unreachable cluster reports no components. Diffing those away would file
// every component on the cluster as removed, then as added on recovery.
func TestChangesUnreachableClusterDoesNotEmptyTheFleet(t *testing.T) {
	st := openTest(t)
	t0 := time.Now().Add(-3 * time.Hour)

	save(t, st, model.Snapshot{Cluster: "a", Time: t0, OK: true, Components: []model.Component{comp("openshift", "4.14.9")}})
	save(t, st, model.Snapshot{Cluster: "a", Time: t0.Add(time.Hour), OK: false, Error: "dial tcp: timeout"})
	save(t, st, model.Snapshot{Cluster: "a", Time: t0.Add(2 * time.Hour), OK: false, Error: "dial tcp: timeout"})
	save(t, st, model.Snapshot{Cluster: "a", Time: t0.Add(3 * time.Hour), OK: true, Components: []model.Component{comp("openshift", "4.14.9")}})

	changes, err := st.Changes(ChangeQuery{})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	kinds := map[string]int{}
	for _, c := range changes {
		kinds[c.Kind]++
	}
	if kinds[model.ChangeRemoved] != 0 || kinds[model.ChangeAdded] != 0 {
		t.Errorf("outage should not add or remove components, got %+v", kinds)
	}
	if kinds[model.ChangeUnreachable] != 1 {
		t.Errorf("want one unreachable event, got %d (a cluster down for hours must not repeat)", kinds[model.ChangeUnreachable])
	}
	if kinds[model.ChangeRecovered] != 1 {
		t.Errorf("want one recovered event, got %d", kinds[model.ChangeRecovered])
	}
}

// A partial scrape failure cannot tell "uninstalled" from "could not read", so
// removals wait for a clean scrape. Additions and updates are still safe.
func TestChangesPartialFailureHoldsBackRemovals(t *testing.T) {
	st := openTest(t)
	t0 := time.Now().Add(-2 * time.Hour)

	save(t, st, model.Snapshot{Cluster: "a", Time: t0, OK: true, Components: []model.Component{
		comp("openshift", "4.14.9"), comp("loki", "5.9.0"),
	}})
	save(t, st, model.Snapshot{Cluster: "a", Time: t0.Add(time.Hour), OK: true, Error: "olm: forbidden",
		Components: []model.Component{comp("openshift", "4.14.12")}})

	changes, err := st.Changes(ChangeQuery{})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	for _, c := range changes {
		if c.Kind == model.ChangeRemoved {
			t.Errorf("removal recorded from a failed scrape: %+v", c)
		}
	}
	var updated bool
	for _, c := range changes {
		if c.Kind == model.ChangeUpdated && c.Key == "openshift" {
			updated = true
		}
	}
	if !updated {
		t.Error("a version change seen during a partial failure is still a real change")
	}
}

func TestChangeDaysAndFiltering(t *testing.T) {
	st := openTest(t)
	day1 := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 2)

	save(t, st, model.Snapshot{Cluster: "a", Time: day1, OK: true, Components: []model.Component{comp("openshift", "4.14.9")}})
	save(t, st, model.Snapshot{Cluster: "b", Time: day1, OK: true, Components: []model.Component{comp("openshift", "4.14.9")}})
	save(t, st, model.Snapshot{Cluster: "a", Time: day2, OK: true, Components: []model.Component{comp("openshift", "4.15.0")}})

	days, err := st.ChangeDays(day1.AddDate(0, 0, -7), day2.AddDate(0, 0, 7), time.UTC)
	if err != nil {
		t.Fatalf("change days: %v", err)
	}
	if len(days) != 2 {
		t.Fatalf("want 2 marked days, got %+v", days)
	}
	if days[0].Date != "2026-03-10" || days[0].Count != 2 || days[0].Clusters != 2 {
		t.Errorf("day 1 = %+v, want 2 changes across 2 clusters", days[0])
	}
	if days[1].Date != "2026-03-12" || days[1].Clusters != 1 {
		t.Errorf("day 2 = %+v", days[1])
	}

	only, err := st.Changes(ChangeQuery{Cluster: "b"})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	for _, c := range only {
		if c.Cluster != "b" {
			t.Errorf("cluster filter leaked %q", c.Cluster)
		}
	}
	if len(only) != 1 {
		t.Errorf("want b's single join, got %d", len(only))
	}
}

// Time travel: the matrix is rebuilt from whatever each cluster last reported at
// that moment, so a cluster scraped later must not leak backwards.
func TestSnapshotsAt(t *testing.T) {
	st := openTest(t)
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

	save(t, st, model.Snapshot{Cluster: "a", Time: base, OK: true, Components: []model.Component{comp("openshift", "4.14.9")}})
	save(t, st, model.Snapshot{Cluster: "a", Time: base.Add(48 * time.Hour), OK: true, Components: []model.Component{comp("openshift", "4.15.0")}})
	save(t, st, model.Snapshot{Cluster: "b", Time: base.Add(72 * time.Hour), OK: true, Components: []model.Component{comp("openshift", "4.15.0")}})

	at, err := st.SnapshotsAt(base.Add(24 * time.Hour))
	if err != nil {
		t.Fatalf("snapshots at: %v", err)
	}
	if len(at) != 1 {
		t.Fatalf("want only cluster a, which was the only one scraped by then, got %d", len(at))
	}
	if at[0].Cluster != "a" || at[0].Components[0].Version != "4.14.9" {
		t.Errorf("got %s %s, want a at 4.14.9", at[0].Cluster, at[0].Components[0].Version)
	}

	now, err := st.SnapshotsAt(base.Add(96 * time.Hour))
	if err != nil {
		t.Fatalf("snapshots at: %v", err)
	}
	if len(now) != 2 {
		t.Errorf("want both clusters later on, got %d", len(now))
	}
}

// History written before the feed existed still has to show up, or the feed
// would claim a fleet that has been running for months never changed.
func TestBackfillReconstructsFeedFromHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t0 := time.Now().Add(-2 * time.Hour)
	save(t, st, model.Snapshot{Cluster: "a", Time: t0, OK: true, Components: []model.Component{comp("openshift", "4.14.9")}})
	save(t, st, model.Snapshot{Cluster: "a", Time: t0.Add(time.Hour), OK: true, Components: []model.Component{comp("openshift", "4.15.0")}})

	// Drop the recorded feed and the marker: this is a database from before.
	if _, err := st.db.Exec(`DELETE FROM changes; DELETE FROM meta`); err != nil {
		t.Fatal(err)
	}
	st.Close()

	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()

	changes, err := st2.Changes(ChangeQuery{})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(changes) != 2 { // joined + updated
		t.Fatalf("want the history rebuilt, got %+v", changes)
	}

	// Reopening again must not double it.
	st2.Close()
	st3, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st3.Close()
	again, err := st3.Changes(ChangeQuery{})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(again) != len(changes) {
		t.Errorf("backfill ran twice: %d changes became %d", len(changes), len(again))
	}
}

// A cluster upgraded while it was unreachable really did change. Comparing
// against the last good snapshot is the only way to notice, and reporting only
// "recovered" would hide a whole release upgrade.
func TestChangesAcrossAnOutageComparesToLastGoodState(t *testing.T) {
	st := openTest(t)
	t0 := time.Now().Add(-4 * time.Hour)

	save(t, st, model.Snapshot{Cluster: "a", Time: t0, OK: true, Components: []model.Component{comp("openshift", "4.14.9")}})
	save(t, st, model.Snapshot{Cluster: "a", Time: t0.Add(time.Hour), OK: false, Error: "timeout"})
	save(t, st, model.Snapshot{Cluster: "a", Time: t0.Add(2 * time.Hour), OK: false, Error: "timeout"})
	// Comes back on a new release: it was upgraded during the outage.
	save(t, st, model.Snapshot{Cluster: "a", Time: t0.Add(3 * time.Hour), OK: true, Components: []model.Component{comp("openshift", "4.15.0")}})

	changes, err := st.Changes(ChangeQuery{})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	var recovered, upgraded bool
	for _, c := range changes {
		if c.Kind == model.ChangeRecovered {
			recovered = true
		}
		if c.Kind == model.ChangeUpdated && c.Key == "openshift" {
			upgraded = true
			if c.From != "4.14.9" || c.To != "4.15.0" {
				t.Errorf("upgrade recorded as %q -> %q, want it measured from the last good state", c.From, c.To)
			}
		}
	}
	if !recovered {
		t.Error("coming back must be reported")
	}
	if !upgraded {
		t.Error("an upgrade that happened during the outage was lost")
	}
}
