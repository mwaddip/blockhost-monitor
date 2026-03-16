package poller

import (
	"sync"
	"time"

	"github.com/mwaddip/blockhost-monitor/internal/collector"
)

type Sample struct {
	VMName    string
	Metrics   *collector.Metrics
	Timestamp time.Time
	Duration  time.Duration
}

type Store struct {
	mu     sync.RWMutex
	latest map[string]*Sample
}

func NewStore() *Store {
	return &Store{
		latest: make(map[string]*Sample),
	}
}

func (s *Store) Record(sample *Sample) {
	s.mu.Lock()
	s.latest[sample.VMName] = sample
	s.mu.Unlock()
}

func (s *Store) Latest(vmName string) *Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest[vmName]
}

func (s *Store) LatestAll() map[string]*Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]*Sample, len(s.latest))
	for k, v := range s.latest {
		cp[k] = v
	}
	return cp
}

// AvgPollDuration returns the average collection duration across all VMs'
// latest samples. Intended for future adaptive scheduling — not yet wired
// into the poller. Stale entries for removed VMs will skew the average;
// eviction will be added when this is consumed.
func (s *Store) AvgPollDuration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.latest) == 0 {
		return 0
	}
	var total time.Duration
	for _, sample := range s.latest {
		total += sample.Duration
	}
	return total / time.Duration(len(s.latest))
}
