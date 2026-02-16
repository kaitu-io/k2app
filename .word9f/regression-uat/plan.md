# UAT Regression Test Plan

## Meta

| Field        | Value                              |
|--------------|------------------------------------|
| Feature      | regression-uat                     |
| Platform     | Tauri Desktop (MCP Bridge)         |
| Driver       | tauri-mcp-server                   |
| Created      | 2026-02-16                         |
| Scope        | 4 features, ~63 ACs               |

## Prerequisites

### P1: Commit MCP Bridge changes
- [ ] Stage and commit: Cargo.toml, main.rs, build.rs, .gitignore, dev.sh
- [ ] Clean git baseline before UAT

### P2: Start Tauri app in dev mode
- [ ] `make dev` (k2 daemon + Vite HMR + Tauri window with `--features mcp-bridge`)
- [ ] Verify app window opens on port 14580
- [ ] Verify MCP Bridge WebSocket on port 9223

### P3: Connect MCP driver session
- [ ] `driver_session(action:"start")` → connected
- [ ] `driver_session(action:"status")` → verify
- [ ] `webview_dom_snapshot(type:"accessibility")` → root element visible

### P4: Determine API availability
- [ ] Check if k2 daemon is reachable (port 1777)
- [ ] Check if Cloud API is reachable (antiblock resolution)
- [ ] Result determines L1-only vs L1+L2 test depth

---

## Test Phases

### Phase 1: Core Navigation & Layout
**Source**: kaitu-feature-migration AC1-AC4
**Depth**: L1 Structure (no API needed)

| Scenario | AC | Steps | Assert |
|----------|----|-------|--------|
| S1: Bottom nav 4 tabs | AC1 | Wait for BottomNav → snapshot | 4 tab items: Dashboard, Purchase, Invite, Account |
| S2: Tab keep-alive | AC2 | Click Purchase tab → scroll → click Dashboard → click Purchase | Scroll position preserved (evaluate JS) |
| S3: Invite tab feature flag | AC3 | Snapshot BottomNav → check invite tab visibility | Tab visible/hidden per feature flag |
| S4: Sub-page back button | AC4 | Navigate to /faq → snapshot top-left | Back button element present |

### Phase 2: VPN State Display
**Source**: webapp-state-alignment (all 9 ACs)
**Depth**: L1 Structure + L2 Visual

| Scenario | AC | Steps | Assert |
|----------|----|-------|--------|
| S5: Stopped state button | AC-SA1 | Wait for ConnectionButton → snapshot | Blue button, "Connect" label |
| S6: No disconnecting variant | AC-SA4 | Snapshot ConnectionButton variants | No "disconnecting" or "error" CVA class |
| S7: Error display orthogonal | AC-SA6 | Evaluate `vpnStore.getState()` | `error` field exists, independent of `state` |
| S8: 3-state type check | AC-SA1,SA9 | Evaluate VpnState type usage | No 'disconnecting' or 'error' as state values |

### Phase 3: Login Flow
**Source**: kaitu-feature-migration AC5-AC9
**Depth**: L1 Structure (L2 if API available)

| Scenario | AC | Steps | Assert |
|----------|----|-------|--------|
| S9: No /login route | AC5 | Navigate to /login → snapshot | Redirects or shows 404, no dedicated login page |
| S10: LoginDialog renders | AC5,AC6 | Evaluate `loginDialogStore.open()` → wait for dialog | Dialog modal visible with email input |
| S11: LoginRequiredGuard | AC9 | Navigate to /invite (unauthenticated) → wait | LoginDialog opens (not redirect) |
| S12: Login two-step flow (L2) | AC7 | Enter email → click send code → code input appears | Two-step UI flow works |
| S13: Login success closes dialog (L2) | AC8 | Complete login → wait | Dialog closes, page refreshes |

### Phase 4: Purchase Page
**Source**: kaitu-feature-migration AC10-AC17
**Depth**: L1 Structure (L2 if API available)

