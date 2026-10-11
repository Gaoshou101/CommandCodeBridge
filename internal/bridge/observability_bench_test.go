package bridge

import (
	"fmt"
	"testing"
	"time"
)

// Measure a full retention buffer rather than an almost-empty new installation.
func BenchmarkRecordAtRetention(b *testing.B) {
	for _, retention := range []int{1000, 10000} {
		b.Run(fmt.Sprint(retention), func(b *testing.B) {
			s := NewService()
			s.cfg.DataDir = b.TempDir()
			s.cfg.LogRetention = retention
			s.loaded = true
			s.startPersistenceLocked()
			b.Cleanup(s.shutdown)
			entry := LogEntry{ID: "benchmark", Time: time.Now().UTC(), Model: "benchmark-model", Status: 200,
				Credential: "benchmark account", PromptTokens: 10, CompletionTokens: 3, UsageReported: true}
			s.logs = make([]LogEntry, retention)
			for i := range s.logs {
				s.logs[i] = entry
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.record(entry)
			}
		})
	}
}
