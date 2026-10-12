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

func panelProcessFixture(t *testing.T, binaryDir, environment string) string {
	t.Helper()
	procRoot := t.TempDir()
	process := filepath.Join(procRoot, "1234")
	if err := os.MkdirAll(process, 0755); err != nil {
		t.Fatal(err)
	}
	for name, destination := range map[string]string{"exe": filepath.Join(binaryDir, "cpa-manager-plus"), "cwd": binaryDir} {
		if err := os.Symlink(destination, filepath.Join(process, name)); err != nil {
			t.Skip("process fixture needs symlink support: ", err)
		}
	}
	if err := os.WriteFile(filepath.Join(process, "environ"), []byte(environment), 0600); err != nil {
		t.Fatal(err)
	}
	return procRoot
}

func TestNativePanelEnvironmentOnlyProcess(t *testing.T) {
	for _, usageVariable := range []string{"USAGE_DATA_DIR=/fixture/data", "USAGE_DATA_DIR=/fixture/data\x00USAGE_DB_PATH=/fixture/usage.sqlite"} {
		t.Run(strings.Split(usageVariable, "=")[0], func(t *testing.T) {
			root := t.TempDir()
			binaryDir := filepath.Join(root, "runtime", "cpa-manager-plus_v1.13.1_linux_amd64")
			config := nativeFixture(t, binaryDir)
			if err := os.Remove(config); err != nil {
				t.Fatal(err)
			}
			proc := panelProcessFixture(t, binaryDir, usageVariable+"\x00HTTP_ADDR=127.0.0.1:1\x00CPA_MANAGER_ADMIN_KEY=fixture-private-key\x00")
			targets := runningPanelTargetsAt(Config{CPAMPInstallDir: root + string(os.PathSeparator)}, proc)
			if len(targets) != 1 {
				t.Fatal("environment-only manager not detected")
			}
			target := targets[0]
			if target.PID != 1234 || target.ConfigExists || target.ConfigurationMode != "environment" || target.ConfigPath != config || target.PanelPath != filepath.Join(root, "management.commandcodebridge.html") || !target.RestartRequired {
				t.Fatalf("incorrect environment-only target: %+v", target)
			}
			status := panelStatus(Config{}, targets, []byte("fixture panel"))
			if !status.Installable || strings.Contains(string(jsonBytes(status)), "fixture-private-key") {
				t.Fatal("target unusable or process secret exposed")
			}
			if _, err := os.Stat(config); !os.IsNotExist(err) {
				t.Fatal("inspection created a config")
			}
			if _, err := os.Stat(target.PanelPath); !os.IsNotExist(err) {
				t.Fatal("inspection created a panel")
			}
			backups, err := applyNativePanel(target, []byte("fixture panel"))
			if err != nil || len(backups) != 0 {
				t.Fatalf("first install failed: %v", err)
			}
			var saved map[string]string
			data, _ := os.ReadFile(config)
			if json.Unmarshal(data, &saved) != nil || saved["panelPath"] != target.PanelPath {
				t.Fatal("minimal panel config not created")
			}
			if len(saved) != 1 {
				t.Fatal("unrelated defaults or secrets written into config")
			}
		})
	}
}

func TestNativePanelDBOnlyProcessRequiresExplicitDefaults(t *testing.T) {
	for _, panelEnv := range []string{"", "PANEL_PATH=existing.html\x00"} {
		t.Run(panelEnv, func(t *testing.T) {
			root := t.TempDir()
			config := nativeFixture(t, root)
			if err := os.Remove(config); err != nil {
				t.Fatal(err)
			}
			proc := panelProcessFixture(t, root, "USAGE_DB_PATH=/fixture/usage.sqlite\x00HTTP_ADDR=127.0.0.1:1\x00"+panelEnv)
			targets := runningPanelTargetsAt(Config{}, proc)
			if len(targets) != 1 || targets[0].ConfigExists {
				t.Fatal("DB-only running process not retained")
			}
			status := panelStatus(Config{}, targets, []byte("fixture panel"))
			_, err := applyNativePanel(targets[0], []byte("fixture panel"))
			if panelEnv == "" {
				if status.Installable || err == nil || !strings.Contains(status.Message, "USAGE_DATA_DIR") {
					t.Fatal("DB-only defaults could be changed by creating config")
				}
				if _, err := os.Stat(targets[0].PanelPath); !os.IsNotExist(err) {
					t.Fatal("blocked installation created a panel")
				}
			} else if !status.Installable || err != nil {
				t.Fatalf("existing DB-only PANEL_PATH cannot be updated: %v", err)
			}
			if _, err := os.Stat(config); !os.IsNotExist(err) {
				t.Fatal("DB-only operation created config")
			}
		})
	}
}

