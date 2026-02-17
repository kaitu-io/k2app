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
│   Web:       JS inject -> web fallbacks                  │
└──────────┬────────────────────────┬──────────────────────┘
           ↓                        ↓
┌─────────────────────┐  ┌─────────────────────────────────┐
│ window._k2: IK2Vpn  │  │ window._platform: IPlatform     │
│   run(action, params)│  │   os, isDesktop, isMobile       │
│                      │  │   version                       │
│ VPN actions:         │  │   storage: ISecureStorage        │
│   start, stop,       │  │   getUdid()                     │
│   status, reconnect, │  │   writeClipboard(), readClipboard│
│   evaluate_tunnels,  │  │   openExternal()                │
│   speedtest, etc.    │  │   debug(), warn()               │
└──────────┬───────────┘  └────────────────────────────────┘
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
| `IPlatform` | `window._platform` | Platform capabilities: storage, UDID, clipboard, logging |
| `ISecureStorage` | `window._platform.storage` | Encrypted key-value storage |

### VPN Actions (via window._k2.run)

`start`, `stop`, `status`, `reconnect`, `get_config`, `set_config`, `get_config_options`, `speedtest`, `get_speedtest_status`, `fix_network`, `version`, `get_metrics`, `evaluate_tunnels`

### API Calls (via cloudApi / k2api)

Cloud API calls go through `cloudApi.request()` which handles auth headers and token refresh. The `k2api()` wrapper adds caching and SWR support. Auth success/failure/401/402 side effects are handled by k2api.

---

## Tech Stack

React 19, Material-UI 5, React Router 7, i18next, Vite 6, Zustand, TypeScript.

---

## Directory Structure

```
webapp/
├── src/
│   ├── types/              # Type definitions (kaitu-core.ts = IK2Vpn + IPlatform)
│   ├── services/           # cloudApi, k2api, authService, web-platform
│   ├── core/               # Core module (getK2, isK2Ready, waitForK2, polling)
│   ├── stores/             # Zustand stores (vpn, auth, evaluation, dashboard, ...)
│   ├── pages/              # Route pages
│   ├── components/         # UI components
│   ├── hooks/              # Custom hooks (useEvaluation, useUser, etc.)
│   ├── i18n/locales/       # Locale files (zh-CN, en-US, ja, zh-TW, etc.)
│   ├── utils/              # Utilities (errorHandler.ts, versionCompare.ts)
│   ├── config/             # App configuration
│   └── theme/              # MUI theme
└── package.json
```

---

## i18n

| Code | Language | Role |
|------|----------|------|
| `zh-CN` | Simplified Chinese | Primary (add new text here first) |
| `en-US` | English | Manual translation |
| `ja` | Japanese | Manual translation |
| `zh-TW` | Traditional Chinese | Manual translation |

---

## Commands

```bash
yarn install                             # Install dependencies (from workspace root)
cd webapp && yarn dev                    # Dev server
cd webapp && yarn build                  # Production build
cd webapp && npx vitest run              # Run all tests
cd webapp && npx vitest run --reporter=verbose  # Verbose test output
```

---

## Troubleshooting

| Problem | Check |
|---------|-------|
| `window._k2` is undefined | Platform injection not running. Desktop: check Tauri inject. Web: check HTTP inject. |
| `window._platform` is undefined | Platform injection not running. Check bootstrap sequence in main.tsx. |
| VPN operations fail | Is kaitu-service running? Check service logs and network permissions. |
| API calls fail | Token expired? Check network. Check browser Network panel. |
| Service reachable? | `curl http://127.0.0.1:1777/ping` |

## Related Docs

- [Client Architecture](../CLAUDE.md)
- [Desktop Adapter](../desktop/CLAUDE.md)
