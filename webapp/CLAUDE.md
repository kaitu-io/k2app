# Kaitu WebApp

Shared React UI codebase running on Web, Desktop (Tauri), and Mobile (Capacitor).

**Long-form notes (not auto-loaded — read the one you need before changing that code):** `docs/webapp/runtime-notes.md` (兼容模型 rationale, Router Tab mechanism, Key Patterns history: revision token, config/failover fields, funnel, viewport) · `docs/webapp/brand-details.md` (full Brand rules, Stripe flow, icons, kaitu-only surfaces).

## Constitutional Rule: Bridge Layer Boundary

**Webapp 代码绝不可跨越 bridge 层直接访问底层服务。** 所有对 desktop daemon / mobile engine / router 的调用，必须且只能通过 `window._k2` 和 `window._platform` 完成。

Bridge 层（`tauri-k2.ts` / `capacitor-k2.ts` / `standalone-k2.ts`）是唯一的封装边界。

违反条件（以下行为均被禁止）:
- Webapp 代码中出现 `fetch('/api/core')` 或任何直接 HTTP 请求 daemon 端口
- Webapp 代码中直接 `import` `@tauri-apps/*` 或 `@capacitor/*`（bridge 文件除外）
- Webapp 代码中直接调用 `window.__TAURI__.core.invoke()` 或 `Capacitor.Plugins`（bridge 文件除外）
- 任何独立页面（如 `debug.html`）内联重造 bridge 实现，而不复用已有 bridge 模块

唯一允许访问底层 API 的文件:
- `src/services/tauri-k2.ts` / `capacitor-k2.ts` / `standalone-k2.ts` — the three bridges
- `src/services/tauri-storage.ts`、`desktop-storage-migration.ts` — 也 import `@tauri-apps/api/core`（storage 后端，只被 bridge 层消费）
- `src/main.tsx`（bridge 选择 + boot 握手）、`utils/viewport-scaling.ts`（只读 `window.__TAURI__` 做平台检测）

## 兼容模型：支持地板 + 运行时能力探测（Web OTA R2）

**只有一个真理的 webapp 版本——最新版。** 最新 webapp 必须在支持地板（`contracts/webapp-support-floor.json`）以上的**所有** app 版本上正确运行。Spec: `docs/superpowers/specs/2026-08-14-web-ota-design.md` §4。全文（含理由）：`docs/webapp/runtime-notes.md`。

规矩（违反即 bug）：

- 新 bridge 能力（`BRIDGE_API_VERSION` bump 引入的方法）的每一处使用都必须有运行时探测分支——**优先存在性检测**（`typeof fn === 'function'`），版本比较只做兜底。
- 封闭在 bridge 文件内的能力（`ui_boot_ok`、`storage_migration_*`、`confirmWebBootOk`）就地 try/catch 静默降级（Linux 那份额外自我门控 `window.__K2_GATEWAY__`）。会漏进业务代码的能力必须由唯一供给者模块导出语义化 flag（目前还没有）；`capacitor-k2.ts` 的 `appList` 是仅剩的 `getPlatform() === 'android'` 平台门（channel 门已改为 `getUpdateChannel` 能力探测）。禁止页面/组件直接探测 `window._k2` / `window._platform` 方法存在性或散落 raw 版本比较。
- **契约门**：`types/__tests__/bridge-contract.test.ts` 把整个 bridge 方法面快照进 `contracts/bridge-api.json`（golden，只读）——方法表漂移而不 bump 版本即红。故意改 bridge 后：bump `BRIDGE_API_VERSION` → `cd webapp && UPDATE_BRIDGE_CONTRACT=1 npx vitest run src/types/__tests__/bridge-contract.test.ts` → 同步壳镜像：Android `K2PluginUtils.BRIDGE_API_VERSION`、iOS `k2BridgeApiVersion`、desktop `DESKTOP_BRIDGE_VERSION`（web_ota.rs）由同一测试门控；**Linux `LinuxBridgeVersion`（`k2/webui/webota.go`）不门控**——同一次提交手动改它。现状：Linux = 4，其余 = 6。地板 `bridge` = 1；地板一旦升到 > 4，Linux 壳会被 `min_bridge` 静默挡在所有 web OTA 之外，没有任何测试会红。
- `src/__tests__/boot-handshake-wiring.test.ts` 断言 `main.tsx` **源码顺序**：`confirmUiBootOk` / `confirmWebBootOk` 必须在 `ReactDOM.createRoot` 之后。
- `min_*` 闸门是安全刹车不是兼容手段：bump 地板文件 = 显式砍旧版本支持的决策，须 review 给理由；bump `BRIDGE_API_VERSION` 不许顺手改地板。

