# Feature: Kaitu Feature Migration

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | kaitu-feature-migration                  |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-16                               |
| Updated   | 2026-02-16                               |
| Tests     | 279 total (150 new)                      |

## Version History

| Version | Date       | Summary                                                    |
|---------|------------|------------------------------------------------------------|
| v1      | 2026-02-16 | Full feature migration from kaitu webapp to k2app          |

## Overview

Migrate all non-Dashboard features from the old kaitu webapp (`kaitu/client/webapp`) to k2app. The old app uses MUI 5 + k2api bridge; the new app uses Tailwind CSS v4 + direct cloudApi (antiblock). All UI must be rewritten in Tailwind; business logic and API contracts are preserved.

**Source**: `kaitu/client/webapp/src/` (MUI 5, React 18, kaitu-service bridge)
**Target**: `k2app/webapp/src/` (Tailwind v4, React 19, direct cloudApi + antiblock)

## Product Requirements

### PR1: Navigation Restructure

- Bottom nav changes from 3 tabs to 4 tabs: **Dashboard** / **Purchase** / **Invite** / **Account**
- Remove standalone Servers tab — server selection merges into Dashboard page
- Remove standalone Settings page — settings merge into Account page
- Discover is an Account sub-page, not a tab
- Feature flag system for conditional features (invite, discover, memberManagement, proHistory, feedback, deviceInstall, updateLoginEmail)
- Keep-alive pattern for tab pages: once mounted, hidden but not unmounted when switching tabs

### PR2: Login Flow Overhaul

- Remove dedicated `/login` route and `Login.tsx` page
- Replace with global `LoginDialog` component — modal overlay triggered on demand
- `LoginDialog` triggered by: AuthGuard redirect (app open when not logged in), Purchase page, Invite page, any LoginRequiredGuard-wrapped route
- Login dialog store (`login-dialog.store.ts`) with `open({ trigger, message })` and `close()`
- Two-step flow preserved: email → verification code
- On login success: dialog closes, triggering page refreshes its data

### PR3: Purchase Page

- New tab page at `/purchase` route
- Fetch plan list from API (`GET /api/plans`), sorted by month, highlight badge on recommended plan
- Plan selection cards: radio select, monthly price, total price, original price (strikethrough), savings chip
- Campaign code input: collapsible text field, real-time preview via `POST /api/user/orders { preview: true }`
- Order creation: `POST /api/user/orders { preview: false }` → open external payment URL → payment result dialog (success/fail)
- Member selection: buy for self and/or team members via `MemberSelection` component
- Inline login form for unauthenticated users (EmailLoginForm)
- Invite reward banner when user has invite code and app config has `inviteReward`
- Membership status banners: "complete first purchase" for trial, "authorization expired" for expired
- Payment result dialog: order number, amount, confirm success/fail buttons
- On payment success/fail: refresh user info, navigate to `/pro-histories?type=recharge`

### PR4: Invite Page

- New tab page at `/invite` route, requires login (LoginRequiredGuard)
- Load latest invite code from `GET /api/invite/my-codes/latest`
- Display invite code in large monospace font, click to copy
- Stats row: registered count + purchased count
- QR code generation from share link (desktop only, using `qrcode` library)
- Share actions with expiration selector popover: "generate complete share content" + "generate share link"
- Generate new invite code via `POST /api/invite/my-codes`
- Editable remark on invite code
- Retailer mode: show `RetailerStatsOverview` for retailer users
- Non-retailer: show `InviteRule` (invite rules) + retailer CTA
- "View All" link to `/invite-codes` sub-page

### PR5: Account Page

- New tab page at `/account` route, replaces old Settings page
- **Brand Banner**: Kaitu.io card with slogan, click opens website
- **Membership Status Card**: status chip (not logged in / active / expired / error), expiry date, refresh button, contextual action button (login / retry / renew)
- **Login Email**: masked email display, set/modify email button → `/update-email`
- **Set Password**: open password dialog → set/change account password
- **My Devices**: navigate to `/devices`
- **Member Management**: navigate to `/member-management`
- **Payment History**: navigate to `/pro-histories`
- **Wallet**: open external wallet URL
- **Device Install Guide**: navigate to `/device-install`
- **FAQ**: navigate to `/faq`
- **Language Selector**: multi-language with country flags, sync to native layer + server
- ~~**Theme Selector**~~: removed — dark-only mode, no user choice needed
- **Version**: display app version, hidden dev-mode activation (multi-click)
- **Logout button**: stop VPN, call logout API, clear session

