# Plan: Kaitu Feature Migration

## Meta

| Field      | Value                                    |
|------------|------------------------------------------|
| Feature    | kaitu-feature-migration                  |
| Spec       | docs/features/kaitu-feature-migration.md |
| Date       | 2026-02-16                               |
| Complexity | complex                                  |

## Key Design Decisions

### Dark-Only Mode

**No light mode. No theme switcher.** The app ships exclusively in dark theme, matching kaitu webapp's dark palette. This simplifies:
- No `dark:` prefix on any Tailwind class — dark colors are the only colors
- No ThemeContext / theme store / theme persistence
- No theme selector in Account page
- CSS variables defined once in `:root`, not duplicated across light/dark

### UI/UX Source of Truth

All visual patterns replicate **kaitu webapp's dark mode** (`theme.ts` dark palette + `theme/colors.ts` dark object). MUI `sx` props → Tailwind classes. No design invention — pixel-match the old app's dark appearance.

---

## Design System: CSS Variables + Tailwind v4

All tokens defined in `webapp/src/app.css` as CSS custom properties. Tailwind v4 consumes them via `@theme`. Every component uses these tokens — no hardcoded hex values in component files.

### Color Tokens (from kaitu dark palette)

```css
/* webapp/src/app.css */
@import "tailwindcss";

@theme {
  /* === Base Surfaces === */
  --color-bg-default: #0F0F13;              /* page background (palette.background.default) */
  --color-bg-paper: #1A1A1D;               /* card/paper surface (palette.background.paper) */
  --color-bg-gradient: linear-gradient(135deg, #0a0e27 0%, #1a1f3a 50%, #2d1b4e 100%);

  /* === Text === */
  --color-text-primary: rgba(255, 255, 255, 0.95);
  --color-text-secondary: rgba(255, 255, 255, 0.7);
  --color-text-disabled: rgba(255, 255, 255, 0.5);

  /* === Primary (Blue) === */
  --color-primary: #42A5F5;
  --color-primary-light: #90CAF9;
  --color-primary-dark: #1976D2;

  /* === Secondary (Teal) === */
  --color-secondary: #26C6DA;
  --color-secondary-light: #4DD0E1;
  --color-secondary-dark: #0097A7;

  /* === Accent (Neon Cyan) === */
  --color-accent: #00ffff;
  --color-accent-light: #33ffff;
  --color-accent-dark: #00cccc;
  --color-accent-glow: rgba(0, 255, 255, 0.4);

  /* === Status: Success (Green) === */
  --color-success: #66bb6a;
  --color-success-light: #81c784;
  --color-success-dark: #4caf50;
  --color-success-glow: rgba(102, 187, 106, 0.3);
  --color-success-glow-strong: rgba(102, 187, 106, 0.5);
  --color-success-bg: rgba(102, 187, 106, 0.15);
  --color-success-bg-light: rgba(102, 187, 106, 0.08);
  --color-success-border: rgba(102, 187, 106, 0.3);
  --color-success-gradient: linear-gradient(135deg, #66bb6a 0%, #4caf50 100%);

  /* === Status: Warning (Orange) === */
  --color-warning: #ffa726;
  --color-warning-light: #ffb74d;
  --color-warning-dark: #ff9800;
  --color-warning-glow: rgba(255, 167, 38, 0.3);
  --color-warning-glow-strong: rgba(255, 167, 38, 0.5);
  --color-warning-bg: rgba(255, 167, 38, 0.15);
  --color-warning-bg-light: rgba(255, 167, 38, 0.08);
  --color-warning-border: rgba(255, 167, 38, 0.3);
  --color-warning-gradient: linear-gradient(135deg, #ffa726 0%, #ff9800 100%);

  /* === Status: Error (Red) === */
  --color-error: #ef5350;
  --color-error-light: #e57373;
  --color-error-dark: #f44336;
  --color-error-glow: rgba(239, 83, 80, 0.3);
  --color-error-glow-strong: rgba(239, 83, 80, 0.5);
  --color-error-bg: rgba(239, 83, 80, 0.15);
  --color-error-bg-light: rgba(239, 83, 80, 0.08);
  --color-error-border: rgba(239, 83, 80, 0.3);
  --color-error-gradient: linear-gradient(135deg, #ef5350 0%, #f44336 100%);

  /* === Status: Info (Blue) === */
  --color-info: #42a5f5;
  --color-info-light: #64b5f6;
  --color-info-dark: #2196f3;
  --color-info-glow: rgba(66, 165, 245, 0.3);
  --color-info-glow-strong: rgba(66, 165, 245, 0.5);
  --color-info-bg: rgba(66, 165, 245, 0.15);
  --color-info-bg-light: rgba(66, 165, 245, 0.1);
  --color-info-border: rgba(66, 165, 245, 0.3);
  --color-info-gradient: linear-gradient(135deg, #42a5f5 0%, #2196f3 100%);

  /* === Card/Surface === */
  --color-card-bg: rgba(20, 25, 45, 0.9);
  --color-card-bg-hover: rgba(30, 35, 55, 0.95);
  --color-card-border: rgba(255, 255, 255, 0.12);
  --color-card-border-light: rgba(100, 150, 255, 0.2);
  --color-card-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
  --color-card-shadow-hover: 0 8px 24px rgba(0, 0, 0, 0.4);

  /* === Selection === */
  --color-selected-bg: rgba(66, 165, 245, 0.15);
  --color-selected-bg-hover: rgba(66, 165, 245, 0.2);
  --color-selected-border: rgba(66, 165, 245, 0.4);
  --color-selected-shadow: 0 8px 24px rgba(33, 150, 243, 0.3);
  --color-selected-gradient: linear-gradient(135deg, rgba(66, 165, 245, 0.2) 0%, rgba(33, 150, 243, 0.15) 100%);

  /* === Disabled === */
  --color-disabled: #757575;
  --color-disabled-bg: rgba(255, 255, 255, 0.08);

  /* === Glass/Overlay === */
  --color-glass-bg: rgba(255, 255, 255, 0.05);
  --color-glass-border: rgba(255, 255, 255, 0.1);
  --color-overlay: linear-gradient(135deg, rgba(255,255,255,0.1) 0%, rgba(255,255,255,0) 100%);

  /* === Divider === */
  --color-divider: rgba(255, 255, 255, 0.12);

  /* === Highlight === */
  --color-highlight: rgba(144, 202, 249, 1);
  --color-highlight-bg: rgba(144, 202, 249, 0.16);

  /* === Membership === */
  --color-membership-premium: #FFB300;
  --color-membership-expired: #FF9800;
  --color-membership-trial: #66BB6A;
  --color-membership-regular: #26C6DA;

  /* === Semantic: Connection Status === */
  --color-status-connected: #66BB6A;
  --color-status-disconnected: #FAFAFA;
  --color-status-connecting: #FFB74D;
  --color-status-error: #EF5350;
}
```

### Typography

