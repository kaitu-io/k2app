# 规则包冷启动修复 — 单 tarball + 服务入口预取设计

**Date:** 2026-04-20
**Status:** Design

## 问题

iOS（与所有平台）首次连接时，规则引擎以 `routes=0` 启动，所有流量退化成"全局代理"，但 UI 仍显示"智能/rule"模式。用户看不到任何异常提示。第二次之后才恢复正常。

证据 —— 工单 #122（ticket 为作者自测）：

| 会话 | App 下发 config | 引擎实际 | WeChat 视频 |
|---|---|---|---|
| 06:28:54 | `preset=bypass, routes=2` (cn→direct, 其他→proxy) | **routes=0, fallback=outbound(2)** | ❌ 失败 |
| 06:31:25 | `preset=global, routes=1` (全部→proxy) | IsAllProxy 早返回 | ✅ |
| 06:31:48 | `preset=bypass, routes=2` 同 #1 | **routes=1, fallback=outbound(2)** | ✅ |

会话 1 与会话 3 的 App 层 config 完全一致，引擎层结果不同 —— 问题是 engine 起来那一刻规则包不在盘上。

## 根因

`k2/rule/loader.go Load(cacheDir)` 同步从 `cacheDir` glob `.k2b`。若 cache 为空，`Index()` 返回空 map → `buildRouteEntries()` 里 preset 展开的 set names 全部 miss → route entry 没有任何 criteria → 被 `hasAnyCriteria` 过滤掉 → `routes=0`。

规则包本可由 updater 自动下载，但：

1. **Desktop `daemon.startRulePrefetch()`** 依赖 `loadState()` 拿 config 决定"下哪些 bundle"——首次运行无 state，直接 `return`，完全不预取。
2. **iOS / Android `appext.PrefetchRules()`** 函数**存在但从未被调用**（`k2/appext/appext.go:295` 代码存在，grep 全仓 0 个 caller）。
3. 即便预取触发成功，当前分 bundle 下载（`cn-direct.k2b`、`overseas.k2b`、...）需要 config 才能算出下载清单，与 App 层启动顺序耦合严重。

## 目标

1. **消除首连接 routes=0**：不论是否首次运行、是否清缓存，connect 返回前规则包一定在盘上。
2. **零 App 层耦合**：预取由原生服务启动入口触发，不穿 JS/UI 层。
3. **CDN 冗余**：任一主流 CN ISP 封锁环境下（含 GFW 净查 GitHub）仍能拿到规则包。
4. **错误显式化**：极端网络下 connect 10s 内硬失败，返回清晰错误——而非"连上但路由错"的隐蔽降级。

## 非目标

- **不做 embed-in-binary**：评估后否决，改用多路 CDN 解决可用性问题。
- **不做 UI 进度条**：预取 99% 情况下用户不可见；connect 阻塞时复用现有"连接中"spinner。
- **不动 Linux 独立 binary**：`daemon.Run()` 已正确预取，单 tarball 改造顺带生效。
- **不改 engine 运行期 rule 匹配逻辑**：本次改造仅限 bundle 分发和 lifecycle。

## 设计

### 架构概览

```
[k2-rules repo CI]
  每日 07:00 CST 生成 .k2b
    ↓
  打包 k2-rules.tar.gz
    ↓
  commit 到 release 分支 + 发布 GitHub Release

[客户端 k2 core]
  rule.EnsureBundles(ctx, cacheDir, sources):
    1. fresh? (< 24h) → return
    2. Happy Eyeballs 9 源 race 下载 tar.gz
    3. 原子解压（.tmp → .new/ → rename 到 rules/）
    4. 写 bundles.version

[平台入口]
  Desktop: daemon.Run() → startRulePrefetch (已有逻辑调整)
  iOS:     K2Plugin.swift load() → appext.PrefetchRules
  Android: K2Plugin.kt load()   → appext.PrefetchRules
    ↓ 后台 safego goroutine，不阻塞启动

[Connect 路径]
  engine.Start() → EnsureBundles(ctx, 10s) 阻塞等待 → buildRuleEngine
```

