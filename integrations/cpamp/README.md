# CPAMP 原生凭证集成

## 两处问题的原因

旧插件 `auth.login.start` 固定返回「从控制台导入 key」，CPA 将它转换为 `failed to generate authorization url`。`0.2.2` 实现原生登录启动、轮询与凭证交付。

CPAMP 当前源码的额度平台列表固定为内置平台，因此仅增加插件额度接口仍不会在原生凭证页渲染 CommandCode。本适配直接在 CPAMP 的原生网格、列表、详情额度页增加套餐、credits、窗口进度与重置时间，复用面板已有的 Button 和 QuotaProgressBar。

## 安装

1. 备份原插件动态库、CPA auth-dir、插件 data_dir 以及 CPAMP 当前管理页面。
2. 安装匹配平台的 CommandCodeBridge `0.2.2` 动态库并重启 CPA。当前本地交付包为 Linux amd64；发布 CI 会构建其他平台。
3. 在 CPAMP 源码目录应用补丁并重新构建前端：

```bash
python /path/to/CommandCodeBridge/integrations/cpamp/apply.py /path/to/CPA-Manager-Plus --check
python /path/to/CommandCodeBridge/integrations/cpamp/apply.py /path/to/CPA-Manager-Plus
cd /path/to/CPA-Manager-Plus
npm ci --ignore-scripts --workspace apps/web
npm --workspace apps/web run type-check
npm --workspace apps/web run test -- src/features/accounts/components/CommandCodeQuotaCard.test.tsx src/services/api/CommandCodeOAuth.test.ts src/services/api/oauth.test.ts src/features/accounts/components/accountDetail/AccountQuotaTab.test.tsx
npm --workspace apps/web run build
# 可选：在 CommandCodeBridge 源码目录打包上述构建产物
# python scripts/package_cpamp.py /path/to/CPA-Manager-Plus
```

脚本会先验证所有插入点再写入，重复执行不会重复插入。已验证的 CPAMP 源码为 `dbf61ddf438f399558722df3467355c42af9005d`。不同源码版本可能需要调整插入点，脚本会拒绝不匹配的结构。

4. 安装构建产物 `apps/web/dist/index.html`，具体方式见下节。本地 `commandcodebridge_0.2.2_cpamp-web.zip` 提供相同源码基线的已构建 `management.html`、许可证、构建信息和校验值；如当前部署版本不匹配，应在对应源码上应用适配后自行构建。
5. 刷新面板，在 OAuth 登录选择 CommandCodeBridge，打开原生链接后点击官方登录。若官网不允许向远程地址回传，使用该页面的 Studio API key 手动回退。
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

## 公开地址

CPA 提供给插件的 `BaseURL` 是服务器内部 loopback 回调地址，不能直接作为用户浏览器地址。默认返回相对资源路径；前端补丁会按当前所选 CPA 地址解析。异源面板或独立插件页面可显式设置：

```yaml
plugins:
  configs:
    commandcodebridge:
      public_base_url: https://your-panel.example/cpa
```

包含真实代理子目录；不包含 `/v0/management`、查询参数或用户名密码。新页面按自身的公开地址构造官方回调，回调不另开端口。

## 数据和边界

- 原生发现：`GET /v0/management/quota/providers`；查询：`POST /v0/management/quota/fetch`，请求仅需要当前凭证的 `auth_index`。
- 上游通过 CPA 宿主传输策略查询现有 `/alpha` 接口，复用插件采样缓存；每次显式刷新至少间隔十秒。
- 本月剩余、本期已用和订阅重置日期显示实际值。只有真实 `monthly` 窗口具备有效 cap 时显示月百分比，其他情况显示「月上限未知」。credits 不换算成美元。
- 打开页面不主动访问上游；点击刷新才查询。网格、列表和详情共享当前内存结果，不把账户余额写到浏览器存储。切换 CPA、管理登录或凭证版本会隔离旧结果。
- 查询失败保留上次观察并标记过期；部分成功只发布本次成功的字段。单个模型无权限不影响账户额度查询。
- 此补丁添加原生展示和刷新，不改变现有 Codex 调度、全局额度排序/筛选或历史快照数据库。
- 原生登录 state 十分钟过期、单次消费；key 先经只读 whoami 验证，再由 CPA 保存到 auth-dir。手动回退接口需要管理鉴权。
- 官方 redirect 参数含 API key：插件立即移除参数并禁用 referrer；代理应对 callback 的查询串脱敏。手动回退 key 仅经过 POST 请求正文。

## 回滚

恢复备份的管理页面和插件库，重启 CPA。新增凭证仍为正常 `type: commandcode` 文件，不需要修改现有模型映射或迁移数据库。
