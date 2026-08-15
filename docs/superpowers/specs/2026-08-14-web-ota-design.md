# Web OTA — 跨平台 UI 热更新设计

**日期**: 2026-08-14（2026-08-15 修订 R2：发布触发改 `webapp/x.y.z` tag + app 发版联动；版本号第 4 段改时间基构建号；`min_*` 语义从"当前要求"改为**支持地板（support floor）**——兼容哲学转为"最新 webapp 运行时探测 app 能力、支持地板以上全部 app"。修订涉及 §1/§3/§3.1/§4/§6/§7/§8/§10，客户端三端闸门代码不变）
**状态**: 已批准（设计决策已获用户确认：激进模式 / tag 触发全自动发布 / 静默下次启动生效 / 方案 A / R2 兼容哲学：单一最新 webapp 支持地板以上全部 app 版本）
**执行**: 实现 plan 交由 Sonnet 5 开发
**Bootstrap 就绪度**（Task 11，2026-08-15 复核；详情见 `.superpowers/sdd/2026-08-14-web-ota-pipeline-mobile/task-11-report.md`）：Task 1-10 全量代码就绪，全量测试矩阵绿（webapp ×2 品牌、tsc ×2、manifest 脚本单测、Android `k2-plugin` JVM 单测、iOS `K2Tests` 60/60 含 `MinisignVerifierTests`），本地用**一次性 throwaway minisign 密钥**（非生产密钥）跑通了 zip→签名→manifest 生成→`sig` 字段 base64 往返解码→独立验签的全链路。§8 步骤 1 锚点复核：`definitions.ts` 最近一次方法新增仍是 `5086d1f1`（`updateConfig`，首见 tag `v0.4.8`）；同分支后续 `99c1bc75` 只给已有方法 `checkReady` 加了一个可选返回字段，未发布过（无 tag），不构成新的方法集合版本，`bridge-versions.json["1"].native=0.4.8` 锚点不变。§8 步骤 2 现状复核：线上 `kaitu/android/latest.json` 与 `kaitu/ios/latest.json` 均仍是 0.4.7 ——**首个 OTA 会被全部存量 native 正确跳过**，符合设计预期，非故障。**代码信心 9/10；业务信心（移动端 OTA 全链路可用）封顶 6-7/10**（release confidence framework：无真机 smoke）——merge、beta/stable 发布、真机 UAT（§8 步骤 3-7，含 CDN 侧独立验签、Android/iOS 正负样本、回滚链路、iOS 无 channel 能力下的 stable-only 验证）仍是**人类必做步骤**，未执行。

## 1. 目标

一套 `webapp/dist` 作为全平台唯一 UI 真理源，通过 CDN 热更新到达四个宿主（iOS / Android / macOS+Windows Tauri / Linux go:embed），实现：

- **快速迭代 + 紧急修复**：UI 改动打 `webapp/x.y.z` tag 当天到达全端用户，不等 App Store / 桌面发版周期。
- **完全绕开发版周期**：native 壳退化为薄封装，新功能默认走热更下发；native 发版只在 bridge 接口变更 / 内核升级时发生。
- **管理简单**：发布 = 打 `webapp/x.y.z` tag（CI 全自动构建、签名、上传、更新 manifest）；app 发版（`v*` tag）时自动联动发布同 commit 的 web bundle；回滚 = `git revert` + 重新打 tag。
- **生效时机**：静默后台下载 → 校验 → 落盘，下次冷启动生效；启动失败自动回滚。

非目标（v1 明确不做）：灰度百分比控制面、antiblock 通道承载 UI bundle（保留为二期）、热更 native 代码。

## 2. 现状（设计依据）

