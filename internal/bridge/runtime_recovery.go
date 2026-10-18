package bridge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type runtimeRecovery struct {
	File       string    `json:"file"`
	Quarantine string    `json:"quarantine"`
	Recovered  bool      `json:"recovered"`
	CheckedAt  time.Time `json:"checked_at"`
	Message    string    `json:"message"`
}

type runtimeFormat struct {
	Schema     int               `json:"schema"`
	Statistics string            `json:"statistics"`
	Recoveries []runtimeRecovery `json:"recoveries,omitempty"`
}

// Data files retain their original array/map shape for rollback compatibility.
// Version metadata is separate; an unknown format must never be overwritten.
func (s *Service) loadRuntimeLocked() error {
	dir := s.cfg.DataDir
	format := runtimeFormat{Schema: 1, Statistics: "minute-and-day-v1"}
	if raw, err := os.ReadFile(filepath.Join(dir, "runtime-format.json")); err == nil {
		var stored runtimeFormat
		if json.Unmarshal(raw, &stored) != nil || stored.Schema != 1 || stored.Statistics != "minute-and-day-v1" {
			return fail(500, "unsupported runtime data format; preserve files and restore a compatible version")
		}
		format = stored
	} else if !os.IsNotExist(err) {
		return fail(500, "runtime format metadata cannot be read")
	}
	s.storageWarnings = format.Recoveries
	var err error
	if s.logs, err = loadRuntimeFile(s, "requests.json", []LogEntry{}); err != nil {
		return err
	}
	if s.statistics, err = loadRuntimeFile(s, "statistics.json", map[string]Counters{}); err != nil {
		return err
	}
	if len(s.statistics) > 0 {
		backup := filepath.Join(dir, "statistics-before-daily-v1.json")
		if _, err := os.Lstat(backup); os.IsNotExist(err) {
			if atomicJSON(backup, s.statistics) != nil {
				return fail(500, "statistics migration backup could not be saved")
			}
		} else if err != nil {
			return fail(500, "statistics migration backup cannot be inspected")
		}
	}
	saved, err := loadRuntimeFile(s, "usage.json", map[string]persistedUsage{})
	if err != nil {
		return err
	}
	for cid, item := range saved {
		s.usage[cid] = &usageCache{fingerprint: item.Fingerprint, value: item.Value}
		if item.Account != "" {
			s.accounts[cid] = item.Account
		}
	}
	if len(s.storageWarnings) > 20 {
		s.storageWarnings = s.storageWarnings[len(s.storageWarnings)-20:]
	}
	format.Recoveries = s.storageWarnings
	if atomicJSON(filepath.Join(dir, "runtime-format.json"), format) != nil {
		return fail(500, "runtime format metadata could not be saved")
	}
	return nil
}

func loadRuntimeFile[T any](s *Service, name string, empty T) (T, error) {
	path := filepath.Join(s.cfg.DataDir, name)
	raw, err := readRegularRuntime(path)
	if os.IsNotExist(err) {
		// Also cover interruption after quarantine but before replacement.
		if _, backupErr := os.Lstat(path + ".last-good"); os.IsNotExist(backupErr) {
			return empty, nil
		}
		raw = []byte("{")
		err = nil
	}
	if err != nil {
		return empty, fail(500, "runtime file cannot be read: "+name)
	}
	var value T
	if decodeRuntime(raw, &value) {
		return value, nil
	}
	notice := runtimeRecovery{File: name, Quarantine: name + ".corrupt-" + id(), CheckedAt: time.Now().UTC(), Message: "历史文件损坏已隔离；没有可用检查点，此文件的历史范围未知，其他配置与历史保留"}
	backup, backupErr := readRegularRuntime(path + ".last-good")
	value = *new(T) // Unmarshal may partially fill a map before returning an error.
	if backupErr == nil && decodeRuntime(backup, &value) {
		notice.Recovered = true
		notice.Message = "已从最近有效检查点恢复；检查点后的记录可能缺失，隔离原件保留"
	} else {
		value = empty
	}
	// Save the notice first so a crash after quarantine cannot silently discard
	// the fact that history is incomplete. A failed recovery remains visible.
	s.storageWarnings = append(s.storageWarnings, notice)
	if atomicJSON(filepath.Join(s.cfg.DataDir, "runtime-format.json"), runtimeFormat{Schema: 1, Statistics: "minute-and-day-v1", Recoveries: s.storageWarnings}) != nil {
		return empty, fail(500, "runtime recovery notice could not be saved: "+name)
	}
	if err := os.Rename(path, filepath.Join(s.cfg.DataDir, notice.Quarantine)); err != nil && !os.IsNotExist(err) {
		return empty, fail(500, "corrupt runtime file could not be isolated: "+name)
	}
	if atomicJSON(path, value) != nil {
		return empty, fail(500, "runtime recovery could not be saved: "+name)
	}
	return value, nil
}

func readRegularRuntime(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, os.ErrPermission
	}
	return os.ReadFile(path)
}
func decodeRuntime(raw []byte, target any) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		// Older writers serialized an empty request slice as null whenever
		// quota-only activity flushed all three files. This is valid legacy data.
		if logs, ok := target.(*[]LogEntry); ok {
			*logs = []LogEntry{}
			return true
		}
		return false
	}
	if json.Unmarshal(raw, target) != nil {
		return false
	}
	if counters, ok := target.(*map[string]Counters); ok {
		for _, c := range *counters {
			if c.Time.IsZero() || c.Model == "" || (c.BucketSeconds != 0 && c.BucketSeconds != 60 && c.BucketSeconds != 86400) {
				return false
			}
		}
	}
	return true
}

// Checkpoints are refreshed at most once a minute rather than doubling every
// request-log write. Forced checkpoints are used when explicitly resetting.
func (s *Service) writeRuntimeSnapshot(dir, name string, value any, force bool) error {
	if err := atomicJSON(filepath.Join(dir, name), value); err != nil {
		return err
	}
	if s.checkpointAt == nil {
		s.checkpointAt = map[string]time.Time{}
	}
	if force || time.Since(s.checkpointAt[name]) >= time.Minute {
		if err := atomicJSON(filepath.Join(dir, name+".last-good"), value); err != nil {
			return err
		}
		s.checkpointAt[name] = time.Now()
	}
	return nil
}
