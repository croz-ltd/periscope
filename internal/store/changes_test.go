package store

import (
	"path/filepath"
	"strconv"
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
	st.waitRebuild() // the startup rebuild is asynchronous; do not race it
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
	st.waitRebuild()
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
	st2.waitRebuild()
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
	st3.waitRebuild()
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

// The calendar has to agree with the feed. Counters move on nearly every
// scrape, so a day is reported with its counter share broken out: marking a day
// that opens empty is what makes people stop trusting the calendar.
func TestChangeDaysSeparatesCounters(t *testing.T) {
	st := openTest(t)
	day := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

	counter := func(key, v string) model.Component {
		return model.Component{Key: key, Name: key, Version: v, Group: "OpenShift Virtualization", Compare: model.CompareInfo}
	}
	save(t, st, model.Snapshot{Cluster: "a", Time: day, OK: true, Components: []model.Component{
		comp("openshift", "4.14.9"), counter("vm-total", "12"),
	}})
	// Next day only the VM count moves: real churn, but not news.
	save(t, st, model.Snapshot{Cluster: "a", Time: day.AddDate(0, 0, 1), OK: true, Components: []model.Component{
		comp("openshift", "4.14.9"), counter("vm-total", "13"),
	}})
	// The day after, a genuine upgrade alongside more counter movement.
	save(t, st, model.Snapshot{Cluster: "a", Time: day.AddDate(0, 0, 2), OK: true, Components: []model.Component{
		comp("openshift", "4.15.0"), counter("vm-total", "14"),
	}})

	days, err := st.ChangeDays(day.AddDate(0, 0, -1), day.AddDate(0, 0, 3), time.UTC)
	if err != nil {
		t.Fatalf("change days: %v", err)
	}
	got := map[string]ChangeDay{}
	for _, d := range days {
		got[d.Date] = d
	}

	// Counter-only day: marked, but every one of its changes is a counter, so
	// the calendar can render it as a dot rather than colouring it in.
	quiet := got["2026-03-11"]
	if quiet.Count != 1 || quiet.Counters != 1 {
		t.Errorf("counter-only day = %+v, want 1 change, all of it counters", quiet)
	}
	busy := got["2026-03-12"]
	if busy.Count != 2 || busy.Counters != 1 {
		t.Errorf("mixed day = %+v, want 2 changes of which 1 counter", busy)
	}
	if busy.Count-busy.Counters != 1 {
		t.Errorf("mixed day should have exactly one substantive change, got %d", busy.Count-busy.Counters)
	}
}

// The limit has to apply to what is shown. A day of counter churn used to fill
// the page with counters that the caller then filtered away, handing back an
// empty feed for a day that really did have upgrades in it.
func TestChangesLimitIsSpentOnVisibleRows(t *testing.T) {
	st := openTest(t)
	day := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

	counter := func(n int) model.Component {
		return model.Component{Key: "vm-total", Name: "Virtual machines", Group: "OpenShift Virtualization",
			Compare: model.CompareInfo, Version: strconv.Itoa(n)}
	}

	// One real upgrade early in the day, then a long tail of counter movement,
	// so the newest rows are all counters.
	save(t, st, model.Snapshot{Cluster: "a", Time: day, OK: true,
		Components: []model.Component{comp("openshift", "4.14.9"), counter(0)}})
	save(t, st, model.Snapshot{Cluster: "a", Time: day.Add(time.Hour), OK: true,
		Components: []model.Component{comp("openshift", "4.15.0"), counter(1)}})
	for i := 2; i < 30; i++ {
		save(t, st, model.Snapshot{Cluster: "a", Time: day.Add(time.Duration(i) * 30 * time.Minute), OK: true,
			Components: []model.Component{comp("openshift", "4.15.0"), counter(i)}})
	}

	q := ChangeQuery{From: day, To: day.AddDate(0, 0, 1), ExcludeCounters: true, Limit: 5}
	changes, err := st.Changes(q)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("the upgrade was pushed out of the page by counter updates")
	}
	for _, c := range changes {
		if c.Compare == model.CompareInfo {
			t.Errorf("counter update leaked into the feed: %+v", c)
		}
	}
	var upgraded bool
	for _, c := range changes {
		if c.Kind == model.ChangeUpdated && c.Key == "openshift" {
			upgraded = true
		}
	}
	if !upgraded {
		t.Error("the upgrade is the one thing this day had to show")
	}

	// And the feed can say what it left out, whatever the limit was.
	hidden, err := st.CountCounters(q)
	if err != nil {
		t.Fatalf("count counters: %v", err)
	}
	if hidden != 29 {
		t.Errorf("hidden counters = %d, want every counter update in the range (29)", hidden)
	}
}

