<h1 align="center">CommandCodeBridge</h1>

<p align="center">将 CommandCode Provider API 接入 CLIProxyAPI，在 CPAMP 中管理凭证、模型、额度和请求记录。</p>

<p align="center"><a href="https://github.com/Gaoshou101/CommandCodeBridge/releases/latest">下载最新版</a> · <a href="./CHANGELOG.md">更新记录</a> · <a href="./docs/DEPLOYMENT.md">部署说明</a></p>

## 功能

- **多凭证管理**：添加 Studio API key 或通过官方网页登录授权，支持备注、优先级、停用、删除和会话重新分配。
- **模型映射**：读取官方模型目录，选择导入，设置客户端别名、协议和逐模型思考强度。
- **协议接入**：支持 Chat Completions、Responses、Anthropic Messages，包含流式响应、工具调用和图片；原生 Messages 历史保留 Claude thinking signature。
- **原生额度窗口**：在 CPAMP 列表、卡片和详情查看 5h、周、月额度，查询窗口请求统计、预测和周期快照。
- **请求记录**：查看凭证健康度、tokens、缓存用量、预估花费、首字耗时、输出速率及模型统计；支持明暗主题和手机布局。

当前版本 **v0.2.9**。插件调用官方 Provider API；Go 订阅的 `/alpha/generate` 路径和固定上游 provider 模式不在支持范围内。账户用量通过官方 `/alpha/...` 查询接口读取，模型权限以实际套餐为准。

## 快速安装

需要启用插件的 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)。四个平台的原生宿主联测基线为 **CPA v8.0.23**；名称含 `no-plugin` 的宿主构建不能加载本插件。

将以下源合并到现有 CPA 配置，保留已有插件源和其他配置：

```yaml
plugins:
  enabled: true
  store-sources:
    - https://raw.githubusercontent.com/Gaoshou101/CommandCodeBridge/main/registry.json
```

保存并重载配置，在 CPAMP「插件商店」找到 **CommandCodeBridge**，选择 Latest 或 **v0.2.9** 安装。支持热升级的宿主会下载匹配平台的 ZIP、验证校验值并切换版本；安装后检查插件控制台显示的运行版本。

