package bridge

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type upstreamStream struct {
	StatusCode int         `json:"status_code"`
	Headers    http.Header `json:"headers"`
	StreamID   string      `json:"stream_id"`
	deadline   time.Time
}
type readChunk struct {
	Payload []byte `json:"payload"`
	Error   string `json:"error"`
	Done    bool   `json:"done"`
}

func (s *Service) openUpstream(payload any, deadline time.Time) (upstreamStream, error) {
	if deadline.IsZero() {
		deadline = time.Now().Add(time.Duration(s.config().TimeoutSeconds) * time.Second)
	}
	if e := s.begin(); e != nil {
		return upstreamStream{}, e
	}
	type result struct {
		up upstreamStream
		e  error
	}
	ch := make(chan result)
	abandoned := make(chan struct{})
	defer close(abandoned)
	go func() {
		defer s.active.Done()
		var up upstreamStream
		e := s.call("host.http.do_stream", payload, &up)
		select {
		case ch <- result{up, e}:
		case <-abandoned:
			if up.StreamID != "" {
				_ = s.call("host.http.stream_close", map[string]string{"stream_id": up.StreamID}, nil)
			}
		}
	}()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case r := <-ch:
		if r.e != nil {
			if r.up.StreamID != "" {
				_ = s.call("host.http.stream_close", map[string]string{"stream_id": r.up.StreamID}, nil)
			}
			return upstreamStream{}, fail(502, "upstream connection failed")
		}
		if r.up.StreamID == "" {
			return upstreamStream{}, fail(502, "host returned no HTTP stream")
		}
		r.up.deadline = deadline
		s.mu.Lock()
		stopped := s.stopped
		if !stopped {
			s.streams[r.up.StreamID] = true
		}
		s.mu.Unlock()
		if stopped {
			s.closeUpstream(r.up.StreamID)
			return upstreamStream{}, fail(503, "plugin is shutting down")
		}
		return r.up, nil
	case <-timer.C:
		return upstreamStream{}, fail(504, "upstream request timed out")
	case <-s.stopCh:
		return upstreamStream{}, fail(503, "plugin is shutting down")
	}
}
func (s *Service) closeUpstream(stream string) {
	if stream == "" {
		return
	}
	s.mu.Lock()
	delete(s.streams, stream)
	s.mu.Unlock()
	_ = s.call("host.http.stream_close", map[string]string{"stream_id": stream}, nil)
}
func (s *Service) read(up upstreamStream, fn func([]byte) error) error {
	defer s.closeUpstream(up.StreamID)
	deadline := up.deadline
	if deadline.IsZero() {
		deadline = time.Now().Add(time.Duration(s.config().TimeoutSeconds) * time.Second)
	}
	var mu sync.Mutex
	timedOut := false
	done := make(chan struct{})
	timer := time.AfterFunc(time.Until(deadline), func() { defer close(done); mu.Lock(); timedOut = true; mu.Unlock(); s.closeUpstream(up.StreamID) })
	defer func() {
		if !timer.Stop() {
			<-done
		}
	}()
	total := 0
	for {
		var chunk readChunk
		e := s.call("host.http.stream_read", map[string]string{"stream_id": up.StreamID}, &chunk)
		mu.Lock()
		timeout := timedOut
		mu.Unlock()
		if timeout {
			return fail(504, "upstream request timed out")
		}
		if e != nil || chunk.Error != "" {
			return fail(502, "upstream stream interrupted")
		}
		total += len(chunk.Payload)
		if total > s.config().MaxResponseBytes {
			return fail(502, "upstream response exceeds configured limit")
		}
		if len(chunk.Payload) > 0 {
			if e := fn(chunk.Payload); e != nil {
				return e
			}
		}
		if chunk.Done {
			return nil
		}
	}
}
func upstreamError(status int, h http.Header, j map[string]any) error {
	if status < 400 {
		status = 502
	}
	kind := str(object(j["error"])["code"])
	if kind == "" {
		kind = str(j["code"])
	}
	if kind == "" {
		switch status {
		case 401:
			kind = "authentication_error"
		case 403:
			kind = "permission_error"
		case 429:
			kind = "rate_limit_error"
		default:
			kind = "upstream_error"
		}
	}
	e := &APIError{Status: status, Kind: kind, Message: errorMessage(j)}
	if status == 429 {
		e.RetryAt = time.Now().Add(60 * time.Second)
		v := h.Get("Retry-After")
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 86400 {
			e.RetryAt = time.Now().Add(time.Duration(n) * time.Second)
		} else if at, err := http.ParseTime(v); err == nil && at.After(time.Now()) {
			e.RetryAt = at
		}
		rate := object(j["rateLimit"])
		if rate == nil {
			rate = object(object(j["error"])["rateLimit"])
		}
		if reset := number(rate["reset"]); reset > 0 {
			if reset > 1e12 {
				reset /= 1000
			}
			at := time.Unix(reset, 0)
			if at.After(time.Now()) && at.Before(time.Now().Add(8*24*time.Hour)) {
				e.RetryAt = at
			}
		}
	}
	return e
}
func (s *Service) readJSON(up upstreamStream) ([]byte, error) {
	var b bytes.Buffer
	if e := s.read(up, func(v []byte) error { b.Write(v); return nil }); e != nil {
		return nil, e
	}
	if up.StatusCode < 200 || up.StatusCode >= 300 {
		j, _ := decodeObject(b.Bytes())
		return nil, upstreamError(up.StatusCode, up.Headers, j)
	}
	if _, e := decodeObject(b.Bytes()); e != nil {
		return nil, fail(502, "upstream returned invalid JSON")
	}
	return b.Bytes(), nil
}
func (s *Service) getJSON(path string, c Credential, callback string, deadline time.Time) (map[string]any, error) {
	if c.ProxyURL != "" {
		return nil, fail(400, "this CPA HTTP bridge supports the global proxy only; remove the credential proxy_url and configure the CPA global proxy")
	}
	up, e := s.openUpstream(map[string]any{"host_callback_id": callback, "method": "GET", "url": s.config().BaseURL + path, "headers": upstreamHeaders(c, false)}, deadline)
	if e != nil {
		return nil, e
	}
	b, e := s.readJSON(up)
	if e != nil {
		return nil, redactError(e, c.APIKey)
	}
	return decodeObject(b)
}