```
Font family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif
```

| Role | Tailwind Class | Size | Weight |
|------|---------------|------|--------|
| Page title | `text-lg font-bold` | ~18px | 700 |
| Section header | `text-base font-semibold` | 16px | 600 |
| Body text | `text-sm` | 14px | 400 |
| Secondary text | `text-sm text-[--color-text-secondary]` | 14px | 400 |
| Caption/label | `text-xs` | 12px | 400 |
| Small label | `text-[0.7rem]` | 11.2px | 400 |
| Monospace code | `font-mono text-2xl` | 24px | 400 |

### Spacing Scale (matching MUI 8px base)

| MUI `spacing()` | px | Tailwind |
|-----|-----|---------|
| 0.5 | 4px | `1` |
| 1 | 8px | `2` |
| 1.5 | 12px | `3` |
| 2 | 16px | `4` |
| 2.5 | 20px | `5` |
| 3 | 24px | `6` |
| 4 | 32px | `8` |

### Border Radius

| MUI | px | Tailwind |
|-----|-----|---------|
| `borderRadius: 1` | 4px | `rounded` |
| `borderRadius: 1.5` | 6px | `rounded-md` |
| `borderRadius: 2` | 8px | `rounded-lg` |
| `borderRadius: 3` | 12px | `rounded-xl` |

### Shadows

| Name | Value | Usage |
|------|-------|-------|
| card | `0 4px 12px rgba(0,0,0,0.3)` | Default card elevation |
| card-hover | `0 8px 24px rgba(0,0,0,0.4)` | Hovered card |
| selected | `0 8px 24px rgba(33,150,243,0.3)` | Selected/active card |
| glow-success | `0 20px 60px rgba(102,187,106,0.3)` | Connected VPN button |
| glow-warning | `0 20px 60px rgba(255,167,38,0.3)` | Connecting VPN button |
| glow-error | `0 20px 60px rgba(239,83,80,0.3)` | Error VPN button |

### Animations

Defined as `@keyframes` in `app.css`, referenced via Tailwind `animate-*` utilities:

| Name | Duration | Usage |
|------|----------|-------|
| `fadeInUp` | 0.3s | Page entrance, card reveal |
| `fadeIn` | 0.3s | Overlay appear |
| `scaleIn` | 0.3s | Dialog entrance |
| `slideInRight` | 0.3s | Sub-page entrance |
| `shimmer` | 1.5s infinite | Skeleton loading |
| `pulse` | 1.5s infinite | Status dot (connecting) |
| `marquee` | 10s linear infinite | Announcement scroll |

### Transition

Standard easing for all interactive elements:
```
transition-all duration-300 ease-[cubic-bezier(0.4,0,0.2,1)]
```

---

## Component UI Patterns

All components follow these patterns. Executors must match these exactly — no improvisation.

### Card

```html
<div class="rounded-xl border border-[--color-card-border] bg-[--color-card-bg]
            shadow-[--color-card-shadow] p-4 transition-all duration-300
            hover:bg-[--color-card-bg-hover] hover:shadow-[--color-card-shadow-hover]">
```
- No MUI elevation — flat design with border-based definition
- Gradient background for premium/status cards: `bg-gradient-to-br from-[status]-bg to-[status]-bg-light`

### Selected Card (Plan Selection, Active Item)

```html
<div class="rounded-xl border-2 border-[--color-selected-border]
            bg-[--color-selected-gradient] shadow-[--color-selected-shadow]
            cursor-pointer transition-all duration-300
            hover:translate-y-[-4px] hover:scale-[1.01]">
```

### Button — Primary (CTA)

```html
<button class="w-full rounded-lg bg-[--color-primary] text-white font-bold
               py-3.5 text-base transition-all duration-300
               hover:translate-y-[-2px] hover:shadow-[--color-selected-shadow]
               active:translate-y-0">
```
- No `uppercase` (MUI `textTransform: 'none'`)
- Status-colored buttons for connection/purchase: use status gradient as background

### Button — Outlined

```html
<button class="rounded-lg border border-[--color-card-border] bg-transparent
               text-[--color-text-primary] font-semibold py-2 px-4
               transition-all duration-300
               hover:border-[--color-primary] hover:bg-[--color-selected-bg]">
```

### Button — Small/Text

```html
<button class="text-sm text-[--color-primary] font-medium px-2 py-1
               hover:bg-[--color-selected-bg] rounded-md transition-all duration-300">
```

### Input Field

```html
<input class="w-full rounded-lg border border-[--color-card-border] bg-[--color-bg-paper]
              text-sm text-[--color-text-primary] placeholder:text-[--color-text-disabled]
              px-3 py-2.5 outline-none transition-colors duration-200
              focus:border-[--color-primary] focus:ring-1 focus:ring-[--color-primary]" />
```

### List Item (Settings/Menu)

```html
<div class="flex items-center justify-between py-3 px-4 cursor-pointer
            transition-all duration-200
            hover:bg-[rgba(255,255,255,0.04)]
            active:bg-[rgba(255,255,255,0.08)]">
  <div class="flex items-center gap-3">
    <Icon class="text-[--color-text-secondary] w-5 h-5" />
    <span class="text-sm text-[--color-text-primary]">Label</span>
  </div>
  <ChevronRight class="text-[--color-text-disabled] w-4 h-4" />
</div>
<div class="h-px bg-[--color-divider] mx-4" />  <!-- divider -->
```

### Status Chip

```html
<span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold
             bg-[--color-success-bg] text-[--color-success] border border-[--color-success-border]">
  Active
</span>
```
Variants: success (green), warning (orange), error (red), info (blue). Same pattern, different token set.

### Status Dot (Connection)

```html
<span class="inline-block w-2 h-2 rounded-full bg-[--color-status-connected] mr-1.5" />
<!-- For connecting state, add animate-pulse -->
<span class="inline-block w-2 h-2 rounded-full bg-[--color-status-connecting] animate-pulse mr-1.5" />
```

### Stats Card (Invite Stats, Membership Info)

```html
<div class="flex-1 rounded-lg bg-[--color-success-bg] border border-[--color-success-border] p-3">
  <div class="flex items-center gap-3">
    <div class="w-9 h-9 rounded-full bg-[--color-success] flex items-center justify-center">
      <Icon class="text-white w-4 h-4" />
    </div>
    <div>
      <div class="text-xs text-[--color-text-secondary]">Label</div>
      <div class="text-lg font-bold text-[--color-success]">42</div>
    </div>
  </div>
</div>
```

### Dialog/Modal

