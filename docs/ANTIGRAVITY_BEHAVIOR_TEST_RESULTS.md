# Antigravity 行为测试执行记录

日期：2026-10-23。环境：Windows、本地隔离进程、Playwright Chromium。仓库基于 main `032a295`；CPAMP 源码基线由 `internal/bridge/compatibility.json` 读取。

## 结果

| 验证 | 结果 | 证据 |
| --- | --- | --- |
| 新增真实 HTTP 请求行为 | **19/19 通过** | `antigravityBehavior.test.ts`，Vitest 4.1.5；未 mock 请求、订阅或 API 客户端 |
| 原有前端回归 | **11 文件、151/151 通过** | 沿用 CI 显式列表 |
| 前端类型检查与生产构建 | **通过** | `tsc --noEmit`、Vite production、`package_cpamp.py` |
| 原有面板身份与发布防线 | **6/6 组通过** | `python -m unittest discover -s tests -p test_cpamp_panel_validation.py -v` |
| Manager CommandCode 快照及健康声明回归 | **通过** | `go test ./internal/service/quotasnapshot ./internal/http/controller/health -run TestCommandCode -count=1` |
| 本次生产 ZIP 的浏览器行为 | **8/8 场景通过** | `dist/antigravity-current/summary.json`，逐场景 JSON 与截图 |
| 源码 Hub 版本降至 2.22.0 | **成功检出** | `dist/antigravity-mutation/full.json`；实际 UA 为低版本，窗口契约报 `missing_gemini_window` |
| 真实 CPA 安装内嵌页面后的行为 | **通过** | `dist/antigravity-installed/summary.json`；实际安装文件与公开 v0.2.11 ZIP、Manager HTTP 页面字节一致，随后相同文件通过隔离 Manager 行为测试 |
| 真实 CPA v8.0.23 HTTP 转发 | **2/2 通过** | `dist/antigravity-forwarding.json`；分别用 `User-Agent` 和 `user-agent` 检查最终 HTTP 头和 `$TOKEN$` 替换 |
| Workflow YAML、依赖链、Python 语法、diff 空白检查 | **通过** | 本地解析与检查 |

浏览器 8 个场景：

1. 完整 Hub 响应：两个账号分别匹配 Gemini / Claude 的 5h 与周额度。
2. 实际传输边界改为 CLI UA：检出 `missing_gemini_window`。
3. 实际传输边界改为 Hub 2.22.0：检出 `missing_gemini_window`。
4. 删除实际 UA：检出 `missing_gemini_window`。
5. 正确 UA 但 summary 仅有 Claude：检出 `missing_gemini_window`。
6. 删除 bucket.window：检出 `missing_window_duration`，不能用 label 推断掩盖缺失。
7. models-only：持久化 `unknown`，不虚构 5h / 周边界；重启恢复正确。
8. 额度查询 429：停止回退，没有成功快照写入。

负向场景的“通过”表示明确检出了指定缺陷。登录失败、超时、构建失败和普通异常不会被当作检出成功。

## 三层链路的实际证据

- 请求层捕获的是 `/api-call` JSON 的 `header`，并独立验证版本下限、endpoint、auth index、project、订阅请求和所有被调用的回退 URL。
- 正式页面在正常登录后点击“刷新额度”，检查每个账号的四个窗口比例及重置提示；继续正常打开“额度”详情，触发页面实际快照 POST。
- 快照通过真实 Manager HTTP 路由写入 SQLite。查询结果与 `account_quota_windows` 的账号、窗口 ID、窗口类型和模型 scope 一致。
- 完整响应用例验证 Gemini 80% / 60%、Claude 40% / 20%；另一账号分别减 10 个百分点。5h / 周时长为 18,000 / 604,800 秒，周期结束时间匹配夹具。
- Manager 重启后查询值、原始观察时间及页面恢复值一致；恢复期间假上游拒绝新抓取，并断言没有新增额度请求。
- 两账号及同名文件不同 auth index 分别覆盖；后者没有借用已有账号的持久化窗口。
- 低版本源码变异使用临时生产构建，真实请求发出 `antigravity/hub/2.22.0 darwin/arm64`，即使页面包含 Hub scheme，窗口完整性检查仍将其拦截。正式构建未被改写。

## 实现文件

| 文件 | 用途 |
| --- | --- |
| `integrations/cpamp/antigravity-behavior-fixtures.json` | 共享响应夹具、独立版本下限和窗口期望 |
| `integrations/cpamp/antigravityBehavior.test.ts` | 实际 HTTP 序列化、解析、窗口和快照构造测试 |
| `integrations/cpamp/vitest.behavior.config.ts` | 关闭演示模式，使用 production alias；测试位于 `behavior-tests/`，避免 Node 类型影响浏览器代码 |
| `integrations/cpamp/apply.py` | 向独立 CPAMP checkout 安装测试配置和夹具，重复应用验证通过 |
| `tests/cpamp_antigravity_behavior.py` | Python Playwright、假 CPA、真实 Manager、SQLite、恢复及故障控制 |
| `tests/cpamp_antigravity_mutations.py` | 隔离源码降版构建并要求精确语义失败 |
| `tests/cpa_antigravity_forwarding.py` | 真实 CPA 到 loopback 接收器的 HTTP 头与 token 替换契约 |
| `tests/cpamp_native_panel.py` | 安装后验证页面一致，再对实际安装的页面执行行为主场景 |
| `tests/requirements-cpamp-behavior.txt` | 锁定 Python Playwright 及依赖版本 |
| `.github/workflows/build.yml` | 行为测试成为构建、发布依赖链中的必经步骤，始终上传诊断证据 |

使用 Python Playwright 代替最初规划中的 Node spec，遵循 `webapp-testing` 技能；上游 npm 依赖未改变。诊断输出使用脱敏 JSON、失败截图与 HTML；未开启可能记录认证正文的浏览器 trace。

## CI 与执行边界

已接入：

```text
cpamp-web：类型检查 → 请求行为 → 原有回归 → 正式构建 → 低版本源码变异 → 打包
    ↓
test：原有宿主测试 → 实际安装页面行为 → 生产页面/SQLite/故障控制 → 真实 CPA 转发
    ↓
四平台 build → release-checksums → publish-release
```

本轮代码尚未 commit / push，因此 **没有本轮 GitHub CI 运行结果，也没有合并或发版**。本地检查通过不能表述为 GitHub CI 已绿。

测试使用虚构凭证和临时数据库。浏览器仅允许本次 loopback 服务；假管理端不访问 JSON 中的 Google URL。真实 CPA 的后台外部连接尝试被本地拒绝代理截获；真正的 UA 转发目标为 loopback。未访问真实 Google 额度服务或任何 VPS。

目前验证的是已知协议和事故模式；Google 将来改变接口、账户权限或客户端最低版本，仍需要新的契约依据与实测。
