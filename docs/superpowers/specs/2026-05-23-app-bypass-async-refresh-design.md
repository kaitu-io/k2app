# App Bypass UI — Async Refresh Optimization

**Status:** Draft (2026-05-23) — pre-implementation
**Scope:** v0.4.5 patch / next desktop & mobile release
**Owner:** webapp + mobile/plugins/k2-plugin (Android)

## Problem

`AppBypass.tsx` 在 mount 和按 Refresh 时把整个"Add more"section 盖成
spinner，期间用户感觉页面卡住。诊断发现 5 个独立阻塞源叠加：

1. **Android plugin 主线程阻塞** — `K2Plugin.kt:listInstalledApps` 在
   Capacitor `@PluginMethod` 默认线程（main）上做
   `pm.queryIntentActivities` + 逐 app `getApplicationLabel` /
   `getInstallSourceInfo`。WebView 渲染共享 main thread → 几百 app
   的设备上整个 webapp 冻 1–3 秒。
2. **Android 上 `listInstalled()` 跑两次** — `refresh()` 先并行调
   `loadAutoDetected()`（内部 `listInstalled`），自己再 `await
   appList.listInstalled()`。PackageManager 全枚举两遍，IPC 数据
   marshal 两遍。
3. **Component-state cache** — `candidates` 在组件 state，离开
   AppBypass 页面后销毁。每次进入重跑 IPC。
4. **页面级 `loading` 状态** — 一个 boolean 控住"Add more"section 全部
   显示。即使有上次的缓存数据也会被 spinner 盖住，refresh 期间
   不可浏览。
5. **Search keystroke 无防抖** — 每次按键触发 `filteredAvailable`
   全量 filter + render 几百个 `Stack+Avatar` MUI 组件。

桌面端（macOS / Windows）的 `list_running_apps` 已用 Rust
`spawn_blocking`，OS 线程层无问题；瓶颈纯在 webapp render 与
IPC 数据量。

## Goals

* 进入 AppBypass 页面 **< 100ms 首帧可交互**（有缓存时）/
  **< 300ms 首帧可交互**（首次冷加载，候选区显示骨架/进度条
  但页面其他 3 个 section 完全可用）。
* Android 设备上 `listInstalled` IPC **不阻塞 WebView 渲染** —
  主线程 frame time 在枚举期间保持 < 16ms。
* Refresh 按钮不打断当前浏览状态 — 列表保留旧数据，只在顶部
  显示细进度条。
* 每次进入页面或按 Refresh **最多触发 1 次** `listInstalled` /
  `listRunning` IPC。
* Search 输入流畅 —— 输入框响应不被 filter+render 阻塞。

## Non-goals

* 列表虚拟化（react-window / IntersectionObserver lazy mount）—
  YAGNI for v0.4.5；如果按完优化后桌面 macOS 仍 jank 再单独立项。
* `candidates` 跨 session 持久化到 `_platform.storage` —— 设备 app
  列表会变；in-memory 单 session 复用足够。
* 增量 IPC streaming（chunked listInstalled）—— 复杂度高、收益边际。
* 改 Tauri Rust 侧 `list_running_apps` —— 已 `spawn_blocking`。
* iOS App Bypass —— 当前未启用 iOS appList provider，本次不动。

## Architecture

### Store-driven candidates (Change A)

把候选列表从组件 state 提升到 `app-bypass.store`：

```ts
interface AppBypassState {
  // …existing fields…
  candidates: Candidate[];          // in-memory cache (not persisted)
  candidatesLoadedAt: number;       // epoch ms of last successful refresh
  candidatesLoading: boolean;       // true while a refresh is in flight
  candidatesError: string | null;   // i18n key on failure (null on success)
}
```

`Candidate` 类型从 `AppBypass.tsx` 提升到 store 文件并 export。
`candidatesError` 存 **i18n key**（如 `'dashboard:appBypass.loadFailed'`），
不存翻译后字符串 —— store 层无 i18n context。页面渲染时 `t(error)`。

新 action：

```ts
refreshCandidates(): Promise<void>
```

行为：

1. 如果已有 in-flight refresh（store 内 `inflightPromise: Promise<void> | null`
   不为 null），直接 return 同一 Promise（in-flight dedup —— 防
   spam-click）。`inflightPromise` 在 finally 块清 null。
   注：`inflightPromise` 是 store 模块作用域的私有变量，不暴露在
   `AppBypassState` 上（用户无需观察它）。