```html
<!-- Radix Dialog with kaitu styling -->
<DialogOverlay class="fixed inset-0 bg-black/60 animate-fadeIn z-50" />
<DialogContent class="fixed top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2
                      w-[calc(100%-32px)] max-w-md rounded-xl
                      bg-[--color-bg-paper] shadow-[--color-card-shadow-hover]
                      animate-scaleIn z-50">
  <!-- Header with gradient -->
  <div class="bg-[--color-bg-gradient] rounded-t-xl px-6 pt-6 pb-4 relative overflow-hidden">
    <DialogTitle class="text-white font-bold text-lg">Title</DialogTitle>
    <!-- Close button absolute top-right -->
    <button class="absolute top-3 right-3 text-[--color-text-secondary]">
      <X class="w-4 h-4" />
    </button>
  </div>
  <!-- Content -->
  <div class="px-6 pt-6 pb-4">...</div>
  <!-- Actions -->
  <div class="px-6 pb-6 pt-2 flex gap-3">
    <button class="flex-1 ...primary-btn">Confirm</button>
    <button class="flex-1 ...outlined-btn">Cancel</button>
  </div>
</DialogContent>
```

### Bottom Navigation

```html
<nav class="relative z-40 border-t border-[--color-divider] bg-[--color-bg-paper]
            pb-[env(safe-area-inset-bottom,0px)]">
  <div class="flex h-14">
    <a class="flex-1 flex flex-col items-center justify-center min-w-[60px] max-w-[100px]
              text-[--color-text-secondary] transition-colors duration-200">
      <Icon class="w-5 h-5" />
      <span class="text-[0.7rem] mt-1">Label</span>
    </a>
    <!-- Active tab -->
    <a class="flex-1 flex flex-col items-center justify-center min-w-[60px] max-w-[100px]
              text-[--color-primary]">
      <Icon class="w-5 h-5" />
      <span class="text-[0.75rem] font-semibold mt-1">Label</span>
    </a>
  </div>
</nav>
```

### Toast/Alert Notification

```html
<div class="fixed top-2 right-2 max-w-[280px] z-50 animate-slideInRight
            rounded-lg border border-[--color-success-border] bg-[--color-success-bg]
            px-2.5 py-1.5 flex items-center gap-1.5">
  <CheckCircle class="w-4 h-4 text-[--color-success]" />
  <span class="text-xs text-[--color-text-primary]">Message</span>
</div>
```

### Loading State

```html
<div class="flex flex-col items-center justify-center min-h-[200px] gap-4">
  <div class="w-10 h-10 border-4 border-[--color-primary]/30 border-t-[--color-primary]
              rounded-full animate-spin" />
  <span class="text-sm text-[--color-text-secondary]">Loading...</span>
</div>
```

### Empty State

```html
<div class="flex flex-col items-center justify-center min-h-[200px] gap-4 p-6">
  <Icon class="w-12 h-12 text-[--color-text-disabled]" />
  <div class="text-center space-y-1">
    <div class="text-base font-semibold text-[--color-text-primary]">Title</div>
    <div class="text-sm text-[--color-text-secondary]">Description</div>
  </div>
  <button class="...outlined-btn">Action</button>
</div>
```

### Back Button (Sub-pages)

```html
<button class="flex items-center gap-1 text-sm text-[--color-text-secondary]
               py-2 px-1 hover:text-[--color-text-primary] transition-colors">
  <ChevronLeft class="w-4 h-4" />
  <span>Back</span>
</button>
```

### Membership Status Card

```html
<!-- Uses gradient bg matching membership status color -->
<div class="rounded-xl border border-[--color-membership-premium]/20
            bg-gradient-to-br from-[--color-membership-premium]/15 to-[--color-membership-premium]/8
            p-4">
  <div class="flex items-center justify-between">
    <div class="flex items-center gap-2">
      <StatusChip />
      <span class="text-sm text-[--color-text-secondary]">Expires: 2026-12-31</span>
    </div>
    <button class="...small action button">Renew</button>
  </div>
</div>
```

### Announcement Banner

```html
<div class="bg-[--color-primary] text-white px-4 py-2 flex items-center gap-2 z-[1100]">
  <Info class="w-4 h-4 shrink-0" />
  <span class="text-sm font-medium flex-1 truncate">Message</span>
  <button class="shrink-0"><X class="w-4 h-4" /></button>
</div>
```

### Pagination

```html
<div class="flex items-center justify-center gap-2 py-4">
  <button class="w-8 h-8 rounded-lg ...disabled or active styles">
    <ChevronLeft class="w-4 h-4" />
  </button>
  <span class="text-sm text-[--color-text-secondary]">1 / 5</span>
  <button class="w-8 h-8 rounded-lg ...">
    <ChevronRight class="w-4 h-4" />
  </button>
</div>
```

### Page Structure Convention

Every tab page:
```html
<div class="w-full py-1 bg-transparent">
  <div class="space-y-5">   <!-- Stack spacing 2.5 = 20px -->
    <!-- Section 1: Card -->
    <!-- Section 2: List -->
    <!-- Section 3: ... -->
  </div>
</div>
```

Every sub-page:
```html
<div class="w-full">
  <BackButton />
  <div class="pt-2 space-y-4">
    <!-- Content -->
  </div>
</div>
```

### Global Styles on `<html>` / `<body>`

```css
html, body {
  background-color: var(--color-bg-default);
  color: var(--color-text-primary);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  -webkit-user-select: none;
  user-select: none;
  -webkit-touch-callout: none;
}
```

### Hover/Active States Convention

All interactive elements:
- **Hover**: `hover:translate-y-[-2px]` for cards/buttons, `hover:bg-[rgba(255,255,255,0.04)]` for list items
- **Active**: `active:translate-y-0` for buttons, `active:bg-[rgba(255,255,255,0.08)]` for list items
- **Transition**: `transition-all duration-300 ease-[cubic-bezier(0.4,0,0.2,1)]`

### Safe Area (Mobile)

```css
padding-top: env(safe-area-inset-top, 0px);
padding-bottom: env(safe-area-inset-bottom, 0px);
```
Applied on: Layout top wrapper, BottomNav bottom padding.

---

## Dependency Graph

```
F1 (API) ──────┐
               ├── F3 (Stores) ──┐
F2 (Platform) ─┤                 ├── F4 (Nav+Layout+Global) ──┬── T5 (Purchase)
               └─────────────────┘                            ├── T6 (Invite)
                                                              ├── T7 (Account)
                                                              ├── T8 (Dashboard)
                                                              ├── T9 (Device/Member/History)
                                                              ├── T10 (FAQ/Issues/Tickets)
                                                              └── T11 (Email/Install/Changelog/Discover)
```

F1 and F2 are parallel (no deps). F3 depends on F1. F4 depends on F2 + F3. T5–T11 are all parallel after F4.

## AC Mapping

