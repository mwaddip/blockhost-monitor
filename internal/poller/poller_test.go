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

	all := store.LatestAll()
	if len(all) != 2 {
		t.Fatalf("got %d samples, want 2", len(all))
	}
	if all["web1"] == nil {
		t.Error("missing sample for web1")
	}
	if all["db1"] == nil {
		t.Error("missing sample for db1")
	}
}

func TestRunOnce_EmptyVMList(t *testing.T) {
	store := NewStore()
	var logBuf bytes.Buffer
	p := New(Options{
		Collector:   mockCollector(0),
		Store:       store,
		DiscoverVMs: mockDiscoverer(nil),
		Budget:      500 * time.Millisecond,
		MinInterval: time.Second,
		Log:         testLogger(&logBuf),
	})

	p.RunOnce(context.Background())

	if len(store.LatestAll()) != 0 {
		t.Error("expected no samples for empty VM list")
	}
}

func TestRunOnce_BudgetExceededWarning(t *testing.T) {
	vms := []vmdb.VM{{Name: "slow1", Status: "active"}}

	store := NewStore()
	var logBuf bytes.Buffer
	// Budget of 1ms but collector takes 50ms
	p := New(Options{
		Collector:   mockCollector(50 * time.Millisecond),
		Store:       store,
		DiscoverVMs: mockDiscoverer(vms),
		Budget:      time.Millisecond,
		MinInterval: time.Second,
		Log:         testLogger(&logBuf),
	})

	p.RunOnce(context.Background())

	if !bytes.Contains(logBuf.Bytes(), []byte("poll cycle exceeded budget")) {
		t.Error("expected budget exceeded warning in logs")
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
