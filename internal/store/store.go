// Package store persists cluster snapshots in embedded SQLite, retaining full
// history so version progression over time can be reconstructed.
package store

import (
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no cgo

	"github.com/croz-ltd/periscope/internal/logging"
	"github.com/croz-ltd/periscope/internal/model"
)

// dsnParams are applied to every pooled connection.
//
//   - busy_timeout makes a connection wait for a lock instead of failing the
//     statement outright with SQLITE_BUSY. It covers contention this process
//     cannot serialise itself, for example `periscope report` reading the same file
//     while `periscope serve` writes.
//   - journal_mode=WAL lets readers (the dashboard, the API) run while a
//     scrape writes. The default rollback journal makes them lock each other
//     out. Filesystems without shared-memory support silently keep the old
//     mode, which is a slowdown, not a failure.
//   - synchronous=NORMAL is the usual companion to WAL: durable across a
//     process crash, and only at risk on host power loss, which for a cache of
//     scraped cluster state costs one interval of history.
//   - txlock=immediate takes the write lock when the transaction begins rather
//     than mid-way through, so a busy database is waited out by busy_timeout
//     instead of erroring on the first INSERT.
const dsnParams = "_pragma=busy_timeout(10000)" +
	"&_pragma=journal_mode(WAL)" +
	"&_pragma=synchronous(NORMAL)" +
	"&_txlock=immediate"

type Store struct {
	db *sql.DB
	// SQLite permits a single writer at a time. Snapshots are saved from one
	// goroutine per cluster, so writes are serialised here rather than left to
	// collide on the database lock.
	writeMu sync.Mutex
	// rebuilt is closed once the startup feed rebuild has finished, whether it
	// did any work or not.
	rebuilt chan struct{}
}

// Open opens (creating if needed) the SQLite database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?"+dsnParams)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, rebuilt: make(chan struct{})}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	logging.For("store").Debug("store opened", "path", path)

	go s.rebuildFeed(time.Now())
	return s, nil
}

// rebuildFeed brings the derived change feed up to date with the current
// detection logic.
//
// It runs in the background because on a fleet with months of history this is
// tens of seconds of work against the database, and doing it before the server
// starts races the pod's liveness probe into a restart loop. Nothing is
// lost by waiting: readers keep seeing the feed that is already recorded until
// the rebuild commits, and then it swaps over in one step. A failure leaves the
// version marker alone, so the next start tries again.
func (s *Store) rebuildFeed(now time.Time) {
	defer close(s.rebuilt)
	log := logging.For("store")

	started := time.Now()
	n, ran, err := s.rebuildChanges(now)
	switch {
	case err != nil:
		log.Error("cannot rebuild the change feed, keeping the recorded one",
			"error", err, "duration", time.Since(started).Round(time.Millisecond))
	case ran:
		log.Info("rebuilt the change feed from stored history",
			"changes", n, "logicVersion", changesLogicVersion,
			"duration", time.Since(started).Round(time.Millisecond))
	default:
		log.Debug("change feed is current", "logicVersion", changesLogicVersion)
	}
}

