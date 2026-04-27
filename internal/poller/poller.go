package poller

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"runtime"
	"sync"
	"time"

	"github.com/mwaddip/blockhost-monitor/internal/collector"
	"github.com/mwaddip/blockhost-monitor/internal/vmdb"
)

const (
	discoveryBackoffBase = 1 * time.Second
	discoveryBackoffMax  = 60 * time.Second
)

type Options struct {
	Collector   *collector.Collector
	Store       *Store
	DiscoverVMs func() ([]vmdb.VM, error)
	Budget      time.Duration
	MinInterval time.Duration
	// Concurrency caps in-flight metric collections per cycle.
	// Zero or negative defaults to runtime.GOMAXPROCS(0).
	Concurrency int
	Log         *slog.Logger
}

type Poller struct {
	collector   *collector.Collector
	store       *Store
	discoverVMs func() ([]vmdb.VM, error)
	budget      time.Duration
	minInterval time.Duration
	concurrency int
	log         *slog.Logger
}

func New(opts Options) *Poller {
	c := opts.Concurrency
	if c <= 0 {
		c = runtime.GOMAXPROCS(0)
	}
	return &Poller{
		collector:   opts.Collector,
		store:       opts.Store,
		discoverVMs: opts.DiscoverVMs,
		budget:      opts.Budget,
		minInterval: opts.MinInterval,
		concurrency: c,
		log:         opts.Log,
	}
}

// RunOnce executes a single poll cycle: discover VMs, collect metrics
// concurrently up to p.concurrency, store results, and prune stored samples
// for VMs that no longer exist. Returns the wall-clock duration of the cycle
// and an error only if VM discovery fails — per-VM collection failures are
// logged and skipped.
func (p *Poller) RunOnce(ctx context.Context) (time.Duration, error) {
	cycleStart := time.Now()

	vms, err := p.discoverVMs()
	if err != nil {
		p.log.Error("vm discovery failed", "error", err)
		return time.Since(cycleStart), err
	}

	active := make(map[string]struct{}, len(vms))
	for _, vm := range vms {
		active[vm.Name] = struct{}{}
	}

	if len(vms) > 0 {
		sem := make(chan struct{}, p.concurrency)
		var wg sync.WaitGroup
		for _, vm := range vms {
			if ctx.Err() != nil {
				break
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(name string) {
				defer wg.Done()
				defer func() { <-sem }()
				m, dur, err := p.collector.Collect(ctx, name)
				if err != nil {
					p.log.Error("metrics collection failed", "vm", name, "error", err)
					return
				}
				p.store.Record(&Sample{
					VMName:    name,
					Metrics:   m,
					Timestamp: time.Now(),
					Duration:  dur,
				})
			}(vm.Name)
		}
		wg.Wait()
	}

	p.store.Prune(active)

	elapsed := time.Since(cycleStart)
	p.log.Info("poll cycle complete",
		"elapsed_ms", elapsed.Milliseconds(),
		"budget_ms", p.budget.Milliseconds(),
		"vm_count", len(vms),
	)
	return elapsed, nil
}

// Run enters the main poll loop. Calls RunOnce each cycle, then sleeps
// until minInterval has elapsed since the cycle started. On consecutive
// discovery failures, sleeps are extended with exponential backoff capped
// at discoveryBackoffMax. Returns when the context is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	discoveryFailures := 0
	for {
		elapsed, err := p.RunOnce(ctx)

		var sleep time.Duration
		if err != nil {
			discoveryFailures++
			sleep = discoveryBackoff(discoveryFailures)
			p.log.Warn("backing off after discovery failure",
				"consecutive_failures", discoveryFailures,
				"sleep_ms", sleep.Milliseconds(),
			)
		} else {
			discoveryFailures = 0
			sleep = p.minInterval - elapsed
		}

		if sleep > 0 {
			timer := time.NewTimer(sleep)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		} else if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// discoveryBackoff returns an exponentially increasing delay with up to
// 25% jitter, capped at discoveryBackoffMax. failures must be >= 1.
func discoveryBackoff(failures int) time.Duration {
	n := failures - 1
	if n > 6 {
		n = 6
	}
	delay := discoveryBackoffBase << n
	if delay > discoveryBackoffMax {
		delay = discoveryBackoffMax
	}
	jitter := time.Duration(rand.Int64N(int64(delay) / 4))
	return delay + jitter
}