- **移动端已有完整自研 Web OTA**，线上 native 至今每次冷启动仍在轮询 manifest：
  - iOS `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift`、Android `.../K2Plugin.kt`：下载 zip → sha256 校验 → 解压 → `setServerBasePath` 切换 → `.boot-pending` 标记 + webapp `checkReady()` 清除实现启动回滚 → `web-backup` 备份。
  - 兼容闸门 `min_native`（`K2PluginUtils.kt:43` / `K2Helpers.swift:49`）：base 版本数字段比较，空值放行。
  - 已应用版本记录在 `web-update/version.txt`，缺省回退 app 版本；仅当 `isNewerVersion(remote, local)` 时应用。
  - manifest 端点 `{CDN}/{brand}/web/latest.json`（beta: `web/beta/latest.json`），CDN base 由 xcconfig / flavor 编译期烘焙。
  - **2026-03 事故**（`git show c27fd926`）：热更 webapp 调用了旧 native 没有的 `storageGet` → auth 全挂。根因是 `min_native` 人工填写。发布管线被封（`Makefile:405`、`scripts/ci/upload-release.sh:11,51`），manifest 至今 404。
- **桌面 Tauri 无热更能力**：`frontendDist: "../../webapp/dist"` 打进 bundle，默认 `tauri://localhost`（macOS）/ `http://tauri.localhost`（Windows）协议。已有自定义协议先例（`icon_protocol.rs`）、minisign updater 基础设施（`updater.rs` + `channel.rs` 双通道）。
- **Linux**：`k2/webui/embed.go` go:embed 死绑二进制；`serve.go NewWebappHandler` 结构清晰可加磁盘覆盖。k2 是只读 submodule，改动须走 k2 仓库。
- **单一真理源已成立**：`webapp/src/main.tsx:91-114` 运行时检测宿主，动态 import 对应 bridge，一份 dist 通吃；品牌构建期烘焙（`__K2_BRAND__`），两品牌各出一份产物。
- **桌面存储事实**（实现阶段核实修正）：桌面的 auth token / UDID 走 `_platform.storage` → Rust 侧 `storage.json`，**与 origin 无关**；localStorage 里只有偏好项（`kaitu-language`、`k2_log_level`、`k2_developer_mode`、`kaitu_cache:*`、公告已读、拖拽位置）。换加载协议不会登出用户，§5.2 的迁移仅为偏好连续性（低风险）。
- 契约：webapp 只通过 `window._k2` / `window._platform` 访问 native；Capacitor 侧约 30 个具名方法（`mobile/plugins/k2-plugin/src/definitions.ts`）是兼容风险的集中点。

## 3. 总体架构

```
git tag webapp/x.y.z（或 app 发版 v* tag 联动触发）
  → CI: test + brand-purity + 契约守卫 + 双品牌构建 + 白屏冒烟
  → 打包 web.zip ×2 (kaitu/overleap) + sha256 + minisign 签名
  → 上传 s3://d0.all7.cc/{brand}/web/{version}/web.zip
  → 写 {brand}/web/latest.json (+ beta 通道按需)
  → CloudFront invalidation (E3W144CRNT652P + E34P52R7B93FSC)

四宿主统一语义：轮询 manifest → 闸门校验(min_*) → 下载 → sha256+minisign 校验
  → 解压 pending → 原子切换 → 下次启动加载 → 启动失败自动回滚
```

### 3.1 UI 版本号

`{package.json version}.{时间基构建号}`，如 `0.4.9.19526400`。构建号 = **发布时刻的 Unix 秒 − 1767225600（2026-01-01T00:00:00Z）**，由 manifest 生成脚本在 CI 运行时计算。

- **全局单调**：跨 workflow（webapp tag / release-desktop / build-mobile 三处都可能触发发布）、跨 ref 都成立——commit count 方案在"给旧 commit 打 tag / dispatch 重发旧 ref"时会算出更小的第 4 段，被 `isNewerVersion` 拒收，回滚即废；时间基没有这个问题。
- 前三段取自根 `package.json`（version source of truth，tag 只是触发器，见 §6），全自动、无人工 bump。
- 数值上界：构建号年增约 3150 万，Kotlin `Int`（移动端 `toIntOrNull`）上限 21 亿 → 2090 年前无溢出；Go/Swift/JS 侧余量更大。
- 与移动端现有 `isNewerVersion` 数字段比较兼容（缺段补 0）；native 新装机的本地回退值是 3 段 app 版本，比较语义正确。
- 并发发布由 workflow 的 `concurrency: publish-web-ota`（排队不取消）串行化，后跑的 run 构建号必然更大，`latest.json` 最终写入者即最新。