### PR6: Account Sub-Pages

- **Devices** (`/devices`): device list with current device badge, editable remark, delete with confirmation dialog
- **Member Management** (`/member-management`): member list with status (active/expired/not activated), add by email, delete with confirmation, refresh
- **Pro History** (`/pro-histories`): payment and authorization history list, pagination, type filter from URL param, copy order number
- **Update Email** (`/update-email`): two-step email change (send code → verify), requires MembershipGuard
- **Device Install** (`/device-install`): multi-platform download links (iOS/Android/Windows/Mac), QR code for install URL, copy URL
- **FAQ** (`/faq`): self-help cards (security info, community link), navigate to issues/submit-ticket
- **Issues** (`/issues`): ticket list from API, status labels (open/closed), comment count, relative time, load more
- **Issue Detail** (`/issues/:number`): issue content + comments, add comment form
- **Submit Ticket** (`/submit-ticket`): subject + content form, silent log upload with feedback ID, requires MembershipGuard
- **Changelog** (`/changelog`): iframe embed of kaitu.io changelog page

### PR7: Discover Sub-Page

- Accessible from Account page, route `/discover`
- iframe embed of kaitu.io with progress bar animation
- External link handling: intercept iframe link clicks, open in system browser
- Auth state broadcast to iframe on login/logout

### PR8: Global Components

- **LoginDialog**: global modal for all login flows, replaces dedicated login page
- **ForceUpgradeDialog**: triggered by `appConfig.minClientVersion` from Cloud API, blocks app usage until upgrade
- **AnnouncementBanner**: top banner from app config, dismissible
- **ServiceAlert**: fixed-top alert for daemon connection failure or network errors
- **ErrorBoundary**: React error boundary with retry button
- **AlertContainer**: global toast/snackbar system for success/error/warning messages
- **MembershipGuard**: route guard that redirects non-members to Purchase page
- **LoginRequiredGuard**: route guard that opens LoginDialog for unauthenticated users
- **FeedbackButton**: floating draggable button for log collection + navigate to submit-ticket

### PR9: Excluded Features

The following features from old kaitu are **NOT migrated** because k2 daemon does not implement them:

- ~~DeveloperSettings page~~ (VPN mode tun/socks5/tproxy, log level, path type filter)
- ~~fix_network button in FAQ~~ (network repair command)
- ~~SpeedTest component in FAQ~~ (Go-level speed test infrastructure)
- ~~Tunnels page~~ (self-deploy nodes, was "coming soon" placeholder)
- ~~BridgeTest page~~ (development-only test page)

## Technical Decisions

### TD1: UI Framework — Tailwind CSS v4 + CVA + Radix

All MUI 5 components are rewritten in Tailwind. Use CVA (class-variance-authority) for variant-based component styling (already used by ConnectionButton). Use Radix UI primitives for accessible dialog/popover/select components. No MUI dependency.

### TD2: API Layer — Extend cloudApi

Old kaitu proxied all API calls through Go service (`k2api bridge`). k2app calls the Cloud API directly via `cloudApi` (fetch + antiblock). All new endpoints are added to `webapp/src/api/cloud.ts`:

```
// Auth
POST /api/auth/logout

// Plans and Orders
GET  /api/plans
POST /api/user/orders

// User
GET    /api/user/devices
DELETE /api/user/devices/:udid
PUT    /api/user/devices/:udid/remark
PUT    /api/user/language
GET    /api/user/pro-histories
PUT    /api/user/email
POST   /api/auth/email/code
POST   /api/user/password

// Members
GET    /api/user/members
POST   /api/user/members
DELETE /api/user/members/:uuid

// Invite
GET  /api/invite/my-codes/latest
GET  /api/invite/my-codes
POST /api/invite/my-codes
PUT  /api/invite/my-codes/:code/remark
POST /api/invite/share-link

// Issues
GET  /api/issues
GET  /api/issues/:number
POST /api/issues
POST /api/issues/:number/comments
```

