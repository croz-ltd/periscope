// Command periscope serves the multi-cluster version-drift dashboard
// (default) or prints a one-off report.
//
//	periscope serve    # scrape on an interval + serve the UI/API
//	periscope report   # print the latest matrix from the store
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/croz-ltd/periscope/internal/api"
	"github.com/croz-ltd/periscope/internal/cluster"
	"github.com/croz-ltd/periscope/internal/config"
	"github.com/croz-ltd/periscope/internal/logging"
	"github.com/croz-ltd/periscope/internal/report"
	"github.com/croz-ltd/periscope/internal/scrape"
	"github.com/croz-ltd/periscope/internal/store"
	"github.com/croz-ltd/periscope/pkg/version"
)

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !isFlag(args[0]) {
		cmd, args = args[0], args[1:]
	}

	switch cmd {
	case "serve":
		serve(args)
	case "report":
		runReport(args)
	case "version", "--version", "-v":
		fmt.Println(version.Raw)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (want: serve, report, version)\n", cmd)
		os.Exit(2)
	}
}

func isFlag(s string) bool { return len(s) > 0 && s[0] == '-' }

type commonFlags struct {
	namespace  string
	labelKey   string
	labelVal   string
	db         string
	staleAfter time.Duration
	logLevel   string
	logFormat  string
}

func registerCommon(fs *flag.FlagSet) *commonFlags {
	c := &commonFlags{}
	fs.StringVar(&c.namespace, "namespace", defaultNamespace(), "hub namespace holding joined-cluster Secrets")
	fs.StringVar(&c.labelKey, "label-key", "periscope.io/cluster", "label key marking joined-cluster Secrets")
	fs.StringVar(&c.labelVal, "label-value", "true", "label value marking joined-cluster Secrets")
	fs.StringVar(&c.db, "db", envOr("DB_PATH", "/data/periscope.db"), "SQLite database path")
	fs.DurationVar(&c.staleAfter, "stale-after", 30*time.Minute, "mark a cluster stale if its last scrape is older than this")
	fs.StringVar(&c.logLevel, "log-level", envOr("LOG_LEVEL", "info"),
		"log verbosity: "+strings.Join(logging.LevelNames(), " | "))
	fs.StringVar(&c.logFormat, "log-format", envOr("LOG_FORMAT", logging.FormatText),
		"log output format: text | json")
	return c
}

// setupLogging installs the logger before anything else runs, so a bad level is
// reported plainly rather than through a logger that does not exist yet.
func (c *commonFlags) setupLogging() slog.Level {
	level, err := logging.Setup(c.logLevel, c.logFormat)
	if err != nil {
		fmt.Fprintln(os.Stderr, "periscope:", err)
		os.Exit(2)
	}
	return level
}

// fatal reports an unrecoverable startup failure and stops.
func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	c := registerCommon(fs)
	addr := fs.String("addr", envOr("LISTEN_ADDR", ":8080"), "HTTP listen address")
	interval := fs.Duration("interval", 10*time.Minute, "scrape interval")
	timeout := fs.Duration("timeout", 30*time.Second, "per-cluster scrape timeout")
	concurrency := fs.Int("concurrency", 4, "max clusters scraped in parallel")
	configPath := fs.String("config", envOr("CONFIG_PATH", ""), "optional extractor config file (see config/extractors.yaml)")
	_ = fs.Parse(args)

	level := c.setupLogging()
	slog.Info("starting periscope",
		"version", version.Raw,
		"namespace", c.namespace,
		"db", c.db,
		"interval", *interval,
		"timeout", *timeout,
		"concurrency", *concurrency,
		"staleAfter", c.staleAfter,
		"logLevel", level)

	st := mustOpenStore(c.db)
	defer st.Close()

	extractors, err := config.BuildExtractors(*configPath)
	if err != nil {
		fatal("cannot build extractors", "config", *configPath, "error", err)
	}
	keys := make([]string, 0, len(extractors))
	for _, e := range extractors {
		keys = append(keys, e.Key())
	}
	slog.Info("extractors ready", "count", len(extractors), "extractors", strings.Join(keys, ","), "config", *configPath)

	reg, err := cluster.NewRegistry(c.namespace, c.labelKey, c.labelVal)
	if err != nil {
		fatal("cannot reach the hub cluster", "namespace", c.namespace, "error", err)
	}
	sched := &scrape.Scheduler{
		Registry:    reg,
		Store:       st,
		Extractors:  extractors,
		Interval:    *interval,
		Timeout:     *timeout,
		Concurrency: *concurrency,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go sched.Run(ctx)

	srv := &api.Server{Store: st, Scheduler: sched, StaleAfter: c.staleAfter}
	httpSrv := &http.Server{Addr: *addr, Handler: srv.Handler()}

	go func() {
		<-ctx.Done()
		slog.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(sctx); err != nil {
			slog.Warn("http shutdown did not finish cleanly", "error", err)
		}
	}()

	slog.Info("listening", "addr", *addr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fatal("http server failed", "addr", *addr, "error", err)
	}
	slog.Info("stopped")
}

func runReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	c := registerCommon(fs)
	_ = fs.Parse(args)

	c.setupLogging()

	st := mustOpenStore(c.db)
	defer st.Close()

	if err := report.Print(os.Stdout, st, c.staleAfter); err != nil {
		fatal("cannot build the report", "error", err)
	}
}

func mustOpenStore(path string) *store.Store {
	st, err := store.Open(path)
	if err != nil {
		fatal("cannot open the store", "path", path, "error", err)
	}
	return st
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// defaultNamespace resolves the namespace to discover joined-cluster Secrets in.
// In a pod this is the pod's OWN namespace (POD_NAMESPACE via downward API, or the
// service-account namespace file), where the join Secrets and RBAC live. Falls
// back to "periscope" off-cluster.
func defaultNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}
	if b, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if ns := strings.TrimSpace(string(b)); ns != "" {
			return ns
		}
	}
	return "periscope"
}
