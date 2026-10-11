package bridge

import (
	"sort"
	"strings"
	"time"
)

// CredentialHealth reports observed account and request state. A successful
// account query alone does not establish access to every catalog model.
type CredentialHealth struct {
	Status         string     `json:"status"`
	Reason         string     `json:"reason"`
	CheckedAt      *time.Time `json:"checked_at,omitempty"`
	LastRequestAt  *time.Time `json:"last_request_at,omitempty"`
	LastStatus     int        `json:"last_status,omitempty"`
	LastModel      string     `json:"last_model,omitempty"`
	CooldownUntil  *time.Time `json:"cooldown_until,omitempty"`
	CooldownModels []string   `json:"cooldown_models"`
	RecentRequests int        `json:"recent_requests"`
	RecentFailed   int        `json:"recent_failed"`
}

// Caller holds s.mu. Counts refer to retained logs from the past 24 hours.
func (s *Service) credentialHealthLocked(c Credential, now time.Time) CredentialHealth {
	h := CredentialHealth{Status: "unknown", Reason: "等待账户查询或模型请求", CooldownModels: []string{}}
	var latest *LogEntry
	for i := len(s.logs) - 1; i >= 0; i-- {
		entry := &s.logs[i]
		if entry.CredentialID != c.ID || entry.Time.Before(now.Add(-24*time.Hour)) || (c.KeyUpdatedAt != nil && entry.Time.Before(*c.KeyUpdatedAt)) {
			continue
		}
		if latest == nil || entry.Time.After(latest.Time) {
			latest = entry
		}
		if !entry.UpstreamSkipped {
			h.RecentRequests++
			if entry.Status >= 400 {
				h.RecentFailed++
			}
		}
	}
	if latest != nil {
		h.LastRequestAt, h.LastStatus, h.LastModel = &latest.Time, latest.Status, latest.Model
	}
	if c.Disabled {
		h.Status, h.Reason = "disabled", "凭据已停用"
		return h
	}
	if c.APIKey == "" {
		h.Status, h.Reason = "invalid", "缺少 API key"
		return h
	}
	var usage *Usage
	if cached := s.usage[c.ID]; cached != nil {
		usage = &cached.value
		h.CheckedAt = usage.CheckedAt
	}
	if usage != nil && usage.Status == "error" && (usage.HTTPStatus == 401 || usage.HTTPStatus == 403) &&
		(latest == nil || usage.CheckedAt == nil || !latest.Time.After(*usage.CheckedAt)) {
		h.Status, h.Reason = "invalid", "账户查询鉴权失败，请检查密钥或权限"
		return h
	}
	if latest != nil && latest.Status == 401 && (usage == nil || usage.CheckedAt == nil || !usage.CheckedAt.After(latest.Time)) {
		h.Status, h.Reason = "invalid", "最近模型请求鉴权失败"
		return h
	}
	account := s.accounts[c.ID]
	if account == "" {
		account = shaKey(c.APIKey)
	}
	for key, until := range s.cooldown {
		if strings.HasPrefix(key, account+"|") && (until.After(now) || s.probes[key]) {
			h.CooldownModels = append(h.CooldownModels, strings.TrimPrefix(key, account+"|"))
			if h.CooldownUntil == nil || until.After(*h.CooldownUntil) {
				t := until
				h.CooldownUntil = &t
			}
		}
	}
	if len(h.CooldownModels) > 0 {
		sort.Strings(h.CooldownModels)
		h.Status, h.Reason = "cooldown", "部分模型限流冷却中，其他模型仍可请求"
		return h
	}
	if latest != nil && latest.Status == 429 {
		h.Status, h.Reason = "warning", "限流冷却已结束，等待新请求确认恢复"
		return h
	}
	if latest != nil && latest.Status >= 400 && latest.Status != 429 {
		h.Status, h.Reason = "warning", "最近模型请求失败，请查看请求日志"
		if latest.Status == 403 {
			h.Reason = "最近模型请求被拒绝，请检查套餐模型权限"
		}
		return h
	}
	if usage != nil {
		if usage.Status == "error" || usage.Status == "partial" {
			h.Status, h.Reason = "warning", "账户数据查询异常，部分数据可能已过期"
			return h
		}
		if usage.UpdatedAt != nil && now.Sub(*usage.UpdatedAt) > time.Duration(s.cfg.UsageRefreshSeconds*2)*time.Second {
			h.Status, h.Reason = "warning", "账户数据已过期，等待刷新"
			return h
		}
		if usage.Status == "ok" {
			h.Status, h.Reason = "healthy", "账户查询正常，模型权限以实际请求为准"
		}
	}
	if latest != nil && latest.Status >= 200 && latest.Status < 400 {
		h.Status, h.Reason = "healthy", "最近模型请求成功"
	}
	return h
}