// waitRebuild blocks until the startup rebuild has finished. Tests use it so
// they read a settled feed rather than racing it.
func (s *Store) waitRebuild() { <-s.rebuilt }

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS snapshots (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  cluster    TEXT    NOT NULL,
  ts         INTEGER NOT NULL,
  ok         INTEGER NOT NULL,
  error      TEXT,
  sort_order INTEGER
);
CREATE TABLE IF NOT EXISTS components (
  snapshot_id  INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
  comp_key     TEXT    NOT NULL,
  name         TEXT,
  kind         TEXT,
  namespace    TEXT,
  version      TEXT,
  extra        TEXT,
  comp_group   TEXT,
  comp_compare TEXT
);
CREATE TABLE IF NOT EXISTS changes (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  cluster      TEXT    NOT NULL,
  ts           INTEGER NOT NULL,
  kind         TEXT    NOT NULL,
  comp_key     TEXT,
  name         TEXT,
  comp_group   TEXT,
  comp_compare TEXT,
  old_value    TEXT,
  new_value    TEXT
);
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT
);
CREATE INDEX IF NOT EXISTS idx_snap_cluster_ts ON snapshots(cluster, ts DESC);
CREATE INDEX IF NOT EXISTS idx_comp_snap ON components(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_comp_key_snap ON components(comp_key, snapshot_id);
CREATE INDEX IF NOT EXISTS idx_changes_ts ON changes(ts DESC);
CREATE INDEX IF NOT EXISTS idx_changes_cluster_ts ON changes(cluster, ts DESC);
`)
	if err != nil {
		return err
	}
	// Add columns introduced after the table's first release. ALTER ... ADD COLUMN
	// errors if the column already exists, which is expected and ignored.
	for _, col := range []string{"comp_group TEXT", "comp_compare TEXT"} {
		_, _ = s.db.Exec("ALTER TABLE components ADD COLUMN " + col)
	}
	_, _ = s.db.Exec("ALTER TABLE snapshots ADD COLUMN sort_order INTEGER")
	return nil
}

// queryer and execer let the same helpers run on the pool or inside a
// transaction, so a save can read the previous snapshot it is diffing against.
type queryer interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

type execer interface {
	Exec(string, ...any) (sql.Result, error)
}

// SaveSnapshot appends a snapshot and its components (history is retained), and
// records what changed since this cluster's previous snapshot in the same
// transaction, so the change feed can never disagree with the history it
// describes.
func (s *Store) SaveSnapshot(snap model.Snapshot) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	prior, err := s.historyFor(tx, snap.Cluster)
	if err != nil {
		return err
	}
	changes := diff(prior, snap)

	okInt := 0
	if snap.OK {
		okInt = 1
	}
	res, err := tx.Exec(`INSERT INTO snapshots(cluster, ts, ok, error, sort_order) VALUES(?,?,?,?,?)`,
		snap.Cluster, snap.Time.Unix(), okInt, snap.Error, snap.Order)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	for _, c := range snap.Components {
		extra, _ := json.Marshal(c.Extra)
		if _, err := tx.Exec(
			`INSERT INTO components(snapshot_id, comp_key, name, kind, namespace, version, extra, comp_group, comp_compare) VALUES(?,?,?,?,?,?,?,?,?)`,
			id, c.Key, c.Name, c.Kind, c.Namespace, c.Version, string(extra), c.Group, c.Compare); err != nil {
			return err
		}
	}
	if err := insertChanges(tx, changes); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	logging.For("store").Debug("snapshot saved",
		"cluster", snap.Cluster, "ok", snap.OK,
		"components", len(snap.Components), "changes", len(changes))
	return nil
}

// LatestSnapshots returns the most recent snapshot per cluster.
func (s *Store) LatestSnapshots() ([]model.Snapshot, error) {
	rows, err := s.db.Query(`
SELECT s.id, s.cluster, s.ts, s.ok, COALESCE(s.error,''), COALESCE(s.sort_order, 1000000)
FROM snapshots s
JOIN (SELECT cluster, MAX(ts) AS mts FROM snapshots GROUP BY cluster) m
  ON s.cluster = m.cluster AND s.ts = m.mts
GROUP BY s.cluster`)
	if err != nil {
		return nil, err
	}
	return s.scanSnapshots(rows)
}

// scanSnapshots reads snapshot rows (id, cluster, ts, ok, error, order) and
// attaches each one's components.
func (s *Store) scanSnapshots(rows *sql.Rows) ([]model.Snapshot, error) {
	defer rows.Close()

	var snaps []model.Snapshot
	var ids []int64
	idx := map[int64]int{}
	for rows.Next() {
		var id, ts int64
		var cluster, errStr string
		var ok, order int
		if err := rows.Scan(&id, &cluster, &ts, &ok, &errStr, &order); err != nil {
			return nil, err
		}
		snaps = append(snaps, model.Snapshot{Cluster: cluster, Time: time.Unix(ts, 0), OK: ok == 1, Error: errStr, Order: order})
		idx[id] = len(snaps) - 1
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		comps, err := s.componentsFor(id)
		if err != nil {
			return nil, err
		}
		snaps[idx[id]].Components = comps
	}
	return snaps, nil
}

func (s *Store) componentsFor(snapID int64) ([]model.Component, error) {
	return s.componentsForQ(s.db, snapID)
}

func (s *Store) componentsForQ(q queryer, snapID int64) ([]model.Component, error) {
	rows, err := q.Query(
		`SELECT comp_key, name, kind, namespace, version, extra,
		        COALESCE(comp_group,''), COALESCE(comp_compare,'')
		 FROM components WHERE snapshot_id = ?`, snapID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Component
	for rows.Next() {
		var c model.Component
		var extra string
		if err := rows.Scan(&c.Key, &c.Name, &c.Kind, &c.Namespace, &c.Version, &extra, &c.Group, &c.Compare); err != nil {
			return nil, err
		}
		if extra != "" && extra != "null" {
			_ = json.Unmarshal([]byte(extra), &c.Extra)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
