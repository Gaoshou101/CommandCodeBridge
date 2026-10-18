package bridge

import (
	"strings"
	"time"
)

const modelEvidenceTTL = 24 * time.Hour

// Hashes bind observations to a credential version, routing configuration and
// model configuration. They contain no reusable credential or account material.
func credentialEvidenceScope(c Credential, cfg Config) string {
	return shaKey(string(jsonBytes([]any{c.APIKey, c.ProxyURL, cfg.BaseURL, c.KeyUpdatedAt, cfg.ZDR})))
}
func modelEvidenceScope(m Model) string { return shaKey(string(jsonBytes(m))) }

type modelObservation struct {
	Source           string     `json:"source"`
	Status           string     `json:"status"`
	CheckedAt        *time.Time `json:"checked_at,omitempty"`
	HTTPStatus       int        `json:"http_status,omitempty"`
	ErrorKind        string     `json:"error_kind,omitempty"`
	RetryAt          *time.Time `json:"retry_at,omitempty"`
	ClientProtocol   string     `json:"client_protocol,omitempty"`
	UpstreamProtocol string     `json:"upstream_protocol,omitempty"`
	ZDRRequested     bool       `json:"zdr_requested"`
}
type modelAvailability struct {
	Model         string             `json:"model"`
	UpstreamModel string             `json:"upstream_model"`
	CredentialID  string             `json:"credential_id"`
	Status        string             `json:"status"`
	Evidence      []modelObservation `json:"evidence"`
}

func observationStatus(row LogEntry, now time.Time) string {
	if now.Sub(row.Time) > modelEvidenceTTL || row.Time.After(now.Add(time.Minute)) {
		return "expired"
	}
	if row.Status >= 200 && row.Status < 400 {
		return "available"
	}
	if strings.EqualFold(row.ErrorKind, "MODEL_NOT_IN_PLAN") {
		return "not_in_plan"
	}
	switch row.Status {
	case 401:
		return "authentication_error"
	case 403:
		return "permission_unknown"
	case 429:
		return "temporary_limit"
	default:
		return "temporary_error"
	}
}

func (s *Service) modelEvidence(cid string) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now().UTC()
	rows := []modelAvailability{}
	for _, m := range s.cfg.Models {
		c, ok := s.creds[cid]
		item := modelAvailability{Model: m.ID, UpstreamModel: m.UpstreamID, CredentialID: cid, Status: "unknown", Evidence: []modelObservation{}}
		if ok && c.Disabled {
			item.Status = "disabled"
		}
		if ok && !c.Disabled {
			scope, modelScope := credentialEvidenceScope(c, s.cfg), modelEvidenceScope(m)
			var latest time.Time
			for _, source := range []string{"plugin_test", "cpa_request"} {
				observation := modelObservation{Source: source, Status: "unknown"}
				for i := len(s.logs) - 1; i >= 0; i-- {
					log := s.logs[i]
					if log.CredentialID != cid || log.Model != m.ID || log.UpstreamModel != m.UpstreamID || log.Source != source || !log.UpstreamAttempted || log.UpstreamSkipped || log.CredentialScope != scope || log.ModelScope != modelScope || log.AccountScope != s.accounts[cid] {
						continue
					}
					observation = modelObservation{Source: source, Status: observationStatus(log, now), CheckedAt: &log.Time, HTTPStatus: log.Status, ErrorKind: log.ErrorKind, RetryAt: log.RetryAt, ClientProtocol: log.ClientProtocol, UpstreamProtocol: log.UpstreamProtocol, ZDRRequested: log.ZDRRequested}
					if log.Time.After(latest) {
						latest = log.Time
						item.Status = observation.Status
					}
					break
				}
				item.Evidence = append(item.Evidence, observation)
			}
		}
		rows = append(rows, item)
	}
	return map[string]any{"credential_id": cid, "checked_at": now, "expires_after_seconds": int(modelEvidenceTTL.Seconds()), "items": rows, "note": "目录可见不代表套餐有权限；证据来自保留日志，按凭证版本、账户和模型配置隔离，24 小时后过期。只读查询不会生成。"}
}
