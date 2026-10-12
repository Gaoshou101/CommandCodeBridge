# 开发验证记录

## v0.2.5 CPAMP 二进制面板一键安装（2026-10-12）

- 核对上游 `bin/install-cpamp.sh`：官方默认安装目录为 `${HOME}/cpa-manager-plus`，活动 runtime 标记写入 `run.sh`，配置位于 runtime 二进制目录；直接运行原生包由程序默认读取二进制旁 `config.json`。
- 插件内置 gzip 压缩的同版本 CPAMP 页面，添加目录/配置/页面路径配置项、控制台安装和检查按钮。Linux 优先探测实际进程及必要的路径环境；否则查默认目录与活动 runtime，多实例不猜测选择。
- Go race/vet 与安装回归通过：修改路径持久化、配置其他字段保留、原文件私有备份、幂等重复安装、非法路径/符号链接拒绝、并发修改拒绝、写入失败回滚、实际 HTTP 内容检查。
- 本地真实 CPA v8.0.23 与官方 CPAMP v1.14.0 Linux 二进制隔离联测通过：运行实例探测、内置页面安装、配置备份、首次重启后 HTTP 字节一致，已有 `PANEL_PATH` 的文件替换可立即生效。使用虚构密钥，不访问目标服务的私有数据。
- CI 先构建 pinned CPAMP 页面，再嵌入四平台插件；统一校验要求所有原生包的内置页面摘要与独立前端包一致，并随原生包附 CPAMP 许可证。

## v0.2.4 官方 POST 回调和二进制面板部署修正（2026-10-11）

- 重新核对官方 CLI 1.79.2 的 `handleBrowserCallback` / `handleLegacyJsonCallback`：`mode=redirect` 使用表单 POST，legacy 使用 JSON POST；凭证不在地址栏中。v0.2.3 的纯 URL 复制方案基于错误假设，先前模拟 URL 通过不能证明官网流程可用。
- 新增浏览器电脑本地接收器，监听 `127.0.0.1:45879`，接收官方表单/JSON POST，再生成供 CPAMP 管理 POST 提交的复制结果。表单完成页 303 跳转只带 state；结果不写日志或文件，十分钟后清除，进程最多运行十五分钟。
- 本地 Go race/vet 通过；接收器覆盖表单/JSON、CORS/PNA、state/有效期/重复字段/重放、拒绝、DNS rebinding 和裸 GET。真实 Linux CPA v8.0.23 验证「官方表单格式 → 实际本地接收器 → 受保护管理接口 → CPA 凭证保存及重启恢复」，使用虚构凭证和本地上游。
- CPAMP 五个相关测试文件共 27 项通过，类型检查及单文件生产构建通过；原生卡片显示接收器说明、「认证结果」输入框和「打开本地接收器」入口。额度组件和真实字段规则保持原有实现。
- 只读检查目标 `https://cpa.gdp.pw/management.html`，仍缺少原生额度与回调标记。用户确认 Manager Server 直接运行二进制；补充启动环境 `PANEL_PATH`、文件权限、内置页面回退行为与实际 HTTP 内容检查脚本。
- 发布产物包含四平台接收器与页面检查脚本，CI 在四平台运行本地接收器到 CPA 的联测并验证包内校验。真实官网登录与目标面板安装仍由用户现场验收，未修改目标服务。

## v0.2.2 原生认证与额度适配（2026-10-11）

