package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"slices"

	"github.com/croz-ltd/periscope/internal/drift"
	"github.com/croz-ltd/periscope/internal/logging"
)

// An export is the matrix a reader is looking at. With no parameters it is the
// whole fleet, which is what a script usually wants. The UI narrows it the way
// the screen is narrowed:
//
//   - page=compare|statistics keeps the rows of that page, in its group order
//   - cluster=<name>, repeated, keeps those columns, in that order
//   - at=<RFC3339> is the matrix at that time, as elsewhere
func (s *Server) exportMatrix(w http.ResponseWriter, r *http.Request) (drift.Matrix, bool) {
	m, err := s.matrix(r.Context(), parseAt(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return m, false
	}
	q := r.URL.Query()
	m, err = narrowMatrix(m, q.Get("page"), q["cluster"])
	if err != nil {
		logging.For("api").Debug("refusing an export", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return m, false
	}
	return m, true
}

func narrowMatrix(m drift.Matrix, page string, clusters []string) (drift.Matrix, error) {
	if page != "" {
		i := slices.IndexFunc(m.Pages, func(p drift.PageView) bool { return p.ID == page })
		if i < 0 {
			return m, fmt.Errorf("no page named %q", page)
		}
		m.Pages = []drift.PageView{m.Pages[i]}
		byKey := make(map[string]drift.Row, len(m.Rows))
		for _, row := range m.Rows {
			byKey[row.Key] = row
		}
		var rows []drift.Row
		for _, g := range m.Pages[0].Groups {
			for _, k := range g.Keys {
				if row, ok := byKey[k]; ok {
					row.Group = g.Title
					rows = append(rows, row)
				}
			}
		}
		m.Rows = rows
	}

	if len(clusters) > 0 {
		shown := make([]drift.ClusterInfo, 0, len(clusters))
		for _, name := range clusters {
			i := slices.IndexFunc(m.Clusters, func(c drift.ClusterInfo) bool { return c.Name == name })
			if i < 0 {
				return m, fmt.Errorf("no cluster named %q", name)
			}
			shown = append(shown, m.Clusters[i])
		}
		m.Clusters = shown
		rows := make([]drift.Row, len(m.Rows))
		for i, row := range m.Rows {
			cells := make(map[string]drift.Cell, len(clusters))
			for _, name := range clusters {
				if c, ok := row.Cells[name]; ok {
					cells[name] = c
				}
			}
			row.Cells = cells
			rows[i] = row
		}
		m.Rows = rows
	}
	return m, nil
}

// exportName says which page an export holds, so two downloads do not collide.
func exportName(r *http.Request, ext string) string {
	if page := r.URL.Query().Get("page"); page != "" {
		return "periscope-" + page + "." + ext
	}
	return "periscope." + ext
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	m, ok := s.exportMatrix(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+exportName(r, "json")+`"`)
	writeJSON(w, m)
}

func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	m, ok := s.exportMatrix(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+exportName(r, "csv")+`"`)

	cw := csv.NewWriter(w)
	defer cw.Flush()

	header := []string{"component", "kind", "leader"}
	for _, c := range m.Clusters {
		header = append(header, c.Name)
	}
	_ = cw.Write(header)

	for _, row := range m.Rows {
		rec := []string{row.Name, row.Kind, row.Leader}
		for _, c := range m.Clusters {
			rec = append(rec, csvCell(row.Cells[c.Name]))
		}
		_ = cw.Write(rec)
	}
}

// csvCell keeps the shape the export has always had, since scripts parse it.
func csvCell(c drift.Cell) string {
	if c.State == "" || c.State == drift.StateNotInstalled {
		return "-"
	}
	return fmt.Sprintf("%s (%s)", c.Version, c.State)
}
