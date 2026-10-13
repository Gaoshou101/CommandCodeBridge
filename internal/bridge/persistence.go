package bridge

import (
	"path/filepath"
	"time"
)

const statisticsFlushInterval = 250 * time.Millisecond

// Only redacted observations and one-way identity hashes belong in data_dir.
// The fingerprint is checked when CPA registers each credential after restart.
type persistedUsage struct {
	Fingerprint string `json:"fingerprint"`
	Value       Usage  `json:"value"`
	Account     string `json:"account,omitempty"`
}

// Called under mu. The worker is separate from active: shutdown waits for
// requests to finish before stopping the worker and flushing their final logs.
func (s *Service) startPersistenceLocked() {
	if s.persistRunning {
		return
	}
	s.persistRunning = true
	s.persistStop = make(chan struct{})
	s.persistDone = make(chan struct{})
	stop, done := s.persistStop, s.persistDone
	go func() {
		defer close(done)
		ticker := time.NewTicker(statisticsFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.flushStatistics()
			case <-stop:
				s.flushStatistics()
				return
			}
		}
	}()
}

// persistMu serializes disk snapshots with reset so an older in-flight snapshot
// cannot overwrite a freshly cleared counter. Serialization and IO run without
// the service lock; new requests and management reads can proceed concurrently.
func (s *Service) flushStatistics() {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.mu.Lock()
	if !s.loaded || !s.persistDirty {
		s.mu.Unlock()
		return
	}
	logs := append([]LogEntry(nil), s.logs...)
	statistics := make(map[string]Counters, len(s.statistics))
	for key, value := range s.statistics {
		statistics[key] = value
	}
	usage := make(map[string]persistedUsage, len(s.usage))
	for key, cached := range s.usage {
		if cached.value.UpdatedAt != nil || cached.value.CheckedAt != nil {
			usage[key] = persistedUsage{Fingerprint: cached.fingerprint, Value: cached.value, Account: s.accounts[key]}
		}
	}
	dir := s.cfg.DataDir
	s.persistDirty = false
	s.persistWriting = true
	s.mu.Unlock()
	logErr := atomicJSON(filepath.Join(dir, "requests.json"), logs)
	statsErr := atomicJSON(filepath.Join(dir, "statistics.json"), statistics)
	usageErr := atomicJSON(filepath.Join(dir, "usage.json"), usage)
	s.mu.Lock()
	s.persistWriting = false
	if logErr != nil || statsErr != nil || usageErr != nil {
		s.storageError = "request statistics or quota persistence failed"
		s.persistDirty = true // Retry at the next tick, including on final shutdown.
	} else {
		s.storageError = ""
	}
	s.mu.Unlock()
}

func (s *Service) stopPersistence() {
	s.mu.Lock()
	if s.persistRunning {
		close(s.persistStop)
		s.persistRunning = false
	}
	done := s.persistDone
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}
