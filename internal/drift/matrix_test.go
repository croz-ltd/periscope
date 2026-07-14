package drift

import (
	"testing"
	"time"

	"github.com/croz-ltd/cluster-comparator/internal/model"
)

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

	m := Build(snaps, now, time.Hour)

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
