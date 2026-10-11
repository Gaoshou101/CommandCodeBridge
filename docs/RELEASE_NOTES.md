# CommandCodeBridge v0.2.1

CPA 原生插件；C ABI 1 / RPC schema 6。官方 Provider API 路线，Go 订阅 `/alpha/generate` 不在当前范围。

## v0.2.1（2026-10-11）

- 左下角链接统一指向 [CommandCode 官网](https://commandcode.ai/)，文字改为「Command Code 官网」。公开控制台不再包含个人工作台路径。

## v0.2.0（2026-10-11）

- 第二轮布局参考 Command Code 官方工作台：固定可收起侧栏、灰白/纯黑配色与品牌紫，使用官方导航图标。凭据改为账号选择与单账号用量详情；日志改为紧凑列表；移除重复说明，将 API 示例收进设置页。
- 使用 CommandCode 官网图标；优化明暗主题、文字层级、账户额度卡片与手机布局。
- 新增月额度窗口：显示实际月余额、本期已用额度和重置时间。月上限缺失时明确显示未知；保留原生 monthly 窗口兼容。
- 运行概览增加凭证健康度：正常、待验证、鉴权异常、需关注、共享模型冷却和已停用；显示最近请求及查询时间。
- 新导入模型默认名称不含供应商前缀，例如 `glm-5.3-flash`、`deepseek-v4.1-flash`；手动添加时自动填写，可编辑为自定义别名。已有映射继续使用原名称。
- 请求日志参考 ClinePassBridge，改为可展开列表，突出状态、首字/总耗时、用量和速率，展开查看缓存写入、思考 tokens、流终态及错误详情。
- 修复 Windows 凭据更新后立即删除偶发失败：短暂文件占用会自动重试，并保留等待期间的外部修改。
- 已有凭据和模型映射无需迁移；月上限缺失时显示未知。安装前备份插件动态库、CPA auth-dir 与插件 data_dir，升级后重启 CPA。

## 下载与安装

提供 Linux amd64/arm64、Windows amd64、macOS arm64 ZIP，各附 SHA256 和包内 `BUILD.json`。选择匹配宿主的平台，按照 [部署说明](https://github.com/Gaoshou101/CommandCodeBridge/blob/main/docs/DEPLOYMENT.md) 安装、备份及回滚。Linux 包要求 glibc ≥2.34；Alpine/musl 需另行构建。需要带插件支持的 CPA，已验证 v8.0.23。

仓库已公开。CPA / CPAMP 链接安装使用 [插件源 registry.json](https://raw.githubusercontent.com/Gaoshou101/CommandCodeBridge/main/registry.json)，Release 提供安装器要求的 `commandcodebridge_0.2.1_<系统>_<架构>.zip` 和 `checksums.txt`。

## 已实现

- 多 key、优先级、会话重分配、模型目录/别名/思考映射。
- Chat Completions、Responses、Anthropic Messages，流/非流、工具和图片。
- 中文管理页、套餐用量、实际 token 记账、日志与统计。
- 修正真实账户 `totalCount` 解析；429 共享冷却、503 换号、超时、取消与异常流恢复。
- 日志/统计每 250ms 批量写入，失败重试、正常退出刷盘与统计清空的一致性保护。

## 验证

- Go 回归与竞态检查、go vet、10 个登录适配测试，以及 Windows 凭据文件占用回归。
- 四种平台均使用实际 CPA v8.0.23 完成原生宿主集成与异常恢复测试。
- 真实账户查询及免费 Ling 模型的 6 次 Chat/Responses、流/非流与工具往返通过，共 864 tokens，插件与 CPA 记账一致。

## 已知限制与安装后验收

- 模型目录可见不代表套餐可用；当前账户的 GPT/Claude 测试模型返回 `MODEL_NOT_IN_PLAN`。Claude 原生 Messages、图片、思考签名与 ZDR 的真实验收需要具备对应权限的模型。
- 强制结束进程或断电可能丢失尚未刷盘的记录；磁盘故障时无法保证 250ms 刷盘窗口，双 JSON 文件不提供跨文件事务。
- 用户自行安装，目标 CPAMP 的完整代理、目录权限和重启持久化仍须现场验收。

详细测试条件与证据见 [验证记录](https://github.com/Gaoshou101/CommandCodeBridge/blob/main/docs/VALIDATION.md)。
