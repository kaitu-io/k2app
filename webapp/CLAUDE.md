# Kaitu WebApp

Shared React UI codebase running on Web, Desktop (Tauri), and Mobile (Capacitor).

---

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

---

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
  → 同步三个壳镜像（Android `K2PluginUtils.BRIDGE_API_VERSION`、iOS `k2BridgeApiVersion`、
  desktop `DESKTOP_BRIDGE_VERSION`，同一测试门控；Linux `LinuxBridgeVersion` 不门控，随 k2 提交）。
- `src/__tests__/boot-handshake-wiring.test.ts` 断言 `main.tsx` **源码顺序**：
  `confirmUiBootOk` / `confirmWebBootOk` 必须在 `ReactDOM.createRoot` 之后——重排 boot 序列即红。
- `min_*` 闸门是安全刹车不是兼容手段：bump 地板文件 = 显式砍旧版本支持的
  决策，须 review 给理由；bump `BRIDGE_API_VERSION` 不许顺手改地板。

---

## Hard Rules

```
DO NOT:
  - Direct fetch/axios calls from pages/components (use cloudApi; cache via cacheStore)
  - Hardcoded UI text in components (i18n — new text goes to zh-CN first, then all 6 other locales)
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

---

## Error Handling

**Rule: `response.message` is for debug logs only. Users see i18n text mapped from `response.code`.**

| Reason | Detail |
|--------|--------|
| Technical gibberish | `"request failed: POST /api/xxx"` is meaningless to users |
| No i18n | Backend messages are English-only |
| Security | Exposes internal API paths |

**Where the mapping lives**: `utils/errorCatalog.ts` — the single declaration of code → i18n key, split into **two** tables by ORIGIN because the Center API and the k2 engine independently chose HTTP-aligned codes whose meanings collide (400/401/403/503 mean different things on each side):

| Table | Channel | Rendered by |
|---|---|---|
| `API_ERROR_CATALOG` | `SResponse.code` from cloudApi | `getErrorMessage()` / `handleResponseError()` (`utils/errorCode.ts`) |
| `ENGINE_ERROR_CATALOG` | `ControlError.code` from `_k2.run()` | `getErrorI18nKey()` (`services/vpn-types.ts`) — returns a **fully-qualified** `ns:key` |

Never hand-write a code→key mapping outside that file. In catch blocks, log the raw error and show an i18n fallback string to the user.

`ENGINE_ERROR_CODES` in the same file mirrors `k2/engine/error.go`; `services/__tests__/k2-engine-codes.test.ts` parses the Go source and fails on drift. It is **fail-closed**: a missing `k2/engine/error.go` throws at load (never skips) — CI inits the submodule; locally `git submodule update --init k2`. `utils/__tests__/errorCatalog.test.ts` fails if any declared code lacks copy in all 7 locales. `i18n/__tests__/static-keys.test.ts` does the same for every statically-written `t('key')` in `src/` (dynamic template keys are a known blind spot).

### API Error Code Constitution

Every backend error code (`api/response.go`) MUST have a matching entry in `utils/errorCatalog.ts`.

Checklist for new backend error codes:
1. Add constant to `api/response.go`
2. Add to `ERROR_CODES` in `utils/errorCatalog.ts`
3. Add a row to `API_ERROR_CATALOG` (or list the code in `LOG_ONLY_CODES` if it is deliberately never shown) — `getErrorMessage()` derives from the table, there is no `switch` to edit
4. Add i18n translation in all 7 locales
5. Never duplicate error code constants outside `errorCatalog.ts`
6. **Regenerate the cross-layer contract** — the error-code registry is part of
   `contracts/api-contract.json`, so a new constant fails `TestExportContract`
   until you run `cd api && UPDATE_CONTRACT=1 go test -count=1 -run TestExportContract ./...`
   and commit the regenerated artifact. (`-count=1` is mandatory: the golden lives
   outside the api module, which go test's cache does not recheck.)

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

---

## Architecture: Split Globals

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

### Key Interfaces (types/kaitu-core.ts)

| Interface | Global | Purpose |
|-----------|--------|---------|
| `IK2Vpn` | `window._k2` | VPN control: `run<T>(action, params): Promise<SResponse<T>>` |
| `IPlatform` | `window._platform` | Platform capabilities: storage, clipboard, openExternal, syncLocale |
| `ISecureStorage` | `window._platform.storage` | Encrypted key-value storage |
| `IUpdater` | `window._platform.updater` | Auto-update: check, apply, status, channel |

### VPN Actions (via window._k2.run)

`up`, `down`, `status`, `version`, `probe` (probe-service), `classify-apps` (App Bypass), `adb-*` (helper endpoint — see Key Patterns), `sync-credential` (Capacitor only), `relay-fetch` / `relay-add-nodes` (antiblock relay — currently disabled, below)

### API Calls (via cloudApi / cacheStore)

Cloud API calls go through `cloudApi.request()` (auth headers, token refresh, 401 / 403003 side effects). Caching / SWR is **not** a wrapper — callers compose `cacheStore.get/set` around `cloudApi` themselves (`hooks/useUser.ts`, `pages/Purchase.tsx`).

**⛔ Relay 已由 kill-switch 关停（2026-07-17）**：`services/relay-flag.ts` `RELAY_ENABLED = false` → 传输为**直连 only**（`resolve-and-fetch.ts`，14s 直连预算；relay 开启时为 relay-first 5s 探测 + 直连兜底，控制面 SNI `k2.52j.me`），并一次性清除 relay 时代污染的 `k2_entry_url` 缓存。relay 代码全保留，恢复 = 改一行重发版；relay 行为细节见 `docs/superpowers/specs/2026-07-17-disable-relay-restore-direct-design.md`。

---

## Tech Stack

React 18, Material-UI 5, React Router 7, i18next, Vite 6, Zustand, TypeScript.

---

## Brand (双品牌: kaitu / overleap)

**Spec**: `docs/superpowers/specs/2026-07-14-brand-split-design.md` §4. Brand is baked at
BUILD TIME — env `K2_BRAND=kaitu|overleap` (default `kaitu`) → Vite/Vitest define
`__K2_BRAND__` → `src/brands/index.ts` selects `KAITU_BRAND` / `OVERLEAP_BRAND`. No runtime switch.

- **`src/brands/` is the single source of truth**: `brandConfig` carries productName /
  domainLabel / baseURL / supportEmail / locale-aware names & slogans /
  defaultLocale / MUI theme tokens / feature gates. Never fork on brand id in
  components — add a gate to `BrandFeatures` and read it via
  `getCurrentAppConfig().features.*` (config/apps.ts merges brand gates with
  platform-static features).
- **品牌差异只进 `src/brands/<id>/{index,theme,assets,locales}`** — 共享代码里禁止品牌
  id 分叉（唯一 resolver 是 `brandConfig`）。IAP 商品 id（`iapProductIds`）、FAQ 品牌
  key（`faqExtraKeys`）、antiblock CDN 镜像（`antiblockCdnSources`）均为品牌配置字段，
  消费方读 config，不留硬编码副本（Phase A defork）。
- **Stripe 购买流**（`features.stripeCheckout` 品牌，即 overleap）：Purchase 页在
  wordgate fallback 之前分支到 `components/stripe/StripePurchasePanel`（订阅模式：套餐卡
  + checkout 外链；管理模式按 `activeSub.manage.kind` 分派 stripe_portal / apple_settings
  / url）。hook `hooks/useStripeCheckout` 走 `POST /api/user/stripe/{checkout,portal}` +
  `openExternal`，入账由服务端 Stripe webhook 完成（客户端不落账）。
- **`X-K2-Brand` header**: injected ONLY in `services/cloud-api.ts` (`request()` +
  `_doRefresh()`), riding both relay and direct transports. 403003 (BRAND_MISMATCH)
  clears the session and opens LoginDialog, mirroring 403002.
- **i18n is brand-neutral**: locale files use `{{brand}}` / `{{brandDomain}}` /
  `{{brandBaseUrl}}` / `{{supportEmail}}` interpolation (defaultVariables installed in
  `i18n/i18n.ts`, refreshed on languageChanged via `brands/i18n-vars.ts`). Brand-exclusive
  copy lives in `src/brands/<brand>/locales/<lang>/<ns>.json` overlays (deep-merged at load;
  only the active brand's overlays are bundled). Guard test:
  `src/i18n/__tests__/brand-literals.test.ts`. Kaitu default locale zh-CN; overleap en-US.
- **Tests must be brand-adaptive**: `vitest` bakes the brand the same way builds do, so
  `K2_BRAND=overleap npx vitest run` must also exit 0. Never hardcode brand identity in
  assertions — assert against `brandConfig.*` / `getBrandName()`, or against the
  `KAITU_BRAND` / `OVERLEAP_BRAND` named imports for registry contracts. Gate
  brand-divergent suites with `describe.runIf(brandConfig.features.x)` and give the
  closed-gate branch a **real assertion** (see `Tunnels.test.tsx` "brand gate" and
  `Purchase.privateNode.test.tsx` "brand payment-channel gate"), not a bare skip.
  Brand literals inside a test are only OK as mock fixtures.
- **Brand literals belong in `src/brands/<brand>/index.ts`, never in a page/component**: pages are
  statically imported, so a literal there ships in EVERY brand's bundle and breaks purity
  (this is why `k2sInstallUrl` lives in the registry). `src/brands/overleap/index.ts` and
  `src/brands/i18n-vars.ts` ship in overleap artifacts — keep them free of `kaitu.io` /
  `开途` / `開途` **even in comments**; don't make the minifier's comment stripping
  load-bearing. (`types.ts` is type-only → erased, so it's exempt.)
- **Artifact purity**: `webapp/scripts/check-brand-purity.sh <brand> dist` (lives in
  `webapp/scripts/`, not the repo-root `scripts/`) — kaitu build must contain zero
  `overleap.io`; overleap build zero `kaitu.io|开途|開途`. Run after any build-affecting change. Bare `kaitu` protocol tokens (X-K2-Client `kaitu-service/`,
  `kaitu-language` storage key) are intentional and excluded.
- **Icons/title**: brand icons live in `src/brands/<brand>/assets/` (single source, both
  brands symmetric — `public/` holds no brand icon). The `k2-brand` vite plugin copies the
  active brand's icons into dist at writeBundle, serves them via dev-server middleware, and
  rewrites `<title>`. Runtime asset paths (`/favicon.png`, `/icon-192x192.png`) are
  brand-stable.
- **Shells** never import webapp brand code: `make build-*` exports `K2_BRAND=$(BRAND)`
  (`Makefile`, `BRAND ?= kaitu`) into `yarn build`; the shared contract is only that env var
  name and the `src/brands/<brand>/assets/` artwork directory.

---

## Directory Structure

```
webapp/
├── src/
│   ├── types/              # Type definitions (kaitu-core.ts = IK2Vpn + IPlatform + IUpdater)
│   ├── services/           # cloudApi, authService, cacheStore, bridges (standalone-k2 / tauri-k2 / capacitor-k2) + status-transform, router-service, antiblock / resolve-and-fetch / entry-pool / relay-flag, probe-service, device-udid, classify-apps, *-storage (secure / tauri / capacitor / plain), desktop-storage-migration
│   ├── core/               # getK2, isK2Ready, waitForK2
│   ├── stores/             # Zustand stores (vpn-machine, vpn, connection, config, auth, alert, layout, dashboard, login-dialog, self-hosted, app-routes, feedback, probe, router)
│   ├── pages/              # Route pages (Dashboard, RouterPage, AppBypass, Purchase, InviteHub, Account, SubmitTicket, FAQ, etc.)
│   ├── components/         # UI components (LoginDialog, AuthGate, FeedbackButton, etc.)
│   ├── hooks/              # Custom hooks (useUser, useAppConfig, useUpdater, etc.)
│   ├── i18n/locales/       # Locale files (zh-CN, en-US, ja, zh-TW, zh-HK, en-AU, en-GB)
│   ├── utils/              # Utilities (errorHandler, versionCompare, tunnel-sort, country, time)
│   ├── config/             # App configuration (apps.ts — feature flags, app config)
│   ├── contexts/           # React contexts (ThemeContext)
│   ├── theme/              # MUI theme tokens (colors.ts)
│   ├── assets/             # Static assets (payment logos)
│   └── test/               # Test setup (setup.ts, setup-dom.ts, utils/)
├── scripts/                # check-brand-purity.sh, generate-tz-country.mjs (+ tz-country-data.mjs), smoke-dist.mjs — NOT the repo-root scripts/
├── e2e/                    # Playwright E2E tests
├── vitest.config.ts / playwright.config.ts / package.json
└── vite.config.openwrt.ts  # DEAD (retired gateway mode; references the deleted gateway-k2). Linux embed uses plain `yarn build` output via Makefile `stage-k2-webui-dist`
```

---

## Bootstrap (main.tsx)

1. Module level: `installChunkReloadGuard()`, `Sentry.init` → then `await i18nPromise`
2. Bridge injection is done **by main.tsx itself**: `window.__TAURI__` → `injectTauriGlobals()` (`tauri-k2.ts`); `Capacitor.isNativePlatform()` → `injectCapacitorGlobals()` (`capacitor-k2.ts`); otherwise, if `window._k2`/`_platform` are missing → `standalone-k2.ts ensureK2Injected()` (the Linux `k2/webui` embed takes this branch too; the daemon only injects `window.__K2_GATEWAY__`)
3. `await ensureSeeded()` (`entry-pool.ts`) — primes the node pool so the first cloud request fired from store init can't race an empty pool
4. `initializeAllStores()` (no args) → `ReactDOM.createRoot().render(<App />)`
5. **After** render: boot handshake `confirmUiBootOk()` (Tauri) / `confirmWebBootOk()` (Capacitor, Linux) — order pinned by `src/__tests__/boot-handshake-wiring.test.ts`

---

## i18n

| Code | Language | Role |
|------|----------|------|
| `zh-CN` | Simplified Chinese | Primary (add new text here first) |
| `en-US` | English | Manual translation |
| `ja` | Japanese | Manual translation |
| `zh-TW` | Traditional Chinese | Manual translation |
| `zh-HK` | Traditional Chinese (HK) | Manual translation |
| `en-AU` | English (AU) | Manual translation |
| `en-GB` | English (GB) | Manual translation |

Namespaces (`i18n/locales/namespaces.ts`, generated — don't hand-edit): account, auth, common, dashboard, feedback, invite, nav, privateNode, purchase, retailer, router, startup, theme, ticket, wallet. Its `namespaceMapping` routes bare key prefixes (e.g. `appBypass` → `dashboard`).

---

## Router Tab (k2r headless + app direct-control)

App is the sole UI for a headless k2r router. Discovery and control both use the single anchor `http://10.17.79.1:1779` (`ROUTER_ANCHOR`, `services/router-service.ts`) that k2r DNAT-intercepts on its forwarding path — no gateway probing, no `lanIP` concept. Design: `docs/superpowers/specs/2026-07-17-k2r-headless-app-control-design.md`.

