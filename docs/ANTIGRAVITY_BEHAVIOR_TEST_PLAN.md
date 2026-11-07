# Antigravity 额度行为测试计划

日期：2026-10-23。状态：已实现并完成本地验证；CI 门禁已接入，尚未推送触发 GitHub CI。结果见[执行记录](ANTIGRAVITY_BEHAVIOR_TEST_RESULTS.md)。

## 1. 目标与验收边界

围绕 PR #13 的事故补齐行为证据：执行实际请求代码，捕获发送的 UA，再验证 Gemini 和 Claude 的额度窗口经过解析、页面展示及 SQLite 持久化后仍然正确。

新增测试必须证明：已知的旧 UA 回退、请求头被覆盖、窗口元信息丢失会让 CI 失败。固定响应测试无法保证 Google 将来不改变接口；真实账号是否拥有 Gemini 额度也不能由测试夹具推断。

实现全部在 CommandCodeBridge 仓库维护，沿用独立临时 CPAMP checkout 应用集成文件的方式。测试未修改上游仓库或线上环境。

## 2. 已核查的现状

依据本地 main `032a295` 及 `dist/cpamp-reference` 中匹配的 CPAMP 源码：

| 链路 | 现有能力 | 新增覆盖点 |
| --- | --- | --- |
| 源码与打包 | `compatibility.json` 决定基线，公共校验拒绝旧 UA scheme | 验证执行中的请求头与独立版本下限 |
| 请求 | `providerRequests.test.ts` mock 整个 `apiCallApi.request`；UA 与同模块常量比较 | 穿过真实 `apiCallApi`、`apiClient` 和 JSON 序列化 |
| 窗口 | 已有 builders、display windows、snapshots、matrix 的分层测试 | 将同一份请求结果贯穿整条链路 |
| 部署 | `tests/cpamp_native_panel.py` 已验证真实 CPA 安装、真实 Manager 服务页面及字节一致性 | 浏览器执行被安装的生产页面并刷新 Antigravity 额度 |
| CI | 显式列出 Vitest 文件；现有额度请求、builder 等测试未全部注册 | 显式注册新测试，作为 build/release 的前置条件 |

两个实现细节影响测试设计：

1. 应捕获 `POST /v0/management/api-call` **JSON 正文中的 `header['User-Agent']`**。浏览器访问 Manager 的 HTTP User-Agent 不代表转发给 Cloud Code 的 UA。
2. 当前 `fetchAntigravityQuota()` 遇到第一个可用的非空 groups 就返回。因此“summary 仅有 Claude”和“summary 均不可用后 models 回退”要分别复现；不能把两者写成必然连续发生的调用链。

上游 Vite 配置在 `mode === 'test'` 时启用演示模式和演示夹具。新行为测试需要明确关闭该模式，并断言本地测试服务确实收到请求。

## 3. 三层行为测试

### 第一层：真实请求代码 → 本地 HTTP 服务

技术：Vitest + Node loopback HTTP 服务。执行真实 `fetchAntigravityQuota`、`antigravitySubscriptionApi`、`apiCallApi` 和 `apiClient`；不 mock 这些被测函数。服务模拟 CPA 管理端，读取序列化后的 JSON。

服务按收到的 UA 选择响应：

- 合法 `antigravity/hub/<version> <platform>` 且版本不低于独立测试契约中的 `2.23.0`：完整 Gemini / Claude 响应。
- CLI、缺失、格式错误或低版本：裁剪响应，仅保留 Claude。
- 根据用例指定返回 404、429、空结构或 models-only 响应。

断言内容：

- 正确的 `authIndex`、POST 方法、目标 endpoint 和正文 `project`；逐一检查实际调用的 summary、models、loadCodeAssist 请求。
- UA 的 scheme、平台格式及数字版本比较；测试期望不导入生产 `ANTIGRAVITY_USER_AGENT` 常量。`2.9.0 < 2.23.0`，`2.100.0 > 2.23.0`。
- 头名称按 HTTP 语义忽略大小写；存在多个大小写变体且值冲突时直接失败。
- 保留管理请求与上游请求各自的 Authorization 语义；只使用虚构凭证。
- 将返回值继续交给真实的 display windows、window definitions、snapshot write builder，检查窗口集合和字段。

单独配置 `vitest.behavior.config.ts`，复用所需 alias / SCSS 设置，强制关闭演示构建及运行模式，避免普通 Vitest 配置的演示开关提前返回夹具。每个正向用例必须看到预期 HTTP 交换，未发生请求不能算通过。