### 3.2 Manifest（向后兼容，只增不改）

端点不变：`{CDN}/{brand}/web/latest.json`（beta：`web/beta/latest.json`）。存量移动 native 依赖的旧字段形状**一个都不能动**：

```json
{
  "version": "0.4.8.1234",
  "url": "0.4.8.1234/web.zip",
  "hash": "sha256:<hex>",
  "size": 1234567,
  "released_at": "2026-08-14T12:00:00Z",
  "min_native": "0.4.8",

  "sig": "<minisign signature of web.zip>",
  "min_bridge": 1,
  "min_desktop": "0.4.9",
  "min_linux": "0.4.9"
}
```

- `url`：**相对 manifest 自身所在目录**解析（线上移动 native `resolveDownloadURL` 的既成语义，`K2PluginUtils.kt:83` / `K2Plugin.swift:1197`——不可改）。beta manifest 位于 `web/beta/latest.json`，故 CI 须把 zip 同步复制到 `web/beta/{version}/web.zip` 使同一相对形式两处可解析。
- `min_native`：存量移动闸门，CI 从支持地板文件自动推导（§4），不再人工填写；语义 = webapp 仍支持的**最老** app 版本，不是"这版 webapp 的要求"。
- `sig`：web.zip 的 minisign 签名，格式 = **base64(整个 .minisig 文件内容)**（标准 minisign prehashed "ED"：Ed25519 over BLAKE2b-512(file)，与现有 `release-desktop.yml` 签 Linux tarball 的方式同源）。密钥**复用 Tauri updater 现有密钥对**（`tauri.conf.json` pubkey / CI secret `TAURI_SIGNING_PRIVATE_KEY`，两品牌共用一把），不引入新密钥。
- `min_desktop` / `min_linux`：新消费者的壳版本闸门。
- 旧移动 native 忽略未知字段（additive JSON，安全）。

### 3.3 Bundle 格式

`web.zip` = `webapp/dist` 全量（`index.html` + `debug.html` + 指纹资源），每品牌一份。zip 顶层即 dist 内容（无外层目录）。解压后校验 `index.html` 存在方可切换。

### 3.4 校验策略

| 消费者 | sha256 | minisign |
|---|---|---|
| 存量移动 native（已发布，不可改） | ✅ | ❌（HTTPS + manifest hash 兜底，接受残余风险） |
| 新版移动 native | ✅ | ✅ |
| 桌面 Rust（新开发） | ✅ | ✅ 强制 |
| Linux Go（新开发） | ✅ | ✅ 强制 |

## 4. 兼容模型（R2）：支持地板 + 运行时能力探测

**兼容哲学**：只有一个真理的 webapp 版本——最新版。最新 webapp 必须在**支持地板（support floor）以上的所有 app 版本**上正确运行，靠 webapp 代码内的运行时能力探测分支实现（浏览器世界的 feature detection 模型）。`min_*` 闸门从"常规兼容手段"降级为**安全刹车**：正常发布时值恒等于地板、几乎不动；只在主动砍掉过老版本支持、或灾难场景（新 webapp 依赖老 WebView 内核根本不具备的浏览器能力，运行时分支救不了）时才 bump。

