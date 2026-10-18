package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"testing"
	"time"
)

// Opt-in measurement, not a timing gate. Uses exactly the same observations
// before/after compaction; no upstream traffic or machine-specific threshold.
func TestRuntimeScaleMeasurement(t *testing.T) {
	if os.Getenv("BRIDGE_MEASURE") != "1" {
		t.Skip("set BRIDGE_MEASURE=1 for the synthetic scale measurement")
	}
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)
	s := NewService()
	for minute := 0; minute < 90*1440; minute++ {
		at := now.Add(-time.Duration(minute) * time.Minute)
		for model := 0; model < 2; model++ {
			name := fmt.Sprintf("model-%d", model)
			s.statistics[at.Format(time.RFC3339)+"|"+name] = Counters{Time: at, Model: name, Requests: 1, Success: 1, Input: 100, Output: 20}
		}
	}
	measure := func() map[string]any {
		runtime.GC()
		runtime.GC() // Drop the previous JSON buffer from sync.Pool before sampling.
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		start := time.Now()
		raw := jsonBytes(s.statistics)
		encode := time.Since(start)
		start = time.Now()
		var restored map[string]Counters
		if json.Unmarshal(raw, &restored) != nil {
			t.Fatal("decode")
		}
		decode := time.Since(start)
		durations := []float64{}
		var total int64
		for i := 0; i < 40; i++ {
			start = time.Now()
			v := s.statisticsView(ManagementRequest{}).(map[string]any)
			total = v["totals"].(Counters).Requests
			durations = append(durations, float64(time.Since(start).Microseconds())/1000)
		}
		sort.Float64s(durations)
		return map[string]any{"buckets": len(s.statistics), "json_bytes": len(raw), "encode_ms": float64(encode.Microseconds()) / 1000, "decode_ms": float64(decode.Microseconds()) / 1000, "query_p95_ms": durations[38], "heap_inuse_bytes": mem.HeapInuse, "heap_alloc_bytes": mem.HeapAlloc, "requests": total}
	}
	before := measure()
	start := time.Now()
	s.compactStatisticsLocked(now)
	compaction := time.Since(start)
	after := measure()
	if before["requests"] != after["requests"] {
		t.Fatal("totals changed")
	}
	t.Log(string(jsonBytes(map[string]any{"fixture": "90 days x 2 models x 1 request/minute", "before": before, "after": after, "compaction_ms": float64(compaction.Microseconds()) / 1000})))
}
