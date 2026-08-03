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
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/croz-ltd/periscope/internal/api"
	"github.com/croz-ltd/periscope/internal/cluster"
	"github.com/croz-ltd/periscope/internal/config"
	"github.com/croz-ltd/periscope/internal/report"
	"github.com/croz-ltd/periscope/internal/scrape"
	"github.com/croz-ltd/periscope/internal/store"
	"github.com/croz-ltd/periscope/pkg/version"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("periscope: ")

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
}

func registerCommon(fs *flag.FlagSet) *commonFlags {
	c := &commonFlags{}
	fs.StringVar(&c.namespace, "namespace", defaultNamespace(), "hub namespace holding joined-cluster Secrets")
	fs.StringVar(&c.labelKey, "label-key", "periscope.io/cluster", "label key marking joined-cluster Secrets")
	fs.StringVar(&c.labelVal, "label-value", "true", "label value marking joined-cluster Secrets")
	fs.StringVar(&c.db, "db", envOr("DB_PATH", "/data/periscope.db"), "SQLite database path")
	fs.DurationVar(&c.staleAfter, "stale-after", 30*time.Minute, "mark a cluster stale if its last scrape is older than this")
	return c
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

	st := mustOpenStore(c.db)
	defer st.Close()

	extractors, err := config.BuildExtractors(*configPath)
	if err != nil {
		log.Fatalf("extractors: %v", err)
	}
	reg, err := cluster.NewRegistry(c.namespace, c.labelKey, c.labelVal)
	if err != nil {
		log.Fatalf("registry: %v", err)
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
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(sctx)
	}()

	log.Printf("periscope %s listening on %s (namespace=%s, interval=%s)", version.Raw, *addr, c.namespace, *interval)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http: %v", err)
	}
}

func runReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	c := registerCommon(fs)
	_ = fs.Parse(args)

	st := mustOpenStore(c.db)
	defer st.Close()

	if err := report.Print(os.Stdout, st, c.staleAfter); err != nil {
		log.Fatalf("report: %v", err)
	}
}

func mustOpenStore(path string) *store.Store {
	st, err := store.Open(path)
	if err != nil {
		log.Fatalf("open store %q: %v", path, err)
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
