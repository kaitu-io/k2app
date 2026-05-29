# App Bypass v2 — Engine-Managed Smart Mode

> **Status:** Phase 0 design, **revision 2** (architecture spec, not implementation plan).
> **Supersedes:** Detection / preset / route-emission portions of [`2026-05-12-app-bypass-design.md`](2026-05-12-app-bypass-design.md). Storage encryption, privacy rules, and UI shell from that spec remain in force.

---

## 0. Revision History

| Rev | Date | Key change |
|-----|------|-----------|
| 1 | 2026-05-25 | Initial: enumerate-then-inject-exact-names model with `AppListProvider` on engine.Start critical path |
| **2** | **2026-05-25** | **Architecture pivot: rule.Engine learns pattern matching natively. YAML patterns inject directly into `MatchConfig` — no enumeration required for routing. `AppListProvider` demoted to preview-only.** |

### Why rev 2 (架构师 self-review on rev 1)

Rev 1 design issues:

1. **rule.Engine 浪费了**：rev 1 选择"枚举 installed apps → 跑 matcher → 算出 exact name list → 喂给 rule.Engine"模型。这把 detection 推到 engine 外面，**rule.Engine 本身就是 per-flow match runtime**，但被退化成只接受 exact list 的 hashtable。
2. **新装 app 失效**：rev 1 模型必须 reconnect 才能纳入新装 app。UX 倒退。
3. **AppListProvider 抽象层错位**：放在 engine.Start 必经路径上，任何枚举漏洞 (mac `/opt/homebrew/`、win 非标准目录) 都让 pattern 失效。
4. **YAML schema 跨平台歧义**：`prefix:` 在不同 platform 匹配的目标字段不同 (`package_name` vs `bundle_id` vs `exe basename`)，字段名没暴露这个信息。
5. **Daemon API 四语义过度**：`exclude_from_auto`/`include_back_to_auto` 在 pattern-based 模型下不必要。

Rev 2 pivot:

| 维度 | Rev 1 | Rev 2 |
|------|-------|-------|
| Routing 数据 | exact name list (枚举命中后展开) | pattern-based MatchConfig (无需枚举) |
| rule.Engine 改动 | 零 | 加 3 个 MatchConfig 字段 |
| AppListProvider | engine.Start 必经 | 仅 preview API 用 |
| 新装 app | 必须 reconnect | runtime 自动命中 |
| Schema 字段名 | `prefix` (匹配目标不明) | `package_prefix` / `process_prefix` (字段名 = `<target>_<kind>`) |
| 桌面三平台 | 各列一份 | 共享 `desktop` section, case-insensitive matcher |

Rev 2 是 Phase 0 的最终设计。

---

## 1. Background