// A scrape whose per-cluster deadline expires partway through the extractor
// list stores a snapshot that is OK but missing everything the later
// extractors would have read. That snapshot must not become the baseline: the
// components were never uninstalled, they were never read, and the next
// healthy scrape would otherwise report every one of them as newly appeared.
func TestChangesTimedOutScrapeDoesNotResurrectComponents(t *testing.T) {
	st := openTest(t)
	t0 := time.Now().Add(-4 * time.Hour)

	full := []model.Component{
		comp("openshift", "4.20.1"), // an early extractor, always succeeds
		comp("loki-operator", "6.5.1"),
		comp("portworx-csi", "26.1.2"),
		comp("kubelet", "v1.33.12"),
	}
	// Everything from Nodes onward failed: the deadline hit mid-list.
	partial := []model.Component{comp("openshift", "4.20.1")}

	save(t, st, model.Snapshot{Cluster: "erls-p", Time: t0, OK: true, Components: full})
	save(t, st, model.Snapshot{Cluster: "erls-p", Time: t0.Add(time.Hour), OK: true,
		Error: "olm: context deadline exceeded; portworx-csi: context deadline exceeded", Components: partial})
	// Next scrape is healthy again and reads the same fleet as before.
	save(t, st, model.Snapshot{Cluster: "erls-p", Time: t0.Add(2 * time.Hour), OK: true, Components: full})

	changes, err := st.Changes(ChangeQuery{})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	for _, c := range changes {
		if c.Kind == model.ChangeAdded {
			t.Errorf("component reported as newly appeared after a timed-out scrape: %+v", c)
		}
		if c.Kind == model.ChangeRemoved {
			t.Errorf("component reported as removed by a timed-out scrape: %+v", c)
		}
	}
	if len(changes) != 1 || changes[0].Kind != model.ChangeJoined {
		t.Errorf("nothing actually changed, so only the join belongs in the feed, got %+v", changes)
	}
}

// The carried-forward baseline must not hide a real uninstall that happened
// while scrapes were degraded: it is reported once the reading is trustworthy.
func TestChangesRealRemovalSurvivesADegradedPeriod(t *testing.T) {
	st := openTest(t)
	t0 := time.Now().Add(-4 * time.Hour)

	save(t, st, model.Snapshot{Cluster: "a", Time: t0, OK: true, Components: []model.Component{
		comp("openshift", "4.20.1"), comp("loki-operator", "6.5.1"),
	}})
	save(t, st, model.Snapshot{Cluster: "a", Time: t0.Add(time.Hour), OK: true,
		Error: "olm: context deadline exceeded", Components: []model.Component{comp("openshift", "4.20.1")}})
	// Loki really was uninstalled, and this scrape read OLM cleanly.
	save(t, st, model.Snapshot{Cluster: "a", Time: t0.Add(2 * time.Hour), OK: true, Components: []model.Component{
		comp("openshift", "4.20.1"),
	}})

	changes, err := st.Changes(ChangeQuery{})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	var removed bool
	for _, c := range changes {
		if c.Kind == model.ChangeRemoved && c.Key == "loki-operator" {
			removed = true
		}
	}
	if !removed {
		t.Error("a genuine uninstall during a degraded period must still be reported")
	}
}

// The feed is derived data, so a correction to how changes are detected has to
// reach what people are already looking at. Events recorded by superseded logic
// are rebuilt from the stored history on the next start, or the bad ones stay on
// the page forever and the fix appears not to have worked.
func TestRebuildReplacesEventsFromSupersededLogic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t0 := time.Now().Add(-3 * time.Hour)
	full := []model.Component{comp("openshift", "4.20.1"), comp("loki-operator", "6.5.1")}
	save(t, st, model.Snapshot{Cluster: "erls-p", Time: t0, OK: true, Components: full})
	save(t, st, model.Snapshot{Cluster: "erls-p", Time: t0.Add(time.Hour), OK: true,
		Error: "olm: context deadline exceeded", Components: []model.Component{comp("openshift", "4.20.1")}})
	save(t, st, model.Snapshot{Cluster: "erls-p", Time: t0.Add(2 * time.Hour), OK: true, Components: full})

	// Stand in for a database written by the old logic: a spurious "appeared"
	// event, and a version marker from before the fix.
	if _, err := st.db.Exec(
		`INSERT INTO changes(cluster, ts, kind, comp_key, name, comp_group, comp_compare, old_value, new_value)
		 VALUES('erls-p', ?, 'added', 'loki-operator', 'Loki', 'Operators', 'version', '', '6.5.1')`,
		t0.Add(2*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE meta SET value = '1' WHERE key = ?`, changesVersionKey); err != nil {
		t.Fatal(err)
	}
	st.Close()

	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	st2.waitRebuild()
	defer st2.Close()

	changes, err := st2.Changes(ChangeQuery{})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	for _, c := range changes {
		if c.Kind == model.ChangeAdded {
			t.Errorf("an event from the old logic survived the rebuild: %+v", c)
		}
	}
	if len(changes) != 1 || changes[0].Kind != model.ChangeJoined {
		t.Errorf("rebuilt feed = %+v, want only the join", changes)
	}

	// A second start must leave the rebuilt feed alone rather than doubling it.
	st2.Close()
	st3, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	st3.waitRebuild()
	defer st3.Close()
	again, err := st3.Changes(ChangeQuery{})
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(again) != len(changes) {
		t.Errorf("rebuild ran again on an up-to-date database: %d became %d", len(changes), len(again))
	}
}
