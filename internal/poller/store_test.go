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

func TestStore_LatestAll(t *testing.T) {
	s := NewStore()
	now := time.Now()

	s.Record(&Sample{VMName: "web1", Metrics: &collector.Metrics{CPUPercent: 10}, Timestamp: now, Duration: time.Millisecond})
	s.Record(&Sample{VMName: "db1", Metrics: &collector.Metrics{CPUPercent: 20}, Timestamp: now, Duration: time.Millisecond})

	all := s.LatestAll()
	if len(all) != 2 {
		t.Fatalf("got %d entries, want 2", len(all))
	}
}

func TestStore_AvgPollDuration(t *testing.T) {
	s := NewStore()
	now := time.Now()

	s.Record(&Sample{VMName: "web1", Metrics: &collector.Metrics{}, Timestamp: now, Duration: 40 * time.Millisecond})
	s.Record(&Sample{VMName: "db1", Metrics: &collector.Metrics{}, Timestamp: now, Duration: 60 * time.Millisecond})

	avg := s.AvgPollDuration()
	if avg != 50*time.Millisecond {
		t.Errorf("avg = %v, want 50ms", avg)
	}
}

func TestStore_AvgPollDuration_Empty(t *testing.T) {
	s := NewStore()
	if s.AvgPollDuration() != 0 {
		t.Error("expected 0 for empty store")
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
			s.LatestAll()
			s.AvgPollDuration()
		}()
	}

	wg.Wait()
}
