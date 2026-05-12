# 客户端按 App 黑名单（App Bypass / Split-Exclude）

**Date:** 2026-05-12
**Status:** Design — pending writing-plans handoff
**Scope:** Per-app traffic exclusion on macOS / Windows / Linux desktop / Android
**Out of scope:** iOS, k2r gateway (per-MAC allowlist already shipped in `gateway/router_device.go`)

---

## 1. Background & Problem

Kaitu 用户主力是国内场景、默认 chnroute 模式。工单中真实的高频痛点：

- 微信 / 支付宝 / 网银因 VPN IP 命中风控被风控
- 12306、医保等强地理校验 app 在 VPN 下不工作
- Adobe CC 等服务（参见 `reference_adobe_cc_stuck_fallback_ip.md`）在某些代理路径下死端
- 部分 china-only 服务对 VPN 出口 IP 极敏感

这些都是 **"这个 app 别走代理"** 的需求。chnroute 按 IP/域名分流，没法做"按 app 分流"。

rule engine 已支持 `MatchConfig.ProcessName []string` 和 `MatchConfig.PackageName []string`
（`k2/rule/target.go:25-26`），TS schema 已对齐
（`webapp/src/types/client-config.ts:46-47`）。本 spec 是把这个能力 expose 到用户面。

## 2. Scope

### 2.1 In Scope

| 平台 | 候选源 | match 字段 |
|---|---|---|
| macOS | 正在运行的进程（按 bundle 分组）+ 手动输入 | `process_name`（bundle 全部 helper） |
| Windows | 正在运行的进程 + 手动输入 | `process_name`（单 exe basename） |
| Linux desktop | 正在运行的进程 + 手动输入 | `process_name`（exe basename） |
| Android | 已安装 user app（PackageManager + `<queries>` LAUNCHER filter） | `package_name` |
| iOS | — | — |
| Web standalone | — | — |

### 2.2 Out of Scope (v1)

- 白名单 / split-include 模式
- 多 routing group（按 app 走不同 server）
- macOS bundle ID 在 rule engine 的原生支持
- 已安装但未运行的 desktop app 枚举
- "国内目标 direct、海外 VPN" 软语义
- 热重载 / 在线切换
- Auto-poll 进程列表
- 跨设备同步

### 2.3 Out-of-Scope Side Effect (Documented)

**Connected-Guard 现在锁定整个 Advanced Settings section**，包括既有的 `RoutingModeSelector`。
对比改动前：用户能在连接中切 global/chnroute（改了但下次连接才生效）。
改动后：明确禁止，按钮 + 文案引导先断开。

Release notes 必须显式提到这一点。

## 3. Architecture

### 3.1 Layer Mapping

```
webapp (React + Zustand)
  ├─ Dashboard.tsx — Advanced Settings collapse
  │   ├─ Connected-Guard (lock section when vpnState ≠ idle)
  │   ├─ RoutingModeSelector (existing)
  │   └─ AppBypass entry (new) ─► navigate('/app-bypass')
  │
  ├─ AppBypass.tsx (new, independent route, no keep-alive)
  │   └─ uses _platform.appList
  │
  ├─ stores/app-bypass.store.ts (new)
  │   └─ persists via _platform.storage('k2.advanced.app_bypass')
  │
  ├─ stores/config.store.ts (extended)
  │   └─ buildConnectConfig prepends bypass routes
  │
  └─ services bridges
      ├─ tauri-k2.ts (macOS / Windows)
      │   └─ appList.listRunning → invoke('list_running_apps')
      ├─ capacitor-k2.ts (Android)
      │   └─ appList.listInstalled → K2Plugin.listInstalledApps
      └─ standalone-k2.ts (Linux desktop branch)
          └─ appList.listRunning → fetch('/api/helper', {action:'app-list-running'})

Native impls:
  ├─ desktop/src-tauri/src/app_list.rs (macOS + Windows)
  │   ├─ macOS: NSWorkspace.runningApplications + libproc::proc_listpids
  │   └─ Windows: sysinfo crate + ExtractIconExW
  │
  ├─ desktop/src-tauri/src/icon_protocol.rs
  │   └─ register_uri_scheme_protocol("kaitu-icon", ...)
  │
  ├─ k2/daemon/helper.go (extended, Linux only)
  │   └─ action 'app-list-running' — /proc enumeration
  │
  └─ mobile/plugins/k2-plugin/android/.../K2Plugin.kt (extended)
      ├─ listInstalledApps — PackageManager + queries filter
      └─ WebViewClient.shouldInterceptRequest for kaitu-icon://
```

