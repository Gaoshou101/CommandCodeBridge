# 开发验证记录

## 源码基线

- 2026-10-11 创建私有仓库 `Gaoshou101/CommandCodeBridge`。
- 初始提交：`9a43db5`，版本 0.1.0，C ABI 1 / RPC schema 6。
- 基线通过 Go 竞态检查、go vet、10 个登录适配测试，以及 Linux/Windows CPA v8.0.23 的 25 次成功模拟请求和 401/403 场景。
- 初次 GitHub Actions：运行 `38102314575` 全部成功，完成 Linux amd64/arm64、Windows amd64、macOS arm64 构建。此轮 ARM64/macOS 只构建，尚未在该轮运行宿主联测。

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
CPA_BINARY=/path/to/cli-proxy-api python3 tests/cpa_reliability.py
CPA_BINARY=/path/to/cli-proxy-api python3 tests/cpa_benchmark.py --requests 64
go test ./internal/bridge -run '^$' -bench BenchmarkRecordAtRetention -benchmem
```

Windows 设置 `BRIDGE_LIBRARY` 为实际 `.dll`；macOS 设置为 `.dylib`。真实测试默认只读，必须显式提供仓库外的凭据文件；`--generate` 才启用少量真实请求，永不进入 CI。

## 部署状态

目标 CPAMP 页面可访问，当前页面要求 Manager Server 管理员密钥，它与 CPA Management Key 不同。当前尚无该登录凭据或目标服务器部署通道，未修改目标服务。
