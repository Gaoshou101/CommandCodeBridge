package bridge

import (
	"encoding/json"
	"errors"
	"testing"
)

// Token counting must stay an availability-neutral endpoint gap: CPA hosts
// before CLIProxyAPI 1c53721 cool a credential down after a count_tokens 501.
func TestCountTokensReportsAvailabilityNeutralGap(t *testing.T) {
	s := NewService()
	called := false
	s.SetHost(func(string, any, any) error { called = true; return nil })
	_, err := s.Handle("executor.count_tokens", json.RawMessage(`{"Model":"mock-claude","Payload":"e30="}`))
	var api *APIError
	if !errors.As(err, &api) {
		t.Fatalf("count_tokens error = %#v, want *APIError", err)
	}
	if api.Status != 404 || api.Code() != "count_tokens_unsupported" || !api.Local {
		t.Fatalf("count_tokens error = %+v, want local 404 count_tokens_unsupported", api)
	}
	if api.Status == 501 || api.Status == 429 || api.Status == 401 || api.Status == 403 {
		t.Fatalf("count_tokens status %d would change credential availability", api.Status)
	}
	if called {
		t.Fatal("count_tokens must not call the host or upstream")
	}
}