### k2-rules 仓库改动

当前仓库只有 `master` 分支。新增：

1. **Nightly cron workflow** (`.github/workflows/nightly-generate.yml`)
   - Cron `0 23 * * *`（UTC = 07:00 CST）
   - 从上游（v2fly geosite/geoip）拉最新数据 → 生成 `.k2b`
   - 打包：`tar -czf k2-rules.tar.gz cn-direct.k2b overseas.k2b ir-direct.k2b ...`
   - force-push 到 `release` 分支（orphan 分支，只含产物 + `VERSION` 文件）
   - `VERSION` 文件格式：`2026-04-20T07:00:00+08:00 sha256:<tarball-sha256>`

2. **Release-on-tag workflow** 保留
   - 人工 cut tag 时仍发布 GitHub Release（产出 `k2-rules.tar.gz` 单个 asset）
   - 同时更新 `release` 分支 ←→ 两个发布路径幂等

### k2 core (`k2/rule/`) 改动

#### 新增 API

```go
// EnsureBundles downloads the rule bundle tarball (if not fresh) and
// atomically extracts .k2b files into cacheDir. Safe for concurrent calls —
// the second caller waits for the first via singleflight
// (golang.org/x/sync/singleflight).
//
// cacheDir is the rules directory (caller supplies, typically
// "<platform-cache>/rules"). Layout after success:
//
//   <cacheDir>/*.k2b              committed bundles
//   <cacheDir>/bundles.version    "<ISO-8601-mtime> sha256:<hex>"
//   <cacheDir>/.tmp/              transient download/extract (auto-cleaned)
//
// Returns nil if bundles already fresh (< 24h) or download+extract succeeded.
// Returns error if ctx cancelled or all sources failed.
func EnsureBundles(ctx context.Context, cacheDir string, sources []string) error
```

内部调用链：
```
EnsureBundles
  ├─ cleanupStale(cacheDir)                // 删 .tmp/ 残留
  ├─ isFresh(cacheDir, 24*time.Hour)        // 检查 bundles.version mtime
  ├─ singleflight.Do(cacheDir, ...)        // 并发去重
  │    ├─ raceDownload(tarball → .tmp/)    // 9 源 Happy Eyeballs
  │    ├─ validateSize(<5MB)               // 防恶意注入
  │    ├─ untar(.tmp/archive → .tmp/new/)
  │    ├─ validateExtracted(.tmp/new/)     // 每个 .k2b 过 ReadBundle
  │    ├─ atomicSwap(.tmp/new/ → cacheDir) // 见下方原子解压细节
  │    └─ writeVersion(bundles.version)
  └─ return
```

#### DefaultSources 扩展

从 3 源扩展到 9 源（沿用 webapp antiblock 镜像列表）：

```go
var DefaultSources = []string{
    // GitHub Releases
    "https://github.com/kaitu-io/k2-rules/releases/latest/download/",
    // GH proxy 类
    "https://ghfast.top/https://github.com/kaitu-io/k2-rules/releases/latest/download/",
    "https://gh-proxy.com/https://github.com/kaitu-io/k2-rules/releases/latest/download/",
    // jsdelivr 镜像（指向 release 分支）
    "https://cdn.jsdelivr.net/gh/kaitu-io/k2-rules@release/",
    "https://fastly.jsdelivr.net/gh/kaitu-io/k2-rules@release/",
    "https://testingcf.jsdelivr.net/gh/kaitu-io/k2-rules@release/",
    "https://gcore.jsdelivr.net/gh/kaitu-io/k2-rules@release/",
    "https://cdn.jsdmirror.com/gh/kaitu-io/k2-rules@release/",
    "https://jsd.onmicrosoft.cn/gh/kaitu-io/k2-rules@release/",
}
```

下载 URL：`<source> + "k2-rules.tar.gz"`。下载器无改动 —— 现有 `raceDownload` 已支持任意 base URL。

#### 删除 / Deprecate

无向后兼容负担，直接删：

