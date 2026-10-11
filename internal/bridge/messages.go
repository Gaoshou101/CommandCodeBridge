package bridge

import (
	"sort"
)

// Native Messages output avoids the host's Chat -> Messages conversion, which
// does not preserve Anthropic signatures and redacted thinking blocks.
type messageCompletion struct {
	root   map[string]any
	blocks map[int64]map[string]any
	args   map[int64]string
	closed map[int64]bool
	done   bool
}

func newMessageCompletion() *messageCompletion {
	return &messageCompletion{root: map[string]any{}, blocks: map[int64]map[string]any{}, args: map[int64]string{}, closed: map[int64]bool{}}
}
func (m *messageCompletion) observe(j map[string]any) error {
	i := number(j["index"])
	switch str(j["type"]) {
	case "message_start":
		m.root = object(j["message"])
	case "content_block_start":
		m.blocks[i] = object(j["content_block"])
	case "content_block_delta":
		block := m.blocks[i]
		if block == nil || m.closed[i] {
			return fail(502, "Messages delta has no active block")
		}
		d := object(j["delta"])
		switch str(d["type"]) {
		case "text_delta":
			block["text"] = str(block["text"]) + str(d["text"])
		case "thinking_delta":
			block["thinking"] = str(block["thinking"]) + str(d["thinking"])
		case "signature_delta":
			block["signature"] = str(block["signature"]) + str(d["signature"])
		case "input_json_delta":
			m.args[i] += str(d["partial_json"])
		default:
			return fail(502, "unsupported Messages delta")
		}
	case "content_block_stop":
		if m.blocks[i] == nil {
			return fail(502, "Messages block stop has no start")
		}
		if args := m.args[i]; args != "" {
			input, err := decodeObject([]byte(args))
			if err != nil {
				return fail(502, "Messages tool arguments are incomplete")
			}
			m.blocks[i]["input"] = input
		}
		m.closed[i] = true
	case "message_delta":
		mergeObjects(m.root, object(j["delta"]), false)
		u := object(m.root["usage"])
		if u == nil {
			u = map[string]any{}
			m.root["usage"] = u
		}
		mergeObjects(u, object(j["usage"]), false)
	case "message_stop":
		m.done = true
	}
	return nil
}
func (m *messageCompletion) result(model string) ([]byte, error) {
	if !m.done || str(m.root["stop_reason"]) == "" {
		return nil, fail(502, "Messages response is incomplete")
	}
	indices := make([]int64, 0, len(m.blocks))
	for i := range m.blocks {
		if !m.closed[i] {
			return nil, fail(502, "Messages content block is incomplete")
		}
		indices = append(indices, i)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	content := []any{}
	for _, i := range indices {
		content = append(content, m.blocks[i])
	}
	m.root["content"], m.root["model"] = content, model
	return jsonBytes(m.root), nil
}
func messageFrame(j map[string]any) []byte {
	return []byte("event: " + str(j["type"]) + "\ndata: " + string(jsonBytes(j)) + "\n\n")
}
func messageOutputStarted(j map[string]any) bool {
	switch str(j["type"]) {
	case "content_block_start":
		b := object(j["content_block"])
		return str(b["type"]) == "tool_use" || str(b["type"]) == "redacted_thinking" || str(b["text"]) != "" || str(b["thinking"]) != ""
	case "content_block_delta":
		d := object(j["delta"])
		return str(d["text"]) != "" || str(d["thinking"]) != "" || str(d["partial_json"]) != ""
	}
	return false
}
func stopReason(finish string) string {
	switch finish {
	case "tool_calls", "function_call":
		return "tool_use"
	case "length":
		return "max_tokens"
	case "stop":
		return "end_turn"
	default:
		return finish
	}
}
func messageUsage(u map[string]any) map[string]any {
	cached := number(object(u["prompt_tokens_details"])["cached_tokens"])
	write := number(object(u["prompt_tokens_details"])["cache_write_tokens"])
	input := number(u["prompt_tokens"]) - cached - write
	if input < 0 {
		input = 0
	}
	return map[string]any{"input_tokens": input, "output_tokens": number(u["completion_tokens"]), "cache_read_input_tokens": cached, "cache_creation_input_tokens": write}
}
func completionToMessage(payload []byte, model string) ([]byte, error) {
	j, err := decodeObject(payload)
	if err != nil {
		return nil, err
	}
	choices := list(j["choices"])
	if len(choices) != 1 {
		return nil, fail(502, "Messages output requires one completion")
	}
	choice := object(choices[0])
	msg := object(choice["message"])
	content := []any{}
	if thought := str(msg["reasoning_content"]); thought != "" {
		b := map[string]any{"type": "thinking", "thinking": thought}
		if sig := str(msg["reasoning_signature"]); sig != "" {
			b["signature"] = sig
		}
		content = append(content, b)
	}
	if b := object(msg["redacted_thinking"]); b != nil {
		content = append(content, b)
	}
	if text := str(msg["content"]); text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	for _, v := range list(msg["tool_calls"]) {
		t := object(v)
		f := object(t["function"])
		args, e := decodeObject([]byte(str(f["arguments"])))
		if e != nil {
			return nil, e
		}
		content = append(content, map[string]any{"type": "tool_use", "id": t["id"], "name": f["name"], "input": args})
	}
	return jsonBytes(map[string]any{"id": j["id"], "type": "message", "role": "assistant", "model": model, "content": content, "stop_reason": stopReason(str(choice["finish_reason"])), "stop_sequence": nil, "usage": messageUsage(object(j["usage"]))}), nil
}

// Used when a Messages client calls a model whose upstream is Chat Completions.
type chatMessageStream struct {
	model   string
	started bool
	blocks  map[string]int
	next    int
	usage   map[string]any
	finish  string
}

func newChatMessageStream(model string) *chatMessageStream {
	return &chatMessageStream{model: model, blocks: map[string]int{}, usage: map[string]any{}}
}
func (a *chatMessageStream) observe(j map[string]any) []map[string]any {
	out := []map[string]any{}
	if !a.started {
		a.started = true
		out = append(out, map[string]any{"type": "message_start", "message": map[string]any{"id": j["id"], "type": "message", "role": "assistant", "model": a.model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": messageUsage(nil)}})
	}
	mergeObjects(a.usage, object(j["usage"]), false)
	block := func(key string, b map[string]any) int {
		if i, ok := a.blocks[key]; ok {
			return i
		}
		i := a.next
		a.next++
		a.blocks[key] = i
		out = append(out, map[string]any{"type": "content_block_start", "index": i, "content_block": b})
		return i
	}
	delta := func(i int, d map[string]any) {
		out = append(out, map[string]any{"type": "content_block_delta", "index": i, "delta": d})
	}
	for _, v := range list(j["choices"]) {
		ch := object(v)
		d := object(ch["delta"])
		if text := str(d["reasoning_content"]); text != "" {
			i := block("thinking", map[string]any{"type": "thinking", "thinking": ""})
			delta(i, map[string]any{"type": "thinking_delta", "thinking": text})
		}
		if sig := str(d["reasoning_signature"]); sig != "" {
			i := block("thinking", map[string]any{"type": "thinking", "thinking": ""})
			delta(i, map[string]any{"type": "signature_delta", "signature": sig})
		}
		if b := object(d["redacted_thinking"]); b != nil {
			block("redacted", b)
		}
		if text := str(d["content"]); text != "" {
			i := block("text", map[string]any{"type": "text", "text": ""})
			delta(i, map[string]any{"type": "text_delta", "text": text})
		}
		for _, v := range list(d["tool_calls"]) {
			t := object(v)
			f := object(t["function"])
			key := "tool:" + string(jsonBytes(t["index"]))
			i := block(key, map[string]any{"type": "tool_use", "id": t["id"], "name": f["name"], "input": map[string]any{}})
			if args := str(f["arguments"]); args != "" {
				delta(i, map[string]any{"type": "input_json_delta", "partial_json": args})
			}
		}
		if finish := str(ch["finish_reason"]); finish != "" {
			a.finish = finish
		}
	}
	return out
}
func (a *chatMessageStream) end() []map[string]any {
	out := []map[string]any{}
	for i := 0; i < a.next; i++ {
		out = append(out, map[string]any{"type": "content_block_stop", "index": i})
	}
	out = append(out, map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason(a.finish), "stop_sequence": nil}, "usage": messageUsage(a.usage)}, map[string]any{"type": "message_stop"})
	return out
}
