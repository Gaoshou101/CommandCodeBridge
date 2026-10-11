# CPAMP 部署、升级与回滚

目标 CPAMP 为 `https://cpa.gdp.pw/`，已确认可达。源码与安装包托管至 GitHub，用户自行安装；以下步骤供现场执行，当前尚未在目标服务完成安装。

## 下载安装包

从 [v0.1.0 Release](https://github.com/Gaoshou101/CommandCodeBridge/releases/tag/v0.1.0) 下载对应平台 ZIP 和 `.zip.sha256`。私有仓库需使用有访问权限的 GitHub 账号。

| 宿主 | 安装包后缀 | 动态库 | 要求 |
| --- | --- | --- | --- |
| Linux x64 | `linux-amd64.zip` | `commandcodebridge.so` | glibc ≥2.34 |
| Linux ARM64 | `linux-arm64.zip` | `commandcodebridge.so` | glibc ≥2.34 |
| Windows x64 | `windows-amd64.zip` | `commandcodebridge.dll` | 64 位 CPA |
| macOS Apple Silicon | `darwin-arm64.zip` | `commandcodebridge.dylib` | ARM64 CPA |

以上均已在对应平台使用 CPA v8.0.23 运行模拟集成与异常恢复测试。Linux Alpine/musl 需另外构建。

Linux 下载后，在文件所在目录核验：

```bash
sha256sum --check commandcodebridge-0.1.0-linux-amd64.zip.sha256
```

ARM64 替换文件名中的 `amd64`。Windows 使用 `Get-FileHash -Algorithm SHA256`，macOS 使用 `shasum -a 256`，与 `.sha256` 中的值比较。

## 部署前确认

1. 从目标主机实际服务配置确认 CPA 版本、启动命令、运行用户、CPU、glibc 和配置/凭据/插件目录。不要沿用其他服务器的路径。
2. 选择对应平台安装包，核验 `.zip.sha256` 和包内 `BUILD.json` 的版本、来源提交、库校验与 glibc 最低要求。使用包含插件支持的 CPA；`no-plugin` 宿主不能加载动态库。
3. 检查是否已安装旧版 CommandCodeBridge。保留旧库、CPA 配置、CPA auth-dir 和插件 data_dir 的可恢复备份。
4. 确认现有 CPA 全局代理。备份与配置中可能有密钥，应保持私有。

## 安装与验收

1. 将库安装到实际 `plugins.dir`，由 CPA 运行用户读取。合并 `config.example.yaml`，使用确认后的 data_dir。首次模型留空。
2. Docker 部署应持久化 plugins、data_dir 和现有 auth-dir，使用容器内路径。已有 settings.json 优先于 YAML。
3. 在维护窗口重启实际 CPA 服务，检查 `plugin loaded plugin_id=commandcodebridge`，确认没有 ABI、元数据或运行库错误。
4. 通过现有 CPAMP 登录进入插件控制台。Manager Server 管理员密钥、CPA Management Key 和客户端密钥分别用于不同接口。
5. 从控制台加入 Studio key；实际 key 只存 CPA auth-dir。先查询用量，再选择具备套餐权限的模型。
6. 验证外部面板的资源页和管理路由均通过正确代理；测试两种管理鉴权头所适用的登录模式，以及子目录路径。
7. 完成一条短客户端调用，检查实际 usage、插件统计和 CPA 记账。测试凭据备注、停用、恢复、模型编辑和手机布局。
8. 确认 `storage_error` 为空，并等待 `storage_pending=false`，再重启 CPA，核实数据、账号登记及模型恢复。

## 升级

1. 先备份旧库、配置和两类数据目录。
2. 检查新包的 ABI/schema、宿主版本与平台要求。
3. 正常停止 CPA，等待插件活动请求结束和最终刷盘，再替换库。强制结束进程可能丢失尚未刷盘的日志/统计。
4. 启动后重复加载、用量、短生成、数据恢复验收。升级时保留现有 settings.json，若更改默认 YAML 需明确迁移。

## 回滚

1. 正常停止 CPA，恢复已验证的旧库和对应配置。
2. 如涉及格式迁移或数据异常，恢复成套的 data_dir 和 auth-dir 备份；恢复 auth-dir 会影响其他渠道，必须使用与当前安装相匹配的备份。
3. 启动 CPA，验证插件加载、凭据登记、模型列表和一次短请求。

## 常见问题

| 现象 | 检查 |
| --- | --- |
| 无法加载库 | CPA 是否含插件支持；CPU/系统/运行库是否匹配；文件是否可读 |
| 模型可见但返回 403 | 目录不保证套餐权限，检查 `MODEL_NOT_IN_PLAN` |
| 接口直连失败或超时 | 确认 CPA 全局代理；插件不继承 Windows 系统代理 |
| 用量缺失 | 查看 `unavailable` / `status`；查询失败保留旧值 |
| 日志写入失败 | 检查 data_dir 的磁盘空间、权限及 `storage_error` |
| 重启后设置未按 YAML 改变 | 已存 settings.json 优先；采用明确的迁移方案 |
| CPAMP 登录失败 | 确认使用 Manager Server 管理员密钥；它不同于 CPA Management Key |
