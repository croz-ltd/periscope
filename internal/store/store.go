// Package store persists cluster snapshots in embedded SQLite, retaining full
// history so version progression over time can be reconstructed.
package store

import (
	"database/sql"
	"encoding/json"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no cgo

	"github.com/croz-ltd/periscope/internal/model"
)

type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

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
CREATE INDEX IF NOT EXISTS idx_snap_cluster_ts ON snapshots(cluster, ts DESC);
CREATE INDEX IF NOT EXISTS idx_comp_snap ON components(snapshot_id);
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

// SaveSnapshot appends a snapshot and its components (history is retained).
func (s *Store) SaveSnapshot(snap model.Snapshot) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

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
	return tx.Commit()
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
	rows, err := s.db.Query(
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