| AC  | Requirement                                         | Test                                                    | Task |
|-----|-----------------------------------------------------|---------------------------------------------------------|------|
| AC1 | 4 tabs: Dashboard/Purchase/Invite/Account           | test_bottom_nav_renders_four_tabs                       | F4   |
| AC2 | Keep-alive tabs preserve state                      | test_keep_alive_preserves_tab_state                     | F4   |
| AC3 | Invite tab hidden when flag false                   | test_invite_tab_hidden_when_flag_false                  | F4   |
| AC4 | Non-tab pages show back button                      | test_sub_page_shows_back_button                         | F4   |
| AC5 | No /login route                                     | test_login_route_does_not_exist                         | F4   |
| AC6 | Auto LoginDialog on no session                      | test_login_dialog_opens_on_no_session                   | F4   |
| AC7 | LoginDialog two-step flow                           | test_login_dialog_email_then_code                       | F4   |
| AC8 | Login success closes dialog and refreshes           | test_login_success_closes_dialog                        | F4   |
| AC9 | LoginRequiredGuard opens dialog                     | test_login_required_guard_opens_dialog                  | F4   |
| AC10| Purchase loads plan list sorted by month             | test_purchase_loads_plans_sorted                        | T5   |
| AC11| Highlighted plan shows ribbon badge                 | test_highlighted_plan_shows_badge                       | T5   |
| AC12| Plan shows monthly and total price                  | test_plan_shows_price_formatting                        | T5   |
| AC13| Campaign code triggers preview                      | test_campaign_code_preview                              | T5   |
| AC14| Pay Now creates order and opens URL                 | test_pay_now_creates_order_opens_url                    | T5   |
| AC15| Payment result dialog with order/amount             | test_payment_result_dialog                              | T5   |
| AC16| Unauthenticated users see inline login              | test_unauthenticated_shows_inline_login                 | T5   |
| AC17| MemberSelection allows buying for members           | test_member_selection_buy_for_members                   | T5   |
| AC18| Invite shows latest code in monospace               | test_invite_shows_latest_code                           | T6   |
| AC19| Click code copies to clipboard with toast            | test_invite_code_copy_to_clipboard                      | T6   |
| AC20| Stats show registered and purchased counts          | test_invite_stats_display                               | T6   |
| AC21| QR code from share link (desktop)                   | test_invite_qr_code_desktop                             | T6   |
| AC22| Share opens expiration selector                     | test_share_opens_expiration_popover                     | T6   |
| AC23| Generate new code via API                           | test_generate_new_invite_code                           | T6   |
| AC24| Invite code remark editable inline                  | test_invite_code_remark_editable                        | T6   |
| AC25| Account membership status card with expiry          | test_account_membership_card                            | T7   |
| AC26| Logout stops VPN + calls API + clears session       | test_account_logout_flow                                | T7   |
| AC27| Language selector syncs to native + server           | test_language_selector_sync                             | T7   |
| AC28| ~~Theme selector~~ **Removed** — dark-only mode     | _(no test needed)_                                      | —    |
| AC29| Version display with dev mode activation            | test_version_dev_mode_activation                        | T7   |
| AC30| Sub-page links navigate correctly                   | test_account_sub_page_links                             | T7   |
| AC31| Devices list with current device highlighted         | test_devices_list_current_highlighted                   | T9   |
| AC32| Device remark editable, delete confirmation         | test_device_remark_edit_delete_confirm                  | T9   |
| AC33| Member add by email, delete confirmation            | test_member_add_delete                                  | T9   |
| AC34| Pro history paginated with type filter              | test_pro_history_paginated_filtered                     | T9   |
| AC35| Update email two-step flow                          | test_update_email_flow                                  | T11  |
| AC36| Device install QR + platform downloads              | test_device_install_qr_buttons                          | T11  |
| AC37| FAQ shows help cards + links                         | test_faq_help_cards                                     | T10  |
| AC38| Issues list with status labels + pagination         | test_issues_list_status_pagination                      | T10  |
| AC39| Issue detail with comments + reply                  | test_issue_detail_comments_reply                        | T10  |
| AC40| Submit ticket sends to API                          | test_submit_ticket_sends                                | T10  |
| AC41| ForceUpgradeDialog blocks on old version            | test_force_upgrade_blocks_app                           | F4   |
| AC42| AnnouncementBanner on app config                    | test_announcement_banner_display                        | F4   |
| AC43| ServiceAlert on daemon unreachable                  | test_service_alert_on_daemon_failure                    | F4   |
| AC44| ErrorBoundary catches + retry                       | test_error_boundary_catches_retry                       | F4   |
| AC45| AlertContainer shows toasts                         | test_alert_container_toast                              | F4   |
| AC46| FeedbackButton draggable + navigate                 | test_feedback_button_navigate                           | F4   |
| AC47| MembershipGuard redirects non-members               | test_membership_guard_redirect                          | F4   |
| AC48| openExternal on Tauri and Capacitor                 | test_open_external_tauri, test_open_external_capacitor  | F2   |
| AC49| writeClipboard on all platforms                     | test_write_clipboard_all_platforms                      | F2   |
| AC50| Dark theme renders correctly with design tokens     | test_dark_theme_css_variables                           | F4   |
| AC51| 20+ cloudApi endpoints callable + error handling    | test_cloud_api_endpoints                                | F1   |
| AC52| Auth token auto-included                            | test_auth_token_auto_included                           | F1   |
| AC53| Token refresh on 401                                | test_token_refresh_on_401                               | F1   |

## Foundation Tasks

### F1: API Layer + Types

**Scope**: Extend `cloudApi` with 20+ new endpoints and add all new TypeScript types. Pure data layer — no UI, no stores.
**Files**:
- `webapp/src/api/cloud.ts` [MODIFIED] — add all new endpoint functions
- `webapp/src/api/types.ts` [MODIFIED] — add Plan, Order, Device, Member, InviteCode, Issue, Comment, AppConfig, ProHistory types
- `webapp/src/api/__tests__/cloud-api-new-endpoints.test.ts` [NEW]
**Depends on**: none
**TDD**:
- RED: Write failing tests for every new endpoint function and shared auth/refresh behavior
  - Test functions: `test_cloud_api_endpoints` (parameterized for each of: `logout`, `getPlans`, `createOrder`, `previewOrder`, `getDevices`, `deleteDevice`, `updateDeviceRemark`, `updateLanguage`, `getProHistories`, `updateEmail`, `sendEmailCode`, `setPassword`, `getMembers`, `addMember`, `deleteMember`, `getLatestInviteCode`, `getInviteCodes`, `createInviteCode`, `updateInviteCodeRemark`, `createShareLink`, `getIssues`, `getIssueDetail`, `createIssue`, `addComment`, `getAppConfig`), `test_auth_token_auto_included`, `test_token_refresh_on_401`
- GREEN: Implement all endpoint functions in `cloud.ts`. Each function: construct URL via antiblock, attach auth header, call fetch, parse `ApiResponse<T>`, throw on error. Add types to `types.ts`.
- REFACTOR:
  - [MUST] Extract shared `authenticatedFetch(path, options)` helper that auto-injects token and handles 401 refresh — all F1 endpoint functions and downstream tasks depend on this
  - [SHOULD] Group endpoint functions by domain (auth, plans, devices, members, invite, issues) with section comments
