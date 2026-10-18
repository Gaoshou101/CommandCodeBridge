package bridge

import (
	"sort"
	"time"
)

// Seven complete UTC days keep minute resolution. Older data is combined by
// UTC day without losing totals. Expiry is opt-in (zero means keep all days).
func (s *Service) compactStatisticsLocked(now time.Time) {
	if now.Before(s.nextCompaction) {
		return
	}
	s.nextCompaction = now.Add(time.Hour)
	day := now.Truncate(24 * time.Hour)
	cutoff := day.Add(-7 * 24 * time.Hour)
	retention := day.Add(-time.Duration(s.cfg.StatisticsRetentionDays) * 24 * time.Hour)
	next := make(map[string]Counters)
	changed := false
	for _, counter := range s.statistics {
		counter.Time = counter.Time.UTC()
		seconds := counter.BucketSeconds
		if seconds == 0 {
			seconds = 60
		}
		if s.cfg.StatisticsRetentionDays > 0 && !counter.Time.Add(time.Duration(seconds)*time.Second).After(retention) {
			changed = true
			continue
		}
		if counter.Time.Before(cutoff) {
			if seconds != 86400 {
				changed = true
			}
			counter.Time = counter.Time.Truncate(24 * time.Hour)
			counter.BucketSeconds = 86400
		}
		key := counter.Time.Format(time.RFC3339) + "|" + counter.Model
		if existing, ok := next[key]; ok {
			existing.add(counter)
			next[key] = existing
			changed = true
		} else {
			next[key] = counter
		}
	}
	if changed {
		s.statistics = next
		s.markDirtyLocked(dirtyStatistics)
	}
}

func (s *Service) storageStatusLocked() any {
	files := []string{}
	for name, flag := range map[string]runtimeFiles{"requests.json": dirtyLogs, "statistics.json": dirtyStatistics, "usage.json": dirtyUsage} {
		if s.persistDirty&flag != 0 {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	writes := map[string]uint64{}
	for name, count := range s.persistWrites {
		writes[name] = count
	}
	return map[string]any{"pending_files": files, "pending_since": optionalTime(s.persistSince), "last_success_at": optionalTime(s.lastPersistAt), "writing": s.persistWriting, "recovery_warnings": append([]runtimeRecovery{}, s.storageWarnings...), "statistics_buckets": len(s.statistics), "writes": writes, "statistics_retention_days": s.cfg.StatisticsRetentionDays, "precision": "最近七个完整 UTC 日保留分钟统计，更早按 UTC 日汇总；保留天数 0 表示不淘汰"}
}
func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