func (s *Service) rateKey(c Credential, model string) string {
	s.mu.RLock()
	a := s.accounts[c.ID]
	s.mu.RUnlock()
	if a == "" {
		h := shaKey(c.APIKey)
		a = h
	}
	return a + "|" + model
}
func (s *Service) acquire(c Credential, model string) (func(error), error) {
	key := s.rateKey(c, model)
	s.mu.Lock()
	until, limited := s.cooldown[key]
	if limited && (time.Now().Before(until) || s.probes[key]) {
		s.mu.Unlock()
		return nil, &APIError{Status: 429, Kind: "local_backoff", Message: "upstream cooldown is active; no upstream request sent", RetryAt: until, Local: true}
	}
	if limited {
		s.probes[key] = true
	}
	s.mu.Unlock()
	return func(err error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.probes, key)
		if e, ok := err.(*APIError); ok && e.Status == 429 && !e.Local {
			if e.RetryAt.After(s.cooldown[key]) {
				s.cooldown[key] = e.RetryAt
			}
		} else if err == nil {
			delete(s.cooldown, key)
		}
	}, nil
}
func (s *Service) prepare(r ExecutorRequest) (map[string]any, Credential, Model, error) {
	c, e := s.credential(r)
	if e != nil {
		return nil, c, Model{}, e
	}
	if c.ProxyURL != "" {
		return nil, c, Model{}, fail(400, "this CPA HTTP bridge supports the global proxy only; remove the credential proxy_url and configure the CPA global proxy")
	}
	m, e := s.model(r.Model)
	if e != nil {
		return nil, c, m, e
	}
	body, e := decodeObject(r.Payload)
	if e != nil {
		return nil, c, m, e
	}
	if len(list(body["messages"])) == 0 {
		return nil, c, m, fail(400, "messages must be a nonempty array")
	}
	body["model"] = m.UpstreamID
	return body, c, m, nil
}
func (s *Service) execute(r ExecutorRequest, stream bool) (any, error) {
	if e := s.begin(); e != nil {
		return nil, e
	}
	r.Stream = stream
	start := time.Now()
	body, c, m, e := s.prepare(r)
	entry := s.newLog(r, c, m, start)
	finish := func(err error) {
		err = redactError(err, c.APIKey)
		entry.Status = statusOf(err)
		entry.Error = safeError(err)
		entry.DurationMS = time.Since(start).Milliseconds()
		if api, ok := err.(*APIError); ok {
			entry.ErrorKind = api.Kind
			entry.UpstreamSkipped = api.Local
			if !api.RetryAt.IsZero() {
				t := api.RetryAt
				entry.RetryAt = &t
			}
		}
		s.record(entry)
	}
	early := func(err error) (any, error) { finish(err); s.active.Done(); return nil, redactError(err, c.APIKey) }
	if e != nil {
		return early(e)
	}
	entry.ReasoningFrom, entry.ReasoningTo = applyReasoning(body, m)
	release, e := s.acquire(c, m.UpstreamID)
	if e != nil {
		return early(e)
	}
	path := "/provider/v1/chat/completions"
	anthropic := !contains(m.Endpoints, "/chat/completions") && contains(m.Endpoints, "/messages")
	nativeMessages := r.Format == "claude"
	if nativeMessages && number(body["n"]) > 1 {
		e = fail(400, "Messages output supports one completion")
		release(e)
		return early(e)
	}
	if anthropic {
		path = "/provider/v1/messages"
		// Original native messages retain signed thinking and tool/image blocks
		// which CPA may discard while preparing the Chat input format.
		if nativeMessages && str(r.Metadata["request_path"]) == "/v1/messages" && len(r.OriginalRequest) > 0 {
			original, err := decodeObject(r.OriginalRequest)
			if err == nil && len(list(original["messages"])) > 0 {
				original["model"] = m.UpstreamID
				if m.Reasoning.Enabled && entry.ReasoningFrom != entry.ReasoningTo {
					if body["reasoning_effort"] != nil {
						original["reasoning_effort"] = body["reasoning_effort"]
					} else {
						delete(original, "reasoning_effort")
						delete(original, "thinking")
						if output := object(original["output_config"]); output != nil {
							delete(output, "effort")
						}
					}
				}
				body = original
			} else {
				e = fail(400, "invalid native Messages request")
			}
		} else {
			body, e = toAnthropic(body)
		}
		if e == nil {
			e = applyAnthropicReasoning(body, m)
		}
		if e != nil {
			release(e)
			return early(e)
		}
	} else {
		opts := object(body["stream_options"])
		if opts == nil {
			opts = map[string]any{}
		}
		opts["include_usage"] = true
		body["stream_options"] = opts
	}
	body["stream"] = true
	up, e := s.openUpstream(map[string]any{"host_callback_id": r.HostCallbackID, "method": "POST", "url": s.config().BaseURL + path, "headers": upstreamHeaders(c, s.config().ZDR), "body": jsonBytes(body)}, start.Add(time.Duration(s.config().TimeoutSeconds)*time.Second))
	if e != nil {
		release(e)
		return early(e)
	}
	if up.StatusCode < 200 || up.StatusCode >= 300 {
		_, e = s.readJSON(up)
		e = redactError(e, c.APIKey)
		release(e)
		return early(e)
	}
	if !strings.Contains(strings.ToLower(up.Headers.Get("Content-Type")), "text/event-stream") {
		s.closeUpstream(up.StreamID)
		e = fail(502, "upstream returned non-SSE content for a streaming request")
		release(e)
		return early(e)
	}
	if !stream {
		cp, err := s.consume(up, r.Model, anthropic, nativeMessages, body, &entry, start, nil)
		var payload []byte
		if err == nil {
			if nativeMessages && cp.messages != nil {
				payload, err = cp.messages.result(r.Model)
			} else {
				payload, err = cp.result(r.Model)
				if err == nil && nativeMessages {
					payload, err = completionToMessage(payload, r.Model)
				}
			}
		}
		err = redactError(err, c.APIKey)
		release(err)
		finish(err)
		s.active.Done()
		if err != nil {
			return nil, err
		}
		return Response{Payload: payload, Headers: http.Header{"Content-Type": {"application/json"}}}, nil
	}
	if r.StreamID == "" {
		s.closeUpstream(up.StreamID)
		e = fail(500, "host provided no output stream ID")
		release(e)
		return early(e)
	}
	ready := make(chan error, 1)
	go func() {
		defer s.active.Done()
		started := false
		var err error
		pending := [][]byte{}
		pendingSize := 0
		defer func() {
			if recover() != nil {
				err = fail(500, "stream processing failed")
			}
			err = redactError(err, c.APIKey)
			release(err)
			finish(err)
			if !started {
				ready <- err
			} else {
				_ = s.call("host.stream.close", map[string]any{"stream_id": r.StreamID, "error": safeError(err)}, nil)
			}
		}()
		emit := func(b []byte) error {
			j, _ := decodeObject(b)
			outputStarted := contentStarted(j)
			if nativeMessages {
				outputStarted = messageOutputStarted(j)
			}
			if !started && !outputStarted {
				pendingSize += len(b)
				if pendingSize > 65536 {
					return fail(502, "stream prelude is too large")
				}
				pending = append(pending, b)
				return nil
			}
			send := func(payload []byte) error {
				if nativeMessages {
					j, _ := decodeObject(payload)
					payload = messageFrame(j)
				}
				if e := s.call("host.stream.emit", map[string]any{"stream_id": r.StreamID, "payload": payload}, nil); e != nil {
					return fail(499, "client disconnected")
				}
				return nil
			}
			if !started {
				started = true
				ready <- nil
				for _, p := range pending {
					if e := send(p); e != nil {
						return e
					}
				}
				pending = nil
			}
			return send(b)
		}
		_, err = s.consume(up, r.Model, anthropic, nativeMessages, body, &entry, start, emit)
		if err == nil && !started {
			err = fail(502, "upstream completed without output")
		}
	}()
	if e := <-ready; e != nil {
		return nil, e
	}
	return map[string]any{"headers": http.Header{"Content-Type": {"text/event-stream"}}}, nil
}
func (s *Service) consume(up upstreamStream, model string, anthropic, nativeMessages bool, request map[string]any, entry *LogEntry, start time.Time, emit func([]byte) error) (*completion, error) {
	cp := newCompletion()
	if nativeMessages && anthropic {
		cp.messages = newMessageCompletion()
	}
	chatMessages := newChatMessageStream(model)
	if !anthropic && number(request["n"]) > 0 {
		cp.expectedChoices = number(request["n"])
	}
	decoder := SSEDecoder{max: s.config().MaxResponseBytes}
	adapter := newAnthropicStream(model)
	observe := func(j map[string]any) error {
		if j["error"] != nil {
			return upstreamError(502, up.Headers, j)
		}
		observeMetadata(j, entry, nil)
		cp.observe(j)
		if entry.TTFTMS == nil && contentStarted(j) {
			t := time.Since(start).Milliseconds()
			entry.TTFTMS = &t
		}
		j["model"] = model
		if emit != nil && !(nativeMessages && anthropic) {
			if nativeMessages {
				for _, event := range chatMessages.observe(j) {
					if err := emit(jsonBytes(event)); err != nil {
						return err
					}
				}
				return nil
			}
			return emit(jsonBytes(j))
		}
		return nil
	}
	e := s.read(up, func(b []byte) error {
		return decoder.Feed(b, func(payload []byte, event string) error {
			if strings.TrimSpace(string(payload)) == "[DONE]" {
				cp.done = true
				if !cp.allFinished() {
					return fail(502, "stream ended without finish_reason")
				}
				entry.StreamEnd = "done"
				return errStreamDone
			}
			j, e := decodeObject(payload)
			if e != nil {
				return fail(502, "invalid stream JSON")
			}
			if event == "error" || j["error"] != nil {
				return upstreamError(502, up.Headers, j)
			}
			if anthropic {
				if cp.messages != nil {
					if err := cp.messages.observe(j); err != nil {
						return err
					}
				}
				chunks, done, e := adapter.observe(j)
				if e != nil {
					return e
				}
				for _, ch := range chunks {
					if e := observe(ch); e != nil {
						return e
					}
				}
				if nativeMessages && emit != nil {
					if msg := object(j["message"]); msg != nil {
						msg["model"] = model
					}
					if err := emit(jsonBytes(j)); err != nil {
						return err
					}
				}
				if done {
					cp.done = true
					entry.StreamEnd = "message_stop"
					return errStreamDone
				}
				return nil
			}
			return observe(j)
		})
	})
	if errors.Is(e, errStreamDone) {
		e = nil
	}
	if e == nil && !cp.done {
		e = decoder.End()
		if e == nil && !anthropic && cp.completeAtEOF() {
			cp.done = true
			entry.StreamEnd = "eof_after_finish"
		} else if e == nil {
			e = fail(502, "upstream stream ended before completion")
		}
	}
	if e == nil && !cp.completeAtEOF() {
		e = fail(502, "upstream returned incomplete choices or tool arguments")
	}
	if e == nil && nativeMessages && !anthropic && emit != nil {
		for _, event := range chatMessages.end() {
			if err := emit(jsonBytes(event)); err != nil {
				return cp, err
			}
		}
	}
	return cp, e
}
