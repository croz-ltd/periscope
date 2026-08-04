package store

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/croz-ltd/periscope/internal/model"
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

// TestConcurrentSaveAndRead guards against SQLITE_BUSY: the scheduler saves one
// snapshot per cluster in parallel while the API reads, and every save must
// land instead of losing a scrape to the database lock.
func TestConcurrentSaveAndRead(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const clusters = 8
	var wg sync.WaitGroup
	errs := make(chan error, clusters*2)
	for i := range clusters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snap := model.Snapshot{Cluster: fmt.Sprintf("c%d", i), Time: time.Unix(1000, 0), OK: true}
			for j := range 50 {
				snap.Components = append(snap.Components, model.Component{Key: fmt.Sprintf("k%d", j), Version: "1"})
			}
			if err := st.SaveSnapshot(snap); err != nil {
				errs <- fmt.Errorf("save: %w", err)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.LatestSnapshots(); err != nil {
				errs <- fmt.Errorf("read: %w", err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	snaps, err := st.LatestSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != clusters {
		t.Fatalf("got %d snapshots, want %d", len(snaps), clusters)
	}
}
