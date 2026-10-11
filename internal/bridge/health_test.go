package bridge

import (
	"strings"
	"testing"
	"time"
)

func TestCredentialHealthObservedState(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name      string
		configure func(*Service, *Credential)
		want      string
	}{
		{"not yet checked", func(s *Service, c *Credential) {}, "unknown"},
		{"disabled", func(s *Service, c *Credential) { c.Disabled = true }, "disabled"},
		{"missing key", func(s *Service, c *Credential) { c.APIKey = "" }, "invalid"},
		{"account success", func(s *Service, c *Credential) {
			s.usage[c.ID] = &usageCache{value: Usage{Status: "ok", CheckedAt: &now, UpdatedAt: &now}}
		}, "healthy"},
		{"account auth failure", func(s *Service, c *Credential) {
			s.usage[c.ID] = &usageCache{value: Usage{Status: "error", HTTPStatus: 401, CheckedAt: &now}}
		}, "invalid"},
		{"transient account failure", func(s *Service, c *Credential) {
			s.usage[c.ID] = &usageCache{value: Usage{Status: "error", HTTPStatus: 503, CheckedAt: &now}}
		}, "warning"},
		{"model permission denied", func(s *Service, c *Credential) { s.logs = []LogEntry{{CredentialID: c.ID, Time: now, Status: 403}} }, "warning"},
		{"request auth failure", func(s *Service, c *Credential) { s.logs = []LogEntry{{CredentialID: c.ID, Time: now, Status: 401}} }, "invalid"},
		{"new request recovers old auth failure", func(s *Service, c *Credential) {
			past := now.Add(-time.Minute)
			s.usage[c.ID] = &usageCache{value: Usage{Status: "error", HTTPStatus: 401, CheckedAt: &past}}
			s.logs = []LogEntry{{CredentialID: c.ID, Time: now, Status: 200}}
		}, "warning"},
		{"success", func(s *Service, c *Credential) { s.logs = []LogEntry{{CredentialID: c.ID, Time: now, Status: 200}} }, "healthy"},
		{"changed key ignores former auth failure", func(s *Service, c *Credential) {
			c.KeyUpdatedAt = &now
			s.logs = []LogEntry{{CredentialID: c.ID, Time: now.Add(-time.Minute), Status: 401}}
		}, "unknown"},
		{"expired cooldown needs a recovery request", func(s *Service, c *Credential) {
			s.logs = []LogEntry{{CredentialID: c.ID, Time: now, Status: 429}}
		}, "warning"},
		{"old logs cannot establish health", func(s *Service, c *Credential) {
			s.logs = []LogEntry{{CredentialID: c.ID, Time: now.Add(-25 * time.Hour), Status: 200}}
		}, "unknown"},
		{"expired account data", func(s *Service, c *Credential) {
			past := now.Add(-time.Hour)
			s.usage[c.ID] = &usageCache{value: Usage{Status: "ok", UpdatedAt: &past, CheckedAt: &past}}
		}, "warning"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewService()
			c := Credential{ID: "fixture", APIKey: "fixture-health-key"}
			tc.configure(s, &c)
			h := s.credentialHealthLocked(c, now)
			if h.Status != tc.want {
				t.Fatalf("want %s, got %+v", tc.want, h)
			}
			if strings.Contains(string(jsonBytes(h)), c.APIKey) && c.APIKey != "" {
				t.Fatal("health leaks API key")
			}
		})
	}
}

func TestCredentialHealthSharedModelCooldown(t *testing.T) {
	s := NewService()
	c := Credential{ID: "first", APIKey: "fixture-health-key"}
	now := time.Now().UTC()
	s.accounts[c.ID] = "shared-account"
	s.cooldown["shared-account|zai/glm-5.3-flash"] = now.Add(time.Minute)
	s.cooldown["other-account|deepseek/model"] = now.Add(time.Hour)
	s.cooldown["shared-account|expired/model"] = now.Add(-time.Minute)
	s.logs = []LogEntry{
		{CredentialID: c.ID, Time: now.Add(-2 * time.Minute), Status: 200},
		{CredentialID: c.ID, Time: now.Add(-time.Minute), Status: 429},
		{CredentialID: c.ID, Time: now, Status: 429, UpstreamSkipped: true},
	}
	h := s.credentialHealthLocked(c, now)
	if h.Status != "cooldown" || len(h.CooldownModels) != 1 || h.CooldownModels[0] != "zai/glm-5.3-flash" || h.RecentRequests != 2 || h.RecentFailed != 1 {
		t.Fatalf("incorrect shared cooldown: %+v", h)
	}
	s.accounts["second"] = "shared-account"
	if s.credentialHealthLocked(Credential{ID: "second", APIKey: "other-key"}, now).Status != "cooldown" {
		t.Fatal("shared credential missed account cooldown")
	}
}

func TestCatalogProviderFreeAliases(t *testing.T) {
	for upstream, want := range map[string]string{"zai/glm-5.3-flash": "glm-5.3-flash", "deepseek/deepseek-v4.1-flash": "deepseek-v4.1-flash", "glm-5.3-flash": "glm-5.3-flash", "provider/group/model": "model"} {
		if got := defaultModelAlias(upstream); got != want {
			t.Fatalf("%s: got %s, want %s", upstream, got, want)
		}
	}
}