### 3.2 Why This Layering

- bypass list 是 **user preference** → 落 `_platform.storage`（与 chnroute、auth token、antiblock 等同层）
- daemon 的 `state.json` 是 ClientConfig **snapshot** 用于 auto-reconnect，**不接管偏好存储**（拒绝理由见 §10.1）
- 候选源是 **platform capability** → 落 `_platform.appList`（与 `_platform.storage`、`_platform.updater` 同层）
- AppBypass 页面是 **独立 route**，**不进 TAB_PAGES**（不享 keep-alive，关闭即销毁）

## 4. Semantics

### 4.1 Mode: Split-Exclude (Blacklist)

加入黑名单的 app **强制 direct**，**无视** chnroute / global 模式。

### 4.2 Route Emission

`buildConnectConfig` 在既有 routes 数组**前**插入 bypass routes：

```ts
function buildBypassRoutes(entries: AppBypassEntry[]): RouteConfig[] {
  if (window._platform?.os === 'ios') return [];  // L3 guard

  const processNames = [...new Set(
    entries.filter(e => e.kind === 'process').flatMap(e => e.names)
  )];
  const packageNames = [...new Set(
    entries.filter(e => e.kind === 'package').flatMap(e => e.names)
  )];

  const routes: RouteConfig[] = [];
  if (processNames.length > 0) {
    routes.push({ via: 'direct', match: { process_name: processNames } });
  }
  if (packageNames.length > 0) {
    routes.push({ via: 'direct', match: { package_name: packageNames } });
  }
  return routes;
}
```

为什么分两条 route：rule engine 的 `MatchConfig` 同 match 块内多字段是 AND；跨数组元素是 OR。
分两条 routes 是 OR 语义的稳态写法。实际部署每平台只产 1 条（Android 全 package，desktop 全 process）。

### 4.3 Example ClientConfig

用户：chnroute + 黑名单加 macOS 微信桌面版：

```json
{
  "routes": [
    {
      "via": "direct",
      "match": {
        "process_name": [
          "WeChat",
          "WeChatAppEx",
          "WeChat Helper",
          "WeChatBrowser"
        ]
      }
    },
    { "via": "direct", "match": { "preset": "cn-access" } },
    { "via": "k2v5://...@server.example.com:443/...", "match": {} }
  ]
}
```

### 4.4 Application Timing

- bypass 修改仅可发生于 **`vpnState === 'idle'`**（见 §6 Connected-Guard）
- 修改即写 storage（per action）
- 下一次 `_k2.run('up', config)` 调用，`buildConnectConfig` 读最新 entries → emit 到 routes → 自然生效
- 无需热重载 / 自动重连 / 排队 banner

### 4.5 Daemon auto-reconnect 自动覆盖

daemon `state.json` 存 last ClientConfig snapshot。VPN 断电 / 重启 / 崩溃 → daemon `<1h auto-reconnect` 用 state.json 配置 → bypass routes 自带在 snapshot 内 → 零额外适配。

## 5. UI / UX

### 5.1 Dashboard Entry

`Dashboard.tsx:593-647` 的 Advanced Settings collapse，在 `RoutingModeSelector` 下方加：

```
┌─ 高级设置 ▼ ─────────────────────────────────────┐
│   分流模式 [chnroute ▼]                          │
│   ─────                                          │
│   不走代理的应用             3 个  ›            │
└──────────────────────────────────────────────────┘
```

- 右侧 badge：`{n} 个` 或 `未选`
- 整行可点 → `navigate('/app-bypass')`
- 不渲染条件：`_platform.appList === undefined`（iOS、web standalone）或 `!features.appBypass`

### 5.2 Connected-Guard

`vpnState !== 'idle'` 时锁定整个 section：