2. 设 `candidatesLoading=true`，**保留** 旧 `candidates` 不动
   （让 UI 继续显示 stale cache）。
3. 选 provider：
   * 若 `_platform.appList.listInstalled` 存在 → 调它，结果
     转 `Candidate{kind:'package'}` 写入 `candidates`，并将
     原 `InstalledApp[]` 传给 `loadAutoDetected(preFetched)`。
   * 否则若 `listRunning` 存在 → 调它，结果转
     `Candidate{kind:'process'}` 写入 `candidates`。无 install
     列表，`loadAutoDetected()` 不传参数（store 内部会跑无 op
     早 return）。
   * 都不存在 → `candidates=[]`、`candidatesError=null`（不算错误）。
4. 成功：`candidatesLoadedAt=Date.now()`、`candidatesError=null`、
   `candidatesLoading=false`。
5. 失败：`candidatesError = <i18n key string>`、**保留**旧
   `candidates`、`candidatesLoading=false`。`console.warn`
   原始 error。

### Dedup listInstalled (Change B)

`loadAutoDetected` 签名扩展：

```ts
loadAutoDetected(preFetchedInstalled?: InstalledApp[]): Promise<void>
```

* 有 `preFetchedInstalled` → 直接跑 `detector.detect(preFetchedInstalled)`，
  跳过 IPC。
* 无 → 现有行为（自己 `await provider.listInstalled()`）。
* 兼容现有 store unit test —— 不传参的调用路径保持不变。

`refreshCandidates` 在 Android（有 listInstalled）路径上**只调一次**
`listInstalled`，把结果同时喂双方。

### Android plugin 后台线程化 (Change C)

`K2Plugin.kt:listInstalledApps` 改造为：

```kotlin
@PluginMethod
fun listInstalledApps(call: PluginCall) {
    Thread {
        try {
            val pm = context.packageManager
            val intent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
            val resolved = pm.queryIntentActivities(intent, 0)
            val seen = mutableSetOf<String>()
            val apps = JSArray()
            for (info in resolved) {
                try {
                    val appInfo = info.activityInfo.applicationInfo
                    val pkg = appInfo.packageName ?: continue
                    if (pkg == context.packageName) continue
                    if (!seen.add(pkg)) continue
                    val label = pm.getApplicationLabel(appInfo).toString()
                    val iconUrl = "kaitu-icon://package/" +
                        java.net.URLEncoder.encode(pkg, "UTF-8")
                    val installer: String? = try {
                        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                            pm.getInstallSourceInfo(pkg).installingPackageName
                        } else {
                            @Suppress("DEPRECATION")
                            pm.getInstallerPackageName(pkg)
                        }
                    } catch (e: Exception) { null }
                    apps.put(JSObject().apply {
                        put("packageName", pkg)
                        put("label", label)
                        put("iconUrl", iconUrl)
                        if (installer != null) put("installerPackageName", installer)
                    })
                } catch (e: Exception) {
                    Log.w(TAG, "listInstalledApps: skip entry", e)
                    // continue — one bad entry doesn't kill the whole list
                }
            }
            call.resolve(JSObject().apply { put("apps", apps) })
        } catch (e: Exception) {
            call.reject("LIST_INSTALLED_FAILED", e)
        }
    }.start()
}
```

**两点不同于原版：**

1. 全函数体包在 `Thread {}.start()` 内 —— 对齐 K2Plugin.kt 内
   既有的 `checkWebUpdate` / `downloadWebUpdate` 等异步 plugin
   pattern（line 308, 361, 399, 526）。Capacitor 自动 marshal
   `call.resolve` 回 main。
2. 内层 per-entry `try/catch` —— 一个 app 的 `getApplicationLabel`
   或 `getInstallSourceInfo` 抛异常不再让整个枚举跳出（原版的
   外层 `catch (e: Exception) { call.reject(...) }` 太宽，部分
   失败会把整个 IPC 拒绝）。

无 plugin API 变更、无 TypeScript `dist/` 定义变更、无 webapp
bridge 改动。

### Search input 解耦 (Change D)

`AppBypass.tsx` 用 React 18 `useDeferredValue`：

```ts
const deferredSearch = useDeferredValue(searchQuery);
const filteredAvailable = useMemo(() => {
  const q = deferredSearch.trim().toLowerCase();
  if (!q) return available;
  return available.filter(c =>
    c.label.toLowerCase().includes(q) || c.id.toLowerCase().includes(q),
  );
}, [available, deferredSearch]);
```

