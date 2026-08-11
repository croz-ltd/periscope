package drift

import (
	"strings"
	"testing"
	"time"

	"github.com/croz-ltd/periscope/internal/model"
)

func TestBuildEmpty(t *testing.T) {
	m := Build(nil, time.Unix(1_000_000, 0), time.Hour, nil)
	if m.Clusters == nil || m.Rows == nil {
		t.Fatalf("empty build must return non-nil slices, got clusters=%v rows=%v", m.Clusters, m.Rows)
	}
	if len(m.Clusters) != 0 || len(m.Rows) != 0 {
		t.Fatalf("empty build must be empty, got %d clusters %d rows", len(m.Clusters), len(m.Rows))
	}
}

func TestClusterOrder(t *testing.T) {
	now := time.Unix(1000, 0)
	comp := []model.Component{{Key: "openshift", Name: "OpenShift", Compare: model.CompareVersion, Version: "4.14.0"}}
	// "zebra" has the lowest order so it must come first despite the name.
	snaps := []model.Snapshot{
		{Cluster: "alpha", Time: now, OK: true, Order: 20, Components: comp},
		{Cluster: "zebra", Time: now, OK: true, Order: 10, Components: comp},
		{Cluster: "mid", Time: now, OK: true, Order: 1000000, Components: comp}, // unlabeled -> right
	}
	m := Build(snaps, now, time.Hour, nil)
	got := []string{m.Clusters[0].Name, m.Clusters[1].Name, m.Clusters[2].Name}
	want := []string{"zebra", "alpha", "mid"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("column order = %v, want %v", got, want)
		}
	}
}

// pageGroups returns the named page's groups as a title->keys map plus the
// ordered titles.
func pageGroups(m Matrix, id string) (titles []string, byTitle map[string][]string) {
	byTitle = map[string][]string{}
	for _, p := range m.Pages {
		if p.ID != id {
			continue
		}
		for _, g := range p.Groups {
			titles = append(titles, g.Title)
			byTitle[g.Title] = g.Keys
		}
	}
	return
}

func TestCustomGroups(t *testing.T) {
	now := time.Unix(1000, 0)
	c := func(key, name, group string) model.Component {
		return model.Component{Key: key, Name: name, Group: group, Compare: model.CompareVersion, Version: "1.0.0"}
	}
	snap := model.Snapshot{Cluster: "a", Time: now, OK: true, Components: []model.Component{
		c("openshift", "OpenShift", model.GroupOpenShift),
		c("portworx-csi", "Portworx", model.GroupOperators),
		c("dell-csi", "Dell", model.GroupOperators),
		c("cert-api", "API", model.GroupCert),
	}}
	cfg := &GroupConfig{Compare: []Group{
		{Title: "Storage", Keys: []string{"portworx-csi", "dell-csi", "ghost"}}, // ghost skipped
		{Title: "Mixed", Keys: []string{"portworx-csi", "openshift"}},           // portworx in 2 groups
		{Title: "Empty", Keys: []string{"nope"}},                                // no match -> dropped
	}}
	m := Build([]model.Snapshot{snap}, now, time.Hour, cfg)

	titles, byTitle := pageGroups(m, PageCompare)
	if strings.Join(titles, ",") != "Storage,Mixed,Ungrouped" {
		t.Fatalf("compare group titles = %v", titles)
	}
	if strings.Join(byTitle["Storage"], ",") != "portworx-csi,dell-csi" {
		t.Errorf("Storage keys = %v (ghost skipped, order preserved)", byTitle["Storage"])
	}
	if strings.Join(byTitle["Mixed"], ",") != "portworx-csi,openshift" {
		t.Errorf("Mixed keys = %v (portworx repeats across groups)", byTitle["Mixed"])
	}
	if strings.Join(byTitle["Ungrouped"], ",") != "cert-api" {
		t.Errorf("Ungrouped keys = %v (want cert-api)", byTitle["Ungrouped"])
	}
}

func TestBuiltinGroupsFallback(t *testing.T) {
	now := time.Unix(1000, 0)
	c := func(key, name, group string) model.Component {
		return model.Component{Key: key, Name: name, Group: group, Compare: model.CompareVersion, Version: "1.0.0"}
	}
	snap := model.Snapshot{Cluster: "a", Time: now, OK: true, Components: []model.Component{
		c("op-z", "Zeta operator", model.GroupOperators),
		c("op-a", "Alpha operator", model.GroupOperators),
		c("openshift", "OpenShift", model.GroupOpenShift),
	}}
	m := Build([]model.Snapshot{snap}, now, time.Hour, nil) // no cfg -> built-in order
	titles, byTitle := pageGroups(m, PageCompare)
	if strings.Join(titles, ",") != model.GroupOpenShift+","+model.GroupOperators {
		t.Fatalf("built-in group order wrong: %v", titles)
	}
	if strings.Join(byTitle[model.GroupOperators], ",") != "op-a,op-z" {
		t.Errorf("Operators keys are not sorted by name: %v", byTitle[model.GroupOperators])
	}
}