### TD3: Platform Abstraction — PlatformApi

New `webapp/src/platform/` module providing cross-platform capabilities:

```typescript
interface PlatformApi {
  openExternal(url: string): Promise<void>;
  writeClipboard(text: string): Promise<void>;
  syncLocale?(lang: string): Promise<void>;
  uploadLogs?(feedbackId: string): Promise<void>;
  isMobile: boolean;
  version: string;
}
```

Three implementations:
- `TauriPlatform`: `@tauri-apps/plugin-shell` (open), `@tauri-apps/plugin-clipboard-manager` (clipboard), `@tauri-apps/api/app` (version)
- `CapacitorPlatform`: `@capacitor/browser` (open), `@capacitor/clipboard` (clipboard), `@capacitor/app` (version)
- `WebPlatform`: `window.open()` (open), `navigator.clipboard` (clipboard), fallback version from package.json

Factory: `createPlatform()` auto-detects Tauri/Capacitor/web environment.

### TD4: Login Pattern — Global LoginDialog Only

Remove `/login` route. All authentication flows use `LoginDialog` modal:
- App startup: if no valid session, show LoginDialog
- Protected routes: `LoginRequiredGuard` opens LoginDialog instead of redirect
- Purchase/Invite pages: open LoginDialog with contextual message
- `login-dialog.store.ts` Zustand store: `{ isOpen, trigger, message, open(), close() }`

### TD5: State Management — Extend Zustand Stores

New stores:
- `user.store.ts` — Extended user profile: membership status, expiry, devices, plan info. Replaces the `useUser()` hook pattern from old kaitu.
- `purchase.store.ts` — Plans list, selected plan, order state, campaign code
- `invite.store.ts` — Invite codes, share links, retailer stats
- `ui.store.ts` — Alert queue, announcement, feature flags, app config (no theme — dark-only)
- `login-dialog.store.ts` — Login dialog open/close state with trigger context

Modified stores:
- `auth.store.ts` — Add logout API, remove redirect, LoginDialog integration
- `servers.store.ts` — Keep as-is, used by merged Dashboard

### TD6: Navigation — 4-Tab BottomNav + Keep-Alive

Tab config:
```
[
  { path: '/',         icon: Dashboard,     label: 'dashboard:title' },
  { path: '/purchase', icon: ShoppingCart,   label: 'nav:purchase' },
  { path: '/invite',   icon: CardGiftcard,   label: 'nav:invite',
    requiresLogin: true, featureFlag: 'invite' },
  { path: '/account',  icon: AccountCircle,  label: 'nav:account' },
]
```

Keep-alive: Tab pages are lazy-loaded on first visit, then hidden (visibility: hidden + position: absolute) when inactive. Non-tab sub-pages use normal `<Outlet />` routing.

### TD7: Dark-Only Theme — CSS Variables

**Dark mode only. No light mode. No theme switcher.** All colors defined as CSS custom properties in `app.css` using kaitu webapp's dark palette (`theme.ts` dark palette + `theme/colors.ts` dark object). No `dark:` Tailwind prefix needed — dark colors are the base colors. No ThemeContext, no theme store, no theme persistence. Body background `#0F0F13`, paper surface `#1A1A1D`, text `rgba(255,255,255,0.95)`.

Key dark palette tokens:
- Primary: `#42A5F5`, Accent: `#00ffff` (neon cyan)
- Card bg: `rgba(20, 25, 45, 0.9)`, Card border: `rgba(255, 255, 255, 0.12)`
- Success: `#66bb6a`, Warning: `#ffa726`, Error: `#ef5350`, Info: `#42a5f5`
- Background gradient: `linear-gradient(135deg, #0a0e27 0%, #1a1f3a 50%, #2d1b4e 100%)`

### TD8: i18n — Extend Existing Setup

Add new namespaces to existing i18n system (zh-CN default, en-US secondary):
- `nav` — navigation labels
- `purchase` — purchase page
- `invite` — invite page
- `account` — account page
- `feedback` — FAQ, issues, tickets
- ~~`theme`~~ — removed (dark-only, no theme labels needed)

