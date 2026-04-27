package poller

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mwaddip/blockhost-monitor/internal/collector"
)

func TestStore_RecordAndLatest(t *testing.T) {
	s := NewStore()

	s.Record(&Sample{
		VMName:    "web1",
		Metrics:   &collector.Metrics{CPUPercent: 45.2},
		Timestamp: time.Now(),
		Duration:  50 * time.Millisecond,
	})

	got := s.Latest("web1")
	if got == nil {
		t.Fatal("expected sample for web1")
	}
	if got.Metrics.CPUPercent != 45.2 {
		t.Errorf("cpu = %f, want 45.2", got.Metrics.CPUPercent)
	}
}

func TestStore_LatestMissing(t *testing.T) {
	s := NewStore()
	if s.Latest("nonexistent") != nil {
		t.Error("expected nil for missing VM")
	}
}

func TestStore_Prune(t *testing.T) {
	s := NewStore()
	now := time.Now()
	s.Record(&Sample{VMName: "web1", Metrics: &collector.Metrics{}, Timestamp: now, Duration: time.Millisecond})
	s.Record(&Sample{VMName: "db1", Metrics: &collector.Metrics{}, Timestamp: now, Duration: time.Millisecond})
	s.Record(&Sample{VMName: "old1", Metrics: &collector.Metrics{}, Timestamp: now, Duration: time.Millisecond})

	s.Prune(map[string]struct{}{"web1": {}, "db1": {}})

	if s.Latest("web1") == nil {
		t.Error("web1 should be retained")
	}
	if s.Latest("db1") == nil {
		t.Error("db1 should be retained")
	}
	if s.Latest("old1") != nil {
		t.Error("old1 should have been pruned")
	}
}

func TestStore_PruneAll(t *testing.T) {
	s := NewStore()
	s.Record(&Sample{VMName: "web1", Metrics: &collector.Metrics{}, Timestamp: time.Now(), Duration: time.Millisecond})
	s.Prune(map[string]struct{}{})
	if s.Latest("web1") != nil {
		t.Error("expected web1 to be pruned with empty active set")
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup

	// Concurrent writers with distinct keys to stress map growth
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.Record(&Sample{
				VMName:    fmt.Sprintf("vm-%d", n),
				Metrics:   &collector.Metrics{CPUPercent: float64(n)},
				Timestamp: time.Now(),
				Duration:  time.Millisecond,
			})
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Latest("vm-0")
		}()
	}

	wg.Wait()
}
