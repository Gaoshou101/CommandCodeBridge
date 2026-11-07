package bridge

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// stallHost blocks one stream read until the plugin closes the stream, like a
// stalled upstream connection that only ends when the bridge gives up.
type stallHost struct {
	*mockHost
	mu      sync.Mutex
	reads   int
	stallAt int
	closed  chan struct{}
	once    sync.Once
}

func (h *stallHost) call(method string, in, out any) error {
	switch method {
	case "host.http.stream_read":
		h.mu.Lock()
		read := h.reads
		h.reads++
		h.mu.Unlock()
		if read == h.stallAt {
			select {
			case <-h.closed:
				return fmt.Errorf("closed stream")
			case <-time.After(10 * time.Second):
				return fmt.Errorf("test stall was never released")
			}
		}
	case "host.http.stream_close":
		h.once.Do(func() { close(h.closed) })
	}
	return h.mockHost.call(method, in, out)
}

func stalled(t *testing.T, stallAt int, edit func(*Config)) (*Service, Credential) {
	t.Helper()
	s, h, c := setup(t, func(string, map[string]any) mockReply {
		return streamReply(chatStream("hello world, streamed in chunks"))
	})
	host := &stallHost{mockHost: h, stallAt: stallAt, closed: make(chan struct{})}
	s.SetHost(host.call)
	// Second-level guards bypass the 5s validation floor to keep tests fast.
	s.mu.Lock()
	edit(&s.cfg)
	s.mu.Unlock()
	return s, c
}

func expectTimeout(t *testing.T, e error, kind string, elapsed time.Duration) {
	t.Helper()
	var api *APIError
	if !errors.As(e, &api) || api.Status != 504 || api.Kind != kind {
		t.Fatalf("expected 504 %s, got %v", kind, e)
	}
	if elapsed < 900*time.Millisecond || elapsed > 5*time.Second {
		t.Fatalf("%s fired after %v", kind, elapsed)
	}
}

func TestFirstByteTimeoutWhenUpstreamSendsNothing(t *testing.T) {
	s, c := stalled(t, 0, func(cfg *Config) { cfg.FirstByteTimeoutSeconds = 1 })
	start := time.Now()
	_, e := s.execute(request(c), false)
	expectTimeout(t, e, "upstream_first_byte_timeout", time.Since(start))
}

func TestFirstByteTimeoutWhileWaitingForHeaders(t *testing.T) {
	s, h, c := setup(t, func(string, map[string]any) mockReply { return streamReply(chatStream("ok")) })
	h.block = make(chan struct{})
	defer close(h.block)
	s.mu.Lock()
	s.cfg.FirstByteTimeoutSeconds = 1
	s.mu.Unlock()
	start := time.Now()
	_, e := s.execute(request(c), false)
	expectTimeout(t, e, "upstream_first_byte_timeout", time.Since(start))
}

func TestStreamIdleTimeoutAfterData(t *testing.T) {
	s, c := stalled(t, 2, func(cfg *Config) {
		cfg.FirstByteTimeoutSeconds = 30
		cfg.IdleTimeoutSeconds = 1
	})
	start := time.Now()
	_, e := s.execute(request(c), false)
	expectTimeout(t, e, "upstream_stream_idle_timeout", time.Since(start))
	s.mu.RLock()
	last := s.logs[len(s.logs)-1]
	s.mu.RUnlock()
	if last.ErrorKind != "upstream_stream_idle_timeout" || last.Status != 504 {
		t.Fatalf("request log did not record the idle timeout: %+v", last)
	}
}

func TestProgressGuardsAllowSteadyStreams(t *testing.T) {
	s, c := stalled(t, -1, func(cfg *Config) {
		cfg.FirstByteTimeoutSeconds = 1
		cfg.IdleTimeoutSeconds = 1
	})
	if _, e := s.execute(request(c), false); e != nil {
		t.Fatalf("steady stream failed: %v", e)
	}
}

func TestProgressGuardSettings(t *testing.T) {
	s, _, _ := setup(t, func(string, map[string]any) mockReply { return streamReply(chatStream("ok")) })
	if got := s.config(); got.FirstByteTimeoutSeconds != 0 || got.IdleTimeoutSeconds != 0 {
		t.Fatalf("guards should be disabled by default: %+v", got)
	}
	for _, value := range []int{-1, 1, 4, 1801} {
		cfg := s.config()
		cfg.FirstByteTimeoutSeconds = value
		if s.saveConfig(cfg) == nil {
			t.Fatalf("first_byte_timeout_seconds=%d was accepted", value)
		}
	}
	response, err := s.management(jsonBytes(ManagementRequest{Method: "PUT", Path: apiBase + "/config", Body: []byte(`{"first_byte_timeout_seconds":90,"stream_idle_timeout_seconds":120}`)}))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.config(); got.FirstByteTimeoutSeconds != 90 || got.IdleTimeoutSeconds != 120 {
		t.Fatalf("settings were not saved: %+v (%v)", got, response)
	}
}
