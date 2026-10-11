// Package loginhelper receives CommandCode's form/JSON POST on the browser's
// computer. It never reads CPA management keys, writes credentials to disk, or
// forwards them over the network; the user transfers the result into CPAMP.
package loginhelper

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const Address = "127.0.0.1:45879"
const LocalURL = "http://" + Address
const lifetime = 10 * time.Minute

type Receiver struct {
	mu                   sync.Mutex
	nonce, state, result string
	expires              time.Time
}

func token() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic("random source unavailable")
	}
	return hex.EncodeToString(b)
}

func New() *Receiver { return &Receiver{nonce: token()} }

// Decode fields individually so duplicated JSON keys cannot overwrite state
// or credentials. Form callbacks are checked for duplicate fields below.
func jsonFields(reader io.Reader) url.Values {
	decoder := json.NewDecoder(reader)
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil
	}
	values := url.Values{}
	for decoder.More() {
		name, err := decoder.Token()
		if err != nil {
			return nil
		}
		key, ok := name.(string)
		if !ok || values.Has(key) {
			return nil
		}
		var value string
		if decoder.Decode(&value) != nil {
			return nil
		}
		values.Set(key, value)
	}
	closing, err := decoder.Token()
	var extra any
	if err != nil || closing != json.Delim('}') || decoder.Decode(&extra) != io.EOF {
		return nil
	}
	return values
}