- **`router-service.ts`**: `probeRouter()` GETs `{ROUTER_ANCHOR}/ping` (1.5s) and requires the `k2r: true` signature. `getControlKey(forceRefresh?)` caches locally, refetching via `POST /api/user/router-control-key`. `routerFetch()` (shared by `routerCore()` / `routerDevicesGet/Post()`) sends `Bearer <controlKey>`; on 401 it force-refreshes once and retries, and if still 401 **backs off 60s** (`force401BackoffUntil`) so the 2s poll doesn't hammer Center during k2r's key-rotation window. Everything goes through `_platform.routerRequest`, never raw `fetch`.
- **`router.store.ts`**: phases `none` / `unconfigured` / `online` / `offline` (tab stays visible when offline). `startPolling()` hits `routerCore('status')` every 2s **only while the Router tab is mounted** (no SSE — CapacitorHttp can't stream); a late poll is dropped if `unbindRouter()` cleared `router` meanwhile. `isRouterTakeover(s)` = `phase === 'online' && status?.state === 'connected'` — the shared predicate for `RouterTakeoverBanner` (Dashboard, when local VPN isn't `connected`) and `RouterExclusionDialog`.
- **`RouterExclusionDialog.tsx`** (`useExclusionGuard`): dual-connection warning fires **only when a second connection is about to start** (Router-tab connect while local VPN is connected, or Dashboard connect while takeover) — never on tab switch. Also owns the unbind confirm (MUI, not `window.confirm`).
- **`RouterPage.tsx`**: `/router` tab appended to `Layout.tsx` once `phase !== 'none'`; enterprise multi-slot routers (`routerSlots(s) !== null`) auto-redirect to `/router` (`Layout.tsx`). Device list uses `routerDevicesGet/Post`.
- **Bridge boundary**: LAN traffic goes through `_platform.routerRequest` (Tauri: Rust `router_http_request`, SSRF-gated; Capacitor: `CapacitorHttp` + TS-side SSRF gate — see `desktop/CLAUDE.md` / `mobile/CLAUDE.md`). `_platform.getDefaultGateway()` exists on both bridges but is **unconsumed**.
- **Retired** embedded gateway mode (`gateway-k2.ts`, `gateway-core.ts`, `gateway-storage.ts`, `GatewaySetup.tsx` — deleted in `521d346d`): `platformType === 'gateway'` survives only for frozen field snapshots; `SmartServerSelector`'s `k2sub` tab and Dashboard's `k2subTabContent` / `serverMode==='k2sub'` are still gated on it and therefore dead — pending cleanup.