### 第二层：生产 ZIP → 浏览器 → 正常操作

技术：Playwright Chromium；依赖独立放在本仓库测试目录并锁定版本。直接使用本次 `package_cpamp.py` 产出的 ZIP，不为浏览器测试重建演示页面。

隔离 harness 启动与该 ZIP 同源构建的真实 Manager，以临时 `PANEL_PATH` 服务包内 `management.html`；Manager 的 CPA upstream 指向 loopback 假管理端。正常登录、打开凭证页、点击刷新额度，使用虚构账号和管理凭证。

断言内容：

1. HTTP 服务页面的 SHA256 与 ZIP、`BUILD.json` 一致；不能只验证磁盘文件。
2. 假管理端实际收到额度请求，并按 UA 返回响应；不拦截页面内函数或填充前端额度 store。
3. 完整响应场景下，Gemini 与 Claude 各有 5h 和周额度，比例与重置时间来自本次响应。分别使用不同数值，避免两个模型组串用仍然通过。
4. 使用现有 `data-account-quota-matrix-row`、`data-account-quota-matrix-cell` 定位窗口，配合可访问名称触发操作；不依赖混淆变量、CSS hash 或整页截图比对。
5. 若最终选择的列表/网格布局不渲染 matrix，使用其现有窗口卡片语义并精确限定账号；不能为了让断言通过直接注入组件。

复用现有原生安装 harness 的启动、退出及页面校验逻辑：在 CI 已经安装内嵌页面的实例上增加一次主场景浏览器验收，覆盖“插件安装后的实际页面”。不重复整套安装兼容矩阵。

### 第三层：页面实际写入 → Manager → SQLite → 恢复

第二层 harness 保留真实 Manager 的 `/quota-snapshots` 与 `/quota-snapshots/query` 路由，禁止用浏览器 mock 响应替代。

- 捕获页面发出的快照 POST；等待 Manager 写入成功后调用真实 query 接口。
- 完整响应用例按 `authIndex`、provider、模型组、provider window ID 精确匹配四个窗口，而非仅断言总数大于零。
- 断言 `window_kind` 为 `five_hour` / `weekly`，时长为 18,000 / 604,800 秒，模型 scope、剩余比例和重置时间对应夹具；完整用例不得产生 Gemini `unknown` 窗口。
- 在隔离数据库中只读检查 `account_quota_windows`；数据库中的主键或聚合结构以当前 schema 为准，不依赖固定自增 ID。
- 刷新浏览器并重启临时 Manager 后再次 query；恢复值及原始观察时间不变。恢复阶段禁止再返回新额度，防止重新抓取掩盖恢复失败。
- 第二个虚构账号使用不同数值；相同文件名但不同 auth index 也要隔离。查询结果不能借用第一个账号的额度。

这一层优先通过真实进程和实际页面验证，不再为同一个映射添加重复的 Go 单元测试。若发现后端未覆盖的独立缺陷，再追加有针对性的 Go 回归。

## 4. 用例矩阵

四个主窗口使用独立夹具：Gemini 5h 80%、周 60%；Claude 5h 40%、周 20%。每次运行只选取一次观察时刻 T0，之后固定使用；周期边界和重置时间相对 T0 定义，响应 Date 与浏览器测试时钟一致。

| ID | 场景 | 必须成立的结果 | 执行层 |
| --- | --- | --- | --- |
| A01 | 合法 Hub，完整 summary | 四窗口完整；请求、UI、快照、DB 一致 | 1 / 2 / 3 |
| A02 | CLI / 低版本 / 缺失 / 错误 UA | 假服务产生裁剪响应；同一健康场景验收必须失败 | 1；CLI 与低版本另做浏览器故障注入 |
| A03 | 页面含 Hub 字样，但实际 header 被覆盖 | 仍捕获错误 UA；不能因页面字符串存在而通过 | 1 / 2 |
| A04 | 合法 UA，summary 只返回 Claude | 忠实保留 Claude；健康夹具的 Gemini 完整性检查失败；不擅自合成 Gemini | 1 / 2 |
| A05 | 第一个 summary 404，后续 summary 完整 | 按当前 URL 顺序回退；每次请求 UA 正确；四窗口正确 | 1 |
| A06 | summary 均不可用，models-only 可用 | 保留可证实的余量；不得凭模型名捏造 5h / 周边界，shared bucket 可为 unknown | 1 / 2 / 3 |
| A07 | summary 有组但 bucket.window 缺失 | 明确检查元信息；完整夹具验收失败，不用 label 推断掩盖 duration 丢失 | 1 |
| A08 | 额度 endpoint 返回 429 | 停止后续额度 URL 回退；无新的成功观察写入 | 1 / 2 |
| A09 | 订阅查询失败或返回 429，额度完整 | 保留四窗口；订阅及限流状态遵循现有契约 | 1 |
| A10 | 空 groups 与未知成功结构 | 保留现有 inventory observed 区别；不当作完整四窗口成功 | 1 |
| A11 | 刷新后 reload / Manager 重启 | 真实快照恢复，原观察时间不被改写，无新抓取掩盖结果 | 2 / 3 |
| A12 | 两个账号 / 同名文件不同 auth index | 各自请求、显示、快照及恢复不串号 | 1 / 2 / 3 |

