package store

import (
	"database/sql"
	"time"

	"github.com/croz-ltd/periscope/internal/logging"
)

// StoredCluster is one cluster the database still holds history for, and how
// much of it. The counts are what makes a purge a decision rather than a
// guess: removing three months of a decommissioned cluster is not the same
// action as removing the two snapshots of one that was joined by mistake.
type StoredCluster struct {
	Name      string    `json:"name"`
	First     time.Time `json:"first"`
	Last      time.Time `json:"last"`
	Snapshots int       `json:"snapshots"`
	Changes   int       `json:"changes"`
}

// Purged counts what one purge removed.
type Purged struct {
	Cluster    string `json:"cluster"`
	Snapshots  int    `json:"snapshots"`
	Components int    `json:"components"`
	Changes    int    `json:"changes"`
}

// StoredClusters lists every cluster the database holds a snapshot for, oldest
// history first, with the span and the size of what is held.
//
// This is deliberately not the matrix. The matrix shows the latest snapshot per
// cluster, so it says nothing about how much history sits behind it, and a
// cluster is worth purging precisely when nobody is looking at it any more.
func (s *Store) StoredClusters() ([]StoredCluster, error) {
	rows, err := s.db.Query(`
SELECT s.cluster, MIN(s.ts), MAX(s.ts), COUNT(*),
       (SELECT COUNT(*) FROM changes c WHERE c.cluster = s.cluster)
FROM snapshots s
GROUP BY s.cluster
ORDER BY s.cluster`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StoredCluster
	for rows.Next() {
		var c StoredCluster
		var first, last int64
		if err := rows.Scan(&c.Name, &first, &last, &c.Snapshots, &c.Changes); err != nil {
			return nil, err
		}
		c.First, c.Last = time.Unix(first, 0), time.Unix(last, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}

// PurgeCluster removes everything the database holds for one cluster: its
// snapshots, their components and its change feed entries. It reports how many
// rows went, so the caller can say what was actually removed rather than that
// something probably was.
//
// The three deletes run in one transaction. A half-purged cluster would keep
// components pointing at snapshots that no longer exist, which nothing in the
// read path expects. Components are deleted explicitly rather than left to the
// foreign key: the schema declares ON DELETE CASCADE, but SQLite only enforces
// it with the foreign_keys pragma on, which this connection does not set.
//
// Purging is not undoable and history is the whole point of the store, so the
// caller decides what may be purged. This function does not.
func (s *Store) PurgeCluster(name string) (Purged, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return Purged{}, err
	}
	defer tx.Rollback()

	comps, err := affected(tx,
		`DELETE FROM components WHERE snapshot_id IN (SELECT id FROM snapshots WHERE cluster = ?)`, name)
	if err != nil {
		return Purged{}, err
	}
	snaps, err := affected(tx, `DELETE FROM snapshots WHERE cluster = ?`, name)
	if err != nil {
		return Purged{}, err
	}
	changes, err := affected(tx, `DELETE FROM changes WHERE cluster = ?`, name)
	if err != nil {
		return Purged{}, err
	}
	if err := tx.Commit(); err != nil {
		return Purged{}, err
	}

	logging.For("store").Info("cluster data purged",
		"cluster", name, "snapshots", snaps, "components", comps, "changes", changes)
	return Purged{Cluster: name, Snapshots: snaps, Components: comps, Changes: changes}, nil
}

func affected(tx *sql.Tx, query string, args ...any) (int, error) {
	res, err := tx.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
