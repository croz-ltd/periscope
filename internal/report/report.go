// Package report renders the drift matrix as a plain-text table for the CLI.
package report

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/croz-ltd/periscope/internal/drift"
	"github.com/croz-ltd/periscope/internal/store"
)

// Print writes the latest matrix as an aligned table to w.
func Print(w io.Writer, st *store.Store, staleAfter time.Duration) error {
	snaps, err := st.LatestSnapshots()
	if err != nil {
		return err
	}
	m := drift.Build(snaps, time.Now(), staleAfter)

	if len(m.Clusters) == 0 {
		fmt.Fprintln(w, "no clusters scraped yet")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	defer tw.Flush()

	header := "COMPONENT\tLEADER"
	for _, c := range m.Clusters {
		name := c.Name
		if c.Stale {
			name += "*"
		}
		header += "\t" + name
	}
	fmt.Fprintln(tw, header)

	for _, row := range m.Rows {
		line := row.Name + "\t" + row.Leader
		for _, c := range m.Clusters {
			line += "\t" + cellText(row.Cells[c.Name])
		}
		fmt.Fprintln(tw, line)
	}

	if hasStale(m) {
		fmt.Fprintln(tw, "\n* = stale (last scrape older than threshold)")
	}
	return nil
}

func cellText(c drift.Cell) string {
	switch c.State {
	case drift.StateNotInstalled:
		return "-"
	case drift.StateLeader:
		return c.Version + " =" // at leader
	case drift.StateUnknown:
		return c.Version + " ?"
	case drift.StateBehind:
		return fmt.Sprintf("%s v(%s)", c.Version, c.GapKind) // behind
	default:
		return c.Version
	}
}

func hasStale(m drift.Matrix) bool {
	for _, c := range m.Clusters {
		if c.Stale {
			return true
		}
	}
	return false
}
