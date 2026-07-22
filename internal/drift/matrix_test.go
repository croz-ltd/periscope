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
	cfg := &GroupConfig{Groups: []Group{
		{Title: "Storage", Keys: []string{"portworx-csi", "dell-csi", "ghost"}}, // ghost skipped
		{Title: "Mixed", Keys: []string{"portworx-csi", "openshift"}},           // portworx in 2 groups
		{Title: "Empty", Keys: []string{"nope"}},                                // no match -> dropped
	}}
	m := Build([]model.Snapshot{snap}, now, time.Hour, cfg)

	var got []string
	byTitle := map[string][]string{}
	for _, g := range m.Groups {
		got = append(got, g.Title)
		byTitle[g.Title] = g.Keys
	}
	want := []string{"Storage", "Mixed", "Ungrouped"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("group titles = %v, want %v", got, want)
	}
	if strings.Join(byTitle["Storage"], ",") != "portworx-csi,dell-csi" {
		t.Errorf("Storage keys = %v (want portworx-csi,dell-csi; ghost skipped, order preserved)", byTitle["Storage"])
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
	if len(m.Groups) != 2 || m.Groups[0].Title != model.GroupOpenShift || m.Groups[1].Title != model.GroupOperators {
		t.Fatalf("built-in group order wrong: %+v", m.Groups)
	}
	// alpha within group by display name
	if strings.Join(m.Groups[1].Keys, ",") != "op-a,op-z" {
		t.Errorf("Operators keys should sort by name: %v", m.Groups[1].Keys)
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
		t.Errorf("info cells should be StateInfo, got %s / %s", row.Cells["a"].State, row.Cells["b"].State)
	}
	if row.Cells["a"].Version != "6" || row.Cells["b"].Version != "12" {
		t.Errorf("info cells should keep their values, got %q / %q", row.Cells["a"].Version, row.Cells["b"].Version)
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
		t.Errorf("a/b should match: %s %s", row.Cells["a"].State, row.Cells["b"].State)
	}
	if row.Cells["c"].State != StateMismatch {
		t.Errorf("c should mismatch, got %s", row.Cells["c"].State)
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
		t.Errorf("30d should be crit, got %s", row.Cells["crit"].State)
	}
	if row.Cells["warn"].State != StateExpiryWarn {
		t.Errorf("90d should be warn, got %s", row.Cells["warn"].State)
	}
	if row.Cells["ok"].State != StateExpiryOK {
		t.Errorf("200d should be ok, got %s", row.Cells["ok"].State)
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
		t.Errorf("cluster a should be stale (2h old, threshold 1h)")
	}
	if m.Clusters[1].Stale != false {
		t.Errorf("cluster b should be fresh")
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
		t.Errorf("b openshift should be leader, got %s", ocp.Cells["b"].State)
	}
	if ocp.Cells["a"].State != StateBehind || ocp.Cells["a"].GapKind != "minor" {
		t.Errorf("a openshift should be behind/minor, got %s/%s", ocp.Cells["a"].State, ocp.Cells["a"].GapKind)
	}

	px := rows["portworx-csi"]
	if px.Cells["b"].State != StateUnknown {
		t.Errorf("b portworx should be unknown (unparseable), got %s", px.Cells["b"].State)
	}
	if px.Cells["a"].State != StateNotInstalled {
		t.Errorf("a portworx should be not_installed, got %s", px.Cells["a"].State)
	}
}
