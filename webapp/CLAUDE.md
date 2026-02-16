# webapp — React Frontend

React 19 + TypeScript + Vite + Tailwind CSS v4. Served by tauri-plugin-localhost on `http://localhost:14580` (production) or Vite dev server on `:1420` (dev).

## Commands

```bash
yarn test              # vitest run (95 tests)
yarn build             # Vite production build → dist/
npx tsc --noEmit       # Type check
```

## Structure

```
src/
├── vpn-client/        VpnClient abstraction (THE boundary to k2 daemon)
│   ├── types.ts       VpnClient interface, VpnStatus, VpnEvent, ReadyState
│   ├── http-client.ts HttpVpnClient: HTTP to :1777 + 2s polling → events (dedup)
│   ├── mock-client.ts MockVpnClient: test double with observable state
│   └── index.ts       Factory: createVpnClient(override?) + getVpnClient()
├── api/               Cloud API (antiblock exception — NOT through VpnClient)
│   ├── cloud.ts       cloudApi: login (email→code), servers, user endpoints
│   ├── antiblock.ts   Entry URL resolution: localStorage cache → CDN JSONP → default
│   └── types.ts       API response types
├── stores/            Zustand state (async init pattern: null → init() → loaded)
│   ├── vpn.store.ts   VPN state, connect/disconnect, event subscription
│   ├── auth.store.ts  Login flow, token persistence (localStorage), session restore
│   └── servers.store.ts Server list, selection, auto-select first
├── components/        Shared UI
│   ├── Layout.tsx     Shell with Outlet + BottomNav
│   ├── BottomNav.tsx  Tab navigation (Dashboard, Servers, Settings)
│   ├── ConnectionButton.tsx CVA-styled connect/disconnect button
│   ├── ServerList.tsx Server list with country, city, load display
│   └── ServiceReadiness.tsx Blocks UI until daemon ready + version match
├── pages/             Route pages
│   ├── Dashboard.tsx  VPN status + connection button + selected server
│   ├── Login.tsx      Two-step: email → verification code
│   ├── Servers.tsx    Server list + selection
│   └── Settings.tsx   Language, version, about
├── i18n/              i18next setup + locale JSON (zh-CN default, en-US)
├── App.tsx            Routes: /login, / (Dashboard), /servers, /settings + AuthGuard
└── main.tsx           React root mount
```

## Key Patterns

- **VpnClient DI**: `createVpnClient(mock)` in tests — no module mocking needed
- **Async store init**: Zustand stores use `init()` action (not async `create()`), called from `useEffect`
- **Dev proxy**: Vite proxies `/api/*` and `/ping` to `:1777`. Production uses absolute URL via `import.meta.env.DEV`
- **connect/disconnect resolve on acceptance** (HTTP 200), not operation completion
- **Polling dedup**: HttpVpnClient compares `JSON.stringify(prev)` — no redundant events

## Testing

- vitest + @testing-library/react + @testing-library/user-event
- Mock pattern: `resetVpnClient()` → `new MockVpnClient()` → `createVpnClient(mock)` in beforeEach
- i18n mock: `vi.mock('react-i18next')` with key→string map
- 13 test files, 95 tests total
