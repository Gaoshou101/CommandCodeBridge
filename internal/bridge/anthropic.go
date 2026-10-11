package bridge

import (
	"strings"
)

// Convert only explicit, supported fields. Unsupported input is rejected rather
// than silently dropping tool definitions, image blocks or thinking history.
func anthropicContent(content any) ([]any, error) {
	if s, ok := content.(string); ok {
		return []any{map[string]any{"type": "text", "text": s}}, nil
	}
	if content == nil {
		return []any{}, nil
	}
	parts, ok := content.([]any)
	if !ok {
		return nil, fail(400, "message content must be text or a block array")
	}
	out := []any{}
	for _, v := range parts {
		p := object(v)
		switch str(p["type"]) {
		case "text":
			out = append(out, p)
		case "image_url":
			image := str(object(p["image_url"])["url"])
			source := map[string]any{"type": "url", "url": image}
			if strings.HasPrefix(image, "data:") {
				comma := strings.Index(image, ",")
				if comma < 0 || !strings.HasSuffix(image[:comma], ";base64") {
					return nil, fail(400, "invalid base64 image URL")
				}
				source = map[string]any{"type": "base64", "media_type": strings.TrimSuffix(strings.TrimPrefix(image[:comma], "data:"), ";base64"), "data": image[comma+1:]}
			}
			out = append(out, map[string]any{"type": "image", "source": source})
		case "image", "thinking", "redacted_thinking", "tool_use", "tool_result":
			out = append(out, p)
		default:
			return nil, unknownFormat(v)
		}
	}
	return out, nil
}
func toAnthropic(chat map[string]any) (map[string]any, error) {
	out := map[string]any{"model": chat["model"], "max_tokens": 4096}
	for _, key := range []string{"max_tokens", "temperature", "top_p", "metadata", "thinking", "output_config", "reasoning_effort"} {
		if chat[key] != nil {
			out[key] = chat[key]
		}
	}
	if chat["max_completion_tokens"] != nil {
		out["max_tokens"] = chat["max_completion_tokens"]
	}
	if number(chat["n"]) > 1 {
		return nil, fail(400, "Anthropic models support one completion per request")
	}
	for _, k := range []string{"response_format", "functions", "function_call", "audio", "modalities"} {
		if chat[k] != nil {
			return nil, fail(400, k+" is not supported for Anthropic models")
		}
	}
	if stop := chat["stop"]; stop != nil {
		if text, ok := stop.(string); ok {
			out["stop_sequences"] = []any{text}
		} else {
			out["stop_sequences"] = stop
		}
	}
	system := []any{}
	messages := []any{}
	for _, v := range list(chat["messages"]) {
		msg := object(v)
		role := str(msg["role"])
		parts, e := anthropicContent(msg["content"])
		if e != nil {
			return nil, e
		}
		if role == "system" || role == "developer" {
			system = append(system, parts...)
			continue
		}
		if role == "tool" {
			role = "user"
			parts = []any{map[string]any{"type": "tool_result", "tool_use_id": msg["tool_call_id"], "content": parts}}
		} else if role == "assistant" {
			if redacted := object(msg["redacted_thinking"]); redacted != nil {
				parts = append([]any{redacted}, parts...)
			}
			if thinking := str(msg["reasoning_content"]); thinking != "" {
				signature := str(msg["reasoning_signature"])
				if signature == "" {
					return nil, fail(400, "Anthropic thinking history requires reasoning_signature")
				}
				parts = append([]any{map[string]any{"type": "thinking", "thinking": thinking, "signature": signature}}, parts...)
			}
			for _, tc := range list(msg["tool_calls"]) {
				tool := object(tc)
				f := object(tool["function"])
				input, e := decodeObject([]byte(str(f["arguments"])))
				if e != nil {
					return nil, fail(400, "tool call arguments must be a JSON object")
				}
				parts = append(parts, map[string]any{"type": "tool_use", "id": tool["id"], "name": f["name"], "input": input})
			}
		} else if role != "user" {
			return nil, fail(400, "unsupported message role for Anthropic model")
		}
		if len(parts) == 0 {
			parts = append(parts, map[string]any{"type": "text", "text": ""})
		}
		if len(messages) > 0 && str(object(messages[len(messages)-1])["role"]) == role {
			last := object(messages[len(messages)-1])
			last["content"] = append(list(last["content"]), parts...)
		} else {
			messages = append(messages, map[string]any{"role": role, "content": parts})
		}
	}
	out["messages"] = messages
	if len(system) > 0 {
		out["system"] = system
	}
	tools := []any{}
	for _, v := range list(chat["tools"]) {
		t := object(v)
		if str(t["type"]) != "function" {
			return nil, fail(400, "Anthropic chat bridge accepts function tools only")
		}
		f := object(t["function"])
		schema := f["parameters"]
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tool := map[string]any{"name": f["name"], "input_schema": schema}
		if f["description"] != nil {
			tool["description"] = f["description"]
		}
		if f["cache_control"] != nil {
			tool["cache_control"] = f["cache_control"]
		}
		tools = append(tools, tool)
	}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	if choice := chat["tool_choice"]; choice != nil {
		switch c := choice.(type) {
		case string:
			switch c {
			case "auto":
				out["tool_choice"] = map[string]any{"type": "auto"}
			case "required":
				out["tool_choice"] = map[string]any{"type": "any"}
			case "none":
				out["tool_choice"] = map[string]any{"type": "none"}
			default:
				return nil, fail(400, "unsupported tool_choice")
			}
		case map[string]any:
			out["tool_choice"] = map[string]any{"type": "tool", "name": object(c["function"])["name"]}
		default:
			return nil, fail(400, "invalid tool_choice")
		}
	}
	if parallel, ok := chat["parallel_tool_calls"].(bool); ok && !parallel {
		choice := object(out["tool_choice"])
		if choice == nil {
			choice = map[string]any{"type": "auto"}
		}
		choice["disable_parallel_tool_use"] = true
		out["tool_choice"] = choice
	}
	return out, nil
}

