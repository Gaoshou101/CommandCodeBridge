# CPAMP 原生凭证集成

## v0.2.11：Antigravity 身份与单一基线

面板必须含 `antigravity/hub/` 且不得含 `antigravity/cli/`；UA 在构建产物中可能为模板拼接，不要求完整版本字面量。所有 Python 工具共用 `panel_validation.py`，发布 ZIP 必须保留 `integrations/cpamp/` 以运行校验与适配。更新 CPAMP 基线只改 `compatibility.json` 的 `cpamp_source_commit`，CI 自动用该源码构建页面和测试用 Manager；新源码仍须通过全部插入点、上游身份测试与原生联测。

升级至 v0.2.11 后更新原生面板、核验实际 HTTP 页面并强刷浏览器。只升级插件动态库不会替换已落盘页面。

## v0.2.9：最近请求与额度恢复

凭证工作区打开时读取插件脱敏缓存，每 30 秒同步最近请求与额度；切回浏览器页面时立即同步。最近请求取插件保存的真实模型请求，保留历史时间，不受 Manager 监控导入延迟影响，本地冷却跳过不计入状态条。Manager 的全量历史统计仍使用原有统计接口。

插件将额度观察写入 `data_dir/usage.json`，重启后按凭证指纹校验恢复；文件备注、模型设置或更新时间变更不清空额度。刷新失败保留上次值及原始采集时间，换密钥、代理或上游后清空旧观察。升级插件和页面均需完成才能使用此修复；Docker 需持久化插件 `data_dir`。

## v0.2.9 原生窗口功能

CommandCode 详情额度页复用 CPAMP 原生窗口卡片，支持当前与上个窗口的请求统计、预测和周期快照。列表保留左侧 5h／周、右侧月额度的简洁布局，两列无分割线，月额度与 5h 顶部对齐；月额度下方显示真实剩余 credits 和月上限。套餐名统一为 Go、GOAT、Pro、Max。

v0.2.9 四平台插件已内嵌匹配页面，独立前端包也包含上述功能。完整周期快照还需基于现有 Manager Server 的同一源码版本重建二进制并重启，使服务器接受 `commandcode` 平台；页面更新入口只更新 HTML。脚本新增的服务器改动仅为快照平台名单，不修改数据库结构。已完成该服务器补丁的环境只需更新插件和页面。

## 推荐入口

同机直接运行二进制时，升级插件后进入插件控制台「设置 → CPAMP 原生面板」，点击「安装原生面板」，首次切换后重启 Manager Server，再点击「检查生效状态」。插件动态库内置匹配版本页面，自动探测运行实例和官方默认 `~/cpa-manager-plus`，无需单独下载管理页面或手动设置环境变量。

**v0.2.6 路径修复**识别 `USAGE_DATA_DIR` / `USAGE_DB_PATH` 环境变量模式下没有默认 `config.json` 的 Linux 运行实例。设置页会显示实际 PID、配置位置和当前 HTTP 页面是否含原生额度组件。检查不会创建文件；只有执行安装才创建必要配置。仅数据库路径且没有 `PANEL_PATH` 时会阻止创建配置，提示先固定数据目录或页面路径。详见[部署诊断](../../docs/DEPLOYMENT.md)。

同处提供 `cpamp_install_dir`、`cpamp_config_path`、`cpamp_panel_path` 修改入口，并向 CPA 注册为插件配置项。安装保留其他 Manager Server 配置并创建私有备份。多个实例需明确选择；已有 `PANEL_PATH` 时更新其文件。异机/独立容器仍按下文在 Manager Server 所在主机安装页面。

## 两处问题的原因

旧插件 `auth.login.start` 固定返回「从控制台导入 key」，CPA 将它转换为 `failed to generate authorization url`。`0.2.2` 实现原生登录启动、轮询与凭证交付。

`0.2.2` 将远程 CPA 公开地址交给官网作为 callback，但 CommandCode 只允许 loopback。`0.2.3` 参考 CPAMP Codex/Claude 的 URL 提交方式，误以为凭证在回调查询参数里。重新核对官方 CLI 1.79.2 后确认：**浏览器流程以表单 POST 回传，legacy 以 JSON POST 回传**，裸 `localhost:45879/callback` 地址不含凭证。

`0.2.4` 补齐浏览器电脑上的本地 POST 接收器。它接收官网凭证，显示可复制认证结果，再由 CPAMP 原生卡片通过受管理鉴权保护的插件 POST 提交给远端 CPA。继续复用原生状态轮询和 CPA 凭证保存；CPA 通用回调仅提取 `code`，无法直接交付 CommandCode 的 `apiKey`。

