package bridge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockReply struct {
	status  int
	headers http.Header
	body    string
	err     string
}
type mockHost struct {
	mu        sync.Mutex
	reply     func(string, map[string]any) mockReply
	streams   map[string]mockReply
	requests  []map[string]any
	emitted   [][]byte
	closed    []string
	authDir   string
	block     chan struct{}
	opened    chan struct{}
	completed chan struct{}
}

func assign(out any, v any) error {
	if out == nil {
		return nil
	}
	return json.Unmarshal(jsonBytes(v), out)
}
func (h *mockHost) call(method string, in, out any) error {
	raw, _ := decodeObject(jsonBytes(in))
	switch method {
	case "host.http.do_stream":
		if h.block != nil {
			if h.opened != nil {
				select {
				case h.opened <- struct{}{}:
				default:
				}
			}
			<-h.block
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		h.requests = append(h.requests, raw)
		reply := h.reply(str(raw["url"]), raw)
		stream := id()
		h.streams[stream] = reply
		return assign(out, upstreamStream{StatusCode: reply.status, Headers: reply.headers, StreamID: stream})
	case "host.http.stream_read":
		h.mu.Lock()
		defer h.mu.Unlock()
		stream := str(raw["stream_id"])
		reply, ok := h.streams[stream]
		if !ok {
			return fmt.Errorf("closed stream")
		}
		chunk := readChunk{}
		if reply.body != "" {
			size := 7
			if len(reply.body) < size {
				size = len(reply.body)
			}
			chunk.Payload = []byte(reply.body[:size])
			reply.body = reply.body[size:]
			h.streams[stream] = reply
		} else {
			chunk.Done = true
			chunk.Error = reply.err
		}
		return assign(out, chunk)
	case "host.http.stream_close":
		h.mu.Lock()
		defer h.mu.Unlock()
		stream := str(raw["stream_id"])
		delete(h.streams, stream)
		h.closed = append(h.closed, stream)
		return nil
	case "host.stream.emit":
		h.mu.Lock()
		defer h.mu.Unlock()
		h.emitted = append(h.emitted, bytesField(raw["payload"]))
		return nil
	case "host.stream.close":
		if h.completed != nil {
			select {
			case h.completed <- struct{}{}:
			default:
			}
		}
		return nil
	case "host.auth.save":
		name := str(raw["name"])
		path := filepath.Join(h.authDir, name)
		if e := atomicJSON(path, raw["json"]); e != nil {
			return e
		}
		return assign(out, map[string]string{"path": path})
	case "host.auth.list":
		return assign(out, map[string]any{"files": []any{}})
	default:
		return fmt.Errorf("unexpected callback %s", method)
	}
}
func setup(t *testing.T, reply func(string, map[string]any) mockReply) (*Service, *mockHost, Credential) {
	t.Helper()
	dir := t.TempDir()
	authDir := filepath.Join(dir, "auth")
	if e := os.MkdirAll(authDir, 0700); e != nil {
		t.Fatal(e)
	}
	h := &mockHost{reply: reply, streams: map[string]mockReply{}, authDir: authDir}
	s := NewService()
	s.SetHost(h.call)
	yaml := []byte("data_dir: " + filepath.ToSlash(filepath.Join(dir, "data")) + "\nbase_url: http://upstream.example\n")
	if _, e := s.Handle("plugin.register", jsonBytes(map[string]any{"config_yaml": yaml})); e != nil {
		t.Fatal(e)
	}
	c := Credential{Type: Provider, ID: "fixture", APIKey: "fixture-secret-key", Label: "fixture account"}
	if e := s.saveCredential(c); e != nil {
		t.Fatal(e)
	}
	cfg := s.config()
	cfg.Models = []Model{{ID: "alias", UpstreamID: "upstream-model", Endpoints: []string{"/chat/completions"}, Reasoning: ReasoningMapping{Rules: map[string]string{}}}}
	if e := s.saveConfig(cfg); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.shutdown)
	return s, h, c
}
func streamReply(s string) mockReply {
	return mockReply{status: 200, headers: http.Header{"Content-Type": {"text/event-stream"}}, body: s}
}
func chatStream(text string) string {
	return "data: " + string(jsonBytes(completionChunk("completion", "upstream-model", map[string]any{"role": "assistant", "content": text}, nil, nil))) + "\n\ndata: " + string(jsonBytes(completionChunk("completion", "upstream-model", map[string]any{}, "stop", map[string]any{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12, "prompt_tokens_details": map[string]any{"cached_tokens": 4}}))) + "\n\ndata: [DONE]\n\n"
}
func request(c Credential) ExecutorRequest {
	return ExecutorRequest{Model: "alias", StorageJSON: jsonBytes(c), Payload: []byte(`{"messages":[{"role":"user","content":"hello"}],"seed":9007199254740993}`)}
}