**Acceptance**: AC51, AC52, AC53
**Knowledge**: docs/knowledge/architecture-decisions.md → "Antiblock Entry URL Resolution"

---

### F2: Platform Abstraction

**Scope**: Create `PlatformApi` interface with Tauri, Capacitor, and Web implementations. Factory function with auto-detection.
**Files**:
- `webapp/src/platform/types.ts` [NEW] — PlatformApi interface
- `webapp/src/platform/tauri.ts` [NEW] — TauriPlatform
- `webapp/src/platform/capacitor.ts` [NEW] — CapacitorPlatform
- `webapp/src/platform/web.ts` [NEW] — WebPlatform fallback
- `webapp/src/platform/index.ts` [NEW] — `createPlatform()` factory + `getPlatform()` singleton
- `webapp/src/platform/__tests__/platform.test.ts` [NEW]
**Depends on**: none
**TDD**:
- RED: Write failing tests for each platform method and factory detection
  - Test functions: `test_open_external_tauri`, `test_open_external_capacitor`, `test_open_external_web`, `test_write_clipboard_all_platforms`, `test_sync_locale_tauri`, `test_upload_logs_capacitor`, `test_create_platform_auto_detect`, `test_get_platform_singleton`
- GREEN: Implement PlatformApi interface, three implementations (mock Tauri/Capacitor globals in tests), and factory with `window.__TAURI__` / `window.Capacitor` detection.
- REFACTOR:
  - [MUST] Ensure `getPlatform()` returns singleton — all downstream components call this
  - [SHOULD] Add JSDoc to PlatformApi interface methods
**Acceptance**: AC48, AC49
**Knowledge**: docs/knowledge/architecture-decisions.md → "VpnClient Abstraction Pattern" (same DI pattern)

---

### F3: Core Stores + Hooks

**Scope**: Create new Zustand stores (user, purchase, invite, login-dialog, ui) and hooks. Modify auth.store for logout + LoginDialog integration. **No theme store** — dark-only mode eliminates theme state.
**Files**:
- `webapp/src/stores/user.store.ts` [NEW] — user profile, membership status, devices count
- `webapp/src/stores/purchase.store.ts` [NEW] — plans, selected plan, order, campaign
- `webapp/src/stores/invite.store.ts` [NEW] — invite codes, latest code, share links, retailer
- `webapp/src/stores/login-dialog.store.ts` [NEW] — `{ isOpen, trigger, message, open(), close() }`
- `webapp/src/stores/ui.store.ts` [NEW] — alerts queue, announcement, feature flags, app config (**no theme**)
- `webapp/src/stores/auth.store.ts` [MODIFIED] — add `logout()` (stop VPN + API call + clear), remove redirect to /login
- `webapp/src/hooks/useUser.ts` [NEW] — convenience hook for user + membership status
- `webapp/src/hooks/useShareLink.ts` [NEW] — share link generation with expiration
- `webapp/src/hooks/useInviteCodeActions.ts` [NEW] — invite code CRUD operations
- `webapp/src/stores/__tests__/user.store.test.ts` [NEW]
- `webapp/src/stores/__tests__/purchase.store.test.ts` [NEW]
- `webapp/src/stores/__tests__/invite.store.test.ts` [NEW]
- `webapp/src/stores/__tests__/login-dialog.store.test.ts` [NEW]
- `webapp/src/stores/__tests__/ui.store.test.ts` [NEW]
- `webapp/src/stores/__tests__/auth.store.logout.test.ts` [NEW]
**Depends on**: [F1]
**TDD**:
- RED: Write failing tests for every store action and hook
  - Test functions:
    - user.store: `test_user_store_init_loads_profile`, `test_user_store_membership_status`, `test_user_store_refresh`
    - purchase.store: `test_purchase_store_load_plans`, `test_purchase_store_select_plan`, `test_purchase_store_preview_order`, `test_purchase_store_create_order`, `test_purchase_store_campaign_code`
    - invite.store: `test_invite_store_load_latest`, `test_invite_store_generate_code`, `test_invite_store_update_remark`, `test_invite_store_load_all_codes`
    - login-dialog.store: `test_login_dialog_open_close`, `test_login_dialog_trigger_context`
    - ui.store: `test_ui_store_alerts_queue`, `test_ui_store_feature_flags`, `test_ui_store_load_app_config`
    - auth.store: `test_auth_logout_stops_vpn_clears_session`, `test_auth_logout_calls_api`
    - hooks: `test_use_user_returns_profile`, `test_use_share_link_generates`, `test_use_invite_code_actions`
- GREEN: Implement all stores following existing Zustand async init pattern (null → init() → loaded). Stores call cloudApi functions from F1. Hooks compose store selectors.
- REFACTOR:
  - [MUST] Ensure all stores follow `init()` action pattern consistent with existing vpn.store/auth.store
  - [MUST] auth.store logout must call `getVpnClient().disconnect()` before API logout
  - [SHOULD] Extract shared `createAsyncStore` helper if pattern repeats 3+ times
**Acceptance**: AC26 (logout via auth.store)

---

### F4: Navigation + Layout + Design Tokens + Global Components

