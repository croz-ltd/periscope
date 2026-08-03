// Package drift turns per-cluster snapshots into a comparison matrix. Rows are
// judged by their comparison kind: semver drift vs the fleet-max (version),
// config consistency vs the fleet's common value (match), or absolute date
// thresholds (expiry). See model.Compare* and DESIGN.md.
package drift

import (
	"sort"
	"strings"
	"time"

	"github.com/croz-ltd/periscope/internal/model"
	"github.com/croz-ltd/periscope/internal/version"
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
	// info comparison
	StateInfo CellState = "info" // informational value, no drift judgement
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
	Order int       `json:"order"`
}

// Group is an ordered matrix section: a title and the row keys under it. The
// same key may appear in multiple groups.
type Group struct {
	Title string   `json:"title"`
	Keys  []string `json:"keys"`
}

// Page identifiers. A row's default page is Statistics for info comparisons
// (counts), Compare for everything else (version/match/expiry).
const (
	PageCompare    = "compare"
	PageStatistics = "statistics"
)

// PageView is one navigable page (Compare / Statistics) and its ordered groups.
type PageView struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Groups []Group `json:"groups"`
}

// GroupConfig is the custom grouping loaded from the periscope-groups ConfigMap.
// Compare/Statistics target a specific page; Hidden removes keys from both;
// Groups is a legacy alias applied to the Compare page.
type GroupConfig struct {
	Compare    []Group  `json:"compare"`
	Statistics []Group  `json:"statistics"`
	Hidden     []string `json:"hidden"`
	Groups     []Group  `json:"groups"`
}

// Matrix is the full comparison view.
type Matrix struct {
	Clusters []ClusterInfo `json:"clusters"`
	Rows     []Row         `json:"rows"`      // all rows, keyed; referenced by page groups
	Pages    []PageView    `json:"pages"`     // Compare + Statistics, each with ordered groups
	Warning  string        `json:"warning,omitempty"`
}

// builtinGroupOrder is the fixed section order used when no custom grouping is set.
var builtinGroupOrder = []string{
	model.GroupOpenShift, model.GroupNode, model.GroupMCP, model.GroupStorage,
	model.GroupCert, model.GroupVirt, model.GroupOperators,
}

type instance struct {
	cluster string
	comp    model.Component
}

// Build assembles the matrix from the latest snapshot per cluster. A cluster is
// marked stale when its snapshot is older than now-staleAfter (0 disables).
// cfg is the custom grouping (nil = built-in section order).
func Build(snaps []model.Snapshot, now time.Time, staleAfter time.Duration, cfg *GroupConfig) Matrix {
	// Non-nil slices so the JSON is always [] not null (keeps the UI .map safe).
	m := Matrix{Clusters: []ClusterInfo{}, Rows: []Row{}, Pages: []PageView{}}
	var clusterNames []string
	for _, s := range snaps {
		stale := staleAfter > 0 && now.Sub(s.Time) > staleAfter
		m.Clusters = append(m.Clusters, ClusterInfo{Name: s.Cluster, Time: s.Time, OK: s.OK, Error: s.Error, Stale: stale, Order: s.Order})
		clusterNames = append(clusterNames, s.Cluster)
	}
	// Column order: by the Secret's order label (lower = left), then by name.
	sort.Slice(m.Clusters, func(i, j int) bool {
		if m.Clusters[i].Order != m.Clusters[j].Order {
			return m.Clusters[i].Order < m.Clusters[j].Order
		}
		return m.Clusters[i].Name < m.Clusters[j].Name
	})

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
		case model.CompareInfo:
			buildInfoRow(&row, insts)
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
	assignPages(&m, cfg)
	return m
}

// defaultPage places info comparisons (counts) on Statistics, everything else
// on Compare.
func defaultPage(compare string) string {
	if compare == model.CompareInfo {
		return PageStatistics
	}
	return PageCompare
}