func TestPagesSplitAndHidden(t *testing.T) {
	now := time.Unix(1000, 0)
	snap := model.Snapshot{Cluster: "a", Time: now, OK: true, Components: []model.Component{
		{Key: "openshift", Name: "OpenShift", Group: model.GroupOpenShift, Compare: model.CompareVersion, Version: "4.14.0"},
		{Key: "node-count", Name: "Total nodes", Group: model.GroupNode, Compare: model.CompareInfo, Version: "6"},
		{Key: "vm-total", Name: "Virtual machines", Group: model.GroupVirt, Compare: model.CompareInfo, Version: "3"},
	}}
	// No cfg: info rows -> Statistics, others -> Compare.
	m := Build([]model.Snapshot{snap}, now, time.Hour, nil)
	_, cmp := pageGroups(m, PageCompare)
	_, stat := pageGroups(m, PageStatistics)
	if _, ok := cmp[model.GroupOpenShift]; !ok {
		t.Errorf("openshift is missing from the Compare page: %+v", cmp)
	}
	if len(stat) == 0 {
		t.Errorf("info rows left the Statistics page empty")
	}
	// Hidden removes a key from every page.
	m2 := Build([]model.Snapshot{snap}, now, time.Hour, &GroupConfig{Hidden: []string{"vm-total"}})
	for _, p := range m2.Pages {
		for _, g := range p.Groups {
			for _, k := range g.Keys {
				if k == "vm-total" {
					t.Errorf("hidden key vm-total still present on page %s", p.ID)
				}
			}
		}
	}
}

func TestBuildInfo(t *testing.T) {
	now := time.Unix(1000, 0)
	mk := func(cluster, n string) model.Snapshot {
		return model.Snapshot{Cluster: cluster, Time: now, OK: true, Components: []model.Component{
			{Key: "node-count", Name: "Total nodes", Group: model.GroupNode, Compare: model.CompareInfo, Version: n},
		}}
	}
	m := Build([]model.Snapshot{mk("a", "6"), mk("b", "12")}, now, time.Hour, nil)
	row := m.Rows[0]
	// info never judges drift, even though the values differ.
	if row.Cells["a"].State != StateInfo || row.Cells["b"].State != StateInfo {
		t.Errorf("info cells are %s / %s, want StateInfo", row.Cells["a"].State, row.Cells["b"].State)
	}
	if row.Cells["a"].Version != "6" || row.Cells["b"].Version != "12" {
		t.Errorf("info cells lost their values, got %q / %q", row.Cells["a"].Version, row.Cells["b"].Version)
	}
}

func TestBuildMatch(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	snaps := []model.Snapshot{
		{Cluster: "a", Time: now, OK: true, Components: []model.Component{
			{Key: "chan", Name: "Channel", Group: model.GroupOpenShift, Compare: model.CompareMatch, Version: "stable-4.14"},
		}},
		{Cluster: "b", Time: now, OK: true, Components: []model.Component{
			{Key: "chan", Name: "Channel", Group: model.GroupOpenShift, Compare: model.CompareMatch, Version: "stable-4.14"},
		}},
		{Cluster: "c", Time: now, OK: true, Components: []model.Component{
			{Key: "chan", Name: "Channel", Group: model.GroupOpenShift, Compare: model.CompareMatch, Version: "fast-4.15"},
		}},
	}
	m := Build(snaps, now, time.Hour, nil)
	row := m.Rows[0]
	if row.Leader != "stable-4.14" { // the common (majority) value
		t.Errorf("expected common value stable-4.14, got %q", row.Leader)
	}
	if row.Cells["a"].State != StateMatch || row.Cells["b"].State != StateMatch {
		t.Errorf("a/b are %s %s, want match", row.Cells["a"].State, row.Cells["b"].State)
	}
	if row.Cells["c"].State != StateMismatch {
		t.Errorf("c is %s, want mismatch", row.Cells["c"].State)
	}
}

func TestBuildExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	mk := func(cluster string, days int) model.Snapshot {
		exp := now.Add(time.Duration(days) * 24 * time.Hour).UTC().Format(time.RFC3339)
		return model.Snapshot{Cluster: cluster, Time: now, OK: true, Components: []model.Component{
			{Key: "cert-api", Name: "API", Group: model.GroupCert, Compare: model.CompareExpiry, Version: exp},
		}}
	}
	m := Build([]model.Snapshot{mk("crit", 30), mk("warn", 90), mk("ok", 200)}, now, time.Hour, nil)
	row := m.Rows[0]
	if row.Cells["crit"].State != StateExpiryCrit {
		t.Errorf("30d is %s, want crit", row.Cells["crit"].State)
	}
	if row.Cells["warn"].State != StateExpiryWarn {
		t.Errorf("90d is %s, want warn", row.Cells["warn"].State)
	}
	if row.Cells["ok"].State != StateExpiryOK {
		t.Errorf("200d is %s, want ok", row.Cells["ok"].State)
	}
	if row.Cells["warn"].Severity != 90 {
		t.Errorf("expected 90 days remaining, got %d", row.Cells["warn"].Severity)
	}
}

func TestBuild(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	snaps := []model.Snapshot{
		{Cluster: "b", Time: now, OK: true, Components: []model.Component{
			{Key: "openshift", Name: "OpenShift", Kind: "openshift", Version: "4.14.9"},
			{Key: "portworx-csi", Name: "Portworx", Kind: "csi", Version: "not-semver"},
		}},
		{Cluster: "a", Time: now.Add(-2 * time.Hour), OK: true, Components: []model.Component{
			{Key: "openshift", Name: "OpenShift", Kind: "openshift", Version: "4.12.0"},
			// no portworx here -> not installed
		}},
	}

	m := Build(snaps, now, time.Hour, nil)

	if len(m.Clusters) != 2 || m.Clusters[0].Name != "a" || m.Clusters[1].Name != "b" {
		t.Fatalf("clusters not sorted/complete: %+v", m.Clusters)
	}
	if m.Clusters[0].Stale != true {
		t.Errorf("cluster a is fresh, want stale (2h old, threshold 1h)")
	}
	if m.Clusters[1].Stale != false {
		t.Errorf("cluster b is stale, want fresh")
	}

	rows := map[string]Row{}
	for _, r := range m.Rows {
		rows[r.Key] = r
	}

	ocp := rows["openshift"]
	if ocp.Leader != "4.14.9" {
		t.Errorf("openshift leader = %q, want 4.14.9", ocp.Leader)
	}
	if ocp.Cells["b"].State != StateLeader {
		t.Errorf("b openshift is %s, want leader", ocp.Cells["b"].State)
	}
	if ocp.Cells["a"].State != StateBehind || ocp.Cells["a"].GapKind != "minor" {
		t.Errorf("a openshift is %s/%s, want behind/minor", ocp.Cells["a"].State, ocp.Cells["a"].GapKind)
	}

	px := rows["portworx-csi"]
	if px.Cells["b"].State != StateUnknown {
		t.Errorf("b portworx is %s, want unknown (unparseable)", px.Cells["b"].State)
	}
	if px.Cells["a"].State != StateNotInstalled {
		t.Errorf("a portworx is %s, want not_installed", px.Cells["a"].State)
	}
}

// A cluster's console banner describes the column, so it must head the column
// rather than become a row that every cluster "differs" on.
func TestBuildLiftsConsoleBannerIntoTheHeader(t *testing.T) {
	now := time.Now()
	snaps := []model.Snapshot{
		{Cluster: "a", Time: now, OK: true, Components: []model.Component{
			{Key: "openshift", Name: "OpenShift", Compare: model.CompareVersion, Version: "4.14.9"},
			{Key: model.KeyClusterBanner, Name: "Console banner", Compare: model.CompareInfo, Version: "PRODUCTION",
				Extra: map[string]string{"color": "#fff", "backgroundColor": "#C9190B"}},
		}},
		{Cluster: "b", Time: now, OK: true, Components: []model.Component{
			{Key: "openshift", Name: "OpenShift", Compare: model.CompareVersion, Version: "4.14.9"},
		}},
	}

	m := Build(snaps, now, time.Hour, nil)

	for _, r := range m.Rows {
		if r.Key == model.KeyClusterBanner {
			t.Fatal("the banner must not be a matrix row")
		}
	}
	var a, b ClusterInfo
	for _, c := range m.Clusters {
		switch c.Name {
		case "a":
			a = c
		case "b":
			b = c
		}
	}
	if a.Label != "PRODUCTION" || a.BgColor != "#C9190B" || a.Color != "#fff" {
		t.Errorf("cluster a header = %+v, want the banner text and colours", a)
	}
	if b.Label != "" {
		t.Errorf("cluster b has no banner, so the header keeps its name, got label %q", b.Label)
	}
}
