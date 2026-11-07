package bridge

import (
	"os"
	"path/filepath"
	"time"
)

const statisticsFlushInterval = 250 * time.Millisecond

// Under sustained traffic each runtime file is rewritten in full on every
// tick, so a large statistics or request log would cost several megabytes per
// second of disk writes. Consecutive rewrites of one file are spaced so that
// its sustained write rate stays near runtimeWriteBytesPerSecond. Small files
// keep the 250ms window, the first change after a quiet period is written on
// the next tick, and shutdown always flushes every pending file.
const (
	runtimeWriteBytesPerSecond = 1 << 20
	runtimeWriteMaxInterval    = 10 * time.Second
)

// runtimeWriteInterval is the minimum spacing between rewrites of a runtime
// file whose last successful snapshot was size bytes.
func runtimeWriteInterval(size int) time.Duration {
	interval := time.Duration(int64(size) * int64(time.Second) / runtimeWriteBytesPerSecond)
	switch {
	case interval < statisticsFlushInterval:
		return statisticsFlushInterval
	case interval > runtimeWriteMaxInterval:
		return runtimeWriteMaxInterval
	default:
		return interval
	}
}

var runtimeFileNames = map[runtimeFiles]string{dirtyLogs: "requests.json", dirtyStatistics: "statistics.json", dirtyUsage: "usage.json"}

type runtimeFiles uint8

const (
	dirtyLogs runtimeFiles = 1 << iota
	dirtyStatistics
	dirtyUsage
)

func (s *Service) markDirtyLocked(files runtimeFiles) {
	if s.persistSince.IsZero() {
		s.persistSince = time.Now().UTC()
	}
	s.persistDirty |= files
}

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
				s.flushRuntimeFiles(false)
			case <-stop:
				s.flushStatistics()
				return
			}
		}
	}()
}

// flushStatistics writes every pending runtime file immediately. It is used at
// shutdown and by tests; the background worker uses the rate-limited path.
func (s *Service) flushStatistics() {
	s.flushRuntimeFiles(true)
}

// persistMu serializes disk snapshots with reset so an older in-flight snapshot
// cannot overwrite a freshly cleared counter. Serialization and IO run without
// the service lock; new requests and management reads can proceed concurrently.
// Unless force is set, files rewritten too recently stay pending for a later tick.
func (s *Service) flushRuntimeFiles(force bool) {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.mu.Lock()
	if !s.loaded {
		s.mu.Unlock()
		return
	}
	now := time.Now()
	s.compactStatisticsLocked(now.UTC())
	dirty := s.persistDirty
	if !force {
		for flag, name := range runtimeFileNames {
			if dirty&flag != 0 && now.Sub(s.persistLastWrite[name]) < runtimeWriteInterval(s.persistLastSize[name]) {
				dirty &^= flag
			}
		}
	}
	if dirty == 0 {
		s.mu.Unlock()
		return
	}
	values := map[string]any{}
	if dirty&dirtyLogs != 0 {
		values["requests.json"] = append([]LogEntry{}, s.logs...)
	}
	if dirty&dirtyStatistics != 0 {
		statistics := make(map[string]Counters, len(s.statistics))
		for key, value := range s.statistics {
			statistics[key] = value
		}
		values["statistics.json"] = statistics
	}
	if dirty&dirtyUsage != 0 {
		usage := make(map[string]persistedUsage, len(s.usage))
		for key, cached := range s.usage {
			if cached.value.UpdatedAt != nil || cached.value.CheckedAt != nil {
				usage[key] = persistedUsage{Fingerprint: cached.fingerprint, Value: cached.value, Account: s.accounts[key]}
			}
		}
		values["usage.json"] = usage
	}
	dir := s.cfg.DataDir
	s.persistDirty &^= dirty
	s.persistWriting = true
	s.mu.Unlock()
	failed := runtimeFiles(0)
	for name, value := range values {
		size, err := s.writeRuntimeSnapshot(dir, name, value, false)
		s.mu.Lock()
		if s.persistErrors == nil {
			s.persistErrors = map[string]bool{}
		}
		if s.persistWrites == nil {
			s.persistWrites = map[string]uint64{}
		}
		if s.persistLastWrite == nil {
			s.persistLastWrite = map[string]time.Time{}
			s.persistLastSize = map[string]int{}
		}
		if err != nil {
			s.persistErrors[name] = true
			failed |= map[string]runtimeFiles{"requests.json": dirtyLogs, "statistics.json": dirtyStatistics, "usage.json": dirtyUsage}[name]
		} else {
			delete(s.persistErrors, name)
			s.persistWrites[name]++
			s.persistLastWrite[name] = time.Now()
			s.persistLastSize[name] = size
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.persistWriting = false
	s.persistDirty |= failed
	if len(s.persistErrors) > 0 {
		s.storageError = "request statistics or quota persistence failed"
	} else {
		s.storageError = ""
	}
	if failed == 0 {
		s.lastPersistAt = time.Now().UTC()
	}
	if s.persistDirty == 0 {
		s.persistSince = time.Time{}
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

// Retire the previous checkpoint before committing a reset. A crash between
// these operations can lose a checkpoint, but cannot resurrect cleared totals.
func (s *Service) resetStatistics() error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.cfg.DataDir
	suffix := id()
	if err := atomicJSON(filepath.Join(dir, "statistics-backup-"+suffix+".json"), s.statistics); err != nil {
		return err
	}
	checkpoint := filepath.Join(dir, "statistics.json.last-good")
	if err := os.Rename(checkpoint, filepath.Join(dir, "statistics-checkpoint-before-reset-"+suffix+".json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	next := map[string]Counters{}
	if err := atomicJSON(filepath.Join(dir, "statistics.json"), next); err != nil {
		return err
	}
	s.statistics = next
	s.statisticsEpoch++
	s.markDirtyLocked(dirtyStatistics)
	// Force the worker to create a fresh checkpoint, including after retry.
	if s.checkpointAt != nil {
		delete(s.checkpointAt, "statistics.json")
	}
	return nil
}
