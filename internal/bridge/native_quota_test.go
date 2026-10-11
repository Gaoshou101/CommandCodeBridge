package bridge

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestNativeQuotaOmitsCachedPlanWhenUpstreamOmitsPlanID(t *testing.T) {
	s, h, c := setup(t, quotaReply)
	u, err := s.credentialUsage(c.ID, "", true)
	if err != nil || u.Plan != "pro" {
		t.Fatalf("initial plan not observed: %+v, %v", u, err)
	}
	h.reply = func(path string, request map[string]any) mockReply {
		reply := quotaReply(path, request)
		reply.body = strings.ReplaceAll(reply.body, `"planId":"pro",`, "")
		reply.body = strings.ReplaceAll(reply.body, `,"planId":"pro"`, "")
		return reply
	}
	s.mu.Lock()
	s.usage[c.ID].value.CheckedAt = nil
	s.mu.Unlock()
	v, err := s.Handle("quota.fetch", jsonBytes(map[string]string{"auth_id": c.ID}))
	if err != nil {
		t.Fatal(err)
	}
	q := v.(quotaResponse)
	if q.Subscription != nil || len(q.Summary) == 0 {
		t.Fatalf("missing upstream plan published cached plan or lost fresh balances: %+v", q)
	}
}

func TestNativeQuota(t *testing.T) {
	s, h, c := setup(t, quotaReply)
	registration := registration().(map[string]any)
	if registration["capabilities"].(map[string]any)["quota_provider"] != true {
		t.Fatal("native quota provider not advertised")
	}
	v, err := s.Handle("quota.fetch", jsonBytes(map[string]any{"provider": Provider, "auth_id": c.ID + ".json", "host_callback_id": "native-callback"}))
	if err != nil {
		t.Fatal(err)
	}
	q := v.(quotaResponse)
	if q.Subscription == nil || q.Subscription.Plan != "pro" || len(q.Groups) != 2 || q.Groups[0].Buckets[0].RemainingFraction != .6 || q.Groups[1].Buckets[0].RemainingFraction != .7 {
		t.Fatalf("bad native quota: %+v", q)
	}
	if bytes.Contains(jsonBytes(q), []byte(c.APIKey)) || bytes.Contains(jsonBytes(q), []byte(c.ID)) {
		t.Fatal("native quota leaked a credential")
	}
	h.mu.Lock()
	for _, request := range h.requests {
		if str(request["host_callback_id"]) != "native-callback" {
			t.Fatal("native quota lost host callback transport")
		}
	}
	h.mu.Unlock()
	s.mu.Lock()
	s.usage[c.ID].value.Status = "error"
	s.usage[c.ID].value.HTTPStatus = 401
	s.usage[c.ID].value.Error = "invalid credential"
	now := time.Now()
	s.usage[c.ID].value.CheckedAt = &now
	s.mu.Unlock()
	if _, err = s.Handle("quota.fetch", jsonBytes(map[string]any{"auth_id": c.ID})); statusOf(err) != 401 {
		t.Fatal("cached quota failure presented as fresh data")
	}
	if _, err = s.Handle("quota.reset", nil); statusOf(err) != 501 {
		t.Fatal("unsupported quota reset accepted")
	}
}

func TestNativeQuotaUnknownAndPartial(t *testing.T) {
	f := func(n float64) *float64 { return &n }
	u := Usage{Plan: "pro", Monthly: f(40), Remaining: f(45), MonthlyUsed: f(10), Windows: []UsageWindow{{Name: "fiveHour", Used: f(2), Cap: f(5)}, {Name: "weekly", Used: f(0)}, {Name: "monthly", Used: f(3), Cap: f(0)}}}
	q := normalizeQuota(u)
	if len(q.Groups) != 1 || len(q.Summary) != 3 {
		t.Fatalf("unknown caps fabricated a percentage: %+v", q)
	}
	u.Unavailable = []string{"credits"}
	q = normalizeQuota(u)
	if len(q.Groups) != 0 || len(q.Summary) != 1 || q.Subscription != nil {
		t.Fatalf("stale credits published by partial response: %+v", q)
	}
	u.Unavailable = []string{"summary", "subscription"}
	q = normalizeQuota(u)
	for _, m := range q.Summary {
		if m.Key == "monthly_used_credits" {
			t.Fatal("stale monthly summary published by partial response")
		}
	}
}
