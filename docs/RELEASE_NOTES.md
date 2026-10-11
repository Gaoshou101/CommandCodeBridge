# CommandCodeBridge v0.1.0

首版 CPA 原生插件；C ABI 1 / RPC schema 6。官方 Provider API 路线，Go 订阅 `/alpha/generate` 不在当前范围。

## 下载与安装

提供 Linux amd64/arm64、Windows amd64、macOS arm64 ZIP，各附 SHA256 和包内 `BUILD.json`。选择匹配宿主的平台，按照 [部署说明](https://github.com/Gaoshou101/CommandCodeBridge/blob/main/docs/DEPLOYMENT.md) 安装、备份及回滚。Linux 包要求 glibc ≥2.34；Alpine/musl 需另行构建。需要带插件支持的 CPA，已验证 v8.0.23。

仓库已公开。CPA / CPAMP 链接安装使用 [插件源 registry.json](https://raw.githubusercontent.com/Gaoshou101/CommandCodeBridge/main/registry.json)，Release 提供安装器要求的 `commandcodebridge_0.1.0_<系统>_<架构>.zip` 和 `checksums.txt`。首版连字符命名的手动下载包仍保留；新增的下划线命名包与原包内容一致。

## 已实现

- 多 key、优先级、会话重分配、模型目录/别名/思考映射。
- Chat Completions、Responses、Anthropic Messages，流/非流、工具和图片。
- 中文管理页、套餐用量、实际 token 记账、日志与统计。
- 修正真实账户 `totalCount` 解析；429 共享冷却、503 换号、超时、取消与异常流恢复。
- 日志/统计每 250ms 批量写入，失败重试、正常退出刷盘与统计清空的一致性保护。

## 验证

- 19 个 Go 顶层测试、竞态检查、go vet、10 个登录适配测试。
- 四种平台均使用实际 CPA v8.0.23 完成原生宿主集成与异常恢复测试。
- 真实账户查询及免费 Ling 模型的 6 次 Chat/Responses、流/非流与工具往返通过，共 864 tokens，插件与 CPA 记账一致。

## 已知限制与安装后验收

- 模型目录可见不代表套餐可用；当前账户的 GPT/Claude 测试模型返回 `MODEL_NOT_IN_PLAN`。Claude 原生 Messages、图片、思考签名与 ZDR 的真实验收需要具备对应权限的模型。
- 强制结束进程或断电可能丢失尚未刷盘的记录；磁盘故障时无法保证 250ms 刷盘窗口，双 JSON 文件不提供跨文件事务。
- 用户自行安装，目标 CPAMP 的完整代理、目录权限和重启持久化仍须现场验收。

详细测试条件与证据见 [验证记录](https://github.com/Gaoshou101/CommandCodeBridge/blob/main/docs/VALIDATION.md)。
