# Webapp runtime notes — compatibility, router tab, key patterns

Moved verbatim from `webapp/CLAUDE.md` (2026-11-14) so the per-session doc stays small. **Read this before changing the bridge files, `BRIDGE_API_VERSION` / the support floor, `main.tsx` boot order, `router-service.ts` / `router.store.ts`, `vpn-machine.store.ts` / `connection.store.ts` / `config.store.ts`, `buildConnectConfig`, the funnel call sites in `Purchase.tsx`, or anything rendered through a MUI portal** — the invariant bullets in `webapp/CLAUDE.md` are the short form; the paragraphs below carry the mechanism and the incident that produced each rule.

## 兼容模型：支持地板 + 运行时能力探测（Web OTA R2）

**只有一个真理的 webapp 版本——最新版。** 最新 webapp 必须在支持地板
（`contracts/webapp-support-floor.json`）以上的**所有** app 版本上正确运行。
Spec: `docs/superpowers/specs/2026-08-14-web-ota-design.md` §4。

规矩（违反即 bug）：

- 新 bridge 能力（`BRIDGE_API_VERSION` bump 引入的方法）的每一处使用都必须有
  运行时探测分支——**优先存在性检测**（`typeof fn === 'function'`），版本比较
  只做兜底。存在性检测天然免疫"TS 声明了但某平台 native 未实现"的平台漂移盲区。
- 调用完全封闭在 bridge 文件内的能力（`ui_boot_ok`、`storage_migration_*`、
  `confirmWebBootOk`）就地 try/catch 静默降级，bridge 层即唯一供给者（Linux 那份
  额外自我门控 `window.__K2_GATEWAY__`）。会漏进业务代码（显示/隐藏、改文案）的
  能力必须由唯一供给者模块导出语义化 flag——目前还没有这样的模块；`capacitor-k2.ts`
  的 `appList` 是仅剩的 `getPlatform() === 'android'` 平台门（channel 门已改为
  `getUpdateChannel` 能力探测）。禁止页面/组件直接探测 `window._k2` /
  `window._platform` 方法存在性或散落 raw 版本比较。
- **契约门**：`types/__tests__/bridge-contract.test.ts` 把整个 bridge 方法面快照进
  `contracts/bridge-api.json`（golden，只读）——方法表漂移而不 bump 版本即红。故意改
  bridge 后：bump `BRIDGE_API_VERSION` → `cd webapp && UPDATE_BRIDGE_CONTRACT=1 npx vitest run src/types/__tests__/bridge-contract.test.ts`
  → 同步壳镜像：Android `K2PluginUtils.BRIDGE_API_VERSION`、iOS `k2BridgeApiVersion`、
  desktop `DESKTOP_BRIDGE_VERSION`（web_ota.rs）由同一测试门控；**Linux `LinuxBridgeVersion`
  （`k2/webui/webota.go`）不门控**——k2 已 in-tree，同一次提交手动改它。现状：Linux = 4，
  其余 = 6。地板 `bridge` = 1，暂无影响；地板一旦升到 > 4，Linux 壳会被 `min_bridge`
  静默挡在所有 web OTA 之外，没有任何测试会红。
- `src/__tests__/boot-handshake-wiring.test.ts` 断言 `main.tsx` **源码顺序**：
  `confirmUiBootOk` / `confirmWebBootOk` 必须在 `ReactDOM.createRoot` 之后——重排 boot 序列即红。
- `min_*` 闸门是安全刹车不是兼容手段：bump 地板文件 = 显式砍旧版本支持的
  决策，须 review 给理由；bump `BRIDGE_API_VERSION` 不许顺手改地板。

## Error Handling — why codes, not messages

| Reason | Detail |
|--------|--------|
| Technical gibberish | `"request failed: POST /api/xxx"` is meaningless to users |
| No i18n | Backend messages are English-only |
| Security | Exposes internal API paths |

## Relay kill-switch (from Architecture: Split Globals)

**⛔ Relay 已由 kill-switch 关停（2026-07-17）**：`services/relay-flag.ts` `RELAY_ENABLED = false` → 传输为**直连 only**（`resolve-and-fetch.ts`，14s 直连预算；relay 开启时为 relay-first 5s 探测 + 直连兜底，控制面 SNI `k2.52j.me`），并一次性清除 relay 时代污染的 `k2_entry_url` 缓存。relay 代码全保留，恢复 = 改一行重发版；relay 行为细节见 `docs/superpowers/specs/2026-07-17-disable-relay-restore-direct-design.md`。