1. **单一整数版本（保留）**：`webapp/src/types/bridge-version.ts` 导出 `BRIDGE_API_VERSION`（当前 = 1）。它覆盖整个 `_k2` / `_platform` 契约面（Capacitor 具名方法表、Tauri 具名 command、daemon HTTP action），方法表任何增删都必须 bump。**R2 语义变化**：bump 的后果不再是"老 app 被 manifest 拒之门外"，而是"webapp 必须为新能力加运行时探测分支"——manifest 的 `min_bridge` 发地板值，不发当前值。
2. **支持地板文件**：`contracts/webapp-support-floor.json`（进 git，替代原 `contracts/bridge-versions.json` 映射表）：`{ "native": "0.4.8", "desktop": "0.4.9", "linux": "0.4.9", "bridge": 1 }` —— webapp 仍支持的最老各壳版本 + 最老 bridge 版本。初始锚定各壳**首个携带 OTA 客户端能力的版本**（比它老的 app 本来就不轮询/不校验，谈不上支持）。**bump 地板 = 一次显式的"砍旧版本支持"决策**，须走 review；被砍的版本被闸门冻结在当前 UI，升级通道回到 app 自身 updater。
3. **CI 契约守卫**（沿用 `contracts/api-contract.json` 门文化）：
   - `mobile/plugins/k2-plugin/src/definitions.ts` 的方法集合快照进 golden 文件；方法集合变更而 `BRIDGE_API_VERSION` 未 bump → 测试红。
   - Tauri 具名 command 表同理纳入快照（`main.rs` `generate_handler!` 清单）。
   - 地板文件形状校验 + 不变式 `floor.bridge ≤ BRIDGE_API_VERSION`（地板不可能高于当前编译面）→ 违反测试红。
4. **发布时推导**：CI 从地板文件推导 `min_native` / `min_desktop` / `min_linux` / `min_bridge` 写入 manifest，全程零人工；manifest 生成器在地板文件缺失/形状非法时硬失败。
5. **webapp 侧运行时探测规矩**（本次先立规矩，首个 >v1 bridge 能力出现时落地代码）：
   - 新 bridge 能力的使用必须经过**能力探测**，优先存在性检测（`typeof fn === 'function'`）而非版本比较——存在性检测天然免疫"TS 声明了但某平台 native 未实现"的平台漂移盲区（§10 已证实形状，例：iOS 0.4.8 缺 channel 方法）。
   - 探测收敛到唯一供给者 `webapp/src/services/capabilities.ts`（届时新建）：启动时读 `window._platform.version` + 平台类型 + 方法存在性，导出语义化 flag；业务代码禁止散落 raw 版本比较或直接探测 `window._k2` 方法（届时配 grep 守卫进 CI，同 brand-purity 守卫做法）。现有先例 `capacitor-k2.ts` 的 `getPlatform() === 'android'` 门在 capabilities.ts 落地时收编。
   - 规矩落点：`webapp/CLAUDE.md` 兼容章节 + `bridge-version.ts` 顶部注释。
6. **纵深防御**：三个新壳在本地记录自身编译期的 bridge 版本，apply 前双重校验 `min_bridge`；webapp 侧 bridge 调用统一 try/catch 并给出可诊断错误（不作为主防线）。
7. **接受的代价**（诚实记录）：回归风险从"老 app 冻结"换成"最新 webapp 在老 app 上没有 CI 全量验证"。缓解：capabilities 层单测穷举模拟各版本注入形态（缺方法/有方法）；发版真机 smoke 保留一台地板版本设备；地板政策是"支持 field 上仍有量的版本"而非字面永远——定期看 device stats 砍地板，砍的动作即 bump 地板文件，闸门自动冻结被砍版本，机制闭环。

## 5. 平台实现

### 5.1 移动端（复活 + 加固）

存量 native 无需任何改动即可工作（发布 manifest 即激活）。新版 native 增量加固：

- `applyWebUpdate` 增加 minisign 验签（manifest 有 `sig` 且本地支持则强制）。
- apply 前校验 `min_bridge` vs 编译期 bridge 版本。
- `checkReady` 返回值增加 `bridgeVersion` 字段（webapp 可观测）。
- 自动检查时序保持现状：冷启动 +3s，native 更新优先于 web OTA。
- 目录/回滚语义（`Documents/web-update`、`.boot-pending`、`web-backup`）保持不变——已部署，不动。