## Hard Rules

```
DO NOT:
  - Direct fetch/axios calls from pages/components (use cloudApi; cache via cacheStore)
  - Hardcoded UI text in components (i18n — new text goes to zh-CN first, then every other locale dir)
  - Access window._k2 for API calls (cloudApi) or for platform capabilities (window._platform)
  - Use npm (use yarn)
  - Display response.message to users (use code + i18n)
  - Bypass bridge layer (see Constitutional Rule above)
  - Ship a change without weighing its Desktop / Mobile / Web impact

DO:
  - VPN control via window._k2.run(action, params)
  - Cloud API calls via cloudApi.request() (src/services/cloud-api.ts); read-through cache / SWR by
    composing cacheStore (src/services/cache-store.ts) — 401 refresh and 403003 handling live in cloud-api.ts
  - Device UDID via getDeviceUdid() from services/device-udid.ts (NOT _platform)
  - Zustand stores for state; Material-UI for components
  - Errors displayed via response.code mapped to i18n keys
```

## Error Handling

**Rule: `response.message` is for debug logs only. Users see i18n text mapped from `response.code`.**

Why: backend messages are English-only technical text that exposes internal API paths (table in `docs/webapp/runtime-notes.md`).

**Where the mapping lives**: `utils/errorCatalog.ts` — the single declaration of code → i18n key, split into **two** tables by ORIGIN because Center API and k2 engine codes collide (400/401/403/503 mean different things on each side):

| Table | Channel | Rendered by |
|---|---|---|
| `API_ERROR_CATALOG` | `SResponse.code` from cloudApi | `getErrorMessage()` / `handleResponseError()` (`utils/errorCode.ts`) |
| `ENGINE_ERROR_CATALOG` | `ControlError.code` from `_k2.run()` | `getErrorI18nKey()` (`services/vpn-types.ts`) — returns a **fully-qualified** `ns:key` |

Never hand-write a code→key mapping outside that file. In catch blocks, log the raw error and show an i18n fallback string to the user.

Guards: `ENGINE_ERROR_CODES` mirrors `k2/engine/error.go` — `services/__tests__/k2-engine-codes.test.ts` parses the Go source, **fail-closed** (a missing `k2/engine/error.go` throws, never skips). `utils/__tests__/errorCatalog.test.ts`: every declared code has copy in every locale. `i18n/__tests__/static-keys.test.ts`: every static `t('key')` in `src/` exists (dynamic template keys are a blind spot).

### API Error Code Constitution

Every backend error code (`api/response.go`) MUST have a matching entry in `utils/errorCatalog.ts`.

Checklist for new backend error codes:
1. Add constant to `api/response.go`
2. Add to `ERROR_CODES` in `utils/errorCatalog.ts`
3. Add a row to `API_ERROR_CATALOG` (or list the code in `LOG_ONLY_CODES` if it is deliberately never shown) — `getErrorMessage()` derives from the table, there is no `switch` to edit
4. Add i18n translation in every locale
5. Never duplicate error code constants outside `errorCatalog.ts`
6. **Regenerate the cross-layer contract** — the error-code registry is part of
   `contracts/api-contract.json`; see the contract gate in `api/CLAUDE.md` "Brand".

Code ranges (all in one `ERROR_CODES` object — the numeric ranges **overlap**, so origin is decided by channel, never by number):
- `0`: Success
- `400-503`: Backend HTTP-aligned codes (sync with `api/response.go`) — includes `500` / `503`
- `<base>NNN`: Backend custom business codes, prefixed by the HTTP-aligned base they
  refine — `400001+` (bad request), `402001` (payment), `403001+` (forbidden),
  `405001` (unavailable), `409001` (conflict), `422001+` (invalid argument). Pick the
  base whose semantics match; don't default everything to `400xxx`.
- `100-112`: Frontend-only network errors
- `504`, `511-573`: Frontend-declared VPN / action / API errors sitting right beside backend 500/503 (570-573 mirror the engine `ConnectionFatal` family)
- `-1`: Frontend-only cloud API network failure

## Architecture: Split Globals

Two globals, installed by `main.tsx` before stores init (see Bootstrap), plus one internal module:

- `window._k2: IK2Vpn` -- VPN tunnel control only (single `run(action, params)` method)
- `window._platform: IPlatform` -- Platform capabilities. **The member list lives in `types/kaitu-core.ts` `IPlatform`** — read it there, don't copy it here.
- `cloudApi` (`services/cloud-api.ts`) -- `request(method, path, body?) -> SResponse`: Bearer injection, `X-K2-Brand`, single-flight 401 refresh (`_doRefresh()`), 403003 session clear. Caching / SWR is **not** a wrapper — callers compose `cacheStore` (`services/cache-store.ts`) around it themselves (`hooks/useUser.ts`, `pages/Purchase.tsx`).

```
main.tsx ─▶ tauri-k2 | capacitor-k2 | standalone-k2  (bridge, chosen at boot)
         ─▶ window._k2 / window._platform
         ─▶ stores/ (Zustand; vpn-machine owns status delivery) ─▶ pages/ + components/
core/      getK2(), isK2Ready(), waitForK2()   (core/polling.ts useStatusPolling has no consumers)
```

`webapp/scripts/` (brand purity, tz-country generator, `smoke-dist.mjs`) is **not** the repo-root `scripts/`.

**VPN actions** (`window._k2.run`): `up`, `down`, `status`, `version`, `probe` (probe-service), `classify-apps` (App Bypass), `adb-*` (helper endpoint — see Key Patterns), `sync-credential` (Capacitor only), `relay-fetch` / `relay-add-nodes` (antiblock relay — disabled, below)

**⛔ Relay 已由 kill-switch 关停（2026-07-17）**：`services/relay-flag.ts` `RELAY_ENABLED = false` → 传输为**直连 only**（`resolve-and-fetch.ts`）。relay 代码全保留，恢复 = 改一行重发版；细节见 `docs/webapp/runtime-notes.md` 与 `docs/superpowers/specs/2026-07-17-disable-relay-restore-direct-design.md`。

## Brand (双品牌: kaitu / overleap)

**Spec**: `docs/superpowers/specs/2026-07-14-brand-split-design.md` §4. Brand is baked at
BUILD TIME — env `K2_BRAND=kaitu|overleap` (default `kaitu`) → Vite/Vitest define
`__K2_BRAND__` → `src/brands/index.ts` selects `KAITU_BRAND` / `OVERLEAP_BRAND`. No runtime switch.
Full rule text, Stripe flow, icons, kaitu-only surfaces: `docs/webapp/brand-details.md`.

- **`src/brands/` is the single source of truth** (`brandConfig`): brand differences go only into `src/brands/<id>/{index,theme,assets,locales}`. Never fork on brand id in components — add a gate to `BrandFeatures`, read `getCurrentAppConfig().features.*`.
- **Brand literals belong in `src/brands/<brand>/index.ts`, never in a page/component** — pages are statically imported, so a literal ships in EVERY brand's bundle (why `k2sInstallUrl` lives in the registry). `src/brands/overleap/index.ts` and `src/brands/i18n-vars.ts` stay free of `kaitu.io` / `开途` / `開途` **even in comments** (`types.ts` is type-only → exempt).
- **Artifact purity**: `webapp/scripts/check-brand-purity.sh <brand> dist` after any build-affecting change — kaitu build zero `overleap.io`; overleap build zero `kaitu.io|开途|開途` (bare `kaitu` protocol tokens like `kaitu-service/` and `kaitu-language` are intentional).
- **`X-K2-Brand` header**: injected ONLY in `services/cloud-api.ts` (`request()` + `_doRefresh()`). 403003 (BRAND_MISMATCH) clears the session and opens LoginDialog, mirroring 403002.
- **i18n is brand-neutral**: `{{brand}}` / `{{brandDomain}}` / `{{brandBaseUrl}}` / `{{supportEmail}}` interpolation, brand-exclusive copy in `src/brands/<brand>/locales/` overlays (guard `src/i18n/__tests__/brand-literals.test.ts`); the brand name is never translated.
- **Languages**: a brand OFFERS only `brandConfig.locales` (kaitu 7 = zh-CN / zh-TW / zh-HK / en-US / ko / ja / ru — **same list as `web/src/i18n/routing.ts`, change both together**; overleap all 23). Auto-detection is never persisted. `ar` / `fa` are RTL — inline `style={{ marginLeft }}` is NOT mirrored. Adding a language: `languages` registry + `locales/<lang>/` with every namespace (`i18n/__tests__/locale-coverage.test.ts`; structure via the repo-root `scripts/i18n-check-translation.mjs`, not `webapp/scripts/`) + the brand's `locales`.
- **Tests must be brand-adaptive**: `K2_BRAND=overleap npx vitest run` must also exit 0. Assert against `brandConfig.*` / `getBrandName()` (or `KAITU_BRAND` / `OVERLEAP_BRAND` for registry contracts); gate with `describe.runIf(brandConfig.features.x)` and give the closed-gate branch a **real assertion** (`Tunnels.test.tsx` "brand gate", `Purchase.privateNode.test.tsx` "brand payment-channel gate"). Brand literals in a test only as mock fixtures.
- **Purchase surface gate**: `utils/purchase-surface.ts` `purchaseSurfaceAvailable()` is the single authority for every purchase / subscription-management entry point — never re-derive at a call site (iOS without StoreKit bridge → false, Apple 3.1.1; Android with `features.androidPurchase` false → false). Guard: `pages/__tests__/Account.android-overleap.test.tsx`. A platform-only (`os !== 'ios'`) gate is never enough for a kaitu payment/programme surface — the overleap build ships it.
- **Icons**: only in `src/brands/<brand>/assets/`, generated from `brand-assets/<brand>/logo.svg` + `generate.sh` — never edit a PNG. **Shells** never import webapp brand code; the shared contract is the `K2_BRAND` env var and that assets directory.

