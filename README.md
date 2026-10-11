# CommandCodeBridge

接入 [CommandCode](https://commandcode.ai/docs/provider) 的 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 原生插件，参考 [ClinePassBridge](https://github.com/xiao-qiu-qiu/ClinePassBridge)。版本 `0.1.0`；C ABI 1 / RPC schema 6。

## 功能

- 多个 Studio API key：添加、备注、编辑、停用、删除和 0–100 优先级。删除前确认 CPA 已停用登记。
- 会话重新分配：CPA watcher 轮换账号登记 ID，保留恢复副本。
- 公开模型目录、选择导入、手动添加、客户端别名、逐模型思考强度映射。
- Chat Completions、Responses、Anthropic Messages；流式/非流式、工具调用和图片。
- Claude 原生 Messages 保留 thinking signature；原生历史直接转发至 Messages 上游。
- 套餐、剩余 credits、5 小时/周窗口、到期时间和账号请求数；采样、缓存、共享账号标识、失败保留旧值。
- 请求日志、实际 tokens、缓存读取/写入、首字耗时、输出速率及模型/时间统计。
- 429 冷却与恢复探测、401/403 状态传播、脱敏、完整 SSE 和工具参数检查。
- 中文管理页、CPA/CPAMP 登录适配、子目录代理、明暗主题和手机布局。

**范围：官方 Provider API。Go 订阅 `/alpha/generate` 和固定上游 provider 不包含在本插件中。** 用量查询使用账户 `/alpha/...` 查询接口。

## 安装

需要带插件支持的 CPA，已在 Linux x64 和 Windows x64 验证 **v8.0.23**。发布文件名含 `no-plugin` 的构建不能加载插件。库必须匹配宿主系统、CPU 和 C 运行库；当前 Linux 包要求 glibc ≥2.34，适用于 CPA 官方 Debian 容器。Alpine/musl 需重新构建。

1. 解压 ZIP，将 `commandcodebridge.so`（Windows：`.dll`；macOS：`.dylib`）复制到 CPA `plugins.dir`。
2. 将 [config.example.yaml](config.example.yaml) 合并至 CPA 配置，调整目录。首次 `models` 留空。
3. 重启 CPA，确认 `plugin loaded plugin_id=commandcodebridge`，且无 `invalid metadata or no capabilities`。
4. 打开 `http://你的CPA地址:端口/v0/resource/plugins/commandcodebridge/console`。
5. 用 **CPA 管理密钥**登录，在「凭据与用量」添加 CommandCode Studio API key，在「模型映射」获取目录并选择导入。
6. 客户端使用 CPA API 地址、**CPA 客户端密钥**和模型别名，默认 `commandcode/<上游ID>`。

插件不另开端口。页面可公开加载，数据及写操作由 CPA 管理鉴权保护。CPAMP 完整代理模式可选择 `X-Management-Key`；真实 CPAMP 部署需现场验证。

Docker 应持久化以下目录，以及现有 CPA auth-dir：

```yaml
volumes:
  - ./plugins:/CLIProxyAPI/plugins
  - ./commandcodebridge-data:/CLIProxyAPI/commandcodebridge-data
```

```bash
curl http://127.0.0.1:8317/v1/chat/completions \
  -H "Authorization: Bearer YOUR_CPA_CLIENT_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"commandcode/YOUR_UPSTREAM_MODEL_ID","messages":[{"role":"user","content":"你好"}],"stream":true}'
```

## 思考与协议

依据 `supported_endpoints` 选择上游：有 `/chat/completions` 时使用 Chat，否则使用 `/messages`。Responses 转换由 CPA 完成。上游总是流式，非流式请求聚合至完整终态。

映射例：`{"unset":"low","medium":"high","ultra":"max"}`。`unset` 表示客户端未指定；未命中则透传。显式 `thinking`/`reasoning` 不被 `unset` 的默认映射覆盖。

| Anthropic 模式 | 参数 | 映射 |
| --- | --- | --- |
| Adaptive | `thinking.type=adaptive` + `output_config.effort` | Claude 4.6 及更新模型默认；minimal→low、xhigh→high、ultra→max |
| Budget | `thinking.type=enabled` + `budget_tokens` | 较早模型默认；minimal/low/medium/high/xhigh/max/ultra→1024/2048/4096/8192/16384/32768/32768 |
| none | `thinking.type=disabled` | 关闭思考 |

模型编辑可修改协议。Budget 的 `max_tokens` 必须大于思考预算，否则返回 400。模型支持的强度以实际上游为准。

原生 Messages→Messages 保留签名和历史。Chat 客户端重放 Claude 思考需保留扩展字段 `reasoning_signature`。Responses 等跨协议调用依赖 CPA 的转换，可能无法保留所有供应商专用字段。未实现服务器工具、视频或精确 token 计数。

## 数据与调度

- API key 仅存 CPA `auth-dir`；管理列表不返回 key，日志不保存请求正文。
- 会话恢复副本含凭据，存于 `auth-dir/.commandcodebridge-affinity-backups/`。稳定账号 ID 与调度 ID 分开，重分配不丢统计。
- `data_dir/settings.json` 保存设置/模型；`requests.json` 保存最近日志；`statistics.json` 保存独立分钟统计。清空统计保存恢复副本，日志保留。
- 已存 `settings.json` 优先于初始 YAML。更换 data_dir 需重启；重新应用 YAML 前应迁移或移除旧设置。
- CPA 负责优先级和会话绑定；重新分配等待 watcher 确认，运行中的生成继续完成。
- HTTP 使用 **CPA 全局代理**，例如 v8 的 `requests.proxy-url`。凭据级 `proxy_url` 明确报错；宿主通用 HTTP RPC 不提供该能力。请求级代理尚未支持。
- 同账号多 key 用上游用户/组织身份关联，额度不相加。429 按账号与上游模型共享冷却；身份未知时按 key 处理。
- 未知用量显示 `—`；查询失败保留旧值并标记过期，部分失败标明相关字段。不用缺失字段估算余额。
- 输出速率仅针对有实际 usage、首字后耗时 ≥500ms 的请求计算。
- 初版每请求同步持久化 JSON，高吞吐性能尚未验证。

## 构建

Go ≥1.26、CGO、目标平台 C 编译器：

```bash
CGO_ENABLED=1 go build -trimpath -buildvcs=false -buildmode=c-shared \
  -ldflags="-s -w" -o dist/commandcodebridge.so ./cmd/commandcodebridge
python3 scripts/package.py dist/commandcodebridge.so --target linux-amd64
```

Windows 用 MinGW GCC 输出 `.dll`；macOS 用 Clang 输出 `.dylib`。跨编译须提供目标 C 编译器。生成的 `.h` 安装时不需要。

```bash
docker buildx build -f Dockerfile.build --platform linux/amd64 --output type=local,dest=dist .
# ARM64：--platform linux/arm64
```

未发布仓库的元数据为 `unpublished (local build)`，满足 CPA 非空检查。正式构建用 `-X commandcodebridge/internal/bridge.Repository=https://github.com/OWNER/REPO` 指定真实地址。GitHub Actions 已配置测试及 Linux amd64/arm64、Windows amd64、macOS arm64 构建，上传 ZIP/SHA256，不自动发布 Release 或注册商店。

## 验证

```bash
go test -race ./...
go vet ./...
node --test tests/cpa-auth.test.cjs
CPA_BINARY=/path/to/cli-proxy-api python3 tests/cpa_e2e.py
# 加 --serve 保持模拟控制台运行。
```

当前：16 个 Go 顶层测试（含多个子场景）、10 个登录适配测试；真实 CPA v8.0.23 分别加载 Linux amd64 `.so` 和 Windows amd64 `.dll`，两种宿主各完成 25 次成功生成及 401/403 场景。覆盖三种协议、流/非流、工具往返、图片、Claude 签名返回/重放、思考映射、优先级、会话重分配、编辑/删除、额度及 CPA 记账。

**上游全为本地模拟服务和虚构 key。尚未实账号验证套餐响应或付费生成，未部署远端。** Linux ARM64 已构建、尚未在 ARM64 宿主运行；macOS 仅配置 CI 构建。

## 许可

MIT。来源与版权见 [NOTICE](NOTICE)、[LICENSE](LICENSE)。此项目是独立集成。
