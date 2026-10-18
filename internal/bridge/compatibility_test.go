package bridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompatibilityPageAndBackendAreIndependent(t *testing.T) {
	for _, tc := range []struct{ name, page, health, wantPage, wantSnapshot string }{
		{"matching", `<html data-commandcode-native-quota data-commandcode-native-windows><meta name="commandcodebridge-capabilities" content="quota recent-requests quota-windows">CommandCodeBridge v` + Version + `</html>`, `{"capabilities":{"commandcode_quota_snapshots_v1":true}}`, "ok", "ok"},
		{"old page", `<html>CommandCodeBridge v0.2.1</html>`, `{"capabilities":{"commandcode_quota_snapshots_v1":true}}`, "mismatch", "ok"},
		{"old backend", `<html>CommandCodeBridge v0.2.1</html>`, `{"ok":true}`, "mismatch", "unknown"},
		{"explicit unsupported", `<html>old</html>`, `{"capabilities":{"commandcode_quota_snapshots_v1":false}}`, "mismatch", "missing"},
		{"malformed capability", `<html>old</html>`, `{"capabilities":{"commandcode_quota_snapshots_v1":"true"}}`, "mismatch", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Management-Key") != "" {
					t.Error("check mutated state or forwarded credentials")
				}
				if r.URL.Path == "/management.html" {
					_, _ = w.Write([]byte(tc.page))
					return
				}
				if r.URL.Path == "/health" {
					_, _ = w.Write([]byte(tc.health))
					return
				}
				t.Error("unexpected probe", r.URL.Path)
			}))
			defer server.Close()
			target := &nativePanelTarget{address: strings.TrimPrefix(server.URL, "http://")}
			expected := []byte(tc.page)
			if tc.wantPage != "ok" {
				expected = []byte("new release")
			}
			panel := nativePanelStatus{nativePanelTarget: target, ServedPage: observePanel(target, expected)}
			report := buildCompatibility(panel)
			if report.Checks[2].Status != tc.wantPage || report.Checks[6].Status != tc.wantSnapshot {
				t.Fatalf("capabilities conflated: %s", jsonBytes(report))
			}
			if tc.wantPage == "ok" && (report.Checks[4].Status != "ok" || panel.ServedPage.Version != Version) {
				t.Fatal("page version or recent request marker missing")
			}
		})
	}
}

func TestCompatibilityUnknownRemoteAndRedirect(t *testing.T) {
	if supported, checked := observeSnapshotCapability(&nativePanelTarget{address: "192.0.2.1:18317"}); supported || checked {
		t.Fatal("remote target probed")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://192.0.2.1/health", http.StatusFound)
	}))
	defer server.Close()
	if supported, checked := observeSnapshotCapability(&nativePanelTarget{address: strings.TrimPrefix(server.URL, "http://")}); supported || checked {
		t.Fatal("redirect treated as capability")
	}
	if buildCompatibility(nativePanelStatus{}).Checks[2].Status != "unknown" {
		t.Fatal("unknown target is not confirmed")
	}
	var baseline map[string]any
	if json.Unmarshal(compatibilityBaseline, &baseline) != nil || baseline["cpa_baseline"] != "8.0.23" {
		t.Fatal("invalid shared compatibility baseline")
	}
}
