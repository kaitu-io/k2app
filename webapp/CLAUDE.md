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
- `src/services/tauri-k2.ts` — Tauri bridge
- `src/services/capacitor-k2.ts` — Capacitor bridge
- `src/services/standalone-k2.ts` — Web fallback
- `src/main.tsx` — 仅用于平台检测和 bridge 选择

---

## Hard Rules

```
DO NOT:
  - Direct fetch/axios calls from pages/components (use cloudApi or k2api)
  - Hardcoded UI text in components (use i18n)
  - Access window._k2 for API calls (use cloudApi module)
  - Access window._k2 for platform capabilities (use window._platform)
  - Use npm (use yarn)
  - Display response.message to users (use code + i18n)
  - Bypass bridge layer (see Constitutional Rule above)

DO:
  - VPN control via window._k2.run(action, params)
  - Cloud API calls via cloudApi.request() (src/services/cloud-api.ts)
  - Platform capabilities via window._platform (storage, clipboard, etc.)
  - Device UDID via getDeviceUdid() from services/device-udid.ts (NOT _platform)
  - State management with Zustand stores
  - Components use Material-UI
  - Errors displayed via response.code mapped to i18n keys
  - New text goes to zh-CN first, then manually translate to other locales
```

### Modification Checklist

- [ ] New text added to all locale files?
- [ ] VPN control goes through `window._k2.run()`?
- [ ] API requests go through `cloudApi` or `k2api` (NOT `window._k2`)?
- [ ] Platform features accessed via `window._platform`?
- [ ] Cross-platform impact considered (Desktop/Mobile)?
- [ ] Errors based on code, not message?

---

## Error Handling

**Rule: `response.message` is for debug logs only. Users see i18n text mapped from `response.code`.**

| Reason | Detail |
|--------|--------|
| Technical gibberish | `"request failed: POST /api/xxx"` is meaningless to users |
| No i18n | Backend messages are English-only |
| Security | Exposes internal API paths |

Error code-to-i18n mapping lives in `utils/errorCode.ts`. Use `handleResponseError()` and `getErrorMessage()` from that module. In catch blocks, log the raw error and show an i18n fallback string to the user.

### API Error Code Constitution

Every backend error code (`api/response.go`) MUST have a matching entry in `utils/errorCode.ts`.

Checklist for new backend error codes:
1. Add constant to `api/response.go`
2. Add to `ERROR_CODES` in `utils/errorCode.ts`
3. Add `case` in `getErrorMessage()` with i18n key
4. Add i18n translation in all 7 locales
5. Never duplicate error code constants outside `errorCode.ts`

Code ranges:
- `0`: Success
- `400-503`: Backend HTTP-aligned codes (sync with `api/response.go`)
- `400001-400999`: Backend custom business codes
- `100-199`: Frontend-only network errors
- `500-579`: Frontend-only VPN/action/API errors
- `-1`: Frontend-only cloud API network failure

---

## Architecture: Split Globals

Frontend uses two separate globals injected before app loads. They have distinct responsibilities:

- `window._k2: IK2Vpn` -- VPN tunnel control only (single `run()` method)
- `window._platform: IPlatform` -- Platform capabilities (storage, clipboard, etc.)
- `cloudApi` (internal module) -- Cloud API HTTP calls with auth injection

```
┌──────────────────────────────────────────────────────────┐
│ Platform Injection (before app loads)                     │
│   Tauri:     Rust inject -> HTTP 127.0.0.1:1777          │
│   Capacitor: Plugin inject -> Native SDK                 │
│   Web:       JS inject -> web fallbacks (standalone-k2)  │
└──────────┬────────────────────────┬──────────────────────┘
           ↓                        ↓
┌─────────────────────┐  ┌─────────────────────────────────┐
│ window._k2: IK2Vpn  │  │ window._platform: IPlatform     │
│   run(action, params)│  │   os, version                   │
│                      │  │   storage: ISecureStorage        │
│ VPN actions:         │  │   syncLocale()                  │
│   up, down,          │  │   writeClipboard(), readClipboard│
│   status, version    │  │   openExternal()                │
│                      │  │   updater?: IUpdater            │
└──────────┬───────────┘  │   reinstallService?(), getPid?()│
│   uploadLogs?(), setLogLevel?()│
           │              └────────────────────────────────┘
           ↓
┌──────────────────────────────────────────────────────────┐
│ cloudApi (services/cloud-api.ts)                         │
│   request(method, path, body?) -> SResponse              │
│   Auth header injection (Bearer token)                   │
│   401 handling with token refresh                        │
│   Uses authService for token management                  │
└──────────────────────────────────────────────────────────┘
           ↓
┌──────────────────────────────────────────────────────────┐
│ Frontend                                                  │
│   services/   - k2api (cache/SWR wrapper over cloudApi)  │
│   core/       - useStatusPolling() (polling via _k2.run) │
│   stores/     - Zustand state management                 │
│   pages/      - Route pages                              │
│   components/ - UI components                            │
└──────────────────────────────────────────────────────────┘
```

