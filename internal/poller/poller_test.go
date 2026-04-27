package poller

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mwaddip/blockhost-monitor/internal/collector"
	"github.com/mwaddip/blockhost-monitor/internal/vmdb"
)

func mockDiscoverer(vms []vmdb.VM) func() ([]vmdb.VM, error) {
	return func() ([]vmdb.VM, error) {
		return vms, nil
	}
}

func mockCollector(delay time.Duration) *collector.Collector {
	return collector.New("mock-metrics", func(ctx context.Context, name string, args ...string) ([]byte, error) {
		time.Sleep(delay)
		return []byte(`{"cpu_percent":10,"cpu_count":1,"memory_used_mb":512,"memory_total_mb":1024,"disk_used_mb":5000,"disk_total_mb":20480,"disk_read_iops":0,"disk_write_iops":0,"disk_read_bytes_sec":0,"disk_write_bytes_sec":0,"net_rx_bytes_sec":0,"net_tx_bytes_sec":0,"net_connections":10,"guest_agent_responsive":true,"uptime_seconds":3600,"state":"running"}`), nil
	})
}

func testLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestRunOnce_CollectsAllVMs(t *testing.T) {
	vms := []vmdb.VM{
		{Name: "web1", Status: "active"},
		{Name: "db1", Status: "active"},
	}

	store := NewStore()
	var logBuf bytes.Buffer
	p := New(Options{
		Collector:   mockCollector(0),
		Store:       store,
		DiscoverVMs: mockDiscoverer(vms),
		Budget:      500 * time.Millisecond,
		MinInterval: time.Second,
		Log:         testLogger(&logBuf),
	})

	p.RunOnce(context.Background())

	if store.Latest("web1") == nil {
		t.Error("missing sample for web1")
	}
	if store.Latest("db1") == nil {
		t.Error("missing sample for db1")
	}
}

func TestRunOnce_EmptyVMList(t *testing.T) {
	var collected atomic.Int32
	coll := collector.New("mock-metrics", func(ctx context.Context, name string, args ...string) ([]byte, error) {
		collected.Add(1)
		return []byte(`{}`), nil
	})

	store := NewStore()
	var logBuf bytes.Buffer
	p := New(Options{
		Collector:   coll,
		Store:       store,
		DiscoverVMs: mockDiscoverer(nil),
		Budget:      500 * time.Millisecond,
		MinInterval: time.Second,
		Log:         testLogger(&logBuf),
	})

	p.RunOnce(context.Background())

	if collected.Load() != 0 {
		t.Errorf("collector invoked %d times, want 0", collected.Load())
	}
}

func TestRunOnce_LogsCycleFacts(t *testing.T) {
	vms := []vmdb.VM{{Name: "slow1", Status: "active"}}

	store := NewStore()
	var logBuf bytes.Buffer
	p := New(Options{
		Collector:   mockCollector(50 * time.Millisecond),
		Store:       store,
		DiscoverVMs: mockDiscoverer(vms),
		Budget:      time.Millisecond,
		MinInterval: time.Second,
		Log:         testLogger(&logBuf),
	})

	elapsed, err := p.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("elapsed = %v, want >= 50ms", elapsed)
	}

	out := logBuf.Bytes()
	if !bytes.Contains(out, []byte("poll cycle complete")) {
		t.Error("expected cycle-complete log")
	}
	if !bytes.Contains(out, []byte("elapsed_ms=")) {
		t.Error("expected elapsed_ms field in cycle log")
	}
	if !bytes.Contains(out, []byte("budget_ms=")) {
		t.Error("expected budget_ms field in cycle log")
	}
}

func TestRun_StopsOnCancel(t *testing.T) {
	// Use an atomic counter to confirm at least one collection happened
	var collected atomic.Int32
	coll := collector.New("mock-metrics", func(ctx context.Context, name string, args ...string) ([]byte, error) {
		collected.Add(1)
		return []byte(`{"cpu_percent":10,"cpu_count":1,"memory_used_mb":512,"memory_total_mb":1024,"disk_used_mb":5000,"disk_total_mb":20480,"disk_read_iops":0,"disk_write_iops":0,"disk_read_bytes_sec":0,"disk_write_bytes_sec":0,"net_rx_bytes_sec":0,"net_tx_bytes_sec":0,"net_connections":10,"guest_agent_responsive":true,"uptime_seconds":3600,"state":"running"}`), nil
	})

	store := NewStore()
	var logBuf bytes.Buffer
	p := New(Options{
		Collector:   coll,
		Store:       store,
		DiscoverVMs: mockDiscoverer([]vmdb.VM{{Name: "vm1", Status: "active"}}),
		Budget:      time.Second,
		MinInterval: 50 * time.Millisecond,
		Log:         testLogger(&logBuf),
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Run in a goroutine, cancel after first collection
	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx)
	}()

	// Wait for at least one collection, then cancel
	deadline := time.After(2 * time.Second)
	for collected.Load() == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatal("timed out waiting for first collection")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancel()
	err := <-done

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if store.Latest("vm1") == nil {
		t.Error("expected at least one sample")
	}
}

func TestRunOnce_DiscoveryFailureReturnsError(t *testing.T) {
	want := errors.New("discovery boom")
	store := NewStore()
	var logBuf bytes.Buffer
	p := New(Options{
		Collector:   mockCollector(0),
		Store:       store,
		DiscoverVMs: func() ([]vmdb.VM, error) { return nil, want },
		Budget:      500 * time.Millisecond,
		MinInterval: time.Second,
		Log:         testLogger(&logBuf),
	})

	_, err := p.RunOnce(context.Background())
	if !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}

