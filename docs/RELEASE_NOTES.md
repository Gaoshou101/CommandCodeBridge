# CommandCodeBridge 发布说明

## v0.2.11

发布日期：2026-10-23。CPA 原生插件，C ABI 1 / RPC schema 6；调用官方 Provider API。

### 本次更新

- **修复 Gemini 额度窗口**：CPAMP 基线升级至 `55d452ef`（上游 #986），Antigravity UA 从 `antigravity/cli/1.0.13` 对齐到 `antigravity/hub/2.23.0`，避免上游裁剪 Gemini 额度组后回落为 `unknown` 窗口。
- **单一源码基线**：构建检出、构建标识、测试用 Manager 二进制与适配报错读取 `internal/bridge/compatibility.json`，清除 workflow 独立旧值。
- **构建、发布和部署防线**：共用身份、集成标识与来源校验；按 `antigravity/hub/` scheme 识别模板拼接，拒绝任何 `antigravity/cli/` 残留。覆盖前端／原生打包、嵌入、发布校验、实际 HTTP 页面及插件安装。
- **回归覆盖**：旧／新／模板／混合 UA、Git 基线与后代、错误来源和摘要、四平台 ZIP 一致性与发行包内核验脚本；CI 同时运行上游 UA 常量测试和匹配源码的真实 Manager 安装联测。

### 升级步骤

1. 更新插件至 **v0.2.11**，确认实际运行版本。
2. 在「设置 → CPAMP 原生面板」更新页面并检查生效状态；异机／独立容器使用 `commandcodebridge_0.2.11_cpamp-web.zip`。
3. 强制刷新浏览器并刷新额度，确认 Gemini 的 5h／周窗口。**只更新动态库不会替换已落盘 HTML。** 原路径页面更新无需重启，首次启用或切换路径仍需重启 Manager Server。
4. 手工部署可用包内 `verify_cpamp_panel.py URL --expected /实际路径/management.html` 核验，保留随包提供的 `integrations/cpamp/`。已有 CommandCode 快照后端补丁不因本次 UA 修复而需要重复部署。

### 下载与校验

四平台原生 ZIP、独立 CPAMP 前端 ZIP、五个 `.zip.sha256` 和统一 `checksums.txt`，共 **11 个资产**。四平台内嵌同一页面，`BUILD.json` 记录匹配版本、来源提交、兼容基线及摘要。CPA 支持基线仍为 v8.0.23；CPAMP 源码以包内兼容信息为准。

发布由标签 CI 执行，要求回归、协议与原生宿主联测、四平台构建和来源／摘要校验通过。本次不访问或修改 VPS，不进行真实模型生成；上游 UA 问题依据 #983／#986 与构建源码核验，未重复使用线上凭证调用配额接口。

## 已发布 v0.2.10

发布日期：2026-10-18。CPA 原生插件，C ABI 1 / RPC schema 6；调用官方 Provider API。

### 本次更新

- **凭证诊断**：保存与宿主加载分别确认，对照 CPA 运行登记检查缺失、重复、旧路由与启停同步；账户查询、插件直测和 CPA 请求分别显示来源与时间，支持脱敏摘要。
- **模型证据**：按凭证版本、账户、模型映射和时间隔离结果，区分套餐拒绝、未知权限、临时限流与过期证据；组合搜索与证据筛选、手机精简列表和详情抽屉。
- **数据恢复**：运行文件独立写入、检查点、损坏隔离及恢复警告；统计保留近期分钟桶、历史日总量，默认不删除日总量。采样保持逐凭证隔离并增加正抖动。
- **Studio 风格控制台**：参照官方公开截图统一五页导航、明暗主题、字阶、按钮和边框；凭据页优先展示额度，设置按任务分区，通用窗口支持键盘焦点循环、Esc 和关闭返回原行。
- **管理会话**：退出清空业务窗口、日志、详情与测试输出，丢弃旧身份的较晚响应；管理认证按 Bearer／X-Management-Key 命名。
- **升级检查**：分别核验插件接口、实际服务页面、额度／最近请求／周期历史标识和快照后端；未知能力保持未知。面板安装前预览目标、备份和重启条件。

### 升级与恢复

1. 备份整个插件 `data_dir`，在 CPA 插件商店更新至 **v0.2.10**，确认实际运行版本与凭证登记状态。
2. 在「设置 → CPAMP 原生面板」更新页面，检查生效状态并强制刷新浏览器；首次启用或改变页面路径需要重启 Manager Server，原路径页面更新无需重启。
3. 原有统计首次分层汇总会保留 `statistics-before-daily-v1.json`。独立文件检查点最多每分钟更新，不提供跨文件事务或断电 fsync 保证；降级无法自动恢复旧分钟精度。非零保留期限会永久过期旧统计。
4. 完整周期快照和只读健康能力声明需要匹配的 Manager Server 源码适配与重建；只更新 HTML 不能替代后端补丁。

### 下载与校验

Linux amd64、Linux arm64、Windows amd64、macOS arm64 四平台 ZIP，以及 `commandcodebridge_0.2.10_cpamp-web.zip`、五个 `.zip.sha256` 和统一 `checksums.txt`，共 **11 个资产**。`BUILD.json` 记录同一来源提交、兼容基线、库、本地 OAuth 接收器与内嵌页面摘要；四平台内嵌页面与独立 CPAMP 页面一致。