func TestProviderAggregationAndNoSecretPersistence(t *testing.T) {
	s, h, c := setup(t, func(_ string, _ map[string]any) mockReply { return streamReply(chatStream("hello")) })
	result, e := s.execute(request(c), false)
	if e != nil {
		t.Fatal(e)
	}
	j, _ := decodeObject(result.(Response).Payload)
	if str(j["model"]) != "alias" || str(object(object(list(j["choices"])[0])["message"])["content"]) != "hello" {
		t.Fatalf("bad completion %s", result.(Response).Payload)
	}
	body, _ := decodeObject(bytesField(h.requests[0]["body"]))
	if fmt.Sprint(body["seed"]) != "9007199254740993" {
		t.Fatal("large integer was rounded")
	}
	if body["stream"] != true || object(body["stream_options"])["include_usage"] != true {
		t.Fatal("missing stream usage request")
	}
	if len(s.logs) != 1 || s.logs[0].PromptTokens != 10 || s.logs[0].CachedTokens != 4 || s.logs[0].TTFTMS == nil {
		t.Fatalf("bad log %+v", s.logs)
	}
	for _, name := range []string{"requests.json", "statistics.json", "settings.json"} {
		b, e := os.ReadFile(filepath.Join(s.config().DataDir, name))
		if e != nil || strings.Contains(string(b), c.APIKey) {
			t.Fatalf("secret persisted in %s", name)
		}
	}
	if len(h.streams) != 0 {
		t.Fatal("HTTP stream leaked")
	}
}
func TestStreamAliasesAndCleanup(t *testing.T) {
	s, h, c := setup(t, func(_ string, _ map[string]any) mockReply { return streamReply(chatStream("hello")) })
	r := request(c)
	h.completed = make(chan struct{}, 1)
	r.StreamID = "downstream"
	r.Metadata = map[string]any{"request_path": "/v1/chat/completions"}
	if _, e := s.execute(r, true); e != nil {
		t.Fatal(e)
	}
	select {
	case <-h.completed:
	case <-time.After(3 * time.Second):
		t.Fatal("output stream was not closed")
	}
	if len(h.emitted) < 2 || len(s.logs) != 1 {
		t.Fatalf("emitted=%d logs=%d", len(h.emitted), len(s.logs))
	}
	for _, frame := range h.emitted {
		j, e := decodeObject(frame)
		if e != nil || str(j["model"]) != "alias" {
			t.Fatalf("invalid downstream %s", frame)
		}
	}
	if len(h.streams) != 0 {
		t.Fatal("HTTP stream leaked")
	}
}
func TestSSEFailuresAndToolCompleteness(t *testing.T) {
	cases := []struct {
		name, body, err string
		ok              bool
	}{{"normal", chatStream("ok"), "", true}, {"clean EOF", strings.ReplaceAll(chatStream("ok"), "data: [DONE]\n\n", ""), "", true}, {"partial frame", "data: {\"choices\":", "", false}, {"no finish", "data: " + string(jsonBytes(completionChunk("id", "m", map[string]any{"content": "partial"}, nil, nil))) + "\n\n", "", false}, {"transport error", chatStream("ok"), "connection reset", true}, {"partial transport", strings.ReplaceAll(chatStream("ok"), "data: [DONE]\n\n", ""), "connection reset", false}, {"invalid tool", "data: " + string(jsonBytes(completionChunk("id", "m", map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "tool", "function": map[string]any{"name": "read", "arguments": "{\"x\":"}}}}, "tool_calls", nil))) + "\n\ndata: [DONE]\n\n", "", false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, c := setup(t, func(_ string, _ map[string]any) mockReply { r := streamReply(tc.body); r.err = tc.err; return r })
			_, e := s.execute(request(c), false)
			if (e == nil) != tc.ok {
				t.Fatalf("error=%v expected ok=%v", e, tc.ok)
			}
		})
	}
}
func TestRateLimitAcrossAliasesAndRedaction(t *testing.T) {
	s, h, c := setup(t, func(_ string, _ map[string]any) mockReply {
		return mockReply{status: 429, headers: http.Header{"Retry-After": {"120"}}, body: `{"error":{"message":"fixture-secret-key failed","code":"rate_limit_error"}}`}
	})
	_, e := s.execute(request(c), false)
	if statusOf(e) != 429 || strings.Contains(e.Error(), c.APIKey) {
		t.Fatalf("unsafe or wrong error %v", e)
	}
	cfg := s.config()
	m := cfg.Models[0]
	m.ID = "second-alias"
	cfg.Models = append(cfg.Models, m)
	if e := s.saveConfig(cfg); e != nil {
		t.Fatal(e)
	}
	r := request(c)
	r.Model = "second-alias"
	_, e = s.execute(r, false)
	if statusOf(e) != 429 || !e.(*APIError).Local || len(h.requests) != 1 {
		t.Fatalf("did not share upstream cooldown: %v requests %d", e, len(h.requests))
	}
}
func TestReasoningMappingPreservesExplicitConfig(t *testing.T) {
	m := Model{Reasoning: ReasoningMapping{Enabled: true, Rules: map[string]string{"unset": "high", "medium": "max", "none": "unset"}}}
	for _, tc := range []struct {
		payload string
		want    any
	}{{`{}`, "high"}, {`{"thinking":{"type":"enabled"}}`, nil}, {`{"reasoning_effort":"medium"}`, "max"}, {`{"reasoning_effort":"none"}`, nil}, {`{"reasoning_effort":"low"}`, "low"}} {
		j, _ := decodeObject([]byte(tc.payload))
		applyReasoning(j, m)
		if j["reasoning_effort"] != tc.want {
			t.Fatalf("mapping %s got %v", tc.payload, j)
		}
	}
}
func TestAnthropicToolImagesAndSignature(t *testing.T) {
	j, _ := decodeObject([]byte(`{"model":"claude","messages":[{"role":"system","content":"rules"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]},{"role":"assistant","content":null,"reasoning_content":"thought","reasoning_signature":"signed","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":[{"type":"text","text":"done"},{"type":"image_url","image_url":{"url":"https://image.example/p.png"}}]}],"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}],"tool_choice":"required","parallel_tool_calls":false}`))
	out, e := toAnthropic(j)
	if e != nil {
		t.Fatal(e)
	}
	messages := list(out["messages"])
	image := object(list(object(messages[0])["content"])[0])
	if str(image["type"]) != "image" || str(object(image["source"])["data"]) != "AAAA" {
		t.Fatal("user image turned into text")
	}
	assistant := list(object(messages[1])["content"])
	if str(object(assistant[0])["signature"]) != "signed" || str(object(assistant[1])["type"]) != "tool_use" {
		t.Fatal("thinking/tool history lost")
	}
	tool := object(list(object(messages[2])["content"])[0])
	if str(tool["tool_use_id"]) != "call_1" || str(object(list(tool["content"])[1])["type"]) != "image" {
		t.Fatal("tool image or ID lost")
	}
	if object(out["tool_choice"])["disable_parallel_tool_use"] != true {
		t.Fatal("tool choice lost")
	}
	delete(object(list(j["messages"])[2]), "reasoning_signature")
	if _, e := toAnthropic(j); e == nil {
		t.Fatal("unsigned thinking was silently replayed")
	}
}
func TestAnthropicStreamToolFragmentsAndUsage(t *testing.T) {
	events := []string{`{"type":"message_start","message":{"id":"message-id","usage":{"input_tokens":10,"output_tokens":0,"cache_read_input_tokens":4,"cache_creation_input_tokens":2}}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call","name":"read","input":{}}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"a\"}"}}`, `{"type":"content_block_stop","index":0}`, `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`, `{"type":"message_stop"}`}
	body := ""
	for _, event := range events {
		body += "data: " + event + "\n\n"
	}
	s, h, c := setup(t, func(_ string, _ map[string]any) mockReply { return streamReply(body) })
	cfg := s.config()
	cfg.Models[0].Endpoints = []string{"/messages"}
	if e := s.saveConfig(cfg); e != nil {
		t.Fatal(e)
	}
	result, e := s.execute(request(c), false)
	if e != nil {
		t.Fatal(e)
	}
	j, _ := decodeObject(result.(Response).Payload)
	msg := object(object(list(j["choices"])[0])["message"])
	tool := object(list(msg["tool_calls"])[0])
	if str(object(tool["function"])["arguments"]) != `{"path":"a"}` {
		t.Fatal("tool fragments lost")
	}
	if number(object(j["usage"])["prompt_tokens"]) != 16 || number(object(j["usage"])["completion_tokens"]) != 3 {
		t.Fatalf("bad Anthropic usage %s", result.(Response).Payload)
	}
	if !strings.HasSuffix(str(h.requests[0]["url"]), "/provider/v1/messages") {
		t.Fatal("wrong Anthropic route")
	}
}
func TestAnthropicEmptyArgumentsAndThinkingFragments(t *testing.T) {
	a := newAnthropicStream("alias")
	cp := newCompletion()
	for _, event := range []string{`{"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":0}}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"thought"}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"first"}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"second"}}`, `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool","name":"ping","input":{}}}`, `{"type":"content_block_stop","index":1}`, `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`, `{"type":"message_stop"}`} {
		j, _ := decodeObject([]byte(event))
		chunks, done, e := a.observe(j)
		if e != nil {
			t.Fatal(e)
		}
		for _, ch := range chunks {
			cp.observe(ch)
		}
		cp.done = done
	}
	if !cp.completeAtEOF() {
		t.Fatal("zero argument tool is incomplete")
	}
	b, e := cp.result("alias")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(b), "firstsecond") {
		t.Fatal("signature fragments lost")
	}
}
func quotaReply(path string, _ map[string]any) mockReply {
	var body string
	switch {
	case strings.Contains(path, "whoami"):
		body = `{"user":{"id":"account"}}`
	case strings.Contains(path, "billing/credits"):
		body = `{"credits":{"monthlyCredits":5,"purchasedCredits":2,"freeCredits":0,"planId":"pro"},"windowLimits":{"fiveHour":{"used":2,"cap":5,"resetAt":1900000000},"weekly":{"used":3,"cap":10,"resetAt":1900000000000}}}`
	case strings.Contains(path, "subscriptions"):
		body = `{"data":{"planId":"pro","currentPeriodEnd":"2026-11-01T00:00:00Z"}}`
	case strings.Contains(path, "summary"):
		body = `{"totalRequests":10}`
	}
	return mockReply{status: 200, headers: http.Header{"Content-Type": {"application/json"}}, body: body}
}
func TestQuotaSharedAccountCacheAndStaleData(t *testing.T) {
	failed := false
	s, h, c := setup(t, func(path string, in map[string]any) mockReply {
		if failed {
			return mockReply{status: 401, body: `{"error":{"message":"fixture-secret-key invalid"}}`}
		}
		return quotaReply(path, in)
	})
	u, e := s.credentialUsage(c.ID, "", true)
	if e != nil || u.Remaining == nil || *u.Remaining != 7 || len(u.Windows) != 2 || u.Windows[0].ResetAt.Unix() != u.Windows[1].ResetAt.Unix() {
		t.Fatalf("bad quota %v %+v", e, u)
	}
	initial := len(h.requests)
	if _, e = s.credentialUsage(c.ID, "", true); e != nil || len(h.requests) != initial {
		t.Fatal("quota cache not used")
	}
	c2 := c
	c2.ID = "second"
	c2.APIKey = "other-fixture-key"
	if e := s.saveCredential(c2); e != nil {
		t.Fatal(e)
	}
	if _, e = s.credentialUsage(c2.ID, "", true); e != nil {
		t.Fatal(e)
	}
	if s.sharedCountLocked(c.ID) != 2 {
		t.Fatal("account identity was not shared")
	}
	failed = true
	past := time.Now().Add(-10 * time.Minute)
	s.usage[c.ID].value.CheckedAt = &past
	stale, e := s.credentialUsage(c.ID, "", true)
	if e != nil || stale.Status != "error" || stale.Remaining == nil || *stale.Remaining != 7 || stale.UpdatedAt == nil || strings.Contains(stale.Error, c.APIKey) {
		t.Fatalf("unsafe or lost stale quota %+v err=%v", stale, e)
	}
}
func TestUnknownQuotaIsNotZero(t *testing.T) {
	s, _, c := setup(t, func(path string, in map[string]any) mockReply {
		if strings.Contains(path, "credits") {
			return mockReply{status: 200, body: `{"credits":{"monthlyCredits":5,"planId":"pro"}}`}
		}
		return quotaReply(path, in)
	})
	u, e := s.credentialUsage(c.ID, "", true)
	if e != nil {
		t.Fatal(e)
	}
	if u.Purchased != nil || u.Free != nil || u.Remaining != nil {
		t.Fatal("missing balance counted as zero")
	}
}
func TestCredentialDeleteAndModelUpdates(t *testing.T) {
	s, _, c := setup(t, func(_ string, _ map[string]any) mockReply { return streamReply(chatStream("ok")) })
	bad := s.config()
	bad.Models = append(bad.Models, bad.Models[0])
	if e := s.saveConfig(bad); e == nil {
		t.Fatal("duplicate alias accepted")
	}
	if len(s.config().Models) != 1 {
		t.Fatal("invalid config mutated live models")
	}
	result, e := s.deleteCredential(c.ID)
	if e != nil || result == nil {
		t.Fatal(e)
	}
	if _, e = s.execute(request(c), false); statusOf(e) != 401 {
		t.Fatal("deleted credential was revived")
	}
}
func TestStatisticsSurviveLogRetentionAndReset(t *testing.T) {
	s, _, c := setup(t, func(_ string, _ map[string]any) mockReply { return streamReply(chatStream("ok")) })
	cfg := s.config()
	cfg.LogRetention = 10
	if e := s.saveConfig(cfg); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 12; i++ {
		if _, e := s.execute(request(c), false); e != nil {
			t.Fatal(e)
		}
	}
	view := s.statisticsView(ManagementRequest{}).(map[string]any)
	if view["totals"].(Counters).Requests != 12 || len(s.logs) != 10 {
		t.Fatal("statistics coupled to log retention")
	}
	r := ManagementRequest{Method: "POST", Path: apiBase + "/statistics/reset"}
	if _, e := s.management(jsonBytes(r)); e != nil {
		t.Fatal(e)
	}
	view = s.statisticsView(ManagementRequest{Query: url.Values{}}).(map[string]any)
	if view["totals"].(Counters).Requests != 0 || len(s.logs) != 10 {
		t.Fatal("reset touched logs or failed")
	}
}

