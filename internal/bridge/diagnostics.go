package bridge

import (
	"path/filepath"
	"sort"
	"time"
)

type diagnosticEvidence struct {
	Source     string     `json:"source"`
	Status     string     `json:"status"`
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
	Model      string     `json:"model,omitempty"`
	HTTPStatus int        `json:"http_status,omitempty"`
}

type credentialDiagnostic struct {
	ID            string               `json:"id,omitempty"`
	Label         string               `json:"label,omitempty"`
	Status        string               `json:"status"`
	Message       string               `json:"message"`
	Action        string               `json:"action"`
	Registrations int                  `json:"registrations"`
	Disabled      bool                 `json:"disabled"`
	Cooldown      bool                 `json:"cooldown"`
	Evidence      []diagnosticEvidence `json:"evidence"`
}

type diagnosticReport struct {
	Version                string                 `json:"version"`
	CheckedAt              time.Time              `json:"checked_at"`
	HostStatus             string                 `json:"host_status"`
	HostMessage            string                 `json:"host_message"`
	Models                 int                    `json:"models"`
	UnmatchedRegistrations int                    `json:"unmatched_registrations"`
	Credentials            []credentialDiagnostic `json:"credentials"`
}

// host.auth.list is metadata only. Never use host.auth.get here: its response
// contains credential material and is not needed to compare registrations.
func (s *Service) diagnostics(redacted bool) diagnosticReport {
	entries, err := s.runtimeAuths()
	now := time.Now().UTC()
	report := diagnosticReport{Version: Version, CheckedAt: now, HostStatus: "ok", HostMessage: "已读取 CPA 运行登记；登记一致不代表所有模型均有权限", Credentials: []credentialDiagnostic{}}
	if err != nil {
		report.HostStatus, report.HostMessage = "unknown", "无法读取 CPA 登记，请确认宿主支持 host.auth.list 后重试"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	report.Models = len(s.cfg.Models)
	for _, entry := range entries {
		if entry.Provider == Provider && !runtimeRegistrationKnown(entry) {
			report.HostStatus, report.HostMessage = "unknown", "宿主返回了文件或未标明来源的登记，运行状态尚未确认"
		}
	}
	matched := make(map[int]bool)
	for _, c := range s.creds {
		name := s.files[c.ID]
		d := credentialDiagnostic{ID: c.ID, Label: c.Label, Disabled: c.Disabled, Status: "confirmed", Message: "凭证与 CPA 登记一致", Action: "如调用仍失败，请检查模型权限、客户端配置和请求日志", Evidence: []diagnosticEvidence{}}
		matches := []runtimeAuth{}
		for i, entry := range entries {
			if entry.Provider == Provider && ((name != "" && sameAuthName(entry.Name, name)) || entry.ID == routingID(c, name) || entry.ID == c.ID) {
				matches = append(matches, entry)
				matched[i] = true
			}
		}
		d.Registrations = len(matches)
		switch {
		case err != nil:
			d.Status, d.Message, d.Action = "unknown", "CPA 运行登记尚未确认", "检查宿主连接后重新诊断；保存结果不会因此回滚"
		case len(matches) == 0:
			d.Status, d.Message, d.Action = "missing", "凭证已保存，但 CPA 未登记", "刷新后重新检查；持续缺失时查看 CPA 插件及凭证加载日志，并安排重新加载或重启"
		case !runtimeRegistrationKnown(matches[0]):
			d.Status, d.Message, d.Action = "unknown", "宿主仅提供文件信息，不能确认运行登记", "使用支持运行登记查询的 CPA 宿主后重新检查"
		case len(matches) > 1:
			d.Status, d.Message, d.Action = "duplicate", "同一凭证存在重复或旧登记", "检查会话重新分配结果和 CPA 加载日志，排除旧路由登记后重试"
		case name == "" || matches[0].ID != routingID(c, name) || !sameAuthName(matches[0].Name, name):
			d.Status, d.Message, d.Action = "routing_mismatch", "CPA 登记与当前路由不一致", "刷新诊断；必要时使用重新分配会话并检查宿主确认结果"
		case matches[0].Disabled != c.Disabled:
			d.Status, d.Message, d.Action = "disabled_mismatch", "凭证停用状态尚未同步到 CPA", "等待宿主加载变更后重新诊断，持续不一致时检查 CPA watcher 日志"
		case matches[0].UpdatedAt.Before(s.expectedRegistration[c.ID]) || (c.KeyUpdatedAt != nil && matches[0].UpdatedAt.Before(*c.KeyUpdatedAt)):
			d.Status, d.Message, d.Action = "pending", "凭证已保存，等待 CPA 确认本次变更", "稍后点击重新诊断；若持续等待，检查宿主凭证 watcher 日志"
		case c.Disabled:
			d.Status, d.Message, d.Action = "disabled", "凭证已停用，CPA 已确认", "需要调用时启用凭证，再检查宿主确认状态"
		}
		account := diagnosticEvidence{Source: "account_query", Status: "unknown"}
		if cached := s.usage[c.ID]; cached != nil {
			account.Status, account.CheckedAt, account.HTTPStatus = cached.value.Status, cached.value.CheckedAt, cached.value.HTTPStatus
		}
		d.Evidence = append(d.Evidence, account)
		for _, source := range []string{"plugin_test", "cpa_request"} {
			evidence := diagnosticEvidence{Source: source, Status: "unknown"}
			for i := len(s.logs) - 1; i >= 0; i-- {
				entry := s.logs[i]
				if entry.CredentialID != c.ID || entry.Source != source || entry.UpstreamSkipped || (c.KeyUpdatedAt != nil && entry.Time.Before(*c.KeyUpdatedAt)) {
					continue
				}
				evidence.Status = "success"
				if entry.Status >= 400 {
					evidence.Status = "error"
				}
				evidence.CheckedAt, evidence.HTTPStatus, evidence.Model = &entry.Time, entry.Status, entry.Model
				break
			}
			d.Evidence = append(d.Evidence, evidence)
		}
		d.Cooldown = s.credentialHealthLocked(c, now).Status == "cooldown"
		report.Credentials = append(report.Credentials, d)
	}
	for i, entry := range entries {
		if entry.Provider == Provider && !matched[i] {
			report.UnmatchedRegistrations++
		}
	}
	sort.Slice(report.Credentials, func(i, j int) bool { return report.Credentials[i].ID < report.Credentials[j].ID })
	if redacted {
		for i := range report.Credentials {
			report.Credentials[i].ID, report.Credentials[i].Label = "", ""
			for j := range report.Credentials[i].Evidence {
				report.Credentials[i].Evidence[j].Model = ""
			}
		}
	}
	return report
}

func sameAuthName(left, right string) bool {
	return left != "" && right != "" && routingID(Credential{}, filepath.Base(left)) == routingID(Credential{}, filepath.Base(right))
}

// CPA also labels live records with backing files as source=file. An auth_index
// is emitted by the runtime manager, but never by its disk-only fallback.
func runtimeRegistrationKnown(entry runtimeAuth) bool {
	return entry.Source == "memory" || (entry.AuthIndex != "" && entry.ID != "")
}

// Saving and loading are separate outcomes. A diagnostic failure must not turn
// a completed credential write into an error that encourages duplicate imports.
func (s *Service) credentialSaveDiagnostic(cid string) credentialDiagnostic {
	for _, row := range s.diagnostics(false).Credentials {
		if row.ID == cid {
			return row
		}
	}
	return credentialDiagnostic{Status: "unknown", Message: "凭证已保存，运行登记尚未确认", Action: "刷新凭证并重新诊断"}
}

func evidenceSource(r ExecutorRequest) string {
	if r.pluginTest {
		return "plugin_test"
	}
	return "cpa_request"
}