func TestDiscoveryBackoff_ExponentialAndCapped(t *testing.T) {
	// Verify the curve is monotonic up to the cap, then plateaus.
	// Jitter is 0–25%, so check ranges, not exact values.
	cases := []struct {
		failures  int
		minDelay  time.Duration
		maxDelay  time.Duration
	}{
		{1, 1 * time.Second, 1250 * time.Millisecond},
		{2, 2 * time.Second, 2500 * time.Millisecond},
		{4, 8 * time.Second, 10 * time.Second},
		{6, 32 * time.Second, 40 * time.Second},
		{10, 60 * time.Second, 75 * time.Second}, // capped
	}
	for _, tc := range cases {
		got := discoveryBackoff(tc.failures)
		if got < tc.minDelay || got > tc.maxDelay {
			t.Errorf("failures=%d: got %v, want in [%v, %v]", tc.failures, got, tc.minDelay, tc.maxDelay)
		}
	}
}

func TestRun_BacksOffOnDiscoveryFailure(t *testing.T) {
	// First few discoveries fail; eventually succeed. Verify backoff log
	// emitted with increasing failure counts.
	var attempts atomic.Int32
	discover := func() ([]vmdb.VM, error) {
		n := attempts.Add(1)
		if n <= 2 {
			return nil, errors.New("nope")
		}
		return []vmdb.VM{{Name: "vm1", Status: "active"}}, nil
	}

	store := NewStore()
	var logBuf bytes.Buffer
	p := New(Options{
		Collector:   mockCollector(0),
		Store:       store,
		DiscoverVMs: discover,
		Budget:      time.Second,
		MinInterval: 10 * time.Millisecond,
		Log:         testLogger(&logBuf),
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	// Wait until a sample lands (proves discovery eventually succeeded
	// after backoff didn't deadlock the loop)
	deadline := time.After(5 * time.Second)
	for store.Latest("vm1") == nil {
		select {
		case <-deadline:
			cancel()
			t.Fatal("timed out waiting for successful collection after backoff")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	cancel()
	<-done

	if !bytes.Contains(logBuf.Bytes(), []byte("backing off after discovery failure")) {
		t.Error("expected backoff log entry")
	}
}

func TestRunOnce_PrunesRemovedVMs(t *testing.T) {
	store := NewStore()
	// Pre-populate as if a previous cycle had recorded an old VM
	store.Record(&Sample{
		VMName:    "old1",
		Metrics:   &collector.Metrics{},
		Timestamp: time.Now(),
		Duration:  time.Millisecond,
	})

	var logBuf bytes.Buffer
	p := New(Options{
		Collector:   mockCollector(0),
		Store:       store,
		DiscoverVMs: mockDiscoverer([]vmdb.VM{{Name: "web1", Status: "active"}}),
		Budget:      500 * time.Millisecond,
		MinInterval: time.Second,
		Log:         testLogger(&logBuf),
	})

	p.RunOnce(context.Background())

	if store.Latest("web1") == nil {
		t.Error("web1 should have been recorded")
	}
	if store.Latest("old1") != nil {
		t.Error("old1 should have been pruned")
	}
}

func TestRunOnce_ConcurrentCollection(t *testing.T) {
	// With 4 slow VMs and concurrency=4, total cycle should be ~one Collect
	// duration, not 4×. Sequential execution would take >= 200ms; concurrent
	// should comfortably stay under 150ms.
	const collectDelay = 50 * time.Millisecond
	vms := []vmdb.VM{
		{Name: "v1"}, {Name: "v2"}, {Name: "v3"}, {Name: "v4"},
	}

	store := NewStore()
	var logBuf bytes.Buffer
	p := New(Options{
		Collector:   mockCollector(collectDelay),
		Store:       store,
		DiscoverVMs: mockDiscoverer(vms),
		Budget:      time.Second,
		MinInterval: time.Second,
		Concurrency: 4,
		Log:         testLogger(&logBuf),
	})

	elapsed, err := p.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed >= 4*collectDelay {
		t.Errorf("elapsed = %v, expected concurrent execution to be << sequential (%v)", elapsed, 4*collectDelay)
	}
	for _, vm := range vms {
		if store.Latest(vm.Name) == nil {
			t.Errorf("missing sample for %s", vm.Name)
		}
	}
}

func TestRunOnce_RespectsConcurrencyLimit(t *testing.T) {
	// With concurrency=2 and 4 VMs each taking 50ms, peak in-flight is 2,
	// total cycle ≥ 2 batches × 50ms = 100ms.
	const collectDelay = 50 * time.Millisecond
	var inFlight, peak atomic.Int32

	coll := collector.New("mock-metrics", func(ctx context.Context, name string, args ...string) ([]byte, error) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(collectDelay)
		inFlight.Add(-1)
		return []byte(`{"cpu_percent":10,"cpu_count":1,"memory_used_mb":512,"memory_total_mb":1024,"disk_used_mb":5000,"disk_total_mb":20480,"disk_read_iops":0,"disk_write_iops":0,"disk_read_bytes_sec":0,"disk_write_bytes_sec":0,"net_rx_bytes_sec":0,"net_tx_bytes_sec":0,"net_connections":10,"guest_agent_responsive":true,"uptime_seconds":3600,"state":"running"}`), nil
	})

	vms := []vmdb.VM{{Name: "v1"}, {Name: "v2"}, {Name: "v3"}, {Name: "v4"}}
	store := NewStore()
	var logBuf bytes.Buffer
	p := New(Options{
		Collector:   coll,
		Store:       store,
		DiscoverVMs: mockDiscoverer(vms),
		Budget:      time.Second,
		MinInterval: time.Second,
		Concurrency: 2,
		Log:         testLogger(&logBuf),
	})

	p.RunOnce(context.Background())

	if peak.Load() > 2 {
		t.Errorf("peak in-flight = %d, want ≤ 2", peak.Load())
	}
}