| Scenario | AC | Steps | Assert |
|----------|----|-------|--------|
| S14: Purchase page loads | AC10 | Click Purchase tab → wait for page | Plan cards rendered (or loading state) |
| S15: Plan card structure | AC11,AC12 | Snapshot plan cards | Ribbon badge on highlighted, price elements |
| S16: Campaign code input | AC13 | Find campaign code section → snapshot | Collapsible text field present |
| S17: Unauthenticated view | AC16 | (Logged out) Navigate to /purchase → snapshot | Inline login form (EmailLoginForm) visible |
| S18: Member selection | AC17 | (Logged in) Navigate to /purchase → snapshot | MemberSelection component present |

### Phase 5: Invite Page
**Source**: kaitu-feature-migration AC18-AC24
**Depth**: L1 Structure + L2 Visual

| Scenario | AC | Steps | Assert |
|----------|----|-------|--------|
| S19: Invite code display | AC18 | Navigate to /invite (authenticated) → snapshot | Large monospace code element |
| S20: Copy invite code | AC19 | Click invite code → wait for toast | Clipboard write + success toast |
| S21: Stats row | AC20 | Snapshot invite page stats section | Registered count + purchased count elements |
| S22: QR code desktop | AC21 | Snapshot QR section | QR code canvas/image rendered |
| S23: Share expiration popover | AC22 | Click share button → wait for popover | Expiration selector popover visible |
| S24: Generate new code (L2) | AC23 | Click "Generate New Code" → wait | New code created via API |
| S25: Edit remark inline | AC24 | Click remark → type text → blur | Remark updated inline |

### Phase 6: Account Page
**Source**: kaitu-feature-migration AC25-AC30
**Depth**: L1 Structure + L2 Visual

| Scenario | AC | Steps | Assert |
|----------|----|-------|--------|
| S26: Membership status card | AC25 | Navigate to /account → snapshot | Status chip + expiry date elements |
| S27: Logout flow (L2) | AC26 | Click logout → confirm → wait | VPN stopped, session cleared, LoginDialog appears |
| S28: Language selector | AC27 | Find language selector → snapshot | Language options with flags |
| S29: Version + dev mode | AC29 | Find version text → click 5 times rapidly | Version displayed, dev mode activates |
| S30: Sub-page links | AC30 | Snapshot all navigation links | Links to /devices, /member-management, /pro-histories, etc. |

### Phase 7: Account Sub-Pages
**Source**: kaitu-feature-migration AC31-AC40
**Depth**: L1 Structure

| Scenario | AC | Steps | Assert |
|----------|----|-------|--------|
| S31: Devices page | AC31 | Navigate to /devices → snapshot | Device list with current device badge |
| S32: Device remark/delete | AC32 | Find device item → snapshot actions | Edit remark + delete button |
| S33: Member management | AC33 | Navigate to /member-management → snapshot | Add by email + member list |
| S34: Pro history | AC34 | Navigate to /pro-histories → snapshot | Paginated list, type filter |
| S35: Update email | AC35 | Navigate to /update-email → snapshot | Email input + send code flow |
| S36: Device install | AC36 | Navigate to /device-install → snapshot | QR code + platform download buttons |
| S37: FAQ page | AC37 | Navigate to /faq → snapshot | Help cards with links |
| S38: Issues list | AC38 | Navigate to /issues → snapshot | Issue items with status labels |
| S39: Issue detail | AC39 | Navigate to /issues/1 → snapshot | Comments + reply form |
| S40: Submit ticket | AC40 | Navigate to /submit-ticket → snapshot | Title + content form |

### Phase 8: Global Components
**Source**: kaitu-feature-migration AC41-AC47
**Depth**: L1 Structure

