package bridge

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const resourceBase = "/v0/resource/plugins/" + PluginID
const loginLifetime = 10 * time.Minute

type loginSession struct {
	expires    time.Time
	credential *Credential
	checking   bool
	denied     bool
}

func (s *Service) pruneLoginsLocked() {
	for state, session := range s.logins {
		if !time.Now().Before(session.expires) {
			delete(s.logins, state)
		}
	}
}

func (s *Service) startLogin(raw json.RawMessage) (any, error) {
	var r struct{ Provider string }
	if json.Unmarshal(raw, &r) != nil || (r.Provider != "" && r.Provider != Provider && r.Provider != PluginID) {
		return nil, fail(400, "invalid CommandCode login request")
	}
	if err := s.begin(); err != nil {
		return nil, err
	}
	defer s.active.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLoginsLocked()
	if len(s.logins) >= 64 {
		return nil, fail(429, "too many pending logins; wait for an existing login to expire")
	}
	state := id() + id()
	expires := time.Now().UTC().Add(loginLifetime)
	s.logins[state] = &loginSession{expires: expires}
	// CPA's BaseURL points to its internal callback address (often loopback).
	// Same-origin panels use a relative resource URL; other panels can configure
	// the public address, including the reverse proxy's subdirectory.
	return map[string]any{"Provider": Provider, "URL": s.cfg.PublicBaseURL + resourceBase + "/login?state=" + state, "State": state, "ExpiresAt": expires}, nil
}

func (s *Service) pollLogin(raw json.RawMessage) (any, error) {
	var r struct{ Provider, State string }
	if json.Unmarshal(raw, &r) != nil || (r.Provider != "" && r.Provider != Provider && r.Provider != PluginID) {
		return nil, fail(400, "invalid CommandCode login poll")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLoginsLocked()
	session := s.logins[r.State]
	if session == nil {
		return map[string]any{"Status": "error", "Message": "登录已失效，请重新开始认证"}, nil
	}
	if session.denied {
		delete(s.logins, r.State)
		return map[string]any{"Status": "error", "Message": "已取消 CommandCode 授权"}, nil
	}
	if session.credential == nil {
		return map[string]any{"Status": "pending"}, nil
	}
	c := *session.credential
	delete(s.logins, r.State)
	// CPA owns persistence and watcher registration for completed login records.
	return map[string]any{"Status": "success", "Auth": authData(c, c.ID+".json")}, nil
}

func (s *Service) completeLogin(state, key, label, callback string) error {
	if err := s.begin(); err != nil {
		return err
	}
	defer s.active.Done()
	key = strings.TrimSpace(key)
	if len(key) < 8 || len(key) > 4096 || strings.ContainsAny(key, "\r\n\t ") {
		return fail(400, "invalid CommandCode Studio API key")
	}
	s.mu.Lock()
	s.pruneLoginsLocked()
	session := s.logins[state]
	if session == nil || session.credential != nil || session.denied {
		s.mu.Unlock()
		return fail(410, "login expired or already completed")
	}
	if session.checking {
		s.mu.Unlock()
		return fail(409, "login validation is already in progress")
	}
	session.checking = true
	s.mu.Unlock()
	c := Credential{Type: Provider, ID: PluginID + "-" + id(), APIKey: key, Label: "CommandCode"}
	who, err := s.getJSON("/alpha/whoami?limits=1", c, callback, time.Now().Add(15*time.Second))
	if err == nil && str(object(who["user"])["id"]) == "" && str(object(who["org"])["id"]) == "" {
		err = fail(502, "account identity response is unrecognized")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session.checking = false
	if err != nil {
		return redactError(err, key)
	}
	if s.logins[state] != session || !time.Now().Before(session.expires) || s.stopped || session.denied {
		return fail(410, "login expired or cancelled during validation")
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = str(object(who["user"])["userName"])
	}
	if label != "" && len(label) <= 100 {
		c.Label = label
	}
	now := time.Now().UTC()
	c.KeyUpdatedAt = &now
	session.credential = &c
	return nil
}

func (s *Service) loginResource(r ManagementRequest) (any, error) {
	if r.Path == resourceBase+"/login" {
		return s.htmlResource("login.html")
	}
	state := r.Query.Get("state")
	completed := "1"
	if r.Query.Get("error") != "" {
		completed = "cancelled"
		s.mu.Lock()
		s.pruneLoginsLocked()
		session := s.logins[state]
		if session != nil && session.credential == nil {
			session.denied = true
		}
		s.mu.Unlock()
		if session == nil {
			completed = "failed"
		}
	} else if err := s.completeLogin(state, r.Query.Get("apiKey"), r.Query.Get("keyName"), r.HostCallbackID); err != nil {
		// Even rejected or expired callbacks must clear the key from the URL.
		// Do not encode upstream errors in the redirect query.
		completed = "failed"
	}
	// Strip the official redirect's key parameters immediately. The landing page
	// has no third party resources and never writes upstream keys to web storage.
	return ManagementResponse{StatusCode: http.StatusSeeOther, Headers: http.Header{"Location": {"login?state=" + url.QueryEscape(state) + "&completed=" + completed}, "Cache-Control": {"no-store"}, "Referrer-Policy": {"no-referrer"}}, Body: []byte{}}, nil
}

func (s *Service) htmlResource(file string) (any, error) {
	b, err := ui.ReadFile("ui/" + file)
	if err != nil {
		return nil, fail(500, "login resource missing")
	}
	auth, _ := ui.ReadFile("ui/cpa-auth.js")
	b = []byte(strings.ReplaceAll(string(b), "/*__CPA_AUTH__*/", string(auth)))
	return ManagementResponse{StatusCode: 200, Headers: http.Header{"Content-Type": {"text/html; charset=utf-8"}, "Cache-Control": {"no-store"}, "Referrer-Policy": {"no-referrer"}, "X-Content-Type-Options": {"nosniff"}}, Body: b}, nil
}