```
┌─ 高级设置 ▼ ─────────────────────────────────────┐
│   ┌─ ⓘ VPN 已连接，请先断开后再修改高级设置 ─┐ │
│   │                              [断开 VPN]  │ │
│   └────────────────────────────────────────────┘ │
│   ─── 以下：pointerEvents:'none', opacity:0.45 ─│
│   分流模式 [chnroute ▼]                          │
│   不走代理的应用             3 个  ›            │
└──────────────────────────────────────────────────┘
```

- 折叠 header 仍可点（可看不可动）
- "断开 VPN" → `dispatch('USER_DISCONNECT')`（已存在事件，见 `vpn-machine.store.ts:32`）
- 影响 RoutingModeSelector — out-of-scope side effect §2.3

### 5.3 AppBypass Page (`/app-bypass`)

独立 route，不进 TAB_PAGES。

```
┌─ ← 不走代理的应用 ───────────────────────────────┐
│  这些应用的流量将不走 VPN，直接出网。            │
│  适合避免被风控的应用：网银 / 微信 / 12306。      │
│  （macOS 注脚：只列出当前账户启动的应用）         │
│                                                  │
│  ─── 已加入（3）──────────────────────────────  │
│  [icon] 微信               屏蔽 4 个进程  ⟳ ✕   │
│  [icon] 招商银行           屏蔽 1 个进程     ✕   │
│  [icon] 12306                屏蔽 1 个进程     ✕   │
│                                                  │
│  ─── 可添加 ────────────────────  [+ 手动添加]  │
│  [icon] Google Chrome                    [+ 添加]│
│  [icon] Notion                            [+ 添加]│
│  ... (virtualized list)                          │
│  [⟳ 刷新]                                        │
└──────────────────────────────────────────────────┘
```

#### Page-Level Guard

```ts
useEffect(() => {
  const check = () => {
    if (useVpnMachineStore.getState().state !== 'idle') {
      navigate('/', { replace: true });
      showToast(t('appBypass.kickedOutDueToConnect'));
      return true;
    }
    return false;
  };
  if (check()) return;
  return useVpnMachineStore.subscribe(s => s.state, check);
}, []);
```

#### Per-entry "重新检测" (M3)

只在 desktop process entries 显示。点击：

1. 调 `_platform.appList.listRunning()`
2. 找出和当前 entry 同 bundle/exe 的 RunningApp
3. 用新的 `processNames` 全集 update entry.names
4. 调 storage 写回
5. UI 显示 `rescanResult` toast

### 5.4 Manual Add Dialog

```
┌─ 添加进程名 ──────────────────┐
│  [chrome.exe / com.tencent... ]│
│              [取消]  [添加]    │
└───────────────────────────────┘
```

- 关闭前 if `input !== ''` → 二次确认 (L5)
- kind 推断：desktop = 'process'，Android = 'package'
- 输入即作为 `names[0]`（无 bundle 全集语义）

### 5.5 Icon Rendering

- `<Avatar src={app.iconUrl} variant="rounded">{label[0].toUpperCase()}</Avatar>`
- `iconUrl` 形如 `kaitu-icon://bundle/com.google.Chrome`、`kaitu-icon://exe/C%3A%5C...`、`kaitu-icon://package/com.tencent.mm`
- onError / undefined → Avatar fallback 自动显示首字母 + 由 label hash 决定的色调

## 6. Storage

### 6.1 Schema

```ts
// _platform.storage key: 'k2.advanced.app_bypass'
interface AppBypassStorageShape {
  v: 1;
  entries: AppBypassEntry[];
}

interface AppBypassEntry {
  id: string;            // macOS bundleId / Win+Linux exe path / Android packageName / manual input
  label: string;
  kind: 'process' | 'package';
  names: string[];       // frozen snapshot at add-time
  iconUrl?: string;
  addedAt: number;
}
```

`v: 1` 显式版本号 — 后续 schema 演进时可加 migrator。

### 6.2 加密

走 `_platform.storage` 既有的 AES-256-GCM（machine-id HKDF）。

### 6.3 写时机