CPAMP 当前源码的额度平台列表固定为内置平台，因此仅增加插件额度接口仍不会在原生凭证页渲染 CommandCode。本适配直接在 CPAMP 的原生网格、列表显示左右两区：左侧 5h、周额度上下两排，右侧月额度与 5h 顶部对齐；保留窗口标签、剩余百分比、进度条、相对重置时间和月余额。完整重置日期可悬停查看，刷新使用右侧图标；复用面板已有的 Button 和 QuotaProgressBar。详情额度页使用原生窗口历史卡片。

## 安装

1. 备份原插件动态库、CPA auth-dir、插件 data_dir 以及 CPAMP 当前管理页面。
2. 通过支持热升级的 CPA 插件商店更新 CommandCodeBridge；确认运行版本为 `0.2.11`。从 [v0.2.11 Release](https://github.com/Gaoshou101/CommandCodeBridge/releases/tag/v0.2.11) 选择匹配平台 ZIP，并核验 `checksums.txt`。
3. **同时升级管理页面**。同机使用上节一键入口；异机面板匹配包内 `BUILD.json` 的 CPAMP 基线时，下载 Release 的 `commandcodebridge_0.2.11_cpamp-web.zip`，使用包内 `management.html`，按下节安装。需要自行构建时，在对应 CPAMP 源码目录应用补丁：

```bash
python /path/to/CommandCodeBridge/integrations/cpamp/apply.py /path/to/CPA-Manager-Plus --check
python /path/to/CommandCodeBridge/integrations/cpamp/apply.py /path/to/CPA-Manager-Plus
cd /path/to/CPA-Manager-Plus
npm ci --ignore-scripts --workspace apps/web
npm --workspace apps/web run type-check
npm --workspace apps/web run test -- src/features/accounts/components/CommandCodeQuotaCard.test.tsx src/services/api/CommandCodeOAuth.test.ts src/services/api/oauth.test.ts src/features/oauth/OAuthPage.test.tsx src/features/accounts/components/accountDetail/AccountQuotaTab.test.tsx
npm --workspace apps/web run build
# 可选：在 CommandCodeBridge 源码目录打包上述构建产物
# python scripts/package_cpamp.py /path/to/CPA-Manager-Plus
```

脚本会先验证所有插入点再写入，重复执行不会重复插入。已验证的 CPAMP 源码由 `internal/bridge/compatibility.json` 的 `cpamp_source_commit` 指定，发行包从 `BUILD.json.compatibility` 读取同一值。适配允许该提交及其 Git 后代，发布必须精确匹配基线；同时拒绝旧 Antigravity UA。不同源码版本可能需要调整插入点，脚本会拒绝不匹配的结构。

4. 安装构建产物 `apps/web/dist/index.html`，或使用上述 Release 中的 `management.html`。ZIP 提供许可证、构建信息、校验值与 `verify_cpamp_panel.py`；如当前部署版本不匹配，应在对应源码上应用适配后自行构建。
5. 按下节核验实际服务页面，再强制刷新浏览器。API key 账号直接在原生凭证列表、卡片或详情额度页点击「刷新额度」，无需重新添加凭证。
6. OAuth 认证按「远程认证步骤」运行浏览器电脑本地接收器。

### 使用现有 Manager Server 托管页面

CPAMP 支持 `PANEL_PATH` 环境变量覆盖内置面板，也支持配置 JSON 的 `panelPath` 字段。可安装自定义页面，无需重新构建 Manager Server。先核验 ZIP 的 `.sha256` 和包内 `BUILD.json` 的页面校验值，将 `management.html` 放到自己选择的持久化位置。

**直接运行二进制**：在启动 Manager Server 的同一环境设置 `PANEL_PATH`，随后用现有启动命令及参数启动程序。以下路径是占位符，需要换成实际文件绝对路径：

```bash
export PANEL_PATH='/实际绝对路径/management.html'
# 使用现有命令启动 Manager Server
```

systemd 部署应在实际 Manager Server 的 service override 中写入 `[Service]` 下的 `Environment="PANEL_PATH=/实际绝对路径/management.html"`，执行 `daemon-reload` 并重启该服务。在另一个终端 export 不会改变已运行进程。Windows 手动启动用 `$env:PANEL_PATH = 'C:\实际路径\management.html'`，再在该 PowerShell 中使用原启动命令。现有 JSON 配置也可设置 `panelPath`；环境变量优先。

Manager Server 运行用户必须能读取页面。**配置文件打不开时服务会静默回退到内置页面**，因此不能只凭重启或本地文件存在判断成功。安装后运行包内检查脚本：

```bash
python3 verify_cpamp_panel.py https://cpa.gdp.pw/management.html --expected '/实际绝对路径/management.html'
```

应显示 `native_quota: PASS`、`native_window_history: PASS`、`native_callback: PASS`、`post_receiver_instructions: PASS` 和 `expected_page: MATCH`。脚本只读且不需要管理密钥。未通过时检查运行进程的 `PANEL_PATH`、读取权限、代理目标和缓存；确认后再强刷浏览器。

Docker Compose 示例仅表示需要合并到现有 **Manager Server 服务**的两项；服务名和宿主目录应替换为现场实际值，并保留原有镜像、环境变量、端口和数据卷：

```yaml
services:
  manager-server:
    environment:
      PANEL_PATH: /opt/cpamp-panel/management.html
    volumes:
      - ./custom-panel:/opt/cpamp-panel:ro
```

将页面保存为 Compose 项目目录下的 `custom-panel/management.html`，然后重新创建对应 Manager Server 容器使环境变量生效。CPA 的插件动态库安装到 CPA 容器，页面挂载到 Manager Server 容器。

保持页面和 Manager Server 的 CPAMP 源码版本一致。安装后清理代理/浏览器缓存并强制刷新。回滚时恢复原 `PANEL_PATH` 配置或原页面文件，再重启 Manager Server；此过程不需要迁移管理数据库。

## 远程认证步骤

1. 下载**浏览器电脑平台**的 ZIP。Windows 运行 `commandcodebridge-login.exe`；macOS/Linux 终端运行 `./commandcodebridge-login`（必要时先 `chmod +x commandcodebridge-login`）。接收器会打开 `http://127.0.0.1:45879`。服务器是 Linux、浏览器电脑是 Windows 时，需要另下载 Windows ZIP 取出 `.exe`。
2. 在新版 CPAMP 的「OAuth 登录」选择 CommandCodeBridge 并重新开始登录，复制卡片上的官方「授权链接」。卡片的「打开本地接收器」按钮可打开已经启动的接收器页面。
3. 把授权链接粘贴到接收器，点击「打开官方授权」。接收器保留本次 state 并跳转官网。
4. 完成官网授权。官网向本地 `/callback` 表单 POST，接收器跳转到只带 state 的完成页。点击「复制认证结果」。该结果包含凭证，是传递字符串，不要打开或分享。
5. 回到 CPAMP 原生卡片，将结果粘贴到「认证结果」并提交，等待状态轮询确认成功。接收器无需 CPA 管理密钥；认证结果由当前 CPAMP 管理登录提交给远端 CPA。
6. 关闭本地接收器，在原生凭证页点击「刷新额度」。若 state 过期，重新从第 2 步开始。

loopback 指浏览器所在电脑。CPA 不需要监听 `45879`，无需开放端口、配置 SSH 隧道或设置 `public_base_url`。官方授权链接使用 `127.0.0.1`，不能替换为远端服务器域名。裸 callback 地址无法恢复已丢失的 POST 正文，需要重新登录。

面板 POST 使用当前选中的 CPA 地址、代理子目录和管理凭据；复制结果只作为请求正文提交。独立插件登录页也支持接收器结果和手动 Studio key 回退。

## 数据和边界

- 原生发现：`GET /v0/management/quota/providers`；查询：`POST /v0/management/quota/fetch`，请求仅需要当前凭证的 `auth_index`。
- 上游通过 CPA 宿主传输策略查询现有 `/alpha` 接口，复用插件采样缓存；每次显式刷新至少间隔十秒。
- 原生凭证额度区月额度固定占据右侧：有真实 `monthly` 窗口时优先显示其百分比；否则用本次查询的 `monthly_credits` 除以已核实月上限。真实上游月 cap 优先，未提供时采用下方官方套餐映射。未知套餐或余额缺失时显示「—」；重置时间取真实订阅周期结束时间。月余额显示至小数点后三位，不计入购买或免费 credits。
- 月上限依据 [官方 Pricing & Limits](https://commandcode.ai/docs/resources/pricing-limits) 和官方 `command-code@1.81.0` 的 `getPlanTotalCredits` 表（2026-10-13 核实）：`individual-go` / `individual-go-v1` 为 10、`individual-goat` 为 70、当前 Pro `individual-pro-v1` 为 80、Max 10× `individual-max` 为 150、Max 20× `individual-ultra` 为 300 credits。旧 Pro `individual-pro` 保留官方 CLI 的 30；两种 Pro 均显示 Pro，两种 Max 均显示 Max，但计算保留原始 ID。Max 标准／高级模型限额独立，不能相加。5h 和周额度继续采用真实上游比例。
- 打开页面不主动访问上游；点击刷新才查询。网格、列表和详情共享当前内存结果，不把账户余额写到浏览器存储。切换 CPA、管理登录或凭证版本会隔离旧结果。
- 查询失败保留上次观察并标记过期；部分成功只发布本次成功的字段。单个模型无权限不影响账户额度查询。
- CommandCode 已接入原生额度刷新、套餐与余量状态、排序/筛选、详情窗口卡片、历史窗口查询及原生快照持久化。列表仍使用简洁的左右两区布局；详情使用 CPAMP 的标准窗口卡片，显示上个窗口、当前窗口的请求数、tokens、预估花费和成功率，以及符合原生条件时的窗口预测。
- 原生额度查询成功后补充读取插件现有的脱敏凭证列表，取得实际观察时间和订阅起止时间；兼容已安装的 v0.2.7 插件。5h/周窗口起点由真实重置时间减去对应窗口时长得到，月窗口取真实订阅周期。缺少真实 cap 且套餐不在已核实映射时百分比为「—」；上个月的边界仅采用已记录的周期。没有匹配历史时使用原生空值展示；花费为 CPAMP 历史请求的价格估算，不将 CommandCode credits 换算成美元。
- 额度缓存按 CPA 地址、管理登录和凭证版本隔离，只保留内存状态；失败结果不会作为新快照写入，不使用过期比例预测。CPA 返回的 `missing provider baseURL` 属于本地路由配置错误，即使其 HTTP 状态是 401，也不能据此要求重新登录；历史失败记录保留在诊断页。
- 持久化周期需要 Manager Server 的 `quotasnapshot.validProviders` 接受 `commandcode`。适配脚本同时修改前端查询名单和服务器名单，并复制 SQLite 持久化及凭证隔离回归测试。仅替换 HTML 可显示实时额度和历史请求；完整快照功能需按当前服务器的同一源码版本重建 Manager Server、备份二进制后替换并重启。此补丁不修改数据库结构；先在 `apps/manager-server` 运行 `go test ./internal/service/quotasnapshot -run TestCommandCode -count=1`。v0.2.9 包内提供源码适配与测试，未提供替换 Manager Server 的通用二进制。
- 原生登录 state 十分钟过期、单次消费；key 先经只读 whoami 验证，再由 CPA 保存到 auth-dir。手动回退接口需要管理鉴权。
- 官方回调凭证在 POST 正文。接收器只监听 IPv4 loopback，校验官网 Origin、Host、本次 state、重复字段、有效期和单次消费；结果仅存内存，十分钟后清除，进程十五分钟自动退出。不保存文件、不向远端自动转发、不读取 CPA 管理密钥。
- 接收器的复制结果包含 API key，请勿分享。浏览器地址栏只含 state；接收器与 CPAMP 输入框遮蔽结果。CPAMP 提交成功后清空，不写入浏览器存储；远端提交使用 POST 正文。插件校验 loopback 地址、固定路径/端口、state、重复参数、有效期和单次消费，API key 先经只读 whoami 验证。

## 回滚

恢复备份的管理页面和插件库，重启 CPA。新增凭证仍为正常 `type: commandcode` 文件，不需要修改现有模型映射或迁移数据库。

## v0.2.10 源码能力检查

开发中的适配脚本增加页面能力元信息，并给 Manager Server 的匿名 `GET /health` 添加 `capabilities.commandcode_quota_snapshots_v1` 布尔值，取自实际 `quotasnapshot.validProviders` 名单。该检查不读取或写入用户快照；旧服务器未声明能力时插件显示「未确认」。

在匹配源码上先运行 `apply.py --check`，再应用；全部插入点检查通过后才写入，重复应用安全。报错会列出缺失文件或不匹配插入点及已验证基线，不能据此认为任意上游版本均兼容。后端验证命令：

```bash
cd /path/to/CPA-Manager-Plus/apps/manager-server
go test ./internal/service/quotasnapshot ./internal/http/controller/health -run TestCommandCode -count=1
```

页面更新与后端更新分别生效。仅复制 HTML 无法新增后端健康能力；需按现有部署方式重建、备份并替换 Manager Server 后重启。已支持快照但尚无新健康声明的后端仍可工作，只是诊断显示未知。发布打包要求独立的上游 Git checkout 与明确提交，嵌套在本仓库中的源码归档只供本地验证，不能作为发行来源证明。