var page = template.Must(template.New("login").Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="referrer" content="no-referrer"><meta name="viewport" content="width=device-width,initial-scale=1"><title>CommandCode 本地认证接收器</title><style nonce="{{.Nonce}}">body{font:16px system-ui;margin:48px auto;padding:0 24px;max-width:620px;line-height:1.7;color:#222;background:#fafafa}input,button{font:inherit;box-sizing:border-box;width:100%;padding:12px;margin:8px 0}small{color:#666}</style></head><body><h1>CommandCode 本地认证接收器</h1>{{if .Result}}<p>已接收官方 POST 认证结果。点击复制，然后返回 CPAMP 原生 CommandCode 登录卡片，粘贴到「认证结果」并提交。</p><input id="result" type="password" readonly autocomplete="off" value="{{.Result}}"><button id="copy">复制认证结果</button><p id="status" role="status"></p><small>结果包含 API key，仅在当前进程内存保留至会话到期。请勿分享；提交成功后可关闭接收器。</small><script nonce="{{.Nonce}}">document.getElementById('copy').onclick=async()=>{try{await navigator.clipboard.writeText(document.getElementById('result').value);document.getElementById('status').textContent='已复制，请返回 CPAMP 粘贴并提交。'}catch{document.getElementById('status').textContent='复制失败，请选中输入框内容复制。'}};</script>{{else}}<p>在 CPAMP 重新开始 CommandCode 登录，复制官方「授权链接」到下方。接收器会打开官网并接收其 POST 回调。</p><form method="post" action="/start"><input type="hidden" name="nonce" value="{{.Nonce}}"><label for="auth_url">CPAMP 生成的官方授权链接</label><input id="auth_url" name="auth_url" type="url" required autocomplete="off"><button>打开官方授权</button></form><small>此程序运行在浏览器所在电脑，只监听 127.0.0.1:45879。无需 CPA 管理密钥。</small>{{end}}</body></html>`))

func (h *Receiver) html(w http.ResponseWriter, result string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'nonce-"+h.nonce+"'; script-src 'nonce-"+h.nonce+"'; form-action 'self' https://commandcode.ai; frame-ancestors 'none'")
	_ = page.Execute(w, struct{ Nonce, Result string }{h.nonce, result})
}

func (h *Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Host != Address && r.Host != "localhost:45879" {
		http.Error(w, "invalid localhost host", http.StatusForbidden)
		return
	}
	origin := r.Header.Get("Origin")
	if origin != "" && origin != LocalURL && origin != "http://localhost:45879" && origin != "https://commandcode.ai" {
		http.Error(w, "origin is not allowed", http.StatusForbidden)
		return
	}
	if origin == "https://commandcode.ai" && r.URL.Path != "/callback" {
		http.Error(w, "origin is not allowed for this path", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/callback" && origin == "https://commandcode.ai" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
		}
	}
	if r.Method == http.MethodOptions && r.URL.Path == "/callback" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state != "" && !time.Now().Before(h.expires) {
		h.state = ""
		h.result = ""
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /":
		h.html(w, "")
	case "POST /start":
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		if r.ParseForm() != nil || r.PostForm.Get("nonce") != h.nonce {
			http.Error(w, "invalid local session", http.StatusForbidden)
			return
		}
		u, err := url.Parse(r.PostForm.Get("auth_url"))
		if err != nil || u.Scheme != "https" || u.Host != "commandcode.ai" || u.Path != "/studio/auth/cli" || u.User != nil || u.Fragment != "" {
			http.Error(w, "paste the official authorization URL from CPAMP", http.StatusBadRequest)
			return
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q["state"]) != 1 || len(q.Get("state")) != 48 || strings.Trim(q.Get("state"), "0123456789abcdef") != "" {
			http.Error(w, "invalid CPAMP login state", http.StatusBadRequest)
			return
		}
		// Match the official CLI's IPv4 listener. Keep its form POST mode.
		q.Set("callback", LocalURL+"/callback")
		q.Set("mode", "redirect")
		u.RawQuery = q.Encode()
		h.state, h.result, h.expires = q.Get("state"), "", time.Now().Add(lifetime)
		expires := h.expires
		time.AfterFunc(lifetime, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.expires.Equal(expires) {
				h.state, h.result = "", ""
			}
		})
		http.Redirect(w, r, u.String(), http.StatusSeeOther)
	case "POST /callback":
		if h.state == "" || h.result != "" {
			http.Error(w, "login expired or already completed; start again in CPAMP", http.StatusGone)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32768)
		contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
		var values url.Values
		if contentType == "application/x-www-form-urlencoded" {
			if r.ParseForm() == nil {
				values = r.PostForm
			}
		} else if contentType == "application/json" {
			values = jsonFields(r.Body)
		} else {
			http.Error(w, "use a form or JSON POST", http.StatusUnsupportedMediaType)
			return
		}
		for _, key := range []string{"state", "apiKey", "keyName", "error", "error_description"} {
			if len(values[key]) > 1 {
				http.Error(w, "ambiguous callback", http.StatusBadRequest)
				return
			}
		}
		if len(values["state"]) != 1 || values.Get("state") != h.state {
			http.Error(w, "callback state mismatch", http.StatusForbidden)
			return
		}
		key := values.Get("apiKey")
		denied := values.Get("error") != ""
		if !denied && (len(key) < 8 || len(key) > 4096 || strings.ContainsAny(key, "\r\n\t ")) {
			http.Error(w, "invalid callback credentials", http.StatusBadRequest)
			return
		}
		result := url.Values{"state": {h.state}}
		if denied {
			result.Set("error", "access_denied")
		} else {
			result.Set("apiKey", key)
			result.Set("keyName", values.Get("keyName"))
		}
		// A copyable handoff string, never visited or added to the address bar.
		h.result = LocalURL + "/callback?" + result.Encode()
		if contentType == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"success":true}`)
		} else {
			http.Redirect(w, r, "/complete?state="+url.QueryEscape(h.state), http.StatusSeeOther)
		}
	case "GET /complete":
		if h.result == "" || r.URL.Query().Get("state") != h.state {
			http.Error(w, "no valid callback received", http.StatusForbidden)
			return
		}
		h.html(w, h.result)
	case "GET /callback":
		if h.result != "" {
			http.Redirect(w, r, "/complete?state="+url.QueryEscape(h.state), http.StatusSeeOther)
			return
		}
		http.Error(w, "CommandCode sends credentials in a POST body. A bare callback URL contains no credentials. Open the official link from the local receiver.", http.StatusMethodNotAllowed)
	default:
		http.NotFound(w, r)
	}
}