**Scope**: The structural overhaul — `app.css` design tokens, App.tsx routing, Layout.tsx keep-alive, BottomNav 4 tabs, all global components (LoginDialog, guards, dialogs, alerts), i18n nav namespace, delete old pages. **No ThemeContext** — dark-only via CSS variables in app.css.
**Files**:
- `webapp/src/app.css` [MODIFIED] — add all CSS custom properties under `@theme`, keyframe animations, global body styles
- `webapp/src/App.tsx` [MODIFIED] — remove /login route, add 15+ new routes, wrap with ErrorBoundary + LoginDialog
- `webapp/src/main.tsx` [MODIFIED] — add ErrorBoundary wrapping (no ThemeProvider needed)
- `webapp/src/components/Layout.tsx` [MODIFIED] — keep-alive tab system (lazy-load + visibility:hidden), safe-area padding
- `webapp/src/components/BottomNav.tsx` [MODIFIED] — 4 tabs with feature flag filtering, dark theme styling, safe-area bottom
- `webapp/src/components/LoginDialog.tsx` [NEW] — global login modal with gradient header, two-step email→code
- `webapp/src/components/EmailLoginForm.tsx` [NEW] — shared email login form (used by LoginDialog + Purchase inline)
- `webapp/src/components/ForceUpgradeDialog.tsx` [NEW] — blocks app when version < minClientVersion
- `webapp/src/components/AnnouncementBanner.tsx` [NEW] — top banner from app config, primary bg, marquee animation
- `webapp/src/components/ServiceAlert.tsx` [NEW] — fixed alert for daemon errors, error-bg styling
- `webapp/src/components/AlertContainer.tsx` [NEW] — global toast/snackbar system, slideInRight animation
- `webapp/src/components/ErrorBoundary.tsx` [NEW] — React error boundary with retry
- `webapp/src/components/MembershipGuard.tsx` [NEW] — redirects non-members to /purchase
- `webapp/src/components/LoginRequiredGuard.tsx` [NEW] — opens LoginDialog for unauth
- `webapp/src/components/FeedbackButton.tsx` [NEW] — floating draggable button
- `webapp/src/components/BackButton.tsx` [NEW] — back navigation for sub-pages, text-secondary + chevron
- `webapp/src/components/LoadingAndEmpty.tsx` [NEW] — loading spinner / empty state / error state with design tokens
- `webapp/src/components/PasswordDialog.tsx` [NEW] — set/change password modal, gradient header dialog
- `webapp/src/i18n/locales/zh-CN/nav.json` [NEW]
- `webapp/src/i18n/locales/en-US/nav.json` [NEW]
- `webapp/src/pages/Login.tsx` [DELETE]
- `webapp/src/pages/Settings.tsx` [DELETE]
- `webapp/src/pages/Servers.tsx` [DELETE]
- `webapp/src/components/__tests__/LoginDialog.test.ts` [NEW]
- `webapp/src/components/__tests__/Layout.keepalive.test.ts` [NEW]
- `webapp/src/components/__tests__/BottomNav.test.ts` [NEW — replace existing if any]
- `webapp/src/components/__tests__/guards.test.ts` [NEW]
- `webapp/src/components/__tests__/global-components.test.ts` [NEW]
**Depends on**: [F2, F3]
**TDD**:
- RED: Write failing tests for navigation, keep-alive, login dialog, guards, and global components
  - Test functions:
    - Navigation: `test_bottom_nav_renders_four_tabs`, `test_keep_alive_preserves_tab_state`, `test_invite_tab_hidden_when_flag_false`, `test_sub_page_shows_back_button`, `test_login_route_does_not_exist`
    - LoginDialog: `test_login_dialog_opens_on_no_session`, `test_login_dialog_email_then_code`, `test_login_success_closes_dialog`
    - Guards: `test_login_required_guard_opens_dialog`, `test_membership_guard_redirect`
    - Global: `test_force_upgrade_blocks_app`, `test_announcement_banner_display`, `test_service_alert_on_daemon_failure`, `test_error_boundary_catches_retry`, `test_alert_container_toast`, `test_feedback_button_navigate`
    - Theme: `test_dark_theme_css_variables` (verify CSS custom properties are applied on body)
- GREEN: Implement all components using design token CSS variables — no hardcoded colors. Layout.tsx uses `useState` for mounted tabs + CSS `visibility:hidden` + `position:absolute` for inactive tabs. BottomNav reads feature flags from ui.store. LoginDialog uses login-dialog.store. Guards use auth.store + login-dialog.store. App.tsx defines all routes with lazy imports.
- REFACTOR:
  - [MUST] `app.css` must contain ALL design tokens — no colors in component files
  - [MUST] EmailLoginForm must be extracted as shared component — used by both LoginDialog (F4) and Purchase inline login (T5)
  - [MUST] Keep-alive tab IDs must match route paths exactly — Layout, BottomNav, and App.tsx must agree
  - [SHOULD] Consolidate guard patterns (LoginRequiredGuard and MembershipGuard share redirect logic)
  - [SHOULD] Group global component registrations in App.tsx with clear section comments
**Acceptance**: AC1–AC9, AC41–AC47, AC50
**Knowledge**: docs/knowledge/task-splitting.md → "Entry-Point Files Are Merge Conflict Hotspots", docs/knowledge/architecture-decisions.md → "VpnClient Abstraction Pattern"

**UI/UX Enforcement for F4**:
- BottomNav: 56px height, `bg-[--color-bg-paper]`, border-top `--color-divider`, active = `--color-primary` + font-semibold 0.75rem, inactive = `--color-text-secondary` + 0.7rem
- Layout body: `bg-[--color-bg-default]`, safe-area top/bottom padding
- LoginDialog: Radix Dialog, gradient header `--color-bg-gradient`, scaleIn animation, 16px margin from edges
- AlertContainer: slideInRight animation, max-w-280px, top-2 right-2
- All text: `-webkit-user-select: none` globally

---

## Feature Tasks

### T5: Purchase Page

**Scope**: Complete purchase flow — plan list, selection, campaign code, order creation, payment result, member selection, inline login for unauthenticated users.
**Files**:
- `webapp/src/pages/Purchase.tsx` [NEW]
- `webapp/src/components/MemberSelection.tsx` [NEW]
- `webapp/src/i18n/locales/zh-CN/purchase.json` [NEW]
- `webapp/src/i18n/locales/en-US/purchase.json` [NEW]
- `webapp/src/pages/__tests__/Purchase.test.ts` [NEW]
- `webapp/src/components/__tests__/MemberSelection.test.ts` [NEW]
**Depends on**: [F4]
**TDD**:
- RED: Write failing tests for purchase flow
  - Test functions: `test_purchase_loads_plans_sorted`, `test_highlighted_plan_shows_badge`, `test_plan_shows_price_formatting`, `test_campaign_code_preview`, `test_pay_now_creates_order_opens_url`, `test_payment_result_dialog`, `test_unauthenticated_shows_inline_login`, `test_member_selection_buy_for_members`
- GREEN: Implement Purchase page using design tokens. Plan cards as Selected Card pattern (border-2, selected-gradient, translate hover). Campaign code collapsible input. Payment via platform.openExternal(). MemberSelection: checkbox list of members.
- REFACTOR:
  - [SHOULD] Extract PlanCard as sub-component if render function exceeds 50 lines
  - [SHOULD] Extract PaymentResultDialog as separate component
**Acceptance**: AC10–AC17
**UI/UX**:
- Plan cards: Selected Card pattern with `--color-selected-border` active, `--color-card-border` inactive
- Highlighted plan: ribbon badge absolute positioned, `bg-[--color-primary]` rotated
- Price display: `text-2xl font-bold text-[--color-text-primary]` for total, `line-through text-[--color-text-disabled]` for original
- Save chip: `bg-[--color-success-bg] text-[--color-success] font-bold text-xs`
- CTA "Pay Now": full-width primary button with `--color-primary` bg, hover translate-y
- Inline login for unauth: EmailLoginForm component with card wrapper

---

### T6: Invite Page

**Scope**: Invite code display, copy, QR, share links, expiration selector, generate new code, remark editing, retailer mode, invite rules.
**Files**:
- `webapp/src/pages/InviteHub.tsx` [NEW]
- `webapp/src/components/InviteRule.tsx` [NEW]
- `webapp/src/components/RetailerStatsOverview.tsx` [NEW]
- `webapp/src/components/ExpirationSelectorPopover.tsx` [NEW]
- `webapp/src/i18n/locales/zh-CN/invite.json` [NEW]
- `webapp/src/i18n/locales/en-US/invite.json` [NEW]
- `webapp/src/pages/__tests__/InviteHub.test.ts` [NEW]
- `webapp/src/components/__tests__/ExpirationSelectorPopover.test.ts` [NEW]
**Depends on**: [F4]
**TDD**:
- RED: Write failing tests for invite features
  - Test functions: `test_invite_shows_latest_code`, `test_invite_code_copy_to_clipboard`, `test_invite_stats_display`, `test_invite_qr_code_desktop`, `test_share_opens_expiration_popover`, `test_generate_new_invite_code`, `test_invite_code_remark_editable`, `test_retailer_mode_shows_stats`, `test_non_retailer_shows_invite_rules`
