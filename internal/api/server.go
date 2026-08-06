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
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/croz-ltd/periscope/internal/drift"
	"github.com/croz-ltd/periscope/internal/scrape"
	"github.com/croz-ltd/periscope/internal/store"
	"github.com/croz-ltd/periscope/pkg/version"
	"github.com/croz-ltd/periscope/web"
)

// Custom grouping is read from this ConfigMap / key in the hub namespace.
const (
	groupConfigMapName = "periscope-groups"
	groupConfigMapKey  = "groups.yaml"
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
	mux.HandleFunc("/api/user", s.handleUser)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.Handle("/", web.Handler())
	return mux
}

func (s *Server) matrix(ctx context.Context) (drift.Matrix, error) {
	snaps, err := s.Store.LatestSnapshots()
	if err != nil {
		return drift.Matrix{}, err
	}
	cfg, warn := s.groupConfig(ctx)
	m := drift.Build(snaps, time.Now(), s.StaleAfter, cfg)
	m.Warning = warn
	return m, nil
}

// groupConfig loads custom grouping from the periscope-groups ConfigMap. Returns
// (nil, "") when absent (built-in grouping), or (nil, warning) on read/parse error.
func (s *Server) groupConfig(ctx context.Context) (*drift.GroupConfig, string) {
	if s.Scheduler == nil || s.Scheduler.Registry == nil {
		return nil, ""
	}
	data, err := s.Scheduler.Registry.ConfigMapData(ctx, groupConfigMapName)
	if err != nil {
		return nil, "custom grouping unavailable: " + err.Error()
	}
	raw := strings.TrimSpace(data[groupConfigMapKey])
	if raw == "" {
		return nil, ""
	}
	var cfg drift.GroupConfig
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, "custom grouping ignored (invalid " + groupConfigMapKey + "): " + err.Error()
	}
	return &cfg, ""
}

func (s *Server) handleMatrix(w http.ResponseWriter, r *http.Request) {
	m, err := s.matrix(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, m)
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	m, err := s.matrix(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="periscope.json"`)
	writeJSON(w, m)
}

func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	m, err := s.matrix(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="periscope.csv"`)

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

// handleUser returns the signed-in user, taken from the headers oauth-proxy
// forwards (--pass-user-headers). Empty when running without the proxy (dev).
func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	name := firstNonEmpty(
		r.Header.Get("X-Forwarded-Preferred-Username"),
		r.Header.Get("X-Forwarded-User"),
		r.Header.Get("X-Forwarded-Email"),
	)
	writeJSON(w, map[string]string{
		"user":  name,
		"email": r.Header.Get("X-Forwarded-Email"),
	})
}

// handleVersion reports the version stamped into the binary at build time. The
// UI reads it from here rather than carrying its own number, because the web
// assets are built before the version is known (see the Dockerfile), so the
// dashboard always shows the version it is actually running.
func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]string{"version": version.Raw})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	m, err := s.matrix(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	fmt.Fprintln(w, "# HELP periscope_component_drift_severity How far a component is behind the fleet leader (0 = leader).")
	fmt.Fprintln(w, "# TYPE periscope_component_drift_severity gauge")
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
			fmt.Fprintf(w, "periscope_component_drift_severity{cluster=%q,component=%q,state=%q} %d\n",
				c, row.Key, cell.State, cell.Severity)
		}
	}

	fmt.Fprintln(w, "# HELP periscope_cluster_stale 1 if the cluster snapshot is stale.")
	fmt.Fprintln(w, "# TYPE periscope_cluster_stale gauge")
	for _, c := range m.Clusters {
		stale := 0
		if c.Stale {
			stale = 1
		}
		fmt.Fprintf(w, "periscope_cluster_stale{cluster=%q} %d\n", c.Name, stale)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