也可从 [Releases](https://github.com/Gaoshou101/CommandCodeBridge/releases/latest) 手动下载 ZIP 和 `checksums.txt`。按 **CPA 所在服务器**选择动态库：

| 平台 | 安装包后缀 | 动态库 |
| --- | --- | --- |
| Linux x64 | `_linux_amd64.zip` | `commandcodebridge.so` |
| Linux ARM64 | `_linux_arm64.zip` | `commandcodebridge.so` |
| Windows x64 | `_windows_amd64.zip` | `commandcodebridge.dll` |
| macOS Apple Silicon | `_darwin_arm64.zip` | `commandcodebridge.dylib` |

Linux 发布包要求 glibc ≥2.34；Alpine/musl 需自行构建。手动安装时核验校验值，将动态库放入实际 `plugins.dir`，合并 [config.example.yaml](./config.example.yaml)，按宿主的加载方式启用插件。完整步骤见[部署说明](./docs/DEPLOYMENT.md)。

## 快速开始

1. 在 CPAMP 打开 CommandCodeBridge 插件控制台，或访问 CPA 地址下的 `/v0/resource/plugins/commandcodebridge/console`。直接访问时使用 **CPA 管理密钥**登录。
2. 在「凭据与用量」添加 **CommandCode Studio API key**，点击刷新，确认套餐和真实余额。
3. 在「模型映射」获取目录，选择当前套餐可用的模型并导入；按需要设置客户端别名。
4. 在客户端填写 **CPA API 地址、CPA 客户端密钥、已导入的模型别名**，即可调用。

管理密钥、客户端密钥和 CommandCode Studio API key 分别用于管理、客户端调用和上游授权，不能互换。上游 key 由 CPA 保存到 `auth-dir`，无需写入模型配置。

官方网页登录入口位于 CPAMP「OAuth 登录」。远端服务器场景下，在**浏览器所在电脑**运行安装包里的 `commandcodebridge-login`（Windows 为 `.exe`），再完成授权并提交接收器显示的认证结果。浏览器电脑与 CPA 服务器平台不同时，接收器需另下载对应电脑平台的包。详见[远程认证步骤](./integrations/cpamp/README.md#远程认证步骤)。

## 安装或更新原生面板

**升级插件后还要更新 CPAMP 页面**，才能使用新版凭证布局。v0.2.9 动态库已内嵌匹配页面。

CPA 与 Manager Server 在同一主机直接运行时：

1. 打开插件控制台「设置 → CPAMP 原生面板」。
2. 首次使用点击「安装原生面板」；已有外部页面点击「更新原生面板」。
3. 点击「检查生效状态」，再强制刷新浏览器，在凭证页点击「刷新额度」。

| 操作 | 是否需要重启 |
| --- | --- |
| 保存日常插件设置，或更新已启用外部面板的文件 | 无需重启 |
| 通过支持热升级的插件商店更新动态库 | 确认宿主已切换到新版本 |
| 首次启用外部页面，或更改 Manager Server 的页面路径 | 重启 Manager Server 一次 |
| 迁移插件数据目录，或旧宿主不支持动态库热升级 | 安排相应服务重启 |

Manager Server 在另一台主机或独立容器中时，在其运行环境安装 `commandcodebridge_0.2.9_cpamp-web.zip` 内的 `management.html`，通过 `PANEL_PATH` 或 `panelPath` 托管。面板基于 CPAMP 源码 `dbf61dd`；不同版本应在匹配源码上应用适配。

**完整周期快照另需 Manager Server 接受 `commandcode` 平台。** 前端包附带适配脚本、服务器名单补丁和测试；按现有 Manager Server 的同一源码版本重建并部署。仅更新插件和页面不能替代这个服务器改动。[原生集成说明](./integrations/cpamp/README.md)包含构建、HTTP 生效核验和回滚步骤。

## 套餐与额度

CPAMP 套餐名统一显示 **Go、GOAT、Pro、Max**。月额度与 5h 顶部对齐，两列之间无分割线；月额度下方显示真实剩余 credits 与月上限。

月比例优先使用上游真实窗口比例和 cap；上游未提供月窗口时，用 `monthly_credits` 除以已核实的套餐月上限。以下依据[官方 Pricing & Limits](https://commandcode.ai/docs/resources/pricing-limits)和官方 `command-code@1.81.0`，核实于 **2026-10-13**：

| 套餐 | 原始套餐标识 | 月上限（credits） |
| --- | --- | ---: |
| Go | `individual-go` / `individual-go-v1` | 10 |
| GOAT | `individual-goat` | 70 |
| Pro | `individual-pro-v1` | 80 |
| Pro（旧版） | `individual-pro` | 30 |
| Max 10× | `individual-max` | 150 |
| Max 20× | `individual-ultra` | 300 |

例如 GOAT 剩余 61.083 credits 时，显示 **61.083 / 70 credits、剩余 87%**。两种 Pro 均显示 Pro，两种 Max 均显示 Max，计算仍保留原始套餐标识。

- 月余额不混入购买或免费 credits；Max 标准与高级模型限额独立，不相加。
- 5h 和周比例采用真实上游数据，重置时间取真实窗口或订阅周期。缺失值显示 `—` 或「重置未知」。
- 详情页的请求数、tokens、预估花费和预测来自原生窗口统计；花费为请求价格估算，不将账户 credits 当作历史美元花费。
- 刷新失败保留上次结果并标记过期；过期比例不用于新预测。历史周期缺失时不推算上个月边界。

## 配置与数据

完整配置见 [config.example.yaml](./config.example.yaml)。日常设置和模型映射可在插件控制台修改，保存后即时生效。已保存的 `settings.json` 通常优先于初始 YAML；显式非空的 CPAMP 路径配置按部署说明处理。

- `auth-dir`：上游凭证及会话恢复副本。
- `data_dir`：设置、模型映射、最近请求日志、统计数据，以及 `usage.json` 中的额度观察。v0.2.9 在重启和页面重新打开后恢复上次额度；凭证页每 30 秒同步服务端缓存与最近请求。额度刷新失败保留上次值和真实采集时间，更换密钥后不复用旧额度。
- Docker 部署应持久化以上目录和插件目录，并使用容器内路径。
- 出站网络复用 CPA 的全局代理；当前宿主通用 HTTP RPC 不支持凭据级代理。
- 同账号多个 key 共享额度及账号／模型冷却，额度不相加。

## 开发与验证

原生插件构建需要 **Go ≥1.26、CGO 和目标平台 C 编译器**。发行构建先生成 pinned CPAMP 页面，再内嵌到四个平台的动态库。

GitHub Actions 运行 Go race/vet、前端测试与类型检查、CPA 原生协议及异常恢复联测、CPAMP 页面安装与热更新联测，并校验四平台插件和独立前端包的来源、校验值及内嵌页面一致性。具体步骤与验证边界见以下文档。

| 文档 | 内容 |
| --- | --- |
| [部署说明](./docs/DEPLOYMENT.md) | 商店／手动安装、路径、升级、热更新和回滚 |
| [CPAMP 原生集成](./integrations/cpamp/README.md) | 页面构建、Manager 快照补丁、OAuth 和实际页面核验 |
| [验证记录](./docs/VALIDATION.md) | 模拟宿主、真实接口、浏览器验证及尚未覆盖的场景 |
| [版本说明](./docs/RELEASE_NOTES.md) | 当前发行内容与升级步骤 |
| [更新记录](./CHANGELOG.md) | 各版本改动 |

## 许可

本项目按 [MIT](./LICENSE) 发布，参考 [ClinePassBridge](https://github.com/xiao-qiu-qiu/ClinePassBridge)。第三方来源与版权见 [NOTICE](./NOTICE)。CommandCodeBridge 是独立集成项目。