| 用户动作 | 写 storage |
|---|---|
| add / remove / clear / rescan | 立即（同步 await） |
| 启动 load | `initializeAllStores` 调用，失败回退空数组并 slog.warn |

### 6.4 跨设备同步

**不同步**。process_name 集合是 per-device 的。即使 future 引入 account-bound sync，本 feature opt-out。

## 7. Native Bridges

### 7.1 Interface

```ts
// webapp/src/types/kaitu-core.ts
interface IPlatform {
  ...existing,
  appList?: IAppListProvider;
}

interface IAppListProvider {
  listRunning?(): Promise<RunningApp[]>;       // desktop
  listInstalled?(): Promise<InstalledApp[]>;   // android
}

interface RunningApp {
  id: string;              // macOS bundleId / Win+Linux exe path
  label: string;
  processNames: string[];  // macOS: bundle 全部 helper / Win+Linux: [basename]
  iconUrl?: string;        // kaitu-icon://...
}

interface InstalledApp {
  packageName: string;
  label: string;
  iconUrl?: string;
}
```

iOS / web standalone bridge：`appList = undefined`。

### 7.2 macOS / Windows (Tauri Rust)

`desktop/src-tauri/src/app_list.rs`:

```rust
#[tauri::command]
async fn list_running_apps() -> Result<Vec<RunningApp>, String> {
    #[cfg(target_os = "macos")]
    return mac::enumerate();
    #[cfg(target_os = "windows")]
    return win::enumerate();
}

// Output struct: #[serde(rename_all = "camelCase")] enforces camelCase.
```

**macOS**:
1. `NSWorkspace.shared.runningApplications` 拿 NSRunningApplication 列表
2. 每个 NSRunningApplication → `bundleIdentifier`, `localizedName`, `bundleURL`, `processIdentifier`
3. 用 `libproc::proc_listpids` 全 PID + `proc_pidpath` 判 path 是否在 bundleURL 之下
4. 收 basename 集合 → `processNames`
5. `iconUrl = "kaitu-icon://bundle/" + url_encode(bundleId)`

**Windows**:
1. `sysinfo` crate 列进程
2. 按 exe path dedup (同一 exe 多 PID → 一条 row)
3. `processNames = [basename(exe)]`
4. `iconUrl = "kaitu-icon://exe/" + url_encode(exe_path)`

### 7.3 Linux Desktop (Go daemon helper)

`k2/daemon/helper.go` 新 action `app-list-running`:

```go
// Response (snake_case per Go convention; standalone-k2.ts remaps to camelCase)
type appListRunningResp struct {
    Apps []runningAppJSON `json:"apps"`
}

type runningAppJSON struct {
    ID            string   `json:"id"`
    Label         string   `json:"label"`
    ProcessNames  []string `json:"process_names"`
}
```

实现：`os.ReadDir("/proc")` → 每个 PID 读 `/proc/<pid>/comm` + `/proc/<pid>/exe`。自己 PID 过滤掉。
权限拒绝的 PID skip（无错）。无 icon 字段（Linux desktop fallback 到首字母 Avatar）。

### 7.4 Android (K2Plugin)

**Manifest 改动**（`mobile/android/app/src/main/AndroidManifest.xml`）:

```xml
<queries>
    <intent>
        <action android:name="android.intent.action.MAIN" />
        <category android:name="android.intent.category.LAUNCHER" />
    </intent>
</queries>
```

**K2Plugin.kt 新方法**:

```kotlin
@PluginMethod
fun listInstalledApps(call: PluginCall) {
    val pm = context.packageManager
    val intent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
    val resolveInfos = pm.queryIntentActivities(intent, 0)
    val apps = resolveInfos
        .map { it.activityInfo.applicationInfo }
        .filter { it.packageName != context.packageName }
        .distinctBy { it.packageName }
        .map { info ->
            mapOf(
                "packageName" to info.packageName,
                "label" to pm.getApplicationLabel(info).toString(),
                "iconUrl" to "kaitu-icon://package/" + URLEncoder.encode(info.packageName, "UTF-8")
            )
        }
    call.resolve(JSObject().put("apps", JSArray(apps)))
}
```

