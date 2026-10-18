# CommandCodeBridge：可行性与实现结论

更新：2026-10-18。

## 结论

**可行，已实现可加载的 CPA 原生插件。** 用户确认排除 Go 订阅和固定上游 provider；生成请求仅用官方 Provider API。凭据、模型、工具/图片/思考、流/非流、套餐用量、日志统计和控制台已实现。

## 验证证据

- 参考 ClinePassBridge commit `7ac0d011fc26c821f27369c0c442f47057a81e8e`，复用 C ABI、SSE 聚合、会话重分配及登录适配，保留 MIT 版权。
- CPA 官方带插件二进制 v8.0.23 实际加载 Linux amd64 和 Windows amd64 动态库：ABI 1 / RPC schema 6。
- 元数据必须含 Name、Version、Author、GitHubRepository。本地未发布构建用明确标记，正式构建可覆盖真实地址。
- 公开目录的 supported_endpoints 决定 Chat 或 Messages 上游。目录包含模型不代表账号有权限。
- Linux 与 Windows 真实宿主各对模拟上游完成 25 次成功生成和两个错误场景。三种协议、流/非流、工具往返、图片、思考映射、Claude 签名返回与重放通过。
- 账号优先级、停用切换、watcher 会话重分配、编辑/删除、额度查询通过。Windows 删除前确认停用，避免 CPA 留下可选中的内存登记。插件与 CPA 均只记账一次，成功请求共 325 tokens。
- CPA 的 Chat→Messages 转换丢失签名，插件增加原生 Messages 输出及原始 Messages 历史转发。
- 通用 HTTP RPC 只支持全局代理，凭据代理明确拒绝，避免意外绕过。
- 管理页检查了登录、备注编辑、模型测试、设置、主题和响应式布局；登录单测含子路径和端点隔离。

### 第一轮补充

v0.2.10 本地源码新增凭证运行登记诊断和升级兼容检查，分别报告保存、加载、页面与快照后端的证据。固定兼容基线仍为 CPA v8.0.23；较新上游候选任务尚无本轮远端结果。第三轮视觉优化已列入 [NEXT_STEPS.md](docs/NEXT_STEPS.md)。

### 第二轮补充

v0.2.10 未发布源码已加入按凭据版本／账户／映射隔离的模型证据、独立历史文件落盘、检查点与损坏恢复，以及分钟／日统计分层。90 天合成数据保留总量、JSON 体积缩减约 91.6%；真实宿主连接模拟端的 13 次扩展协议验收通过。真实 Claude／图片语义／thinking／ZDR 和账户共享按用户要求仍为待验证，详见 [VALIDATION.md](docs/VALIDATION.md)。没有跨 key 合并采样，没有发布部署。

## 架构

```text
客户端 → CPA 鉴权、模型路由、账号调度
              ↓
       CommandCodeBridge（C ABI 动态库）
          ├─ /provider/v1/chat/completions
          ├─ /provider/v1/messages
          ├─ /alpha/whoami、billing、usage 账户查询
          └─ CPA 管理 API + 中文控制台
              ↓
       完整响应校验、usage、错误 → CPA → 客户端
```

凭据存 CPA auth-dir；模型/设置/日志/统计存 data_dir。上游始终流式，非流式聚合完整终态；余额及窗口来自账户接口，缺失值不估算。Responses 依赖 CPA 转换。

## 待真实环境核实

仓库已有真实账户查询、套餐字段与免费 Ling 模型 Chat／Responses／工具往返的验证记录；已有短时模拟并发基线，不能代表真实上游速度或长期生产容量。详见 [VALIDATION.md](docs/VALIDATION.md)，本轮未重复使用真实账户。

Claude 原生 Messages、图片、thinking signature 和 ZDR 仍缺真实权限／行为验证；个人／组织身份、同账号多 key 的共享边界及长期运行容量仍需核实。CPAMP 已有隔离二进制及历史部署页面验证，本轮未连接或修改目标部署。安装与协议边界见 [README](README.md)。

## 参考

- [ClinePassBridge](https://github.com/xiao-qiu-qiu/ClinePassBridge)
- [CLIProxyAPI v8.0.23](https://github.com/router-for-me/CLIProxyAPI/tree/v8.0.23)
- [CommandCode Provider 文档](https://commandcode.ai/docs/provider)
- [公开模型目录](https://api.commandcode.ai/provider/v1/models)