| Scenario | AC | Steps | Assert |
|----------|----|-------|--------|
| S41: ForceUpgradeDialog | AC41 | Evaluate `uiStore` inject minClientVersion | Dialog blocks app when version < min |
| S42: AnnouncementBanner | AC42 | Evaluate `uiStore` inject announcement | Banner displays at top |
| S43: ServiceAlert | AC43 | (daemon unreachable) → wait for alert | Fixed-top alert visible |
| S44: ErrorBoundary | AC44 | Evaluate: trigger render error → snapshot | Error UI with retry button |
| S45: AlertContainer toast | AC45 | Evaluate: trigger alert → wait for toast | Toast notification visible |
| S46: FeedbackButton | AC46 | Wait for floating button → snapshot | Draggable button present |
| S47: MembershipGuard | AC47 | Navigate to /update-email (non-member) → wait | Redirect to Purchase page |

### Phase 9: Theme & Platform
**Source**: kaitu-feature-migration AC48-AC50
**Depth**: L1 Structure + L2 Visual

| Scenario | AC | Steps | Assert |
|----------|----|-------|--------|
| S48: External link opens browser | AC48 | Trigger openExternal() → check IPC | Tauri shell open called |
| S49: Clipboard write | AC49 | Trigger writeClipboard() → check | Clipboard API called |
| S50: Dark theme consistency | AC50 | Get computed styles on 10 elements | All use CSS variables, no hardcoded colors |
| S51: Dark theme visual | AC50 | Screenshot full page | Dark background, correct palette |

### Phase 10: Config-Driven Connect
**Source**: config-driven-connect AC1-AC3
**Depth**: L1 Structure + L3 State

| Scenario | AC | Steps | Assert |
|----------|----|-------|--------|
| S52: ClientConfig type exists | AC1 | Evaluate: check ClientConfig in webapp | Type has server, mode, proxy, dns, rule, log |
| S53: Config assembly | AC2 | Evaluate: inspect buildConfig function | Assembles from wireUrl + preferences |
| S54: Rule mode in config | AC3 | Evaluate: check rule mode preference → config | rule.global field set from user pref |

---

## Execution Strategy

### Batch Plan

| Batch | Scenarios | Dependency | Estimated Time |
|-------|-----------|------------|----------------|
| B1    | S1-S4 (Navigation) | None | 2 min |
| B2    | S5-S8 (VPN State) | None | 2 min |
| B3    | S9-S13 (Login) | B1 | 3 min |
| B4    | S14-S18 (Purchase) | B1 | 3 min |
| B5    | S19-S25 (Invite) | B3 (needs auth) | 3 min |
| B6    | S26-S30 (Account) | B1 | 3 min |
| B7    | S31-S40 (Sub-pages) | B6 | 5 min |
| B8    | S41-S47 (Global) | B1 | 4 min |
| B9    | S48-S51 (Theme) | B1 | 2 min |
| B10   | S52-S54 (Config) | B2 | 2 min |

### Total: 54 scenarios, ~29 min estimated

### API-Dependent Scenarios (L2)

These require k2 daemon + Cloud API + valid credentials:
- S12, S13 (Login two-step + success)
- S24 (Generate invite code)
- S27 (Logout flow)

If API unavailable: mark as SKIPPED, test L1 structure only.

---

## Evidence Output

```
.word9f/regression-uat/
  uat-scenarios.md          Generated from this plan (Phase 1: PREPARE)
  uat-evidence/
    S{n}-step{m}.jpg        L2 screenshots (max 20)
    S{n}-failure/            Failure bundles (screenshot + DOM + logs)
  uat-report.md             Final report with pass/fail/skip
```

## Checklist Before Execution

- [ ] P1: MCP bridge changes committed
- [ ] P2: `make dev` running (app window visible)
- [ ] P3: `driver_session(action:"start")` connected
- [ ] P4: API availability determined → set L1-only or L1+L2 depth
- [ ] P5: Unit tests passing (`cd webapp && yarn test`)
