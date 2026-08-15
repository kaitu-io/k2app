# Web OTA — 跨平台 UI 热更新设计

**日期**: 2026-08-14
**状态**: 已批准（设计决策已获用户确认：激进模式 / git push 全自动发布 / 静默下次启动生效 / 方案 A）
**执行**: 实现 plan 交由 Sonnet 5 开发

## 1. 目标

一套 `webapp/dist` 作为全平台唯一 UI 真理源，通过 CDN 热更新到达四个宿主（iOS / Android / macOS+Windows Tauri / Linux go:embed），实现：

- **快速迭代 + 紧急修复**：UI 改动 merge 到 main 当天到达全端用户，不等 App Store / 桌面发版周期。
- **完全绕开发版周期**：native 壳退化为薄封装，新功能默认走热更下发；native 发版只在 bridge 接口变更 / 内核升级时发生。
- **管理简单**：发布 = `git push`（CI 全自动构建、签名、上传、更新 manifest）；回滚 = `git revert` + push。
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
git push main (webapp/** 变更)
  → CI: test + brand-purity + 契约守卫 + 双品牌构建 + 白屏冒烟
  → 打包 web.zip ×2 (kaitu/overleap) + sha256 + minisign 签名
  → 上传 s3://d0.all7.cc/{brand}/web/{version}/web.zip
  → 写 {brand}/web/latest.json (+ beta 通道按需)
  → CloudFront invalidation (E3W144CRNT652P + E34P52R7B93FSC)

四宿主统一语义：轮询 manifest → 闸门校验(min_*) → 下载 → sha256+minisign 校验
  → 解压 pending → 原子切换 → 下次启动加载 → 启动失败自动回滚
```

### 3.1 UI 版本号

`{package.json version}.{git commit count}`，如 `0.4.8.1234`（commit count = `git rev-list --count HEAD`）。

- 单调递增、全自动、无人工 bump。
- `git revert` 后重发产生**更高**版本号，天然规避"回滚被判降级"。
- 与移动端现有 `isNewerVersion` 数字段比较兼容（缺段补 0）；native 新装机的本地回退值是 3 段 app 版本，比较语义正确。

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
- `min_native`：存量移动闸门，CI 自动推导（§4），不再人工填写。
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

## 4. 兼容闸门：BRIDGE_API_VERSION 自动推导

2026-03 事故的根因修复——把兼容判定从"人记得"变成"CI 不通过"：

1. **单一整数版本**：`webapp/src/types/bridge-version.ts` 导出 `BRIDGE_API_VERSION`（初始 = 1）。它覆盖整个 `_k2` / `_platform` 契约面（Capacitor 具名方法表、Tauri 具名 command、daemon HTTP action），任何 bridge 面**破坏性或新增依赖**变更都必须 bump。
2. **映射表**：`contracts/bridge-versions.json`（进 git）：`{ "1": { "native": "0.4.8", "desktop": "0.4.9", "linux": "0.4.9" } }` —— bridge 版本 → 首个搭载它的各壳版本。
3. **CI 契约守卫**（沿用 `contracts/api-contract.json` 门文化）：
   - `mobile/plugins/k2-plugin/src/definitions.ts` 的方法集合快照进 golden 文件；方法集合变更而 `BRIDGE_API_VERSION` 未 bump → 测试红。
   - `bridge-versions.json` 缺当前 `BRIDGE_API_VERSION` 条目 → 测试红。
   - Tauri 具名 command 表同理纳入快照（从 `main.rs` 的 `generate_handler!` 清单提取或维护镜像清单 + 守卫）。
4. **发布时推导**：CI 从 `BRIDGE_API_VERSION` + 映射表推导 `min_native` / `min_desktop` / `min_linux` / `min_bridge` 写入 manifest，全程零人工。
5. **纵深防御**：三个新壳在本地记录自身编译期的 bridge 版本，apply 前双重校验 `min_bridge`；webapp 侧 bridge 调用统一 try/catch 并给出可诊断错误（不作为主防线）。

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

新 workflow `.github/workflows/publish-web-ota.yml`：

- **触发**：push 到 main 且 paths 含 `webapp/**` 或 `contracts/bridge-versions.json`；另支持 `workflow_dispatch`（inputs: `ref` 任意 commit 重发/应急回滚、`channel` stable|beta，默认 stable）。
- **步骤**：
  1. `yarn install`（root workspace）→ `cd webapp && yarn test`（vitest）
  2. 契约守卫测试（§4）+ `check-brand-purity.sh`
  3. 双品牌构建：`K2_BRAND=kaitu` / `K2_BRAND=overleap` 各 build 一次
  4. **白屏冒烟门**：每份 dist 起静态 server + headless Chromium（Playwright）加载 `index.html`，断言 app shell 渲染（根节点非空）且无未捕获异常——防止把白屏 bundle 推给全量存量用户（standalone bridge 分支下跑，native 调用失败必须降级不崩）
  5. 版本号推导（§3.1）+ zip + sha256 + minisign 签名（私钥 = 现有 `TAURI_SIGNING_PRIVATE_KEY` secret）
  6. 上传 S3 `{brand}/web/{version}/web.zip` → 写 `{brand}/web[/beta]/latest.json`（`min_*` 从映射表推导）→ 两个 CloudFront 分发 invalidation
- **解封现有封堵**：移除 `scripts/ci/upload-release.sh:11,51` 的 `--web` 硬报错（改为指向新 workflow），保持 `publish-mobile.sh` 不碰 web manifest。
- **通道策略**：main → stable 全自动（用户已确认零手工）；beta 通道保留给 `workflow_dispatch` 手动指定（客户端四端都已/将支持 beta manifest 路径）。

## 7. 回滚与应急

- **常规回滚**：`git revert <坏 commit>` → push → CI 自动发更高版本号的旧内容。
- **应急重发**：`workflow_dispatch` + `ref=<已知好 commit>`。
- **核按钮**：删除/清空 `{brand}/web/latest.json`（native 拿不到 manifest 即停止更新，已应用的保持现状）；单设备逃生：移动端删 app 数据 / 桌面删 `web-ota/` 目录 / Linux `?ui=embedded`。

## 8. 首次启用（bootstrap 顺序）

存量移动 native 一直在轮询 manifest，**首次发布即触达全量移动用户**，顺序必须严格：

1. 落地契约守卫 + `BRIDGE_API_VERSION=1`，映射表锚定 `native: 0.4.8`（实现阶段已从 git 历史核实：`definitions.ts` 完整方法集含 `updateConfig` 首见于 v0.4.8，commit `5086d1f1`）。
2. **已知时间线约束**：线上移动 manifest 仍是 0.4.7，`min_native=0.4.8` 的首个 OTA 会被在网 0.4.7 存量 native 正确跳过——热更触达面随 0.4.8 移动端发版铺开，这是闸门的预期行为而非故障。
3. 首个 OTA bundle 内容 = 与当前线上 native 兼容的 main 头（冒烟门通过）。
4. **先发 beta 通道**，在真机（iOS + Android，新旧两个 native 版本）UAT 验证下载/校验/切换/回滚全链路。注意：iOS native 现状缺 update channel 支持（`getUpdateChannel`/`setUpdateChannel` 未实现、web manifest 端点硬编码 stable）——iOS 的 beta-first UAT 依赖 pipeline 计划 Task 10 补齐并随 native 发版到位；在此之前 iOS 侧用 Android beta 结果 + iOS stable 灰观察替代。
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
| main 每次 merge 直达全量用户 | 白屏冒烟门 + 契约守卫为强制闸；重大改版可先 dispatch 到 beta |
| bridge 守卫盲区：行为变更不改方法签名 | 诚实记录：守卫只覆盖方法表增删，语义变更仍靠 review + bump 纪律 |
| 首发 min_native 锚错（重演 2026-03） | §8 步骤 1 要求从 git 历史核实方法集合完整版本，宁高勿低；先 beta 后 stable |
