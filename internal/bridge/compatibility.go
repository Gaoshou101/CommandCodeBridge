package bridge

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

//go:embed compatibility.json
var compatibilityBaseline json.RawMessage

type compatibilityCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Action  string `json:"action,omitempty"`
}

type compatibilityReport struct {
	CheckedAt time.Time            `json:"checked_at"`
	Version   string               `json:"version"`
	Baseline  json.RawMessage      `json:"baseline"`
	Checks    []compatibilityCheck `json:"checks"`
	Panel     nativePanelStatus    `json:"panel"`
}

func (s *Service) compatibility() compatibilityReport {
	return buildCompatibility(inspectNativePanel(s.config()))
}

func buildCompatibility(panel nativePanelStatus) compatibilityReport {
	r := compatibilityReport{CheckedAt: time.Now().UTC(), Version: Version, Baseline: compatibilityBaseline, Panel: panel}
	r.Checks = []compatibilityCheck{
		{Name: "plugin", Status: "ok", Message: "当前插件 " + Version + " 提供诊断、额度缓存和最近请求管理接口"},
		{Name: "host_version", Status: "unknown", Message: "宿主 RPC 未提供运行版本", Action: "核对 CPA 版本；已验证基线为 8.0.23，较新版本以兼容任务结果为准"},
		{Name: "page", Status: "unknown", Message: "未确认实际服务页面", Action: "同机填写实际路径后检查；异机或容器请在 Manager Server 所在环境检查页面与缓存"},
		{Name: "page_quota", Status: "unknown", Message: "未确认页面额度组件"},
		{Name: "page_recent_requests", Status: "unknown", Message: "未确认页面最近请求同步能力"},
		{Name: "page_windows", Status: "unknown", Message: "未确认页面周期历史组件"},
		{Name: "snapshot_backend", Status: "unknown", Message: "未确认 Manager Server 的 CommandCode 快照能力", Action: "在 Manager Server 运行环境应用匹配源码补丁、重建并重启；页面更新只替换 HTML"},
		{Name: "browser_cache", Status: "unknown", Message: "HTTP 核验不代表当前浏览器已载入新页面", Action: "强制刷新浏览器后核对版本"},
	}
	if panel.ServedPage.Checked {
		r.Checks[2].Status = "mismatch"
		r.Checks[2].Message = "服务页面与内嵌页面不同；已识别版本：" + panel.ServedPage.Version
		r.Checks[2].Action = "备份现有页面，更新原生面板后重新检查实际 HTTP 页面"
		if panel.ServedPage.Matches {
			r.Checks[2].Status, r.Checks[2].Message = "ok", "实际 HTTP 页面与插件内嵌页面摘要一致"
			r.Checks[2].Action = "强制刷新浏览器后使用"
		}
		for i, enabled := range []bool{panel.ServedPage.NativeQuota, panel.ServedPage.RecentRequests, panel.ServedPage.NativeWindows} {
			r.Checks[i+3].Status = "missing"
			r.Checks[i+3].Action = "同时更新插件与匹配的 CPAMP 页面"
			if enabled {
				r.Checks[i+3].Status, r.Checks[i+3].Message, r.Checks[i+3].Action = "ok", "实际页面含对应能力标识；接口由当前插件提供", ""
			}
		}
	}
	if panel.nativePanelTarget != nil {
		if supported, checked := observeSnapshotCapability(panel.nativePanelTarget); checked {
			r.Checks[6].Status, r.Checks[6].Message, r.Checks[6].Action = "missing", "Manager Server 明确报告缺少 CommandCode 快照能力", "应用匹配版本的 Manager Server 快照补丁并重建、重启"
			if supported {
				r.Checks[6].Status, r.Checks[6].Message, r.Checks[6].Action = "ok", "Manager Server 只读健康接口报告支持 CommandCode 快照", ""
			}
		}
	}
	return r
}

var panelVersionPattern = regexp.MustCompile(`CommandCodeBridge v([0-9]+\.[0-9]+\.[0-9]+)`)

func annotatePanel(observation *panelObservation, actual []byte) {
	if match := panelVersionPattern.FindSubmatch(actual); len(match) == 2 {
		observation.Version = string(match[1])
	}
	observation.NativeWindows = bytes.Contains(actual, []byte("data-commandcode-native-windows"))
	observation.RecentRequests = bytes.Contains(actual, []byte("commandcodebridge-capabilities")) && bytes.Contains(actual, []byte("recent-requests"))
}

// Read a dedicated anonymous capability assertion. Old health responses stay
// unknown; a successful generic health response cannot prove snapshot support.
func observeSnapshotCapability(target *nativePanelTarget) (bool, bool) {
	address := panelLoopbackAddress(target.address)
	if address == "" {
		return false, false
	}
	client := http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://" + address + "/health")
	if err != nil {
		return false, false
	}
	defer response.Body.Close()
	var health struct {
		Capabilities map[string]*bool `json:"capabilities"`
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(body) > 65536 || response.StatusCode != http.StatusOK || json.Unmarshal(body, &health) != nil {
		return false, false
	}
	value := health.Capabilities["commandcode_quota_snapshots_v1"]
	if value == nil {
		return false, false
	}
	return *value, true
}

func panelLoopbackAddress(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return ""
	}
	if host == "0.0.0.0" || host == "" || host == "localhost" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		return ""
	}
	return net.JoinHostPort(host, port)
}
