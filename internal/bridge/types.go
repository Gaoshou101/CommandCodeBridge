package bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const Version = "0.2.8"
const PluginID = "commandcodebridge"
const Provider = "commandcode"

// CPA requires both fields. Other source repositories can override the URL
// with -ldflags -X when building.
var Author = "CommandCodeBridge contributors"
var Repository = "https://github.com/Gaoshou101/CommandCodeBridge"

type APIError struct {
	Status        int
	Kind, Message string
	RetryAt       time.Time
	Local         bool
}

func (e *APIError) Error() string   { return e.Message }
func (e *APIError) StatusCode() int { return e.Status }
func (e *APIError) Code() string    { return e.Kind }
func fail(status int, message string) error {
	return &APIError{Status: status, Kind: "invalid_request", Message: message}
}

type HostCall func(string, any, any) error
type ExecutorRequest struct {
	AuthID, AuthProvider, Model, Format, SourceFormat, Alt string
	Stream                                                 bool
	Headers                                                http.Header
	Query                                                  url.Values
	OriginalRequest, Payload, StorageJSON                  []byte
	Metadata, AuthMetadata                                 map[string]any
	AuthAttributes                                         map[string]string
	StreamID                                               string `json:"stream_id"`
	HostCallbackID                                         string `json:"host_callback_id"`
}
type Response struct {
	Payload  []byte
	Headers  http.Header
	Metadata map[string]any `json:",omitempty"`
}
type ManagementRequest struct {
	Method, Path   string
	Headers        http.Header
	Query          url.Values
	Body           []byte
	HostCallbackID string `json:"host_callback_id"`
}
type ManagementResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}
type Credential struct {
	Type          string     `json:"type"`
	ID            string     `json:"id"`
	Label         string     `json:"label"`
	APIKey        string     `json:"api_key"`
	KeyUpdatedAt  *time.Time `json:"key_updated_at,omitempty"`
	Disabled      bool       `json:"disabled"`
	Priority      int        `json:"priority"`
	ModelRevision string     `json:"model_revision,omitempty"`
	RoutingID     string     `json:"routing_id,omitempty"`
	ProxyURL      string     `json:"proxy_url,omitempty"`
}

func priorityValue(c Credential) int { return c.Priority }

type ReasoningMapping struct {
	Enabled bool              `json:"enabled" yaml:"enabled"`
	Rules   map[string]string `json:"rules" yaml:"rules"`
}
type Model struct {
	ID            string           `json:"id" yaml:"id"`
	UpstreamID    string           `json:"upstream_id" yaml:"upstream_id"`
	Name          string           `json:"name,omitempty" yaml:"name,omitempty"`
	Endpoints     []string         `json:"supported_endpoints" yaml:"supported_endpoints"`
	ContextLength int64            `json:"context_length,omitempty" yaml:"context_length,omitempty"`
	ThinkingMode  string           `json:"thinking_mode,omitempty" yaml:"thinking_mode,omitempty"`
	Reasoning     ReasoningMapping `json:"reasoning_mapping" yaml:"reasoning_mapping"`
}
type Config struct {
	DataDir             string  `json:"data_dir" yaml:"data_dir"`
	BaseURL             string  `json:"base_url" yaml:"base_url"`
	PublicBaseURL       string  `json:"public_base_url,omitempty" yaml:"public_base_url"`
	CPAMPInstallDir     string  `json:"cpamp_install_dir" yaml:"cpamp_install_dir"`
	CPAMPConfigPath     string  `json:"cpamp_config_path" yaml:"cpamp_config_path"`
	CPAMPPanelPath      string  `json:"cpamp_panel_path" yaml:"cpamp_panel_path"`
	Models              []Model `json:"models" yaml:"models"`
	TimeoutSeconds      int     `json:"timeout_seconds" yaml:"timeout_seconds"`
	LogRetention        int     `json:"log_retention" yaml:"log_retention"`
	MaxResponseBytes    int     `json:"max_response_bytes" yaml:"max_response_bytes"`
	UsageRefreshSeconds int     `json:"usage_refresh_seconds" yaml:"usage_refresh_seconds"`
	ZDR                 bool    `json:"zdr" yaml:"zdr"`
}

func defaultConfig() Config {
	return Config{DataDir: "plugins/commandcodebridge-data", BaseURL: "https://api.commandcode.ai", Models: []Model{}, TimeoutSeconds: 600, LogRetention: 1000, MaxResponseBytes: 16 << 20, UsageRefreshSeconds: 60}
}

var efforts = map[string]bool{"unset": true, "none": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true, "ultra": true}

