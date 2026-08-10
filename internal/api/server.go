// Package api serves the REST endpoints the React UI consumes, plus export and
// Prometheus surfaces.
package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/croz-ltd/periscope/internal/drift"
	"github.com/croz-ltd/periscope/internal/logging"
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
	mux.HandleFunc("/api/changes", s.handleChanges)
	mux.HandleFunc("/api/changes/calendar", s.handleChangeCalendar)
	mux.HandleFunc("/api/export.json", s.handleExportJSON)
	mux.HandleFunc("/api/export.csv", s.handleExportCSV)
	mux.HandleFunc("/api/refresh", s.handleRefresh)
	mux.HandleFunc("/api/user", s.handleUser)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.Handle("/", web.Handler())
	return logRequests(mux)
}

// logRequests records every request once it has finished. Served pages are
// debug-only (the UI polls, and an access log at info would drown the scrape
// lines), while a server error is always worth seeing.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		log := logging.For("api")
		level := slog.LevelDebug
		switch {
		case rec.status >= http.StatusInternalServerError:
			level = slog.LevelError
		case rec.status >= http.StatusBadRequest:
			level = slog.LevelWarn
		}
		log.Log(r.Context(), level, "request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"status", rec.status,
			"duration", time.Since(started).Round(time.Millisecond))
	})
}

// statusRecorder remembers the status code so it can be logged; without it
// every response looks like a 200.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.written {
		r.status, r.written = status, true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.written = true // an implicit 200
	return r.ResponseWriter.Write(b)
}

// matrix builds the current matrix, or the matrix as it stood at "at" when that
// is non-zero. Time travel reuses the whole pipeline: only the snapshots differ,
// and staleness is judged against the moment being viewed, so a cluster that was
// fresh back then does not read as stale now.
func (s *Server) matrix(ctx context.Context, at time.Time) (drift.Matrix, error) {
	now := time.Now()
	snaps, err := s.Store.LatestSnapshots()
	if !at.IsZero() {
		now = at
		snaps, err = s.Store.SnapshotsAt(at)
	}
	if err != nil {
		return drift.Matrix{}, err
	}
	cfg, warn := s.groupConfig(ctx)
	m := drift.Build(snaps, now, s.StaleAfter, cfg)
	m.Warning = warn
	if !at.IsZero() {
		m.At = &at
	}
	return m, nil
}

// parseAt reads the "at" query parameter (RFC3339) used for time travel. An
// unparseable value is ignored rather than failing the request, so a stale
// bookmark still shows the live matrix.
func parseAt(r *http.Request) time.Time {
	raw := r.URL.Query().Get("at")
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

// groupConfig loads custom grouping from the periscope-groups ConfigMap. Returns
// (nil, "") when absent (built-in grouping), or (nil, warning) on read/parse error.
func (s *Server) groupConfig(ctx context.Context) (*drift.GroupConfig, string) {
	if s.Scheduler == nil || s.Scheduler.Registry == nil {
		return nil, ""
	}
	log := logging.For("api")
	data, err := s.Scheduler.Registry.ConfigMapData(ctx, groupConfigMapName)
	if err != nil {
		log.Warn("cannot read the grouping ConfigMap", "configMap", groupConfigMapName, "error", err)
		return nil, "custom grouping unavailable: " + err.Error()
	}
	raw := strings.TrimSpace(data[groupConfigMapKey])
	if raw == "" {
		log.Debug("no custom grouping, using the built-in sections", "configMap", groupConfigMapName)
		return nil, ""
	}
	var cfg drift.GroupConfig
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		// The UI shows this too, but a malformed ConfigMap is worth a log line:
		// the matrix silently falls back to the built-in grouping.
		log.Warn("ignoring invalid grouping ConfigMap",
			"configMap", groupConfigMapName, "key", groupConfigMapKey, "error", err)
		return nil, "custom grouping ignored (invalid " + groupConfigMapKey + "): " + err.Error()
	}
	log.Debug("custom grouping loaded", "compareGroups", len(cfg.Compare),
		"statisticsGroups", len(cfg.Statistics), "hidden", len(cfg.Hidden))
	return &cfg, ""
}

func (s *Server) handleMatrix(w http.ResponseWriter, r *http.Request) {
	m, err := s.matrix(r.Context(), parseAt(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, m)
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	m, err := s.matrix(r.Context(), parseAt(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="periscope.json"`)
	writeJSON(w, m)
}

func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	m, err := s.matrix(r.Context(), parseAt(r))
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

// defaultChangeLimit bounds an unfiltered feed request. The feed is read newest
// first, so a bound loses only the distant past, and the calendar is how you
// reach that anyway.
const defaultChangeLimit = 500

// handleChanges serves the change feed, newest first, optionally narrowed to a
// time range and a cluster.
func (s *Server) handleChanges(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := store.ChangeQuery{
		From:    parseTimeParam(q.Get("from")),
		To:      parseTimeParam(q.Get("to")),
		Cluster: q.Get("cluster"),
		Limit:   defaultChangeLimit,
		// Counter updates are dropped here rather than by the caller, so the
		// limit is spent on rows that will actually be shown.
		ExcludeCounters: q.Get("counters") == "false",
	}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		query.Limit = n
	}

	changes, err := s.Store.Changes(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	body := map[string]any{"changes": changes}
	if query.ExcludeCounters {
		// Say how many were left out, so "nothing changed" can be told apart
		// from "nothing but counters changed". Count over the window actually
		// returned: on an open-ended request the answer would otherwise be
		// every counter update ever recorded, next to a page holding a day.
		counted := query
		if counted.From.IsZero() && len(changes) > 0 {
			counted.From = changes[len(changes)-1].Time
		}
		hidden, err := s.Store.CountCounters(counted)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body["hiddenCounters"] = hidden
	}
	writeJSON(w, body)
}

// handleChangeCalendar serves per-day change counts so the calendar can mark
// the days something happened, plus the span history covers so the calendar
// knows which months are worth offering.
func (s *Server) handleChangeCalendar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to := parseTimeParam(q.Get("from")), parseTimeParam(q.Get("to"))
	if from.IsZero() {
		from = time.Now().AddDate(-1, 0, 0)
	}
	if to.IsZero() {
		to = time.Now()
	}

	days, err := s.Store.ChangeDays(from, to, tzOffsetLocation(q.Get("tzOffset")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	first, last, err := s.Store.Span()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	body := map[string]any{"days": days}
	if !first.IsZero() {
		body["first"], body["last"] = first, last
	}
	writeJSON(w, body)
}

// tzOffsetLocation turns the browser's UTC offset in minutes (east of UTC, the
// negation of JavaScript's getTimezoneOffset) into a location. Days are bucketed
// with it so the calendar agrees with the reader's clock. An offset is used
// rather than a zone name because the runtime image carries no tzdata.
func tzOffsetLocation(raw string) *time.Location {
	mins, err := strconv.Atoi(raw)
	if err != nil || mins < -12*60 || mins > 14*60 {
		return time.UTC
	}
	return time.FixedZone("browser", mins*60)
}

// parseTimeParam reads an RFC3339 query parameter, zero when absent or invalid.
func parseTimeParam(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
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
	logging.For("api").Info("refresh requested, scraping now")
	go s.Scheduler.ScrapeAll(context.Background())
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"refresh started"}`))
}

// handleMetrics emits a minimal Prometheus text exposition (no client dep):
// per-cell drift severity and staleness, so alerts can fire on intra-fleet drift.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	m, err := s.matrix(r.Context(), time.Time{})
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