func TestLateOpenIsClosedAndShutdownStopsSampler(t *testing.T) {
	s, h, _ := setup(t, func(_ string, _ map[string]any) mockReply { return streamReply(chatStream("ok")) })
	h.block = make(chan struct{})
	h.opened = make(chan struct{}, 1)
	_, err := s.openUpstream(map[string]string{"url": "http://upstream.example"}, time.Now().Add(30*time.Millisecond))
	if statusOf(err) != 504 {
		t.Fatalf("expected timeout: %v", err)
	}
	close(h.block)
	s.active.Wait()
	h.mu.Lock()
	if len(h.streams) != 0 || len(h.closed) != 1 {
		t.Fatal("late HTTP stream was leaked")
	}
	h.mu.Unlock()
	s.startSampler()
	done := make(chan struct{})
	go func() { s.shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not stop background sampling")
	}
	if _, err := s.openUpstream(nil, time.Time{}); statusOf(err) != 503 {
		t.Fatal("shutdown accepted new HTTP work")
	}
}

func TestPartialQuotaKeepsLastBalance(t *testing.T) {
	partial := false
	s, _, c := setup(t, func(path string, in map[string]any) mockReply {
		if partial && strings.Contains(path, "credits") {
			return mockReply{status: 503, body: `{"error":"temporarily unavailable"}`}
		}
		return quotaReply(path, in)
	})
	if _, err := s.credentialUsage(c.ID, "", true); err != nil {
		t.Fatal(err)
	}
	partial = true
	past := time.Now().Add(-time.Hour)
	s.usage[c.ID].value.CheckedAt = &past
	u, err := s.credentialUsage(c.ID, "", true)
	if err != nil || u.Status != "partial" || u.Remaining == nil || *u.Remaining != 7 || len(u.Windows) != 2 || !contains(u.Unavailable, "credits") {
		t.Fatalf("partial refresh lost last balance: %+v, %v", u, err)
	}
}

