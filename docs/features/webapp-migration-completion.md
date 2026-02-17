# Feature: Webapp Migration Completion

## Meta

| Field     | Value                          |
|-----------|--------------------------------|
| Feature   | webapp-migration-completion    |
| Version   | v1                             |
| Status    | draft                          |
| Created   | 2026-02-16                     |
| Updated   | 2026-02-16                     |

## Version History

| Version | Date       | Summary                                                    |
|---------|------------|------------------------------------------------------------|
| v1      | 2026-02-16 | Initial: Full webapp parity with kaitu/client/webapp       |

## Product Requirements

### PR1: New Pages (v1)

- PR1.1: **DeveloperSettings** page at `/developer-settings` — VPN mode selector (tun/socks5/tproxy), log level selector, protocol path filter (TCP-WS/QUIC/all), feature-flagged visibility (v1)
- PR1.2: **Tunnels** page at `/tunnels` — self-deploy node placeholder ("coming soon" state), cloud node section with login prompt for unauthenticated users (v1)
- PR1.3: **ServiceError** page at `/service-error` — error reason cards (auto-update failure, security software blocking), solution suggestions, FAQ link, accessible from service readiness failure (v1)

### PR2: New Components (v1)

- PR2.1: **SideNavigation** — desktop sidebar navigation (MUI Drawer style, Tailwind implementation), menu items matching BottomNav tabs + sub-page links, collapsible groups, active state highlighting (v1)
- PR2.2: **CloudTunnelList** — cloud tunnel/node list with connection status badges, load percentage via VerticalLoadBar, country/city display, selection handling (v1)
- PR2.3: **CollapsibleConnectionSection** — expandable/collapsible VPN connection detail area with animation (v1)
- PR2.4: **CompactConnectionButton** — compact variant of ConnectionButton for mobile/narrow layouts (v1)
- PR2.5: **ConnectionNotification** — toast notification for VPN state changes (connected/disconnected/error), auto-dismiss (v1)
- PR2.6: **WithdrawDialog** — withdrawal/refund request dialog with reason selection and confirmation (v1)
- PR2.7: **RetailerConfig** — retailer/reseller configuration panel, retailer-specific settings display (v1)
- PR2.8: **VersionComparison** — installed vs latest version visual comparison component (v1)
- PR2.9: **HighlightedText** — search term highlighting within text strings (v1)
- PR2.10: **VerticalLoadBar** — vertical progress/load indicator for tunnel load display (v1)
- PR2.11: **AuthGate** — auth state wrapper component, alternative to LoginRequiredGuard for inline auth-gating (v1)

### PR3: Desktop Dual-Mode Layout (v1)

- PR3.1: Layout.tsx switches between SideNavigation (desktop, width >= 768px) and BottomNav (mobile, width < 768px) based on window width (v1)
- PR3.2: Layout store manages responsive state: `isDesktop`, `isSidebarCollapsed`, sidebar width (v1)
- PR3.3: SideNavigation items mirror BottomNav tabs plus sub-page quick links, with feature flag support (v1)
- PR3.4: Layout.tsx integrates global components: AnnouncementBanner, ServiceAlert, FeedbackButton (v1)
- PR3.5: Lazy loading with Suspense for tab pages (v1)
- PR3.6: Feature flag system in Layout for conditional tab/route rendering (v1)

### PR4: Existing Page Content Alignment (v1)

#### PR4.1: Purchase Page Completion

- Campaign code input with inline validation and error display (v1)
- Order preview with original amount, discount breakdown, final price (v1)
- MemberSelection component for family/group purchase target (v1)
- Invite reward banner showing referral bonus (from appConfig) (v1)
- Order creation payload with `forMyself`, `forUserUUIDs` fields (v1)

#### PR4.2: Account Page Completion

