// Package drift turns per-cluster snapshots into a comparison matrix. Rows are
// judged by their comparison kind: semver drift vs the fleet-max (version),
// config consistency vs the fleet's common value (match), or absolute date
// thresholds (expiry) — see model.Compare* and DESIGN.md.
package drift

import (
	"sort"
	"time"

	"github.com/croz-ltd/cluster-comparator/internal/model"
	"github.com/croz-ltd/cluster-comparator/internal/version"
)

// CellState classifies one cluster's standing for one component.
type CellState string

const (
	// version comparison
	StateLeader CellState = "leader" // equals the fleet-max version
	StateBehind CellState = "behind" // parseable but below the leader
	// match comparison
	StateMatch    CellState = "match"    // equals the fleet's common value
	StateMismatch CellState = "mismatch" // differs from the common value
	// expiry comparison
	StateExpiryOK   CellState = "expiry_ok"   // > 120 days
	StateExpiryWarn CellState = "expiry_warn" // <= 120 days
	StateExpiryCrit CellState = "expiry_crit" // <= 60 days
	// shared
	StateUnknown      CellState = "unknown"       // present but value unparseable
	StateNotInstalled CellState = "not_installed" // component absent on this cluster
)

// Absolute certificate-expiry thresholds (days remaining).
const (
	expiryWarnDays = 120
	expiryCritDays = 60
)

// Cell is one component's status on one cluster.
type Cell struct {
	Cluster   string            `json:"cluster"`
	Version   string            `json:"version,omitempty"`
	State     CellState         `json:"state"`
	Severity  int               `json:"severity"`          // version: gap score; expiry: days remaining
	GapKind   string            `json:"gapKind,omitempty"` // major | minor | patch | prerelease
	Namespace string            `json:"namespace,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// Row is one component across all clusters.
type Row struct {
	Key     string          `json:"key"`
	Name    string          `json:"name"`
	Group   string          `json:"group"`
	Compare string          `json:"compare"`
	Kind    string          `json:"kind"`
	Leader  string          `json:"leader"` // version: fleet-max; match: common value; expiry: unused
	Cells   map[string]Cell `json:"cells"`  // keyed by cluster name
}

// ClusterInfo is a column header: which clusters exist and how fresh each is.
type ClusterInfo struct {
	Name  string    `json:"name"`
	Time  time.Time `json:"time"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
	Stale bool      `json:"stale"`
}

// Matrix is the full comparison view.
type Matrix struct {
	Clusters []ClusterInfo `json:"clusters"`
	Rows     []Row         `json:"rows"`
}

type instance struct {
	cluster string
	comp    model.Component
}

// Build assembles the matrix from the latest snapshot per cluster. A cluster is
// marked stale when its snapshot is older than now-staleAfter (0 disables).
func Build(snaps []model.Snapshot, now time.Time, staleAfter time.Duration) Matrix {
	// Non-nil slices so the JSON is always [] not null (keeps the UI .map safe).
	m := Matrix{Clusters: []ClusterInfo{}, Rows: []Row{}}
	var clusterNames []string
	for _, s := range snaps {
		stale := staleAfter > 0 && now.Sub(s.Time) > staleAfter
		m.Clusters = append(m.Clusters, ClusterInfo{Name: s.Cluster, Time: s.Time, OK: s.OK, Error: s.Error, Stale: stale})
		clusterNames = append(clusterNames, s.Cluster)
	}
	sort.Slice(m.Clusters, func(i, j int) bool { return m.Clusters[i].Name < m.Clusters[j].Name })

	byKey := map[string][]instance{}
	meta := map[string]model.Component{}
	for _, s := range snaps {
		for _, c := range s.Components {
			byKey[c.Key] = append(byKey[c.Key], instance{cluster: s.Cluster, comp: c})
			meta[c.Key] = c
		}
	}

	for key, insts := range byKey {
		c := meta[key]
		row := Row{Key: key, Name: c.Name, Group: c.Group, Compare: c.Compare, Kind: c.Kind, Cells: map[string]Cell{}}

		switch c.Compare {
		case model.CompareMatch:
			buildMatchRow(&row, insts)
		case model.CompareExpiry:
			buildExpiryRow(&row, insts, now)
		default: // CompareVersion / ""
			buildVersionRow(&row, insts)
		}

		// Absent on a cluster => not installed.
		for _, cn := range clusterNames {
			if _, ok := row.Cells[cn]; !ok {
				row.Cells[cn] = Cell{Cluster: cn, State: StateNotInstalled}
			}
		}
		m.Rows = append(m.Rows, row)
	}
	sort.Slice(m.Rows, func(i, j int) bool { return m.Rows[i].Key < m.Rows[j].Key })
	return m
}