- `bundleFileMap` (downloader.go) — 不再按 set name 映射 bundle 文件
- `BundlesForConfig()` — 不再按 config 算下载清单
- `Download()` / `downloadOne()` 的 per-bundle 接口 — 改为 `EnsureBundles` 内部使用
- `DownloadAndReload()` 的 `bundles []string` 参数 — 改成内部处理

保留：

- `Load(cacheDir)` — glob `.k2b` 不变
- `Index()`, `BundleSet`, `Bundle`, `ReadBundle()` — 格式层零改动
- `IsFresh` 改成检查 `bundles.version` 文件的 mtime

#### 原子解压细节

`cacheDir` 本身就是规则目录，不再嵌套 `rules/` 子目录。交换策略：

```go
func atomicSwap(newDir, cacheDir string) error {
    // newDir 已包含所有合法 .k2b（由 validateExtracted 确认）
    // 逐个移动 .k2b 到 cacheDir，原子替换同名旧文件
    entries, _ := os.ReadDir(newDir)
    for _, e := range entries {
        if !strings.HasSuffix(e.Name(), ".k2b") { continue }
        src := filepath.Join(newDir, e.Name())
        dst := filepath.Join(cacheDir, e.Name())
        if err := os.Rename(src, dst); err != nil {  // 同 FS 原子
            return err
        }
    }
    // 清理新版本里已不存在的旧 .k2b（bundle 被 upstream 删除的情况）
    pruneOrphanedBundles(cacheDir, newDir)
    return nil
}
```

选择"逐文件 rename"而非"整个目录 swap"：Go stdlib `os.Rename` 在同一 FS 下对单文件是原子的；逐文件替换避免了目录替换跨平台的边角问题（Windows 上 `os.Rename` 目标目录存在会失败）。

Crash 一致性：
- 进程在下载/解压阶段 crash → 下次 `cleanupStale` 删整个 `.tmp/`
- 进程在 atomicSwap 中途 crash → 部分 `.k2b` 是新版、部分旧版。**这是已验证合法的混合状态**（validateExtracted 已跑），能继续工作；下次启动触发 updater，补齐剩余文件并写 `bundles.version`

#### 并发去重

```go
var bundleSF singleflight.Group  // key = cacheDir

func EnsureBundles(ctx context.Context, cacheDir string, sources []string) error {
    _, err, _ := bundleSF.Do(cacheDir, func() (any, error) {
        return nil, ensureBundlesLocked(ctx, cacheDir, sources)
    })
    return err
}
```

第二个调用阻塞等第一个结果，共享下载。

### appext / daemon 层改动

#### `k2/daemon/daemon.go`

`startRulePrefetch` 大幅简化 —— 不再需要 `loadState` / `BundlesForConfig`：

```go
func (d *Daemon) startRulePrefetch(ctx context.Context) {
    dir := filepath.Join(cacheDir(), "rules")
    sources := rule.DefaultSources
    safego.Go(func() {
        dlCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
        defer cancel()
        if err := rule.EnsureBundles(dlCtx, dir, sources); err != nil {
            slog.Warn("rule prefetch failed", "err", err)
            return
        }
        slog.Info("rule prefetch complete")
    })
}
```

首次运行也能预取（不再 early-return on missing state）。

#### `k2/appext/appext.go`

`PrefetchRules` 签名改为无 config 依赖：

```go
// PrefetchRules downloads the rule bundle tarball in the background.
// Safe to call multiple times — concurrent calls share one download.
// Must be called from native service entry (K2Plugin.load).
func PrefetchRules(cfg *EngineConfig) {
    if cfg == nil || cfg.CacheDir == "" { return }
    dir := filepath.Join(cfg.CacheDir, "rules")
    safego.Go(func() {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        if err := rule.EnsureBundles(ctx, dir, rule.DefaultSources); err != nil {
            slog.Warn("appext: PrefetchRules failed", "err", err)
            return
        }
        slog.Info("appext: PrefetchRules complete")
    })
}
```

#### Connect 路径阻塞

`engine.Start()` 在 `buildRuleEngine` 之前插入：

