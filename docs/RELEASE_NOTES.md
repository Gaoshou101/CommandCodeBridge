# CommandCodeBridge v0.2.8

发布日期：2026-10-13。CPA 原生插件，C ABI 1 / RPC schema 6；调用官方 Provider API。

## 本次更新

- 修复 CPAMP 月额度比例缺失：优先使用真实上游月比例／cap，否则按官方文档与 CLI 核实的套餐月限额计算。只采用 `monthly_credits`，不混入购买或免费 credits。
- 套餐名统一显示 Go、GOAT、Pro、Max；区分旧 Pro 30、新 Pro 80，以及 Max 两档 150／300 credits。GOAT 61.083 credits 显示 `61.083 / 70 credits`、剩余 87%。
- 列表布局去掉中间分割线，右侧月额度的标题与进度条和左侧 5h 对齐；保留周额度、真实余额和重置时间。
- 接入原生窗口请求历史、当前窗口统计、预测与周期快照。月周期使用真实订阅起止，历史缺失时不推算上个月边界，刷新失败不使用过期比例预测。
- 区分本地 `missing provider baseURL` 配置错误与失效凭证，修复历史 401 导致的「需重登」误判。
- 四平台动态库内嵌新版 CPAMP 页面，与独立前端包一致；重写 README 的安装、快速开始、额度和升级说明。

## 升级步骤

1. 在支持热升级的 CPA 插件商店更新至 **v0.2.8**，确认插件控制台运行版本。
2. 进入「设置 → CPAMP 原生面板」，点击「更新原生面板」并检查生效状态，再强制刷新浏览器。
3. 在原生凭证列表或详情页刷新额度，现有凭证与模型无需重新导入。

已经启用的外部页面可原路径热替换，无需重启 Manager Server。首次启用外部页面或改变其路径，需要重启 Manager Server；旧宿主和手动覆盖被加载的动态库按宿主要求重启。

**完整周期快照需要 Manager Server 的平台名单接受 `commandcode`。** 包内 `integrations/cpamp/apply.py` 提供服务器补丁与回归测试，需在现有 Manager Server 的同一源码版本上应用、重建并部署；页面更新入口不会替换该服务器。已经应用该补丁的实例无需重复重建。

## 下载内容

提供 Linux amd64、Linux arm64、Windows amd64、macOS arm64 四平台 ZIP，以及 `commandcodebridge_0.2.8_cpamp-web.zip`、各自 SHA256 文件和统一 `checksums.txt`。各包 `BUILD.json` 记录来源提交和摘要，四平台插件内嵌页面与前端包一致。

动态库按 CPA 服务器平台选择，本地 OAuth 接收器按浏览器电脑平台选择。Linux 包要求 glibc ≥2.34；Alpine/musl 需另行构建。CPAMP 页面基于 `dbf61dd`，不同版本请在匹配源码应用适配。

插件商店源：[registry.json](https://raw.githubusercontent.com/Gaoshou101/CommandCodeBridge/main/registry.json)。

## 验证与边界

发布流程执行 Go race/vet、前端类型检查和相关回归、四平台 CPA v8.0.23 原生协议及异常恢复联测，以及 0.2.7 → 0.2.8 插件热升级和 CPAMP 页面安装／热更新检查。统一校验来源、安装包和内嵌页面一致性。

页面现场验证已确认 GOAT 87%、真实月余额、桌面两种视图顶部对齐、390px 深色卡片无溢出，窗口历史与快照返回 200，浏览器无脚本错误。

模型目录可见不代表套餐允许调用。Claude 原生 Messages、图片、签名和 ZDR 的真实验收仍需具备相应模型权限；原生协议模拟联测不代表这些场景都已使用真实账户验收。Go 订阅 `/alpha/generate` 与固定上游 provider 模式不在本插件范围。

详见[部署说明](./DEPLOYMENT.md)、[原生集成](../integrations/cpamp/README.md)、[验证记录](./VALIDATION.md)和[更新记录](../CHANGELOG.md)。