## Bridge & VPN State Contract

**`transformStatus()` (`services/status-transform.ts`) is mandatory** — every bridge (`tauri-k2.ts`, `capacitor-k2.ts`, `standalone-k2.ts`) routes status responses *and* push events through it. Raw backend state MUST NOT pass through.

- Daemon `"stopped"` → `"disconnected"`.
- With `error` present (structured `{code, message}`, or legacy string → code 570) and state `disconnected` **or** `connected`, state becomes `'error'`; `retrying = connected && code ∉ {400,401,402,403}` (connected+error = TUN up, wire broken, engine retries; disconnected+error = engine gave up).
- VPN machine maps `error` to `idle` (terminal) or `reconnecting` (retrying) from that `retrying` flag (dispatch payload `isRetrying`) — error is a field overlay, never a machine state.
- `reconnecting` is transient (engine state stays `connected`). Do not treat it as a terminal disconnect.

## Key Patterns

- **Store init**: `initializeAllStores()` runs layout → config.loadConfig (then `detectGeo`) → selfHosted.loadTunnel → appRoutes.load (legacy-key migration) → auth → vpn-machine → connection, in order; returns a cleanup function.
- **VPN state machine**: `vpn-machine.store.ts` — explicit 6-state machine (`idle`, `connecting`, `connected`, `reconnecting`, `disconnecting`, `serviceDown`) with transition table. Error is a field overlay (`error: ControlError | null`), not a state — `BACKEND_ERROR` routes to `idle` (terminal) or `reconnecting` (retrying). Module-level `dispatch(event, payload)` is the only way to change state. 3s debounce for `connected → reconnecting` is the only state timer. `initializeVPNMachine()` owns status delivery: event-driven (`onStatusChange` SSE / native) plus a safety-net poll (iOS never fires for engine error-overlay changes), else a 2s poll. `core/polling.ts useStatusPolling()` has no consumers — don't build on it.
  - **Staleness token (`revision`)**: every applied transition bumps a monotonic `revision`. Any code that issues an async `run('status')` must snapshot `currentRevision()` **before** the call and pass it to `applyStatus(status, rev)`; a mismatch drops the whole snapshot (state *and* `startAt`). Without it a poll issued while connected can land after an SSE `disconnected` and re-enter `connected` via `idle + BACKEND_CONNECTED` — UI claims protected while traffic runs in the clear. Push deliveries (SSE/native) pass no revision: they have no in-flight window. `connectEpoch` cannot serve this role — it lives in connection.store (which imports this file, so reading it back would be a cycle) and only bumps on user intent, missing backend-initiated drops.