Locale files: `webapp/src/i18n/locales/{lang}/{namespace}.json`

### TD9: Feature Flags

App config from `GET /api/app/config` provides feature flags. Local fallback config for offline/error scenarios. Feature flags control:
- Tab visibility (invite, discover)
- Route availability
- Conditional UI sections

Store in `ui.store.ts` or dedicated `app-config.store.ts`.

### TD10: QR Code — qrcode library

Use `qrcode` npm package for invite code QR generation and device install QR. Same library as old kaitu.

## Architecture

### Route Structure

```
/                       Dashboard (tab, keep-alive)
/purchase               Purchase (tab, keep-alive)
/invite                 InviteHub (tab, keep-alive, LoginRequired)
/account                Account (tab, keep-alive)

/pro-histories          ProHistory (sub-page, LoginRequired)
/invite-codes           MyInviteCodeList (sub-page, LoginRequired)
/member-management      MemberManagement (sub-page, LoginRequired)
/devices                Devices (sub-page, LoginRequired)
/device-install         DeviceInstall (sub-page)
/update-email           UpdateLoginEmail (sub-page, MembershipGuard)
/faq                    FAQ (sub-page)
/issues                 Issues (sub-page, LoginRequired)
/issues/:number         IssueDetail (sub-page, LoginRequired)
/submit-ticket          SubmitTicket (sub-page, MembershipGuard)
/changelog              Changelog (sub-page)
/discover               Discover (sub-page)
```

### File Structure (new/modified)

```
webapp/src/
  api/
    cloud.ts                 [MODIFIED] Add 20+ new endpoints
    types.ts                 [MODIFIED] Add Plan, Order, Device, Member, Issue types
  platform/
    types.ts                 [NEW] PlatformApi interface
    tauri.ts                 [NEW] TauriPlatform implementation
    capacitor.ts             [NEW] CapacitorPlatform implementation
    web.ts                   [NEW] WebPlatform fallback
    index.ts                 [NEW] Factory: createPlatform()
  stores/
    auth.store.ts            [MODIFIED] Add logout API call, LoginDialog integration
    vpn.store.ts             (unchanged)
    servers.store.ts         (unchanged)
    user.store.ts            [NEW] Extended user profile, membership status
    purchase.store.ts        [NEW] Plans, orders, campaign
    invite.store.ts          [NEW] Invite codes, share links
    login-dialog.store.ts    [NEW] Global login dialog state
    ui.store.ts              [NEW] Alerts, announcements, feature flags (no theme)
  components/
    Layout.tsx               [MODIFIED] Keep-alive tabs, SideNav support
    BottomNav.tsx            [MODIFIED] 4 tabs, feature flags
    LoginDialog.tsx          [NEW] Global login modal
    EmailLoginForm.tsx       [NEW] Inline email login form
    ForceUpgradeDialog.tsx   [NEW] Force upgrade modal
    AnnouncementBanner.tsx   [NEW] Top announcement bar
    ServiceAlert.tsx         [NEW] Fixed service error alert
    AlertContainer.tsx       [NEW] Toast notification container
    ErrorBoundary.tsx        [NEW] React error boundary
    MembershipGuard.tsx      [NEW] Membership route guard
    LoginRequiredGuard.tsx   [NEW] Login route guard (opens dialog)
    FeedbackButton.tsx       [NEW] Floating feedback button
    MemberSelection.tsx      [NEW] Buy-for-self/members selector
    InviteRule.tsx           [NEW] Invite rules display
    RetailerStatsOverview.tsx [NEW] Retailer dashboard
    ExpirationSelectorPopover.tsx [NEW] Share link expiration picker
    BackButton.tsx           [NEW] Navigation back button
    Pagit.tsx                [NEW] Pagination component
    LoadingAndEmpty.tsx      [NEW] Loading/empty/error states
    PasswordDialog.tsx       [NEW] Set/change password modal
    VersionItem.tsx          [NEW] Version display with dev mode
    ConnectionButton.tsx     (unchanged)
    ServerList.tsx           (unchanged)
    ServiceReadiness.tsx     (unchanged)
    UpdatePrompt.tsx         (unchanged)
  pages/
    Dashboard.tsx            [MODIFIED] Integrate server selection from Servers page
    Purchase.tsx             [NEW] Plan selection + payment
    InviteHub.tsx            [NEW] Invite code management
    Account.tsx              [NEW] Account settings (replaces Settings.tsx)
    Devices.tsx              [NEW] Device management
    MemberManagement.tsx     [NEW] Team member management
    ProHistory.tsx           [NEW] Payment/authorization history
    UpdateLoginEmail.tsx     [NEW] Change login email
    DeviceInstall.tsx        [NEW] Multi-device install guide
    FAQ.tsx                  [NEW] Help and troubleshooting
    Issues.tsx               [NEW] Support ticket list
    IssueDetail.tsx          [NEW] Ticket detail + comments
    SubmitTicket.tsx         [NEW] Submit support ticket
    Changelog.tsx            [NEW] Embedded changelog
    Discover.tsx             [NEW] Embedded discover page
    Servers.tsx              [DELETE] Merged into Dashboard
    Settings.tsx             [DELETE] Replaced by Account
    Login.tsx                [DELETE] Replaced by LoginDialog
  hooks/
    useUser.ts               [NEW] User profile convenience hook
    useShareLink.ts          [NEW] Invite share link generation
    useInviteCodeActions.ts  [NEW] Invite code operations
  i18n/
    locales/
      zh-CN/
        nav.json             [NEW]
        purchase.json        [NEW]
        invite.json          [NEW]
        account.json         [NEW]
        feedback.json        [NEW]
        _(theme.json removed — dark-only)_
      en-US/
        nav.json             [NEW]
        purchase.json        [NEW]
        invite.json          [NEW]
        account.json         [NEW]
        feedback.json        [NEW]
        _(theme.json removed — dark-only)_
  App.tsx                    [MODIFIED] Remove /login route, add new routes, LoginDialog
  main.tsx                   [MODIFIED] Add ThemeProvider, ErrorBoundary
```

