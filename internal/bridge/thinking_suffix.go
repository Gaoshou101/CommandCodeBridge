package bridge

import (
	"encoding/json"
	"strconv"
	"strings"
)

// CPA lets clients append a thinking level or budget to a model name, such as
// "gpt-5(high)" or "claude-sonnet(16384)". Since CLIProxyAPI c9af529 (first
// released in v8.0.59) the host strips that suffix and injects the matching
// reasoning settings before it calls a plugin. Older hosts, including the
// pinned v8.0.23 baseline, pass the suffixed name through unchanged. A 404
// model_not_found for that name made those hosts cool down the base model for
// the credential, so later unsuffixed requests failed with auth_unavailable.
// The bridge therefore resolves the suffix itself with the host's rules.

// splitThinkingSuffix mirrors CPA thinking.ParseSuffix.
func splitThinkingSuffix(model string) (base, raw string, ok bool) {
	open := strings.LastIndex(model, "(")
	if open < 0 || !strings.HasSuffix(model, ")") {
		return model, "", false
	}
	return strings.TrimSpace(model[:open]), model[open+1 : len(model)-1], true
}

// thinkingSuffixEffort converts a raw suffix to a Chat reasoning_effort using
// CPA's special, level and budget-to-level rules. "unset" requests automatic
// thinking, which leaves the upstream default in place. Unknown suffixes are
// ignored, matching hosts that strip the suffix before the plugin call.
func thinkingSuffixEffort(raw string) (string, bool) {
	switch lower := strings.ToLower(raw); lower {
	case "none":
		return "none", true
	case "auto", "-1":
		return "unset", true
	case "minimal", "low", "medium", "high", "xhigh", "max":
		return lower, true
	}
	budget, err := strconv.Atoi(raw)
	switch {
	case err != nil || budget < 0:
		return "", false
	case budget == 0:
		return "none", true
	case budget <= 512:
		return "minimal", true
	case budget <= 1024:
		return "low", true
	case budget <= 8192:
		return "medium", true
	case budget <= 24576:
		return "high", true
	default:
		return "xhigh", true
	}
}

// originalThinkingSuffix recovers the client's suffix when a newer host has
// already removed it from the executor model. Native Messages requests are
// rebuilt from the original body, so the suffix must be reapplied there.
func originalThinkingSuffix(original []byte, alias string) string {
	if len(original) == 0 {
		return ""
	}
	var request struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(original, &request) != nil {
		return ""
	}
	if base, raw, ok := splitThinkingSuffix(request.Model); ok && base == alias {
		return raw
	}
	return ""
}

// applyThinkingSuffix lets a recognized suffix override body reasoning, as the
// host pipeline does. CPA's internal "auto" level is not a Chat
// reasoning_effort value, so it is removed instead of being sent upstream.
func applyThinkingSuffix(body map[string]any, raw string) string {
	effort, ok := thinkingSuffixEffort(raw)
	if ok {
		body["reasoning_effort"] = effort
	}
	if value := str(body["reasoning_effort"]); value == "unset" || value == "auto" {
		delete(body, "reasoning_effort")
	}
	if !ok {
		return ""
	}
	return effort
}
