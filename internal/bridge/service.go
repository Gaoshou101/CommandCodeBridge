package bridge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type Service struct {
	mu                                           sync.RWMutex
	editMu                                       sync.Mutex
	host                                         HostCall
	cfg                                          Config
	creds                                        map[string]Credential
	files                                        map[string]string
	revoked                                      map[string]bool
	authDir                                      string
	loaded, stopped, samplerStarted              bool
	stopCh                                       chan struct{}
	active                                       sync.WaitGroup
	streams                                      map[string]bool
	logs                                         []LogEntry
	statistics                                   map[string]Counters
	statisticsEpoch                              uint64
	storageError                                 string
	persistMu                                    sync.Mutex
	persistDirty, persistRunning, persistWriting bool
	persistStop, persistDone                     chan struct{}
	usage                                        map[string]*usageCache
	accounts                                     map[string]string
	cooldown                                     map[string]time.Time
	probes                                       map[string]bool
	logins                                       map[string]*loginSession
}

func NewService() *Service {
	return &Service{cfg: defaultConfig(), creds: map[string]Credential{}, files: map[string]string{}, revoked: map[string]bool{}, stopCh: make(chan struct{}), streams: map[string]bool{}, statistics: map[string]Counters{}, usage: map[string]*usageCache{}, accounts: map[string]string{}, cooldown: map[string]time.Time{}, probes: map[string]bool{}, logins: map[string]*loginSession{}}
}
func (s *Service) SetHost(h HostCall) { s.mu.Lock(); s.host = h; s.mu.Unlock() }
func (s *Service) call(method string, in, out any) error {
	s.mu.RLock()
	h := s.host
	s.mu.RUnlock()
	if h == nil {
		return errors.New("host callback is not initialized")
	}
	return h(method, in, out)
}
func (s *Service) config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var c Config
	_ = json.Unmarshal(jsonBytes(s.cfg), &c)
	return c
}
func id() string {
	b := make([]byte, 12)
	if _, e := rand.Read(b); e != nil {
		panic("random source unavailable")
	}
	return hex.EncodeToString(b)
}
func atomicJSON(path string, v any) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	tmp := path + ".tmp-" + id()
	if e = os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	defer os.Remove(tmp)
	return os.Rename(tmp, path)
}
func (s *Service) configure(raw json.RawMessage) error {
	s.editMu.Lock()
	defer s.editMu.Unlock()
	var r struct {
		ConfigYAML []byte `json:"config_yaml"`
	}
	if e := json.Unmarshal(raw, &r); e != nil {
		return fail(400, "invalid registration request")
	}
	c := defaultConfig()
	var cpampPaths struct {
		Install string `yaml:"cpamp_install_dir"`
		Config  string `yaml:"cpamp_config_path"`
		Panel   string `yaml:"cpamp_panel_path"`
	}
	if len(r.ConfigYAML) > 0 {
		if e := yaml.Unmarshal(r.ConfigYAML, &c); e != nil {
			return fail(400, "invalid plugin YAML")
		}
		_ = yaml.Unmarshal(r.ConfigYAML, &cpampPaths)
	}
	if b, e := os.ReadFile(filepath.Join(c.DataDir, "settings.json")); e == nil {
		if json.Unmarshal(b, &c) != nil {
			return fail(500, "stored settings are invalid")
		}
	}
	// Explicit nonempty host configuration paths remain authoritative even
	// when the console has already persisted model/request settings.
	if cpampPaths.Install != "" {
		c.CPAMPInstallDir = cpampPaths.Install
	}
	if cpampPaths.Config != "" {
		c.CPAMPConfigPath = cpampPaths.Config
	}
	if cpampPaths.Panel != "" {
		c.CPAMPPanelPath = cpampPaths.Panel
	}
	if e := c.validate(); e != nil {
		return e
	}
	if e := os.MkdirAll(c.DataDir, 0700); e != nil {
		return fail(500, "cannot create plugin data directory")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded && c.DataDir != s.cfg.DataDir {
		return fail(409, "changing data_dir requires restarting the plugin")
	}
	if s.stopped {
		if s.persistDone != nil {
			select {
			case <-s.persistDone:
			default:
				return fail(409, "plugin shutdown is still in progress")
			}
		}
		s.stopped = false
		s.stopCh = make(chan struct{})
		s.samplerStarted = false
	}
	s.cfg = c
	if !s.loaded {
		if b, e := os.ReadFile(filepath.Join(c.DataDir, "requests.json")); e == nil {
			if json.Unmarshal(b, &s.logs) != nil {
				return fail(500, "stored request logs are invalid")
			}
		}
		if b, e := os.ReadFile(filepath.Join(c.DataDir, "statistics.json")); e == nil {
			if json.Unmarshal(b, &s.statistics) != nil {
				return fail(500, "stored statistics are invalid")
			}
		}
		if b, e := os.ReadFile(filepath.Join(c.DataDir, "usage.json")); e == nil {
			var saved map[string]persistedUsage
			if json.Unmarshal(b, &saved) != nil {
				return fail(500, "stored quota observations are invalid")
			}
			for cid, item := range saved {
				s.usage[cid] = &usageCache{fingerprint: item.Fingerprint, value: item.Value}
				if item.Account != "" {
					s.accounts[cid] = item.Account
				}
			}
		}
		s.loaded = true
	}
	if len(s.logs) > c.LogRetention {
		s.logs = s.logs[len(s.logs)-c.LogRetention:]
	}
	s.startPersistenceLocked()
	return nil
}
func registration() any {
	fields := []map[string]string{
		{"Name": "data_dir", "Type": "string", "Description": "Persistent plugin data directory"},
		{"Name": "public_base_url", "Type": "string", "Description": "Public CPA/CPAMP URL including any reverse proxy prefix; optional for same-origin panels"},
		{"Name": "cpamp_install_dir", "Type": "string", "Description": "CPAMP native install directory; empty detects the running instance or ~/cpa-manager-plus"},
		{"Name": "cpamp_config_path", "Type": "string", "Description": "Optional CPAMP config.json override for multiple instances or custom deployments"},
		{"Name": "cpamp_panel_path", "Type": "string", "Description": "Optional native management.html destination; empty uses the detected panel path"},
	}
	return map[string]any{"schema_version": 6, "metadata": map[string]any{"Name": "CommandCodeBridge", "Version": Version, "Author": Author, "GitHubRepository": Repository, "Description": "CommandCode Provider API, credentials, models, usage and request statistics", "ConfigFields": fields}, "capabilities": map[string]any{"auth_provider": true, "quota_provider": true, "model_provider": true, "executor": true, "executor_model_scope": "both", "executor_input_formats": []string{"chat-completions"}, "executor_output_formats": []string{"chat-completions", "claude"}, "management_api": true}}
}
func (s *Service) Handle(method string, raw json.RawMessage) (any, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		if e := s.configure(raw); e != nil {
			return nil, e
		}
		return registration(), nil
	case "executor.identifier", "auth.identifier", "quota.identifier":
		return map[string]string{"identifier": Provider}, nil
	case "model.static", "model.for_auth":
		return s.modelRegistration(), nil
	case "auth.parse":
		return s.parseAuth(raw)
	case "auth.refresh":
		var r struct{ StorageJSON []byte }
		if json.Unmarshal(raw, &r) != nil {
			return nil, fail(400, "invalid auth refresh")
		}
		var c Credential
		if json.Unmarshal(r.StorageJSON, &c) != nil {
			return nil, fail(401, "invalid stored credential")
		}
		s.mu.RLock()
		current, ok := s.creds[c.ID]
		revoked := s.revoked[c.ID]
		name := s.files[c.ID]
		s.mu.RUnlock()
		if revoked {
			return nil, fail(401, "credential removed")
		}
		if ok {
			c = current
		}
		return map[string]any{"Auth": authData(c, name), "NextRefreshAfter": time.Now().Add(365 * 24 * time.Hour)}, nil
	case "auth.login.start":
		return s.startLogin(raw)
	case "auth.login.poll":
		return s.pollLogin(raw)
	case "quota.describe":
		return map[string]any{"supported_providers": []string{Provider}, "display_name": "CommandCode", "supports_reset": false}, nil
	case "quota.fetch":
		return s.nativeQuota(raw)
	case "quota.reset":
		return nil, fail(501, "CommandCode does not support resetting subscription quota")
	case "executor.execute", "executor.execute_stream":
		var r ExecutorRequest
		if json.Unmarshal(raw, &r) != nil {
			return nil, fail(400, "invalid executor request")
		}
		return s.execute(r, method == "executor.execute_stream")
	case "executor.count_tokens":
		return nil, fail(501, "CommandCode has no exact token counting endpoint")
	case "executor.http_request":
		return nil, fail(400, "use the model executor")
	case "management.register":
		s.startSampler()
		return s.registerManagement(), nil
	case "management.handle":
		return s.management(raw)
	case "plugin.quiesce", "plugin.shutdown":
		s.shutdown()
		return map[string]any{}, nil
	default:
		return nil, fail(400, "unsupported plugin method")
	}
}
func (s *Service) begin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return fail(503, "plugin is shutting down")
	}
	s.active.Add(1)
	return nil
}
func (s *Service) shutdown() {
	s.mu.Lock()
	if !s.stopped {
		s.stopped = true
		close(s.stopCh)
	}
	clear(s.logins)
	streams := []string{}
	for stream := range s.streams {
		streams = append(streams, stream)
	}
	s.mu.Unlock()
	for _, stream := range streams {
		s.closeUpstream(stream)
	}
	s.active.Wait()
	s.stopPersistence()
}
func (s *Service) modelRegistration() any {
	models := []any{}
	for _, m := range s.config().Models {
		models = append(models, map[string]any{"ID": m.ID, "Name": m.UpstreamID, "Object": "model", "OwnedBy": Provider, "DisplayName": m.Name, "SupportedGenerationMethods": []string{"chat"}, "UserDefined": true})
	}
	return map[string]any{"Provider": Provider, "Models": models}
}
func (s *Service) model(alias string) (Model, error) {
	for _, m := range s.config().Models {
		if m.ID == alias {
			return m, nil
		}
	}
	return Model{}, fail(404, "model alias is not configured")
}
func authData(c Credential, name string) any {
	if name == "" {
		name = c.ID + ".json"
	}
	return map[string]any{"Provider": Provider, "ID": routingID(c, name), "FileName": name, "Label": c.Label, "Disabled": c.Disabled, "ProxyURL": c.ProxyURL, "StorageJSON": jsonBytes(c), "Metadata": map[string]any{"type": Provider, "priority": c.Priority}, "Attributes": map[string]string{"auth_kind": "api_key"}}
}
func (s *Service) parseAuth(raw json.RawMessage) (any, error) {
	var r struct {
		Provider, Path, FileName string
		RawJSON                  []byte
		Host                     struct{ AuthDir string }
	}
	if json.Unmarshal(raw, &r) != nil {
		return nil, fail(400, "invalid auth input")
	}
	var c Credential
	if json.Unmarshal(r.RawJSON, &c) != nil || c.Type != Provider {
		return map[string]bool{"Handled": false}, nil
	}
	if c.APIKey == "" {
		return nil, fail(401, "credential has no API key")
	}
	if c.ID == "" {
		c.ID = strings.TrimSuffix(r.FileName, ".json")
	}
	if c.ID == "" {
		return nil, fail(400, "credential has no ID")
	}
	if c.Label == "" {
		c.Label = "CommandCode"
	}
	s.mu.Lock()
	if s.revoked[c.ID] {
		s.mu.Unlock()
		return nil, fail(401, "credential removed")
	}
	if cached := s.usage[c.ID]; cached != nil && cached.fingerprint != shaKey(c.APIKey+"|"+c.ProxyURL+"|"+s.cfg.BaseURL) {
		delete(s.usage, c.ID)
		delete(s.accounts, c.ID)
		s.persistDirty = true
	}
	s.creds[c.ID] = c
	s.files[c.ID] = filepath.Base(r.FileName)
	if r.Host.AuthDir != "" {
		s.authDir = r.Host.AuthDir
	}
	delete(s.revoked, c.ID)
	s.mu.Unlock()
	return map[string]any{"Handled": true, "Auth": authData(c, r.FileName)}, nil
}
func (s *Service) credential(r ExecutorRequest) (Credential, error) {
	var c Credential
	_ = json.Unmarshal(r.StorageJSON, &c)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if latest, ok := s.creds[c.ID]; ok {
		c = latest
	}
	if c.ID == "" {
		for _, v := range s.creds {
			if routingID(v, s.files[v.ID]) == r.AuthID {
				c = v
				break
			}
		}
	}
	if c.Type != Provider || c.APIKey == "" || c.Disabled || s.revoked[c.ID] {
		return c, fail(401, "credential missing, removed or disabled")
	}
	return c, nil
}
func (s *Service) credentials() []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []map[string]any{}
	for _, c := range s.creds {
		item := map[string]any{"id": c.ID, "file_name": s.files[c.ID], "routing_id": routingID(c, s.files[c.ID]), "key_updated_at": c.KeyUpdatedAt, "label": c.Label, "disabled": c.Disabled, "priority": c.Priority, "has_key": c.APIKey != "", "usage": usageView(s.usage[c.ID]), "health": s.credentialHealthLocked(c, time.Now()), "shared_credentials": s.sharedCountLocked(c.ID), "recent_requests": s.credentialRecentRequestsLocked(c)}
		item["credential_revision"] = shaKey(c.APIKey + "|" + c.ProxyURL + "|" + s.cfg.BaseURL)
		if cached := s.usage[c.ID]; cached != nil && cached.value.UpdatedAt != nil {
			// The management snapshot is explicitly annotated with Usage status.
			// Retain unavailable sections for display without publishing them as
			// fresh native quota observations.
			last := cached.value
			last.Unavailable, last.PlanStale = nil, false
			item["quota"] = normalizeQuota(last)
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i]["priority"].(int) != out[j]["priority"].(int) {
			return out[i]["priority"].(int) > out[j]["priority"].(int)
		}
		return str(out[i]["id"]) < str(out[j]["id"])
	})
	return out
}
func (s *Service) sharedCountLocked(cid string) int {
	account := s.accounts[cid]
	if account == "" {
		return 1
	}
	n := 0
	for id, a := range s.accounts {
		if a == account && !s.creds[id].Disabled {
			n++
		}
	}
	return n
}
func (s *Service) saveConfig(c Config) error {
	if e := c.validate(); e != nil {
		return e
	}
	old := s.config()
	if c.DataDir != old.DataDir {
		return fail(409, "data_dir cannot change at runtime")
	}
	if e := atomicJSON(filepath.Join(c.DataDir, "settings.json"), c); e != nil {
		return fail(500, "settings persistence failed")
	}
	s.mu.Lock()
	s.cfg = c
	if c.BaseURL != old.BaseURL {
		s.usage = map[string]*usageCache{}
		s.accounts = map[string]string{}
		s.cooldown = map[string]time.Time{}
		s.probes = map[string]bool{}
		s.persistDirty = true
	}
	creds := []Credential{}
	for _, v := range s.creds {
		creds = append(creds, v)
	}
	s.mu.Unlock()
	hash := sha256.Sum256(jsonBytes(c.Models))
	for _, cred := range creds {
		cred.ModelRevision = hex.EncodeToString(hash[:])
		if e := s.saveCredential(cred); e != nil {
			return fail(500, "settings saved; credential model registration refresh failed")
		}
	}
	return nil
}
