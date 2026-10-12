# CPAMP 部署、升级与回滚

目标 CPAMP 为 `https://cpa.gdp.pw/`，已确认可达。源码与安装包托管至 GitHub，用户自行安装；以下步骤供现场执行，当前尚未在目标服务完成安装。

## v0.2.5：插件内一键安装原生面板

CPA 与 CPAMP Manager Server 直接运行在同一主机时：

1. 在插件商店升级 CommandCodeBridge 至 v0.2.5，或安装对应平台动态库并重启 CPA。页面已内置到动态库，商店安装也能使用。
2. 打开插件控制台的「设置 → CPAMP 原生面板」，点击「安装原生面板」。默认查找 Linux 正在运行的 `cpa-manager-plus`、官方安装目录 `~/cpa-manager-plus` 及 `run.sh` 指向的 runtime。
3. 首次从内嵌页面切换时，按原方式重启 **Manager Server**，返回点击「检查生效状态」，确认「原生面板已生效」。后续更新已配置的页面通常只需强制刷新浏览器。已有 `PANEL_PATH` 时直接更新它实际指向的页面。
4. 原生凭证列表、卡片或详情额度页点击「刷新额度」，现有 API key 账号无需重新导入。OAuth 继续使用 v0.2.4 的浏览器电脑本地接收器。

路径不同可在此页面修改，安装或检查按钮会先保存路径。相同入口也通过 CPA 插件配置字段提供：

| 配置项 | 默认 | 作用 |
| --- | --- | --- |
| `cpamp_install_dir` | 留空自动探测；官方默认 `~/cpa-manager-plus` | 自定义 CPAMP 安装目录或直接解压运行目录 |
| `cpamp_config_path` | 自动选择活动 `config.json` | 多实例或独立配置文件时明确指定 |
| `cpamp_panel_path` | 已有页面路径；否则安装目录下 `management.commandcodebridge.html` | 自定义页面文件位置，父目录须已存在 |

路径可在插件控制台修改并存入 `settings.json`。CPA YAML 中显式填写的非空 CPAMP 路径在注册/重载时优先，确保插件配置页修改能覆盖旧保存值；使用控制台管理路径时，保留 YAML 的这三项为空。安装仅改变 Manager Server 的 `panelPath`，保存其他 JSON 值，备份原配置和页面，保留文件权限。发现多个实例不会自动选择。CPA 运行用户必须有相应目录读写权限。

此入口操作 **CPA 插件所在主机的文件**。Manager Server 位于另一台机器或不共享目录的容器时，默认路径无法跨主机生效，应按下一节在 Manager Server 所在主机安装页面。不会连接其他服务器，也不会读取或更换管理密钥。

## v0.2.4：二进制部署的面板替换与 POST 认证

**同时升级插件动态库和 CPAMP 管理页面**。Release 的 `commandcodebridge_0.2.4_cpamp-web.zip` 包含已构建的 `management.html`，基于 CPAMP `dbf61dd`。用户确认 Manager Server 直接运行二进制：在该二进制的启动环境设置 `PANEL_PATH` 为自定义页面的绝对路径，再重启 Manager Server。只重启 CPA 或强刷浏览器不会替换服务端内置页面。

Linux 手动启动示例（将两处路径换成现场实际值，并保留现有参数）：

```bash
export PANEL_PATH='/实际绝对路径/management.html'
# 在此 shell 使用现有命令启动 Manager Server
```

若由 systemd 管理，在实际 Manager Server 的 service override 中加入以下设置，再执行 `daemon-reload` 并重启该服务：

```ini
[Service]
Environment="PANEL_PATH=/实际绝对路径/management.html"
```

Windows 则在启动该程序的同一 PowerShell 进程设置 `$env:PANEL_PATH = 'C:\实际路径\management.html'`；Windows 服务应修改其实际启动环境。不要只在另一个终端设置环境变量。也可在既有 JSON 配置中设置 `panelPath`，`PANEL_PATH` 优先。

Manager Server 无法打开文件时会回退到内置页面，因此安装后必须检查实际 HTTP 内容：

```bash
python3 verify_cpamp_panel.py https://cpa.gdp.pw/management.html --expected '/实际绝对路径/management.html'
```

脚本随两类 ZIP 提供，不需要管理密钥；应显示三项 `PASS` 和 `expected_page: MATCH`。若失败，检查运行进程的环境、文件读取权限、代理目标和缓存，再强刷浏览器。现有 API key 凭证无需重新导入；在原生列表、卡片或详情额度页点击「刷新额度」。余额与窗口百分比使用上游实际值；缺少月上限时显示未知。

OAuth 还需在**浏览器所在电脑**运行 `commandcodebridge-login`。Windows 从 Windows ZIP 解压并运行 `.exe`，无需在远端 Linux 服务器运行此接收器。在 CPAMP 重新开始登录，复制官方授权链接到接收器，授权后复制接收器显示的认证结果到原生卡片提交。官方回传是 POST 正文，地址栏的裸 callback 无法交付凭证；v0.2.3 的地址栏方案需要升级。详细步骤见 [CPAMP 前端适配](../integrations/cpamp/README.md)。

## 通过 GitHub 链接安装

仓库已公开。CPA v8.0.23 从插件源登记信息解析 GitHub Release，安装时按宿主平台选包并核验 `checksums.txt`，无需 GitHub Token。

插件源链接：

```text
https://raw.githubusercontent.com/Gaoshou101/CommandCodeBridge/main/registry.json
```

将它添加到现有 CPA 配置的 `plugins.store-sources`（保留已有源和配置）：

```yaml
plugins:
  enabled: true
  store-sources:
    - https://raw.githubusercontent.com/Gaoshou101/CommandCodeBridge/main/registry.json
```