## Bootstrap (main.tsx)

1. Module level: `installChunkReloadGuard()`, `Sentry.init` → then `await i18nPromise`
2. Bridge injection is done **by main.tsx itself**: `window.__TAURI__` → `injectTauriGlobals()` (`tauri-k2.ts`); `Capacitor.isNativePlatform()` → `injectCapacitorGlobals()` (`capacitor-k2.ts`); otherwise, if `window._k2`/`_platform` are missing → `standalone-k2.ts ensureK2Injected()` (the Linux `k2/webui` embed takes this branch too; the daemon only injects `window.__K2_GATEWAY__`)
3. `await ensureSeeded()` (`entry-pool.ts`) — primes the node pool so the first cloud request fired from store init can't race an empty pool
4. `initializeAllStores()` (no args) → `ReactDOM.createRoot().render(<App />)`
5. **After** render: boot handshake `confirmUiBootOk()` (Tauri) / `confirmWebBootOk()` (Capacitor, Linux) — order pinned by `src/__tests__/boot-handshake-wiring.test.ts`

## i18n

New text goes into `zh-CN` first, then every other locale dir (23 under `src/i18n/locales/`; which ones a brand offers — see Brand "Languages").

Namespaces: `i18n/locales/namespaces.ts` `namespaces` (account, auth, common, dashboard, feedback, invite, nav, privateNode, purchase, retailer, router, startup, theme, ticket, wallet). **Ignore the file's `DO NOT EDIT` banner** — the generator `scripts/i18n/split-namespaces.js` it names no longer exists; hand-editing is the only way to register a namespace. Its `namespaceMapping` (bare key prefix → namespace, e.g. `appBypass` → `dashboard`) is not read at runtime — only `i18n/__tests__/static-keys.test.ts` uses it to resolve bare keys.

## Router Tab (k2r headless + app direct-control)

App is the sole UI for a headless k2r router; discovery and control use the single anchor `http://10.17.79.1:1779` (`ROUTER_ANCHOR`, `services/router-service.ts`) — no gateway probing, no `lanIP`. Design: `docs/superpowers/specs/2026-07-17-k2r-headless-app-control-design.md`; mechanism (`router.store.ts` phases, `RouterPage.tsx`, retired gateway leftovers): `docs/webapp/runtime-notes.md`.

