package bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func nativeFixture(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cpa-manager-plus"), []byte("fixture binary"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"httpAddr":"127.0.0.1:1","adminKeyFile":"private-key","unknown":{"keep":true},"dataDir":"./data"}`), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNativePanelDiscovery(t *testing.T) {
	root := t.TempDir()
	first := nativeFixture(t, filepath.Join(root, "runtime", "cpa-manager-plus_v1_linux_amd64"))
	_ = nativeFixture(t, filepath.Join(root, "runtime", "cpa-manager-plus_v2_linux_amd64"))
	targets := discoverPanelTargets(Config{}, []string{root})
	if len(targets) != 2 || panelStatus(Config{}, targets, nil).nativePanelTarget != nil {
		t.Fatal("multiple versions were silently selected")
	}
	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("# CPAMP_RUNTIME_PACKAGE=cpa-manager-plus_v1_linux_amd64\n"), 0755); err != nil {
		t.Fatal(err)
	}
	targets = discoverPanelTargets(Config{}, []string{root})
	if len(targets) != 1 || targets[0].ConfigPath != first || targets[0].PanelPath != filepath.Join(root, "management.commandcodebridge.html") {
		t.Fatal("active runtime marker was not respected")
	}
	override := t.TempDir()
	custom := nativeFixture(t, override)
	targets = discoverPanelTargets(Config{CPAMPInstallDir: override}, []string{root})
	if len(targets) != 1 || targets[0].ConfigPath != custom {
		t.Fatal("install directory override ignored")
	}
	separate := filepath.Join(t.TempDir(), "custom-config.json")
	if err := os.WriteFile(separate, []byte(`{"httpAddr":"127.0.0.1:1"}`), 0644); err != nil {
		t.Fatal(err)
	}
	targets = discoverPanelTargets(Config{CPAMPInstallDir: override, CPAMPConfigPath: separate}, nil)
	if len(targets) != 1 || targets[0].ConfigPath != separate {
		t.Fatal("external config override ignored")
	}
}

