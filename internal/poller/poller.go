package poller

import (
	"context"
	"log/slog"
	"time"

	"github.com/mwaddip/blockhost-monitor/internal/collector"
	"github.com/mwaddip/blockhost-monitor/internal/vmdb"
)

type Options struct {
	Collector   *collector.Collector
	Store       *Store
	DiscoverVMs func() ([]vmdb.VM, error)
	Budget      time.Duration
	MinInterval time.Duration
	Log         *slog.Logger
}

type Poller struct {
	collector   *collector.Collector
	store       *Store
	discoverVMs func() ([]vmdb.VM, error)
	budget      time.Duration
	minInterval time.Duration
	log         *slog.Logger
}

func New(opts Options) *Poller {
	return &Poller{
		collector:   opts.Collector,
		store:       opts.Store,
		discoverVMs: opts.DiscoverVMs,
		budget:      opts.Budget,
		minInterval: opts.MinInterval,
		log:         opts.Log,
	}
}

// RunOnce executes a single poll cycle: discover VMs, collect metrics, store.
// Logs a warning if the cycle exceeds the configured budget.
func (p *Poller) RunOnce(ctx context.Context) {
	cycleStart := time.Now()

	vms, err := p.discoverVMs()
	if err != nil {
		p.log.Error("vm discovery failed", "error", err)
		return
	}
	if len(vms) == 0 {
		return
	}

	for _, vm := range vms {
		if ctx.Err() != nil {
			return
		}
		m, dur, err := p.collector.Collect(ctx, vm.Name)
		if err != nil {
			p.log.Error("metrics collection failed", "vm", vm.Name, "error", err)
			continue
		}
		p.store.Record(&Sample{
			VMName:    vm.Name,
			Metrics:   m,
			Timestamp: time.Now(),
			Duration:  dur,
		})
	}

	elapsed := time.Since(cycleStart)
	if elapsed > p.budget {
		p.log.Warn("poll cycle exceeded budget",
			"elapsed_ms", elapsed.Milliseconds(),
			"budget_ms", p.budget.Milliseconds(),
			"vm_count", len(vms),
		)
	}
}

// Run enters the main poll loop. Calls RunOnce each cycle, then sleeps
// until minInterval has elapsed since the cycle started. Returns when
// the context is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	for {
		cycleStart := time.Now()
		p.RunOnce(ctx)

		elapsed := time.Since(cycleStart)
		sleep := p.minInterval - elapsed
		if sleep <= 0 {
			p.log.Warn("poll cycle took longer than min interval",
				"elapsed_ms", elapsed.Milliseconds(),
				"min_interval_ms", p.minInterval.Milliseconds(),
			)
			sleep = 0
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
	}
}
