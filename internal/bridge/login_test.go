package bridge

import (
	"bytes"
	"net/url"
	"testing"
	"time"
)

func TestNativeLogin(t *testing.T) {
	s, _, _ := setup(t, quotaReply)
	start := func() map[string]any {
		t.Helper()
		v, err := s.Handle("auth.login.start", jsonBytes(map[string]any{"Provider": Provider, "BaseURL": "http://127.0.0.1:8317/v0/management/oauth-callback"}))
		if err != nil {
			t.Fatal(err)
		}
		return v.(map[string]any)
	}
	poll := func(state string) map[string]any {
		t.Helper()
		v, err := s.Handle("auth.login.poll", jsonBytes(map[string]string{"Provider": Provider, "State": state}))
		if err != nil {
			t.Fatal(err)
		}
		return v.(map[string]any)
	}
	login := start()
	state := str(login["State"])
	if state == "" || len(state) < 40 || str(login["URL"]) != authorizationURL(state) || str(poll(state)["Status"]) != "pending" {
		t.Fatalf("invalid login start: %+v", login)
	}
	if err := s.completeLogin("wrong-state", "fixture-secret-key", "", ""); statusOf(err) != 410 {
		t.Fatalf("unknown state accepted: %v", err)
	}
	if err := s.completeLogin(state, "fixture-login-key", "login account", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.completeLogin(state, "fixture-other-key", "", ""); statusOf(err) != 410 {
		t.Fatalf("completed login overwritten: %v", err)
	}
	response := poll(state)
	if str(response["Status"]) != "success" || !bytes.Contains(response["Auth"].(map[string]any)["StorageJSON"].([]byte), []byte("fixture-login-key")) {
		t.Fatalf("login did not return auth to CPA: %+v", response)
	}
	if str(poll(state)["Status"]) != "error" {
		t.Fatal("completed login can be replayed")
	}
	login = start()
	state = str(login["State"])
	s.mu.Lock()
	s.logins[state].expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if err := s.completeLogin(state, "fixture-login-key", "", ""); statusOf(err) != 410 {
		t.Fatalf("expired login accepted: %v", err)
	}
	if str(poll(state)["Status"]) != "error" {
		t.Fatal("expired login still pending")
	}
	login = start()
	state = str(login["State"])
	result, err := s.loginResource(ManagementRequest{Path: resourceBase + "/callback", Query: url.Values{"state": {state}, "error": {"access_denied"}}})
	if err != nil || result.(ManagementResponse).StatusCode != 303 || str(poll(state)["Status"]) != "error" {
		t.Fatal("denied login accepted")
	}
	login = start()
	state = str(login["State"])
	result, err = s.loginResource(ManagementRequest{Path: resourceBase + "/callback", Query: url.Values{"state": {state}, "apiKey": {"fixture-login-key"}}})
	if err != nil {
		t.Fatal(err)
	}
	r := result.(ManagementResponse)
	if r.StatusCode != 303 || bytes.Contains(jsonBytes(r), []byte("fixture-login-key")) || r.Headers.Get("Referrer-Policy") != "no-referrer" || str(poll(state)["Status"]) != "success" {
		t.Fatal("browser redirect failed or leaked key")
	}
}

func TestLoginInvalidKeyAndPublicURL(t *testing.T) {
	s, _, _ := setup(t, func(_ string, _ map[string]any) mockReply {
		return mockReply{status: 401, body: `{"error":"invalid fixture-login-key"}`}
	})
	cfg := s.config()
	cfg.PublicBaseURL = "https://panel.example/cpa/"
	if err := s.saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	v, err := s.Handle("auth.login.start", jsonBytes(map[string]string{"Provider": Provider}))
	if err != nil {
		t.Fatal(err)
	}
	login := v.(map[string]any)
	u, parseErr := url.Parse(str(login["URL"]))
	if parseErr != nil || u.Scheme+"://"+u.Host+u.Path != officialLoginURL || u.Query().Get("callback") != localhostCallbackURL || u.Query().Get("mode") != "redirect" || u.Query().Get("state") != str(login["State"]) {
		t.Fatal("remote CPA must use the official localhost redirect flow")
	}
	err = s.completeLogin(str(login["State"]), "fixture-login-key", "", "")
	if statusOf(err) != 401 || bytes.Contains([]byte(err.Error()), []byte("fixture-login-key")) {
		t.Fatalf("invalid key accepted or leaked: %v", err)
	}
	result, callbackErr := s.loginResource(ManagementRequest{Path: resourceBase + "/callback", Query: url.Values{"state": {str(login["State"])}, "apiKey": {"fixture-login-key"}}})
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
	redirect := result.(ManagementResponse)
	if redirect.StatusCode != 303 || redirect.Headers.Get("Location") != "login?state="+str(login["State"])+"&completed=failed" || bytes.Contains(jsonBytes(redirect), []byte("fixture-login-key")) {
		t.Fatal("rejected callback retained the key in its URL")
	}
	v, _ = s.Handle("auth.login.poll", jsonBytes(map[string]string{"State": str(login["State"])}))
	if str(v.(map[string]any)["Status"]) != "pending" {
		t.Fatal("invalid key must allow retry")
	}
	for _, bad := range []string{"https://key@panel.example", "https://panel.example?key=x", "javascript:alert(1)"} {
		cfg.PublicBaseURL = bad
		if cfg.validate() == nil {
			t.Fatalf("unsafe public URL accepted: %s", bad)
		}
	}
}

func TestRemoteLoginCallback(t *testing.T) {
	s, _, _ := setup(t, quotaReply)
	start := func() string {
		v, err := s.startLogin(jsonBytes(map[string]string{"Provider": Provider}))
		if err != nil {
			t.Fatal(err)
		}
		return str(v.(map[string]any)["State"])
	}
	state, other := start(), start()
	redirect := localhostCallbackURL + "?" + url.Values{"state": {state}, "apiKey": {"fixture-remote-key"}, "keyName": {"remote account"}}.Encode()
	for _, bad := range []string{
		"https://remote.example/callback?state=" + state + "&apiKey=fixture-remote-key",
		"http://localhost.evil.example:45879/callback?state=" + state + "&apiKey=fixture-remote-key",
		"http://key@localhost:45879/callback?state=" + state + "&apiKey=fixture-remote-key",
		redirect + "#fragment", redirect + "&state=" + other, redirect + "&apiKey=another-key",
		localhostCallbackURL + "?state=" + other + "&apiKey=fixture-remote-key",
		localhostCallbackURL + "?state=" + state + "&apiKey=%ZZ",
		"http://localhost:1455/auth/callback?state=" + state + "&code=codex-code",
	} {
		if err := s.completeRedirectLogin(state, bad, ""); statusOf(err) != 400 {
			t.Fatalf("invalid callback accepted or wrong status: %v", err)
		}
	}
	if err := s.completeRedirectLogin("", redirect, ""); statusOf(err) != 400 {
		t.Fatal("missing current state accepted")
	}
	if err := s.completeRedirectLogin(state, redirect, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.completeRedirectLogin(state, redirect, ""); statusOf(err) != 410 {
		t.Fatal("callback replay accepted")
	}
	v, _ := s.pollLogin(jsonBytes(map[string]string{"State": state}))
	result := v.(map[string]any)
	if str(result["Status"]) != "success" || !bytes.Contains(result["Auth"].(map[string]any)["StorageJSON"].([]byte), []byte("fixture-remote-key")) {
		t.Fatal("remote callback did not deliver credential to CPA")
	}
	if err := s.completeRedirectLogin(other, localhostCallbackURL+"?state="+other+"&error=access_denied", ""); err != nil {
		t.Fatal(err)
	}
	v, _ = s.pollLogin(jsonBytes(map[string]string{"State": other}))
	if str(v.(map[string]any)["Status"]) != "error" {
		t.Fatal("denied callback stayed pending")
	}
	state = start()
	s.mu.Lock()
	s.logins[state].expires = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if err := s.completeRedirectLogin(state, localhostCallbackURL+"?state="+state+"&apiKey=fixture-remote-key", ""); statusOf(err) != 410 {
		t.Fatal("expired callback accepted")
	}
}