“健康夹具验收失败”指测试中预先声明拥有四窗口的 fixture。正常业务允许合法账号只有部分分组，不新增“任何缺 Gemini 都是错误”的生产规则。

## 5. 证明防线有效：故障注入与定向变异

必须保留正向控制：相同页面、账号、project 及假服务，仅将 UA 或窗口字段改变，结果从通过变为明确的语义失败。

1. **低版本源码变异**：在临时构建副本中把 Hub 版本改为 `2.22.0`，仍保留 Hub scheme。构建后让相同浏览器场景验收失败，证明仅检查 scheme 无法绕过行为防线。
2. **实际 header 故障注入**：保持生产页面与 Hub 字符串不变，在测试传输边界覆盖实际送达假服务的 UA 为 CLI 或删除它。先证实错误请求确已送达，再检查完整窗口契约失败。
3. **窗口元信息变异**：从完整响应中删除 Gemini 或删除 bucket.window；即使 UA 正确，仍需分别由窗口集合、duration / snapshot 断言发现。

变异构建仅存在于临时目录，不进入正式 ZIP、内嵌目录或上传制品。若静态防线先拦截，也记录为静态检测，不能冒充行为层已经执行。

负向用例必须匹配明确的原因码，例如 `ua_below_minimum`、`missing_gemini_window`、`missing_window_duration`；异常退出、服务没启动、超时或登录失败不能算“成功杀死变异”。不为运行变异放宽发布校验。

## 6. 文件与依赖规划

以下保留最初规划。实际实现使用技能要求的 Python Playwright、锁定的 requirements 文件和独立 `behavior-tests/` 目录；文件对应关系见执行记录。

| 文件 | 职责 |
| --- | --- |
| `integrations/cpamp/antigravity-behavior-fixtures.json` | 单一测试夹具，包含响应、独立期望、相对 T0 的时间偏移及 UA 版本下限；跨 TS / Python / 浏览器共用 |
| `integrations/cpamp/antigravityBehavior.test.ts` | 第一层真实请求和解析→快照行为测试 |
| `integrations/cpamp/vitest.behavior.config.ts` | 非演示行为测试配置 |
| `integrations/cpamp/apply.py` | 校验目标结构后，将测试、配置与夹具复制到独立 CPAMP checkout；保持重复应用安全 |
| `tests/cpamp_antigravity_behavior.py` | 假 CPA 管理端、UA 敏感响应、真实 Manager 和临时 DB 生命周期 |
| `tests/cpamp-behavior/antigravity.spec.cjs` | 浏览器正常登录、刷新、窗口、快照、恢复验收 |
| `tests/cpamp-behavior/package.json` / `package-lock.json` | 隔离并锁定 Playwright 依赖，不改变上游依赖 |
| `tests/cpamp_antigravity_mutations.py` | 故障注入与隔离源码变异，分类失败原因和恢复正向控制 |
| `tests/cpamp_native_panel.py` | 暴露可复用生命周期，在已安装页面上执行一个主场景 |
| `.github/workflows/build.yml` | 注册测试、浏览器环境、强制失败门槛与证据上传 |
| `integrations/cpamp/README.md` | 本地执行入口、夹具及测试边界 |

测试独立维护 `2.23.0` 协议下限是防止生产常量与断言同时回退。它不复制 CPAMP commit，构建基线继续只从 `compatibility.json` 读取。将来提升协议下限须有单独的契约依据和评审。

