package bridge

import (
	"errors"
	"strings"
	"sync"
)

// Some Claude models served by CommandCode reject request shapes that other
// Claude models accept, and the catalog does not describe these limits:
//
//   - claude-sonnet-5-5 rejects thinking.type "disabled" and asks for
//     "between_tools", which CommandCode's own validation rejects for every
//     model. Thinking therefore cannot be turned off; the closest request is
//     adaptive thinking at low effort, which in live tests answered without
//     thinking.
//   - claude-sonnet-5-5 rejects forced tool_choice ("tool" / "any"); "auto"
//     with the same tools still calls the requested tool when asked to.
//
// The bridge retries such a rejection once with the nearest accepted request.
// Only after the adjusted request is accepted does it remember the model for
// later requests in this process (until restart or a base_url change), and it
// records every change on the request log so the substitution is never silent.
const (
	adjustThinkingOff = "thinking_off_unsupported:adaptive_low"
	adjustForcedTool  = "forced_tool_choice_unsupported:auto"
)

type anthropicLimits struct {
	mu     sync.Mutex
	models map[string]map[string]bool
}

func (l *anthropicLimits) known(model string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []string{}
	for _, adjustment := range []string{adjustThinkingOff, adjustForcedTool} {
		if l.models[model][adjustment] {
			out = append(out, adjustment)
		}
	}
	return out
}

func (l *anthropicLimits) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.models = nil
}

func (l *anthropicLimits) learn(model, adjustment string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.models == nil {
		l.models = map[string]map[string]bool{}
	}
	if l.models[model] == nil {
		l.models[model] = map[string]bool{}
	}
	l.models[model][adjustment] = true
}

// rejectedAdjustment maps an upstream 400 to the adjustment that avoids it.
func rejectedAdjustment(err error, body map[string]any) string {
	var api *APIError
	if !errors.As(err, &api) || api.Status != 400 || api.Local {
		return ""
	}
	message := strings.ToLower(api.Message)
	if strings.Contains(message, "between_tools") && thinkingDisabled(body) {
		return adjustThinkingOff
	}
	if forcedToolChoice(body) && mentionsToolChoice(message) && (strings.Contains(message, "not supported") || strings.Contains(message, "unsupported")) {
		return adjustForcedTool
	}
	return ""
}

func mentionsToolChoice(message string) bool {
	for _, phrase := range []string{"tool_choice", "tool choice", "forced tool", "forces tool"} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

func thinkingDisabled(body map[string]any) bool {
	return str(object(body["thinking"])["type"]) == "disabled"
}

func forcedToolChoice(body map[string]any) bool {
	kind := str(object(body["tool_choice"])["type"])
	return kind == "tool" || kind == "any"
}

// applyAdjustment rewrites body in place and reports whether anything changed.
func applyAdjustment(body map[string]any, adjustment string) bool {
	switch adjustment {
	case adjustThinkingOff:
		if !thinkingDisabled(body) {
			return false
		}
		body["thinking"] = map[string]any{"type": "adaptive"}
		output := object(body["output_config"])
		if output == nil {
			output = map[string]any{}
		}
		output["effort"] = "low"
		body["output_config"] = output
		return true
	case adjustForcedTool:
		if !forcedToolChoice(body) {
			return false
		}
		choice := map[string]any{"type": "auto"}
		if parallel, ok := object(body["tool_choice"])["disable_parallel_tool_use"]; ok {
			choice["disable_parallel_tool_use"] = parallel
		}
		body["tool_choice"] = choice
		return true
	}
	return false
}

func noteAdjustment(entry *LogEntry, adjustment string) {
	for _, existing := range entry.Adjustments {
		if existing == adjustment {
			return
		}
	}
	entry.Adjustments = append(entry.Adjustments, adjustment)
	if adjustment == adjustThinkingOff && entry.ReasoningTo == "none" {
		entry.ReasoningTo = "low"
	}
}