```go
// k2/engine/engine.go, in Start()
ruleDir := filepath.Join(client.CacheDir, "rules")
ensureSources := client.RuleSources
if len(ensureSources) == 0 {
    ensureSources = rule.DefaultSources
}
ensureCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
defer cancel()
if err := rule.EnsureBundles(ensureCtx, ruleDir, ensureSources); err != nil {
    return &EngineError{
        Code:     510,  // 新 code: RuleBundlesUnavailable
        Category: "client",
        Message:  fmt.Sprintf("rule bundles unavailable: %v", err),
    }
}
// 然后 buildRuleEngine → 保证 routes > 0（若 config 有 preset）
```

单 tarball 方案下，prefetch 已完成的常规路径里 `EnsureBundles` 几乎 no-op（只读 `bundles.version` mtime）。若 config 路由全为 `{all: true}` 或仅 inline rules（不引用 preset / geoip 包），可 short-circuit 跳过 EnsureBundles——但为了一致性和未来安全性，统一走同一路径。

#### EngineError 新 code

`k2/engine/error.go` 扩展：

```go
CodeRuleBundlesUnavailable = 510  // all CDNs unreachable within timeout
CategoryClient             = "client"
```

### 平台入口 wiring

#### Desktop (Tauri)

无改动 —— `daemon.Run()` 已在启动时调 `startRulePrefetch`。

#### iOS — `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift`

在 `override public func load()` 中追加。**必须用 app group 共享容器**（与 `PacketTunnelProvider.swift:245` 的 cacheDir 对齐），否则 NE 进程看不到预取结果：

```swift
override public func load() {
    // ... existing setup ...

    guard let containerURL = FileManager.default
            .containerURL(forSecurityApplicationGroupIdentifier: kAppGroup) else {
        logger.warning("rule prefetch: app group container unavailable")
        return
    }

    let rulesURL = containerURL.appendingPathComponent("k2/rules")
    try? FileManager.default.createDirectory(
        at: rulesURL, withIntermediateDirectories: true)

    // 防 iCloud 备份：规则包可再下载，不应占用户配额
    var values = URLResourceValues()
    values.isExcludedFromBackup = true
    var mutableURL = rulesURL
    try? mutableURL.setResourceValues(values)

    DispatchQueue.global(qos: .utility).async {
        let cfg = AppextEngineConfig()
        cfg.cacheDir = containerURL.appendingPathComponent("k2").path
        Appext.prefetchRules(cfg)
    }
}
```

`PacketTunnelProvider.swift` 里已有的 cacheDir 创建代码（line 249-253）同步加 `isExcludedFromBackup` 设置，保证两侧一致。

#### Android — `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt`

在 `override fun load()` 中追加：

```kotlin
override fun load() {
    // ... existing setup ...

    Thread {
        val cacheDir = context.cacheDir.absolutePath
        val cfg = AppextEngineConfig()
        cfg.setCacheDir(cacheDir)
        Appext.prefetchRules(cfg)
    }.start()
}
```

### 错误处理 & UI

Connect 路径返回 `EngineError{Code:510}` 时，webapp 映射到用户文案：

| locale | 文案 |
|---|---|
| zh-CN | 规则包下载失败，请检查网络后重试 |
| zh-TW/zh-HK | 規則包下載失敗，請檢查網絡後重試 |
| en-* | Failed to download routing rules. Check network and retry. |
| ja | ルーティング規則のダウンロードに失敗しました。ネットワークを確認してください。 |

UI 按钮复用现有"重试连接"交互（`webapp/src/pages/Dashboard.tsx` 现已处理 `EngineError.Code 返回码`）。

### iOS 特殊处理

iOS 是唯一双进程 + 沙盒隔离 + iCloud 潜在同步的平台，需要显式处理：

#### 跨进程共享目录（已有基础设施）

- App group：`group.io.kaitu`
- UI 进程（K2Plugin）和 NE 进程（PacketTunnelProvider）都通过 `FileManager.containerURL(forSecurityApplicationGroupIdentifier:)` 拿到同一路径
- 现状（`PacketTunnelProvider.swift:245`）：`engineCfg.cacheDir = <containerURL>/k2`
- 本方案规则目录：`<containerURL>/k2/rules/` —— 自动跨进程可见

