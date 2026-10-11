package loginhelper

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const fixtureState = "0123456789abcdef0123456789abcdef0123456789abcdef"

func request(h *Receiver, method, path, contentType, body, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, LocalURL+path, strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func start(t *testing.T, h *Receiver) {
	t.Helper()
	auth := "https://commandcode.ai/studio/auth/cli?callback=http%3A%2F%2Flocalhost%3A45879%2Fcallback&state=" + fixtureState + "&mode=redirect"
	w := request(h, "POST", "/start", "application/x-www-form-urlencoded", url.Values{"nonce": {h.nonce}, "auth_url": {auth}}.Encode(), LocalURL)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("start failed: %d", w.Code)
	}
	u, _ := url.Parse(w.Header().Get("Location"))
	if u.Query().Get("callback") != LocalURL+"/callback" || u.Query().Get("state") != fixtureState {
		t.Fatal("official callback lost loopback or state")
	}
}

func TestOfficialFormPOST(t *testing.T) {
	h := New()
	start(t, h)
	fields := url.Values{"apiKey": {"fixture-official-secret"}, "state": {fixtureState}, "userId": {"fixture-user"}, "userName": {"fixture account"}, "keyName": {"CPAMP key"}}
	w := request(h, "POST", "/callback", "application/x-www-form-urlencoded", fields.Encode(), "https://commandcode.ai")
	if w.Code != http.StatusSeeOther || strings.Contains(w.Header().Get("Location"), "fixture-official-secret") {
		t.Fatal("form callback failed or leaked secret into URL")
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("credential page cache or referrer enabled")
	}
	landing := request(h, "GET", w.Header().Get("Location"), "", "", "")
	if landing.Code != 200 || !strings.Contains(landing.Body.String(), "fixture-official-secret") {
		t.Fatal("no copyable POST result")
	}
	if !strings.Contains(landing.Body.String(), `type="password"`) || !strings.Contains(landing.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("result is not masked or frame-protected")
	}
	u, _ := url.Parse(h.result)
	if u.Query().Get("state") != fixtureState || u.Query().Get("apiKey") != "fixture-official-secret" || u.Query().Get("keyName") != "CPAMP key" {
		t.Fatal("copyable handoff lost the actual POST fields")
	}
	if request(h, "POST", "/callback", "application/x-www-form-urlencoded", fields.Encode(), "https://commandcode.ai").Code != 410 {
		t.Fatal("callback replay accepted")
	}
	h.mu.Lock()
	h.expires = time.Now().Add(-time.Second)
	h.mu.Unlock()
	if request(h, "GET", "/complete?state="+fixtureState, "", "", "").Code != 403 || h.result != "" {
		t.Fatal("expired result retained")
	}
}

func TestCallbackValidation(t *testing.T) {
	for _, test := range []struct {
		body, contentType, origin string
		status                    int
	}{
		{"state=wrong&apiKey=fixture-secret", "application/x-www-form-urlencoded", "https://commandcode.ai", 403},
		{"state=" + fixtureState + "&state=other&apiKey=fixture-secret", "application/x-www-form-urlencoded", "https://commandcode.ai", 400},
		{"state=" + fixtureState + "&apiKey=fixture-secret", "application/x-www-form-urlencoded", "https://evil.example", 403},
		{"apiKey=fixture-secret", "text/plain", "https://commandcode.ai", 415},
		{`{"state":"` + fixtureState + `","apiKey":"fixture-secret"} {}`, "application/json", "https://commandcode.ai", 403},
		{`{"state":"wrong","state":"` + fixtureState + `","apiKey":"fixture-secret"}`, "application/json", "https://commandcode.ai", 403},
		{`{"state":"` + fixtureState + `","apiKey":"other-secret","apiKey":"fixture-secret"}`, "application/json", "https://commandcode.ai", 403},
		{"state=" + fixtureState + "&apiKey=" + strings.Repeat("x", 40000), "application/x-www-form-urlencoded", "https://commandcode.ai", 403},
	} {
		h := New()
		start(t, h)
		w := request(h, "POST", "/callback", test.contentType, test.body, test.origin)
		if w.Code != test.status || strings.Contains(w.Body.String(), "fixture-secret") || h.result != "" {
			t.Fatalf("invalid callback accepted or echoed: %d expected %d", w.Code, test.status)
		}
	}
	h := New()
	if request(h, "GET", "/callback", "", "", "").Code != 405 {
		t.Fatal("bare URL mistaken for credentials")
	}
	r := httptest.NewRequest("GET", "http://evil.example/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("DNS rebinding host accepted")
	}
	if request(h, "POST", "/start", "application/x-www-form-urlencoded", "nonce=wrong&auth_url=https://evil.example", LocalURL).Code != 403 {
		t.Fatal("unauthorized local start")
	}
}

func TestLegacyJSONAndDenial(t *testing.T) {
	h := New()
	start(t, h)
	w := request(h, "POST", "/callback", "application/json", `{"state":"`+fixtureState+`","apiKey":"fixture-json-secret","keyName":"fixture key"}`, "https://commandcode.ai")
	if w.Code != 200 || w.Body.String() != `{"success":true}` || !strings.Contains(h.result, "fixture-json-secret") {
		t.Fatal("legacy JSON callback lost")
	}
	h = New()
	start(t, h)
	w = request(h, "POST", "/callback", "application/x-www-form-urlencoded", "state="+fixtureState+"&error=access_denied", "https://commandcode.ai")
	if w.Code != 303 || !strings.Contains(h.result, "error=access_denied") {
		t.Fatal("denial not handed off")
	}
	r := httptest.NewRequest("OPTIONS", LocalURL+"/callback", nil)
	r.Header.Set("Origin", "https://commandcode.ai")
	r.Header.Set("Access-Control-Request-Private-Network", "true")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Private-Network") != "true" {
		t.Fatal("official PNA preflight failed")
	}
	_, _ = io.Copy(io.Discard, w.Result().Body)
}
