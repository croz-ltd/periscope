package store

import (
	"database/sql"
	"sort"
	"strconv"
	"time"

	"github.com/croz-ltd/periscope/internal/model"
)

// history is what came before a snapshot: the last one taken, whatever state it
// was in, and the state to compare against.
//
// The baseline is carried forward rather than taken from a single snapshot.
// Extractors share one deadline and run in order, so a slow cluster produces a
// snapshot that is "successful" but stops partway down the list: everything the
// later extractors would have read is simply missing. Treating that as the
// state of the cluster claims a dozen operators were uninstalled and then
// reinstalled a scrape later. What a degraded scrape did not read is filled in
// from the last scrape that did read it, up to the most recent clean one, which
// by definition read everything.
type history struct {
	last        model.Snapshot                // most recent, may be a failed scrape
	baseline    map[[2]string]model.Component // effective state before this scrape
	hasLast     bool
	hasBaseline bool
}

// observe folds a snapshot into the carried-forward state, walking forwards.
// It is the mirror of the backwards walk in historyFor, and the two must agree.
func (h *history) observe(snap model.Snapshot) {
	h.last, h.hasLast = snap, true
	if !snap.OK {
		return // read nothing, so it says nothing about what is installed
	}
	if snap.Error == "" {
		// A clean scrape read everything: it replaces the state outright, which
		// is what lets a genuine uninstall be noticed.
		h.baseline, h.hasBaseline = indexComponents(snap.Components), true
		return
	}
	if h.baseline == nil {
		h.baseline = map[[2]string]model.Component{}
	}
	for _, c := range snap.Components { // partial: overlay what it did read
		h.baseline[[2]string{c.Key, c.Namespace}] = c
	}
	h.hasBaseline = true
}

// diff returns the events worth showing for a new snapshot. A scrape that found
// nothing new returns nothing, so the feed never fills with "no change" rows.
//
// Components are compared against the carried-forward baseline rather than
// against whatever the previous snapshot happened to contain, so neither an
// outage nor a scrape that timed out partway through invents changes. A cluster
// upgraded while it was unreachable is still reported when it comes back.
//
// One silence is deliberate: when a scrape reports a partial error, removals
// are held back. An extractor that failed cannot be told apart from a component
// that was uninstalled, and inventing removals every time a token expires would
// teach people to ignore the feed. The removal is reported by the next clean
// scrape, which can tell the difference.
func diff(h history, next model.Snapshot) []model.Change {
	at := next.Time

	if !next.OK {
		if !h.hasLast || h.last.OK {
			return []model.Change{{Time: at, Cluster: next.Cluster, Kind: model.ChangeUnreachable, To: next.Error}}
		}
		return nil // still down, already reported
	}

	var out []model.Change
	if !h.hasBaseline {
		// Nothing to compare against: the cluster is arriving, and listing
		// every component it came with as an addition says nothing.
		return append(out, model.Change{Time: at, Cluster: next.Cluster, Kind: model.ChangeJoined})
	}
	if h.hasLast && !h.last.OK {
		out = append(out, model.Change{Time: at, Cluster: next.Cluster, Kind: model.ChangeRecovered})
	}

	prevByID := h.baseline
	nextByID := indexComponents(next.Components)

	for id, c := range nextByID {
		old, existed := prevByID[id]
		switch {
		case !existed:
			out = append(out, change(at, next.Cluster, model.ChangeAdded, c, "", c.Version))
		case old.Version != c.Version:
			out = append(out, change(at, next.Cluster, model.ChangeUpdated, c, old.Version, c.Version))
		}
	}
	if next.Error == "" { // see the note above on partial scrapes
		for id, c := range prevByID {
			if _, still := nextByID[id]; !still {
				out = append(out, change(at, next.Cluster, model.ChangeRemoved, c, c.Version, ""))
			}
		}
	}

	// Stable order for a stable feed: group, then name, then key.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Key < b.Key
	})
	return out
}

// indexComponents keys components by key and namespace: one operator installed
// in two namespaces is two facts, and would otherwise flap as a single row.
func indexComponents(comps []model.Component) map[[2]string]model.Component {
	out := make(map[[2]string]model.Component, len(comps))
	for _, c := range comps {
		out[[2]string{c.Key, c.Namespace}] = c
	}
	return out
}