v0.4.5 痛点（详见 [v0.4.5 spec §1](2026-05-12-app-bypass-design.md#1-background--problem)）：

1. Detection 数据 hardcoded in webapp (`china.ts`) → 改一条规则要发 client 版本
2. 桌面无 smart mode (`loadAutoDetected()` 在 `listInstalled` 缺失时 short-circuit)
3. 桌面用户第一次打开列表是空的，必须手动加每个想 bypass 的 app

约束 fix：**k2 engine 自己管理 smart mode**。webapp 退化为纯 UI，只负责 custom override 增删。

## 2. Goals & Non-Goals

### 2.1 Goals

- **G1**: Smart-mode 端到端工作**不需要 webapp 做任何 detection**。
- **G2**: Curated pattern 数据从 `kaitu-io/k2-rules` CDN OTA，跟 `.k2b` 共用 publish 通道，**无需 client release** 即可上新 app。
- **G3**: `rule.Engine` 学会 pattern matching；webapp 配置的 routes 通过同一套 `MatchConfig` 表达 user-added + curated patterns。
- **G4**: **新装 app 立即 bypass**（runtime per-flow 匹配），不需要 reconnect。
- **G5**: 向后兼容：v0.4.5 client 拿到 CDN 上的 `app-bypass-cn.yaml` **自然忽略**（按文件后缀过滤，不进 `.k2b` 解析路径）。
- **G6**: Webapp 退化为纯 UI：不做 detection / 不做枚举 / 不做 route 组装。

### 2.2 Non-Goals

- **N1**: iOS support。NEPacketTunnelProvider 无法做 per-flow process identity attribution；v0.4.5 起 `buildBypassRoutes()` 就在 iOS 返 `[]`，沿用此约束。
- **N2**: Custom overrides 跨设备同步。
- **N3**: 在 webapp 解释"某条 auto 命中是因为哪条 pattern"。命中是 runtime 结果，preview API (Phase 2) 给列表，不给原因。
- **N4**: `.k2b` binary schema 升级 —— 本设计是独立 YAML 文件，不动 `.k2b` 格式。
- **N5**: 给单个 auto-命中条目做"用户撇掉"功能。Pattern-based 模型下用户没有这个位置（要砍的是 publisher 维护的 pattern，不是个别命中）。如果用户讨厌某个 app 被 bypass，应通过 user feedback → publisher 调整 pattern。

## 3. Architecture

```
┌────────────────────────────────────────────────────────────────────┐
│ CDN: kaitu-io/k2-rules                                             │
│   cn-direct.k2b              (现有, domain+IP)                      │
│   app-bypass-cn.yaml         (新增, plain YAML, 50-200 patterns)    │
│   manifest.json              (共享, SHA256 + size, 同 refresh 通道) │
└────────────────────┬───────────────────────────────────────────────┘
                     │
                     ▼
┌────────────────────────────────────────────────────────────────────┐
│ k2 engine (daemon / appext / gateway)                              │
│                                                                    │
│  rule.Engine                                                       │
│   ├─ .k2b bundles                       (现状)                     │
│   └─ MatchConfig 扩展字段                (新)                       │
│       ├─ ProcessNamePrefix []string                                │
│       ├─ PackageNamePrefix []string                                │
│       └─ InstallerPackage  []string                                │
│                                                                    │
│  appbypass.Preset           (新增)      ← YAML loader + compile   │
│       inject into rule.Engine as one direct-route at Start time    │
│                                                                    │
│  provider.AppListProvider   (新增)      ← demoted to preview-only  │
│       Phase 2: backs action app-bypass-preview                     │
│       Phase 1: not yet called by anything                          │
└────────────────────▲───────────────────────────────────────────────┘
                     │ POST /api/core { action: "app-bypass-*" }
                     │
┌────────────────────────────────────────────────────────────────────┐
│ Webapp (纯 UI)                                                     │
│   action app-bypass-get                                            │
│   action app-bypass-set-custom                                     │
│   action app-bypass-set-region                                     │
│   action app-bypass-preview            (Phase 2)                   │
│                                                                    │
│ DELETED:                                                           │
│   - webapp/src/utils/regionalAppDetection/  (整目录)                │
│   - webapp/src/stores/app-bypass.store.ts::loadAutoDetected        │
│   - webapp/src/stores/config.store.ts::buildBypassRoutes           │
└────────────────────────────────────────────────────────────────────┘
```

### 3.1 v0.4.5 vs v2 对比

| 层 | v0.4.5 | v2 (本 spec) |
|----|--------|---------------|
| Detection 数据 | webapp `china.ts` | CDN `app-bypass-cn.yaml` |
| Detection 逻辑 | webapp 跑 JS | `rule.Engine` per-flow 跑 (Go) |
| 枚举 installed apps | webapp `listInstalled()` | **不需要** (Phase 1)；Phase 2 仅 preview |
| Route 组装 | webapp `buildBypassRoutes` 内联 `process_name`/`package_name` 到 ClientConfig | engine 内部直接注入 rule.Engine direct route |
| Custom 存储 | `_platform.storage` (webapp 写) | daemon 加密 storage |
| 桌面 smart mode | ❌ | ✅ |
| 新装 app 立即 bypass | ❌ (重连后才生效) | ✅ (runtime) |

### 3.2 保留 v0.4.5 的设计

- `MatchConfig.ProcessName` / `MatchConfig.PackageName` exact 字段（`k2/rule/target.go:25-26`）—— 用户手动添加的 exact entries 走这两个字段，跟 v0.4.5 完全一致。
- Storage 加密 ENC1 (`storage_crypto.rs` / `mcp/storage_crypto.go`)。custom overrides 用同一套。
- Webapp 隐私规则：entries 名字 **never logged / never in feedback / never in Sentry**。

## 4. YAML Schema

### 4.1 文件命名 & CDN 发布

- 命名：`app-bypass-<region>.yaml`，`region` 是 ISO 3166-1 alpha-2 小写 (`cn` / `ir` / `ru` / `tr` / …)。
- CDN 路径：跟 `.k2b` 兄弟文件并列，**共享 `manifest.json`**（SHA256 + size + modified-at）。
- Refresh 通道：跟 `.k2b` 同 downloader 路径，复用现有 cache + ETag 机制。

### 4.2 Schema (v1)

```yaml
# app-bypass-cn.yaml
version: 1                       # gates reader compat — bumping rejects on old client
region: cn                       # must match filename
description: |                   # human note (parser ignored)
  Bypass list for users in China. Migrated 2026-05-25 from china.ts.

# ── Android (package_name based, with installer signal) ─────────
android:
  installer_exact:               # match InstallerPackageName == entry
    - com.xiaomi.market
    - com.huawei.appmarket
    - com.bbk.appstore
    - com.oppo.market
    - com.heytap.market
    - com.tencent.android.qqdownloader
    - com.qihoo.appstore
    - com.baidu.appsearch
    - com.wandoujia.phoenix2
    - com.lenovo.leos.appstore
    - com.coolapk.market

  package_exact:                 # match packageName == entry (case-sensitive)
    - com.eg.android.AlipayGphone
    - com.UCMobile
    - com.smile.gifmaker

  package_prefix:                # match strings.HasPrefix(packageName, entry)
    - com.tencent.
    - com.alipay.
    - com.alibaba.
    - com.taobao.
    - com.tmall.
    - com.baidu.
    - com.bytedance.
    - com.ss.android.
    # ... (full list migrated from china.ts CHINESE_PACKAGE_PREFIXES)

# ── Desktop (macOS + Windows + Linux 共享, process_name based) ──
# 匹配对象 = k2 ProcessSearcher.FindProcess 返回的 processName 字段
#   macOS:   helper basename ("WeChat", "WeChatHelper", "WeChatAppEx")
#   Windows: exe basename     ("WeChat.exe", "WeChatUpdate.exe")
#   Linux:   /proc/comm       ("wechat")
# Matcher 大小写不敏感 — publisher 写一份命中三平台
desktop:
  process_exact:                 # match strings.EqualFold(processName, entry)
    - WeChat                     # mac
    - WeChat.exe                 # win
    - wechat                     # linux

  process_prefix:                # match strings.HasPrefix(lower(processName), lower(entry))
    - WeChat                     # mac: WeChat/WeChatHelper/WeChatAppEx; win: WeChat.exe/WeChatStart.exe; linux: wechat
    - QQ
    - DingTalk
    - Tencent                    # 兜底 TencentMeeting.exe / TencentDocs.exe
    - Alipay
    - Alibaba
```

### 4.3 字段名 → MatchConfig 字段映射

字段名格式 = **`<target>_<kind>`**，一看就知道匹配什么。

| YAML 字段 | 注入到 MatchConfig 的字段 | 匹配语义 | Case |
|----------|--------------------------|---------|------|
| `android.installer_exact` | `InstallerPackage []string` | exact | sensitive |
| `android.package_exact` | `PackageName []string` | exact | sensitive |
| `android.package_prefix` | `PackageNamePrefix []string` | `strings.HasPrefix` | sensitive |
| `desktop.process_exact` | `ProcessName []string` | `strings.EqualFold` (case-insensitive) | insensitive |
| `desktop.process_prefix` | `ProcessNamePrefix []string` | `strings.HasPrefix(lower, lower)` | insensitive |

Case 选择的依据：
- Android `packageName` 是 case-sensitive 反向域名 convention，必须严格匹配
- Desktop process names 跨平台大小写不一致 (`WeChat` mac vs `wechat` linux)，case-insensitive 让 publisher 写一遍命中三平台

### 4.4 故意不支持的 kind

| Kind | 为什么不做 |
|------|-----------|
| `contains` | prefix + `Tencent` 这种兜底前缀已经覆盖 95% 跨厂商场景，contains 性能差且语义模糊 |
| `suffix` | 罕用 (反向域名末尾不稳定)，pattern 命中 expressiveness 不够 |
| `glob` | 编译复杂度高；常见 `*foo*` / `foo*` 形态用 prefix 已表达 |
| `regex` | publisher 误用风险大 (`.*` 灾难)；要回归请走 schema v2 加 |

**KISS 原则**：Phase 1 只做 exact + prefix + installer 三种 kind。Phase 2+ 视实战需求再加。

### 4.5 Schema 升级路径

- `version: 1` → 当前。Reader 接受 `version <= 1`。
- 升级时 bump：reader 看到未知 version reject 整个文件（log + skip），跟 `.k2b` 同 fail-safe 模式。
- Publisher 不允许追溯改 v1 字段语义。新加 kind 必须 bump version + 双发兼容 (留给未来决定)。

### 4.6 向后兼容验证

| 场景 | 老 client (v0.4.5) | 新 client (本 spec) |
|------|--------------------|----------------------|
| CDN 上有 `.yaml` 文件 | `rule.loader.go:23` 按 `.k2b` 后缀过滤，自动跳过 ✅ | `appbypass.Load()` 读取 ✅ |
| CDN 没有 `.yaml` 文件 (老 CDN) | 走 `china.ts` 旧路径 ✅ | `appbypass.Load()` 返空 PresetSet，engine 跳过注入，行为等同 v0.4.5 桌面 (manual-only) ✅ |
| YAML 文件损坏 / version 不识别 | N/A | log warn + skip，engine 正常 connect 但无 smart bypass ✅ |

零回归风险确认。

## 5. `k2/appbypass` Package

新建独立包，**不复用 `k2/rule/` 的 binary mmap 路径**（YAML 是 plain text，规模小，直接 yaml.v3 解析）。

### 5.1 Public API

```go
package appbypass

// Preset is one region's compiled bypass patterns.
type Preset struct {
    Region string
    // Pre-compiled per-target slices, ready to inject into rule.MatchConfig.
    Android AndroidPatterns
    Desktop DesktopPatterns
}

type AndroidPatterns struct {
    InstallerExact []string  // → MatchConfig.InstallerPackage
    PackageExact   []string  // → MatchConfig.PackageName (union with user adds)
    PackagePrefix  []string  // → MatchConfig.PackageNamePrefix
}

type DesktopPatterns struct {
    ProcessExact  []string   // → MatchConfig.ProcessName (lower-cased here at compile)
    ProcessPrefix []string   // → MatchConfig.ProcessNamePrefix (lower-cased)
}

// PresetSet is all loaded presets indexed by region.
type PresetSet struct {
    presets map[string]*Preset
}

// Load reads all app-bypass-*.yaml under cacheDir. Invalid files log + skip
// (do not abort). Empty PresetSet returned when no files found.
func Load(cacheDir string) (*PresetSet, error)

// Get returns the preset for region (e.g. "cn"), or nil.
func (s *PresetSet) Get(region string) *Preset
```

### 5.2 Compile-time normalization

Compile 时一次性做：
- Desktop entries `strings.ToLower` (case-insensitive matcher 的右半边)
- Prefix list 去重 + sort (per-flow 走二分时 sort 必需，目前 prefix-only 不需二分但 sort 帮 dedup)
- Validate (entry 不能空字符串、不能超长 256 字符)

per-flow runtime 不再做这些，纯查表 + `strings.HasPrefix`。

### 5.3 File 布局

```
k2/appbypass/
├── format.go      # YAML struct (yaml tags), schema validation
├── loader.go      # filesystem walk, version check, Load()
├── compile.go     # normalize + dedup + sort
├── preset.go      # Preset / PresetSet types + Get()
└── *_test.go      # golden tests
```

**测试覆盖**（最小集，Phase 1 plan 会细化）：
- Parse valid v1 YAML → 字段映射正确
- Parse with `version: 999` → skipped (log warn)
- Parse with malformed YAML → skipped, no panic
- Empty cacheDir → empty PresetSet, no error
- Desktop entries lower-cased at compile
- Android entries case preserved

## 6. `rule.Engine` MatchConfig 扩展

这是 v2 的核心改动 —— rule.Engine 从"exact list 匹配器"升级到支持 prefix。

### 6.1 字段扩展

`k2/rule/target.go`:

```go
type MatchConfig struct {
    // ── existing (v0.4.5) ──
    Preset       string
    Names        []string
    Exclude      []string
    DomainSuffix []string
    IPCIDR       []string
    ProcessName  []string   // exact, hash O(1)
    PackageName  []string   // exact, hash O(1)
    Network      string
    IPIsPrivate  bool
    All          bool

    // ── new (v2) ──
    ProcessNamePrefix []string  // prefix, linear (sorted, case-insensitive)
    PackageNamePrefix []string  // prefix, linear (case-sensitive)
    InstallerPackage  []string  // Android InstallerPackageName, exact hash
}
```

### 6.2 Per-flow matcher 改动

`k2/rule/engine.go` 的 `Match(metadata)` 路径加新分支：

```go
// pseudo, exact API TBD in Phase 1 plan
func (mc *compiledMatch) matchProcess(processName string) bool {
    if _, ok := mc.processExact[processName]; ok { return true }
    if _, ok := mc.processExactLower[strings.ToLower(processName)]; ok { return true }
    lower := strings.ToLower(processName)
    for _, p := range mc.processPrefixLower {
        if strings.HasPrefix(lower, p) { return true }
    }
    return false
}

func (mc *compiledMatch) matchPackage(packageName, installerPackage string) bool {
    if _, ok := mc.installerExact[installerPackage]; ok { return true }  // Android only
    if _, ok := mc.packageExact[packageName]; ok { return true }
    for _, p := range mc.packagePrefix {
        if strings.HasPrefix(packageName, p) { return true }
    }
    return false
}
```

**性能预算**：
- 一条 route 50 个 prefix × `strings.HasPrefix` = ~5μs
- ProcessSearcher.FindProcess 本身 1-100ms（DIAG threshold），prefix 匹配是 noise
- 仅在有 ProcessName/PackageName attribution 时跑 (DNS-only flows 跳过 process match)

### 6.3 Installer 信号传播

当前 `ProcessSearcher` 接口只返 `(processName, packageName)`。**v2 需要把 `installerPackageName` 也带出来**（Android 专属）。

```go
// k2/provider/process.go (modified)
type ProcessSearcher interface {
    FindProcess(network string, srcIP netip.Addr, srcPort uint16) (
        processName string,
        packageName string,
        installerPackageName string,  // new (Android: from PackageManager; others: "")
    )
}
```

Android `appext` 的 `PackageResolver` 扩展：

```go
// k2/provider/process.go (modified)
type PackageResolver interface {
    PackageForUID(uid int32) string
    // new: installer source for an installed package
    InstallerForPackage(packageName string) string
}
```

K2Plugin.kt 注入实现：调 `PackageManager.getInstallSourceInfo(pkg).installingPackageName`，cache 进程内（Android 上 installer 是 install-time 决定的，运行时不变）。

### 6.4 跟现状的兼容性

- 老 ClientConfig (没有 `process_name_prefix` 字段) → MatchConfig 新字段为空 → matcher 走 fast path，行为等同 v0.4.5。
- 新 MatchConfig 字段 yaml/json tag 用 snake_case (`process_name_prefix`)，跟现状 convention 一致。

## 7. Engine Auto-Injection Flow

### 7.1 engine.Start() 增量

```go
// engine/engine.go (modified)
func (e *Engine) Start(ctx context.Context) error {
    // ── 现状不变 ──
    bundles, _ := rule.Load(e.cfg.RuleCacheDir)
    routes := e.cfg.Routes

    // ── 新增：appbypass patterns 注入 ──
    if route := e.buildAppBypassRoute(); route != nil {
        routes = append(routes, *route)  // prepended? appended? See §7.2
    }

    e.ruleEngine = rule.NewEngine(routes, bundles)
    // ... rest of Start unchanged
}

func (e *Engine) buildAppBypassRoute() *rule.RouteConfig {
    if e.cfg.AppBypassRegion == "" {
        return nil
    }
    presets, err := appbypass.Load(e.cfg.RuleCacheDir)
    if err != nil {
        slog.Warn("appbypass load failed", "err", err)
        return nil
    }
    preset := presets.Get(e.cfg.AppBypassRegion)
    if preset == nil {
        return nil
    }
    custom := e.cfg.AppBypassCustom

    mc := rule.MatchConfig{
        // Android
        InstallerPackage:  preset.Android.InstallerExact,
        PackageName:       union(preset.Android.PackageExact, custom.PackageAdds),
        PackageNamePrefix: preset.Android.PackagePrefix,
        // Desktop
        ProcessName:       union(preset.Desktop.ProcessExact, custom.ProcessAdds),
        ProcessNamePrefix: preset.Desktop.ProcessPrefix,
    }
    if !mc.HasCriteria() { return nil }

    return &rule.RouteConfig{ Via: "direct", Match: mc }
}
```

### 7.2 Route order

Auto-bypass route 放在 user-defined routes 之前还是之后？

**决定：放在 user routes 之前** (first match wins)。原因：
- App bypass 是"上层用户意图覆盖底层规则" → 必须先匹配，不然 user `routes: [{via: serverUrl, match: {all: true}}]` 会先把所有流量吞掉
- 用户手动 add 的 entries 通过 `custom.ProcessAdds` 合并进同一条 route，跟 auto 同优先级

### 7.3 Custom override 合并语义

daemon 持久化 + engine 装载：

```go
type AppBypassCustom struct {
    ProcessAdds []string  // 用户额外加的 process exact name
    PackageAdds []string  // 用户额外加的 package exact name
}
```

合并：`union(preset.Exact, custom.Adds)` —— **纯加法**，无减法。

为什么没有"撇掉 auto 命中"功能：
- 用户视角："WeChat 我不想 bypass" → 实际是想砍掉 process_prefix `WeChat` 这条 publisher rule
- 但这是 publisher decision，不是 user decision (砍掉影响所有用户)
- 单个 user 想 "对我而言禁用 WeChat bypass" 需求极罕见 (用户主动选择智能模式就是想要 WeChat direct)
- 真要做：Phase 2+ 加 `custom.ProcessBlocks []string` 字段，注入到 `rule.MatchConfig.Exclude` 之类的逻辑。Phase 1 不做。

### 7.4 Reload triggers

| 触发 | 处理 |
|------|------|
| engine.Start | 加载 YAML + 装配 route（上面） |
| action `app-bypass-set-custom` | daemon 持久化 → `engine.RefreshAppBypass()`（重 build rule.Engine） |
| CDN YAML refresh 完成 | daemon downloader 检测到 YAML 更新 → `engine.RefreshAppBypass()` |
| 用户切 region (webapp country) | webapp 触发 daemon action `app-bypass-set-region` → reload |

**rule.Engine 重 build 成本**：domain bundles mmap 共享、CIDR tree 已构建，重 build 主要是重新 compile MatchConfig + 重 sort prefix list。预计 < 50ms。

### 7.5 Failure modes (永远不阻塞 connect)

- YAML 解析错 → log + skip 该文件，engine 正常 Start（无 smart bypass）
- 没有匹配 region 的 YAML → 同上
- `appbypass.Load` panic（不应发生）→ recover + log，engine 正常 Start
- MatchConfig 新字段 `HasCriteria()` 返回 false → route 不加入，无副作用

## 8. `provider.AppListProvider` (Phase 2)

**Phase 1 不实现**。Spec 在此 reserve 接口形态，Phase 1 留 nil stub。

### 8.1 接口

```go
// provider/applist.go (Phase 2)
type InstalledApp struct {
    ID                   string   // platform-specific stable ID (package / bundle id / exe path)
    Label                string   // display name
    InstallerPackageName string   // Android only
    InstallPath          string   // desktop only (mac: .app path, win: exe path, linux: .desktop path)
    ProcessNames         []string // desktop: helper basenames or [exe basename]; android: [packageName]
}

type AppListProvider interface {
    ListInstalled(ctx context.Context) ([]InstalledApp, error)
}
```

### 8.2 平台实现策略

| 平台 | 路径 | 备注 |
|------|------|------|
| macOS daemon | 扫 `/Applications/**/*.app` + `~/Applications/**/*.app`, 读 Info.plist | daemon 无 GUI session, 不能用 NSWorkspace, 走文件系统 |
| Windows daemon | 读 `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\*` + `HKCU\...`. 拿 `InstallLocation` + `DisplayName`, 扫该目录下 `*.exe` 拿 basename | 不扫进程 |
| Linux daemon | 扫 `/usr/share/applications/*.desktop` + `~/.local/share/applications/*.desktop`. 解 `Exec=` + `Name=` | |
| Android appext | gomobile setter: `appext.SetInstalledApps(jsonList string)`. K2Plugin.kt 在 connect 前调用 | 复用现有 PackageResolver injection 模式 |
| iOS appext | nil (sandbox 限制) | preview API 返 `feature_supported: false` |
| Gateway (k2r) | nil (TPROXY 无 per-app identity) | 同上 |

### 8.3 仅 preview 用，不影响 routing

```go
// daemon/api_app_bypass.go (Phase 2)
// action "app-bypass-preview" dispatched from /api/core handleCore
//   1. Call AppListProvider.ListInstalled()
//   2. Run preset.MatchInstalled(platform, installed)  -- dry run, doesn't touch rule.Engine
//   3. Return matched list with hit reason (which pattern kind hit)
```

Phase 1 webapp UI 显示一句 "智能识别已启用 (region: cn)" 就够；Phase 2 真正显示命中列表。

## 9. Daemon HTTP API

**对齐既有 `/api/core` action-dispatch 模式**（rev 3, 2026-05-25 修订）—— 不新增 mux endpoint，所有 app-bypass 操作通过 `POST /api/core` body 里的 `action` 分发。Webapp 已封装 `daemonClient.core({action, params})`，新 action 零额外封装层。

### 9.1 action `app-bypass-get`

**请求**：`{ "action": "app-bypass-get" }`（无 params）。

**响应**：`Response{Code: 0, Message: "ok", Data: ...}`，Data 形如：
```json
{
  "feature_supported": true,
  "region": "cn",
  "custom": {
    "process_adds": ["Steam", "steam_osx"],
    "package_adds": ["com.gtja.client"]
  }
}
```

- `feature_supported: false` → iOS / gateway，webapp 隐藏 AppBypass 入口
- `region` → 当前生效的 preset region（与 webapp country selector 同步）
- `custom` → 用户加的 exact entries（无 auto 列表 —— auto 是 runtime 行为，不可枚举）

### 9.2 action `app-bypass-set-custom`

**请求**：
```json
{
  "action": "app-bypass-set-custom",
  "params": {
    "add":    { "process": ["Steam"], "package": [] },
    "remove": { "process": [],        "package": ["com.gtja.client"] }
  }
}
```

**响应**：同 9.1 的完整 state (PATCH-style，前端不需要再 GET)。

**副作用**：daemon 持久化 → `engine.RefreshAppBypass()` → 立即生效，**不要求 disconnect**。

### 9.3 action `app-bypass-set-region`

**请求**：
```json
{ "action": "app-bypass-set-region", "params": { "region": "cn" } }
```
（或 `region: ""` 关闭 smart bypass）

**响应**：同 9.1。

**副作用**：daemon 持久化 + reload。

### 9.4 action `app-bypass-preview` (Phase 2)

**请求**：`{ "action": "app-bypass-preview" }`

**响应** Data 形如：
```json
{
  "matched": [
    { "id": "/Applications/WeChat.app", "label": "WeChat",
      "process_names": ["WeChat", "WeChatHelper"],
      "hit_kind": "process_prefix", "hit_pattern": "WeChat" }
  ],
  "phase_one_stub": true
}
```

Phase 1 此 action 返 `{ "matched": [], "phase_one_stub": true }`，webapp 据此 fall back 到一行状态文案。

## 10. Webapp 退化清单

### 10.1 删除

- `webapp/src/utils/regionalAppDetection/` (整目录及 `__tests__/`)
- `webapp/src/stores/app-bypass.store.ts` 中：
  - `loadAutoDetected()`、`autoDetected`、`autoDetectorMeta`、`autoDetectLoaded` 字段
  - `AutoDetectedAppEntry` re-export
- `webapp/src/stores/config.store.ts` 中：
  - `buildBypassRoutes()` 函数及其调用
  - `config.store.ts:482` 附近 `...buildBypassRoutes(bypassEntries, autoPackageNames)` 注入
- `webapp/src/stores/__tests__/build-bypass-routes.test.ts`、`webapp/src/stores/__tests__/app-bypass-privacy.test.ts` 中关联 sub-tests

### 10.2 改写

- `webapp/src/stores/app-bypass.store.ts` 中 `load() / add() / remove() / clear()`：从读写 `_platform.storage` 改成调 `daemonClient.core({action: "app-bypass-*"})` daemon endpoints。
- `webapp/src/pages/AppBypass.tsx`:
  - 删除 section 2 "Smart detection" 整块 UI (Phase 1 智能识别无可视化命中列表)
  - 添加一个状态条：「智能识别已启用 (region: cn) — 已识别的应用会自动直连」
  - section 3 "Manual added" 数据源换成 daemon GET 返回的 `custom`
  - section 4 "Add more" 保留 `listRunning()` 调用 (仅作 UI 候选源，不再喂 detection)

### 10.3 保留

- `_platform.appList.listRunning()` 桥 → "Add more" 列表数据源
- `_platform.appList.listInstalled()` 桥 (Android) → Phase 2 之前继续存活；Phase 2 K2Plugin 改成主动注入到 daemon 后才能删

### 10.4 Storage migration

`webapp/src/stores/app-bypass.store.ts::load()` 首次启动 (检测到 daemon 端没有 custom 但本地有):
1. 读 `_platform.storage["k2.advanced.app_bypass"]`
2. `daemonClient.core({action: "app-bypass-set-custom", params: {add: {process, package}, remove: {process: [], package: []}}})` 把每条 entry 转成 add list
3. 删除本地 `_platform.storage["k2.advanced.app_bypass"]`

**遵循 `feedback_no_defensive_migration_bridges.md`**: 一次性迁移，不保留兼容桥。Migration 代码在 Phase 2 切 webapp 时加，**Phase 3 删 migration 代码** (一个 release 之后所有用户都迁完了)。

## 11. `k2-rules` Publisher 工作流

### 11.1 仓库结构 (新增)

```
kaitu-io/k2-rules/
├── sources/                         # 现状: .k2b sources
├── app-bypass/                      # NEW
│   ├── cn.yaml
│   ├── ir.yaml                      # 未来
│   └── README.md                    # schema doc + maintenance SOP
├── tools/
│   ├── build-k2b/                   # 现状
│   └── validate-app-bypass/         # NEW — schema validator + lint
└── .github/workflows/publish.yml    # 现状 + 加 *.yaml 到 rsync 列表
```

### 11.2 CI lint (`validate-app-bypass`)

PR 上跑：
1. `yaml.v3 + struct unmarshal` schema check
2. `version: 1` 字段必须存在
3. `region` 必须匹配文件名
4. 每个 platform section entry 数 ≤ 500
5. **Lint rule**: `package_prefix` entry 必须以 `.` 结尾（反向域名 prefix 应该以 `.` 收尾防止 `com.tencent` 误命中 `com.tencentX.foo`）
6. **Lint rule**: `process_prefix` entry 必须非空且去重
7. **Lint rule**: 没有重复 entry（exact 跟 prefix list 之间 cross-check）

### 11.3 初版 `cn.yaml` 来源

完全迁自 `webapp/src/utils/regionalAppDetection/china.ts`：

| `china.ts` 来源 | `cn.yaml` 目标 |
|----------------|----------------|
| `CHINESE_INSTALLERS` (11 条) | `android.installer_exact` |
| `CHINESE_PACKAGE_PREFIXES` 以 `.` 结尾 (37 条) | `android.package_prefix` |
| `CHINESE_PACKAGE_PREFIXES` 不以 `.` 结尾 (3 条) | `android.package_exact` |

Desktop section Phase 1 **空起步** —— v0.4.5 desktop 本来就没 smart mode，Phase 1 不预设。Publisher 在 Phase 2 跟据用户反馈逐步填 (WeChat / QQ / DingTalk 等高优先级)。

## 12. Migration Plan

| Phase | Deliverables | Smoke gate |
|-------|--------------|-----------|
| **Phase 0** (本 spec) | Design sign-off | User OK |
| **Phase 1** | `rule.Engine` MatchConfig 3 新字段；`k2/appbypass` 包；engine.Start 注入；3 个 daemon endpoint；daemon storage；k2-rules `cn.yaml` + CI lint；webapp 切 API + 删 detection code + storage migration | 真机三平台（mac/win/android）实测：装 WeChat → 连接 cn region → WeChat 实际走 direct（通过 daemon DIAG 日志 + control-plane curl 验证目标 IP） |
| **Phase 2** | `provider.AppListProvider` 接口 + 三平台 daemon 自扫；K2Plugin 反向注入 installed list；action `app-bypass-preview` 实落地；webapp 命中列表 UI | 真机三平台：preview 命中 ≥ china.ts 旧路径 |
| **Phase 3** | 删 webapp Storage migration code；退役 `_platform.appList.*` 桥；清理 v0.4.5 残留 | 一个 release 间隔后 |

## 13. Open Questions — All Closed

| # | 问题 | Phase 0 决定 |
|---|------|-------------|
| Q1 | YAML schema 字段名 ok? | ✅ Rev 2 改成 `<target>_<kind>` 形式 |
| Q2 | Daemon API 四语义? | ✅ Rev 2 砍到两语义 (add/remove)，无 auto exclude |
| Q3 | glob/regex 支持? | ✅ Phase 1 只做 exact + prefix + installer，glob/regex 永不（除非 v2 schema） |
| Q4 | 跨设备同步? | ❌ 不做 (N2) |
| Q5 | Process attribution 暴露 installer? | ✅ 改 `ProcessSearcher.FindProcess` 多返一个 string (Android 用，桌面空字符串) |
| Q6 | Phase 1 加 AppListProvider 实现? | ❌ Phase 2 才做。Phase 1 智能识别 runtime-only，UI 不显示命中详情 |
| Q7 | macOS bundle_id 进 matching? | ❌ 不需要。process attribution 在 mac 返 helper basename，bundle_id 仅用于 preview UI ID |

## 14. Privacy / Telemetry (沿用 v0.4.5 §8)

- daemon storage 加密 (ENC1)
- entries 名 **never logged**：daemon 只输出 count `slog.Info("appbypass injected", "auto_route_added", true, "custom_count", N)`
- Feedback zip 排除 `k2.advanced.app_bypass_overrides_v2` storage key
- Sentry breadcrumb 黑名单 (已在 v0.4.5 PR 加，沿用)
- YAML 文件 public CDN，仅含 publisher pattern，无 user data

## 15. Test Strategy (sketch — Phase 1 plan 拆 TDD)

**Unit**:
- `appbypass/*_test.go`: parse / version skip / glob 降级 / dedup / case normalization (≥ 6 cases)
- `rule/target_test.go`: MatchConfig 新字段 `HasCriteria()` / `HasHostCriteria()` (≥ 3 cases)
- `rule/engine_test.go`: per-flow matcher 命中 / miss / case-insensitive process / case-sensitive package (≥ 5 cases)
- `provider/process_*_test.go`: `FindProcess` 新 return 值跨平台 mock (≥ 3 cases)

**Integration**:
- `engine_test.go`: 端到端 — fixture YAML + mock ProcessSearcher → 期望流量走 direct route
- daemon API: HTTP contract round-trip + storage encryption verify

**Webapp** (Phase 2 切完后):
- `app-bypass.store.test.ts`: daemon API mock + storage migration path
- `AppBypass.tsx`: snapshot with feature_supported true/false

**Real-device smoke** (Phase 1 gate):
- macOS: 装 WeChat → connect cn region → 查 `desktop.log` `DIAG: appbypass injected` + WeChat 流量 IP geolocation 不在 VPN 节点
- Windows: 同
- Android: 同（adb shell pm install 后验证）

---

## Architect's Final Confidence Statement

| 维度 | Rev 2 评分 | 评注 |
|------|-----------|------|
| 总方向 (engine-managed) | 10/10 | 不可推翻 |
| Schema 表达 | 9/10 | `<target>_<kind>` 命名解决了跨平台歧义；KISS 砍掉 contains/glob/regex 是 v2 收益 |
| rule.Engine 集成 | 9.5/10 | 让核心 rule engine 学会 prefix；扩 3 字段无侵入；per-flow 性能可控 |
| AppListProvider 抽象层 | 10/10 | 降级为 preview-only，从 critical path 移除，错位修正 |
| 新装 app 体验 | 10/10 | runtime 命中，零 reconnect |
| Daemon API surface | 9/10 | 砍到两语义 (add/remove)，contract 简洁 |
| 向后兼容 | 10/10 | 老 client 自动忽略 YAML，新 client 在没数据时优雅降级 |
| 隐私 / Storage | 10/10 | 沿用 v0.4.5 加密 + log redaction |
| 测试 strategy | 8/10 | sketch 完备，Phase 1 plan 会细化 TDD 拆分 |

**Overall: 9.5/10**. 0.5 缺口在"真机 smoke 还没跑过" —— 这是 Phase 1 实施后才能补的实证信心。架构层面的设计决策已经 lock-in。

**Ready for Phase 1 plan**.
