# webapp — React Frontend

React 19 + TypeScript + Vite + Tailwind CSS v4. Served by tauri-plugin-localhost on `http://localhost:14580` (production) or Vite dev server on `:1420` (dev).

## Commands

```bash
yarn test              # vitest run (294 tests, 50 files)
yarn build             # Vite production build → dist/
npx tsc --noEmit       # Type check
```

## Structure

```
src/
├── vpn-client/        VpnClient abstraction (THE boundary to k2 daemon)
│   ├── types.ts       VpnClient interface, VpnStatus, VpnEvent, ReadyState
│   ├── http-client.ts HttpVpnClient: HTTP to :1777 + 2s polling → events (dedup)
│   ├── native-client.ts NativeVpnClient: Capacitor K2Plugin bridge (mobile)
│   ├── mock-client.ts MockVpnClient: test double with observable state
│   └── index.ts       Factory: initVpnClient() (async, mobile) + createVpnClient() (sync, desktop)
├── api/               Cloud API (antiblock exception — NOT through VpnClient)
│   ├── cloud.ts       cloudApi: login, servers, user, purchase, invite, member, device, issue endpoints
│   ├── antiblock.ts   Entry URL resolution: AES-256-GCM decrypt → localStorage cache → CDN JSONP → default
│   └── types.ts       API response types (Plan, Order, InviteCode, Device, Member, Issue, etc.)
├── stores/            Zustand state (async init pattern: null → init() → loaded)
│   ├── vpn.store.ts       VPN state, connect/disconnect, event subscription, daemonReachable
│   ├── auth.store.ts      Login flow, token persistence (localStorage), session restore, logout
│   ├── servers.store.ts   Server list, selection, auto-select first
│   ├── user.store.ts      User profile, membership info
│   ├── purchase.store.ts  Plans, order preview, campaign code, order creation
│   ├── invite.store.ts    Invite codes CRUD, latest code, share link
│   ├── ui.store.ts        App config, feature flags, alerts, announcement
│   └── login-dialog.store.ts  Global login modal state (open/close/callback)
├── hooks/             Composition hooks
│   ├── useUser.ts     User profile from user.store
│   ├── useShareLink.ts  Share link generation via cloudApi
│   └── useInviteCodeActions.ts  Invite code CRUD operations
├── components/        Shared UI
│   ├── Layout.tsx         Keep-alive shell: caches visited tab outlets, hides inactive tabs
│   ├── BottomNav.tsx      4 tabs: Dashboard, Purchase, Invite (feature-flagged), Account
│   ├── LoginDialog.tsx    Global login modal (replaces /login route for auth flows)
│   ├── LoginRequiredGuard.tsx  Opens LoginDialog when unauthenticated
│   ├── MembershipGuard.tsx     Redirects to /purchase when no active membership
│   ├── ErrorBoundary.tsx  React error boundary with retry
│   ├── ForceUpgradeDialog.tsx  Blocking overlay when app < minClientVersion
│   ├── AnnouncementBanner.tsx  Top banner from app config
│   ├── ServiceAlert.tsx   Daemon unreachable alert
│   ├── AlertContainer.tsx Toast notification system from ui.store
│   ├── FeedbackButton.tsx Navigates to /issues
│   ├── BackButton.tsx     Sub-page back navigation
│   ├── Pagit.tsx          Reusable pagination component
│   ├── LoadingAndEmpty.tsx  Loading spinner + empty state
│   ├── MemberSelection.tsx  Member picker for purchase flow
│   ├── ExpirationSelectorPopover.tsx  Expiration date picker
│   ├── InviteRule.tsx     Invite rules display
│   ├── RetailerStatsOverview.tsx  Retailer stats card
│   ├── PasswordDialog.tsx  Password input dialog
│   ├── VersionItem.tsx    Version display item
│   ├── ConnectionButton.tsx CVA-styled connect/disconnect button
│   ├── ServerList.tsx     Server list with country, city, load display
│   ├── ServiceReadiness.tsx Blocks UI until daemon ready + version match
│   ├── UpdatePrompt.tsx   OTA update notification + apply flow
│   └── EmailLoginForm.tsx Two-step email login form
├── pages/             Route pages
│   ├── Dashboard.tsx      VPN status + connection button + selected server
│   ├── Purchase.tsx       Plan cards, campaign code, payment flow
│   ├── InviteHub.tsx      Invite code display, QR, share, remark
│   ├── Account.tsx        Membership card, settings menu, language, logout
│   ├── Devices.tsx        Device list, remark edit, delete
│   ├── MemberManagement.tsx  Member CRUD (add/edit/delete)
│   ├── ProHistory.tsx     Paginated purchase history
│   ├── MyInviteCodeList.tsx  All invite codes list with remark editing
│   ├── UpdateLoginEmail.tsx  Two-step email update flow
│   ├── DeviceInstall.tsx  Platform download cards
│   ├── FAQ.tsx            Help topic cards
│   ├── Issues.tsx         Paginated issue list
│   ├── IssueDetail.tsx    Issue detail + comments
│   ├── SubmitTicket.tsx   Submit ticket form
│   ├── Changelog.tsx      iframe changelog embed
│   ├── Discover.tsx       iframe + auth postMessage bridge
│   ├── Login.tsx          Legacy login page (to be removed)
│   ├── Servers.tsx        Server list + selection (to be absorbed into Dashboard)
│   └── Settings.tsx       Language, version, about (to be absorbed into Account)
├── platform/          PlatformApi abstraction (Tauri, Capacitor, Web backends)
│   ├── types.ts       PlatformApi interface
│   ├── tauri.ts       Tauri desktop backend
│   ├── capacitor.ts   Capacitor mobile backend
│   ├── web.ts         Web fallback backend
│   └── index.ts       Auto-detect platform, export singleton
├── i18n/              i18next: 8 namespaces × 2 locales (zh-CN default, en-US)
│                      Namespaces: common, dashboard, auth, settings, purchase, invite, account, feedback
├── App.tsx            16 routes + global components (ErrorBoundary, LoginDialog, ForceUpgradeDialog, etc.)
└── main.tsx           Async bootstrap: initVpnClient() → React render
```