func TestNativePanelEnvironmentOverrideWithoutConfig(t *testing.T) {
	root := t.TempDir()
	config := nativeFixture(t, root)
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	proc := panelProcessFixture(t, root, "USAGE_DATA_DIR=/fixture/data\x00PANEL_PATH=existing.html\x00HTTP_ADDR=127.0.0.1:1\x00")
	targets := runningPanelTargetsAt(Config{}, proc)
	if len(targets) != 1 || targets[0].RestartRequired || targets[0].PanelPath != filepath.Join(root, "existing.html") {
		t.Fatal("process PANEL_PATH not respected without config")
	}
	status := panelStatus(Config{}, targets, []byte("fixture panel"))
	if !status.Installable || status.RestartRequired || !strings.Contains(status.Message, "无需创建配置文件") {
		t.Fatal("environment page update incorrectly requires config or restart")
	}
	if _, err := applyNativePanel(targets[0], []byte("fixture panel")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config); !os.IsNotExist(err) {
		t.Fatal("environment panel installation unnecessarily created config")
	}
	status = panelStatus(Config{}, targets, []byte("fixture panel"))
	if status.RestartRequired || !strings.Contains(status.Message, "HTTP 核验未通过") {
		t.Fatal("unreachable listener incorrectly reported as requiring restart")
	}
	status = panelStatus(Config{CPAMPPanelPath: filepath.Join(root, "other.html")}, targets, []byte("fixture panel"))
	if status.Installable {
		t.Fatal("conflicting process PANEL_PATH was installable")
	}
}

func TestNativePanelMissingOrInvalidConfigIsNotIgnored(t *testing.T) {
	root := t.TempDir()
	config := nativeFixture(t, root)
	for _, value := range []string{"invalid JSON", "missing"} {
		t.Run(value, func(t *testing.T) {
			if value == "missing" {
				if err := os.Remove(config); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(config, []byte(value), 0644); err != nil {
				t.Fatal(err)
			}
			proc := panelProcessFixture(t, root, "USAGE_DATA_DIR=/fixture/data\x00CPA_MANAGER_CONFIG="+config+"\x00HTTP_ADDR=127.0.0.1:1\x00")
			targets := runningPanelTargetsAt(Config{}, proc)
			if len(targets) != 1 || targets[0].DiscoveryError == "" || panelStatus(Config{}, targets, []byte("fixture")).Installable {
				t.Fatal("bad running config hidden or accepted for installation")
			}
		})
	}
}

func TestNativePanelNewConfigConcurrentEditAndRollback(t *testing.T) {
	root := t.TempDir()
	config := nativeFixture(t, root)
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	target, err := readPanelTargetConfig(config, root, "fixture", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(`{"external":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := applyNativePanel(target, []byte("fixture")); err == nil {
		t.Fatal("new external config overwritten")
	}
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	_, err = applyNativePanelWithWriter(target, []byte("fixture"), func(path string, data []byte, mode os.FileMode, existed bool) error {
		if path == config {
			return fmt.Errorf("fixture disk failure")
		}
		return writePanelFile(path, data, mode, existed)
	})
	if err == nil {
		t.Fatal("new config write failure ignored")
	}
	for _, path := range []string{config, target.PanelPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("failed install left new files behind")
		}
	}
}

func TestNativePanelConfigPreparationRejectsSnapshotDrift(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(fmt.Sprint(exists), func(t *testing.T) {
			root := t.TempDir()
			config := nativeFixture(t, root)
			if !exists {
				if err := os.Remove(config); err != nil {
					t.Fatal(err)
				}
			}
			target, err := readPanelTargetConfig(config, root, "fixture", "", true)
			if err != nil {
				t.Fatal(err)
			}
			external := []byte(`{"external":true,"dataDir":"/keep"}`)
			if err := os.WriteFile(config, external, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := preparePanelConfigChange(target, []byte(`{"panelPath":"new.html"}`)); err == nil {
				t.Fatal("configuration changed during preparation was accepted")
			}
			actual, _ := os.ReadFile(config)
			if !bytes.Equal(actual, external) {
				t.Fatal("snapshot check changed external configuration")
			}
		})
	}
}

func TestNativePanelNewConfigAtomicCreationPreservesConcurrentFile(t *testing.T) {
	root := t.TempDir()
	config := nativeFixture(t, root)
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	target, err := readPanelTargetConfig(config, root, "fixture", "", true)
	if err != nil {
		t.Fatal(err)
	}
	external := []byte(`{"external":true,"dataDir":"/keep"}`)
	_, err = applyNativePanelWithWriter(target, []byte("fixture"), func(path string, data []byte, mode os.FileMode, existed bool) error {
		if path == config {
			if err := os.WriteFile(config, external, 0600); err != nil {
				return err
			}
		}
		return writePanelFile(path, data, mode, existed)
	})
	actual, _ := os.ReadFile(config)
	if err == nil || !bytes.Equal(actual, external) {
		t.Fatal("external config created just before commit was overwritten")
	}
	if _, err := os.Stat(target.PanelPath); !os.IsNotExist(err) {
		t.Fatal("panel not rolled back after config creation conflict")
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
	_, err := applyNativePanelWithWriter(target, []byte("fixture new panel"), func(path string, data []byte, mode os.FileMode, existed bool) error {
		if path == config {
			return fmt.Errorf("fixture config disk failure")
		}
		return writePanelFile(path, data, mode, existed)
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
	observation := observePanel(target, nil)
	if !observation.Checked || observation.NativeQuota || observation.Matches || len(observation.SHA256) != 64 {
		t.Fatal("served-page diagnosis requires no embedded asset and must report actual content")
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