输入框值仍是 `searchQuery`（立即响应）；filter 用 `deferredSearch`
（React 在空闲时跑）。比 `setTimeout` debounce 更原生、无清理逻辑。

### Manual rescan 复用缓存

Manual section 的 rescan 按钮（`AppBypass.tsx:288-310`）改为
**先查 store `candidates` cache** 找 match；只有 cache miss 才
fallback 调 `listRunning()`。常见情况（用户刚加完应用、cache 还热）
零 IPC。

```ts
const cache = useAppBypassStore.getState().candidates;
let match: Candidate | undefined = cache.find(c => c.kind === 'process' && c.id === e.id);
if (!match) {
  const running = await window._platform!.appList!.listRunning!();
  match = running.find(r => r.id === e.id) as any;
}
if (!match) return; // user-visible toast: app not running
await useAppBypassStore.getState().rescan(e.id, (match as any).processNames);
```

## Data flow (after)

```
Mount AppBypass page
  │
  ├─ render immediately:
  │    – Rule card                (entries.length from store)
  │    – Smart detection          (autoDetected from store, possibly stale)
  │    – Manual added             (entries from store)
  │    – Add more / candidates    (cached candidates from store, may be []
  │                                 first time; LinearProgress shown if
  │                                 candidatesLoading)
  │
  └─ useEffect → store.refreshCandidates()
       │
       ├─ in-flight dedup check
       ├─ provider.listInstalled() | listRunning()   ← single IPC
       │    (Android side runs on Thread, doesn't block main)
       ├─ set candidates + candidatesLoadedAt
       └─ store.loadAutoDetected(preFetched)         ← no second IPC
            └─ regional detector → autoDetected updated

Rule card Refresh button
  └─ store.refreshCandidates() + toast on completion
       (UI shows in-place LinearProgress; candidates list NOT cleared)

Search input keystroke
  ├─ setSearchQuery(value)             ← <TextField> re-renders instantly
  └─ useDeferredValue defers filter+map until idle
```

## Failure modes

| Scenario | Behavior |
|----------|----------|
| First mount, no cache, IPC succeeds | `candidatesLoading=true` → `LinearProgress` shown above list area; list updates when IPC returns |
| First mount, no cache, IPC fails | `candidatesError` set; show error caption in Add more section; other 3 sections unaffected |
| Second mount, has cache, IPC succeeds | Cache rendered immediately; background refresh; list updates (no flicker — react reconciler diffs) |
| Second mount, has cache, IPC fails | Cache preserved; small inline error caption; `console.warn` |
| User taps Refresh while one is in flight | Returns same Promise; no double IPC |
| VPN comes up mid-refresh | Page navigates to `/`; in-flight refresh continues writing to store (harmless — store survives nav) |
| Manual rescan, cache hit | Zero IPC; store updated; toast |
| Manual rescan, cache miss | Falls back to `listRunning()`; cache NOT refreshed (intentional —— rescan is targeted) |
| Android: PackageManager throws on one app | Skipped via inner try/catch; remaining apps returned |
| Android: PackageManager throws on whole `queryIntentActivities` | `call.reject` → webapp `candidatesError` |

## Privacy invariant (unchanged)

* `candidates` 仅在内存中（Zustand state），**不写**
  `_platform.storage`、**不写**日志（`console.debug` 只打数量）、
  **不进**反馈 zip 上传。
* `app-bypass-privacy.test.ts` 的现有断言（`buildConnectConfig`
  调试日志不出现 entry name，只出现 count）继续通过 —— 本次
  改动不触及 `buildConnectConfig`。
* 不新增任何对 `console.log/info/debug` 写 candidate label 或 id
  的语句。

## Test plan

### vitest (webapp, must all pass)

新增到 `webapp/src/stores/__tests__/app-bypass.store.test.ts`：

1. `refreshCandidates()` 在 Android（有 `listInstalled`）路径调 1 次
   `listInstalled`（不是 2 次）。
2. `refreshCandidates()` 在桌面（有 `listRunning`，无 `listInstalled`）
   路径调 1 次 `listRunning`。
3. `refreshCandidates()` 期间 `candidatesLoading=true`，旧 `candidates`
   保留不清空。
4. `refreshCandidates()` 二次连续调用（第一个未 resolve 时）返回
   同一 Promise，underlying provider 只被调 1 次。
5. `refreshCandidates()` 失败：旧 `candidates` 保留，
   `candidatesError` 写入；`autoDetected` 状态由 `loadAutoDetected`
   分支自己决定（成功 / 失败独立处理）。
