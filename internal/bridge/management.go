package bridge

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed ui/*
var ui embed.FS

const apiBase = "/v0/management/commandcodebridge"

func (s *Service) registerManagement() any {
	routes := []map[string]string{}
	for method, paths := range map[string][]string{"GET": {"status", "config", "models", "credentials", "credentials/usage", "logs", "statistics"}, "POST": {"credentials", "auth/complete", "models/refresh", "models/test", "credentials/clear-affinity", "statistics/reset"}, "PUT": {"credentials", "models", "models/reasoning", "config"}, "DELETE": {"credentials"}} {
		for _, p := range paths {
			routes = append(routes, map[string]string{"Method": method, "Path": apiBase + "/" + p})
		}
	}
	return map[string]any{"routes": routes, "resources": []map[string]string{{"Path": "/console", "Menu": "CommandCodeBridge", "Description": "CommandCode 凭据、模型、套餐用量和日志"}, {"Path": "/login"}, {"Path": "/callback"}}}
}
func managementJSON(status int, v any) (any, error) {
	return ManagementResponse{StatusCode: status, Headers: http.Header{"Content-Type": {"application/json; charset=utf-8"}, "Cache-Control": {"no-store"}}, Body: jsonBytes(v)}, nil
}
func queryInt(text string, fallback, min, max int) int {
	v, e := strconv.Atoi(text)
	if e != nil {
		return fallback
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
func (s *Service) management(raw json.RawMessage) (any, error) {
	var r ManagementRequest
	if json.Unmarshal(raw, &r) != nil {
		return managementJSON(400, map[string]string{"error": "invalid management request"})
	}
	if r.Method == "GET" && (r.Path == resourceBase+"/login" || r.Path == resourceBase+"/callback") {
		return s.loginResource(r)
	}
	if r.Method == "GET" && r.Path == "/v0/resource/plugins/"+PluginID+"/console" {
		b, e := ui.ReadFile("ui/index.html")
		if e != nil {
			return nil, fail(500, "console resource missing")
		}
		auth, _ := ui.ReadFile("ui/cpa-auth.js")
		b = []byte(strings.ReplaceAll(string(b), "/*__CPA_AUTH__*/", string(auth)))
		for marker, file := range map[string]string{"/*__CPA_STYLE__*/": "ui/console.css", "/*__CPA_CONSOLE__*/": "ui/console.js"} {
			asset, err := ui.ReadFile(file)
			if err != nil {
				return nil, fail(500, "console asset missing")
			}
			b = []byte(strings.ReplaceAll(string(b), marker, string(asset)))
		}
		icon, _ := ui.ReadFile("ui/commandcode-icon.svg")
		b = []byte(strings.ReplaceAll(string(b), "/*__COMMANDCODE_ICON__*/", string(icon)))
		b = []byte(strings.ReplaceAll(string(b), "/*__COMMANDCODE_FAVICON__*/", "data:image/svg+xml;base64,"+base64.StdEncoding.EncodeToString(icon)))
		return ManagementResponse{StatusCode: 200, Headers: http.Header{"Content-Type": {"text/html; charset=utf-8"}, "Cache-Control": {"no-store"}, "X-Content-Type-Options": {"nosniff"}}, Body: b}, nil
	}
	path := strings.TrimPrefix(r.Path, apiBase)
	var value any
	var e error
	switch r.Method + " " + path {
	case "GET /status":
		s.mu.RLock()
		value = map[string]any{"version": Version, "models": len(s.cfg.Models), "credentials": len(s.creds), "storage_error": s.storageError, "storage_pending": s.persistDirty || s.persistWriting}
		s.mu.RUnlock()
	case "GET /config":
		value = s.config()
	case "GET /models":
		value = map[string]any{"models": s.config().Models}
	case "GET /credentials":
		value = map[string]any{"credentials": s.credentials()}
	case "GET /credentials/usage":
		value, e = s.credentialUsage(r.Query.Get("id"), r.HostCallbackID, r.Query.Get("refresh") == "1")
	case "GET /logs":
		value = s.logView(r)
	case "GET /statistics":
		value = s.statisticsView(r)
	case "POST /credentials", "PUT /credentials":
		value, e = s.editCredential(r)
	case "POST /auth/complete":
		var in struct {
			State  string `json:"state"`
			APIKey string `json:"api_key"`
			Label  string `json:"label"`
		}
		if json.Unmarshal(r.Body, &in) != nil {
			e = fail(400, "invalid login completion request")
		} else {
			e = s.completeLogin(in.State, in.APIKey, in.Label, r.HostCallbackID)
			value = map[string]bool{"success": e == nil}
		}
	case "DELETE /credentials":
		value, e = s.deleteCredential(r.Query.Get("id"))
	case "POST /models/refresh":
		value, e = s.refreshModels(r.HostCallbackID)
	case "PUT /models":
		s.editMu.Lock()
		c := s.config()
		var in struct {
			Models []Model `json:"models"`
		}
		if json.Unmarshal(r.Body, &in) != nil || in.Models == nil {
			e = fail(400, "models must be an array")
		} else {
			c.Models = in.Models
			e = s.saveConfig(c)
			value = map[string]any{"models": c.Models}
		}
		s.editMu.Unlock()
	case "PUT /models/reasoning":
		s.editMu.Lock()
		c := s.config()
		var in struct {
			Model      string           `json:"model"`
			UpstreamID string           `json:"upstream_id"`
			Reasoning  ReasoningMapping `json:"reasoning_mapping"`
		}
		if json.Unmarshal(r.Body, &in) != nil {
			e = fail(400, "invalid reasoning mapping")
		} else {
			found := false
			for i := range c.Models {
				if c.Models[i].ID == in.Model {
					found = true
					if c.Models[i].UpstreamID != in.UpstreamID {
						e = fail(409, "upstream model changed; refresh and retry")
					} else {
						c.Models[i].Reasoning = in.Reasoning
					}
				}
			}
			if !found {
				e = fail(404, "model not found")
			}
			if e == nil {
				e = s.saveConfig(c)
				value = map[string]bool{"saved": e == nil}
			}
		}
		s.editMu.Unlock()
	case "PUT /config":
		s.editMu.Lock()
		c := s.config()
		var in map[string]json.RawMessage
		if json.Unmarshal(r.Body, &in) != nil {
			e = fail(400, "invalid settings JSON")
		} else {
			for k := range in {
				if k != "timeout_seconds" && k != "log_retention" && k != "max_response_bytes" && k != "usage_refresh_seconds" && k != "zdr" {
					e = fail(400, "unsupported editable setting")
					break
				}
			}
			if e == nil {
				e = json.Unmarshal(r.Body, &c)
				if e == nil {
					e = s.saveConfig(c)
					value = c
				}
			}
		}
		s.editMu.Unlock()
	case "POST /models/test":
		value, e = s.testModel(r)
	case "POST /credentials/clear-affinity":
		return s.clearAffinity(r)
	case "POST /statistics/reset":
		s.persistMu.Lock()
		s.mu.Lock()
		old := s.statistics
		next := map[string]Counters{}
		e = atomicJSON(filepath.Join(s.cfg.DataDir, "statistics-backup-"+id()+".json"), old)
		if e == nil {
			e = atomicJSON(filepath.Join(s.cfg.DataDir, "statistics.json"), next)
		}
		if e == nil {
			s.statistics = next
			s.statisticsEpoch++
			s.persistDirty = true
		}
		s.mu.Unlock()
		s.persistMu.Unlock()
		value = map[string]bool{"reset": e == nil}
	default:
		return managementJSON(404, map[string]string{"error": "not found"})
	}
	if e != nil {
		return managementJSON(statusOf(e), map[string]string{"error": safeError(e)})
	}
	return managementJSON(200, value)
}
func (s *Service) editCredential(r ManagementRequest) (any, error) {
	s.editMu.Lock()
	defer s.editMu.Unlock()
	var in struct {
		Label    *string `json:"label"`
		APIKey   *string `json:"api_key"`
		Disabled *bool   `json:"disabled"`
		Priority *int    `json:"priority"`
	}
	if json.Unmarshal(r.Body, &in) != nil {
		return nil, fail(400, "invalid credential JSON")
	}
	c := Credential{Type: Provider, ID: PluginID + "-" + id(), Label: "CommandCode"}
	if r.Method == "PUT" {
		s.mu.RLock()
		found, ok := s.creds[r.Query.Get("id")]
		s.mu.RUnlock()
		if !ok {
			return nil, fail(404, "credential not found")
		}
		c = found
	}
	if in.Label != nil {
		c.Label = strings.TrimSpace(*in.Label)
		if len(c.Label) > 100 {
			return nil, fail(400, "label exceeds 100 characters")
		}
		if c.Label == "" {
			c.Label = "CommandCode"
		}
	}
	if in.APIKey != nil && strings.TrimSpace(*in.APIKey) != "" {
		key := strings.TrimSpace(*in.APIKey)
		if key != c.APIKey {
			now := time.Now().UTC()
			c.KeyUpdatedAt = &now
		}
		c.APIKey = key
	}
	if len(c.APIKey) < 8 || strings.ContainsAny(c.APIKey, "\r\n\t ") {
		return nil, fail(400, "invalid API key")
	}
	if in.Priority != nil {
		if *in.Priority < 0 || *in.Priority > 100 {
			return nil, fail(400, "priority must be between 0 and 100")
		}
		c.Priority = *in.Priority
	}
	if in.Disabled != nil {
		c.Disabled = *in.Disabled
	}
	if e := s.saveCredential(c); e != nil {
		return nil, e
	}
	return map[string]any{"id": c.ID, "label": c.Label, "disabled": c.Disabled, "priority": c.Priority}, nil
}
func (s *Service) deleteCredential(cid string) (any, error) {
	s.editMu.Lock()
	defer s.editMu.Unlock()
	s.mu.RLock()
	c, ok := s.creds[cid]
	name, dir := s.files[cid], s.authDir
	s.mu.RUnlock()
	if !ok {
		return nil, fail(404, "credential not found")
	}
	if dir == "" || name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return nil, fail(409, "credential file location is not resolved")
	}
	path := filepath.Join(dir, name)
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() {
		return nil, fail(409, "credential file is missing or is not regular")
	}
	b, e := os.ReadFile(path)
	var disk Credential
	if e != nil || json.Unmarshal(b, &disk) != nil || disk.Type != Provider || disk.ID != c.ID || disk.APIKey != c.APIKey {
		return nil, fail(409, "credential file ownership or content changed")
	}
	c.Disabled = true
	if err := s.saveCredential(c); err != nil {
		return nil, err
	}
	if err := s.awaitDisabled(name); err != nil {
		return nil, err
	}
	// Preserve an external update made while waiting for the watcher.
	b, e = os.ReadFile(path)
	if e != nil || json.Unmarshal(b, &disk) != nil || disk.ID != c.ID || disk.APIKey != c.APIKey || !disk.Disabled {
		return nil, fail(409, "credential changed while waiting for CPA; retry deletion")
	}
	if err := removeCredentialFile(path, b); err != nil {
		return nil, err
	}
	s.mu.Lock()
	delete(s.creds, cid)
	delete(s.files, cid)
	delete(s.usage, cid)
	delete(s.accounts, cid)
	s.revoked[cid] = true
	s.mu.Unlock()
	return map[string]bool{"deleted": true}, nil
}
func (s *Service) refreshModels(callback string) (any, error) {
	if e := s.begin(); e != nil {
		return nil, e
	}
	defer s.active.Done()
	up, e := s.openUpstream(map[string]any{"host_callback_id": callback, "method": "GET", "url": s.config().BaseURL + "/provider/v1/models"}, time.Now().Add(15*time.Second))
	if e != nil {
		return nil, e
	}
	b, e := s.readJSON(up)
	if e != nil {
		return nil, e
	}
	j, e := decodeObject(b)
	if e != nil {
		return nil, e
	}
	models := []Model{}
	seen := map[string]bool{}
	for _, v := range list(j["data"]) {
		item := object(v)
		up := str(item["id"])
		if up == "" || seen[up] {
			continue
		}
		endpoints := []string{}
		for _, ep := range list(item["supported_endpoints"]) {
			endpoints = append(endpoints, str(ep))
		}
		if !contains(endpoints, "/chat/completions") && !contains(endpoints, "/messages") {
			continue
		}
		mode := ""
		if contains(endpoints, "/messages") && !contains(endpoints, "/chat/completions") {
			mode = "adaptive"
			for _, version := range []string{"3-", "4-0", "4-1", "4-5"} {
				if strings.Contains(up, version) {
					mode = "budget"
				}
			}
		}
		models = append(models, Model{ID: defaultModelAlias(up), UpstreamID: up, Name: str(item["name"]), Endpoints: endpoints, ContextLength: number(item["context_length"]), ThinkingMode: mode, Reasoning: ReasoningMapping{Rules: map[string]string{}}})
		seen[up] = true
	}
	if len(models) == 0 {
		return nil, fail(502, "catalog returned no supported models")
	}
	return map[string]any{"models": models}, nil
}