- Linux amd64 的真实 CPA v8.0.23 加载插件：`commandcode-auth-url` 成功、pending 轮询、受保护手动完成、官方 redirect 形状回调、CPA 保存 auth-dir、重启加载均通过；未使用真实账号或付费请求。
- `/quota/providers` 正确发现 CommandCode；`/quota/fetch` 返回套餐、45 credits 总余额、40 credits 月余额、10 credits 本期月用量、5 小时 60% 和周 70% 余量。上游未提供月 cap 时不产生月百分比。
- Go race / vet、原有 25 次原生协议生成、异常恢复套件通过；新增登录有效期、state 重放、拒绝、无效 key 脱敏、公开地址、额度失败/部分失败与未知字段回归。
- 11 项浏览器管理登录存储测试通过，新增登录页脚本语法检查通过。
- CPAMP 源码基线 `dbf61ddf438f399558722df3467355c42af9005d` 的原生卡片、列表与详情页补丁已完成 TypeScript 类型检查、生产单文件构建；11 项卡片、OAuth URL 及原有详情/API 回归测试通过。
- 浏览器登录隔离 CPA 面板，实测原生列表刷新返回 pro、45/40 credits、5 小时 60%、周 70%、真实重置日期和「月上限未知」；切换卡片视图复用本次观察。修正列表刷新按钮嵌入详情触发按钮的问题。详情额度组件已通过测试；浏览器连接恢复被安全策略拦截，未完成详情页截图验证。
- 发布前复核已修复 Windows CI 原生测试退出码未检查，以及套餐名缺失时缓存套餐被发布为新观察的问题；补充缺少 planId 的上游回归。四个平台和面板的发布构建由 GitHub Actions 执行，统一校验五个安装包及来源提交。
- 此轮未部署目标 CPAMP，未完成真实 CommandCode 网站授权。当前 CPAMP 需要前端补丁才能显示插件额度，不能把 CPA 额度接口已实现等同于线上面板已更新。

## 工作台布局二次调整（2026-10-11）

- 参考指定官方用量页公开的工作台组件、CSS 变量和 SVG 图标；未认证访问返回登录页，未读取用户的官方账户数据。
- Chromium 使用当前源码内嵌的 HTML/CSS/JS 与本地虚构 API 数据，验证登录/刷新恢复、健康度、月额度、账号切换/编辑/停用状态、日志展开/搜索/筛选、主题、模型别名/导入、设置保存和自定义统计时间。
- 5 个页面在 11 种宽度（375–1440px）完成 55 组检查，页面无横向溢出或脚本错误；桌面侧栏收起、手机导航抽屉及 Escape 关闭通过。
- `node --check internal/bridge/ui/console.js`、10 个登录适配测试通过；查看明暗主题、概览、用量、日志和手机截图后调整数字排版。
- 本轮仅调整 UI 资源。本机 WSL 调用无响应，已取消本轮动态库重编译；下节原生宿主与 Go 验证结果属于上一轮功能实现，不代表本次布局已重新完成原生宿主联测。

## 管理界面验证（2026-10-11）

- Go 竞态测试与 `go vet` 通过；新增健康状态、同账号模型冷却、旧密钥记录隔离、默认别名和月度字段解析回归场景。
- 10 个 CPA/CPAMP 登录适配测试通过，控制台脚本语法检查通过。
- WSL Linux amd64 的 CPA v8.0.23 加载本轮本地动态库，完成 25 次模拟成功请求及 401/403 场景；确认新别名、真实字段月消耗、健康状态及内嵌 CSS/JS/SVG 资源。
- 异常恢复套件通过：429/别名/同账号冷却、503 换号、SSE 中断、超时、取消、重启持久化和记账。
- Chromium 验证控制台登录、3 类凭证状态、月额度显示、日志展开与失败筛选、官方图标、别名自动填写/自定义保留、明暗主题及 390px 手机布局，无脚本错误或页面横向溢出。
- 新增月窗口与健康度的本轮运行使用虚构 key 和本地模拟上游；真实账户字段参考此前脱敏响应，未进行付费生成。

## 源码基线