func change(at time.Time, cluster, kind string, c model.Component, from, to string) model.Change {
	return model.Change{
		Time: at, Cluster: cluster, Kind: kind,
		Key: c.Key, Name: c.Name, Group: c.Group, Compare: c.Compare,
		From: from, To: to,
	}
}

// baselineWalk bounds how far back a carried-forward baseline is assembled. A
// healthy cluster stops at the first snapshot, so this only costs anything
// while scrapes are degraded, and a cluster degraded for longer than this is
// better served by an incomplete baseline than by an unbounded scan.
const baselineWalk = 200

// historyFor loads what a cluster reported before now: the last snapshot taken,
// and the carried-forward state to compare against.
//
// Walking backwards, each snapshot fills in only the components no newer one
// reported, and the walk stops at the first clean scrape because that one read
// everything. See history for why the state is assembled this way.
func (s *Store) historyFor(q queryer, cluster string) (history, error) {
	type snapRow struct {
		id   int64
		snap model.Snapshot
	}

	rows, err := q.Query(
		`SELECT id, ts, ok, COALESCE(error,'') FROM snapshots WHERE cluster = ?
		 ORDER BY ts DESC, id DESC LIMIT ?`, cluster, baselineWalk)
	if err != nil {
		return history{}, err
	}
	// Read the rows out before loading any components: a transaction serves one
	// query at a time, and the save path runs this inside one.
	var recent []snapRow
	for rows.Next() {
		var r snapRow
		var ts int64
		var ok int
		if err := rows.Scan(&r.id, &ts, &ok, &r.snap.Error); err != nil {
			rows.Close()
			return history{}, err
		}
		r.snap.Cluster, r.snap.Time, r.snap.OK = cluster, time.Unix(ts, 0), ok == 1
		recent = append(recent, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return history{}, err
	}
	rows.Close()

	var h history
	for i, r := range recent {
		if i == 0 {
			h.last, h.hasLast = r.snap, true
		}
		if !r.snap.OK {
			continue // read nothing, so it says nothing about what is installed
		}
		comps, err := s.componentsForQ(q, r.id)
		if err != nil {
			return history{}, err
		}
		if h.baseline == nil {
			h.baseline = make(map[[2]string]model.Component, len(comps))
		}
		for _, c := range comps {
			id := [2]string{c.Key, c.Namespace}
			if _, newer := h.baseline[id]; !newer {
				h.baseline[id] = c // a newer reading already won
			}
		}
		h.hasBaseline = true
		if r.snap.Error == "" {
			break // a clean scrape read everything; older ones cannot add to it
		}
	}
	return h, nil
}

func insertChanges(e execer, changes []model.Change) error {
	for _, c := range changes {
		if _, err := e.Exec(
			`INSERT INTO changes(cluster, ts, kind, comp_key, name, comp_group, comp_compare, old_value, new_value)
			 VALUES(?,?,?,?,?,?,?,?,?)`,
			c.Cluster, c.Time.Unix(), c.Kind, c.Key, c.Name, c.Group, c.Compare, c.From, c.To); err != nil {
			return err
		}
	}
	return nil
}

// backfillWindow bounds how far back a rebuild walks. It reads every snapshot in
// the window, so a database with years of history would spend startup
// recovering changes nobody is going to scroll back to.
const backfillWindow = 90 * 24 * time.Hour

// changesLogicVersion identifies how the feed was derived. The feed is not
// source data: it is what diff() made of the snapshot history, so a correction
// to diff() leaves already-recorded events wrong, and no amount of running the
// fixed code repairs them. Bumping this rebuilds the feed from history on the
// next start, which is the only way a fix reaches what people are looking at.
//
//	1: initial
//	2: carried-forward baseline, so a scrape that timed out partway through the
//	   extractor list no longer reports the fleet as uninstalled and reinstalled
const changesLogicVersion = 2

const changesVersionKey = "changes_version"

// rebuildChanges derives the feed from stored history whenever the recorded one
// was produced by older logic (or does not exist yet). Returns how many events
// it wrote, and whether it ran at all.
func (s *Store) rebuildChanges(now time.Time) (written int, ran bool, err error) {
	var have string
	switch err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, changesVersionKey).Scan(&have); err {
	case nil:
		if have == strconv.Itoa(changesLogicVersion) {
			return 0, false, nil // recorded by the current logic, leave it alone
		}
	case sql.ErrNoRows: // never recorded, or recorded before this key existed
	default:
		return 0, false, err
	}

	// Hold the write lock for the whole rebuild: it rewrites the same table a
	// scrape appends to, and SQLite takes one writer at a time anyway.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()

	// Re-derive exactly the range that gets replayed. Deleting the whole table
	// would drop everything older than the window, since the snapshots needed to
	// regenerate those events are no longer being read.
	from := now.Add(-backfillWindow)
	if _, err := tx.Exec(`DELETE FROM changes WHERE ts >= ?`, from.Unix()); err != nil {
		return 0, false, err
	}

	written, err = s.replayHistory(tx, from)
	if err != nil {
		return 0, false, err
	}

	if _, err := tx.Exec(
		`INSERT INTO meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		changesVersionKey, strconv.Itoa(changesLogicVersion)); err != nil {
		return 0, false, err
	}
	return written, true, tx.Commit()
}

// replayHistory walks every snapshot since "from" in order and records what
// each one changed.
//
// Snapshots and their components are read in one ordered pass rather than a
// query per snapshot: a fleet scraped every ten minutes for ninety days is
// hundreds of thousands of snapshots, and a round trip each would turn startup
// into minutes of work while the liveness probe watches.
func (s *Store) replayHistory(tx execer, from time.Time) (int, error) {
	rows, err := s.db.Query(`
SELECT s.id, s.cluster, s.ts, s.ok, COALESCE(s.error,''),
       c.comp_key, c.namespace, c.name, c.version, c.comp_group, c.comp_compare
FROM snapshots s
LEFT JOIN components c ON c.snapshot_id = s.id
WHERE s.ts >= ?
ORDER BY s.cluster, s.ts, s.id`, from.Unix())
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var (
		prior   history
		current model.Snapshot
		haveOne bool
		curID   int64
		total   int
	)

	// flush records what the snapshot just finished reading changed.
	flush := func() error {
		if !haveOne {
			return nil
		}
		changes := diff(prior, current)
		if err := insertChanges(tx, changes); err != nil {
			return err
		}
		total += len(changes)
		prior.observe(current)
		return nil
	}

	for rows.Next() {
		var (
			id                             int64
			cluster, errStr                string
			ts                             int64
			ok                             int
			key, ns, name, ver, grp, cmpre sql.NullString
		)
		if err := rows.Scan(&id, &cluster, &ts, &ok, &errStr, &key, &ns, &name, &ver, &grp, &cmpre); err != nil {
			return 0, err
		}

		if !haveOne || id != curID {
			if err := flush(); err != nil {
				return 0, err
			}
			if !haveOne || cluster != current.Cluster {
				prior = history{} // rows are grouped by cluster; start the next clean
			}
			current = model.Snapshot{Cluster: cluster, Time: time.Unix(ts, 0), OK: ok == 1, Error: errStr}
			curID, haveOne = id, true
		}
		if key.Valid { // NULL when the snapshot read nothing at all
			current.Components = append(current.Components, model.Component{
				Key:       key.String,
				Namespace: ns.String,
				Name:      name.String,
				Version:   ver.String,
				Group:     grp.String,
				Compare:   cmpre.String,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := flush(); err != nil {
		return 0, err
	}
	return total, nil
}

// ChangeQuery filters the change feed. Zero values mean "no filter".
type ChangeQuery struct {
	From            time.Time
	To              time.Time
	Cluster         string
	Limit           int
	ExcludeCounters bool // drop counter updates before the limit is applied
}

// where builds the shared predicate, so the feed and the counts it is described
// by can never be selected from different sets of rows.
func (q ChangeQuery) where() (string, []any) {
	sql := " WHERE 1=1"
	var args []any
	if !q.From.IsZero() {
		sql += " AND ts >= ?"
		args = append(args, q.From.Unix())
	}
	if !q.To.IsZero() {
		sql += " AND ts <= ?"
		args = append(args, q.To.Unix())
	}
	if q.Cluster != "" {
		sql += " AND cluster = ?"
		args = append(args, q.Cluster)
	}
	if q.ExcludeCounters {
		sql += " AND COALESCE(comp_compare,'') != ?"
		args = append(args, model.CompareInfo)
	}
	return sql, args
}

// Changes returns recorded changes, newest first.
//
// Counters are excluded here rather than by the caller, because the limit has
// to apply to what is actually shown. A day with thousands of counter updates
// would otherwise fill the limit with them and hand back a page that looks
// empty once they are filtered out.
func (s *Store) Changes(q ChangeQuery) ([]model.Change, error) {
	where, args := q.where()
	sql := `SELECT cluster, ts, kind, COALESCE(comp_key,''), COALESCE(name,''),
	               COALESCE(comp_group,''), COALESCE(comp_compare,''),
	               COALESCE(old_value,''), COALESCE(new_value,'')
	        FROM changes` + where + " ORDER BY ts DESC, id DESC"
	if q.Limit > 0 {
		sql += " LIMIT ?"
		args = append(args, q.Limit)
	}

	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Change{}
	for rows.Next() {
		var c model.Change
		var ts int64
		if err := rows.Scan(&c.Cluster, &ts, &c.Kind, &c.Key, &c.Name, &c.Group, &c.Compare, &c.From, &c.To); err != nil {
			return nil, err
		}
		c.Time = time.Unix(ts, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountCounters returns how many counter updates the query's range holds,
// ignoring its limit, so the feed can say what it is not showing. The query's
// own ExcludeCounters is ignored: this counts exactly what that flag removes.
func (s *Store) CountCounters(q ChangeQuery) (int, error) {
	q.ExcludeCounters = false
	where, args := q.where()
	args = append(args, model.CompareInfo)

	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM changes`+where+` AND COALESCE(comp_compare,'') = ?`, args...).Scan(&n)
	return n, err
}