**Icon protocol** via `bridge.webViewClient.shouldInterceptRequest` for `kaitu-icon://` scheme,
returns PNG WebResourceResponse with 32×32 scaled icon from `pm.getApplicationIcon(packageName)`.

### 7.5 Icon URL Scheme (Tauri side)

`desktop/src-tauri/src/icon_protocol.rs`:

```rust
fn register(builder: tauri::Builder<R>) -> tauri::Builder<R> {
    builder.register_uri_scheme_protocol("kaitu-icon", |_app, req| {
        let url = req.uri();
        // parse kaitu-icon://<kind>/<id>
        match kind {
            "bundle" => mac::icon_for_bundle(&id),
            "exe"    => win::icon_for_exe(&id),
            _ => not_found(),
        }
    })
}
```

返回 32×32 PNG，浏览器 image cache 自动接管。

### 7.6 Bridge JSON Remapping (H1)

| 路径 | 序列化 | Remap 责任 |
|---|---|---|
| macOS / Win Tauri | Rust 输出 camelCase via `#[serde(rename_all="camelCase")]` | 无需 remap |
| Linux daemon | Go 输出 snake_case (default convention) | **standalone-k2.ts 内做 remap** before returning to webapp |
| Android Capacitor | Kotlin 输出 camelCase (manual map) | 无需 remap |

## 8. Privacy / Telemetry

**bypass list 永不出现在以下路径**：

- ❌ slog.Info / slog.Warn / slog.Debug
- ❌ `webapp/src/services/beta-auto-upload.ts` 收集范围
- ❌ `K2Plugin.uploadLogs()` zip 抓取范围
- ❌ `rule_miss` telemetry（即使 future 启用）
- ❌ Sentry breadcrumb / error context
- ❌ `console.debug([ConfigStore] buildConnectConfig: ...)` 只 emit `bypassEntryCount=N`，不 emit 内容

**用户协议 / 隐私政策**：v1 隐私政策无需更新（数据从未离开本机）。

## 9. Feature Flag

```ts
// webapp/src/config/apps.ts — features 扩展
features: {
  ...existing,
  appBypass: __K2_BUILD_CHANNEL__ === 'beta',  // stable=false, beta=true
}
```

- flag 关闭：Dashboard 入口不渲染、`/app-bypass` route 不挂、`buildBypassRoutes` 返 []
- flag 切换：现网用户从 stable 升级到 beta 时，自动看到入口；遗留的 storage entries 不被读到 → 零数据迁移

## 10. Trade-offs & Alternatives Considered

### 10.1 Daemon storage 接管 (rejected)

考虑过：让 daemon 通过 `/api/preferences/app-bypass` 持久化，webapp 仅读写。

**拒绝原因**：
- 多 4 套 native CRUD（macOS Tauri / Win Tauri / Linux daemon / Android plugin）违反 YAGNI
- 违反 webapp 拥有 user preference 的架构边界（与 chnroute、auth token、antiblock 一致）
- Linux desktop 上 `_platform.storage` backend 本来就是 daemon `/api/storage`，零简化空间
- Icon URL / label 等 UI 元数据 daemon 不该存

### 10.2 白名单（Split-Include） (deferred to v2)

考虑过 "只这些 app 走 VPN" 模式。

**defer 原因**：
- chnroute 已是 host 维度的等价 split-include
- 真实工单 dominant 是 split-exclude 痛点（风控、地理校验）
- 一套 UI / 文案就比双模式简洁

### 10.3 macOS bundle ID 原生支持 (deferred to v2)

考虑过给 rule engine 加 `bundle_id` matcher。

**defer 原因**：
- 本 spec 用 "Native bridge 返 bundle 全集 helper names" 绕过，已可用
- 引入 bundle_id 是 k2 子模块改动，scope 失控

### 10.4 Inline base64 icon (rejected)

考虑过 JSON 内嵌 base64 PNG。

**拒绝原因**：
- Android JSON 体积可达 1 MB → 主线程 parse 50-150ms 阻塞
- 浏览器 image cache 完全 bypass，滚动重复 decode
- 跨平台用 custom protocol 净增 ~50 行 native，但收益巨大

## 11. i18n Key Inventory

