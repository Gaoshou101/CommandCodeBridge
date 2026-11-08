package bridge

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

func anthropicTextStream(text string) string {
	events := []string{
		`{"type":"message_start","message":{"id":"message-id","usage":{"input_tokens":5,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":` + string(jsonBytes(text)) + `}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`{"type":"message_stop"}`,
	}
	body := ""
	for _, event := range events {
		body += "data: " + event + "\n\n"
	}
	return body
}

func anthropicError(status int, message string) mockReply {
	return mockReply{status: status, headers: http.Header{"Content-Type": {"application/json"}}, body: string(jsonBytes(map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error", "message": message}}))}
}

// Live CommandCode wording for claude-sonnet-5-5, 2026-11-08.
const betweenToolsMessage = `To turn thinking off on this model, send "thinking": {"type": "between_tools"} instead of {"type": "disabled"}. The model does not think before responding.`

func useMessagesModel(t *testing.T, s *Service) {
	t.Helper()
	cfg := s.config()
	cfg.Models[0].Endpoints = []string{"/messages"}
	if e := s.saveConfig(cfg); e != nil {
		t.Fatal(e)
	}
}

func requestBody(t *testing.T, h *mockHost, i int) map[string]any {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	body, e := decodeObject(bytesField(h.requests[i]["body"]))
	if e != nil {
		t.Fatal(e)
	}
	return body
}

func requestCount(h *mockHost) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.requests)
}

func lastLog(s *Service) LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.logs[len(s.logs)-1]
}

func TestThinkingOffFallsBackToAdaptiveLowAndIsRemembered(t *testing.T) {
	s, h, c := setup(t, func(_ string, raw map[string]any) mockReply {
		body, _ := decodeObject(bytesField(raw["body"]))
		if str(object(body["thinking"])["type"]) == "disabled" {
			return anthropicError(400, betweenToolsMessage)
		}
		return streamReply(anthropicTextStream("ok"))
	})
	useMessagesModel(t, s)
	r := request(c)
	r.Model = "alias(none)"
	result, e := s.execute(r, false)
	if e != nil {
		t.Fatal(e)
	}
	j, _ := decodeObject(result.(Response).Payload)
	if str(object(object(list(j["choices"])[0])["message"])["content"]) != "ok" {
		t.Fatalf("unexpected response %s", result.(Response).Payload)
	}
	if n := requestCount(h); n != 2 {
		t.Fatalf("expected one retry, got %d upstream requests", n)
	}
	if str(object(requestBody(t, h, 0)["thinking"])["type"]) != "disabled" {
		t.Fatal("first request did not ask to disable thinking")
	}
	retried := requestBody(t, h, 1)
	if str(object(retried["thinking"])["type"]) != "adaptive" || str(object(retried["output_config"])["effort"]) != "low" {
		t.Fatalf("retry was not adaptive low: %v", retried)
	}
	entry := lastLog(s)
	if !reflect.DeepEqual(entry.Adjustments, []string{adjustThinkingOff}) || entry.ReasoningFrom != "none" || entry.ReasoningTo != "low" || entry.Status != 200 {
		t.Fatalf("adjustment not recorded: %+v", entry)
	}

	// The model is remembered: the next request goes out adjusted at once.
	if _, e := s.execute(r, false); e != nil {
		t.Fatal(e)
	}
	if n := requestCount(h); n != 3 {
		t.Fatalf("remembered adjustment still retried: %d upstream requests", n)
	}
	if str(object(requestBody(t, h, 2)["thinking"])["type"]) != "adaptive" {
		t.Fatal("remembered adjustment not applied")
	}
	if entry := lastLog(s); !reflect.DeepEqual(entry.Adjustments, []string{adjustThinkingOff}) || entry.ReasoningTo != "low" {
		t.Fatalf("remembered adjustment not recorded: %+v", entry)
	}

	// Requests that do not disable thinking are untouched.
	r.Model = "alias(high)"
	if _, e := s.execute(r, false); e != nil {
		t.Fatal(e)
	}
	if body := requestBody(t, h, 3); str(object(body["output_config"])["effort"]) != "high" {
		t.Fatalf("unrelated request changed: %v", body)
	}
	if entry := lastLog(s); len(entry.Adjustments) != 0 {
		t.Fatalf("unrelated request recorded an adjustment: %+v", entry)
	}

	// A base_url change forgets learned limits.
	cfg := s.config()
	cfg.BaseURL = "http://other.example"
	if e := s.saveConfig(cfg); e != nil {
		t.Fatal(e)
	}
	if known := s.anthropicLimits.known("upstream-model"); len(known) != 0 {
		t.Fatalf("limits survived base_url change: %v", known)
	}
}

func TestFailedRetryIsNotRemembered(t *testing.T) {
	s, h, c := setup(t, func(_ string, raw map[string]any) mockReply {
		body, _ := decodeObject(bytesField(raw["body"]))
		if str(object(body["thinking"])["type"]) == "disabled" {
			return anthropicError(400, betweenToolsMessage)
		}
		return anthropicError(400, "Invalid input at thinking")
	})
	useMessagesModel(t, s)
	r := request(c)
	r.Model = "alias(none)"
	if _, e := s.execute(r, false); statusOf(e) != 400 {
		t.Fatalf("expected the retry's 400, got %v", e)
	}
	if n := requestCount(h); n != 2 {
		t.Fatalf("expected exactly one retry, got %d upstream requests", n)
	}
	if known := s.anthropicLimits.known("upstream-model"); len(known) != 0 {
		t.Fatalf("rejected adjustment was remembered: %v", known)
	}
	if entry := lastLog(s); !reflect.DeepEqual(entry.Adjustments, []string{adjustThinkingOff}) || entry.Status != 400 {
		t.Fatalf("attempted adjustment not recorded: %+v", entry)
	}
}

func TestOtherUpstreamErrorsAreNotRetried(t *testing.T) {
	for name, reply := range map[string]mockReply{
		"unrelated 400":     anthropicError(400, "max_tokens is too large"),
		"between_tools 500": anthropicError(500, betweenToolsMessage),
		"other unsupported": anthropicError(400, "image input is not supported for this model"),
	} {
		t.Run(name, func(t *testing.T) {
			s, h, c := setup(t, func(string, map[string]any) mockReply { return reply })
			useMessagesModel(t, s)
			r := request(c)
			r.Model = "alias(none)"
			r.Payload = []byte(`{"messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object"}}}],"tool_choice":"required"}`)
			if _, e := s.execute(r, false); statusOf(e) != reply.status {
				t.Fatalf("expected %d, got %v", reply.status, e)
			}
			if n := requestCount(h); n != 1 {
				t.Fatalf("unexpected retry: %d upstream requests", n)
			}
			if entry := lastLog(s); len(entry.Adjustments) != 0 {
				t.Fatalf("unexpected adjustment: %+v", entry)
			}
		})
	}
}

func TestForcedToolChoiceFallsBackToAuto(t *testing.T) {
	s, h, c := setup(t, func(_ string, raw map[string]any) mockReply {
		body, _ := decodeObject(bytesField(raw["body"]))
		if kind := str(object(body["tool_choice"])["type"]); kind == "tool" || kind == "any" {
			return anthropicError(400, "tool_choice type '"+kind+"' is not supported for this model")
		}
		return streamReply(anthropicTextStream("ok"))
	})
	useMessagesModel(t, s)
	r := request(c)
	r.Payload = []byte(`{"messages":[{"role":"user","content":"read a"}],"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"read"}},"parallel_tool_calls":false}`)
	if _, e := s.execute(r, false); e != nil {
		t.Fatal(e)
	}
	if n := requestCount(h); n != 2 {
		t.Fatalf("expected one retry, got %d upstream requests", n)
	}
	if first := object(requestBody(t, h, 0)["tool_choice"]); str(first["type"]) != "tool" || str(first["name"]) != "read" {
		t.Fatalf("first request was not forced: %v", first)
	}
	retried := object(requestBody(t, h, 1)["tool_choice"])
	if !reflect.DeepEqual(retried, map[string]any{"type": "auto", "disable_parallel_tool_use": true}) {
		t.Fatalf("retry tool_choice = %v", retried)
	}
	if entry := lastLog(s); !reflect.DeepEqual(entry.Adjustments, []string{adjustForcedTool}) || entry.ReasoningTo != entry.ReasoningFrom {
		t.Fatalf("adjustment not recorded: %+v", entry)
	}
	if known := s.anthropicLimits.known("upstream-model"); !reflect.DeepEqual(known, []string{adjustForcedTool}) {
		t.Fatalf("limit not remembered: %v", known)
	}
}