// ChangeDay is one calendar day that has changes, so the calendar can mark it.
// Counters are reported separately from the total because they move on nearly
// every scrape: a calendar that weighed them the same would mark every day and
// promise changes the feed then hides.
type ChangeDay struct {
	Date     string `json:"date"`     // YYYY-MM-DD, in the location the query was made with
	Count    int    `json:"count"`    // all changes that day
	Counters int    `json:"counters"` // how many of those were counter updates
	Clusters int    `json:"clusters"` // number of distinct clusters that changed
}

// ChangeDays counts changes per calendar day in the given range. Days are
// bucketed in loc, so the calendar lines up with the reader's clock rather than
// with UTC.
func (s *Store) ChangeDays(from, to time.Time, loc *time.Location) ([]ChangeDay, error) {
	if loc == nil {
		loc = time.UTC
	}
	rows, err := s.db.Query(
		`SELECT cluster, ts, COALESCE(comp_compare,'') FROM changes WHERE ts >= ? AND ts <= ?`,
		from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	counters := map[string]int{}
	clusters := map[string]map[string]bool{}
	for rows.Next() {
		var cluster, compare string
		var ts int64
		if err := rows.Scan(&cluster, &ts, &compare); err != nil {
			return nil, err
		}
		day := time.Unix(ts, 0).In(loc).Format("2006-01-02")
		counts[day]++
		if compare == model.CompareInfo {
			counters[day]++
		}
		if clusters[day] == nil {
			clusters[day] = map[string]bool{}
		}
		clusters[day][cluster] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]ChangeDay, 0, len(counts))
	for day, n := range counts {
		out = append(out, ChangeDay{Date: day, Count: n, Counters: counters[day], Clusters: len(clusters[day])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

// SnapshotsAt returns each cluster's newest snapshot at or before t, which is
// what the matrix looked like then. Clusters that had not been scraped yet are
// absent, exactly as they were.
func (s *Store) SnapshotsAt(t time.Time) ([]model.Snapshot, error) {
	rows, err := s.db.Query(`
SELECT s.id, s.cluster, s.ts, s.ok, COALESCE(s.error,''), COALESCE(s.sort_order, 1000000)
FROM snapshots s
JOIN (SELECT cluster, MAX(ts) AS mts FROM snapshots WHERE ts <= ? GROUP BY cluster) m
  ON s.cluster = m.cluster AND s.ts = m.mts
GROUP BY s.cluster`, t.Unix())
	if err != nil {
		return nil, err
	}
	return s.scanSnapshots(rows)
}

// Span reports the time range history covers, for bounding a calendar. Both are
// zero when there is no history yet.
func (s *Store) Span() (first, last time.Time, err error) {
	var lo, hi sql.NullInt64
	if err := s.db.QueryRow(`SELECT MIN(ts), MAX(ts) FROM snapshots`).Scan(&lo, &hi); err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !lo.Valid || !hi.Valid {
		return time.Time{}, time.Time{}, nil
	}
	return time.Unix(lo.Int64, 0), time.Unix(hi.Int64, 0), nil
}