### Dashboard Integration

Current Dashboard shows: VPN status + connect button + uptime.
After migration, Dashboard also includes:
- Server selection (merged from Servers page): server list or compact selected server display
- Cloud tunnel list (from cloud API when logged in)
- Connection notification bar (connected server info)

The current `ServerList` and `ConnectionButton` components are reused. The `Servers.tsx` page content moves into Dashboard as a collapsible section or integrated panel.

## Acceptance Criteria

### Navigation and Layout
- AC1: Bottom nav shows 4 tabs: Dashboard, Purchase, Invite, Account
- AC2: Tab pages use keep-alive pattern — switching tabs preserves scroll position and state
- AC3: Invite tab hidden when feature flag `invite` is false
- AC4: Non-tab pages show back button in top-left corner

### Login
- AC5: No `/login` route exists; all login flows use LoginDialog modal
- AC6: Opening app without valid session shows LoginDialog automatically
- AC7: LoginDialog supports two-step flow: email then code then success
- AC8: After login success, dialog closes and current page refreshes data
- AC9: LoginRequiredGuard opens LoginDialog instead of redirecting

### Purchase
- AC10: Purchase page loads plan list from API and displays sorted by month
- AC11: Highlighted plan shows ribbon badge
- AC12: Selecting plan shows monthly and total price with currency formatting
- AC13: Campaign code input triggers preview order and shows discounted price
- AC14: "Pay Now" button creates order and opens payment URL in system browser
- AC15: Payment result dialog shows order number and amount, with success/fail actions
- AC16: Unauthenticated users see inline login form instead of member selection
- AC17: MemberSelection allows buying for self and/or selected team members

### Invite
- AC18: Invite page shows latest invite code in large monospace font
- AC19: Clicking invite code copies to clipboard with success toast
- AC20: Stats show registered and purchased counts
- AC21: QR code generated from share link (desktop only)
- AC22: Share buttons open expiration selector popover
- AC23: "Generate New Code" creates new invite code via API
- AC24: Invite code remark is editable inline

