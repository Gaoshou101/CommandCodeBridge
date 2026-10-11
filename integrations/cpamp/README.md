# CPAMP 原生凭证集成

## 两处问题的原因

旧插件 `auth.login.start` 固定返回「从控制台导入 key」，CPA 将它转换为 `failed to generate authorization url`。`0.2.2` 实现原生登录启动、轮询与凭证交付。

`0.2.2` 将远程 CPA 公开地址交给官网作为 callback，但 CommandCode 只允许 loopback。`0.2.3` 参考 CPAMP Codex/Claude 的 URL 提交方式，误以为凭证在回调查询参数里。重新核对官方 CLI 1.79.2 后确认：**浏览器流程以表单 POST 回传，legacy 以 JSON POST 回传**，裸 `localhost:45879/callback` 地址不含凭证。

`0.2.4` 补齐浏览器电脑上的本地 POST 接收器。它接收官网凭证，显示可复制认证结果，再由 CPAMP 原生卡片通过受管理鉴权保护的插件 POST 提交给远端 CPA。继续复用原生状态轮询和 CPA 凭证保存；CPA 通用回调仅提取 `code`，无法直接交付 CommandCode 的 `apiKey`。

CPAMP 当前源码的额度平台列表固定为内置平台，因此仅增加插件额度接口仍不会在原生凭证页渲染 CommandCode。本适配直接在 CPAMP 的原生网格、列表、详情额度页增加套餐、credits、窗口进度与重置时间，复用面板已有的 Button 和 QuotaProgressBar。

## 安装

