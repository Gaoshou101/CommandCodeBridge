package bridge

import (
	"strings"
	"testing"
)

func TestMonthlyUsageUsesBillingPeriodAndRealFields(t *testing.T) {
	for _, tc := range []struct {
		name, subscription, credits string
		wantUsed                    bool
		wantWindows                 int
	}{
		{"billing period", `{"data":{"planId":"pro","currentPeriodStart":"2026-10-01T00:00:00Z","currentPeriodEnd":"2026-11-01T00:00:00Z"}}`, "", true, 2},
		{"no billing start", `{"data":{"planId":"pro","currentPeriodEnd":"2026-11-01T00:00:00Z"}}`, "", false, 2},
		{"native monthly window", `{"data":{"planId":"pro","currentPeriodStart":"2026-10-01T00:00:00Z"}}`, `{"credits":{"monthlyCredits":5,"purchasedCredits":2,"freeCredits":0},"windowLimits":{"monthly":{"used":25,"cap":30,"resetAt":1900000000}}}`, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, c := setup(t, func(path string, in map[string]any) mockReply {
				r := quotaReply(path, in)
				if strings.Contains(path, "subscriptions") {
					r.body = tc.subscription
				}
				if strings.Contains(path, "credits") && tc.credits != "" {
					r.body = tc.credits
				}
				if strings.Contains(path, "/summary") {
					if tc.wantUsed && !strings.Contains(path, "since=2026-10-01") {
						t.Error("summary was not limited to subscription period")
					}
					r.body = `{"totalCount":12,"totalMonthlyCredits":0.24}`
				}
				return r
			})
			u, err := s.credentialUsage(c.ID, "", true)
			if err != nil {
				t.Fatal(err)
			}
			if (u.MonthlyUsed != nil) != tc.wantUsed || len(u.Windows) != tc.wantWindows {
				t.Fatalf("incorrect monthly data: %+v", u)
			}
			if tc.wantUsed && *u.MonthlyUsed != 0.24 {
				t.Fatal("monthly usage differs from account response")
			}
			if tc.name == "native monthly window" && (u.Windows[0].Name != "monthly" || *u.Windows[0].Cap != 30) {
				t.Fatal("native monthly window lost")
			}
		})
	}
}