func TestNativePanelInstallAndPreservation(t *testing.T) {
	root := t.TempDir()
	config := nativeFixture(t, root)
	before, _ := os.ReadFile(config)
	target, err := readPanelTarget(config, root, "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	page := []byte("<html>fixture native integration</html>")
	backups, err := applyNativePanel(target, page)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatal("original config not backed up")
	}
	saved, _ := os.ReadFile(backups[0])
	if !bytes.Equal(saved, before) {
		t.Fatal("backup changed original bytes")
	}
	info, _ := os.Stat(backups[0])
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("config backup is not private")
	}
	var after map[string]json.RawMessage
	data, _ := os.ReadFile(config)
	if json.Unmarshal(data, &after) != nil {
		t.Fatal("config damaged")
	}
	var originalFields, savedFields map[string]any
	_ = json.Unmarshal(before, &originalFields)
	_ = json.Unmarshal(data, &savedFields)
	delete(savedFields, "panelPath")
	if !reflect.DeepEqual(originalFields, savedFields) {
		t.Fatal("unrelated config changed")
	}
	var path string
	_ = json.Unmarshal(after["panelPath"], &path)
	if path != target.PanelPath {
		t.Fatal("panelPath not configured")
	}
	content, _ := os.ReadFile(path)
	if !bytes.Equal(content, page) {
		t.Fatal("page not installed")
	}
	target, err = readPanelTarget(config, root, "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	backups, err = applyNativePanel(target, page)
	if err != nil || len(backups) != 0 {
		t.Fatal("unchanged install created backups or failed")
	}
	if err = os.WriteFile(config, []byte(`{"updatedBySomeoneElse":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = applyNativePanel(target, []byte("changed")); err == nil {
		t.Fatal("concurrent config edit overwritten")
	}
}

func TestNativePanelPathAndWriteFailures(t *testing.T) {
	root := t.TempDir()
	config := nativeFixture(t, root)
	if _, err := readPanelTarget(config, root, "fixture", config); err == nil {
		t.Fatal("config accepted as HTML destination")
	}
	target, _ := readPanelTarget(config, root, "fixture", filepath.Join(root, "missing", "panel.html"))
	before, _ := os.ReadFile(config)
	if _, err := applyNativePanel(target, []byte("fixture")); err == nil {
		t.Fatal("missing destination directory accepted")
	}
	after, _ := os.ReadFile(config)
	if !bytes.Equal(before, after) {
		t.Fatal("failed install changed config")
	}
	link := filepath.Join(root, "linked.html")
	if err := os.Symlink(config, link); err == nil {
		target, _ = readPanelTarget(config, root, "fixture", link)
		if _, err = applyNativePanel(target, []byte("fixture")); err == nil {
			t.Fatal("symlink overwritten")
		}
	}
	if err := os.WriteFile(config, []byte("invalid JSON"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPanelTarget(config, root, "fixture", ""); err == nil {
		t.Fatal("invalid config accepted")
	}
}

func TestNativePanelRollback(t *testing.T) {
	root := t.TempDir()
	config := nativeFixture(t, root)
	original, _ := os.ReadFile(config)
	target, _ := readPanelTarget(config, root, "fixture", "")
	_, err := applyNativePanelWithWriter(target, []byte("fixture new panel"), func(path string, data []byte, mode os.FileMode) error {
		if path == config {
			return fmt.Errorf("fixture config disk failure")
		}
		return writePanelFile(path, data, mode)
	})
	if err == nil {
		t.Fatal("config disk failure ignored")
	}
	if _, err = os.Stat(target.PanelPath); !os.IsNotExist(err) {
		t.Fatal("new panel was not rolled back")
	}
	after, _ := os.ReadFile(config)
	if !bytes.Equal(original, after) {
		t.Fatal("config changed on failed install")
	}
}

func TestNativePanelActivation(t *testing.T) {
	page := []byte("fixture panel")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/management.html" {
			t.Error("wrong check URL")
		}
		_, _ = w.Write(page)
	}))
	defer server.Close()
	target := &nativePanelTarget{address: strings.TrimPrefix(server.URL, "http://")}
	if !panelIsActive(target, page) || panelIsActive(target, []byte("other")) {
		t.Fatal("activation did not compare actual HTTP page")
	}
	target.address = "remote.example:18317"
	if panelIsActive(target, page) {
		t.Fatal("activation tried a nonlocal target")
	}
}

func TestNativePanelSettingsAndEmbeddedPage(t *testing.T) {
	service := NewService()
	service.cfg.DataDir = t.TempDir()
	raw := jsonBytes(ManagementRequest{Method: "PUT", Path: apiBase + "/config", Body: jsonBytes(map[string]string{"cpamp_install_dir": "~/cpa-manager-plus", "cpamp_config_path": "/custom/config.json", "cpamp_panel_path": "/custom/panel.html"})})
	response, err := service.management(raw)
	if err != nil || response.(ManagementResponse).StatusCode != 200 {
		t.Fatal("path settings rejected")
	}
	stored, _ := os.ReadFile(filepath.Join(service.cfg.DataDir, "settings.json"))
	var config Config
	_ = json.Unmarshal(stored, &config)
	if config.CPAMPInstallDir != "~/cpa-manager-plus" || config.CPAMPConfigPath != "/custom/config.json" || config.CPAMPPanelPath != "/custom/panel.html" {
		t.Fatal("path settings not persisted")
	}
	raw = jsonBytes(map[string]any{"config_yaml": []byte("data_dir: " + filepath.ToSlash(service.cfg.DataDir) + "\ncpamp_install_dir: /host-config-manager\n")})
	if err := service.configure(raw); err != nil || service.config().CPAMPInstallDir != "/host-config-manager" {
		t.Fatal("host plugin configuration path did not override old stored settings")
	}
	if _, err := nativePanelAssets.ReadFile("panel-assets/management.html.gz"); err == nil {
		page, err := nativePanelContent()
		if err != nil || len(page) < 1024 {
			t.Fatal("bundled production page invalid")
		}
	} else if _, err := nativePanelContent(); err == nil {
		t.Fatal("development build pretended to contain a panel")
	}
}