- GREEN: Implement InviteHub with design tokens. Code display as monospace card. Stats as Stats Card pattern. QR conditional on desktop. ExpirationSelectorPopover via Radix Popover.
- REFACTOR:
  - [SHOULD] Extract InviteCodeCard sub-component for code display + copy + remark
  - [SHOULD] Memoize QR code generation with useMemo
**Acceptance**: AC18–AC24
**UI/UX**:
- Invite code display: `font-mono text-2xl tracking-wider text-[--color-accent]` in card with `bg-[--color-card-bg]`
- Copy action: click code → platform.writeClipboard() → success toast via AlertContainer
- Stats row: two Stats Card pattern side-by-side with `--color-success-*` (registered) and `--color-info-*` (purchased)
- QR: white bg `#fff` with `rounded-xl p-3` for contrast against dark card
- Share buttons: outlined buttons with icons, trigger Radix Popover
- Generate new code: outlined button `border-dashed border-[--color-primary]`
- Remark: inline editable text, `text-xs text-[--color-text-secondary]`, click to edit → input field

---

### T7: Account Page

**Scope**: Account page with brand banner, membership card, menu items linking to sub-pages, language selector, version display, logout. **No theme selector** — dark-only.
**Files**:
- `webapp/src/pages/Account.tsx` [NEW]
- `webapp/src/components/VersionItem.tsx` [NEW]
- `webapp/src/i18n/locales/zh-CN/account.json` [NEW]
- `webapp/src/i18n/locales/en-US/account.json` [NEW]
- `webapp/src/pages/__tests__/Account.test.ts` [NEW]
**Depends on**: [F4]
**TDD**:
- RED: Write failing tests for account page features
  - Test functions: `test_account_membership_card`, `test_account_logout_flow`, `test_language_selector_sync`, `test_version_dev_mode_activation`, `test_account_sub_page_links`, `test_account_brand_banner`, `test_account_password_dialog`
- GREEN: Implement Account page using List Item pattern for menu items, Membership Status Card pattern for membership info. Language selector uses Radix Select. VersionItem tracks click count → 5 rapid clicks activates dev mode.
- REFACTOR:
  - [SHOULD] Extract AccountSection component for repeated list-item pattern
  - [SHOULD] Group menu items by category (profile, membership, support, preferences)
**Acceptance**: AC25–AC27, AC29–AC30
**UI/UX**:
- Brand banner: card with `bg-[--color-bg-gradient]` full-width, brand logo + slogan, click → openExternal
- Membership card: Membership Status Card pattern, status-colored gradient bg, chip + expiry + action button
- Menu items: List Item pattern — icon (w-5 h-5 text-secondary) + label + chevron, divider between groups
- Menu groups: Profile (email, password), Membership (devices, members, history, wallet), Support (install, FAQ), Preferences (language), About (version)
- Language selector: Radix Select with country flag emoji + language name
- Logout: full-width error-styled button at bottom `bg-[--color-error] text-white font-bold rounded-lg py-3`
- Version: `text-xs text-[--color-text-disabled]` centered at bottom, tap-to-reveal dev mode

---

### T8: Dashboard Integration

**Scope**: Merge server selection from deleted Servers.tsx into Dashboard. Dashboard now shows VPN status + server selection panel + connect button.
**Files**:
- `webapp/src/pages/Dashboard.tsx` [MODIFIED]
- `webapp/src/pages/__tests__/Dashboard.integration.test.ts` [NEW]
**Depends on**: [F4]
**TDD**:
- RED: Write failing tests for integrated dashboard
  - Test functions: `test_dashboard_shows_server_list`, `test_dashboard_server_selection_connects`, `test_dashboard_vpn_status_display`, `test_dashboard_shows_selected_server_info`
- GREEN: Import ServerList component into Dashboard. Add collapsible server selection panel below connection button. Reuse existing ServerList + ConnectionButton. Selected server displayed prominently when connected.
- REFACTOR:
  - [SHOULD] Extract server selection into a DashboardServerPanel sub-component if Dashboard exceeds 100 lines
**Acceptance**: Dashboard server integration (implied by PR1 — server selection merges into Dashboard)
**UI/UX**:
- ConnectionButton: center of page, update CVA variants to use design token gradients (`--color-success-gradient` for connected, `--color-warning-gradient` for connecting, `--color-info-gradient` for stopped), glow shadows from status tokens
- Selected server: below button, card with server name + country flag + status dot
- Server list: collapsible panel, each item = List Item pattern with flag emoji + name + city + load bar
- Load bar: `h-1 rounded-full bg-[--color-success]` (green <70%), `bg-[--color-warning]` (70-90%), `bg-[--color-error]` (>90%)
- Connection notification: slideIn animation, positioned top-right, auto-dismiss 3s

---

### T9: Device / Member / History Sub-Pages

**Scope**: Three CRUD sub-pages that share similar patterns (list + actions).
**Files**:
- `webapp/src/pages/Devices.tsx` [NEW]
- `webapp/src/pages/MemberManagement.tsx` [NEW]
- `webapp/src/pages/ProHistory.tsx` [NEW]
- `webapp/src/components/Pagit.tsx` [NEW] — reusable pagination component
- `webapp/src/pages/__tests__/Devices.test.ts` [NEW]
- `webapp/src/pages/__tests__/MemberManagement.test.ts` [NEW]
- `webapp/src/pages/__tests__/ProHistory.test.ts` [NEW]
**Depends on**: [F4]
**TDD**:
- RED: Write failing tests for CRUD operations on each page
  - Test functions: `test_devices_list_current_highlighted`, `test_device_remark_edit_delete_confirm`, `test_member_add_delete`, `test_pro_history_paginated_filtered`, `test_pagination_component_navigation`, `test_devices_delete_confirmation_dialog`, `test_member_add_by_email`
- GREEN: Implement three pages following same pattern: BackButton + list + actions. Devices: current device via `vpnClient.getConfig()` deviceUdid match. Members: add form (email input). ProHistory: list with Pagit component.
- REFACTOR:
  - [SHOULD] Extract ConfirmDialog wrapper around Radix AlertDialog (used by Devices + Members)
  - [SHOULD] Extract EditableRemark sub-component (used by Devices + InviteHub)