func TestCredentialProxyNeverSilentlyBypassed(t *testing.T) {
	s, h, c := setup(t, quotaReply)
	c.ProxyURL = "http://proxy.example:8080"
	if err := s.saveCredential(c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.execute(request(c), false); statusOf(err) != 400 {
		t.Fatal("credential proxy was ignored")
	}
	if _, err := s.getJSON("/alpha/whoami", c, "", time.Time{}); statusOf(err) != 400 {
		t.Fatal("quota proxy was ignored")
	}
	if len(h.requests) != 0 {
		t.Fatal("request bypassed credential proxy")
	}
}

func TestAnthropicReasoningProtocols(t *testing.T) {
	adaptive := map[string]any{"reasoning_effort": "xhigh", "max_tokens": 64}
	if err := applyAnthropicReasoning(adaptive, Model{}); err != nil {
		t.Fatal(err)
	}
	if str(object(adaptive["thinking"])["type"]) != "adaptive" || str(object(adaptive["output_config"])["effort"]) != "high" || adaptive["reasoning_effort"] != nil {
		t.Fatal("invalid adaptive reasoning")
	}
	budget := map[string]any{"reasoning_effort": "medium", "max_tokens": 8192}
	if err := applyAnthropicReasoning(budget, Model{ThinkingMode: "budget"}); err != nil {
		t.Fatal(err)
	}
	if number(object(budget["thinking"])["budget_tokens"]) != 4096 {
		t.Fatal("invalid manual budget")
	}
	if err := applyAnthropicReasoning(map[string]any{"reasoning_effort": "high", "max_tokens": 64}, Model{ThinkingMode: "budget"}); statusOf(err) != 400 {
		t.Fatal("invalid budget accepted")
	}
}