- All LAN traffic goes through `_platform.routerRequest` (SSRF-gated on both bridges), never raw `fetch`. `_platform.getDefaultGateway()` is **unconsumed**.
- `routerFetch()`: on 401 refresh the control key once and retry; still 401 → **back off 60s** (`force401BackoffUntil`) so the 2s poll doesn't hammer Center during k2r key rotation.
- Status polls only while the Router tab is mounted (no SSE — CapacitorHttp can't stream). `isRouterTakeover(s)` is the shared predicate for `RouterTakeoverBanner` and `RouterExclusionDialog`.
- `RouterExclusionDialog.tsx` warns **only when a second connection is about to start**, never on tab switch.
- Embedded gateway mode is retired (deleted in `521d346d`) — don't resurrect it.

## Bridge & VPN State Contract

**`transformStatus()` (`services/status-transform.ts`) is mandatory** — every bridge (`tauri-k2.ts`, `capacitor-k2.ts`, `standalone-k2.ts`) routes status responses *and* push events through it. Raw backend state MUST NOT pass through.

- Daemon `"stopped"` → `"disconnected"`.
- With `error` present (structured `{code, message}`, or legacy string → code 570) and state `disconnected` **or** `connected`, state becomes `'error'`; `retrying = connected && code ∉ {400,401,402,403}` (connected+error = TUN up, wire broken, engine retries; disconnected+error = engine gave up).
- VPN machine maps `error` to `idle` (terminal) or `reconnecting` (retrying) from that `retrying` flag (dispatch payload `isRetrying`) — error is a field overlay, never a machine state.
- `reconnecting` is transient (engine state stays `connected`). Do not treat it as a terminal disconnect.

## Key Patterns

Invariants only; store-init order, keep-alive tabs, last-server-URL, probe, dev proxy / dev mode and the history behind each rule are in `docs/webapp/runtime-notes.md` "Key Patterns".

- **VPN state machine** (`vpn-machine.store.ts`): 6 states (`idle`, `connecting`, `connected`, `reconnecting`, `disconnecting`, `serviceDown`); error is a field overlay, never a state. Module-level `dispatch(event, payload)` is the only way to change state. `initializeVPNMachine()` owns status delivery — `core/polling.ts useStatusPolling()` has no consumers, don't build on it.
  - **Staleness token**: any async `run('status')` must snapshot `currentRevision()` **before** the call and pass it to `applyStatus(status, rev)` — else a late poll re-enters `connected` after an SSE `disconnected` (UI claims protected, traffic in the clear). `connectEpoch` cannot serve this role. Pin: `vpn-machine.store.test.ts` "applyStatus: rejects a superseded revision".
- **Connection store**: `connectEpoch` is checked both *before* `run('up')` and *after* it returns, so a superseded attempt's failure can't tear down the live one.
- **Keep-alive tabs** hide with `visibility:hidden`; WebKit doesn't recomposite `opacity`/`filter` changes made while hidden — Dashboard's `translateZ(0)` toggle is the fix, new compositing-layer CSS there needs the same.
- **Config store (v3)** (`stores/config.store.ts`): `buildConnectConfig({serverUrl})` assembles the wire `ClientConfig`. Country detection is client-side only (`utils/geo-detect.ts` + committed `tz-country.gen.ts`, regen `cd webapp && node scripts/generate-tz-country.mjs`, locked by `tz-country.gen.test.ts`). **Any country written into `country` MUST pass `routableCountry()`** (utils/routes.ts).
- **Failover fields (F2)**: `buildConnectConfig` emits `fallbacks`, `netcheck` and `netcheck_dns` but **never `dns.direct`** — an OTA that set it would change DNS on every installed engine (`config.store.test.ts` "never dns.direct"). `ServiceAlert` shows its network banner for 101/106/107/108 but **not** for a reconnecting 503.
- **`ClientConfig` mirrors the Go wire contract** — webapp-only state stays outside it (e.g. `k2.vpn.last_server_url`, persisted by `connection.store` for cold-start restore, cleared on explicit `disconnect()`).
- **k2subs is desktop-only** (root CLAUDE.md "k2subs"): a raw `k2subs://` reaching the mobile engine (code 570) is always a webapp bug.
- **Funnel events**: `statsService.trackFunnel` / `trackFunnelOnce` / `trackFunnelDaily` (`services/stats.ts`) is the only entry; fire-and-forget, never awaited. Names typed by `services/funnel-events.ts`, locked to the Center registry by the cross-layer contract test — add there (and in the API) first.
  - Open brand-site links through `openExternal` (applies `withAppAttribution`), never `window.open`, or the visit counts as direct traffic.
  - **Never instrument `Purchase.tsx`'s preview effect or `handleOrder({preview:true})`** (infinite-loop history, 2026-09-11). `paywallSurface` mirrors the component's early returns — change one, change the other. `checkout_start` only for `!preview`, always with `plan` (`Purchase.funnel.test.tsx`).
  - Every in-app navigation to `/purchase` passes `state: { from: <PaywallSource> }`.
- **LoginDialog** is a global modal (`login-dialog.store`); guards open it instead of redirecting. **AuthGate** is a loading gate only; per-page login is `LoginRequiredGuard`.
- **Helper API routing**: `adb-*` actions route to `/api/helper`, not `/api/core` (Tauri `daemon_helper_exec`); new daemon helper actions follow this pattern.
- **Viewport scaling**: `main.tsx` puts CSS `zoom` (not `transform:scale()`) on **`#root`** below the 430px design width. **Anything positioned in design-space pixels must render inside `#root`** — pass `container={() => document.getElementById('root')}` to a numerically positioned MUI portal (`FeedbackButton`, `useDraggable`). `#discover-overlay` is deliberately outside `#root` (WebKit mis-renders zoomed iframes).
- **Web OTA stale chunks**: `utils/chunk-reload-guard.ts` reloads **once** on `vite:preloadError` (`k2_chunk_reload_once`); never loops.

## Commands

```bash
yarn install                             # from workspace root
cd webapp && yarn dev                    # Dev server
cd webapp && yarn build                  # = check-i18n.mjs --ci && tsc && vite build — i18n drift fails the build
cd webapp && yarn test                   # pretest runs the repo-root ../scripts/check-k2-plugin-fresh.sh: k2-plugin dist vs src, and the
                                         #   yarn-v1 `file:` copy in node_modules (a stale copy is only refreshed by `make build-k2-plugin`)
cd webapp && npx vitest run [--reporter=verbose]   # same tests, but SKIPS that pretest gate
cd webapp && K2_BRAND=overleap npx vitest run      # must also be green
cd webapp && npx playwright test         # E2E
cd webapp && npx tsc --noEmit            # Type check
cd webapp && UPDATE_BRIDGE_CONTRACT=1 npx vitest run src/types/__tests__/bridge-contract.test.ts   # regen bridge golden — only after a deliberate bump
webapp/scripts/check-brand-purity.sh <brand> dist   # artifact purity after any build-affecting change
# yarn build:openwrt / dev:openwrt + vite.config.openwrt.ts + scripts/dev-openwrt.sh: deprecated, pending removal
#   (retired gateway mode — they inject __K2_GATEWAY__ for the deleted gateway-k2.ts). Linux embed = plain `yarn build`.
```

## Troubleshooting

| Problem | Check |
|---------|-------|
| White flash on app start | `index.html` must use `background: #0f0f13` directly on `html, body` WITHOUT `@media (prefers-color-scheme: dark)`. Media query causes 100-300ms white flash on light-mode OS before MUI loads. |
| Vitest mock state leaks between tests | `src/test/setup.ts` runs a global `beforeEach` (`vi.clearAllMocks()` + localStorage mock reset) and `afterEach(vi.restoreAllMocks())`. `clearAllMocks` wipes implementations, not just call counts — re-call `mockFn.mockResolvedValue()` in each `describe`'s `beforeEach`, not once at module level. |
| Login fails with 422 | All login paths must include `udid` from `getDeviceUdid()` (in `services/device-udid.ts`) in POST body. Backend requires UDID for device association. |

## Domain Vocabulary

- **Route preset** — `config.store` persists `defaultVia` (`proxy|direct`) + `countryVia` (`direct|k2p|null`); derived presets `global` / `bypass` / `home` / `home_proxy`. `buildRoutes()` emits `match:{region: country}` on the direct branch for `bypass`; `match:{preset}` (e.g. `cn-access`) appears only on `k2p://home` routes. Legacy `ruleMode` survives solely as a v0/v1 migration field.
- **App routing** — region routing is `routes[].match.region` (no separate `app_bypass` field). Per-app overrides (`forceProxy` / `forceDirect`, `app-routes.store`, storage key `k2.routes.overrides`) are prepended by `buildConnectConfig()` as Tier-1 `{match:{apps, app_paths}}` routes; `app_paths` = the apps' directories (`InstalledApp.paths`), ORed with names by the engine.
  - Old overrides gain paths at boot via `syncOverridePaths()`; `pathsSynced: true` is persisted **only when the shell actually reported directories** — an OTA webapp can run on a pre-paths shell, and the reconcile must stay owed until that shell updates.
  - `AppBypass.tsx`: `listInstalled` is primary; `listRunning` is primary only when it is the sole provider (iOS has neither). With both (desktop), `foldRunningIntoInstalled()` folds running Windows exes under an installed app's dir into its `processNames` (see `desktop/CLAUDE.md` "App Bypass app lists").
  - Legacy `k2.advanced.app_bypass` is discarded on first `load()`. Spec: `docs/superpowers/specs/2026-05-27-app-bypass-routes-unification.md`.

## Style

- **Dark theme forced at runtime**: `ThemeContext` always applies `darkTheme`. `lightTheme` and the `theme` i18n namespace are deliberately retained (`void lightTheme` defeats dead-code elimination) so a switcher can return — don't delete them as dead code, and don't add `@media (prefers-color-scheme)` branches.