**Acceptance**: AC31–AC34
**UI/UX**:
- Device item: card with device name + platform icon + remark (text-xs text-secondary), current device = `border-[--color-primary] bg-[--color-selected-bg]` + chip "Current"
- Delete action: Radix AlertDialog with gradient header, error-styled confirm button
- Member item: avatar (initials, `bg-[--color-primary] w-9 h-9 rounded-full`) + email + status chip (active=success, expired=warning, not activated=info)
- Add member: input field + outlined add button, inline within card
- ProHistory item: card with order number (monospace), date, amount, type chip (recharge=success, authorization=info), copy order number via clipboard
- Pagination: Pagit component centered, `text-[--color-text-secondary]`

---

### T10: FAQ / Issues / Tickets Sub-Pages

**Scope**: Help and support sub-pages — FAQ cards, issue list with pagination, issue detail with comments, submit ticket with log upload.
**Files**:
- `webapp/src/pages/FAQ.tsx` [NEW]
- `webapp/src/pages/Issues.tsx` [NEW]
- `webapp/src/pages/IssueDetail.tsx` [NEW]
- `webapp/src/pages/SubmitTicket.tsx` [NEW]
- `webapp/src/i18n/locales/zh-CN/feedback.json` [NEW]
- `webapp/src/i18n/locales/en-US/feedback.json` [NEW]
- `webapp/src/pages/__tests__/FAQ.test.ts` [NEW]
- `webapp/src/pages/__tests__/Issues.test.ts` [NEW]
- `webapp/src/pages/__tests__/IssueDetail.test.ts` [NEW]
- `webapp/src/pages/__tests__/SubmitTicket.test.ts` [NEW]
**Depends on**: [F4]
**TDD**:
- RED: Write failing tests for support pages
  - Test functions: `test_faq_help_cards`, `test_faq_links_to_issues_and_ticket`, `test_issues_list_status_pagination`, `test_issue_detail_comments_reply`, `test_submit_ticket_sends`, `test_submit_ticket_uploads_logs`
- GREEN: FAQ: static cards with icons + links. Issues: list from cloudApi.getIssues() with status labels + load more. IssueDetail: content + comments + add comment form. SubmitTicket: form with subject + content.
- REFACTOR:
  - [SHOULD] Extract IssueStatusLabel sub-component for status chip styling
  - [SHOULD] Extract CommentItem sub-component for reuse in IssueDetail
**Acceptance**: AC37–AC40
**UI/UX**:
- FAQ cards: icon (w-10 h-10 `text-[--color-primary]`) + title + description in Card pattern, `hover:translate-y-[-2px]`
- Issue item: title + status chip (open=`--color-success`, closed=`--color-text-disabled`) + comment count icon + relative time `text-xs text-[--color-text-secondary]`
- Issue detail: content in card, comments list below with avatar + author + time + content, divider between comments
- Add comment: textarea input field + submit button (primary, outlined)
- Submit ticket: subject input + multiline content textarea + submit primary button, loading spinner on submit
- Load more: outlined button centered `text-[--color-primary] border-[--color-primary]`

---

### T11: Email / Install / Changelog / Discover / InviteCodes Sub-Pages

**Scope**: Remaining sub-pages — update email flow, device install guide, changelog iframe, discover iframe, invite codes list.
**Files**:
- `webapp/src/pages/UpdateLoginEmail.tsx` [NEW]
- `webapp/src/pages/DeviceInstall.tsx` [NEW]
- `webapp/src/pages/Changelog.tsx` [NEW]
- `webapp/src/pages/Discover.tsx` [NEW]
- `webapp/src/pages/MyInviteCodeList.tsx` [NEW]
- `webapp/src/pages/__tests__/UpdateLoginEmail.test.ts` [NEW]
- `webapp/src/pages/__tests__/DeviceInstall.test.ts` [NEW]
- `webapp/src/pages/__tests__/Discover.test.ts` [NEW]
- `webapp/src/pages/__tests__/MyInviteCodeList.test.ts` [NEW]
**Depends on**: [F4]
**TDD**:
- RED: Write failing tests for each sub-page
  - Test functions: `test_update_email_flow`, `test_device_install_qr_buttons`, `test_changelog_iframe_loads`, `test_discover_iframe_external_links`, `test_discover_auth_broadcast`, `test_invite_codes_list_loads`, `test_invite_codes_remark_editable`
- GREEN: UpdateLoginEmail: two-step form guarded by MembershipGuard. DeviceInstall: platform cards + QR. Changelog: iframe. Discover: iframe + postMessage auth. MyInviteCodeList: list with remark editing.
- REFACTOR:
  - [SHOULD] Extract IframeEmbed component shared by Changelog and Discover
  - [SHOULD] Extract StepForm pattern shared by UpdateLoginEmail and LoginDialog
**Acceptance**: AC35, AC36
**UI/UX**:
- Update email: step indicator (1/2) with `text-[--color-primary]`, input fields per step, primary submit button
- Device install: platform cards (icon + platform name + download button), card per platform with `bg-[--color-card-bg]`, QR code in white-bg container
- Changelog/Discover iframe: full-height (`calc(100vh - 120px)`), `bg-[--color-bg-paper]` loading state, progress bar at top during load
- MyInviteCodeList: list of invite code cards, each with code (monospace accent), stats row, remark editable, divider between items

---

## New Dependencies

| Package | Version | Purpose | Task |
|---------|---------|---------|------|
| `qrcode` | ^1.5 | QR code generation (invite, device install) | T6, T11 |
| `@radix-ui/react-dialog` | ^1.1 | Accessible modal dialogs | F4 |
| `@radix-ui/react-popover` | ^1.1 | Expiration selector popover | T6 |
| `@radix-ui/react-select` | ^2.1 | Language selector | T7 |
| `@radix-ui/react-alert-dialog` | ^1.1 | Confirmation dialogs | T9 |

Tauri plugins (verify presence, add if missing):
- `@tauri-apps/plugin-shell` — openExternal
- `@tauri-apps/plugin-clipboard-manager` — writeClipboard

## Execution Summary

| Task | Scope | Files | Parallel Group |
|------|-------|-------|----------------|
| F1   | API Layer + Types | 3 | Group A (parallel with F2) |
| F2   | Platform Abstraction | 6 | Group A (parallel with F1) |
| F3   | Core Stores + Hooks | 16 | Group B (after F1) |
| F4   | Nav + Layout + Design Tokens + Global | 25 | Group C (after F2 + F3) |
| T5   | Purchase Page | 6 | Group D (all parallel, after F4) |
| T6   | Invite Page | 8 | Group D |
| T7   | Account Page | 5 | Group D |
| T8   | Dashboard Integration | 2 | Group D |
| T9   | Device/Member/History | 7 | Group D |
| T10  | FAQ/Issues/Tickets | 10 | Group D |
| T11  | Email/Install/Changelog/Discover | 9 | Group D |
| **Total** | | **~97 files** | |

Critical path: F1 → F3 → F4 → (T5–T11 parallel)
Shortest path with parallelism: F1‖F2 → F3 → F4 → T5‖T6‖T7‖T8‖T9‖T10‖T11
