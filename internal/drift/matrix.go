// Package drift turns per-cluster snapshots into a comparison matrix, computing
// per-component "ahead/behind" against the highest version seen across the fleet
// (intra-fleet drift only — see DESIGN.md).
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
	StateLeader       CellState = "leader"        // equals the fleet-max version
	StateBehind       CellState = "behind"        // parseable but below the leader
	StateUnknown      CellState = "unknown"       // present but version unparseable
	StateNotInstalled CellState = "not_installed" // component absent on this cluster
)

// Cell is one component's status on one cluster.
type Cell struct {
	Cluster   string            `json:"cluster"`
	Version   string            `json:"version,omitempty"`
	State     CellState         `json:"state"`
	Severity  int               `json:"severity"`          // 0 = leader; larger = further behind
	GapKind   string            `json:"gapKind,omitempty"` // major | minor | patch | prerelease
	Namespace string            `json:"namespace,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// Row is one component across all clusters.
type Row struct {
	Key    string          `json:"key"`
	Name   string          `json:"name"`
	Kind   string          `json:"kind"`
	Leader string          `json:"leader"` // the fleet-max version (baseline), if any parseable
	Cells  map[string]Cell `json:"cells"`  // keyed by cluster name
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

// Build assembles the matrix from the latest snapshot per cluster. A cluster is
// marked stale when its snapshot is older than now-staleAfter (0 disables).
func Build(snaps []model.Snapshot, now time.Time, staleAfter time.Duration) Matrix {
	// Initialise as empty (non-nil) slices so the JSON is always [] not null,
	// which keeps the UI's .map() safe when no clusters are defined.
	m := Matrix{Clusters: []ClusterInfo{}, Rows: []Row{}}
	var clusterNames []string
	for _, s := range snaps {
		stale := staleAfter > 0 && now.Sub(s.Time) > staleAfter
		m.Clusters = append(m.Clusters, ClusterInfo{Name: s.Cluster, Time: s.Time, OK: s.OK, Error: s.Error, Stale: stale})
		clusterNames = append(clusterNames, s.Cluster)
	}
	sort.Slice(m.Clusters, func(i, j int) bool { return m.Clusters[i].Name < m.Clusters[j].Name })

	type inst struct {
		cluster string
		comp    model.Component
	}
	byKey := map[string][]inst{}
	meta := map[string]model.Component{}
	for _, s := range snaps {
		for _, c := range s.Components {
			byKey[c.Key] = append(byKey[c.Key], inst{cluster: s.Cluster, comp: c})
			meta[c.Key] = c
		}
	}

	for key, insts := range byKey {
		row := Row{Key: key, Name: meta[key].Name, Kind: meta[key].Kind, Cells: map[string]Cell{}}

		// Baseline: highest parseable version across the fleet.
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
			cell := Cell{Cluster: in.cluster, Version: in.comp.Version, Namespace: in.comp.Namespace, Extra: in.comp.Extra}
			switch {
			case !v.OK || !haveLeader:
				cell.State = StateUnknown
			case version.Compare(v, leader) == 0:
				cell.State = StateLeader
			default:
				cell.State = StateBehind
				cell.Severity, cell.GapKind = gap(v, leader)
			}
			row.Cells[in.cluster] = cell // last write wins if a cluster reports the key twice
		}

		// Absent on a cluster => not installed, and excluded from the baseline above.
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

// gap scores how far v is behind the leader and names the most-significant
// differing level. Major gaps dominate minor, which dominate patch, so the UI
// can shade darker for bigger gaps.
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