### 5.2 桌面 Tauri（新开发）

**加载**：新自定义协议 `kaitu-ui://`（参照 `icon_protocol.rs` 先例）。Handler 逻辑：`$APPDATA/web-ota/current/` 存在且有效 → 从磁盘 serve；否则用 `app.asset_resolver()` serve 内嵌资源。含 MIME 推断 + SPA fallback 到 index.html。窗口 URL 恒定指向该协议（有无 OTA 都走同一条代码路径）。

**OTA 模块** `desktop/src-tauri/src/web_ota.rs`：

- 轮询与现有 updater 同节奏（启动 +5s，随后 30min），复用 `channel.rs` 的 stable/beta 与品牌 CDN base，拉 `{brand}/web[/beta]/latest.json`。
- 闸门：`min_desktop` vs app 版本、`min_bridge` vs 编译期常量、`isNewerVersion(remote, local)`（本地版本 = `web-ota/current/version.txt`，缺省回退 app 版本——与移动端语义一致）。
- 下载到 `web-ota/pending/` → sha256 + minisign（强制）→ 解压 → 校验 index.html → 原子 rename：`current → previous`、`pending → current`。
- **启动回滚**：加载磁盘 UI 前写 `.boot-pending` 标记；webapp 启动成功后调用新 Tauri command `ui_boot_ok` 清除（`tauri-k2.ts` 初始化时调用，try/catch 兼容旧壳）。下次启动若标记仍在 → 判定上次白屏 → `current` 移入 `quarantine/`，回退 `previous/` 或内嵌资源。

**origin 迁移（低风险，偏好连续性）**：换协议 = 换 origin，localStorage 会清空。核实后确认桌面 auth/UDID 在 Rust 侧 `storage.json`（origin 无关），localStorage 仅偏好项（语言、日志级别、公告已读等）——迁移失败最坏结果是偏好重置，**不会登出**。迁移设计保留（随引入 web-ota 能力的那一次桌面发版一并完成，一次性）：

1. 新增 Rust command `storage_migration_put(json)` / `storage_migration_get()` / `storage_migration_clear()` / `storage_migration_done()`（数据落 app data 目录文件）。
2. Rust 启动时查 `storage-migrated` 标志：未迁移 → 主窗口先加载**旧 origin** 的内嵌 UI 并带 `?migrate=export`；bundled webapp 检测该参数，dump 全部 localStorage 调 `storage_migration_put`，完成后调 `storage_migration_done` → Rust 置标志并 navigate 到 `kaitu-ui://`。
3. 新 origin webapp 启动时：localStorage 为空且 `storage_migration_get` 有数据 → 导入 → `storage_migration_clear`。
4. 失败兜底：迁移任何一步失败 → 直接进新 origin（偏好重置，登录态无损）。迁移代码在下一个大版本移除。

### 5.3 Linux（k2 submodule，独立仓库工作）

- `k2/webui/serve.go`：`NewWebappHandler` 增加 override 目录参数（`/etc/kaitu/web-ota/current`——锚定 `daemon/webui_linux.go` 现有 `webuiStateDir` 常量，非 config 包），目录存在且含 index.html → 从磁盘 serve（保持现有 SPA fallback / `__K2_GATEWAY__` 注入 / 缓存头逻辑），否则 embed。
- 新 `k2/webui/webota.go`：镜像 `Upgrader` 模式的轮询器——拉 `{CDN}/kaitu/web/latest.json`（端点列表沿用 `daemon/webui_linux.go` 的双域名模式，路径改 `/kaitu/web`），闸门 `min_linux` / `min_bridge`，sha256 + minisign（Go 侧用 `aead/minisign`）校验，解压 pending → 原子 swap。
- **生效语义**：server 侧 swap 后新页面加载即新 UI（比"下次启动"更快，可接受）；无 `.boot-pending`（没有 app 启动概念）。
- **应急逃生口**：`?ui=embedded` query 参数强制本次会话 serve 内嵌 UI（支持排障）；坏 bundle 的正式回滚 = manifest revert 重发。
- k2 只读约束：以上改动在 k2 仓库的 feature 分支完成 → merge → 本仓 bump submodule 指针。

