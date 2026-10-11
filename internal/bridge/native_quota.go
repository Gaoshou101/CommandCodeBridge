package bridge

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// These shapes follow CPA's QuotaProvider RPC contract. CPAMP discovers the
// provider and renders these fields on its native credential cards.
type quotaMetric struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
}
type quotaBucket struct {
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
	Description       string  `json:"description,omitempty"`
}
type quotaGroup struct {
	DisplayName string        `json:"displayName"`
	Buckets     []quotaBucket `json:"buckets"`
}
type quotaResponse struct {
	Subscription *struct {
		Plan string `json:"plan"`
	} `json:"subscription,omitempty"`
	Summary []quotaMetric `json:"summary,omitempty"`
	Groups  []quotaGroup  `json:"groups,omitempty"`
}

func (s *Service) nativeQuota(raw json.RawMessage) (any, error) {
	var r struct {
		AuthID         string `json:"auth_id"`
		Provider       string `json:"provider"`
		StorageJSON    []byte `json:"storage_json"`
		HostCallbackID string `json:"host_callback_id"`
	}
	if json.Unmarshal(raw, &r) != nil || (r.Provider != "" && r.Provider != Provider) {
		return nil, fail(400, "invalid CommandCode quota request")
	}
	var stored Credential
	_ = json.Unmarshal(r.StorageJSON, &stored)
	s.mu.RLock()
	var c Credential
	for cid, candidate := range s.creds {
		if !s.revoked[cid] && ((stored.Type == Provider && stored.ID == cid) || r.AuthID == cid || r.AuthID == routingID(candidate, s.files[cid])) {
			c = candidate
			break
		}
	}
	s.mu.RUnlock()
	if c.ID == "" {
		return nil, fail(404, "credential not found")
	}
	u, err := s.credentialUsage(c.ID, r.HostCallbackID, true)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	current, exists := s.creds[c.ID]
	changed := !exists || s.revoked[c.ID] || current.Disabled || current.APIKey != c.APIKey || current.ProxyURL != c.ProxyURL
	s.mu.RUnlock()
	if changed || u.Status == "disabled" {
		return nil, fail(409, "credential changed or is disabled; refresh the credential list")
	}
	// Native quota responses have no stale/error field. Do not publish cached
	// failures as fresh observations, or stale sections of partial responses.
	if u.Status == "error" {
		return nil, &APIError{Status: u.HTTPStatus, Kind: "quota_unavailable", Message: u.Error}
	}
	return normalizeQuota(u), nil
}

func normalizeQuota(u Usage) quotaResponse {
	out := quotaResponse{}
	if u.Plan != "" && !u.PlanStale && !contains(u.Unavailable, "subscription") && !contains(u.Unavailable, "credits") {
		out.Subscription = &struct {
			Plan string `json:"plan"`
		}{Plan: u.Plan}
	}
	metric := func(key, label string, value *float64) {
		if value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0 {
			out.Summary = append(out.Summary, quotaMetric{Key: key, Label: label, Value: *value, Unit: "credits"})
		}
	}
	if !contains(u.Unavailable, "credits") {
		metric("remaining_credits", "剩余额度", u.Remaining)
		metric("monthly_credits", "本月剩余", u.Monthly)
		metric("purchased_credits", "购买余额", u.Purchased)
		metric("free_credits", "免费余额", u.Free)
		for _, w := range u.Windows {
			name := map[string]string{"fiveHour": "5 小时窗口", "weekly": "每周窗口", "monthly": "月额度窗口"}[w.Name]
			if name == "" || w.Used == nil || w.Cap == nil || *w.Cap <= 0 || math.IsNaN(*w.Cap) || math.IsInf(*w.Cap, 0) || math.IsNaN(*w.Used) || math.IsInf(*w.Used, 0) {
				continue
			}
			b := quotaBucket{Window: w.Name, RemainingFraction: math.Max(0, math.Min(1, 1 - *w.Used / *w.Cap)), Description: fmt.Sprintf("已用 %g / %g credits", *w.Used, *w.Cap)}
			if w.ResetAt != nil {
				b.ResetTime = w.ResetAt.UTC().Format(time.RFC3339)
			}
			out.Groups = append(out.Groups, quotaGroup{DisplayName: name, Buckets: []quotaBucket{b}})
		}
	}
	if !contains(u.Unavailable, "summary") && !contains(u.Unavailable, "subscription") {
		metric("monthly_used_credits", "本期月额度已用", u.MonthlyUsed)
	}
	if !contains(u.Unavailable, "subscription") && u.SubscriptionEnd != nil {
		out.Summary = append(out.Summary, quotaMetric{Key: "subscription_reset_at", Label: "月额度重置", Value: float64(u.SubscriptionEnd.Unix()), Unit: "unix_seconds"})
	}
	return out
}