- Brand banner with clickable kaitu.io link (v1)
- Membership card with dynamic gradient by status (active/expired/none) (v1)
- Membership action buttons: Login (unauthenticated), Retry (error), Renew (expired) (v1)
- Email warning alert for members without email set (v1)
- Full menu items (13 total): Password, Devices, Member Management, Payment History, Wallet (external), Device Install, FAQ, Language, Theme, Version (dev mode), Developer Settings (conditional), Logout (v1)
- Theme selector: light/dark/system (v1)
- VersionItem with dev mode detection (5-tap activation) (v1)
- Refresh button on membership card for manual data reload (v1)

#### PR4.3: InviteHub Page Completion

- QR code generation with QRCode library (desktop only, hidden on mobile) (v1)
- Inline remark editing with collapse animation (v1)
- ExpirationSelectorPopover for share link expiration (24h, 7d, 30d, etc.) (v1)
- Action buttons: "Generate Complete" (full share), "Generate Link" (URL only), "Generate New Code" (v1)
- RetailerStatsOverview for retailer users, InviteRule for non-retailers (v1)

#### PR4.4: Devices Page Enhancement

- Inline remark editing with proper focus management (delayed focus utility) (v1)
- Saving state spinner during API calls (v1)
- Keyboard shortcuts: Enter to save, Escape to cancel (v1)
- Styled delete confirmation dialog (v1)

#### PR4.5: Issues + IssueDetail Enhancement

- Official badge styling on official responses (left border accent) (v1)
- Avatar placeholder with author initial letter (v1)
- Enhanced relative time formatting (m/h/d/w format) (v1)
- Error state with retry button on list page (v1)
- Rich comment metadata (author, time, official indicator) (v1)

#### PR4.6: SubmitTicket Enhancement

- Feedback mode detection via `?feedback=true` query param (v1)
- Auto log upload on page mount with generated feedback ID (v1)
- Character limit counters on subject (200) and content (5000) fields (v1)
- Inline field validation with error messages (v1)
- Success state card with celebratory styling after submission (v1)
- Helper "How it works" information card (v1)

#### PR4.7: Other Page Refinements

- **MemberManagement**: Align member add/delete/status UI with kaitu (v1)
- **ProHistory**: Align pagination, status badges, order detail display (v1)
- **MyInviteCodeList**: Align used/unused status, remark inline editing (v1)
- **UpdateLoginEmail**: Align two-step flow UI with kaitu (v1)
- **DeviceInstall**: Align platform cards with download links (v1)
- **FAQ**: Align help topic cards with styled icons (v1)
- **Changelog**: Align iframe embed with loading state (v1)
- **Discover**: Align iframe + postMessage auth bridge (v1)

### PR5: Supporting Infrastructure (v1)

#### PR5.1: New Hooks

- **useAppConfig** — fetch and cache app config (links, minClientVersion, announcement, rewards) with SWR pattern and 1-hour TTL (v1)
- **useEvaluation** — tunnel evaluation/quality scoring, integrates with evaluation.store (v1)
- **useAppLinks** — app external links from config (docs, social, support URLs) (v1)
- **useUpdater** — app update checking and OTA flow for desktop (v1)

#### PR5.2: New/Enhanced Stores

- **layout.store** — responsive layout state: `isDesktop`, `isSidebarCollapsed`, layout mode detection (v1)
- **evaluation.store** — tunnel evaluation state: status, evaluated tunnels, recommended domain, relay info (v1)
- **dashboard.store** — dashboard-specific state: advanced settings persistence, scroll position (v1)
- **Enhance ui.store** — add feature flag system with route-level controls (invite, discover, feedback, memberManagement, proHistory, deviceInstall, updateLoginEmail, developerSettings) (v1)
- **Enhance purchase.store** — campaign code validation errors, detailed order preview with discount breakdown (v1)

#### PR5.3: Utilities

- **tunnel.ts** — tunnel sorting/filtering helpers (v1)
- **tunnel-sort.ts** — advanced tunnel sorting algorithms (v1)
- **country.ts** — country/region lookup, flag emoji utilities (v1)
- **errorCode.ts** — error code to i18n key mapping (v1)
- **errorHandler.ts** — centralized error handling with user-facing messages (v1)
- **versionCompare.ts** — semantic version comparison (v1)
- **time.ts** — relative time formatting, countdown, timestamp utilities (v1)
- **user.ts** — user data helpers (membership checks, expiration) (v1)
- **ui.ts** — UI utilities (delayedFocus, scroll management) (v1)

