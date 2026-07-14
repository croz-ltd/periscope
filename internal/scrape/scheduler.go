// Package scrape periodically polls every cluster and stores a snapshot.
package scrape

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/croz-ltd/periscope/internal/cluster"
	"github.com/croz-ltd/periscope/internal/extract"
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
	targets, err := s.Registry.Discover(ctx)
	if err != nil {
		log.Printf("scrape: cluster discovery: %v", err)
	}
	conc := s.Concurrency
	if conc <= 0 {
		conc = 4
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(t cluster.Target) {
			defer wg.Done()
			defer func() { <-sem }()
			snap := s.scrapeOne(ctx, t)
			if err := s.Store.SaveSnapshot(snap); err != nil {
				log.Printf("scrape: save %s: %v", t.Name, err)
			}
		}(t)
	}
	wg.Wait()
}

func (s *Scheduler) scrapeOne(ctx context.Context, t cluster.Target) model.Snapshot {
	snap := model.Snapshot{Cluster: t.Name, Time: time.Now(), OK: true}

	cctx := ctx
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}

	typed, err := kubernetes.NewForConfig(t.Config)
	if err != nil {
		snap.OK, snap.Error = false, err.Error()
		return snap
	}
	dyn, err := dynamic.NewForConfig(t.Config)
	if err != nil {
		snap.OK, snap.Error = false, err.Error()
		return snap
	}
	clients := &extract.Clients{Typed: typed, Dynamic: dyn, Host: t.Config.Host}

	var errs []string
	for _, e := range s.Extractors {
		comps, err := e.Extract(cctx, clients)
		if err != nil {
			// Per-extractor failure (missing CRD, forbidden, etc.) is recorded but
			// does not fail the whole cluster — other components still get through.
			errs = append(errs, e.Key()+": "+err.Error())
			continue
		}
		snap.Components = append(snap.Components, comps...)
	}
	if len(errs) > 0 {
		snap.Error = strings.Join(errs, "; ")
	}
	return snap
}