1. 备份原插件动态库、CPA auth-dir、插件 data_dir 以及 CPAMP 当前管理页面。
2. 安装匹配 CPA 平台的 CommandCodeBridge `0.2.4` 动态库并重启 CPA。从 [v0.2.4 Release](https://github.com/Gaoshou101/CommandCodeBridge/releases/tag/v0.2.4) 选择匹配平台 ZIP，并核验 `checksums.txt`。
3. **同时升级管理页面**。面板版本匹配 CPAMP `dbf61dd` 时，下载 Release 的 `commandcodebridge_0.2.4_cpamp-web.zip`，使用包内 `management.html`，按下节安装。只升级插件不会让旧面板出现认证结果输入框和原生额度卡片。需要自行构建时，在对应 CPAMP 源码目录应用补丁：

```bash
python /path/to/CommandCodeBridge/integrations/cpamp/apply.py /path/to/CPA-Manager-Plus --check
python /path/to/CommandCodeBridge/integrations/cpamp/apply.py /path/to/CPA-Manager-Plus
cd /path/to/CPA-Manager-Plus
npm ci --ignore-scripts --workspace apps/web
npm --workspace apps/web run type-check
npm --workspace apps/web run test -- src/features/accounts/components/CommandCodeQuotaCard.test.tsx src/services/api/CommandCodeOAuth.test.ts src/services/api/oauth.test.ts src/features/oauth/OAuthPage.test.tsx src/features/accounts/components/accountDetail/AccountQuotaTab.test.tsx
npm --workspace apps/web run build
# 可选：在 CommandCodeBridge 源码目录打包上述构建产物
# python scripts/package_cpamp.py /path/to/CPA-Manager-Plus
```

脚本会先验证所有插入点再写入，重复执行不会重复插入。已验证的 CPAMP 源码为 `dbf61ddf438f399558722df3467355c42af9005d`。不同源码版本可能需要调整插入点，脚本会拒绝不匹配的结构。

4. 安装构建产物 `apps/web/dist/index.html`，或使用上述 Release 中的 `management.html`。ZIP 提供许可证、构建信息、校验值与 `verify_cpamp_panel.py`；如当前部署版本不匹配，应在对应源码上应用适配后自行构建。
5. 按下节核验实际服务页面，再强制刷新浏览器。API key 账号直接在原生凭证列表、卡片或详情额度页点击「刷新额度」，无需重新添加凭证。
6. OAuth 认证按「远程认证步骤」运行浏览器电脑本地接收器。

### 使用现有 Manager Server 托管页面

CPAMP 支持 `PANEL_PATH` 环境变量覆盖内置面板，也支持配置 JSON 的 `panelPath` 字段。可安装自定义页面，无需重新构建 Manager Server。先核验 ZIP 的 `.sha256` 和包内 `BUILD.json` 的页面校验值，将 `management.html` 放到自己选择的持久化位置。

**直接运行二进制**：在启动 Manager Server 的同一环境设置 `PANEL_PATH`，随后用现有启动命令及参数启动程序。以下路径是占位符，需要换成实际文件绝对路径：

```bash
export PANEL_PATH='/实际绝对路径/management.html'
# 使用现有命令启动 Manager Server
```

systemd 部署应在实际 Manager Server 的 service override 中写入 `[Service]` 下的 `Environment="PANEL_PATH=/实际绝对路径/management.html"`，执行 `daemon-reload` 并重启该服务。在另一个终端 export 不会改变已运行进程。Windows 手动启动用 `$env:PANEL_PATH = 'C:\实际路径\management.html'`，再在该 PowerShell 中使用原启动命令。现有 JSON 配置也可设置 `panelPath`；环境变量优先。

Manager Server 运行用户必须能读取页面。**配置文件打不开时服务会静默回退到内置页面**，因此不能只凭重启或本地文件存在判断成功。安装后运行包内检查脚本：

```bash
python3 verify_cpamp_panel.py https://cpa.gdp.pw/management.html --expected '/实际绝对路径/management.html'
```

应显示 `native_quota: PASS`、`native_callback: PASS`、`post_receiver_instructions: PASS` 和 `expected_page: MATCH`。脚本只读且不需要管理密钥。未通过时检查运行进程的 `PANEL_PATH`、读取权限、代理目标和缓存；确认后再强刷浏览器。

Docker Compose 示例仅表示需要合并到现有 **Manager Server 服务**的两项；服务名和宿主目录应替换为现场实际值，并保留原有镜像、环境变量、端口和数据卷：

```yaml
services:
  manager-server:
    environment:
      PANEL_PATH: /opt/cpamp-panel/management.html
    volumes:
      - ./custom-panel:/opt/cpamp-panel:ro
```

将页面保存为 Compose 项目目录下的 `custom-panel/management.html`，然后重新创建对应 Manager Server 容器使环境变量生效。CPA 的插件动态库安装到 CPA 容器，页面挂载到 Manager Server 容器。

保持页面和 Manager Server 的 CPAMP 源码版本一致。安装后清理代理/浏览器缓存并强制刷新。回滚时恢复原 `PANEL_PATH` 配置或原页面文件，再重启 Manager Server；此过程不需要迁移管理数据库。

## 远程认证步骤

1. 下载**浏览器电脑平台**的 ZIP。Windows 运行 `commandcodebridge-login.exe`；macOS/Linux 终端运行 `./commandcodebridge-login`（必要时先 `chmod +x commandcodebridge-login`）。接收器会打开 `http://127.0.0.1:45879`。服务器是 Linux、浏览器电脑是 Windows 时，需要另下载 Windows ZIP 取出 `.exe`。
2. 在新版 CPAMP 的「OAuth 登录」选择 CommandCodeBridge 并重新开始登录，复制卡片上的官方「授权链接」。卡片的「打开本地接收器」按钮可打开已经启动的接收器页面。
3. 把授权链接粘贴到接收器，点击「打开官方授权」。接收器保留本次 state 并跳转官网。
4. 完成官网授权。官网向本地 `/callback` 表单 POST，接收器跳转到只带 state 的完成页。点击「复制认证结果」。该结果包含凭证，是传递字符串，不要打开或分享。
5. 回到 CPAMP 原生卡片，将结果粘贴到「认证结果」并提交，等待状态轮询确认成功。接收器无需 CPA 管理密钥；认证结果由当前 CPAMP 管理登录提交给远端 CPA。
6. 关闭本地接收器，在原生凭证页点击「刷新额度」。若 state 过期，重新从第 2 步开始。

loopback 指浏览器所在电脑。CPA 不需要监听 `45879`，无需开放端口、配置 SSH 隧道或设置 `public_base_url`。官方授权链接使用 `127.0.0.1`，不能替换为远端服务器域名。裸 callback 地址无法恢复已丢失的 POST 正文，需要重新登录。

面板 POST 使用当前选中的 CPA 地址、代理子目录和管理凭据；复制结果只作为请求正文提交。独立插件登录页也支持接收器结果和手动 Studio key 回退。

## 数据和边界

- 原生发现：`GET /v0/management/quota/providers`；查询：`POST /v0/management/quota/fetch`，请求仅需要当前凭证的 `auth_index`。
- 上游通过 CPA 宿主传输策略查询现有 `/alpha` 接口，复用插件采样缓存；每次显式刷新至少间隔十秒。
- 本月剩余、本期已用和订阅重置日期显示实际值。只有真实 `monthly` 窗口具备有效 cap 时显示月百分比，其他情况显示「月上限未知」。credits 不换算成美元。
- 打开页面不主动访问上游；点击刷新才查询。网格、列表和详情共享当前内存结果，不把账户余额写到浏览器存储。切换 CPA、管理登录或凭证版本会隔离旧结果。
- 查询失败保留上次观察并标记过期；部分成功只发布本次成功的字段。单个模型无权限不影响账户额度查询。
- 此补丁添加原生展示和刷新，不改变现有 Codex 调度、全局额度排序/筛选或历史快照数据库。
- 原生登录 state 十分钟过期、单次消费；key 先经只读 whoami 验证，再由 CPA 保存到 auth-dir。手动回退接口需要管理鉴权。
- 官方回调凭证在 POST 正文。接收器只监听 IPv4 loopback，校验官网 Origin、Host、本次 state、重复字段、有效期和单次消费；结果仅存内存，十分钟后清除，进程十五分钟自动退出。不保存文件、不向远端自动转发、不读取 CPA 管理密钥。
- 接收器的复制结果包含 API key，请勿分享。浏览器地址栏只含 state；接收器与 CPAMP 输入框遮蔽结果。CPAMP 提交成功后清空，不写入浏览器存储；远端提交使用 POST 正文。插件校验 loopback 地址、固定路径/端口、state、重复参数、有效期和单次消费，API key 先经只读 whoami 验证。

## 回滚

恢复备份的管理页面和插件库，重启 CPA。新增凭证仍为正常 `type: commandcode` 文件，不需要修改现有模型映射或迁移数据库。