保存配置并按面板提示重载或重启，在 CPAMP「插件商店」选择 CommandCodeBridge，版本选 Latest 或 `v0.2.5`，点击安装。安装器自动选择 ZIP、验证校验值、写入插件目录并保存配置。同机二进制的原生页面通过插件设置一键安装；浏览器电脑的接收器需从对应平台 ZIP 取出。随后核实插件启用，按下方验收步骤添加 Studio key 和模型。

GitHub 仓库首页用于浏览源码；在“插件源”输入框应填写上面的 JSON 链接。若当前面板没有源配置入口，可通过 CPA 配置文件添加。服务器需能访问 `raw.githubusercontent.com`、`api.github.com` 和 GitHub 下载地址；无法访问时检查现有 CPA 全局代理。

## 手动下载安装包

从 [v0.2.5 Release](https://github.com/Gaoshou101/CommandCodeBridge/releases/tag/v0.2.5) 下载对应平台 ZIP 和 `checksums.txt`，可匿名下载。动态库按 CPA 服务器平台选择；本地接收器按浏览器电脑平台选择，两者可能不同。

| 宿主 | 安装包后缀 | 动态库 | 要求 |
| --- | --- | --- | --- |
| Linux x64 | `_linux_amd64.zip` | `commandcodebridge.so` | glibc ≥2.34 |
| Linux ARM64 | `_linux_arm64.zip` | `commandcodebridge.so` | glibc ≥2.34 |
| Windows x64 | `_windows_amd64.zip` | `commandcodebridge.dll` | 64 位 CPA |
| macOS Apple Silicon | `_darwin_arm64.zip` | `commandcodebridge.dylib` | ARM64 CPA |

以上均已在对应平台使用 CPA v8.0.23 运行模拟集成与异常恢复测试。Linux Alpine/musl 需另外构建。

Linux 下载后，在文件所在目录核验：

```bash
sha256sum --check --ignore-missing checksums.txt
```

Windows 使用 `Get-FileHash -Algorithm SHA256`，macOS 使用 `shasum -a 256`，与 `checksums.txt` 中对应文件的值比较。首版旧式连字符命名 ZIP 保留供已下载用户使用，链接安装使用下划线命名 ZIP。

## 部署前确认

1. 从目标主机实际服务配置确认 CPA 版本、启动命令、运行用户、CPU、glibc 和配置/凭据/插件目录。不要沿用其他服务器的路径。
2. 选择对应平台安装包，核验 `.zip.sha256` 和包内 `BUILD.json` 的版本、来源提交、库校验与 glibc 最低要求。使用包含插件支持的 CPA；`no-plugin` 宿主不能加载动态库。
3. 检查是否已安装旧版 CommandCodeBridge。保留旧库、CPA 配置、CPA auth-dir 和插件 data_dir 的可恢复备份。
4. 确认现有 CPA 全局代理。备份与配置中可能有密钥，应保持私有。

## 安装与验收

1. 将库安装到实际 `plugins.dir`，由 CPA 运行用户读取。合并 `config.example.yaml`，使用确认后的 data_dir。首次模型留空。
2. Docker 部署应持久化 plugins、data_dir 和现有 auth-dir，使用容器内路径。已有 settings.json 优先于 YAML。
3. 在维护窗口重启实际 CPA 服务，检查 `plugin loaded plugin_id=commandcodebridge`，确认没有 ABI、元数据或运行库错误。
4. 通过现有 CPAMP 登录进入插件控制台。Manager Server 管理员密钥、CPA Management Key 和客户端密钥分别用于不同接口。
5. 从控制台加入 Studio key；实际 key 只存 CPA auth-dir。先查询用量，再选择具备套餐权限的模型。
6. 验证外部面板的资源页和管理路由均通过正确代理；测试两种管理鉴权头所适用的登录模式，以及子目录路径。
7. 完成一条短客户端调用，检查实际 usage、插件统计和 CPA 记账。测试凭据备注、停用、恢复、模型编辑和手机布局。
8. 确认 `storage_error` 为空，并等待 `storage_pending=false`，再重启 CPA，核实数据、账号登记及模型恢复。

## 升级

1. 先备份旧库、配置和两类数据目录。
2. 检查新包的 ABI/schema、宿主版本与平台要求。
3. 正常停止 CPA，等待插件活动请求结束和最终刷盘，再替换库。强制结束进程可能丢失尚未刷盘的日志/统计。
4. 启动后重复加载、用量、短生成、数据恢复验收。升级时保留现有 settings.json，若更改默认 YAML 需明确迁移。

## 回滚

1. 正常停止 CPA，恢复已验证的旧库和对应配置。
2. 如涉及格式迁移或数据异常，恢复成套的 data_dir 和 auth-dir 备份；恢复 auth-dir 会影响其他渠道，必须使用与当前安装相匹配的备份。
3. 启动 CPA，验证插件加载、凭据登记、模型列表和一次短请求。

## 常见问题

| 现象 | 检查 |
| --- | --- |
| 无法加载库 | CPA 是否含插件支持；CPU/系统/运行库是否匹配；文件是否可读 |
| 模型可见但返回 403 | 目录不保证套餐权限，检查 `MODEL_NOT_IN_PLAN` |
| 接口直连失败或超时 | 确认 CPA 全局代理；插件不继承 Windows 系统代理 |
| 用量缺失 | 查看 `unavailable` / `status`；查询失败保留旧值 |
| 日志写入失败 | 检查 data_dir 的磁盘空间、权限及 `storage_error` |
| 重启后设置未按 YAML 改变 | 已存 settings.json 优先；采用明确的迁移方案 |
| CPAMP 登录失败 | 确认使用 Manager Server 管理员密钥；它不同于 CPA Management Key |