### Account
- AC25: Account page shows membership status card with expiry date
- AC26: Logout button stops VPN, calls logout API, clears session
- AC27: Language selector changes i18n locale and syncs to native + server
- ~~AC28~~: Removed — dark-only mode, no theme selector needed
- AC29: Version displays app version; 5 rapid clicks activates dev mode (hidden)
- AC30: All sub-page links navigate to correct routes

### Account Sub-Pages
- AC31: Devices page lists all devices with current device highlighted
- AC32: Device remark editable inline, delete with confirmation
- AC33: Member management allows add by email and delete with confirmation
- AC34: Pro history shows paginated list with type filter
- AC35: Update email flow: enter email then send code then enter code then confirm
- AC36: Device install shows QR code and platform-specific download buttons
- AC37: FAQ shows help cards with links to issues and submit-ticket
- AC38: Issues list loads from API with status labels and pagination
- AC39: Issue detail shows comments with ability to reply
- AC40: Submit ticket sends title + content to API

### Global Components
- AC41: ForceUpgradeDialog blocks app when version is less than minClientVersion
- AC42: AnnouncementBanner displays when app config has announcement
- AC43: ServiceAlert shows when daemon is unreachable
- AC44: ErrorBoundary catches render errors and shows retry UI
- AC45: AlertContainer shows toast notifications triggered from any component
- AC46: FeedbackButton is draggable and navigates to submit-ticket
- AC47: MembershipGuard redirects non-members to Purchase page

### Platform
- AC48: openExternal(url) opens URL in system browser on Tauri and Capacitor
- AC49: writeClipboard(text) copies text on all platforms
- AC50: Dark theme renders correctly — all components use CSS variable design tokens, no hardcoded colors

### API
- AC51: All 20+ new cloudApi endpoints are callable and handle errors consistently
- AC52: Auth token automatically included in authenticated requests
- AC53: Token refresh handles 401 responses transparently

## Testing Strategy

- Unit tests for all new Zustand stores (purchase, invite, user, ui, login-dialog) using MockVpnClient pattern
- Unit tests for PlatformApi implementations (mock Tauri/Capacitor globals)
- Component tests for LoginDialog, MemberSelection, PlanList, ExpirationSelectorPopover using testing-library/react
- Integration tests for login flow (dialog open, email, code, close, state updated)
- Integration tests for purchase flow (select plan, preview, pay, result)
- API tests for all new cloudApi endpoints (mock fetch)
- Target: maintain 80% or higher coverage on new code

## Deployment and CI/CD

- No CI changes needed — existing `yarn test` and `yarn build` cover new code
- New Tailwind components use existing `app.css` + Tailwind v4 config
- New npm dependencies: `qrcode`, `@radix-ui/react-dialog`, `@radix-ui/react-popover`, `@radix-ui/react-select`
- Tauri plugins (if not already present): `@tauri-apps/plugin-shell`, `@tauri-apps/plugin-clipboard-manager`

## Impact Analysis

### Affected Modules

| Module | Impact | Details |
|--------|--------|---------|
| `App.tsx` | **Major** | Complete route restructure, remove /login, add 15+ routes |
| `components/Layout.tsx` | **Major** | Keep-alive tab system, side nav support |
| `components/BottomNav.tsx` | **Major** | 3 to 4 tabs, feature flags, icon change |
| `stores/auth.store.ts` | **Moderate** | Add logout API, remove redirect, LoginDialog integration |
| `pages/Dashboard.tsx` | **Moderate** | Integrate server selection |
| `api/cloud.ts` | **Major** | Add 20+ new API endpoints |
| `api/types.ts` | **Major** | Add 15+ new type definitions |
| `pages/Login.tsx` | **Delete** | Replaced by LoginDialog |
| `pages/Settings.tsx` | **Delete** | Replaced by Account |
| `pages/Servers.tsx` | **Delete** | Merged into Dashboard |
| `i18n/locales/` | **Major** | 6 new namespace files per language |

### Scope Assessment

- **Size**: Large (30+ new files, 3 deleted, 6 modified)
- **Risk**: Medium — UI rewrite from MUI to Tailwind is mechanical but extensive; API contracts preserved from production-verified old app
- **Migration notes**: All i18n keys from old app must be audited and mapped to new namespace structure. Old kaitu used k2api bridge proxy pattern; all calls must be converted to direct cloudApi functions.
