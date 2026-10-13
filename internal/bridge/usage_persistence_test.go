package bridge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func restartUsageService(t *testing.T, previous *Service, c Credential) *Service {
	t.Helper()
	cfg := previous.config()
	previous.shutdown()
	s := NewService()
	if err := s.configure(jsonBytes(map[string]any{"config_yaml": []byte("data_dir: " + filepath.ToSlash(cfg.DataDir) + "\nbase_url: " + cfg.BaseURL + "\n")})); err != nil {
		t.Fatal(err)
	}
	if _, err := s.parseAuth(jsonBytes(map[string]any{"FileName": c.ID + ".json", "RawJSON": jsonBytes(c)})); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.shutdown)
	return s
}

func TestUsageAndRequestHistorySurviveRestart(t *testing.T) {
	s, _, c := setup(t, quotaReply)
	u, err := s.credentialUsage(c.ID, "", true)
	if err != nil || len(u.Windows) == 0 {
		t.Fatalf("quota observation failed: %+v, %v", u, err)
	}
	s.record(LogEntry{ID: id(), CredentialID: c.ID, Model: "alias", Status: 200})
	last := s.logs[0].Time.UnixMilli()
	restored := restartUsageService(t, s, c)
	if cached := restored.usage[c.ID]; cached == nil || !cached.value.UpdatedAt.Equal(*u.UpdatedAt) || len(cached.value.Windows) != len(u.Windows) {
		t.Fatalf("quota not restored: %+v", cached)
	}
	// A still-valid cache can be read with no initialized host or upstream IO.
	v, err := restored.credentialUsage(c.ID, "", false)
	if err != nil || v.Monthly == nil || *v.Monthly != *u.Monthly {
		t.Fatalf("restored cache was not reused: %+v, %v", v, err)
	}
	view := restored.credentials()[0]
	if recent := view["recent_requests"].([]RecentRequest); len(recent) != 1 || recent[0].TimestampMS != last || recent[0].Failed {
		t.Fatalf("recent request not restored: %+v", recent)
	}
	if _, ok := view["quota"].(quotaResponse); !ok {
		t.Fatal("management snapshot did not expose cached quota")
	}
	data, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "usage.json"))
	if err != nil || !json.Valid(data) || bytes.Contains(data, []byte(c.APIKey)) || bytes.Contains(jsonBytes(view), []byte(c.APIKey)) {
		t.Fatal("invalid persisted observation or credential leak")
	}
}

func TestPersistedUsageDoesNotSurviveCredentialReplacement(t *testing.T) {
	s, _, c := setup(t, quotaReply)
	if _, err := s.credentialUsage(c.ID, "", true); err != nil {
		t.Fatal(err)
	}
	c.APIKey = "fixture-replacement-key"
	restored := restartUsageService(t, s, c)
	if restored.usage[c.ID] != nil || restored.accounts[c.ID] != "" {
		t.Fatal("a replacement credential borrowed persisted quota")
	}
}

func TestRecentRequestHistoryKeepsOldAttemptsAndIgnoresLocalSkips(t *testing.T) {
	s, _, c := setup(t, quotaReply)
	now := time.Now().UTC()
	s.mu.Lock()
	s.logs = []LogEntry{
		{CredentialID: "other", Time: now, Status: 200},
		{CredentialID: c.ID, Time: now.Add(-48 * time.Hour), Status: 200},
		{CredentialID: c.ID, Time: now.Add(-time.Minute), Status: 429, UpstreamSkipped: true},
	}
	recent := s.credentialRecentRequestsLocked(c)
	s.mu.Unlock()
	if len(recent) != 1 || recent[0].TimestampMS != now.Add(-48*time.Hour).UnixMilli() || recent[0].Failed {
		t.Fatalf("local skip or 24h cutoff overwrote the latest attempt: %+v", recent)
	}
	c.KeyUpdatedAt = &now
	if len(s.credentialRecentRequestsLocked(c)) != 0 {
		t.Fatal("replacement key borrowed previous request evidence")
	}
}

func TestExplicitUsageRefreshCanRecoverBeforeAutomaticErrorBackoff(t *testing.T) {
	s, h, c := setup(t, quotaReply)
	if _, err := s.credentialUsage(c.ID, "", true); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	cached := s.usage[c.ID]
	cached.value.Status = "error"
	at := time.Now().Add(-15 * time.Second)
	cached.value.CheckedAt = &at
	s.mu.Unlock()
	h.mu.Lock()
	before := len(h.requests)
	h.mu.Unlock()
	v, err := s.credentialUsage(c.ID, "", true)
	if err != nil || v.Status != "ok" {
		t.Fatalf("explicit retry remained in five-minute backoff: %+v, %v", v, err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.requests) <= before {
		t.Fatal("explicit retry did not reach upstream")
	}
}