### 5.4 各平台生效路径汇总

| 平台 | 加载机制 | 生效 | 回滚 |
|---|---|---|---|
| iOS/Android | `setServerBasePath`（origin 不变） | 下次冷启动 | `.boot-pending` + web-backup（已部署） |
| macOS/Windows | `kaitu-ui://` 协议，磁盘优先内嵌兜底 | 下次启动 | `.boot-pending` + previous/ + 内嵌兜底 |
| Linux | daemon serve 磁盘优先内嵌兜底 | 下次页面加载 | manifest revert + `?ui=embedded` 逃生口 |

## 6. CI 发布管线

新 workflow `.github/workflows/publish-web-ota.yml`，三种触发入口共用同一个 publish job：

- **触发 1 — `webapp/x.y.z` tag（常规发布）**：`push.tags: ['webapp/*']`。CI 校验 tag 里的 `x.y.z` 必须等于根 `package.json` 的 version，不一致 fail loud——tag 是触发器和人为确认，**不是第二个版本真相源**（version source of truth 恒为 package.json）。
- **触发 2 — app 发版联动（`workflow_call`）**：`release-desktop.yml` 与 `build-mobile.yml` 在各自构建成功后以 `uses: ./.github/workflows/publish-web-ota.yml` + `secrets: inherit` 调用，从**同一 tag commit** 构建并发布 web bundle（channel 恒 stable）。价值：app 发版后 CDN latest 自动追平该 app 内嵌的 UI，新装设备不出现"CDN 比内嵌还旧"的倒挂；且同 commit 构建，兼容天然成立。任一平台构建失败则跳过联动发布。
- **触发 3 — `workflow_dispatch`（应急）**：inputs `ref`（任意 commit 重发/回滚）、`channel`（stable|beta，默认 stable）。
- **并发**：`concurrency: publish-web-ota`（排队不取消）挪到 job 级（workflow 级 concurrency 在被 `workflow_call` 调用时不生效）。
- **多品牌**：每次发布**恒双品牌**（kaitu + overleap 各自 build → purity gate → 签名 → 各自 S3 树 + manifest），与触发入口无关——即使触发者是 `v*-overleap` 单品牌 app tag 也发双品牌（同 commit 构建无害，换来两品牌 web 版本永不漂移）。两品牌共享同一版本号与同一地板文件（同一份代码，刻意为之）。
- **步骤**：
  1. `yarn install`（root workspace）→ `cd webapp && yarn test`（vitest）
  2. 契约守卫测试（§4）+ `check-brand-purity.sh`
  3. 双品牌构建：`K2_BRAND=kaitu` / `K2_BRAND=overleap` 各 build 一次
  4. **白屏冒烟门**：每份 dist 起静态 server + headless Chromium（Playwright）加载 `index.html`，断言 app shell 渲染（根节点非空）且无未捕获异常——防止把白屏 bundle 推给全量存量用户（standalone bridge 分支下跑，native 调用失败必须降级不崩）
  5. 版本号推导（§3.1）+ zip + sha256 + minisign 签名（私钥 = 现有 `TAURI_SIGNING_PRIVATE_KEY` secret）
  6. 上传 S3 `{brand}/web/{version}/web.zip` → 写 `{brand}/web[/beta]/latest.json`（`min_*` 从地板文件推导）→ 两个 CloudFront 分发 invalidation
- **解封现有封堵**：移除 `scripts/ci/upload-release.sh:11,51` 的 `--web` 硬报错（改为指向新 workflow），保持 `publish-mobile.sh` 不碰 web manifest。
- **通道策略**：`webapp/*` tag 与 app 联动 → stable 全自动；beta 通道保留给 `workflow_dispatch` 手动指定（客户端四端都已/将支持 beta manifest 路径）。stable 发布保持超集语义：恒写 `beta/` 目录的 zip + manifest，`channel=stable` 时再额外写顶层。