func TestBothAdjustmentsInOneRequest(t *testing.T) {
	s, h, c := setup(t, func(_ string, raw map[string]any) mockReply {
		body, _ := decodeObject(bytesField(raw["body"]))
		if str(object(body["thinking"])["type"]) == "disabled" {
			return anthropicError(400, betweenToolsMessage)
		}
		if str(object(body["tool_choice"])["type"]) == "any" {
			return anthropicError(400, "tool_choice is not supported for this model")
		}
		return streamReply(anthropicTextStream("ok"))
	})
	useMessagesModel(t, s)
	r := request(c)
	r.Model = "alias(none)"
	r.Payload = []byte(`{"messages":[{"role":"user","content":"read a"}],"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object"}}}],"tool_choice":"required"}`)
	// Streaming: the retries happen before any output reaches the client.
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
	if n := requestCount(h); n != 3 {
		t.Fatalf("expected two retries, got %d upstream requests", n)
	}
	final := requestBody(t, h, 2)
	if str(object(final["thinking"])["type"]) != "adaptive" || str(object(final["tool_choice"])["type"]) != "auto" {
		t.Fatalf("final request = %v", final)
	}
	if entry := lastLog(s); !reflect.DeepEqual(entry.Adjustments, []string{adjustThinkingOff, adjustForcedTool}) {
		t.Fatalf("adjustments = %v", entry.Adjustments)
	}
}