## 7. CI 接入与实施顺序

**步骤一：夹具与第一层。** 增加非演示 Vitest 配置及 HTTP harness，优先 A01–A07，再补限流、空响应与订阅失败。将新文件加入 `cpamp-web` 显式执行列表；现有 constants 测试保留。

**步骤二：生产页面、持久化与安装后验收。** 在现有 `test` job 中，原生部署步骤后运行 Chromium 测试，复用下载的生产 ZIP、匹配 Manager 和临时 CPA 资源。所有进程使用临时目录及 loopback 随机端口。

**步骤三：故障注入与源码变异。** header / 响应注入属于每次主行为测试的一部分；低版本源码变异在 `cpamp-web` 内完成隔离生产构建。明确指定正常产物路径，避免后续打包误取变异页面。全部三步完成后才满足本计划验收。

CI 依赖保持：

```text
cpamp-web（源码行为 + 隔离变异 + 正式页面打包）
    → test（现有宿主 / 安装测试 + 生产页面行为 + SQLite 恢复）
    → build（四平台）→ release-checksums → publish-release
```

- 不使用 `continue-on-error`；断言失败不通过自动重试消除。
- UI 通过具体状态与网络事件等待，不使用固定 sleep 充当就绪条件；重启用例有明确的进程和服务就绪检查。
- `if: always()` 仅用于上传证据和清理资源，不能用于绕过依赖触发发布。
- 记录新增测试耗时后再设合理超时，不预先承诺尚未测量的运行时间。

## 8. 可复现性与失败证据

- 浏览器只放行本次创建的 loopback 服务，意外域名/端口访问立即失败。假服务按 endpoint 返回固定数据，不向 JSON 中的 Google URL 发起网络访问。
- 真实 Manager 的后台同步也必须通过配置或测试进程的网络限制保持本地可控；仅设置浏览器 route 不能约束后台进程。首次执行需验证进程外连边界。
- 夹具、浏览器和假服务使用同一 T0，报告记录 T0 以便复现。真实 Manager 使用系统时间，启动与写入应落在约定容差内，重置时间留足余量；不修改宿主系统时钟。新进程、新账号上下文和新 SQLite 文件隔离每组用例。
- 产出 JSON 报告：场景、实际 UA、请求 endpoint、窗口集合、snapshot 字段、DB 对照、页面 SHA256、CPAMP 基线及变异检测原因。
- 浏览器失败时保存 trace、截图和相关日志；屏蔽 Authorization、Cookie、管理密钥及 access token。全部使用虚构凭证，报告也不输出原始认证头。
- 临时进程和端口均在 finally 中回收；测试不读取用户浏览器状态或已有数据库。

## 9. 独立补充：真实 CPA 转发契约

上述三层证明生产页面将正确 UA 交给管理 API、Manager 保持窗口语义，假 CPA 并不能证明真实 CPA 最终向 Google 发出的 HTTP 头。

已增加 `tests/cpa_antigravity_forwarding.py`：用虚构 auth fixture 调用真实 CPA `/api-call`，目标为 loopback 接收器，检查接收器读到的真实 HTTP User-Agent 及 token 占位符处理。CPA v8.0.23 实测通过；后台的外部连接尝试被本地拒绝代理截获，不访问 Google。

此项单独报告覆盖状态。真实 Google 接口变化仍需未来另行安排授权的实测；本计划不引入定时监控或真实账号访问。

## 10. 完成标准

- [x] 完整夹具贯穿真实请求、生产页面、快照 HTTP、SQLite 和恢复，四窗口逐项匹配。
- [x] CLI / 低版本 / 缺失 UA 的负向控制均得到预期的语义失败，正向控制通过。
- [x] 页面含 Hub 字符串但实际 header 错误也会失败。
- [x] 缺 Gemini 与 models-only 分别覆盖，不虚构周期元信息。
- [x] 两账号及同名不同 auth index 不串数据，重启恢复没有重新抓取掩盖问题。
- [x] 演示路径未介入，预期 HTTP 交换确实发生，未访问真实上游。
- [x] 新测试接入 build/release 的必经依赖链；本地检查 YAML 与依赖通过，GitHub 运行结果待推送验证。
- [x] 已输出请求、快照、DB、截图及变异原因证据；真实 CPA 转发契约已补齐。

满足这些标准后，可表述为“已知事故模式有可执行的回归防线”；不能表述为“以后绝不会出现类似 bug”。