## Router Tab (k2r headless + app direct-control)

App is the sole UI for a headless k2r router. Discovery and control both use the single anchor `http://10.17.79.1:1779` (`ROUTER_ANCHOR`, `services/router-service.ts`) that k2r DNAT-intercepts on its forwarding path — no gateway probing, no `lanIP` concept. Design: `docs/superpowers/specs/2026-07-17-k2r-headless-app-control-design.md`.

- **`router-service.ts`**: `probeRouter()` GETs `{ROUTER_ANCHOR}/ping` (1.5s) and requires the `k2r: true` signature. `getControlKey(forceRefresh?)` caches locally, refetching via `POST /api/user/router-control-key`. `routerFetch()` (shared by `routerCore()` / `routerDevicesGet/Post()`) sends `Bearer <controlKey>`; on 401 it force-refreshes once and retries, and if still 401 **backs off 60s** (`force401BackoffUntil`) so the 2s poll doesn't hammer Center during k2r's key-rotation window. Everything goes through `_platform.routerRequest`, never raw `fetch`.
- **`router.store.ts`**: phases `none` / `unconfigured` / `online` / `offline` (tab stays visible when offline). `startPolling()` hits `routerCore('status')` every 2s **only while the Router tab is mounted** (no SSE — CapacitorHttp can't stream); a late poll is dropped if `unbindRouter()` cleared `router` meanwhile. `isRouterTakeover(s)` = `phase === 'online' && status?.state === 'connected'` — the shared predicate for `RouterTakeoverBanner` (Dashboard, when local VPN isn't `connected`) and `RouterExclusionDialog`.
- **`RouterExclusionDialog.tsx`** (`useExclusionGuard`): dual-connection warning fires **only when a second connection is about to start** (Router-tab connect while local VPN is connected, or Dashboard connect while takeover) — never on tab switch. Also owns the unbind confirm (MUI, not `window.confirm`).
- **`RouterPage.tsx`**: `/router` tab appended to `Layout.tsx` once `phase !== 'none'`; enterprise multi-slot routers (`routerSlots(s) !== null`) auto-redirect to `/router` (`Layout.tsx`). Device list uses `routerDevicesGet/Post`.
- **Bridge boundary**: LAN traffic goes through `_platform.routerRequest` (Tauri: Rust `router_http_request`, SSRF-gated; Capacitor: `CapacitorHttp` + TS-side SSRF gate — see `desktop/CLAUDE.md` / `mobile/CLAUDE.md`). `_platform.getDefaultGateway()` exists on both bridges but is **unconsumed**.
- **Retired** embedded gateway mode (`gateway-k2.ts`, `gateway-core.ts`, `gateway-storage.ts`, `GatewaySetup.tsx` — deleted in `521d346d`): the k2sub tab / `K2subConfig` / `GatewayUpgradeBanner` / `serverMode==='k2sub'` / `subsCountry` / `buildSubsUrl` are removed too (k2r production ships `-tags nowebapp`, so it never serves this webapp). Leftovers that still read `platformType === 'gateway'`: `standalone-k2.ts` platform detection, `cloud-api.ts` UA class, `social-sign-in.ts`; `k2/gateway` still reports `gateway` on `/api/platform` for non-nowebapp dev builds.

## Key Patterns