- 2026-10-11 创建私有仓库 `Gaoshou101/CommandCodeBridge`。
- 初始提交：`9a43db5`，版本 0.1.0，C ABI 1 / RPC schema 6。
- 基线通过 Go 竞态检查、go vet、10 个登录适配测试，以及 Linux/Windows CPA v8.0.23 的 25 次成功模拟请求和 401/403 场景。
- 初次 GitHub Actions：运行 `38102314575` 全部成功，完成 Linux amd64/arm64、Windows amd64、macOS arm64 构建。此轮 ARM64/macOS 只构建，尚未在该轮运行宿主联测。
- 后续 [CI 38103638587](https://github.com/Gaoshou101/CommandCodeBridge/actions/runs/38103638587) 全部成功：Go 竞态检查、go vet、10 个登录测试，及 Linux amd64/arm64、Windows amd64、macOS arm64 的 CPA v8.0.23 原生宿主集成与异常恢复测试。四个平台均已完成实际宿主加载与调用。
- 四平台 ZIP 的 SHA256、`BUILD.json` 动态库校验、干净构建状态及来源提交已核验；此轮两个 Linux 包的 glibc 最低要求均为 2.34。正式发布使用主分支 CI 产物，并再次核对版本及来源提交。

## 真实账户

- 只读身份、余额、套餐、5 小时/周窗口、到期时间及请求数已验证；公开模型目录返回 87 个模型。
- 真实用量汇总字段为 `totalCount`。插件已修正，并兼容较旧的 `totalRequests`；匿名字段样本位于 `internal/bridge/testdata/account-summary.json`。
- 当前验证账户套餐为 `individual-goat`；GPT-5.4 Mini 和 Claude Haiku 4.5 返回明确的 `MODEL_NOT_IN_PLAN`，目录可见不能视为账号可用。
- 免费模型 `inclusionai/ling-3.1-flash:free` 已在 Windows CPA v8.0.23 完成 6 次真实请求：Chat/Responses 流与非流，以及工具调用和结果回传。共 864 tokens，插件与 CPA 的实际 usage 一致。
- Claude 原生 Messages、思考签名、图片和 ZDR 的真实验证仍待具备对应权限/能力的模型；已有模拟覆盖不代表真实验证。
- 免费模型曾触发 429；后续联测应遵守上游冷却，避免快速重复启动整套生成测试。
- 本机 Python 使用现有 Windows 系统代理，隔离 CPA 需明确设置全局代理。真实测试脚本提供 `--system-proxy`，将该设置仅应用于测试 CPA。
- 真实 key 和原始账户响应保存在仓库之外，源码与模拟夹具不含该 key。

## 异常与恢复

`tests/cpa_reliability.py` 通过实际动态库和官方 CPA 宿主验证：

- 429 / Retry-After、同模型别名共享冷却、同账号多 key 共享冷却、到期恢复。
- 503 在多个凭据间切换；不完整 SSE 的非流式错误与流式中途错误。
- 请求超时与客户端取消后释放上游连接。
- 会话重新分配、停用和备注在宿主重启后保留。
- 模型映射、日志与统计在重启后恢复，恢复后的请求正常记账。

所有异常测试使用本地模拟上游和虚构凭据。

CPA 的失败队列按上游尝试记录；503 换号可能产生多条失败记录。新增断言按实际尝试数核对，未报告 usage 的失败不得虚构 tokens。

## 持久化优化

原实现每次记录都在服务锁内序列化并写入日志/统计 JSON。当前实现：

- 单个写入任务每 250ms 合并一次变更，无逐请求写入队列。
- 在锁内取得快照，序列化和磁盘写入在服务锁外完成。
- 写入失败保留待写状态并重试，管理状态返回 `storage_error` 与 `storage_pending`。
- 正常卸载先等待活动请求完成，再执行最终刷盘。
- 统计清空与磁盘快照串行，旧快照不能覆盖清空结果；旧请求的日志保留，计数按统计 epoch 处理。

正常刷新间隔为 250ms；强制结束进程或断电可能丢失尚未写入的记录。磁盘故障时不能保证这一时间窗口，错误状态会保留。双 JSON 文件不是跨文件事务。

### 前台记录基准

WSL Linux amd64，Go 1.27.2，i5-12600KF，完整保留缓存，每组 1 秒：

| 日志保留量 | 原实现 | 批量写入 |
| --- | ---: | ---: |
| 1,000 | 2.820ms/op，约 2.10MB 分配/op | 0.522µs/op，约 1.15KB 分配/op |
| 10,000 | 22.853ms/op，约 17.78MB 分配/op | 0.769µs/op，约 1.79KB 分配/op |

这是前台记录调用的开销；新实现将序列化与 IO 转移至后台，不能将此倍率直接当作端到端吞吐提升。

### 实际 CPA 并发基线

`tests/cpa_benchmark.py --requests 64`，同一机器、默认保留 1,000 条，模拟 SSE 固定分片延迟，监听队列 128：

| 并发 | 原请求/秒 | 新请求/秒 | 原 P95 | 新 P95 | 原磁盘写入 | 新磁盘写入 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 18.42 | 20.04 | 57.34ms | 50.87ms | 39.54MB | 8.66MB |
| 8 | 98.62 | 138.68 | 100.22ms | 60.52ms | 39.54MB | 1.86MB |
| 32 | 130.95 | 212.41 | 281.01ms | 182.21ms | 39.54MB | 1.25MB |

短时模拟结果用于比较两种持久化方案，不代表真实 CommandCode 的速度或生产环境容量。

## 复现

```bash
go test -race ./...
go vet ./...
node --test tests/cpa-auth.test.cjs
CPA_BINARY=/path/to/cli-proxy-api python3 tests/cpa_e2e.py
CPA_BINARY=/path/to/cli-proxy-api python3 tests/cpa_native_management.py
CPA_BINARY=/path/to/cli-proxy-api python3 tests/cpa_reliability.py
CPA_BINARY=/path/to/cli-proxy-api python3 tests/cpa_benchmark.py --requests 64
go test ./internal/bridge -run '^$' -bench BenchmarkRecordAtRetention -benchmem
```

Windows 设置 `BRIDGE_LIBRARY` 为实际 `.dll`；macOS 设置为 `.dylib`。真实测试默认只读，必须显式提供仓库外的凭据文件；`--generate` 才启用少量真实请求，永不进入 CI。

## 部署状态

目标 CPAMP `https://cpa.gdp.pw/` 页面可访问，当前页面要求 Manager Server 管理员密钥，它与 CPA Management Key 不同。用户已提供管理凭据并明确由其自行安装；本次交付源码、四平台安装包及安装/升级/回滚说明，未修改目标服务。现场代理、目录权限和 CPAMP 全链路验收留待用户安装后完成。

## GitHub 链接安装

- 2026-10-11 按用户要求将仓库改为公开，匿名读取仓库、latest Release 和 `checksums.txt` 通过。
- CPA v8.0.23 要求 ZIP 名称为 `<插件ID>_<版本>_<GOOS>_<GOARCH>.zip`，且 Release 必须含统一 `checksums.txt`。v0.1.0 新增四平台兼容命名别名，与原发布 ZIP 字节一致；九个新增资产的 GitHub SHA256 与本地值一致。
- 提供公开 `registry.json`，通过 `plugins.store-sources` 加入自定义插件源。当前未登记至官方商店。
- [CI 38113046358](https://github.com/Gaoshou101/CommandCodeBridge/actions/runs/38113046358) 通过四平台构建/宿主联测及统一校验文件生成。
- `tests/cpa_store_install.py` 在隔离 Windows CPA v8.0.23 中通过：公开源发现、匿名 Release 获取、选包、校验安装、配置登记、重启后加载和插件状态确认。测试移除 GitHub Token 环境设置，仅使用虚构 CPA 管理密钥，未发模型请求。
- GitHub 安装器只写入动态库；目标主机的持久化目录、CPAMP 代理及真实凭据仍由用户安装后验收。

## v0.2.3 远程 localhost 回调修复

- 更正：此版本只模拟 URL 查询参数回调，未覆盖官网实际 POST 正文。地址栏复制方案不完整，已由 v0.2.4 的本地接收器补齐。
- 用户截图确认官网拒绝远程公开域名 callback；官方 CLI 1.79.2 使用 loopback `/callback` 和 `mode=redirect`。
- 对照 CPAMP Codex/Claude 的原生回调提交与当前 CPA `oauth_callback.go`：通用接口只读取 `code/state/error`，不会交付 CommandCode 的 `apiKey`。新面板复用原生卡片、请求作用域和状态轮询，经插件管理 POST 提交完整 URL 与当前 state。
- 本地 Go race/vet、原生 CPA v8.0.23 登录/额度/协议/异常恢复测试通过。远程提交验证管理鉴权、错误 state、重复提交、取消、凭证重启持久化和日志脱敏。前端新增原生卡片交互与接口回归测试。
- 自动化使用虚构凭证与本地上游，不访问用户账户，不修改目标 CPAMP。用户真实官网授权及现场代理验收待升级后测试。