- **Connection store**: `connection.store.ts` — owns tunnel selection, connect/disconnect orchestration, `connectedTunnel` snapshot (frozen at connect time for stable UI), and `connectEpoch` guard (prevents stale async ops — checked both *before* `run('up')` and *after* it returns, so a superseded attempt's failure can't tear down the live one)
- **Keep-alive tabs**: Layout caches visited tab outlets, hides inactive with `visibility:hidden`. Tab paths: `/`, `/invite`, `/discover`, `/account`, plus `/router` once `router.store.phase !== 'none'`
- **Keep-alive + GPU layers gotcha**: WebKit doesn't recomposite layers when `opacity`/`filter` are removed while an element is `visibility:hidden`. Dashboard uses a `translateZ(0)` toggle on hidden→visible transitions to force layer rebuild. Any new compositing-layer CSS changes on keep-alive tabs need similar consideration.
- **Config store (v3)**: `useConfigStore()` in `stores/config.store.ts` persists `defaultVia`/`countryVia`/`country`/`autoDetect` (key `k2.vpn.config`; legacy v0/v1/v2 shapes auto-migrate on first load). `buildConnectConfig({serverUrl})` assembles the wire-contract `ClientConfig` — emits `routes[]`, forces `log.level = __K2_BUILD_LOG_LEVEL__` (build-time constant). **Country detection is fully client-side** (`detectGeo` → `utils/geo-detect.ts`, system timezone via the committed `tz-country.gen.ts` artifact — regenerate with `cd webapp && node scripts/generate-tz-country.mjs`, drift-locked by `tz-country.gen.test.ts`); the server's `/api/geo` is frozen (hotfix, always `cn`) and no longer consulted. Any country written into `country` MUST pass `routableCountry()` (utils/routes.ts) — an unclamped bundle-less country 504s pre-2026-06-14 engines and silently routes as cn on newer ones; `buildRoutes` falls back to the global shape as the in-depth defense, and `connection.store.connect()` retries once with `forceGlobalRoutes` on 504.
- **Last server URL persistence**: The k2v5 URL sent on last connect is persisted separately by `connection.store` under key `k2.vpn.last_server_url` (not inside `ClientConfig`, which mirrors the Go wire contract). Used only for cold-start restore when the webapp process was killed while VPN stayed up. Cleared on explicit `disconnect()`.
- **k2subs is desktop-only**: Webapp NEVER resolves `k2subs://` — raw `k2subs://` URLs go straight to the desktop daemon (which runs a persistent `Subscription` with refresh loop + Phase-B hot-swap in `k2/config/subscription.go`). Mobile has no smart mode at all; user picks one `k2v5://` tunnel manually on Dashboard and the webapp passes that single URL to `_k2.run('up')`. If raw `k2subs://` ever reaches mobile engine → code 570 "no k2v5 outbound configured" — always a webapp bug.
- **Probe service**: `services/probe-service.ts` `runProbe(tunnels)` calls `_k2.run('probe', {urls, timeoutMs})`, records results into `probe.store`. Self-gates on `platformType === 'web'` and on any VPN state other than `idle` / `serviceDown` (probing while VPN is up would loop UDP through TUN). `CloudTunnelList` triggers on mount, on tunnels change, and every 5 min while mounted. `ProbeChip` renders per-tunnel RTT / loss / jitter from `probe.store` alongside `RecommendBar`.
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

