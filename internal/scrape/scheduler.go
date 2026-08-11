// Package scrape periodically polls every cluster and stores a snapshot.
package scrape

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/croz-ltd/periscope/internal/cluster"
	"github.com/croz-ltd/periscope/internal/extract"
	"github.com/croz-ltd/periscope/internal/logging"
	"github.com/croz-ltd/periscope/internal/model"
	"github.com/croz-ltd/periscope/internal/store"
)

type Scheduler struct {
	Registry    *cluster.Registry
	Store       *store.Store
	Extractors  []extract.Extractor
	Interval    time.Duration
	Timeout     time.Duration // per-cluster
	Concurrency int
}

// Run scrapes immediately, then on every Interval tick until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	s.ScrapeAll(ctx)
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.ScrapeAll(ctx)
		}
	}
}

// ScrapeAll polls every discovered cluster concurrently and persists each result
// independently, so one slow or down cluster never blocks the others.
func (s *Scheduler) ScrapeAll(ctx context.Context) {
	log := logging.For("scrape")
	started := time.Now()

	targets, err := s.Registry.Discover(ctx)
	if err != nil {
		log.Error("cluster discovery failed", "error", err)
	}
	if len(targets) == 0 {
		log.Warn("no clusters to scrape: no labeled Secret matched",
			"namespace", s.Registry.Namespace, "label", s.Registry.LabelKey+"="+s.Registry.LabelVal)
		return
	}

	conc := s.Concurrency
	if conc <= 0 {
		conc = 4
	}
	log.Info("scrape cycle starting", "clusters", len(targets), "concurrency", conc)

	var degraded, failed atomic.Int64
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(t cluster.Target) {
			defer wg.Done()
			defer func() { <-sem }()
			snap := s.scrapeOne(ctx, t)
			switch {
			case !snap.OK:
				failed.Add(1)
			case snap.Error != "":
				degraded.Add(1)
			}
			if err := s.Store.SaveSnapshot(snap); err != nil {
				log.Error("cannot save snapshot", "cluster", t.Name, "error", err)
			}
		}(t)
	}
	wg.Wait()

	// One line per cycle carries the whole fleet's health, so a scrape that has
	// been quietly degrading for a week is visible without turning on debug.
	log.Info("scrape cycle finished",
		"clusters", len(targets),
		"degraded", degraded.Load(),
		"unreachable", failed.Load(),
		"duration", time.Since(started).Round(time.Millisecond))
}

func (s *Scheduler) scrapeOne(ctx context.Context, t cluster.Target) model.Snapshot {
	log := logging.For("scrape").With("cluster", t.Name)
	started := time.Now()
	snap := model.Snapshot{Cluster: t.Name, Time: started, OK: true, Order: t.Order}

	cctx := ctx
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	log.Debug("scraping cluster", "host", t.Config.Host, "timeout", s.Timeout)

	typed, err := kubernetes.NewForConfig(t.Config)
	if err != nil {
		log.Error("cannot build a client for the cluster", "error", err)
		snap.OK, snap.Error = false, err.Error()
		return snap
	}
	dyn, err := dynamic.NewForConfig(t.Config)
	if err != nil {
		log.Error("cannot build a dynamic client for the cluster", "error", err)
		snap.OK, snap.Error = false, err.Error()
		return snap
	}
	clients := &extract.Clients{Typed: typed, Dynamic: dyn, Host: t.Config.Host}

	var errs []string
	for _, e := range s.Extractors {
		at := time.Now()
		comps, err := e.Extract(cctx, clients)
		took := time.Since(at).Round(time.Millisecond)
		if err != nil {
			// Per-extractor failure (missing CRD or forbidden) is recorded but
			// does not fail the whole cluster, other components still get through.
			// Extractors share one deadline and run in order, so the first of
			// these is usually the one worth reading: the rest are collateral.
			log.Warn("extractor failed", "extractor", e.Key(), "duration", took,
				"remaining", remainingIn(cctx), "error", err)
			errs = append(errs, e.Key()+": "+err.Error())
			continue
		}
		log.Debug("extractor finished", "extractor", e.Key(), "components", len(comps), "duration", took)
		snap.Components = append(snap.Components, comps...)
	}
	if len(errs) > 0 {
		snap.Error = strings.Join(errs, "; ")
	}

	// A cluster that reports fewer components than it used to is the shape of
	// the deadline expiring mid-list, so both numbers are on the same line.
	level := slog.LevelInfo
	if len(errs) > 0 {
		level = slog.LevelWarn
	}
	log.Log(ctx, level, "cluster scraped",
		"components", len(snap.Components),
		"extractorsFailed", len(errs),
		"duration", time.Since(started).Round(time.Millisecond))
	return snap
}

// remainingIn reports how much of the per-cluster budget was left when an
// extractor gave up, which is what separates "this cluster is slow" from "this
// extractor is broken".
func remainingIn(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	return time.Until(deadline).Round(time.Millisecond)
}
