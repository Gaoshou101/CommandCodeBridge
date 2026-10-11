# CPAMP 原生凭证集成

## 两处问题的原因

旧插件 `auth.login.start` 固定返回「从控制台导入 key」，CPA 将它转换为 `failed to generate authorization url`。`0.2.2` 实现原生登录启动、轮询与凭证交付。

`0.2.2` 将远程 CPA 公开地址交给官网作为 callback，但 CommandCode 只允许 localhost。`0.2.3` 对照 CPAMP Codex/Claude 的远程回调提交流程：直接打开官方授权链接，使用固定 localhost 回调，再在原生卡片提交完整回调 URL。CommandCode 返回 `apiKey` 而 CPA 通用接口只提取 `code`，因此前端通过受管理鉴权保护的插件 POST 提交完整 URL，仍沿用原生状态轮询和 CPA 凭证保存。

CPAMP 当前源码的额度平台列表固定为内置平台，因此仅增加插件额度接口仍不会在原生凭证页渲染 CommandCode。本适配直接在 CPAMP 的原生网格、列表、详情额度页增加套餐、credits、窗口进度与重置时间，复用面板已有的 Button 和 QuotaProgressBar。

## 安装

1. 备份原插件动态库、CPA auth-dir、插件 data_dir 以及 CPAMP 当前管理页面。
2. 安装匹配平台的 CommandCodeBridge `0.2.3` 动态库并重启 CPA。从 [v0.2.3 Release](https://github.com/Gaoshou101/CommandCodeBridge/releases/tag/v0.2.3) 选择匹配平台 ZIP，并核验 `checksums.txt`。
3. **同时升级管理页面**。面板版本匹配 CPAMP `dbf61dd` 时，下载 Release 的 `commandcodebridge_0.2.3_cpamp-web.zip`，使用包内 `management.html`，按下节挂载。只升级插件不会让旧面板出现回调输入框。需要自行构建时，在对应 CPAMP 源码目录应用补丁：

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

4. 安装构建产物 `apps/web/dist/index.html`，具体方式见下节。Release 的 `commandcodebridge_0.2.3_cpamp-web.zip` 提供相同源码基线的已构建 `management.html`、许可证、构建信息和校验值；如当前部署版本不匹配，应在对应源码上应用适配后自行构建。
5. 强制刷新面板，在 OAuth 登录选择 CommandCodeBridge，重新发起登录并打开官方链接。完成授权后会跳转到 `http://localhost:45879/callback?apiKey=…&state=…`。远程模式下页面无法连接是预期情况；复制地址栏的**完整 URL**，回到 CPAMP 原生卡片粘贴并点击「提交回调 URL」，等待认证成功。不要修改 URL 或复用旧登录的 state。
6. 打开原生凭证页，在 CommandCode 卡片点击「刷新额度」。凭证详情的额度 Tab 也可刷新，并复用本次观察。

### 使用现有 Manager Server 托管页面

CPAMP 支持 `PANEL_PATH` 环境变量覆盖内置面板，也支持配置 JSON 的 `panelPath` 字段。可安装自定义页面，无需重新构建 Manager Server。先核验 ZIP 的 `.sha256` 和包内 `BUILD.json` 的页面校验值，将 `management.html` 放到自己选择的持久化位置。

Docker Compose 示例仅表示需要合并到现有 **Manager Server 服务**的两项；服务名和宿主目录应替换为现场实际值，并保留原有镜像、环境变量、端口和数据卷：

```yaml
services:
  manager-server:
    environment:
      PANEL_PATH: /opt/cpamp-panel/management.html
    volumes:
      - ./custom-panel:/opt/cpamp-panel:ro
```

将页面保存为 Compose 项目目录下的 `custom-panel/management.html`，然后重新创建对应 Manager Server 容器使环境变量生效。CPA 的插件动态库应安装到 CPA 容器，而页面挂载到 Manager Server 容器。二进制部署则在原启动环境中设置 `PANEL_PATH=/实际绝对路径/management.html` 并重启该服务。若使用配置 JSON，可将 `panelPath` 设为实际文件路径，环境变量优先。

保持页面和 Manager Server 的 CPAMP 源码版本一致。安装后清理代理/浏览器缓存并强制刷新。回滚时恢复原 `PANEL_PATH` 配置或原页面文件，再重启 Manager Server；此过程不需要迁移管理数据库。

## 远程回调

localhost 指浏览器所在的电脑。此流程沿用 CPAMP 原生供应商的远程方式，用户将浏览器回调交给远端 CPA；CPA 不需要监听 `45879`，无需开放端口、配置 SSH 隧道或设置 `public_base_url`。官方授权链接始终使用 localhost，不能替换为远端服务器域名。

面板 POST 继续使用当前选中的 CPA 地址、代理子目录和管理凭据；完整 callback 只作为请求正文提交，不请求该 URL。独立插件登录页也提供相同回调输入框和手动 Studio key 回退。

## 数据和边界

- 原生发现：`GET /v0/management/quota/providers`；查询：`POST /v0/management/quota/fetch`，请求仅需要当前凭证的 `auth_index`。
- 上游通过 CPA 宿主传输策略查询现有 `/alpha` 接口，复用插件采样缓存；每次显式刷新至少间隔十秒。
- 本月剩余、本期已用和订阅重置日期显示实际值。只有真实 `monthly` 窗口具备有效 cap 时显示月百分比，其他情况显示「月上限未知」。credits 不换算成美元。
- 打开页面不主动访问上游；点击刷新才查询。网格、列表和详情共享当前内存结果，不把账户余额写到浏览器存储。切换 CPA、管理登录或凭证版本会隔离旧结果。
- 查询失败保留上次观察并标记过期；部分成功只发布本次成功的字段。单个模型无权限不影响账户额度查询。
- 此补丁添加原生展示和刷新，不改变现有 Codex 调度、全局额度排序/筛选或历史快照数据库。
- 原生登录 state 十分钟过期、单次消费；key 先经只读 whoami 验证，再由 CPA 保存到 auth-dir。手动回退接口需要管理鉴权。
- 官方 localhost redirect 参数含 API key：不要分享完整回调 URL。CPAMP 输入框遮蔽内容，提交成功后清空，不写入浏览器存储；远端提交通过 POST 请求正文。服务器校验 loopback 地址、固定路径/端口、当前 state、重复参数、有效期和单次消费，API key 先经只读 whoami 验证。

## 回滚

恢复备份的管理页面和插件库，重启 CPA。新增凭证仍为正常 `type: commandcode` 文件，不需要修改现有模型映射或迁移数据库。