---

## Commands

```bash
yarn install                             # from workspace root
cd webapp && yarn dev                    # Dev server
cd webapp && yarn build                  # = check-i18n.mjs --ci && tsc && vite build — i18n drift fails the build
cd webapp && yarn test                   # pretest runs scripts/check-k2-plugin-fresh.sh first: k2-plugin dist vs src, and the
                                         #   yarn-v1 `file:` copy in node_modules (a stale copy is only refreshed by `make build-k2-plugin`)
cd webapp && npx vitest run [--reporter=verbose]   # same tests, but SKIPS that pretest gate
cd webapp && K2_BRAND=overleap npx vitest run      # must also be green
cd webapp && npx playwright test         # E2E
cd webapp && npx tsc --noEmit            # Type check
cd webapp && UPDATE_BRIDGE_CONTRACT=1 npx vitest run src/types/__tests__/bridge-contract.test.ts   # regen bridge golden — only after a deliberate bump
webapp/scripts/check-brand-purity.sh <brand> dist   # artifact purity after any build-affecting change
# yarn build:openwrt / dev:openwrt and scripts/dev-openwrt.sh are dead (retired gateway mode)
```

---

## Troubleshooting

| Problem | Check |
|---------|-------|
| `window._k2` is undefined | Platform injection not running. Desktop: check Tauri inject. Web: check standalone-k2 fallback. |
| `window._platform` is undefined | Platform injection not running. Check bootstrap in main.tsx → ensureK2Injected(). |
| VPN operations fail | Is k2 daemon running? (`sc query kaitu` on Windows, `launchctl list kaitu` on macOS). Check service logs and network permissions. |
| API calls fail | Token expired? Check network. Check browser Network panel. |
| Service reachable? | `curl http://127.0.0.1:1777/ping` |
| White flash on app start | `index.html` must use `background: #0f0f13` directly on `html, body` WITHOUT `@media (prefers-color-scheme: dark)`. Media query causes 100-300ms white flash on light-mode OS before MUI loads. |
| Vitest mock state leaks between tests | `src/test/setup.ts` runs a global `beforeEach` (`vi.clearAllMocks()` + localStorage mock reset) and `afterEach(vi.restoreAllMocks())`. `clearAllMocks` wipes implementations, not just call counts — re-call `mockFn.mockResolvedValue()` in each `describe`'s `beforeEach`, not once at module level. |
| Login fails with 422 | All login paths must include `udid` from `getDeviceUdid()` (in `services/device-udid.ts`) in POST body. Backend requires UDID for device association. |
| RegExp `.replace()` skips some matches | Module-level global regexes (`/g` flag) retain `lastIndex` between calls. Reset with `re.lastIndex = 0` before each `.replace()`, or inline the literal. |

