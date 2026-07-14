package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/croz-ltd/cluster-comparator/internal/model"
)

// TestRoundTripPreservesGroupCompare guards against dropping fields on the
// SQLite round-trip (group/compare were lost once because the columns were
// missing from the INSERT/SELECT).
func TestRoundTripPreservesGroupCompare(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	in := model.Snapshot{Cluster: "a", Time: time.Unix(1000, 0), OK: true, Components: []model.Component{
		{Key: "cert-api", Name: "API", Group: model.GroupCert, Compare: model.CompareExpiry, Kind: "cert", Version: "2026-01-02T00:00:00Z"},
		{Key: "chan", Name: "Channel", Group: model.GroupOpenShift, Compare: model.CompareMatch, Version: "stable-4.14", Extra: map[string]string{"x": "y"}},
	}}
	if err := st.SaveSnapshot(in); err != nil {
		t.Fatal(err)
	}

	snaps, err := st.LatestSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || len(snaps[0].Components) != 2 {
		t.Fatalf("unexpected shape: %+v", snaps)
	}
	got := map[string]model.Component{}
	for _, c := range snaps[0].Components {
		got[c.Key] = c
	}
	if c := got["cert-api"]; c.Group != model.GroupCert || c.Compare != model.CompareExpiry {
		t.Errorf("cert-api lost group/compare: group=%q compare=%q", c.Group, c.Compare)
	}
	if c := got["chan"]; c.Group != model.GroupOpenShift || c.Compare != model.CompareMatch || c.Extra["x"] != "y" {
		t.Errorf("chan lost fields: %+v", c)
	}
}