### PR6: i18n Expansion (v1)

- Copy all locale files from kaitu: zh-CN, zh-TW, zh-HK, en-US, en-GB, en-AU, ja (v1)
- Add missing namespaces: developer, nav, retailer, startup, theme, ticket, wallet (v1)
- Merge new keys into existing namespaces (common, dashboard, auth, account, purchase, invite, feedback) (v1)

## Technical Decisions

- **Styling**: All new components use Tailwind CSS (k2app convention), NOT MUI. kaitu's MUI components are reimplemented in Tailwind during migration (v1)
- **Store pattern**: Zustand with async init pattern (matching existing k2app stores) (v1)
- **API layer**: All API calls go through existing `api/cloud.ts` + `vpn-client/` abstraction. No `window._k2` direct calls (v1)
- **Feature flags**: Expanded in `ui.store.getFeatureFlags()`, sourced from `appConfig` (v1)
- **Desktop detection**: CSS media query `min-width: 768px` for layout switching, `matchMedia` listener in layout.store (v1)
- **QR code**: `qrcode` npm package (same as kaitu) (v1)
- **i18n**: Direct copy of kaitu locale files, adapt keys where k2app namespace structure differs (v1)
- **Theme**: Defer light/dark/system toggle to PR4.2 scope. Dark-only remains default, theme selector is UI-ready but system-level theming is phase 2 (v1)

## Acceptance Criteria

### New Pages

- AC1: DeveloperSettings page renders VPN mode dropdown (tun/socks5/tproxy), log level dropdown, path type filter dropdown. Changes persist via daemon config API (v1)
- AC2: DeveloperSettings is feature-flagged — only accessible when dev mode activated (5-tap version) (v1)
- AC3: Tunnels page shows "coming soon" placeholder for self-deploy nodes section, and login prompt for cloud nodes section when unauthenticated (v1)
- AC4: ServiceError page renders error reason cards, solution suggestions, and FAQ link. Accessible via programmatic navigation from ServiceReadiness (v1)

### New Components

- AC5: SideNavigation renders on desktop (>=768px), BottomNav renders on mobile (<768px). Switching is seamless with no layout jump (v1)
- AC6: CloudTunnelList displays tunnel list with country, city, load percentage (VerticalLoadBar), and connection status badge. Selection triggers callback (v1)
- AC7: CollapsibleConnectionSection toggles between expanded/collapsed with CSS transition animation (v1)
- AC8: CompactConnectionButton renders same 3 states (stopped/connecting/connected) as ConnectionButton but at compact size (v1)
- AC9: ConnectionNotification displays toast on VPN state transition with auto-dismiss after 3s (v1)
- AC10: WithdrawDialog opens as modal, presents reason selection, submits withdrawal request via API (v1)
- AC11: RetailerConfig renders retailer settings panel with stats display, only visible to retailer-role users (v1)
- AC12: VersionComparison shows current version vs latest available with visual diff (v1)
- AC13: HighlightedText wraps matched substring in `<mark>` tag with accent color (v1)
- AC14: VerticalLoadBar renders 0-100% vertical progress indicator with color gradient (green→yellow→red) (v1)
- AC15: AuthGate renders children only when authenticated, shows login prompt otherwise (v1)

### Desktop Dual-Mode Layout

- AC16: Layout.tsx renders SideNavigation when `window.innerWidth >= 768` and BottomNav when `< 768`. Resize triggers re-evaluation (v1)
- AC17: SideNavigation menu items match BottomNav tabs and respect same feature flags (showInviteTab, etc.) (v1)
- AC18: layout.store tracks `isDesktop` boolean and fires `matchMedia` listener on mount (v1)
- AC19: Global components (AnnouncementBanner, ServiceAlert, FeedbackButton) render in Layout above page content (v1)
- AC20: Tab pages lazy-load with Suspense fallback (LoadingPage component) (v1)

### Purchase Page

