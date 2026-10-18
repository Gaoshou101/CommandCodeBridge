package bridge

import (
	"testing"
	"time"
)

func evidenceItem(s *Service, cid string) modelAvailability {
	return s.modelEvidence(cid).(map[string]any)["items"].([]modelAvailability)[0]
}

func TestModelEvidenceScopesExpiryAndSources(t *testing.T) {
	s, _, c := setup(t, func(string, map[string]any) mockReply { return streamReply(chatStream("ok")) })
	m := s.config().Models[0]
	row := s.newLog(ExecutorRequest{Model: m.ID, pluginTest: true}, c, m, time.Now())
	row.UpstreamAttempted = true
	row.Status = 200
	s.record(row)
	if item := evidenceItem(s, c.ID); item.Status != "available" || item.Evidence[0].Status != "available" || item.Evidence[1].Status != "unknown" {
		t.Fatal(item)
	}
	s.mu.Lock()
	s.logs[0].Time = time.Now().Add(-25 * time.Hour)
	s.mu.Unlock()
	if evidenceItem(s, c.ID).Status != "expired" {
		t.Fatal("old success not expired")
	}
	s.mu.Lock()
	s.logs[0].Time = time.Now()
	s.accounts[c.ID] = "new-account"
	s.mu.Unlock()
	if evidenceItem(s, c.ID).Status != "unknown" {
		t.Fatal("account isolation failed")
	}
	s.mu.Lock()
	delete(s.accounts, c.ID)
	s.cfg.Models[0].UpstreamID = "changed"
	s.mu.Unlock()
	if evidenceItem(s, c.ID).Status != "unknown" {
		t.Fatal("mapping isolation failed")
	}
	s.mu.Lock()
	s.cfg.Models[0] = m
	s.creds[c.ID] = Credential{ID: c.ID, APIKey: "replacement"}
	s.mu.Unlock()
	if evidenceItem(s, c.ID).Status != "unknown" {
		t.Fatal("key isolation failed")
	}
}

func TestModelEvidenceFailureClassificationAndLocalSkips(t *testing.T) {
	for _, tc := range []struct {
		code       int
		kind, want string
	}{{403, "MODEL_NOT_IN_PLAN", "not_in_plan"}, {403, "permission_error", "permission_unknown"}, {429, "rate_limit_error", "temporary_limit"}, {503, "upstream_error", "temporary_error"}, {401, "authentication_error", "authentication_error"}} {
		t.Run(tc.want, func(t *testing.T) {
			s, _, c := setup(t, func(string, map[string]any) mockReply { return streamReply(chatStream("ok")) })
			m := s.config().Models[0]
			row := s.newLog(ExecutorRequest{Model: m.ID}, c, m, time.Now())
			row.Status = tc.code
			row.ErrorKind = tc.kind
			row.UpstreamAttempted = true
			s.record(row)
			if item := evidenceItem(s, c.ID); item.Status != tc.want || item.Evidence[1].Status != tc.want {
				t.Fatal(item)
			}
			row.Status = 200
			row.UpstreamSkipped = true
			s.record(row)
			if evidenceItem(s, c.ID).Status != tc.want {
				t.Fatal("local skip replaced real result")
			}
		})
	}
}