zh-CN 母版（其余 6 locale：ja, en-US, en-AU, en-GB, zh-TW, zh-HK 草稿翻译 + LLM review，与既有 RouterDevices 同流程）：

```jsonc
// webapp/src/i18n/locales/zh-CN/dashboard.json — 追加
{
  "dashboard": {
    "advancedSettingsLocked": "VPN 已连接，请先断开后再修改高级设置",
    "disconnectVpn": "断开 VPN",
    "appBypassEntry": {
      "label": "不走代理的应用",
      "count": "{{count}} 个",
      "empty": "未选"
    }
  }
}

// webapp/src/i18n/locales/zh-CN/appBypass.json — 新文件
{
  "title": "不走代理的应用",
  "description": "这些应用的流量将不走 VPN，直接出网。适合避免被风控的应用（网银、微信、12306 等）。",
  "macMultiUserNote": "此处只列出当前账户启动的应用。",
  "addedSection": "已加入（{{count}}）",
  "availableSection": "可添加",
  "manualAdd": "+ 手动添加",
  "manualAddTitle": "添加进程名",
  "manualAddPlaceholder": "如 chrome.exe / Adobe Photoshop / com.tencent.mm",
  "manualAddConfirm": "添加",
  "manualAddCancel": "取消",
  "manualAddUnsavedConfirm": "未保存的输入将丢失，确认离开？",
  "refresh": "刷新",
  "rescan": "重新检测",
  "rescanResult": "已更新（共 {{count}} 个进程）",
  "processCount": "屏蔽 {{count}} 个进程",
  "kickedOutDueToConnect": "VPN 连接后无法修改，已返回主页",
  "loadFailed": "加载应用列表失败",
  "saveFailed": "保存失败，请重试",
  "uninstalledHint": "应用可能已卸载"
}
```

新 namespace `appBypass` 加入 `webapp/src/i18n/index.ts` 的 namespaces 列表，与既有 `account`/`dashboard` 等同级。

## 12. Phase 0 — Attribution Verification

**实施前必跑**。每平台独立 smoke：

| 平台 | 步骤 | Pass |
|---|---|---|
| macOS | 手写 routes 加 `{via:'direct', match:{process_name:['curl']}}` 在 chnroute 前 → 起隧道 → `curl https://ip.kaitu.io` vs 浏览器 `https://ip.kaitu.io` | IP 不同 |
| Windows | 同（`curl.exe`） | 同 |
| Linux desktop | 同 | 同 |
| Android | `{match:{package_name:['com.android.chrome']}}` → Chrome vs 其他 app 访问 ip.kaitu.io | IP 不同 |

任一失败 → 该平台 v1 砍出，开 k2 ticket 单独修。

## 13. Definition of Done

- [ ] Phase 0 全 4 平台通过（或砍出失败平台）
- [ ] Unit tests 覆盖 buildBypassRoutes、appBypassStore、bridge remap、Connected-Guard、page guard；coverage ≥ 80% on new files
- [ ] E2E spec (Playwright, web/Linux 路径) 通过
- [ ] 7 个 locale 全翻译 commit
- [ ] `apps.ts` `features.appBypass` 配 build channel 默认值
- [ ] Release notes 列 RoutingModeSelector 锁定 behavior change
- [ ] Privacy invariant 写入 `webapp/CLAUDE.md` Domain Vocabulary
- [ ] `make build-android` / `build-macos` / `build-windows` 全过
- [ ] Manual QA：icon 渲染、Chrome 全 helper、微信支付不风控、删除已卸载、`<queries>` 在 Android 11/12/13/14 实测

## 14. Cross-References

- **Existing**: `k2/rule/target.go` (MatchConfig)、`webapp/src/types/client-config.ts` (TS schema)、`webapp/src/stores/config.store.ts` (buildConnectConfig)、`webapp/src/stores/vpn-machine.store.ts` (USER_DISCONNECT)、`webapp/src/pages/Dashboard.tsx:593` (Advanced Settings collapse)
- **Not analogous**: `k2/gateway/router_device.go` is k2r MAC allowlist — different feature, different platform, kept separate per design decision (see §1 scope statement)