### Key Interfaces (types/kaitu-core.ts)

| Interface | Global | Purpose |
|-----------|--------|---------|
| `IK2Vpn` | `window._k2` | VPN control: `run<T>(action, params): Promise<SResponse<T>>` |
| `IPlatform` | `window._platform` | Platform capabilities: storage, clipboard, openExternal, syncLocale |
| `ISecureStorage` | `window._platform.storage` | Encrypted key-value storage |
| `IUpdater` | `window._platform.updater` | Auto-update: check, apply, status, channel |

### VPN Actions (via window._k2.run)

`up`, `down`, `status`, `version`, `classify-apps` (App Bypass), `relay-fetch` (antiblock control-plane relay through a camouflage node)

### API Calls (via cloudApi / k2api)

Cloud API calls go through `cloudApi.request()` which handles auth headers and token refresh. The `k2api()` wrapper adds caching and SWR support. Auth success/failure/401/402 side effects are handled by k2api.

**Antiblock relay transport (Phase 3):** `cloudApi.request()`/`_doRefresh()` resolve transport via `resolve-and-fetch.ts` — **relay-first, direct-fallback**: it relays the request through a camouflage VPN node via `_k2.run('relay-fetch')` (control-plane inner SNI fixed to `k2.52j.me`, must match node-side `control_plane_routes`) and only on relay failure falls back to a direct `fetch()` (5s probe) to the control-plane host. Relay is primary because that path is identical for blocked and unblocked clients (so external testing represents in-region behaviour); direct is the safety net and the only path on web/mobile, where relay is unsupported — detected when `relay-fetch` returns `code:-1` (capacitor / daemon-less standalone / daemon down), which flips a session-scoped `isRelaySupported()` flag so subsequent requests skip the doomed relay attempt. Node faults return `code:502` and trigger node-failover (not capability-downgrade). `entry-pool.ts` is a persistent, scored, node-only pool seeded from every successful `/api/tunnels` response (`node-descriptor.ts` extracts `{ip,pin,ech}` from each tunnel's `serverUrl`) plus the antiblock cold-start seed (`antiblock-seed.ts` embedded floor + galloping CDN refresh). 401 refresh atomicity stays in `cloud-api.ts` — the transport never handles 401 (relay passes non-2xx through verbatim). Mobile relay is unsupported until Phase 2b.

---

## Tech Stack

React 18, Material-UI 5, React Router 7, i18next, Vite 6, Zustand, TypeScript.

---

## Brand (双品牌: kaitu / overleap)

**Spec**: `docs/superpowers/specs/2026-07-14-brand-split-design.md` §4. Brand is baked at
BUILD TIME — env `K2_BRAND=kaitu|overleap` (default `kaitu`) → Vite/Vitest define
`__K2_BRAND__` → `src/brand/index.ts` selects `KAITU_BRAND` / `OVERLEAP_BRAND`. No runtime switch.

- **`src/brand/` is the single source of truth**: `brandConfig` carries productName /
  domainLabel / baseURL / websiteOrigins / supportEmail / locale-aware names & slogans /
  defaultLocale / MUI theme tokens / feature gates. Never fork on brand id in
  components — add a gate to `BrandFeatures` and read it via
  `getCurrentAppConfig().features.*` (config/apps.ts merges brand gates with
  platform-static features).
- **`X-K2-Brand` header**: injected ONLY in `services/cloud-api.ts` (`request()` +
  `_doRefresh()`), riding both relay and direct transports. 403003 (BRAND_MISMATCH)
  clears the session and opens LoginDialog, mirroring 403002.
- **i18n is brand-neutral**: locale files use `{{brand}}` / `{{brandDomain}}` /
  `{{brandBaseUrl}}` / `{{supportEmail}}` interpolation (defaultVariables installed in
  `i18n/i18n.ts`, refreshed on languageChanged via `brand/i18n-vars.ts`). Brand-exclusive
  copy lives in `src/i18n/brand/<brand>/<lang>/<ns>.json` overlays (deep-merged at load;
  only the active brand's overlays are bundled). Guard test:
  `src/i18n/__tests__/brand-literals.test.ts`. Kaitu default locale zh-CN; overleap en-US.
- **Artifact purity**: `scripts/check-brand-purity.sh <brand> dist` — kaitu build must
  contain zero `overleap.io`; overleap build zero `kaitu.io|开途|開途`. Run after any
  build-affecting change. Bare `kaitu` protocol tokens (X-K2-Client `kaitu-service/`,
  `kaitu-language` storage key) are intentional and excluded.
- **Icons/title**: `public/` holds kaitu defaults; `brand-assets/<brand>/` overrides are
  copied over dist icons by the `k2-brand` vite plugin, which also rewrites `<title>`.
  Runtime asset paths (`/favicon.png`, `/icon-192x192.png`) are brand-stable.
- **Phase 4/5 seam (shells)**: shells do NOT import webapp brand code. Desktop/mobile
  builds pass `K2_BRAND` through the existing `make build-* → cd webapp && yarn build`
  path (Makefile gets `BRAND ?= kaitu` + `export K2_BRAND=$(BRAND)` in Phase 4). Shell-
  native brand config (tauri.conf.overleap.json, Android flavors, iOS schemes, IAP
  product ids) is separate per-shell work; the shared contract is only the env var name
  `K2_BRAND` and the `brand-assets/<brand>/` artwork directory.

---

## Directory Structure

```
webapp/
├── src/
│   ├── types/              # Type definitions (kaitu-core.ts = IK2Vpn + IPlatform + IUpdater)
│   ├── services/           # cloudApi, authService, cacheStore, standalone-k2 / tauri-k2 / capacitor-k2 / gateway-k2 (bridges), antiblock, stats, api-types, device-udid, network-env, status-transform, classify-apps, capacitor-app-map, disconnect-feedback, beta-auto-upload, *-storage (secure / tauri / capacitor / gateway / plain), probe-service
│   ├── core/               # Core module (getK2, isK2Ready, waitForK2)
│   ├── stores/             # Zustand stores (vpn-machine, vpn, connection, config, auth, alert, layout, dashboard, login-dialog, self-hosted, app-routes, feedback, probe)
│   ├── pages/              # Route pages (Dashboard, SubmitTicket, FAQ, Purchase, Invite, Account, etc.)
│   ├── components/         # UI components (LoginDialog, AuthGate, FeedbackButton, etc.)
│   ├── hooks/              # Custom hooks (useUser, useAppConfig, useUpdater, etc.)
│   ├── i18n/locales/       # Locale files (zh-CN, en-US, ja, zh-TW, zh-HK, en-AU, en-GB)
│   ├── utils/              # Utilities (errorHandler, versionCompare, tunnel-sort, country, time)
│   ├── config/             # App configuration (apps.ts — feature flags, app config)
│   ├── contexts/           # React contexts (ThemeContext)
│   ├── theme/              # MUI theme tokens (colors.ts)
│   ├── assets/             # Static assets (payment logos)
│   └── test/               # Test setup (setup.ts, setup-dom.ts, utils/)
├── e2e/                    # Playwright E2E tests
├── vitest.config.ts
├── playwright.config.ts
└── package.json
```

---

## Bootstrap (main.tsx)

1. Initialize Sentry
2. Await i18next initialization
3. Check `window._k2` / `window._platform` — if missing, import `standalone-k2.ts` and call `ensureK2Injected()`
4. `initializeAllStores()` (no args — globals already injected)
5. `ReactDOM.createRoot().render(<App />)`

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

Namespaces: account, auth, common, dashboard, feedback, invite, nav, onboarding, purchase, retailer, startup, theme, ticket, wallet

---

## Bridge & VPN State Contract

**Bridge `transformStatus()` is mandatory.** Every bridge (`tauri-k2.ts`, `capacitor-k2.ts`, `standalone-k2.ts`) must implement `transformStatus()` and normalize backend state before returning to the webapp. Raw backend state MUST NOT pass through.

- Daemon emits `"stopped"`; webapp expects `"disconnected"` → bridge rewrites.
- Bridge synthesizes `state: 'error'` from `disconnected + lastError`. Handles both structured `{code, message}` and legacy string errors.
- VPN machine then maps `error` to `idle` (non-retrying, terminal) or `reconnecting` (retrying) based on the `isRetrying` payload — error is a display field overlay, never a machine state.
- `reconnecting` is a transient engine signal (engine state stays `connected`). Do not treat it as a terminal disconnect.

## Key Patterns

- **Store init**: `initializeAllStores()` calls layout → config.loadConfig → selfHosted.loadTunnel → auth → vpn-machine init in order. Returns cleanup function.
- **VPN state machine**: `vpn-machine.store.ts` — explicit 6-state machine (`idle`, `connecting`, `connected`, `reconnecting`, `disconnecting`, `serviceDown`) with transition table. Error is a field overlay (`error: ControlError | null`), not a state — `BACKEND_ERROR` routes to `idle` (terminal) or `reconnecting` (retrying). Module-level `dispatch(event, payload)` is the only way to change state. 3s debounce for `connected → reconnecting` is the only timer. Supports both SSE event-driven mode (desktop) and 2s polling fallback (standalone).
- **Connection store**: `connection.store.ts` — owns tunnel selection, connect/disconnect orchestration, `connectedTunnel` snapshot (frozen at connect time for stable UI), and `connectEpoch` guard (prevents stale async ops)
- **Keep-alive tabs**: Layout caches visited tab outlets, hides inactive with `visibility:hidden`. Tab paths: `/`, `/invite`, `/discover`, `/account`
- **Keep-alive + GPU layers gotcha**: WebKit doesn't recomposite layers when `opacity`/`filter` are removed while an element is `visibility:hidden`. Dashboard uses a `translateZ(0)` toggle on hidden→visible transitions to force layer rebuild. Any new compositing-layer CSS changes on keep-alive tabs need similar consideration.
- **Config store**: `useConfigStore()` in `stores/config.store.ts` persists the UI rule-mode toggle (`ruleMode: 'global' | 'chnroute'`, key `k2.vpn.config`). `buildConnectConfig({serverUrl})` assembles the wire-contract `ClientConfig` — emits `routes[]` from `ruleMode` + `serverUrl`, forces `log.level = __K2_BUILD_LOG_LEVEL__` (build-time constant). `updateRuleMode(mode)` is the only mutator. Legacy persisted `{server, rule.global}` shape is auto-migrated on first load.
- **Last server URL persistence**: The k2v5 URL sent on last connect is persisted separately by `connection.store` under key `k2.vpn.last_server_url` (not inside `ClientConfig`, which mirrors the Go wire contract). Used only for cold-start restore when the webapp process was killed while VPN stayed up. Cleared on explicit `disconnect()`.
- **k2subs is desktop-only**: Webapp NEVER resolves `k2subs://` — raw `k2subs://` URLs go straight to the desktop daemon (which runs a persistent `Subscription` with refresh loop + Phase-B hot-swap in `k2/config/subscription.go`). Mobile has no smart mode at all; user picks one `k2v5://` tunnel manually on Dashboard and the webapp passes that single URL to `_k2.run('up')`. If raw `k2subs://` ever reaches mobile engine → code 570 "no k2v5 outbound configured" — always a webapp bug.
- **Probe service**: `services/probe-service.ts` `runProbe(tunnels)` calls `_k2.run('probe', {urls, timeoutMs})`, records results into `probe.store`. Self-gates on web platform and non-idle VPN state (running probe while VPN is up would loop UDP through TUN). `CloudTunnelList` triggers on mount, on tunnels change, and every 5 min while mounted (matching daemon-side cadence). `ProbeChip` renders per-tunnel RTT / loss / jitter from `probe.store` alongside `RecommendDot`.
- **LoginDialog**: Global modal via `login-dialog.store`. Guards call `openLoginDialog()` instead of redirecting
- **Feature flags**: `getCurrentAppConfig().features` controls route/tab visibility
- **Dev proxy**: Vite proxies `/api/core`, `/api/helper`, `/api/device`, `/ping` to `:1777` (or `K2_DAEMON_PORT`). Production uses absolute URL
- **Helper API routing**: `adb-*` actions route to `/api/helper` (not `/api/core`). Tauri bridge uses `daemon_helper_exec` IPC command. Standalone bridge checks `action.startsWith('adb-')` to pick endpoint. New daemon helper actions must follow this pattern.
- **Config-driven connect**: `_k2.run('up', config)` where config is assembled from the selected tunnel URL + user preferences via `buildConnectConfig`
- **AuthGate**: Wraps all routes — checks service readiness + version match before rendering
- **Viewport scaling**: Uses CSS `zoom` (not `transform:scale()`) to avoid breaking `position:fixed` elements (react-joyride spotlight, MUI Portals)
- **Onboarding**: Button-driven flow (Next/Done/Skip) with step indicator. Includes invite share phase (navigates to /invite)
- **Dev mode**: `_platform.setDevEnabled?.(true)` enables WebView inspection (iOS `isInspectable`, Android `WebContentsDebugging`). State persisted to localStorage, auto-restored on launch

---

## Commands

```bash
yarn install                             # Install dependencies (from workspace root)
cd webapp && yarn dev                    # Dev server
cd webapp && yarn build                  # Production build
cd webapp && npx vitest run              # Run all tests
cd webapp && npx vitest run --reporter=verbose  # Verbose test output
cd webapp && npx playwright test         # E2E tests (Playwright)
cd webapp && npx tsc --noEmit            # Type check
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
| Vitest mock state leaks between tests | `vi.clearAllMocks()` clears implementations, not just call counts. Re-call `mockFn.mockResolvedValue()` in each `describe`'s `beforeEach` — not just once at module level. |
| Vitest 3 gotchas (when adding new tests) | `mockReset()` now **restores** the original implementation (was: returns `undefined`). `vi.spyOn()` on the same method twice **reuses** the existing spy instead of creating a new one. `expect(err).toEqual(new Error('x'))` now checks `name` + `message` + `cause` + prototype — `TypeError` no longer matches a generic `Error`. `vi.useFakeTimers()` mocks **everything** including `performance.now()` by default — pass `{ toFake: [...] }` to restore old scope. |
| Login fails with 422 | All login paths must include `udid` from `getDeviceUdid()` (in `services/device-udid.ts`) in POST body. Backend requires UDID for device association. |
| RegExp `.replace()` skips some matches | Module-level global regexes (`/g` flag) retain `lastIndex` between calls. Reset with `re.lastIndex = 0` before each `.replace()`, or inline the literal. |

## Domain Vocabulary

- **IK2Vpn** — VPN control interface (`window._k2`), single `run(action, params)` method
- **IPlatform** — Platform capabilities interface (`window._platform`): storage, UDID, clipboard, openExternal, updater, uploadLogs
- **cloudApi** — Cloud API HTTP module with auth injection and token refresh
- **ClientConfig** — Universal config contract: Go `config.ClientConfig` = TS `ClientConfig`. Assembled from Cloud API + user preferences, passed to `_k2.run('up', config)`. Outbounds are expressed as `routes: [{via, match}]` — there is no top-level `server` field. Global = `[{via: url, match: {all: true}}]`; chnroute = `[{via: 'direct', match: {preset: 'cn-access'}}, {via: url, match: {}}]`. See `k2/engine/engine.go buildRouteEntries`.
- **Rule mode** — Webapp-only UI toggle (`ruleMode: 'global' | 'chnroute'`) persisted in `config.store`. Translated to different `routes[]` shapes at connect time. Not a Go-side field.
- **Antiblock** — Multi-CDN entry URL resolution for Cloud API in blocked regions.
- **AuthGate** — Startup gate: checks service readiness + version match before showing main UI.
- **LoginDialog** — Global modal for all auth flows (no `/login` route).
- **transformStatus()** — Bridge normalization (see "Bridge & VPN State Contract" above).
- **App routing (Plan B + C unification)** — Smart region routing is expressed
  via `routes[].match.region` (the old separate `app_bypass` field is gone),
  sourced from `config.store.country`; `buildRoutes()` emits `match.region` on the
  direct branch. Per-app force overrides (`forceProxy` / `forceDirect`) live in
  `app-routes.store` (`_platform.storage` key `k2.routes.overrides`).
  `buildConnectConfig()` prepends them as Tier-1 `{match:{apps:[...]}}` routes
  before the region route (forceDirect → `direct`, forceProxy → server URL). The
  redesigned `AppBypass.tsx` page (`appBypass.v2.*` i18n) lists apps via
  `_platform.appList.listInstalled`/`listRunning` and toggles overrides. The
  legacy `k2.advanced.app_bypass` key is migration-cleanup-only (discarded on
  first `load()`).
  See `docs/superpowers/specs/2026-05-27-app-bypass-routes-unification.md`.

## Style

- **MUI dark theme only**: Material-UI 5 with custom theme tokens. No light mode — do not add `@media (prefers-color-scheme)` branches or light palette variants.
- **Webapp subagent tasks**: For webapp UI decisions, prefer frontend-specialized agents (see root agent registry).

## Related Docs

- [Client Architecture](../CLAUDE.md)
- [Desktop Adapter](../desktop/CLAUDE.md)
- [Center API](../api/CLAUDE.md)