动态库按 CPA 服务器平台选择，本地 OAuth 接收器按浏览器电脑平台选择。Linux 包要求 glibc ≥2.34；Alpine/musl 需另行构建。CPA 支持基线为 v8.0.23，CPAMP 页面基于 `dbf61dd`。插件源：[registry.json](https://raw.githubusercontent.com/Gaoshou101/CommandCodeBridge/main/registry.json)。

### 验证与边界

本地已完成 Node 31 项、Go 测试／vet、Windows 动态库、隔离 CPA 与 CPAMP 联测，以及五页 × 桌面／手机 × 明暗主题的 20 组前后对照。标签发布流程要求协议测试、四平台构建和来源／摘要一致性校验通过后才发布完整资产。

按用户选择，**真实 Claude Messages、图片语义、thinking 权限与签名、ZDR 上游接受及保留策略、个人／组织与同账号多 key 身份共享仍待验证**。实际浏览器 200% zoom、当前登录后官网参照仍待补证据；不宣称逐像素复刻。本次发版不包含服务器部署，也没有真实模型生成。

详细记录：[验证](https://github.com/Gaoshou101/CommandCodeBridge/blob/v0.2.10/docs/VALIDATION.md)、[升级与恢复](https://github.com/Gaoshou101/CommandCodeBridge/blob/v0.2.10/docs/DEPLOYMENT.md)、[第三轮设计](https://github.com/Gaoshou101/CommandCodeBridge/blob/v0.2.10/docs/ROUND3_UI_REVIEW.md)。

## 已发布 v0.2.9

发布日期：2026-10-13。CPA 原生插件，C ABI 1 / RPC schema 6；调用官方 Provider API。

### 本次更新

- **最近请求自动更新**：CPAMP 凭证页读取插件保存的真实请求记录，每 30 秒同步时间和成功／失败状态；切回页面时立即同步。本地冷却跳过不覆盖实际请求，也不因超过 24 小时而隐藏最后一次请求。
- **额度持久化**：脱敏额度观察写入 `data_dir/usage.json`，插件重启和页面重载后恢复。文件更新时间、备注或模型配置变化不再清空已查额度。
- **凭证隔离**：更换密钥、代理或上游后不复用旧额度；前端按管理连接和凭证索引隔离缓存，并校验服务端凭证版本。
- **失败保留**：额度刷新失败保留上次值和真实采集时间；读取同一旧快照不会把失败标记清掉。手动重试不再被自动采样的五分钟错误退避阻挡。
- 四平台插件内嵌匹配的新 CPAMP 页面，与独立前端包一致；保留 v0.2.8 的月额度、套餐名和原生窗口历史功能。

### 升级步骤

1. 在支持热升级的 CPA 插件商店更新至 **v0.2.9**，确认插件控制台运行版本。
2. 进入「设置 → CPAMP 原生面板」，点击「更新原生面板」并检查生效状态，再强制刷新浏览器。
3. 打开凭证页，首次升级可点击一次「刷新额度」建立观察；之后页面和插件重启会恢复缓存，最近请求自动更新。
4. Docker 部署需持久化插件 `data_dir`，其中包括新的 `usage.json`；无需重新导入现有凭证和模型。

已启用的外部页面可原路径热替换，无需重启 Manager Server。首次启用外部页面或改变其路径，需要重启该服务；旧宿主和手动覆盖被加载的动态库按宿主要求重启。实际额度重置时间仍取上游真实窗口。

**完整周期快照仍需 Manager Server 的平台名单接受 `commandcode`。** v0.2.8 已应用该补丁的环境无需再次重建；包内 `integrations/cpamp/apply.py` 提供对应源码适配，页面更新入口只替换 HTML。

### 下载内容

Linux amd64、Linux arm64、Windows amd64、macOS arm64 四平台 ZIP，另附 `commandcodebridge_0.2.9_cpamp-web.zip`、各包 SHA256 文件和统一 `checksums.txt`。`BUILD.json` 记录来源提交、库和内嵌页面摘要。

动态库按 CPA 服务器平台选择，本地 OAuth 接收器按浏览器电脑平台选择。Linux 包要求 glibc ≥2.34；Alpine/musl 需另行构建。CPAMP 页面基于 `dbf61dd`。

插件商店源：[registry.json](https://raw.githubusercontent.com/Gaoshou101/CommandCodeBridge/main/registry.json)。

### 验证

发布流程执行 Go race/vet、前端回归和类型检查、四平台 CPA v8.0.23 原生协议与异常恢复联测、0.2.8 → 0.2.9 热升级、CPAMP 页面安装与热更新验证。新增原生宿主重启后的额度窗口、最近请求状态和原始观察时间恢复断言。

实际账户的额度由上游查询返回，最近请求保留范围受插件日志保留设置约束。Go 订阅 `/alpha/generate` 与固定上游 provider 模式不在支持范围。

详见[部署说明](./DEPLOYMENT.md)、[原生集成](../integrations/cpamp/README.md)、[验证记录](./VALIDATION.md)和[更新记录](../CHANGELOG.md)。
