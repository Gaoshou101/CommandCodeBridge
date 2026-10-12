package bridge

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed panel-assets/*
var nativePanelAssets embed.FS

type nativePanelTarget struct {
	InstallDir        string `json:"install_dir"`
	ConfigPath        string `json:"config_path"`
	PanelPath         string `json:"panel_path"`
	Source            string `json:"source"`
	PID               int    `json:"pid,omitempty"`
	ConfigExists      bool   `json:"config_exists"`
	ConfigurationMode string `json:"configuration_mode"`
	DiscoveryError    string `json:"discovery_error,omitempty"`
	RestartRequired   bool   `json:"restart_required"`
	config            []byte
	fields            map[string]json.RawMessage
	panelEnvironment  string
	address           string
}

type nativePanelStatus struct {
	*nativePanelTarget
	DefaultInstallDir string           `json:"default_install_dir"`
	Available         bool             `json:"available"`
	Installed         bool             `json:"installed"`
	Active            bool             `json:"active"`
	Message           string           `json:"message"`
	Candidates        []string         `json:"candidates,omitempty"`
	Backups           []string         `json:"backups,omitempty"`
	Installable       bool             `json:"installable"`
	ServedPage        panelObservation `json:"served_page"`
}

type panelObservation struct {
	Checked     bool   `json:"checked"`
	HTTPStatus  int    `json:"http_status,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	NativeQuota bool   `json:"native_quota"`
	Matches     bool   `json:"matches"`
}

func expandedPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimLeft(path[1:], "/\\"))
	}
	return filepath.Abs(path)
}

func nativePanelContent() ([]byte, error) {
	packed, err := nativePanelAssets.ReadFile("panel-assets/management.html.gz")
	if err != nil {
		return nil, fail(503, "当前构建未包含原生面板，请安装官方发布的插件包")
	}
	reader, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, fail(500, "内置原生面板损坏")
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, 16<<20+1))
	if err != nil || len(content) > 16<<20 || !bytes.Contains(content, []byte("data-commandcode-native-quota")) || !bytes.Contains(content, []byte("CommandCodeBridge v"+Version)) {
		return nil, fail(500, "内置原生面板版本或内容不匹配")
	}
	return content, nil
}

func managerBinaryExists(dir string) bool {
	for _, name := range []string{"cpa-manager-plus", "cpa-manager-plus.exe"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func readPanelTarget(configPath, root, source, panelOverride string) (*nativePanelTarget, error) {
	return readPanelTargetConfig(configPath, root, source, panelOverride, false)
}

func readPanelTargetConfig(configPath, root, source, panelOverride string, allowMissing bool) (*nativePanelTarget, error) {
	path, err := expandedPath(configPath)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	configExists := err == nil
	// CPAMP deliberately skips creating config.json when USAGE_DATA_DIR or
	// USAGE_DB_PATH is set. A confirmed environment-based process is still a
	// valid target; inspection must not create its missing configuration.
	if allowMissing && os.IsNotExist(err) {
		content, err = nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("无法读取 CPAMP config.json")
	}
	fields := map[string]json.RawMessage{}
	if configExists && (len(content) > 1<<20 || json.Unmarshal(content, &fields) != nil || fields == nil) {
		return nil, fmt.Errorf("CPAMP 配置不是有效的 JSON 对象")
	}
	var panel, address string
	if raw, ok := fields["panelPath"]; ok && json.Unmarshal(raw, &panel) != nil {
		return nil, fmt.Errorf("CPAMP panelPath 格式无效")
	}
	if raw, ok := fields["httpAddr"]; ok && json.Unmarshal(raw, &address) != nil {
		return nil, fmt.Errorf("CPAMP httpAddr 格式无效")
	}
	if panelOverride != "" {
		panel, err = expandedPath(panelOverride)
	} else if panel == "" {
		panel = filepath.Join(root, "management.commandcodebridge.html")
	} else if !filepath.IsAbs(panel) {
		panel = filepath.Join(filepath.Dir(path), panel)
	}
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(filepath.Ext(panel), ".html") || filepath.Clean(panel) == filepath.Clean(path) {
		return nil, fmt.Errorf("CPAMP 面板路径必须是独立的 .html 文件")
	}
	if address == "" {
		address = "0.0.0.0:18317"
	}
	var configured string
	_ = json.Unmarshal(fields["panelPath"], &configured)
	if configured != "" && !filepath.IsAbs(configured) {
		configured = filepath.Join(filepath.Dir(path), configured)
	}
	mode := "file"
	if !configExists {
		mode = "environment"
	}
	return &nativePanelTarget{InstallDir: root, ConfigPath: path, PanelPath: filepath.Clean(panel), Source: source, ConfigExists: configExists, ConfigurationMode: mode, RestartRequired: filepath.Clean(configured) != filepath.Clean(panel), config: content, fields: fields, address: address}, nil
}

// Only read the needed process environment values. Never return management
// secrets or the entire environment through the plugin API.
func runningPanelTargets(c Config) []*nativePanelTarget {
	if runtime.GOOS != "linux" {
		return nil
	}
	return runningPanelTargetsAt(c, "/proc")
}

func runningPanelTargetsAt(c Config, procRoot string) []*nativePanelTarget {
	entries, _ := os.ReadDir(procRoot)
	var targets []*nativePanelTarget
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		base := filepath.Join(procRoot, entry.Name())
		executable, err := os.Readlink(filepath.Join(base, "exe"))
		if err != nil || filepath.Base(executable) != "cpa-manager-plus" {
			continue
		}
		binaryDir := filepath.Dir(executable)
		configPath := filepath.Join(binaryDir, "config.json")
		environment, err := os.ReadFile(filepath.Join(base, "environ"))
		// Without environment access we cannot rule out a custom config path.
		if err != nil {
			continue
		}
		panelEnv, address := "", ""
		configEnv, usageEnv := false, false
		dataDir := ""
		for _, variable := range strings.Split(string(environment), "\x00") {
			key, value, _ := strings.Cut(variable, "=")
			switch key {
			case "CPA_MANAGER_CONFIG":
				if strings.TrimSpace(value) != "" {
					configPath = value
					configEnv = true
				}
			case "PANEL_PATH":
				panelEnv = value
			case "HTTP_ADDR":
				address = value
			case "USAGE_DATA_DIR":
				dataDir = strings.TrimSpace(value)
				usageEnv = usageEnv || dataDir != ""
			case "USAGE_DB_PATH":
				usageEnv = usageEnv || strings.TrimSpace(value) != ""
			}
		}
		cwd, _ := os.Readlink(filepath.Join(base, "cwd"))
		if !filepath.IsAbs(configPath) {
			configPath = filepath.Join(cwd, configPath)
		}
		if panelEnv != "" && !filepath.IsAbs(panelEnv) {
			panelEnv = filepath.Join(cwd, panelEnv)
		}
		root := binaryDir
		if filepath.Base(filepath.Dir(binaryDir)) == "runtime" {
			root = filepath.Dir(filepath.Dir(binaryDir))
		}
		if c.CPAMPInstallDir != "" {
			wanted, e := expandedPath(c.CPAMPInstallDir)
			if e != nil || (wanted != root && wanted != binaryDir) {
				continue
			}
		}
		if c.CPAMPConfigPath != "" {
			wanted, e := expandedPath(c.CPAMPConfigPath)
			if e != nil || wanted != filepath.Clean(configPath) {
				continue
			}
		}
		target, e := readPanelTargetConfig(configPath, root, "running_process", c.CPAMPPanelPath, usageEnv && !configEnv)
		if e != nil {
			// Keep the confirmed process visible. A bad config must not make us
			// fall back to an inactive runtime and install into the wrong place.
			info, statErr := os.Stat(configPath)
			target = &nativePanelTarget{InstallDir: root, ConfigPath: filepath.Clean(configPath), Source: "running_process", ConfigExists: statErr == nil && info.Mode().IsRegular(), ConfigurationMode: "file", DiscoveryError: e.Error(), address: "0.0.0.0:18317"}
		} else if !target.ConfigExists && dataDir == "" && panelEnv == "" {
			// A new config changes CPAMP's implicit data and archive defaults in
			// DB-only mode. Require an explicit data directory or existing panel.
			target.DiscoveryError = "仅设置 USAGE_DB_PATH 时无法安全创建默认配置；请先在 Manager Server 启动环境中固定 USAGE_DATA_DIR 或 PANEL_PATH"
		}
		target.PID = pid
		target.panelEnvironment = panelEnv
		if panelEnv != "" {
			target.PanelPath = panelEnv
			target.RestartRequired = false
		}
		if address != "" {
			target.address = address
		}
		targets = append(targets, target)
	}
	return targets
}

func discoverPanelTargets(c Config, roots []string) []*nativePanelTarget {
	if c.CPAMPConfigPath != "" {
		path, e := expandedPath(c.CPAMPConfigPath)
		if e != nil {
			return nil
		}
		root := filepath.Dir(path)
		if c.CPAMPInstallDir != "" {
			root, e = expandedPath(c.CPAMPInstallDir)
			if e != nil {
				return nil
			}
		}
		validRoot := managerBinaryExists(root)
		if !validRoot {
			matches, _ := filepath.Glob(filepath.Join(root, "runtime", "*", "cpa-manager-plus*"))
			for _, candidate := range matches {
				if filepath.Base(candidate) == "cpa-manager-plus" || filepath.Base(candidate) == "cpa-manager-plus.exe" {
					validRoot = true
				}
			}
		}
		if !validRoot {
			return nil
		}
		target, e := readPanelTarget(path, root, "configured_path", c.CPAMPPanelPath)
		if e == nil {
			return []*nativePanelTarget{target}
		}
		return nil
	}
	if c.CPAMPInstallDir != "" {
		root, e := expandedPath(c.CPAMPInstallDir)
		if e != nil {
			return nil
		}
		roots = []string{root}
	}
	var targets []*nativePanelTarget
	seen := map[string]bool{}
	for _, root := range roots {
		root, e := expandedPath(root)
		if e != nil {
			continue
		}
		var configs []string
		if managerBinaryExists(root) {
			configs = append(configs, filepath.Join(root, "config.json"))
		}
		// The installer writes this marker for the active runtime. Never choose
		// the lexically newest package when several versions are retained.
		if script, e := os.ReadFile(filepath.Join(root, "run.sh")); e == nil {
			for _, line := range strings.Split(string(script), "\n") {
				if packageName, ok := strings.CutPrefix(strings.TrimSpace(line), "# CPAMP_RUNTIME_PACKAGE="); ok && strings.HasPrefix(packageName, "cpa-manager-plus_") && !strings.ContainsAny(packageName, "/\\\r\n") {
					dir := filepath.Join(root, "runtime", packageName)
					if managerBinaryExists(dir) {
						configs = append(configs, filepath.Join(dir, "config.json"))
					}
				}
			}
		}
		if len(configs) == 0 {
			matches, _ := filepath.Glob(filepath.Join(root, "runtime", "*", "config.json"))
			for _, path := range matches {
				if managerBinaryExists(filepath.Dir(path)) {
					configs = append(configs, path)
				}
			}
		}
		for _, path := range configs {
			if seen[path] {
				continue
			}
			seen[path] = true
			target, e := readPanelTarget(path, root, "default_directory", c.CPAMPPanelPath)
			if e == nil {
				targets = append(targets, target)
			}
		}
	}
	return targets
}

func defaultPanelRoots() []string {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	executable, _ := os.Executable()
	return []string{filepath.Join(home, "cpa-manager-plus"), filepath.Join(cwd, "cpa-manager-plus"), filepath.Join(filepath.Dir(executable), "cpa-manager-plus"), cwd}
}

func panelStatus(c Config, targets []*nativePanelTarget, content []byte) nativePanelStatus {
	home, _ := os.UserHomeDir()
	status := nativePanelStatus{DefaultInstallDir: filepath.Join(home, "cpa-manager-plus"), Available: len(content) > 0}
	if len(targets) != 1 {
		status.Message = "未找到同机 CPAMP 二进制安装，请填写安装目录或配置文件路径；不同服务器需要在 Manager Server 所在主机安装面板"
		if len(targets) > 1 {
			status.Message = "发现多个 CPAMP 实例，请选择安装目录或配置文件路径"
			for _, t := range targets {
				status.Candidates = append(status.Candidates, t.ConfigPath)
			}
			sort.Strings(status.Candidates)
		}
		return status
	}
	status.nativePanelTarget = targets[0]
	if status.DiscoveryError != "" {
		status.ServedPage = observePanel(status.nativePanelTarget, content)
		status.Message = "已找到运行中的 CPAMP，但配置无法用于安装：" + status.DiscoveryError
		return status
	}
	if c.CPAMPPanelPath != "" && status.panelEnvironment != "" {
		requested, e := expandedPath(c.CPAMPPanelPath)
		if e != nil || requested != status.PanelPath {
			status.Message = "运行中 CPAMP 的 PANEL_PATH 覆盖了面板路径；请使用检测到的路径或调整 Manager Server 启动环境"
			return status
		}
	}
	status.Installable = len(content) > 0
	if data, e := os.ReadFile(status.PanelPath); e == nil {
		status.Installed = bytes.Equal(data, content) && len(content) > 0
	}
	status.ServedPage = observePanel(status.nativePanelTarget, content)
	status.Active = status.ServedPage.Matches
	if status.Active {
		status.RestartRequired = false
		status.Message = "原生面板已生效，刷新浏览器后可使用"
	} else if status.Installed {
		status.RestartRequired = true
		status.Message = "面板文件已安装，请重启 Manager Server 后检查生效状态"
		if status.panelEnvironment != "" {
			status.RestartRequired = false
			status.Message = "面板文件已安装到运行中 PANEL_PATH，但 HTTP 核验未通过，请检查监听地址和页面读取权限"
		}
	} else {
		status.Message = "已找到 CPAMP，点击安装即可写入内置原生面板并配置 panelPath"
		if status.panelEnvironment != "" {
			status.Message = "已找到运行中 CPAMP 的 PANEL_PATH，安装将更新它指向的页面，无需创建配置文件或重启 Manager Server"
		} else if !status.ConfigExists {
			status.Message = "已找到通过环境变量运行的 CPAMP，config.json 不存在属正常情况；安装会创建最小面板配置，首次生效需重启 Manager Server"
		}
	}
	return status
}

func panelIsActive(target *nativePanelTarget, content []byte) bool {
	return observePanel(target, content).Matches
}

func observePanel(target *nativePanelTarget, content []byte) panelObservation {
	observation := panelObservation{}
	host, port, e := net.SplitHostPort(target.address)
	if e != nil {
		return observation
	}
	if host == "0.0.0.0" || host == "" || host == "localhost" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	ip := net.ParseIP(host)
	// Status verification is anonymous and confined to the local listener.
	if ip == nil || !ip.IsLoopback() {
		return observation
	}
	client := http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	r, e := client.Get("http://" + net.JoinHostPort(host, port) + "/management.html")
	if e != nil {
		return observation
	}
	defer r.Body.Close()
	actual, e := io.ReadAll(io.LimitReader(r.Body, 16<<20+1))
	observation.HTTPStatus = r.StatusCode
	if e != nil || len(actual) > 16<<20 || r.StatusCode != http.StatusOK {
		return observation
	}
	sum := sha256.Sum256(actual)
	observation.Checked = true
	observation.SHA256 = hex.EncodeToString(sum[:])
	observation.NativeQuota = bytes.Contains(actual, []byte("data-commandcode-native-quota"))
	observation.Matches = len(content) > 0 && bytes.Equal(actual, content)
	return observation
}

func inspectNativePanel(c Config) nativePanelStatus {
	content, _ := nativePanelContent()
	targets := runningPanelTargets(c)
	if len(targets) == 0 {
		targets = discoverPanelTargets(c, defaultPanelRoots())
	}
	return panelStatus(c, targets, content)
}

func installNativePanel(c Config) (any, error) {
	content, e := nativePanelContent()
	if e != nil {
		return nil, e
	}
	status := inspectNativePanel(c)
	if status.nativePanelTarget == nil {
		return nil, fail(409, status.Message)
	}
	if !status.Installable {
		return nil, fail(409, status.Message)
	}
	if c.CPAMPPanelPath != "" && status.panelEnvironment != "" {
		path, e := expandedPath(c.CPAMPPanelPath)
		if e != nil || path != status.PanelPath {
			return nil, fail(409, status.Message)
		}
	}
	backups, e := applyNativePanel(status.nativePanelTarget, content)
	if e != nil {
		return nil, e
	}
	next := inspectNativePanel(c)
	next.Backups = backups
	if !next.Active {
		if next.panelEnvironment == "" {
			next.RestartRequired = true
			next.Message = "面板与配置已写入，原有文件已备份。请重启 Manager Server，然后点击检查生效状态"
		}
	}
	return next, nil
}

type panelFileChange struct {
	path          string
	before, after []byte
	existed       bool
	mode          os.FileMode
	backup        string
}

func preparePanelChange(path string, content []byte) (panelFileChange, error) {
	change := panelFileChange{path: path, after: content, mode: 0644}
	info, e := os.Lstat(path)
	if e == nil {
		if !info.Mode().IsRegular() {
			return change, fail(409, "面板或配置路径不是普通文件，请检查路径")
		}
		change.existed = true
		change.mode = info.Mode().Perm()
		change.before, e = os.ReadFile(path)
	} else if !os.IsNotExist(e) {
		return change, fail(500, "无法读取 CPAMP 文件，请检查 CPA 运行用户权限")
	}
	if e != nil && change.existed {
		return change, fail(500, "无法读取 CPAMP 文件，请检查 CPA 运行用户权限")
	}
	return change, nil
}

func preparePanelConfigChange(target *nativePanelTarget, content []byte) (panelFileChange, error) {
	change, err := preparePanelChange(target.ConfigPath, content)
	if err != nil {
		return change, err
	}
	if change.existed != target.ConfigExists || !bytes.Equal(change.before, target.config) {
		return change, fail(409, "CPAMP 配置已变化，请重新检测后再安装")
	}
	return change, nil
}

func writePanelFile(path string, content []byte, mode os.FileMode, existed bool) error {
	file, e := os.CreateTemp(filepath.Dir(path), ".commandcodebridge-*")
	if e != nil {
		return e
	}
	name := file.Name()
	defer os.Remove(name)
	if _, e = file.Write(content); e == nil {
		e = file.Chmod(mode)
	}
	if e == nil {
		e = file.Sync()
	}
	closeErr := file.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if !existed {
		// Publish the complete file atomically without replacing a file another
		// process created since inspection. Temp and destination share a directory.
		return os.Link(name, path)
	}
	return os.Rename(name, path)
}

// Only panelPath changes. Preserve every other config value and private backup
// byte-for-byte; do not launch shells, alter service units or stop processes.
func applyNativePanel(target *nativePanelTarget, content []byte) ([]string, error) {
	return applyNativePanelWithWriter(target, content, writePanelFile)
}

func applyNativePanelWithWriter(target *nativePanelTarget, content []byte, write func(string, []byte, os.FileMode, bool) error) ([]string, error) {
	if target.DiscoveryError != "" {
		return nil, fail(409, target.DiscoveryError)
	}
	if !strings.EqualFold(filepath.Ext(target.PanelPath), ".html") || filepath.Clean(target.PanelPath) == filepath.Clean(target.ConfigPath) {
		return nil, fail(409, "面板路径必须是独立的 .html 文件")
	}
	current, e := os.ReadFile(target.ConfigPath)
	if (target.ConfigExists && (e != nil || !bytes.Equal(current, target.config))) || (!target.ConfigExists && !os.IsNotExist(e)) {
		return nil, fail(409, "CPAMP 配置已变化，请重新检测后再安装")
	}
	changes := []panelFileChange{}
	panel, e := preparePanelChange(target.PanelPath, content)
	if e != nil {
		return nil, e
	}
	if !bytes.Equal(panel.before, content) {
		changes = append(changes, panel)
	}
	if target.panelEnvironment == "" {
		fields := map[string]json.RawMessage{}
		for k, v := range target.fields {
			fields[k] = v
		}
		old := ""
		_ = json.Unmarshal(fields["panelPath"], &old)
		if old != "" && !filepath.IsAbs(old) {
			old = filepath.Join(filepath.Dir(target.ConfigPath), old)
		}
		if filepath.Clean(old) != target.PanelPath {
			fields["panelPath"] = jsonBytes(target.PanelPath)
			data, e := json.MarshalIndent(fields, "", "  ")
			if e != nil {
				return nil, fail(500, "无法生成 CPAMP 配置")
			}
			data = append(data, '\n')
			config, e := preparePanelConfigChange(target, data)
			if e != nil {
				return nil, e
			}
			changes = append(changes, config)
		}
	}
	backups := []string{}
	for i := range changes {
		change := &changes[i]
		if !change.existed {
			continue
		}
		sum := sha256.Sum256(change.before)
		change.backup = change.path + ".commandcodebridge-backup-" + hex.EncodeToString(sum[:6]) + "-" + id()
		file, e := os.OpenFile(change.backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return backups, fail(500, "备份失败，未修改 CPAMP；请检查目录写入权限")
		}
		_, writeErr := file.Write(change.before)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return backups, fail(500, "备份失败，未修改 CPAMP；请检查磁盘空间")
		}
		backups = append(backups, change.backup)
	}
	for i, change := range changes {
		if e = panelFileUnchanged(change); e == nil {
			e = write(change.path, change.after, change.mode, change.existed)
		}
		if e != nil {
			rollbackFailed := false
			for j := i - 1; j >= 0; j-- {
				previous := changes[j]
				current, readErr := os.ReadFile(previous.path)
				if readErr != nil || !bytes.Equal(current, previous.after) {
					rollbackFailed = true
					continue
				}
				if previous.existed {
					rollbackFailed = write(previous.path, previous.before, previous.mode, true) != nil || rollbackFailed
				} else {
					rollbackFailed = os.Remove(previous.path) != nil || rollbackFailed
				}
			}
			if rollbackFailed {
				return backups, fail(500, "安装失败且自动恢复未完成，请使用备份恢复 CPAMP 文件")
			}
			return backups, fail(500, "安装失败，已恢复原文件；请检查目录写入权限和磁盘空间")
		}
	}
	return backups, nil
}

func panelFileUnchanged(change panelFileChange) error {
	info, e := os.Lstat(change.path)
	if !change.existed && os.IsNotExist(e) {
		return nil
	}
	if e != nil || !change.existed || !info.Mode().IsRegular() {
		return fmt.Errorf("CPAMP file changed")
	}
	current, e := os.ReadFile(change.path)
	if e != nil || !bytes.Equal(current, change.before) {
		return fmt.Errorf("CPAMP file changed")
	}
	return nil
}