## 7. 回滚与应急

- **常规回滚**：`git revert <坏 commit>` → 重新打 `webapp/x.y.z` tag → CI 用更高的时间基构建号重发旧内容。
- **应急重发**：`workflow_dispatch` + `ref=<已知好 commit>`。R2 时间基构建号下，重发旧 ref 也拿到**更高**的第 4 段版本号——只要该 ref 的 `package.json` 前三段没有低于当前线上（通常同版），**已中招设备也会被拉回**，dispatch 重发从"只保护未中招设备"升级为完整回滚手段。唯一例外：好 commit 的前三段低于坏版本（跨 package.json bump 回滚）时 base 比较更小、被跳过——此时只能走 revert + 重打 tag。
- **Incident playbook：仍然 revert-first**——`git revert` + 重打 tag 让 main 与线上内容保持一致（tag 即审计记录）；`workflow_dispatch` 重发是止血捷径，用后必须补 revert，否则下一个常规 tag 会把坏内容重新发出去。
- **核按钮**：删除/清空 `{brand}/web/latest.json`（native 拿不到 manifest 即停止更新，已应用的保持现状）；单设备逃生：移动端删 app 数据 / 桌面删 `web-ota/` 目录 / Linux `?ui=embedded`。

## 8. 首次启用（bootstrap 顺序）

存量移动 native 一直在轮询 manifest，**首次发布即触达全量移动用户**，顺序必须严格：

1. 落地契约守卫 + `BRIDGE_API_VERSION=1`，支持地板文件锚定 `native: 0.4.8`（实现阶段已从 git 历史核实：`definitions.ts` 完整方法集含 `updateConfig` 首见于 v0.4.8，commit `5086d1f1`）。
2. **已知时间线约束**：线上移动 manifest 仍是 0.4.7，`min_native=0.4.8` 的首个 OTA 会被在网 0.4.7 存量 native 正确跳过——热更触达面随 0.4.8 移动端发版铺开，这是闸门的预期行为而非故障。
3. 首个 OTA bundle 内容 = 与当前线上 native 兼容的 main 头（冒烟门通过）。
4. **先发 beta 通道**，在真机（iOS + Android，新旧两个 native 版本）UAT 验证下载/校验/切换/回滚全链路。注意：iOS native 现状缺 update channel 支持（`getUpdateChannel`/`setUpdateChannel` 未实现、web manifest 端点硬编码 stable）——iOS 的 beta-first UAT 依赖 pipeline 计划 Task 10 补齐并随 native 发版到位；在此之前 iOS 侧用 Android beta 结果 + iOS stable 灰观察替代。
   - **Beta-clobber 注意**（`publish-web-ota.yml` 步骤"Upload to S3 + publish manifests"）：stable 发布是超集语义——无论 `channel` 输入是什么，每次运行都会先写 `beta/latest.json`，只有 `channel=stable` 时才**再额外**写顶层 `latest.json`；而 tag / app 联动触发的运行恒为 `channel=stable`。R2 改 tag 触发后，日常 main merge 不再自动发布，clobber 面大幅收窄——但在 beta UAT 窗口期间，**任何 `webapp/*` tag 或 app 发版 `v*` tag** 仍会覆盖 `beta/latest.json`。窗口期内应冻结这两类 tag，或事后 `workflow_dispatch` + `channel=beta` + `ref=<UAT 目标 commit>` 恢复 UAT 内容。
5. 再发 stable。桌面/Linux 能力随各自下一次壳发版上线，不阻塞移动端先行。

## 9. 测试策略