#### iCloud 备份排除

App group 容器不在 `Library/Caches/`，默认**会被 iCloud 备份**（除非显式排除）。规则包是可再下载资源，不应占用户 iCloud 配额。

创建规则目录时设置：

```swift
// K2Plugin.swift load() 里、appext.prefetchRules 调用前
let rulesURL = containerURL.appendingPathComponent("k2/rules")
try? FileManager.default.createDirectory(at: rulesURL, withIntermediateDirectories: true)

var resourceValues = URLResourceValues()
resourceValues.isExcludedFromBackup = true
var mutableURL = rulesURL
try? mutableURL.setResourceValues(resourceValues)
```

NE 侧（`PacketTunnelProvider.swift`）的 cacheDir 创建代码（line 249-253）也加同样的排除设置，保证两侧写的标记一致。

#### UI 与 NE 进程职责划分

| 进程 | 何时启动 | 何时预取 | 内存预算 |
|---|---|---|---|
| UI (K2Plugin) | App 冷启动 / 回前台 | `load()` 调用时立即后台预取（30s timeout） | 正常，无 jetsam 压力 |
| NE (PacketTunnelProvider) | 用户按 Connect | `engine.Start()` 内 `EnsureBundles`（10s timeout） | 50 MB jetsam 硬限 |

**UI 是优先预取方**。NE 只在 UI 预取还没完成时（罕见）或 cache 被 OS 清除时兜底。

#### 跨进程并发去重

Go singleflight 只在进程内生效。若 UI 和 NE 同时下载，浪费 ~1.4 MB 带宽但不破坏正确性（原子交换保证 cache 最终一致）。

**简化决策**：不做跨进程锁。代价可忽略，收益小。若未来发现确实频繁竞争，再加 `flock` 文件锁。

#### NE 内存 / 时长风险

1.4 MB tarball 在 NE 进程下载 + gzip 流式解压，常驻内存峰值 <5 MB，远低于 50 MB jetsam。
10s timeout 落在用户"连接中"的体感预期内。若 10s 不够用，下一次连接 cache 已 fresh，立即成功。

### 观测

新增 log 点便于线上 triage：

```
INFO  rule: bundles fresh, version=<stamp>        // 跳过下载
INFO  rule: ensure start sources=<N>              // 开始下载
INFO  rule: ensure complete size=<bytes> ms=<ms>  // 成功
WARN  rule: ensure failed err=<...>               // 失败
```

Desktop 已有的 `startRulePrefetch` 日志保留，改成调用 `EnsureBundles`。

## 测试

### 单元（`k2/rule/`）

| 测试 | 覆盖 |
|---|---|
| `TestEnsureBundles_FreshSkip` | cache < 24h 零 HTTP |
| `TestEnsureBundles_ColdDownload` | 空 cache → 下载 + 解压 + 落地 |
| `TestEnsureBundles_AtomicExtract_CrashMidway` | 模拟 extract 中断 → 下次重下非半写 |
| `TestEnsureBundles_ConcurrentSingleflight` | 两 goroutine 同时调 → 一次下载 |
| `TestEnsureBundles_AllSourcesFail` | mock 9 路全 500 → ctx 内返回 error |
| `TestEnsureBundles_OneSourceWorks` | 第 N 源返回 200 其他挂 → 成功 |
| `TestEnsureBundles_CorruptTarball` | tarball 乱码 → 不污染 cache |
| `TestEnsureBundles_MaxSize` | >5MB → 拒绝 |
| `TestCleanupStaleExtractDirs` | `.tmp` / `.new` 残留清理 |

### 集成（`k2/engine/`）

| 测试 | 覆盖 |
|---|---|
| `TestBuildRuleEngine_AlwaysHasRoutesAfterEnsure` | 调 `EnsureBundles` → `buildRuleEngine`，任何 preset config 必 `routes > 0`（routes=0 回归守卫） |
| `TestEngineStart_RuleBundlesUnavailable` | mock 源全挂 → `Start` 在 10s 内返回 `EngineError{Code:510}` |