func newCell(in instance) Cell {
	return Cell{Cluster: in.cluster, Version: in.comp.Version, Namespace: in.comp.Namespace, Extra: in.comp.Extra}
}

// buildVersionRow: semver drift against the highest parseable version seen.
func buildVersionRow(row *Row, insts []instance) {
	var leader version.Version
	haveLeader := false
	for _, in := range insts {
		v := version.Parse(in.comp.Version)
		if v.OK && (!haveLeader || version.Compare(v, leader) > 0) {
			leader, haveLeader = v, true
		}
	}
	if haveLeader {
		row.Leader = leader.Raw
	}
	for _, in := range insts {
		v := version.Parse(in.comp.Version)
		cell := newCell(in)
		switch {
		case !v.OK || !haveLeader:
			cell.State = StateUnknown
		case version.Compare(v, leader) == 0:
			cell.State = StateLeader
		default:
			cell.State = StateBehind
			cell.Severity, cell.GapKind = gap(v, leader)
		}
		row.Cells[in.cluster] = cell
	}
}

// buildMatchRow: flag cells that differ from the fleet's most common value.
func buildMatchRow(row *Row, insts []instance) {
	counts := map[string]int{}
	for _, in := range insts {
		counts[in.comp.Version]++
	}
	expected := mode(counts)
	row.Leader = expected
	for _, in := range insts {
		cell := newCell(in)
		if in.comp.Version == expected {
			cell.State = StateMatch
		} else {
			cell.State = StateMismatch
		}
		row.Cells[in.cluster] = cell
	}
}

// buildExpiryRow: colour each cell by days-to-expiry against absolute thresholds.
func buildExpiryRow(row *Row, insts []instance, now time.Time) {
	for _, in := range insts {
		cell := newCell(in)
		t, err := time.Parse(time.RFC3339, in.comp.Version)
		if in.comp.Version == "" || err != nil {
			cell.State = StateUnknown
			row.Cells[in.cluster] = cell
			continue
		}
		days := int(t.Sub(now).Hours() / 24)
		cell.Version = t.Format("2006-01-02")
		cell.Severity = days
		switch {
		case days <= expiryCritDays:
			cell.State = StateExpiryCrit
		case days <= expiryWarnDays:
			cell.State = StateExpiryWarn
		default:
			cell.State = StateExpiryOK
		}
		row.Cells[in.cluster] = cell
	}
}

// mode returns the most frequent value; ties broken by lexically smallest for
// deterministic output.
func mode(counts map[string]int) string {
	best, bestN := "", -1
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if counts[k] > bestN {
			best, bestN = k, counts[k]
		}
	}
	return best
}

// gap scores how far v is behind the leader and names the most-significant
// differing level. Major gaps dominate minor, which dominate patch.
func gap(v, leader version.Version) (int, string) {
	switch {
	case leader.Major != v.Major:
		return (leader.Major-v.Major)*10000 + 10000, "major"
	case leader.Minor != v.Minor:
		return (leader.Minor-v.Minor)*100 + 100, "minor"
	case leader.Patch != v.Patch:
		return leader.Patch - v.Patch, "patch"
	default:
		return 1, "prerelease"
	}
}