// assignPages splits the rows into the Compare and Statistics pages. Each page's
// groups come from its ConfigMap section when present (authoritative: unmatched/
// hidden keys skipped, empty groups dropped, leftovers for that page collected
// into "Ungrouped"), otherwise from the built-in section order filtered to that
// page's rows. Hidden keys are removed from both pages.
func assignPages(m *Matrix, cfg *GroupConfig) {
	nameByKey := map[string]string{}
	groupByKey := map[string]string{}
	pageByKey := map[string]string{}
	var allKeys []string
	for _, r := range m.Rows {
		nameByKey[r.Key] = r.Name
		groupByKey[r.Key] = r.Group
		pageByKey[r.Key] = defaultPage(r.Compare)
		allKeys = append(allKeys, r.Key)
	}
	exists := func(k string) bool { _, ok := nameByKey[k]; return ok }
	sortByName := func(keys []string) {
		sort.Slice(keys, func(i, j int) bool {
			return strings.ToLower(nameByKey[keys[i]]) < strings.ToLower(nameByKey[keys[j]])
		})
	}

	hidden := map[string]bool{}
	var compareSection, statSection []Group
	if cfg != nil {
		for _, k := range cfg.Hidden {
			hidden[k] = true
		}
		compareSection = cfg.Compare
		if len(compareSection) == 0 {
			compareSection = cfg.Groups // legacy alias -> Compare page
		}
		statSection = cfg.Statistics
	}

	used := map[string]bool{}
	buildSection := func(section []Group) []Group {
		var out []Group
		for _, g := range section {
			var keys []string
			seen := map[string]bool{}
			for _, k := range g.Keys { // preserve config order
				if exists(k) && !hidden[k] && !seen[k] {
					keys = append(keys, k)
					seen[k] = true
					used[k] = true
				}
			}
			if len(keys) > 0 {
				out = append(out, Group{Title: g.Title, Keys: keys})
			}
		}
		return out
	}
	buildBuiltin := func(page string) []Group {
		byGroup := map[string][]string{}
		for _, k := range allKeys {
			if hidden[k] || pageByKey[k] != page {
				continue
			}
			byGroup[groupByKey[k]] = append(byGroup[groupByKey[k]], k)
			used[k] = true
		}
		var out []Group
		emit := func(title string) {
			if keys := byGroup[title]; len(keys) > 0 {
				sortByName(keys)
				out = append(out, Group{Title: title, Keys: keys})
				delete(byGroup, title)
			}
		}
		for _, title := range builtinGroupOrder {
			emit(title)
		}
		var rest []string
		for title := range byGroup {
			rest = append(rest, title)
		}
		sort.Strings(rest)
		for _, title := range rest {
			display := title
			if display == "" {
				display = "Ungrouped"
			}
			keys := byGroup[title]
			sortByName(keys)
			out = append(out, Group{Title: display, Keys: keys})
		}
		return out
	}

	compareAuthoritative := len(compareSection) > 0
	statAuthoritative := len(statSection) > 0
	var compareGroups, statGroups []Group
	if compareAuthoritative {
		compareGroups = buildSection(compareSection)
	} else {
		compareGroups = buildBuiltin(PageCompare)
	}
	if statAuthoritative {
		statGroups = buildSection(statSection)
	} else {
		statGroups = buildBuiltin(PageStatistics)
	}
	// Leftovers land in their default page's "Ungrouped" (authoritative pages only;
	// built-in pages already include all their rows).
	ungroupedFor := func(page string) []string {
		var ung []string
		for _, k := range allKeys {
			if !hidden[k] && !used[k] && pageByKey[k] == page {
				ung = append(ung, k)
			}
		}
		sortByName(ung)
		return ung
	}
	if compareAuthoritative {
		if ung := ungroupedFor(PageCompare); len(ung) > 0 {
			compareGroups = append(compareGroups, Group{Title: "Ungrouped", Keys: ung})
		}
	}
	if statAuthoritative {
		if ung := ungroupedFor(PageStatistics); len(ung) > 0 {
			statGroups = append(statGroups, Group{Title: "Ungrouped", Keys: ung})
		}
	}
	if compareGroups == nil {
		compareGroups = []Group{}
	}
	if statGroups == nil {
		statGroups = []Group{}
	}
	m.Pages = []PageView{
		{ID: PageCompare, Title: "Compare", Groups: compareGroups},
		{ID: PageStatistics, Title: "Statistics", Groups: statGroups},
	}
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

// buildInfoRow: display each cell's value with no drift judgement.
func buildInfoRow(row *Row, insts []instance) {
	for _, in := range insts {
		cell := newCell(in)
		cell.State = StateInfo
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