### 平台手工验证

| 平台 | 场景 | 预期 |
|---|---|---|
| iOS | 全新安装 → 首次启动 → connect | `K2Plugin load` 后数秒日志 `rule: ensure complete`；connect 时引擎日志 `routes ≥ 1` |
| iOS | 卸载重装 + 离线 → connect | 10s 后 `EngineError{Code:510}`，UI "规则包下载失败" |
| iOS | 预取路径 | 文件落地 `<appGroup>/k2/rules/*.k2b`，NE 进程 `cacheDir` 可见同一路径 |
| iOS | iCloud 备份 | iOS 设置 → iCloud → 管理存储 → App：规则目录不计入应用备份大小 |
| iOS | UI 预取 + 立即 connect 竞争 | 两个下载都完成后，`<cacheDir>/*.k2b` 完整可读，`bundles.version` 存在 |
| Android | 同 iOS 两个场景 | 同 |
| Desktop | 清 `~/Library/Caches/io.kaitu.app/rules/` → 重启 Tauri | daemon 启动后预取成功 |
| Desktop | 杀进程 mid-extract → 重启 | `cleanupStale` 删残留 → 重下成功 |

### k2-rules CI 验证

- Nightly workflow 触发后，`release` 分支有新 commit
- `tar -tzf k2-rules.tar.gz` 能 list 所有预期 `.k2b`
- 每 `.k2b` 通过 `ReadBundle()` 反序列化
- `VERSION` 文件存在且格式正确

### 网络场景（可选）

iptables block `github.com` → 仅 ghfast/gh-proxy/jsdmirror/onmicrosoft 可达 → 验证命中 CN CDN 且总耗时 < 5s。

## Rollout

### 阶段 1 — k2-rules 准备（独立仓库 `kaitu-io/k2-rules`，独立 PR）

1. 新建 orphan `release` 分支（首次手动初始化）
2. 添加 nightly workflow + release-on-tag 追加 `k2-rules.tar.gz` 产物
3. 先 cut 一次 tag 预热 GitHub Release + jsdelivr 缓存
4. 验证 9 路 CDN 访问 `k2-rules.tar.gz` 均返回 200（含 jsdelivr 从 `@release` 分支拉到最新 commit）

### 阶段 2 — k2 core（k2 submodule）

1. `k2/rule/` 重构：`EnsureBundles` + 删 per-bundle 接口
2. `k2/appext/`, `k2/daemon/`, `k2/engine/` 调用点更新
3. 单元 + 集成测试绿
4. 合并到 k2 master，parent 仓库 bump submodule

### 阶段 3 — Mobile wiring

1. iOS `K2Plugin.swift load()` 追加 prefetch
2. Android `K2Plugin.kt load()` 追加 prefetch
3. webapp `EngineError 510` 文案 + 重试交互
4. 7 locale i18n 补全

### 阶段 4 — 手工回归 + 发版

按 `integration-qa` skill 跑 iOS / Android / macOS / Windows 冷启动 + 离线场景。

## Appendix

### k2-rules 现状

```
$ gh api repos/kaitu-io/k2-rules/branches --jq '.[].name'
master
```

需新建：`release`（orphan 分支）

### 单 tarball 尺寸估算

| 产物 | 大小 |
|---|---|
| cn-direct.k2b | 236 KB |
| overseas.k2b | 845 KB |
| 其他 13 国 direct 包（估计） | ~900 KB |
| **原始合计** | ~2.0 MB |
| **.tar.gz 压缩后（估计）** | ~1.4 MB |

若后续发现压缩率偏低（大段二进制 index 压缩性差），备选 `.tar.zst`（`github.com/klauspost/compress/zstd`，约 1.0 MB）。

### EngineError 500 系新 code

| 现有 | 新增 |
|---|---|
| 502 ProtocolError | **510 RuleBundlesUnavailable** |
| 503 ServerUnreachable | |
| 570 ConnectionFatal | |