## Domain Vocabulary

- **IK2Vpn** — VPN control interface (`window._k2`), single `run(action, params)` method
- **IPlatform** — Platform capabilities interface (`window._platform`); members listed in `types/kaitu-core.ts`
- **cloudApi** — Cloud API HTTP module with auth injection and token refresh
- **ClientConfig** — see root `CLAUDE.md`; assembled here by `buildConnectConfig()`.
- **Route preset** — `config.store` persists `defaultVia` (`proxy|direct`) + `countryVia` (`direct|k2p|null`); derived presets `global` / `bypass` / `home` / `home_proxy`. `buildRoutes()` emits `match:{region: country}` on the direct branch for `bypass`; `match:{preset}` (e.g. `cn-access`) appears only on `k2p://home` routes. Legacy `ruleMode` survives solely as a v0/v1 migration field.
- **Antiblock** — Multi-CDN entry URL resolution for Cloud API in blocked regions.
- **AuthGate** — Loading gate while auth state is being checked; no service/version check (see Key Patterns).
- **LoginDialog** — Global modal for all auth flows (no `/login` route).
- **transformStatus()** — Bridge normalization (see "Bridge & VPN State Contract" above).
- **App routing (Plan B + C unification)** — Smart region routing is expressed
  via `routes[].match.region` (the old separate `app_bypass` field is gone),
  sourced from `config.store.country`; `buildRoutes()` emits `match.region` on the
  direct branch. Per-app force overrides (`forceProxy` / `forceDirect`) live in
  `app-routes.store` (`_platform.storage` key `k2.routes.overrides`).
  `buildConnectConfig()` prepends them as Tier-1 `{match:{apps:[...]}}` routes
  before the region route (forceDirect → `direct`, forceProxy → server URL).
  `AppBypass.tsx` (`appBypass.v2.*` i18n) has **two data sources**: `listInstalled`
  is primary, `listRunning` only becomes primary when it is the sole provider (iOS has
  neither). When both exist (desktop bridge — `installed_apps.rs` / `app_list.rs`, see
  `desktop/CLAUDE.md`), `foldRunningIntoInstalled()` folds running Windows exes located
  under an installed app's directory into that app's `processNames` and drops them from
  the "more — running" section, so one toggle covers every exe the app runs. The legacy
  `k2.advanced.app_bypass` key is migration-cleanup-only (discarded on first `load()`).
  See `docs/superpowers/specs/2026-05-27-app-bypass-routes-unification.md`.

## Style

- **Dark theme forced at runtime**: `ThemeContext` always applies `darkTheme`. `lightTheme` and the `theme` i18n namespace are deliberately retained (`void lightTheme` defeats dead-code elimination) so a switcher can return — don't delete them as dead code, and don't add `@media (prefers-color-scheme)` branches.
- **Webapp subagent tasks**: For webapp UI decisions, prefer frontend-specialized agents (see root agent registry).

## Related Docs

- [Client Architecture](../CLAUDE.md)
- [Desktop Adapter](../desktop/CLAUDE.md)
- [Center API](../api/CLAUDE.md)
