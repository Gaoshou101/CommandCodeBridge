package bridge

import "testing"

func TestThinkingSuffixMatchesHostRules(t *testing.T) {
	cases := map[string]string{
		"none": "none", "NONE": "none", "auto": "unset", "-1": "unset",
		"minimal": "minimal", "low": "low", "medium": "medium", "HIGH": "high", "xhigh": "xhigh", "max": "max",
		"0": "none", "1": "minimal", "512": "minimal", "513": "low", "1024": "low",
		"1025": "medium", "8192": "medium", "8193": "high", "24576": "high", "24577": "xhigh",
	}
	for raw, want := range cases {
		if got, ok := thinkingSuffixEffort(raw); !ok || got != want {
			t.Errorf("thinkingSuffixEffort(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"", "ultra", " high", "-2", "bogus"} {
		if got, ok := thinkingSuffixEffort(raw); ok {
			t.Errorf("thinkingSuffixEffort(%q) = %q; unknown suffixes must be ignored", raw, got)
		}
	}
}

// Hosts before CLIProxyAPI v8.0.59 pass "alias(level)" to the plugin. A 404
// there cools the base alias down for the credential.
func TestModelResolvesThinkingSuffixToBaseAlias(t *testing.T) {
	s := NewService()
	s.cfg.Models = []Model{{ID: "chat", UpstreamID: "upstream-chat"}, {ID: "odd(name)", UpstreamID: "upstream-odd"}}
	for alias, want := range map[string][2]string{
		"chat":         {"upstream-chat", ""},
		"chat(high)":   {"upstream-chat", "high"},
		"chat (8192)":  {"upstream-chat", "8192"},
		"chat(bogus)":  {"upstream-chat", "bogus"},
		"odd(name)":    {"upstream-odd", ""},
		"odd(name)(1)": {"upstream-odd", "1"},
	} {
		m, suffix, err := s.model(alias)
		if err != nil || m.UpstreamID != want[0] || suffix != want[1] {
			t.Errorf("model(%q) = %q, %q, %v; want %q, %q", alias, m.UpstreamID, suffix, err, want[0], want[1])
		}
	}
	for _, alias := range []string{"missing", "missing(high)", "chat(high"} {
		if _, _, err := s.model(alias); err == nil || statusOf(err) != 404 {
			t.Errorf("model(%q) error = %v, want 404", alias, err)
		}
	}
}

func TestThinkingSuffixOverridesBodyReasoning(t *testing.T) {
	body := map[string]any{"reasoning_effort": "high"}
	if got := applyThinkingSuffix(body, "low"); got != "low" || body["reasoning_effort"] != "low" {
		t.Fatalf("suffix low: effort %q, body %v", got, body)
	}
	body = map[string]any{"reasoning_effort": "high"}
	if got := applyThinkingSuffix(body, "auto"); got != "unset" || body["reasoning_effort"] != nil {
		t.Fatalf("suffix auto: effort %q, body %v", got, body)
	}
	body = map[string]any{"reasoning_effort": "high"}
	if got := applyThinkingSuffix(body, "ultra"); got != "" || body["reasoning_effort"] != "high" {
		t.Fatalf("unknown suffix: effort %q, body %v", got, body)
	}
	// Newer hosts translate the "auto" suffix into reasoning_effort=auto.
	body = map[string]any{"reasoning_effort": "auto"}
	if got := applyThinkingSuffix(body, ""); got != "" || body["reasoning_effort"] != nil {
		t.Fatalf("host auto: effort %q, body %v", got, body)
	}
	if got := originalThinkingSuffix([]byte(`{"model":"chat(xhigh)","messages":[]}`), "chat"); got != "xhigh" {
		t.Fatalf("original suffix = %q", got)
	}
	if got := originalThinkingSuffix([]byte(`{"model":"other(xhigh)"}`), "chat"); got != "" {
		t.Fatalf("original suffix for another alias = %q", got)
	}
}
