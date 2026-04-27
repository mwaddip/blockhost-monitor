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

// Prune removes any stored samples whose VM name is not in the active set.
// Called once per poll cycle so deleted VMs don't linger in the store.
func (s *Store) Prune(active map[string]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name := range s.latest {
		if _, ok := active[name]; !ok {
			delete(s.latest, name)
		}
	}
}