func TestChatModelsAreNeverAdjusted(t *testing.T) {
	s, h, c := setup(t, func(string, map[string]any) mockReply {
		return anthropicError(400, betweenToolsMessage)
	})
	r := request(c)
	r.Model = "alias(none)"
	if _, e := s.execute(r, false); statusOf(e) != 400 {
		t.Fatalf("expected 400, got %v", e)
	}
	if n := requestCount(h); n != 1 {
		t.Fatalf("chat request retried: %d upstream requests", n)
	}
	if entry := lastLog(s); len(entry.Adjustments) != 0 {
		t.Fatalf("chat request adjusted: %+v", entry)
	}
}

func TestApplyAdjustmentOnlyChangesMatchingRequests(t *testing.T) {
	body := map[string]any{"thinking": map[string]any{"type": "adaptive"}, "tool_choice": map[string]any{"type": "auto"}}
	if applyAdjustment(body, adjustThinkingOff) || applyAdjustment(body, adjustForcedTool) || applyAdjustment(body, "unknown") {
		t.Fatal("adjustment reported a change on a compatible request")
	}
	body = map[string]any{"thinking": map[string]any{"type": "disabled"}, "output_config": map[string]any{"format": "kept"}}
	if !applyAdjustment(body, adjustThinkingOff) || !reflect.DeepEqual(body["output_config"], map[string]any{"format": "kept", "effort": "low"}) {
		t.Fatalf("output_config not merged: %v", body)
	}
	if rejectedAdjustment(&APIError{Status: 400, Message: betweenToolsMessage, Local: true}, map[string]any{"thinking": map[string]any{"type": "disabled"}}) != "" {
		t.Fatal("local error treated as an upstream limit")
	}
}

func TestDefaultTimeoutCoversLongReasoning(t *testing.T) {
	if got := defaultConfig().TimeoutSeconds; got != 1800 {
		t.Fatalf("default timeout_seconds = %d, want 1800", got)
	}
	cfg := defaultConfig()
	if e := cfg.validate(); e != nil {
		t.Fatal(e)
	}
}