6. `loadAutoDetected(preFetched)` 跳过 `listInstalled` IPC（mock
   计数 = 0）。
7. 无 provider（`_platform.appList` 缺失）：`refreshCandidates`
   resolve，`candidates=[]`、`candidatesError=null`。

现有测试（privacy + load/add/remove/rescan/clear + 各 country
分支）**保持原样通过** —— 本次改动对 entries 持久化、privacy
路径、country dispatcher 行为均无破坏。

### 桌面 smoke (我跑，standalone-k2.ts 或 macOS Tauri dev)

* `make dev-macos` → 进入 AppBypass → 观察控制台 IPC log（应 1 次
  `list_running_apps`）→ 按 Rule card Refresh → 观察列表不被
  spinner 盖、顶部 LinearProgress 闪一下 → 在 search 框快速输入
  10+ 字符 → 输入框无延迟。
* 退回 Dashboard → 重新进 AppBypass → 缓存命中：列表立即出现，
  顶部 LinearProgress 闪一下（后台刷新）。

### Android smoke (你跑 ~5 分钟)

设备 prerequisites：装 0.4.4 release 或 dev build。

步骤：
1. 卸载旧版，安装新构建（带本次改动）。
2. 启动 app → Dashboard 立即可见 → 进入 AppBypass。
3. **观察 1**: 页面 header 与"已添加 / 智能检测"section **必须立
   即出现**（不再有 1-3 秒白屏）；"添加更多 (Add more)"section
   上方应短暂显示 LinearProgress 横条，1-2 秒后填充 app 列表。
4. **观察 2**: 加载期间，用手指上下滑动整个页面 —— **必须流畅**，
   不能有冻结。
5. 按 Rule card 的 Refresh 按钮 → 列表不消失、只看到顶部
   LinearProgress 一闪。
6. 在 search 框快速输入"a b c" → 输入框 **每个字符立即出现**，
   不卡。
7. 退回 Dashboard → 立即重新进入 AppBypass → 列表立即出现（缓存
   命中）。

**计时阈值** (Pass / Fail):
- 进入 AppBypass 到第一帧渲染 < 300ms (PASS)
- 滑动 FPS 主观 ≥ 30（不"顿"）(PASS)
- search 输入到字符显示 < 100ms (PASS)

**反例对照**：在 0.4.4 release 上跑同样步骤，对比"卡 1-3 秒"+
"输入卡顿"的体感。

### iOS smoke (无需 —— 改动不影响 iOS)

iOS Capacitor bridge `appList` 当前为 `undefined`（`capacitor-k2.ts:235`
只在 Android 注册），所以 iOS 上 AppBypass 行为不变（早 return
"this feature isn't available")。

## Confidence model

| Layer | Confidence | Notes |
|-------|-----------|-------|
| A (store cache) | 10/10 | Pure Zustand pattern, vitest 覆盖 |
| B (dedup) | 10/10 | 单签名扩展，向后兼容 |
| C (Android thread) | 6/10 → **10/10 after device smoke** | 既有 K2Plugin pattern；理论无风险；但跨进程线程化必须真机验证 |
| D (useDeferredValue) | 10/10 | React 18 原生 API |

**封顶 10/10 唯一硬依赖：Android 真机 smoke 通过**（步骤见上）。
桌面/web 端无设备风险。

## Out-of-scope follow-ups

* List virtualization (if user still feels macOS jank with 500+ processes)
* iOS App Bypass provider (separate spec)
* `listInstalled` 缓存到 `sessionStorage` 跨页面 reload（边际收益）

## Files touched

| File | Change |
|------|--------|
| `webapp/src/stores/app-bypass.store.ts` | Add candidates state + refreshCandidates action; loadAutoDetected accepts preFetched param |
| `webapp/src/stores/__tests__/app-bypass.store.test.ts` | Add 7 new test cases |
| `webapp/src/pages/AppBypass.tsx` | Remove component candidates/loading/error state; read from store; useDeferredValue for search; rescan-from-cache |
| `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt` | Wrap listInstalledApps body in `Thread{}`; per-entry try/catch |

No changes to:
- `K2Plugin.kt` plugin API surface, TypeScript `dist/` definitions
- `tauri-k2.ts` / `capacitor-k2.ts` / `standalone-k2.ts` bridges
- `app_list.rs` Rust side
- Wire protocol / storage shape / privacy boundary
- i18n files (uses existing keys; new `candidatesError` mapped via existing `appBypass.loadFailed`)
