package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestBatchedPersistenceShutdownAndReset(t *testing.T) {
	s, _, _ := setup(t, func(string, map[string]any) mockReply { return streamReply(chatStream("ok")) })
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for n := 0; n < 25; n++ {
				s.record(LogEntry{ID: id(), Model: "model", Status: 200, epoch: 0})
				_ = s.statisticsView(ManagementRequest{})
			}
		}()
	}
	group.Wait()
	// An old request which finishes after reset must stay in logs without
	// contributing to the new epoch, including after a concurrent batch flush.
	group.Add(1)
	go func() { defer group.Done(); s.flushStatistics() }()
	if _, err := s.management(jsonBytes(ManagementRequest{Method: "POST", Path: apiBase + "/statistics/reset"})); err != nil {
		t.Fatal(err)
	}
	group.Wait()
	s.record(LogEntry{ID: id(), Model: "model", Status: 200, epoch: 0})
	s.record(LogEntry{ID: id(), Model: "model", Status: 200, epoch: 1})
	s.shutdown()
	var logs []LogEntry
	var statistics map[string]Counters
	for name, target := range map[string]any{"requests.json": &logs, "statistics.json": &statistics} {
		data, err := os.ReadFile(filepath.Join(s.cfg.DataDir, name))
		if err != nil || json.Unmarshal(data, target) != nil {
			t.Fatalf("invalid persisted %s: %v", name, err)
		}
	}
	if len(logs) != 202 {
		t.Fatalf("lost shutdown logs: %d", len(logs))
	}
	total := int64(0)
	for _, counter := range statistics {
		total += counter.Requests
	}
	if total != 1 {
		t.Fatalf("reset was overwritten by an older snapshot: %d", total)
	}
}

func TestBatchedPersistenceRetriesWriteFailure(t *testing.T) {
	s, _, _ := setup(t, func(string, map[string]any) mockReply { return streamReply(chatStream("ok")) })
	path := filepath.Join(s.cfg.DataDir, "requests.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	s.record(LogEntry{ID: id(), Model: "model", Status: 200})
	s.flushStatistics()
	s.mu.RLock()
	dirty, reported := s.persistDirty, s.storageError
	s.mu.RUnlock()
	if !dirty || reported == "" {
		t.Fatal("write failure was not retained for retry")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s.flushStatistics()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.persistDirty || s.storageError != "" {
		t.Fatal("successful retry did not clear persistence error")
	}
}