// Keep the upstream ID intact for routing, while offering a provider-free
// client name. Duplicate aliases are rejected by config validation on import.
func defaultModelAlias(upstream string) string {
	return upstream[strings.LastIndex(upstream, "/")+1:]
}
func (s *Service) testModel(r ManagementRequest) (any, error) {
	var in struct {
		CredentialID string `json:"credential_id"`
		Model        string `json:"model"`
		Reasoning    string `json:"reasoning_effort"`
	}
	if json.Unmarshal(r.Body, &in) != nil {
		return nil, fail(400, "invalid model test")
	}
	s.mu.RLock()
	c, ok := s.creds[in.CredentialID]
	s.mu.RUnlock()
	if !ok || c.Disabled {
		return nil, fail(400, "select an enabled credential")
	}
	payload := map[string]any{"model": in.Model, "messages": []any{map[string]any{"role": "user", "content": "Reply with OK."}}, "max_tokens": 256}
	if in.Reasoning != "" {
		if !efforts[in.Reasoning] || in.Reasoning == "unset" {
			return nil, fail(400, "invalid test reasoning effort")
		}
		payload["reasoning_effort"] = in.Reasoning
	}
	response, e := s.execute(ExecutorRequest{Model: in.Model, StorageJSON: jsonBytes(c), HostCallbackID: r.HostCallbackID, Payload: jsonBytes(payload)}, false)
	if e != nil {
		return nil, e
	}
	out := response.(Response)
	j, e := decodeObject(out.Payload)
	if e != nil {
		return nil, e
	}
	return map[string]any{"response": j}, nil
}

// Decode bytes from host structures without giving the browser a credential copy.
func bytesField(v any) []byte { b, _ := base64.StdEncoding.DecodeString(str(v)); return b }