func (c *Config) validate() error {
	for _, path := range []string{c.CPAMPInstallDir, c.CPAMPConfigPath, c.CPAMPPanelPath} {
		if strings.ContainsAny(path, "\x00\r\n") || len(path) > 4096 {
			return fail(400, "invalid CPAMP path")
		}
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return fail(400, "base_url must be an HTTP(S) origin without credentials or a path")
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.PublicBaseURL != "" {
		publicURL, err := url.Parse(c.PublicBaseURL)
		if err != nil || publicURL.Host == "" || (publicURL.Scheme != "https" && publicURL.Scheme != "http") || publicURL.User != nil || publicURL.RawQuery != "" || publicURL.Fragment != "" {
			return fail(400, "public_base_url must be an HTTP(S) URL without credentials, query or fragment")
		}
		c.PublicBaseURL = strings.TrimRight(c.PublicBaseURL, "/")
	}
	if c.DataDir == "" || c.TimeoutSeconds < 10 || c.TimeoutSeconds > 1800 || c.LogRetention < 10 || c.LogRetention > 10000 || c.MaxResponseBytes < 65536 || c.MaxResponseBytes > 64<<20 || c.UsageRefreshSeconds < 30 || c.UsageRefreshSeconds > 3600 {
		return fail(400, "invalid data directory, timeout, retention, response limit or refresh interval")
	}
	seen := map[string]bool{}
	for i := range c.Models {
		m := &c.Models[i]
		m.ID = strings.TrimSpace(m.ID)
		m.UpstreamID = strings.TrimSpace(m.UpstreamID)
		if m.ID == "" || m.UpstreamID == "" || len(m.ID) > 200 || len(m.UpstreamID) > 200 || strings.ContainsAny(m.ID+m.UpstreamID, "\r\n\t ") || seen[m.ID] {
			return fail(400, "model aliases must be unique and have a valid upstream ID")
		}
		seen[m.ID] = true
		if len(m.Endpoints) == 0 {
			return fail(400, "select supported_endpoints for every model")
		}
		for _, ep := range m.Endpoints {
			if ep != "/chat/completions" && ep != "/messages" && ep != "/responses" {
				return fail(400, "unsupported model endpoint")
			}
		}
		if !contains(m.Endpoints, "/chat/completions") && !contains(m.Endpoints, "/messages") {
			return fail(400, "this model has no supported chat endpoint")
		}
		if m.ThinkingMode != "" && m.ThinkingMode != "adaptive" && m.ThinkingMode != "budget" {
			return fail(400, "thinking_mode must be adaptive or budget")
		}
		for from, to := range m.Reasoning.Rules {
			if !efforts[from] || !efforts[to] {
				return fail(400, "invalid reasoning mapping")
			}
		}
	}
	return nil
}
func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
func jsonBytes(v any) []byte      { b, _ := json.Marshal(v); return b }
func str(v any) string            { s, _ := v.(string); return s }
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func number(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return i
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}
func numeric(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, e := n.Float64()
		return f, e == nil
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}
func decodeObject(b []byte) (map[string]any, error) {
	var j map[string]any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(&j) != nil || j == nil {
		return nil, fail(400, "invalid JSON object")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, fail(400, "trailing JSON data")
	}
	return j, nil
}
func errorMessage(j map[string]any) string {
	if e := object(j["error"]); e != nil {
		if m := str(e["message"]); m != "" {
			return m
		}
	}
	if m := str(j["error"]); m != "" {
		return m
	}
	if m := str(j["message"]); m != "" {
		return m
	}
	return "upstream request failed"
}
func statusOf(e error) int {
	if e == nil {
		return 200
	}
	if s, ok := e.(interface{ StatusCode() int }); ok && s.StatusCode() > 0 {
		return s.StatusCode()
	}
	return 502
}

var secretPattern = regexp.MustCompile(`(?i)(?:bearer\s+|sk[-_]|user_)[a-z0-9_.-]+`)

func safeError(e error) string {
	if e == nil {
		return ""
	}
	s := secretPattern.ReplaceAllString(e.Error(), "[REDACTED]")
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}
func redactError(e error, key string) error {
	if e == nil {
		return nil
	}
	msg := e.Error()
	if key != "" {
		msg = strings.ReplaceAll(msg, key, "[REDACTED]")
	}
	if api, ok := e.(*APIError); ok {
		copy := *api
		copy.Message = secretPattern.ReplaceAllString(msg, "[REDACTED]")
		return &copy
	}
	return &APIError{Status: statusOf(e), Kind: "upstream_error", Message: secretPattern.ReplaceAllString(msg, "[REDACTED]")}
}
func applyReasoning(body map[string]any, m Model) (string, string) {
	from := str(body["reasoning_effort"])
	if from == "" {
		from = "unset"
	}
	to := from
	if m.Reasoning.Enabled {
		if mapped, ok := m.Reasoning.Rules[from]; ok {
			to = mapped
			if to == "unset" {
				delete(body, "reasoning_effort")
			} else if from != "unset" || (body["thinking"] == nil && body["reasoning"] == nil) {
				body["reasoning_effort"] = to
			} else {
				to = from
			}
		}
	}
	return from, to
}
func upstreamHeaders(c Credential, zdr bool) http.Header {
	h := http.Header{"Authorization": {"Bearer " + c.APIKey}, "Content-Type": {"application/json"}, "User-Agent": {"CommandCodeBridge/" + Version}, "Anthropic-Version": {"2023-06-01"}}
	if zdr {
		h.Set("X-Cmd-Zdr", "1")
	}
	return h
}
func completionChunk(id, model string, delta any, finish any, usage any) map[string]any {
	j := map[string]any{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
	if usage != nil {
		j["usage"] = usage
	}
	return j
}
func require(ok bool, msg string) error {
	if !ok {
		return fail(400, msg)
	}
	return nil
}
func unknownFormat(v any) error {
	return fail(400, fmt.Sprintf("unsupported content type %q", str(object(v)["type"])))
}
