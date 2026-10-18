package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeRecoveryIsolatesOnlyDamagedFile(t *testing.T) {
	for _, backup := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing_checkpoint", true: "checkpoint"}[backup], func(t *testing.T) {
			s := NewService()
			s.cfg.DataDir = t.TempDir()
			// A partially decoded primary must not contaminate the restored map.
			os.WriteFile(filepath.Join(s.cfg.DataDir, "statistics.json"), []byte(`{"partial":{"model":"bad"},"invalid":5}`), 0600)
			good := map[string]Counters{"valid": {Time: time.Now().UTC().Truncate(time.Minute), Model: "kept", Requests: 9}}
			if backup {
				atomicJSON(filepath.Join(s.cfg.DataDir, "statistics.json.last-good"), good)
			}
			atomicJSON(filepath.Join(s.cfg.DataDir, "requests.json"), []LogEntry{{ID: "kept-log"}})
			if err := s.loadRuntimeLocked(); err != nil {
				t.Fatal(err)
			}
			if len(s.logs) != 1 || len(s.statistics) != map[bool]int{false: 0, true: 1}[backup] {
				t.Fatalf("recovery contaminated history: %+v", s.statistics)
			}
			if len(s.storageWarnings) != 1 || s.storageWarnings[0].Recovered != backup {
				t.Fatal(s.storageWarnings)
			}
			if _, err := os.Stat(filepath.Join(s.cfg.DataDir, s.storageWarnings[0].Quarantine)); err != nil {
				t.Fatal(err)
			}
			other := NewService()
			other.cfg = s.cfg
			if err := other.loadRuntimeLocked(); err != nil || len(other.storageWarnings) != 1 {
				t.Fatal("recovery notice lost", err)
			}
		})
	}
}

func TestRuntimeFormatAndSettingsFailClosed(t *testing.T) {
	for _, name := range []string{"runtime-format.json", "settings.json"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			raw := []byte(`{"schema":99}`)
			if name == "settings.json" {
				raw = []byte(`broken`)
			}
			os.WriteFile(filepath.Join(dir, name), raw, 0600)
			os.WriteFile(filepath.Join(dir, "requests.json"), []byte(`corrupt-preserved`), 0600)
			s := NewService()
			if err := s.configure(jsonBytes(map[string]any{"config_yaml": []byte("data_dir: " + filepath.ToSlash(dir))})); err == nil {
				t.Fatal("unsafe format accepted")
			}
			got, _ := os.ReadFile(filepath.Join(dir, "requests.json"))
			if string(got) != "corrupt-preserved" {
				t.Fatal("runtime file touched")
			}
		})
	}
}

func TestIndependentPersistenceAndCheckpointThrottle(t *testing.T) {
	s, _, _ := setup(t, func(string, map[string]any) mockReply { return streamReply(chatStream("ok")) })
	s.flushStatistics()
	s.mu.Lock()
	s.markDirtyLocked(dirtyUsage)
	s.mu.Unlock()
	s.flushStatistics()
	s.mu.RLock()
	writes := s.storageStatusLocked().(map[string]any)["writes"].(map[string]uint64)
	s.mu.RUnlock()
	if writes["usage.json"] != 1 || writes["requests.json"] != 0 || writes["statistics.json"] != 0 {
		t.Fatal(writes)
	}
	s.mu.Lock()
	s.markDirtyLocked(dirtyUsage)
	s.mu.Unlock()
	s.flushStatistics()
	if writes["usage.json"] != 1 {
		t.Fatal("status snapshot aliases live map")
	}
	checkpoint := filepath.Join(s.cfg.DataDir, "usage.json.last-good")
	before, _ := os.Stat(checkpoint)
	s.mu.Lock()
	s.markDirtyLocked(dirtyUsage)
	s.mu.Unlock()
	s.flushStatistics()
	after, _ := os.Stat(checkpoint)
	if before == nil || after == nil || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("checkpoint rewritten within one minute")
	}
}

func TestResetCannotRestoreOldCheckpoint(t *testing.T) {
	s, _, _ := setup(t, func(string, map[string]any) mockReply { return streamReply(chatStream("ok")) })
	s.record(LogEntry{Model: "alias", Status: 200})
	s.flushStatistics()
	if err := s.resetStatistics(); err != nil {
		t.Fatal(err)
	}
	// Simulate an immediate crash before the background worker runs.
	s.stopPersistence()
	os.WriteFile(filepath.Join(s.cfg.DataDir, "statistics.json"), []byte(`truncated`), 0600)
	restored := NewService()
	restored.cfg = s.cfg
	if err := restored.loadRuntimeLocked(); err != nil || len(restored.statistics) != 0 {
		t.Fatal("reset statistics resurrected", err)
	}
}

