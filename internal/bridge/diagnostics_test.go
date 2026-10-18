package bridge

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRegistrationDiagnostics(t *testing.T) {
	s, host, c := setup(t, func(string, map[string]any) mockReply { return streamReply(chatStream("ok")) })
	now := time.Now().UTC().Add(time.Second)
	good := runtimeAuth{ID: routingID(c, c.ID+".json"), Name: c.ID + ".json", Provider: Provider, Source: "memory", UpdatedAt: now}
	for _, tc := range []struct {
		name, want string
		entries    []runtimeAuth
		hostErr    bool
	}{
		{"registered", "confirmed", []runtimeAuth{good}, false},
		{"live file registration", "confirmed", []runtimeAuth{{ID: good.ID, Name: good.Name, Provider: Provider, Source: "file", AuthIndex: "runtime-index", UpdatedAt: now}}, false},
		{"missing", "missing", nil, false},
		{"duplicate", "duplicate", []runtimeAuth{good, good}, false},
		{"old route", "routing_mismatch", []runtimeAuth{{ID: "old-route", Name: good.Name, Provider: Provider, Source: "memory"}}, false},
		{"disable pending", "disabled_mismatch", []runtimeAuth{{ID: good.ID, Name: good.Name, Provider: Provider, Source: "memory", Disabled: true}}, false},
		{"file fallback", "unknown", []runtimeAuth{{ID: good.ID, Name: good.Name, Provider: Provider, Source: "file"}}, false},
		{"edit pending", "pending", []runtimeAuth{{ID: good.ID, Name: good.Name, Provider: Provider, Source: "memory"}}, false},
		{"host error", "unknown", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.SetHost(func(method string, in, out any) error {
				if method == "host.auth.list" {
					if tc.hostErr {
						return errors.New("private-error-" + c.APIKey)
					}
					return assign(out, map[string]any{"files": tc.entries})
				}
				return host.call(method, in, out)
			})
			report := s.diagnostics(false)
			if len(report.Credentials) != 1 || report.Credentials[0].Status != tc.want {
				t.Fatalf("unexpected diagnosis: %s", jsonBytes(report))
			}
			if strings.Contains(string(jsonBytes(report)), c.APIKey) {
				t.Fatal("secret leaked in diagnostic")
			}
		})
	}
	if len(host.requests) != 0 {
		t.Fatal("diagnosis made upstream requests")
	}
}

func TestDiagnosticsRedactionAndEvidence(t *testing.T) {
	s, host, c := setup(t, func(string, map[string]any) mockReply { return streamReply(chatStream("ok")) })
	s.SetHost(func(method string, in, out any) error {
		if method == "host.auth.list" {
			return assign(out, map[string]any{"files": []runtimeAuth{{ID: c.ID + ".json", Name: c.ID + ".json", Provider: Provider, Source: "memory", UpdatedAt: time.Now().Add(time.Second)}}})
		}
		return host.call(method, in, out)
	})
	r := request(c)
	r.pluginTest = true
	if _, err := s.execute(r, false); err != nil {
		t.Fatal(err)
	}
	r.pluginTest = false
	if _, err := s.execute(r, false); err != nil {
		t.Fatal(err)
	}
	row := s.diagnostics(false).Credentials[0]
	if row.Evidence[0].Status != "unknown" || row.Evidence[1].Source != "plugin_test" || row.Evidence[1].Status != "success" || row.Evidence[2].Status != "success" {
		t.Fatalf("sources merged: %+v", row.Evidence)
	}
	encoded := string(jsonBytes(s.diagnostics(true)))
	for _, private := range []string{c.APIKey, c.Label, `"id":`, `"model":`, c.ID + ".json"} {
		if strings.Contains(encoded, private) {
			t.Fatalf("summary includes %q", private)
		}
	}
	// Old logs predate the source field. Do not turn them into routing evidence.
	s.mu.Lock()
	s.logs[0].Source = ""
	s.logs[1].Source = ""
	s.mu.Unlock()
	if row = s.diagnostics(false).Credentials[0]; row.Evidence[2].Status != "unknown" {
		t.Fatal("old log falsely proves CPA request")
	}
}

func TestCredentialSaveSeparatesHostConfirmation(t *testing.T) {
	s, _, _ := setup(t, func(string, map[string]any) mockReply { return mockReply{} })
	value, err := s.editCredential(ManagementRequest{Method: "POST", Body: jsonBytes(map[string]any{"api_key": "fixture-new-secret", "label": "saved account"})})
	if err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	if result["saved"] != true || result["registration"].(credentialDiagnostic).Status != "missing" {
		t.Fatal("save and registration were conflated")
	}
	if strings.Contains(string(jsonBytes(result)), "fixture-new-secret") {
		t.Fatal("saved key exposed")
	}
}

func TestIdenticalSaveDoesNotInvalidateHostConfirmation(t *testing.T) {
	s, host, c := setup(t, func(string, map[string]any) mockReply { return mockReply{} })
	c = s.creds[c.ID] // setup also saves the current model revision.
	confirmedAt := time.Now().UTC()
	expected := s.expectedRegistration[c.ID]
	s.SetHost(func(method string, in, out any) error {
		if method == "host.auth.list" {
			return assign(out, map[string]any{"files": []runtimeAuth{{ID: c.ID + ".json", Name: c.ID + ".json", Provider: Provider, Source: "memory", UpdatedAt: confirmedAt}}})
		}
		return host.call(method, in, out)
	})
	if err := s.saveCredential(c); err != nil {
		t.Fatal(err)
	}
	if !s.expectedRegistration[c.ID].Equal(expected) {
		t.Fatal("identical save changed expected registration timestamp")
	}
	if got := s.diagnostics(false).Credentials[0].Status; got != "confirmed" {
		t.Fatalf("identical save invalidated unchanged runtime registration: %s", got)
	}
}