// Messages accepts output_config.effort or thinking.budget_tokens, rather than
// Chat's reasoning_effort. The protocol is selectable per model.
func applyAnthropicReasoning(body map[string]any, m Model) error {
	effort := str(body["reasoning_effort"])
	delete(body, "reasoning_effort")
	if effort == "" || effort == "unset" {
		return nil
	}
	if !efforts[effort] {
		return fail(400, "unsupported Anthropic reasoning effort")
	}
	output := object(body["output_config"])
	if output == nil {
		output = map[string]any{}
	}
	if effort == "none" {
		body["thinking"] = map[string]any{"type": "disabled"}
		delete(output, "effort")
	} else if m.ThinkingMode == "budget" {
		budgets := map[string]int64{"minimal": 1024, "low": 2048, "medium": 4096, "high": 8192, "xhigh": 16384, "max": 32768, "ultra": 32768}
		budget := budgets[effort]
		if max := number(body["max_tokens"]); max <= budget {
			return fail(400, "max_tokens must exceed the mapped thinking budget; increase max_tokens or select a lower effort")
		}
		body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
		delete(output, "effort")
	} else {
		mapped := effort
		switch effort {
		case "minimal":
			mapped = "low"
		case "xhigh":
			mapped = "high"
		case "ultra":
			mapped = "max"
		}
		body["thinking"] = map[string]any{"type": "adaptive"}
		output["effort"] = mapped
	}
	if len(output) > 0 {
		body["output_config"] = output
	} else {
		delete(body, "output_config")
	}
	return nil
}

type anthropicStream struct {
	id, model        string
	tools            map[int64]int
	nextTool         int
	usage            map[string]any
	stopped          bool
	finish           string
	signature        string
	toolHasArguments map[int64]bool
}