- **Store init**: `initializeAllStores()` runs layout → config.loadConfig (then `detectGeo`) → selfHosted.loadTunnel → appRoutes.load (legacy-key migration) → auth → vpn-machine → connection, in order; returns a cleanup function.
- **VPN state machine**: `vpn-machine.store.ts` — explicit 6-state machine (`idle`, `connecting`, `connected`, `reconnecting`, `disconnecting`, `serviceDown`) with transition table. Error is a field overlay (`error: ControlError | null`), not a state — `BACKEND_ERROR` routes to `idle` (terminal) or `reconnecting` (retrying). Module-level `dispatch(event, payload)` is the only way to change state. 3s debounce for `connected → reconnecting` is the only state timer. `initializeVPNMachine()` owns status delivery: event-driven (`onStatusChange` SSE / native) plus a safety-net poll (iOS never fires for engine error-overlay changes), else a 2s poll. `core/polling.ts useStatusPolling()` has no consumers — don't build on it.
  - **Staleness token (`revision`)**: every applied transition bumps a monotonic `revision`. Any code that issues an async `run('status')` must snapshot `currentRevision()` **before** the call and pass it to `applyStatus(status, rev)`; a mismatch drops the whole snapshot (state *and* `startAt`). Without it a poll issued while connected can land after an SSE `disconnected` and re-enter `connected` via `idle + BACKEND_CONNECTED` — UI claims protected while traffic runs in the clear. Push deliveries (SSE/native) pass no revision: they have no in-flight window. `connectEpoch` cannot serve this role — it lives in connection.store (which imports this file, so reading it back would be a cycle) and only bumps on user intent, missing backend-initiated drops.
