# Kaitu WebApp

Shared React UI codebase running on Web, Desktop (Tauri), and Mobile (Capacitor).

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

Error code-to-i18n mapping lives in `utils/errorHandler.ts`. Use `handleResponseError()` and `getErrorMessage()` from that module. In catch blocks, log the raw error and show an i18n fallback string to the user.

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
│   uploadLogs?()                │
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
| `IUpdater` | `window._platform.updater` | Auto-update: check, apply, status |

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
│   ├── services/           # cloudApi, k2api, authService, cacheStore, web-platform, standalone-k2
│   ├── core/               # Core module (getK2, isK2Ready, waitForK2, polling)
│   ├── stores/             # Zustand stores (vpn, auth, alert, layout, dashboard, login-dialog)
│   ├── pages/              # Route pages
│   ├── components/         # UI components
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

Namespaces: common, dashboard, auth, purchase, invite, account, feedback, nav, retailer, startup, theme, ticket, wallet

---

## Key Patterns

- **Store init**: `initializeAllStores()` calls layout → auth → vpn store init in order. Stores use `init()` action (not async `create()`)
- **Keep-alive tabs**: Layout caches visited tab outlets, hides inactive with `visibility:hidden`. Tab paths: `/`, `/invite`, `/discover`, `/account`
- **LoginDialog**: Global modal via `login-dialog.store`. Guards call `openLoginDialog()` instead of redirecting
- **Feature flags**: `getCurrentAppConfig().features` controls route/tab visibility
- **Dev proxy**: Vite proxies `/api/*` and `/ping` to `:1777`. Production uses absolute URL
- **Config-driven connect**: `_k2.run('up', config)` where config is assembled from server + user preferences
- **AuthGate**: Wraps all routes — checks service readiness + version match before rendering

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

## Related Docs

- [Client Architecture](../CLAUDE.md)
- [Desktop Adapter](../desktop/CLAUDE.md)
- [Center API](../api/CLAUDE.md)
