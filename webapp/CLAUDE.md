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
  - Platform capabilities via window._platform (storage, getUdid, clipboard, etc.)
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
- `window._platform: IPlatform` -- Platform capabilities (storage, UDID, clipboard, etc.)
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
│ VPN actions:         │  │   getUdid(), syncLocale()       │
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
| `IPlatform` | `window._platform` | Platform capabilities: storage, UDID, clipboard, openExternal, syncLocale |
| `ISecureStorage` | `window._platform.storage` | Encrypted key-value storage |
| `IUpdater` | `window._platform.updater` | Auto-update: check, apply, status, channel |

### VPN Actions (via window._k2.run)

`up`, `down`, `status`, `version`

### API Calls (via cloudApi / k2api)

Cloud API calls go through `cloudApi.request()` which handles auth headers and token refresh. The `k2api()` wrapper adds caching and SWR support. Auth success/failure/401/402 side effects are handled by k2api.

---

## Tech Stack

React 18, Material-UI 5, React Router 7, i18next, Vite 6, Zustand, TypeScript.

---

## Directory Structure

```
webapp/
├── src/
│   ├── types/              # Type definitions (kaitu-core.ts = IK2Vpn + IPlatform + IUpdater)
│   ├── services/           # cloudApi, k2api, authService, cacheStore, web-platform, standalone-k2, beta-auto-upload
│   ├── core/               # Core module (getK2, isK2Ready, waitForK2)
│   ├── stores/             # Zustand stores (vpn-machine, connection, config, auth, alert, layout, dashboard, login-dialog, self-hosted, onboarding)
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

## Key Patterns

- **Store init**: `initializeAllStores()` calls layout → config.loadConfig → selfHosted.loadTunnel → auth → vpn-machine init in order. Returns cleanup function.
- **VPN state machine**: `vpn-machine.store.ts` — explicit 7-state machine (`idle`, `connecting`, `connected`, `reconnecting`, `disconnecting`, `error`, `serviceDown`) with transition table. Module-level `dispatch(event, payload)` is the only way to change state. 3s debounce for `connected → reconnecting` is the only timer. Supports both SSE event-driven mode (desktop) and 2s polling fallback (standalone).
- **Connection store**: `connection.store.ts` — owns tunnel selection, connect/disconnect orchestration, `connectedTunnel` snapshot (frozen at connect time for stable UI), and `connectEpoch` guard (prevents stale async ops)
- **Keep-alive tabs**: Layout caches visited tab outlets, hides inactive with `visibility:hidden`. Tab paths: `/`, `/invite`, `/discover`, `/account`
- **Keep-alive + GPU layers gotcha**: WebKit doesn't recomposite layers when `opacity`/`filter` are removed while an element is `visibility:hidden`. Dashboard uses a `translateZ(0)` toggle on hidden→visible transitions to force layer rebuild. Any new compositing-layer CSS changes on keep-alive tabs need similar consideration.
- **Config store**: `useConfigStore()` in `stores/config.store.ts` persists VPN settings (ruleMode, proxyMode, logLevel, server). `buildConnectConfig(serverUrl?)` assembles `ClientConfig` from stored preferences — forces `log.level = 'debug'` when beta channel active. `updateConfig(partial)` merges and persists.
- **LoginDialog**: Global modal via `login-dialog.store`. Guards call `openLoginDialog()` instead of redirecting
- **Feature flags**: `getCurrentAppConfig().features` controls route/tab visibility
- **Dev proxy**: Vite proxies `/api/*` and `/ping` to `:1777`. Production uses absolute URL
- **Config-driven connect**: `_k2.run('up', config)` where config is assembled from server + user preferences
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
cd webapp && npx tsc --noEmit            # Type check
```

---

## Troubleshooting

| Problem | Check |
|---------|-------|
| `window._k2` is undefined | Platform injection not running. Desktop: check Tauri inject. Web: check standalone-k2 fallback. |
| `window._platform` is undefined | Platform injection not running. Check bootstrap in main.tsx → ensureK2Injected(). |
| VPN operations fail | Is kaitu-service running? Check service logs and network permissions. |
| API calls fail | Token expired? Check network. Check browser Network panel. |
| Service reachable? | `curl http://127.0.0.1:1777/ping` |
| White flash on app start | `index.html` must use `background: #0f0f13` directly on `html, body` WITHOUT `@media (prefers-color-scheme: dark)`. Media query causes 100-300ms white flash on light-mode OS before MUI loads. |
| Login fails with 422 | All login paths must include `udid` from `window._platform!.getUdid()` in POST body. Backend requires UDID for device association. |

## Related Docs

- [Client Architecture](../CLAUDE.md)
- [Desktop Adapter](../desktop/CLAUDE.md)
- [Center API](../api/CLAUDE.md)
