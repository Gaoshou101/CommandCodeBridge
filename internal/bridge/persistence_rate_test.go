package bridge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeWriteInterval(t *testing.T) {
	for _, item := range []struct {
		size int
		want time.Duration
	}{
		{0, statisticsFlushInterval},
		{64 << 10, statisticsFlushInterval},
		{512 << 10, 500 * time.Millisecond},
		{6 << 20, 6 * time.Second},
		{100 << 20, runtimeWriteMaxInterval},
	} {
		if got := runtimeWriteInterval(item.size); got != item.want {
			t.Fatalf("runtimeWriteInterval(%d) = %v, want %v", item.size, got, item.want)
		}
	}
}

func TestBackgroundFlushPacesLargeRuntimeFiles(t *testing.T) {
	s, _, _ := setup(t, func(string, map[string]any) mockReply { return streamReply(chatStream("ok")) })
	// A first write initializes the pacing state for every runtime file.
	s.record(LogEntry{ID: id(), Model: "model", Status: 200})
	s.flushStatistics()
	// Pretend statistics.json was just written at 6MiB; requests.json stays small.
	s.mu.Lock()
	s.persistLastWrite["statistics.json"] = time.Now()
	s.persistLastSize["statistics.json"] = 6 << 20
	s.persistLastWrite["requests.json"] = time.Now().Add(-time.Second)
	before := s.persistWrites["statistics.json"]
	s.mu.Unlock()
	s.record(LogEntry{ID: id(), Model: "model", Status: 200})
	s.flushRuntimeFiles(false)
	s.mu.RLock()
	pending, written, logs := s.persistDirty, s.persistWrites["statistics.json"], s.persistWrites["requests.json"]
	s.mu.RUnlock()
	if pending&dirtyStatistics == 0 || pending&dirtyLogs != 0 {
		t.Fatalf("large statistics should stay pending and logs should be written: dirty=%b", pending)
	}
	if written != before || logs == 0 {
		t.Fatalf("unexpected writes: statistics %d -> %d, requests %d", before, written, logs)
	}
	// Shutdown and explicit flushes ignore pacing.
	s.flushStatistics()
	s.mu.RLock()
	pending, written = s.persistDirty, s.persistWrites["statistics.json"]
	s.mu.RUnlock()
	if pending != 0 || written != before+1 {
		t.Fatalf("forced flush left files pending: dirty=%b writes=%d", pending, written)
	}
}

func TestRuntimeSnapshotsAreCompactJSON(t *testing.T) {
	s, _, _ := setup(t, func(string, map[string]any) mockReply { return streamReply(chatStream("ok")) })
	s.record(LogEntry{ID: "compact", Model: "model", Status: 200})
	s.flushStatistics()
	for _, name := range []string{"requests.json", "statistics.json"} {
		data, err := os.ReadFile(filepath.Join(s.cfg.DataDir, name))
		if err != nil || !json.Valid(data) {
			t.Fatalf("invalid %s: %v", name, err)
		}
		if bytes.Contains(data, []byte("\n")) {
			t.Fatalf("%s is not compact", name)
		}
		s.mu.RLock()
		size := s.persistLastSize[name]
		s.mu.RUnlock()
		if size != len(data) {
			t.Fatalf("%s recorded size %d, file has %d bytes", name, size, len(data))
		}
	}
}