func newAnthropicStream(model string) *anthropicStream {
	return &anthropicStream{id: "chatcmpl-" + id(), model: model, tools: map[int64]int{}, toolHasArguments: map[int64]bool{}, usage: map[string]any{}}
}
func (a *anthropicStream) chunk(delta any, finish any, usage any) map[string]any {
	return completionChunk(a.id, a.model, delta, finish, usage)
}
func (a *anthropicStream) observe(j map[string]any) ([]map[string]any, bool, error) {
	out := []map[string]any{}
	switch str(j["type"]) {
	case "message_start":
		m := object(j["message"])
		if v := str(m["id"]); v != "" {
			a.id = v
		}
		a.observeUsage(object(m["usage"]))
		out = append(out, a.chunk(map[string]any{"role": "assistant", "content": ""}, nil, nil))
	case "content_block_start":
		block := object(j["content_block"])
		idx := number(j["index"])
		switch str(block["type"]) {
		case "tool_use":
			toolIdx := a.nextTool
			a.nextTool++
			a.tools[idx] = toolIdx
			args := ""
			if input := object(block["input"]); len(input) > 0 {
				args = string(jsonBytes(input))
				a.toolHasArguments[idx] = true
			}
			out = append(out, a.chunk(map[string]any{"tool_calls": []any{map[string]any{"index": toolIdx, "id": block["id"], "type": "function", "function": map[string]any{"name": block["name"], "arguments": args}}}}, nil, nil))
		case "text":
			if text := str(block["text"]); text != "" {
				out = append(out, a.chunk(map[string]any{"content": text}, nil, nil))
			}
		case "thinking":
			if text := str(block["thinking"]); text != "" {
				out = append(out, a.chunk(map[string]any{"reasoning_content": text}, nil, nil))
			}
		case "redacted_thinking":
			out = append(out, a.chunk(map[string]any{"redacted_thinking": block}, nil, nil))
		default:
			return nil, false, fail(502, "unsupported Anthropic content block")
		}
	case "content_block_delta":
		d := object(j["delta"])
		switch str(d["type"]) {
		case "text_delta":
			out = append(out, a.chunk(map[string]any{"content": d["text"]}, nil, nil))
		case "thinking_delta":
			out = append(out, a.chunk(map[string]any{"reasoning_content": d["thinking"]}, nil, nil))
		case "signature_delta":
			a.signature += str(d["signature"])
			out = append(out, a.chunk(map[string]any{"reasoning_signature": str(d["signature"])}, nil, nil))
		case "input_json_delta":
			idx, ok := a.tools[number(j["index"])]
			if !ok {
				return nil, false, fail(502, "tool delta has no tool start")
			}
			if str(d["partial_json"]) != "" {
				a.toolHasArguments[number(j["index"])] = true
			}
			out = append(out, a.chunk(map[string]any{"tool_calls": []any{map[string]any{"index": idx, "function": map[string]any{"arguments": d["partial_json"]}}}}, nil, nil))
		default:
			return nil, false, fail(502, "unsupported Anthropic content delta")
		}
	case "message_delta":
		d := object(j["delta"])
		a.observeUsage(object(j["usage"]))
		if reason := str(d["stop_reason"]); reason != "" {
			switch reason {
			case "tool_use":
				a.finish = "tool_calls"
			case "max_tokens":
				a.finish = "length"
			case "end_turn", "stop_sequence":
				a.finish = "stop"
			case "refusal":
				a.finish = "stop"
			default:
				return nil, false, fail(502, "unsupported Anthropic stop reason")
			}
			a.stopped = true
			out = append(out, a.chunk(map[string]any{}, a.finish, a.usage))
		}
	case "message_stop":
		if !a.stopped {
			return nil, false, fail(502, "Anthropic stream ended without stop_reason")
		}
		return out, true, nil
	case "content_block_stop":
		blockIndex := number(j["index"])
		if toolIndex, ok := a.tools[blockIndex]; ok && !a.toolHasArguments[blockIndex] {
			out = append(out, a.chunk(map[string]any{"tool_calls": []any{map[string]any{"index": toolIndex, "function": map[string]any{"arguments": "{}"}}}}, nil, nil))
			a.toolHasArguments[blockIndex] = true
		}
	case "ping":
	case "error":
		return nil, false, upstreamError(502, nil, j)
	default:
		return nil, false, fail(502, "unknown Anthropic stream event")
	}
	return out, false, nil
}
func (a *anthropicStream) observeUsage(u map[string]any) {
	if u == nil {
		return
	}
	for _, key := range []string{"input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"} {
		if u[key] != nil {
			a.usage[key] = u[key]
		}
	}
	input := number(a.usage["input_tokens"]) + number(a.usage["cache_read_input_tokens"]) + number(a.usage["cache_creation_input_tokens"])
	if a.usage["input_tokens"] != nil {
		a.usage["prompt_tokens"] = input
	}
	if a.usage["output_tokens"] != nil {
		a.usage["completion_tokens"] = a.usage["output_tokens"]
	}
	a.usage["total_tokens"] = input + number(a.usage["output_tokens"])
	a.usage["prompt_tokens_details"] = map[string]any{"cached_tokens": number(a.usage["cache_read_input_tokens"]), "cache_write_tokens": number(a.usage["cache_creation_input_tokens"])}
}