- AC21: MemberSelection component appears when authenticated, allows selecting self + family members as purchase target (v1)
- AC22: Campaign code input shows validation error inline when code is invalid/expired (v1)
- AC23: Order preview displays original price, campaign discount, invite reward discount, final price (v1)
- AC24: Invite reward banner visible when appConfig.inviteReward > 0 (v1)

### Account Page

- AC25: Account page renders all 13 menu items matching kaitu layout (v1)
- AC26: Membership card shows action buttons: Login (when !isLoggedIn), Retry (on error), Renew (when expired) (v1)
- AC27: Email warning alert appears for authenticated members without email set (v1)
- AC28: Theme selector offers light/dark/system options (dark-only functional initially, UI-ready for future) (v1)
- AC29: VersionItem activates dev mode after 5 rapid taps, revealing DeveloperSettings menu item (v1)
- AC30: Membership card refresh button triggers user.store.refresh() (v1)

### InviteHub Page

- AC31: QR code renders on desktop using qrcode library, hidden on mobile (v1)
- AC32: Remark editing opens inline TextField with collapse animation, saves on blur/Enter, cancels on Escape (v1)
- AC33: ExpirationSelectorPopover opens on share button click, offers duration options (24h, 7d, 30d, 90d, 365d) (v1)
- AC34: Generate actions: "Generate Complete" copies full share text, "Generate Link" copies URL only (v1)

### Devices Page

- AC35: Remark editing uses delayed focus, shows saving spinner, handles Enter/Escape keys (v1)

### Issues Pages

- AC36: Official responses show accent-colored left border and official badge (v1)
- AC37: Issue list error state shows retry button (v1)
- AC38: Relative time formatting uses compact format (1m, 2h, 3d, 1w) (v1)

### SubmitTicket Page

- AC39: Auto log upload on mount when `?feedback=true` query param present (v1)
- AC40: Character counters display remaining characters for subject (200) and content (5000) (v1)
- AC41: Success state replaces form with celebratory card after successful submission (v1)

### Infrastructure

- AC42: useAppConfig hook returns cached config with 1-hour TTL, auto-refreshes on stale (v1)
- AC43: evaluation.store manages tunnel evaluation lifecycle: idle → running → complete/error (v1)
- AC44: layout.store.isDesktop reflects current viewport, updates on resize (v1)
- AC45: Feature flags from ui.store.getFeatureFlags() control route visibility in Layout and App.tsx (v1)
- AC46: All utility functions (tunnel, country, time, error, version) have unit tests (v1)

### i18n

- AC47: All 7 locales (zh-CN, zh-TW, zh-HK, en-US, en-GB, en-AU, ja) load correctly (v1)
- AC48: All 14+ namespaces resolve keys without fallback warnings in console (v1)

## Testing Strategy

- Unit tests for all new stores, hooks, and utilities (vitest) (v1)
- Component tests for new components using @testing-library/react (v1)
- Existing test suite (294 tests) must continue passing after all changes (v1)
- i18n: Verify all namespace/locale combinations load without missing key warnings (v1)
- Manual UAT: SideNavigation ↔ BottomNav responsive switching verified at 768px breakpoint (v1)

## Deployment & CI/CD

- No CI/CD changes needed — existing `ci.yml` covers webapp build + test (v1)
- `yarn build` must succeed with all new pages/components (v1)
- `npx tsc --noEmit` must pass with zero type errors (v1)

## Impact Analysis

- **Affected modules**: Layout.tsx (major rewrite), App.tsx (route additions), all existing pages (content alignment), stores (3 new + 2 enhanced), hooks (4 new), i18n (7 locales × 14+ namespaces)
- **Scope**: Large — touches ~40 files, adds ~20 new files
- **Migration approach**: Two-phase execution:
  - Phase 1: Architecture + stubs (routes, stores, layout, components as empty shells)
  - Phase 2: Page-by-page content implementation (one page per task)
- **Risk**: MUI → Tailwind translation may require design judgment calls. Reference kaitu screenshots for visual parity.
- **Dependencies**: `qrcode` npm package (new dependency for InviteHub QR code)