- **Connection store**: `connection.store.ts` — owns tunnel selection, connect/disconnect orchestration, `connectedTunnel` snapshot (frozen at connect time for stable UI), and `connectEpoch` guard (prevents stale async ops — checked both *before* `run('up')` and *after* it returns, so a superseded attempt's failure can't tear down the live one)
- **Keep-alive tabs**: Layout caches visited tab outlets, hides inactive with `visibility:hidden`. Tab paths: `/`, `/invite`, `/discover`, `/account`, plus `/router` once `router.store.phase !== 'none'`
- **Keep-alive + GPU layers gotcha**: WebKit doesn't recomposite layers when `opacity`/`filter` are removed while an element is `visibility:hidden`. Dashboard uses a `translateZ(0)` toggle on hidden→visible transitions to force layer rebuild. Any new compositing-layer CSS changes on keep-alive tabs need similar consideration.
- **Config store (v3)**: `useConfigStore()` in `stores/config.store.ts` persists `defaultVia`/`countryVia`/`country`/`autoDetect` (key `k2.vpn.config`; legacy v0/v1/v2 shapes auto-migrate on first load). `buildConnectConfig({serverUrl})` assembles the wire-contract `ClientConfig` — emits `routes[]`, forces `log.level = __K2_BUILD_LOG_LEVEL__` (build-time constant). **Country detection is fully client-side** (`detectGeo` → `utils/geo-detect.ts`, system timezone via the committed `tz-country.gen.ts` artifact — regenerate with `cd webapp && node scripts/generate-tz-country.mjs`, drift-locked by `tz-country.gen.test.ts`); the server's `/api/geo` is frozen (hotfix, always `cn`) and no longer consulted. Any country written into `country` MUST pass `routableCountry()` (utils/routes.ts) — an unclamped bundle-less country 504s pre-2026-06-14 engines and silently routes as cn on newer ones; `buildRoutes` falls back to the global shape as the in-depth defense, and `connection.store.connect()` retries once with `forceGlobalRoutes` on 504.
- **Failover fields (F2)**: `buildConnectConfig` also emits `fallbacks: { [serverUrl]: fallbackUrls }` (Auto mode: ≤3 k2v5 URLs on other machines, picked by `utils/auto-tunnel-pick.ts`, keyed by the exact route `via`), `netcheck` (always — `brandConfig.netcheck`, plain-HTTP vendor endpoints with exact status/body, see `brands/types.ts`) and `netcheck_dns` (only when `brandConfig.netcheckDns` is non-empty: overleap's overseas resolvers; resolves the netcheck hosts only). It never sends `dns.direct` — direct-routed lookups keep the engine default for both brands, and since `dns.direct` is an old field, a web OTA that set it would change DNS on every installed engine. Old engines ignore `fallbacks` / `netcheck` / `netcheck_dns`. While reconnecting, the engine's hold code drives the reason line (`RECONNECT_REASON_CATALOG`); `ServiceAlert` shows its network banner for 101/106/107/108 but **not** for a reconnecting 503 (the engine just judged the network fine; 503 after a disconnect keeps the banner).
- **Last server URL persistence**: The k2v5 URL sent on last connect is persisted separately by `connection.store` under key `k2.vpn.last_server_url` (not inside `ClientConfig`, which mirrors the Go wire contract). Used only for cold-start restore when the webapp process was killed while VPN stayed up. Cleared on explicit `disconnect()`.
- **k2subs is desktop-only** (root CLAUDE.md "k2subs"): the webapp never resolves `k2subs://`; on mobile it passes one `k2v5://` URL — a raw `k2subs://` reaching the mobile engine (code 570) is always a webapp bug.
- **Probe service (retained, not wired up)**: `services/probe-service.ts` `runProbe(tunnels)` calls `_k2.run('probe', {urls, timeoutMs})`, records results into `probe.store`. Self-gates on `platformType === 'web'` and on any VPN state other than `idle` / `serviceDown` (probing while VPN is up would loop UDP through TUN). **`runProbe()` has no production caller** (`CloudTunnelList` dropped it in 528fe6027) and **`ProbeChip` has no importer**, so `probe.store` stays empty in production; Auto pick (`utils/auto-tunnel-pick.ts`) reads only `recommendScore`.
- **Funnel events (services)**: `statsService.trackFunnel` / `trackFunnelOnce` (once per install) / `trackFunnelDaily` (once per UTC day, `connect_ok`) (`services/stats.ts`) is the only entry for conversion-funnel events; call sites are fire-and-forget (`void …`), never awaited, never change control flow. Event names and paywall sources are typed by `services/funnel-events.ts` and locked to the Center registry by the cross-layer contract test — add a name there (and in the API) before using it.
- **Funnel + links to the brand site**: every platform `openExternal` (tauri-k2 / capacitor-k2 / web-platform) passes the URL through `withAppAttribution` (`utils/site-link.ts`), which adds `utm_source=app&utm_medium=<os>&utm_campaign=<first route segment>` to brand-site page links only (not third-party payment / store URLs, not `/api` / `/app`, not `embed=true`, never over an existing `utm_source`). Center's `app_to_web` path enters on that tag — open the site through `openExternal`, never `window.open` directly, or the visit counts as direct traffic.
- **Funnel + preview effect**: never instrument `Purchase.tsx`'s preview effect or `handleOrder({preview:true})` — that effect has a documented infinite-loop history (2026-09-11) and tracking there would also count quotes as checkouts. `paywall_view` is its own effect keyed on one boolean (`paywallReady`), reported at most once per mount and only when the rendered branch is a purchase surface (kaitu order UI, Stripe purchase state, iOS subscribe panel) and — for a signed-in user — the user record has loaded; members landing on a manage/status panel report nothing. `paywallSurface` mirrors the early returns at the bottom of the component: change one, change the other. `checkout_start` only fires for `!preview` and always carries `plan`.
- **Funnel + /purchase entry points**: every in-app navigation to `/purchase` passes `state: { from: <PaywallSource> }` — including the nav entries (`BottomNavigation` / `SideNavigation` → `nav`); Purchase maps anything outside `PAYWALL_SOURCES` to `direct`.
- **LoginDialog**: Global modal via `login-dialog.store`. Guards call `useLoginDialogStore(s => s.open)` (`LoginRequiredGuard` aliases it `openLoginDialog`) instead of redirecting
- **Feature flags**: `getCurrentAppConfig().features` controls route/tab visibility
- **Dev proxy**: Vite proxies `/api/core`, `/api/helper`, `/api/device`, `/ping` to `:1777` (or `K2_DAEMON_PORT`). Production uses absolute URL
- **Helper API routing**: `adb-*` actions route to `/api/helper` (not `/api/core`). Tauri bridge uses `daemon_helper_exec` IPC command. Standalone bridge checks `action.startsWith('adb-')` to pick endpoint. New daemon helper actions must follow this pattern.
- **Config-driven connect**: `_k2.run('up', config)` where config is assembled from the selected tunnel URL + user preferences via `buildConnectConfig`
- **AuthGate**: loading gate only — renders `LoadingPage` while `auth.store.isAuthChecking`, then children regardless of login (open access). Per-page login enforcement is `LoginRequiredGuard`; there is no service/version check here.
- **Viewport scaling**: `main.tsx` puts CSS `zoom` (not `transform:scale()`) on **`#root`** when the window is narrower than the 430px design width — `transform` would create a containing block and break `position:fixed`. `--app-zoom` on `<html>` mirrors the factor. Two consequences that bite:
  - **Anything measured in design-space pixels must render inside `#root`.** A MUI `<Portal>` defaults to `document.body`, which is *outside* the zoom — design-space coordinates are then read as device pixels. This clipped half of `FeedbackButton` on every sub-430px viewport (all iPhones below Pro Max) until it was given `container={() => document.getElementById('root')}`. Pass that container to any portal you position numerically; `useDraggable` documents the contract.
  - `#discover-overlay` is the deliberate *opposite*: a `<body>` sibling of `#root` with no zoom, so the Discover tab's cross-origin iframe renders at native scale (WebKit mis-renders zoomed iframes).
- **Web OTA stale chunks**: `utils/chunk-reload-guard.ts` (installed at the top of `main.tsx`) catches `vite:preloadError` — a lazy chunk 404 after a mid-session OTA swap of `current/` — and reloads **once**, guarded by sessionStorage `k2_chunk_reload_once`; never loops.
- **Dev mode**: `_platform.setDevEnabled?.(true)` enables WebView inspection (iOS `isInspectable`, Android `WebContentsDebugging`). State persisted to localStorage, auto-restored on launch

## Error Handling + Split Globals — original text

**Where the mapping lives**: `utils/errorCatalog.ts` — the single declaration of code → i18n key, split into **two** tables by ORIGIN because the Center API and the k2 engine independently chose HTTP-aligned codes whose meanings collide (400/401/403/503 mean different things on each side):

| Table | Channel | Rendered by |
|---|---|---|
| `API_ERROR_CATALOG` | `SResponse.code` from cloudApi | `getErrorMessage()` / `handleResponseError()` (`utils/errorCode.ts`) |
| `ENGINE_ERROR_CATALOG` | `ControlError.code` from `_k2.run()` | `getErrorI18nKey()` (`services/vpn-types.ts`) — returns a **fully-qualified** `ns:key` |

Never hand-write a code→key mapping outside that file. In catch blocks, log the raw error and show an i18n fallback string to the user.

`ENGINE_ERROR_CODES` in the same file mirrors `k2/engine/error.go`; `services/__tests__/k2-engine-codes.test.ts` parses the Go source and fails on drift. It is **fail-closed**: a missing `k2/engine/error.go` throws at load (never skips) — k2 is in-tree, so it is always there. `utils/__tests__/errorCatalog.test.ts` fails if any declared code lacks copy in every locale. `i18n/__tests__/static-keys.test.ts` does the same for every statically-written `t('key')` in `src/` (dynamic template keys are a known blind spot).

Two globals, installed by `main.tsx` before stores init (see Bootstrap), plus one internal module:

- `window._k2: IK2Vpn` -- VPN tunnel control only (single `run(action, params)` method)
- `window._platform: IPlatform` -- Platform capabilities (storage, clipboard, updater, routerRequest, appList, iap …). **The member list lives in `types/kaitu-core.ts` `IPlatform`** — read it there, don't copy it here.
- `cloudApi` (`services/cloud-api.ts`) -- `request(method, path, body?) -> SResponse`: Bearer injection, `X-K2-Brand`, single-flight 401 refresh (`_doRefresh()`), 403003 session clear. `cacheStore` (`services/cache-store.ts`) is the read-through cache / SWR layer callers compose on top.

```
main.tsx ─▶ tauri-k2 | capacitor-k2 | standalone-k2  (bridge, chosen at boot)
         ─▶ window._k2 / window._platform
         ─▶ stores/ (Zustand; vpn-machine owns status delivery) ─▶ pages/ + components/
core/      getK2(), isK2Ready(), waitForK2()   (core/polling.ts useStatusPolling has no consumers)
```

`webapp/scripts/` (brand purity, tz-country generator, `smoke-dist.mjs`) is **not** the repo-root `scripts/`.

### VPN Actions (via window._k2.run)

`up`, `down`, `status`, `version`, `probe` (probe-service), `classify-apps` (App Bypass), `adb-*` (helper endpoint — see Key Patterns), `sync-credential` (Capacitor only), `relay-fetch` / `relay-add-nodes` (antiblock relay — currently disabled, below)

### API Calls (via cloudApi / cacheStore)

Cloud API calls go through `cloudApi.request()` (auth headers, token refresh, 401 / 403003 side effects). Caching / SWR is **not** a wrapper — callers compose `cacheStore.get/set` around `cloudApi` themselves (`hooks/useUser.ts`, `pages/Purchase.tsx`).