- **单元**：Rust（web_ota 校验/原子切换/回滚状态机，mock 文件系统）、Go（serve override 优先级、poller 闸门/校验，k2 仓库内）、TS（契约守卫、版本推导脚本）。
- **契约门**：bridge 方法表快照 golden（`-count=1` 同款纪律：golden 只读、进 git）。
- **冒烟门**：CI 每次发布前的 headless 白屏检测（§6 步骤 4）。
- **变异验证**（`feedback_green_test_may_never_reach_its_target`）：契约守卫写完后，手动往 `definitions.ts` 加一个方法不 bump 版本，确认测试真的红；冒烟门用一个故意抛错的 index.html 确认真的拦。
- **UAT 矩阵**：iOS/Android 真机（新旧 native × 首次应用/增量更新/坏包回滚）、macOS/Windows（origin 迁移前后偏好保持 + 登录态回归验证）、Linux（磁盘覆盖 + 逃生口）。桌面/移动 bugfix 无真机 smoke 信心封顶 6-7（release confidence framework）。

## 10. 风险登记

| 风险 | 缓解 |
|---|---|
| 存量移动 native 只验 sha256，CDN 被攻破可注入 UI | HTTPS + 新壳强制 minisign；接受残余风险并记录 |
| 桌面 origin 迁移失败 → 偏好重置（核实后确认不会登出，auth 在 Rust storage.json） | 迁移失败兜底为直接进新 origin；UAT 覆盖迁移路径 |
| iOS 3.3.2 审核风险（热更显著改变功能） | 用户已知情选择激进模式；bundle 不改变 app 宣称用途；保留随时停发能力（核按钮） |
| 每次 `webapp/*` tag / app 发版直达全量用户 | 白屏冒烟门 + 契约守卫为强制闸；tag 是人为确认动作（R2 已从 main-push 自动收紧到 tag 触发）；重大改版可先 dispatch 到 beta |
| R2 兼容负担转移：最新 webapp 须在地板以上全部 app 版本运行，但 CI 只验证最新组合 | capabilities 层单测穷举模拟各版本注入形态；真机 smoke 保留一台地板版本设备；地板政策"支持 field 上仍有量的版本"，定期砍地板收敛分支数（§4.7） |
| 地板 bump 被随手当成兼容手段（回退到 R1 冻结哲学） | 地板文件 bump 必须走 review 并给出"砍支持"理由；契约守卫强制 `floor.bridge ≤ BRIDGE_API_VERSION`，且 bump `BRIDGE_API_VERSION` 不再要求同步改地板 |
| bridge 守卫盲区：两种已证实形状——① 行为变更不改方法签名 ② 平台级实现漂移：TS 声明不变而某一平台的 native 未实现该方法（例：bridge v1 的 channel 方法，iOS 0.4.8 未实现，仅 Android 0.4.8 实现） | 诚实记录：守卫只覆盖方法表增删，两种形状都靠 review + bump 纪律；②见 `bridge-version.ts` 顶部 iOS caveat 注释 + `capacitor-k2.ts` 的 `getPlatform() === 'android'` 门 |
| 首发 min_native 锚错（重演 2026-03） | §8 步骤 1 要求从 git 历史核实方法集合完整版本，宁高勿低；先 beta 后 stable |
| manifest 本身未签名，CDN/TLS 位置攻击者可剥离 `sig` 并重算 `hash`——新版 native 回退到纯 sha256 legacy 路径，接受攻击者内容 | 首个签名 manifest 上线且 0.4.9+ native 铺开后，新版 native 应无条件 REQUIRE `sig`（CI 恒发，不再"有则验"）；列为下一次移动端发版的第一个 OTA 加固项 |
| Mix-and-match 版本钉死：`sig` 只覆盖 web.zip，不覆盖 `version` 字段——攻击者可拼接"旧的、真实签名过"的 zip + 有效 `sig` + 匹配 `hash` + 伪造的巨大 `version`，native 校验通过并应用，把 `web-update/version.txt` 钉在高位，此后所有合法更新都被判定更旧（设备冻结在旧 UI，直到 app 数据重置） | v2 改进：签名 manifest 本身，或把 `version` 纳入签名数据 |