## Key Patterns

- **VpnClient DI**: `createVpnClient(mock)` in tests — no module mocking needed. Mobile uses `initVpnClient()` (async) with `registerPlugin('K2Plugin')` from `@capacitor/core`
- **Async store init**: Zustand stores use `init()` action (not async `create()`), called from `useEffect`
- **Keep-alive tabs**: Layout caches visited tab outlets in state, hides inactive with `visibility:hidden; position:absolute`. Tab paths: `/`, `/purchase`, `/invite`, `/account`
- **LoginDialog pattern**: Global modal via `login-dialog.store`. Guards call `openLoginDialog()` instead of redirecting to `/login`
- **Feature flags**: `ui.store.getFeatureFlags()` controls tab visibility (e.g., `showInviteTab`)
- **Dev proxy**: Vite proxies `/api/*` and `/ping` to `:1777`. Production uses absolute URL via `import.meta.env.DEV`
- **Config-driven connect**: `connect(config: ClientConfig)` replaces `connect(wireUrl: string)`. Webapp assembles ClientConfig from server.wireUrl + user preferences (rule mode). Dashboard exports `buildConfig()`.
- **connect/disconnect resolve on acceptance** (HTTP 200), not operation completion
- **Polling dedup**: HttpVpnClient compares `JSON.stringify(prev)` — no redundant events
- **Store mock pattern**: `vi.mock('../../stores/X.store')` then `vi.mocked(useXStore).mockReturnValue({...})` for component tests

## Testing

- vitest + @testing-library/react + @testing-library/user-event
- Mock pattern: `resetVpnClient()` → `new MockVpnClient()` → `createVpnClient(mock)` in beforeEach
- Store mock pattern: `vi.mock('../../stores/X.store')` + `vi.mocked(useXStore).mockReturnValue({})`
- i18n mock: `vi.mock('react-i18next')` with key→string map (returns keys as values)
- 50 test files, 294 tests total
