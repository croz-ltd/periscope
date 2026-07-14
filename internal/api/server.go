// Package api serves the REST endpoints the React UI consumes, plus export and
// Prometheus surfaces.
package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/croz-ltd/cluster-comparator/internal/drift"
	"github.com/croz-ltd/cluster-comparator/internal/scrape"
	"github.com/croz-ltd/cluster-comparator/internal/store"
	"github.com/croz-ltd/cluster-comparator/web"
)

type Server struct {
	Store      *store.Store
	Scheduler  *scrape.Scheduler
	StaleAfter time.Duration
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/matrix", s.handleMatrix)
	mux.HandleFunc("/api/export.json", s.handleExportJSON)
	mux.HandleFunc("/api/export.csv", s.handleExportCSV)
	mux.HandleFunc("/api/refresh", s.handleRefresh)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.Handle("/", web.Handler())
	return mux
}

func (s *Server) matrix() (drift.Matrix, error) {
	snaps, err := s.Store.LatestSnapshots()
	if err != nil {
		return drift.Matrix{}, err
	}
	return drift.Build(snaps, time.Now(), s.StaleAfter), nil
}

func (s *Server) handleMatrix(w http.ResponseWriter, _ *http.Request) {
	m, err := s.matrix()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, m)
}

func (s *Server) handleExportJSON(w http.ResponseWriter, _ *http.Request) {
	m, err := s.matrix()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="cluster-comparator.json"`)
	writeJSON(w, m)
}

func (s *Server) handleExportCSV(w http.ResponseWriter, _ *http.Request) {
	m, err := s.matrix()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="cluster-comparator.csv"`)

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
			cell := row.Cells[c.Name]
			switch cell.State {
			case drift.StateNotInstalled:
				rec = append(rec, "-")
			default:
				rec = append(rec, fmt.Sprintf("%s (%s)", cell.Version, cell.State))
			}
		}
		_ = cw.Write(rec)
	}
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if s.Scheduler == nil {
		http.Error(w, "no scheduler", http.StatusServiceUnavailable)
		return
	}
	go s.Scheduler.ScrapeAll(context.Background())
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"refresh started"}`))
}

// handleMetrics emits a minimal Prometheus text exposition (no client dep):
// per-cell drift severity and staleness, so alerts can fire on intra-fleet drift.
func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	m, err := s.matrix()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	fmt.Fprintln(w, "# HELP cluster_comparator_component_drift_severity How far a component is behind the fleet leader (0 = leader).")
	fmt.Fprintln(w, "# TYPE cluster_comparator_component_drift_severity gauge")
	for _, row := range m.Rows {
		clusters := make([]string, 0, len(row.Cells))
		for c := range row.Cells {
			clusters = append(clusters, c)
		}
		sort.Strings(clusters)
		for _, c := range clusters {
			cell := row.Cells[c]
			if cell.State == drift.StateNotInstalled {
				continue
			}
			fmt.Fprintf(w, "cluster_comparator_component_drift_severity{cluster=%q,component=%q,state=%q} %d\n",
				c, row.Key, cell.State, cell.Severity)
		}
	}

	fmt.Fprintln(w, "# HELP cluster_comparator_cluster_stale 1 if the cluster snapshot is stale.")
	fmt.Fprintln(w, "# TYPE cluster_comparator_cluster_stale gauge")
	for _, c := range m.Clusters {
		stale := 0
		if c.Stale {
			stale = 1
		}
		fmt.Fprintf(w, "cluster_comparator_cluster_stale{cluster=%q} %d\n", c.Name, stale)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