func TestStatisticsCompactionPreservesTotalsAndPrecision(t *testing.T) {
	s := NewService()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 60; i++ {
		at := now.Add(-10 * 24 * time.Hour).Add(time.Duration(i) * time.Minute)
		s.statistics[at.String()] = Counters{Time: at, Model: "old", Requests: 2, Input: 3, Output: 4}
	}
	s.statistics["recent"] = Counters{Time: now, Model: "recent", Requests: 1}
	s.compactStatisticsLocked(now)
	if len(s.statistics) != 2 {
		t.Fatal(len(s.statistics))
	}
	totals := s.statisticsView(ManagementRequest{}).(map[string]any)
	if totals["totals"].(Counters).Requests != 121 || totals["bucket_seconds"] != 86400 {
		t.Fatal(totals)
	}
	s.cfg.StatisticsRetentionDays = 9
	s.nextCompaction = time.Time{}
	s.compactStatisticsLocked(now)
	if len(s.statistics) != 1 {
		t.Fatal("expiry ignored")
	}
}

func TestLegacyMigrationBackupBeforeCompaction(t *testing.T) {
	s := NewService()
	s.cfg.DataDir = t.TempDir()
	old := map[string]Counters{"old": {Time: time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Minute), Model: "old", Requests: 3}}
	atomicJSON(filepath.Join(s.cfg.DataDir, "statistics.json"), old)
	if err := s.loadRuntimeLocked(); err != nil {
		t.Fatal(err)
	}
	s.compactStatisticsLocked(time.Now().UTC())
	raw, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "statistics-before-daily-v1.json"))
	var saved map[string]Counters
	if err != nil || json.Unmarshal(raw, &saved) != nil || saved["old"].BucketSeconds != 0 || saved["old"].Requests != 3 {
		t.Fatal("missing original precision backup", err)
	}
}

func TestSamplerJitterDoesNotIncreaseRate(t *testing.T) {
	for i := 0; i < 100; i++ {
		d := samplerDelay(time.Minute)
		if d < time.Minute || d > 72*time.Second {
			t.Fatal(d)
		}
	}
}

func TestMissingPrimaryRecoversCheckpoint(t *testing.T) {
	s := NewService()
	s.cfg.DataDir = t.TempDir()
	atomicJSON(filepath.Join(s.cfg.DataDir, "requests.json.last-good"), []LogEntry{{ID: "checkpoint"}})
	if err := s.loadRuntimeLocked(); err != nil || len(s.logs) != 1 || !s.storageWarnings[0].Recovered {
		t.Fatal("missing primary recovery failed", err)
	}
}

func TestLegacyEmptyLogsNullNeedsNoRecovery(t *testing.T) {
	s := NewService()
	s.cfg.DataDir = t.TempDir()
	os.WriteFile(filepath.Join(s.cfg.DataDir, "requests.json"), []byte("null"), 0600)
	if err := s.loadRuntimeLocked(); err != nil || len(s.logs) != 0 || len(s.storageWarnings) != 0 {
		t.Fatal("legacy empty logs misidentified", err)
	}
}

func TestResetWriteFailurePreservesMemory(t *testing.T) {
	s, _, _ := setup(t, func(string, map[string]any) mockReply { return streamReply(chatStream("ok")) })
	s.record(LogEntry{Model: "kept", Status: 200})
	s.flushStatistics()
	s.stopPersistence()
	path := filepath.Join(s.cfg.DataDir, "statistics.json")
	os.Remove(path)
	os.Mkdir(path, 0700)
	if err := s.resetStatistics(); err == nil {
		t.Fatal("reset unexpectedly succeeded")
	}
	if len(s.statistics) == 0 || s.statisticsEpoch != 0 {
		t.Fatal("failed reset erased in-memory observations")
	}
	os.Remove(path)
	s.mu.Lock()
	s.markDirtyLocked(dirtyStatistics)
	s.mu.Unlock()
	s.flushStatistics()
	if s.storageError != "" {
		t.Fatal(s.storageError)
	}
}
