# Feature: Unified Debug Page

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | unified-debug-page                       |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-18                               |
| Updated   | 2026-02-18                               |

## Version History

| Version | Date       | Summary                                              |
|---------|------------|------------------------------------------------------|
| v1      | 2026-02-18 | Unified debug page replacing mobile-only debug.html (from TODO: desktop-debug-page) |

## Supersedes

- `mobile-debug` v1 — the current `debug.html` tests Capacitor K2Plugin directly. This spec replaces it with a platform-agnostic page testing at the `window._k2` / `window._platform` layer, which works on Tauri, Capacitor, and standalone.

## Product Requirements

- PR1: Single `debug.html` that works on all platforms: Tauri desktop, Capacitor mobile, standalone web (v1)
- PR2: Tests at the `window._k2.run()` / `window._platform` abstraction layer — not raw native APIs (v1)
- PR3: ClientConfig JSON editor with preset configs for connect testing (v1)
- PR4: Desktop-only features (updater, log upload, reinstall service, PID) shown conditionally when `window.__TAURI__` is present (v1)
- PR5: Live scrolling log with color-coded entries (API/EVENT/ERROR) — carried from mobile-debug v1 (v1)
- PR6: Hidden entry point via 5-tap version number in Account page (v1)
- PR7: Pure HTML+JS, zero React/Store dependencies — page works even when webapp bootstrap is broken (v1)
- PR8: Available in all builds (dev + release), all platforms (v1)

## Technical Decisions

### TD1: Abstraction Layer Testing

Test `window._k2.run(action, params)` and `window._platform.*` instead of raw K2Plugin or Tauri IPC. Benefits:
- One page works everywhere — same JS code tests the bridge layer that the real app uses
- Catches bridge bugs (transformStatus, key remapping) that raw native testing misses
- If you need raw native layer debugging, use Xcode/Android Studio native debuggers

### TD2: ClientConfig JSON Editor

Replace wireUrl input with a `<textarea>` for full ClientConfig JSON. Presets stored as JS objects in the page, serialized to textarea on select. The same ClientConfig format used by `_k2.run('up', config)` on all platforms.

Minimal preset structure:
```json
{
  "server": { "wireUrl": "vless://..." },
  "rule": { "global": true }
}
```

### TD3: Platform Detection for Conditional Sections

```javascript
var isTauri = !!window.__TAURI__;
var isCapacitor = !!(window.Capacitor && window.Capacitor.isNativePlatform && window.Capacitor.isNativePlatform());
```

Desktop-only section (`isTauri`): reinstallService, getPid, updater (check/apply), uploadLogs.
Platform label shown in header: "Tauri" / "Capacitor" / "Standalone".

### TD4: Vite Multi-Page Entry (unchanged)

`debug.html` remains as the existing entry in `rollupOptions.input`. No rename needed — the file name stays `debug.html`, content is rewritten.

### TD5: No Framework (unchanged from mobile-debug)

Vanilla HTML+JS+CSS. No React, no imports, no bundler transforms. This ensures the debug page works even if the webapp's module system or bootstrap is broken.

### TD6: Waiting for Globals

`debug.html` loads before React bootstrap. The globals `window._k2` and `window._platform` may not be injected yet. The page must either:
- Import and call the injection function (adds module dependency — defeats TD5), OR
- **Poll for globals** with a retry loop on DOMContentLoaded, showing "Waiting for _k2..." status

Chosen: **Poll approach**. Check `window._k2` every 200ms for up to 5s. If not found after timeout, offer a "Load Standalone Fallback" button that inlines minimal standalone injection. This preserves the zero-dependency guarantee.

## API Coverage

### VPN Control (all platforms)

| Action | Method | Input |
|--------|--------|-------|
| Status | `_k2.run('status')` | none |
| Connect | `_k2.run('up', config)` | ClientConfig JSON from textarea |
| Disconnect | `_k2.run('down')` | none |
| Version | `_k2.run('version')` | none |

### Platform Capabilities (all platforms)

| Capability | Method | Notes |
|-----------|--------|-------|
| OS / Version | `_platform.os`, `_platform.version` | Display in header |
| Get UDID | `_platform.getUdid()` | Button → log |
| Open External | `_platform.openExternal(url)` | Input + button |
| Write Clipboard | `_platform.writeClipboard(text)` | Input + button |
| Read Clipboard | `_platform.readClipboard()` | Button → log |
| Sync Locale | `_platform.syncLocale(locale)` | Select + button |

### Desktop Only (Tauri)

| Capability | Method | Notes |
|-----------|--------|-------|
| Get PID | `_platform.getPid()` | Button → log |
| Reinstall Service | `_platform.reinstallService()` | Button (confirm first) |
| Check Update | `_platform.updater.checkUpdateManual()` | Button → log |
| Upload Logs | `_platform.uploadLogs(params)` | Button with reason input |

## UI Design

### Layout (v1)

```
┌─────────────────────────────────────────┐
│  K2 Debug                [Tauri] 0.4.0  │  ← sticky header, platform badge
│                                         │
│  ── VPN Control ──────────────────────  │
│  [Status] [Version] [Disconnect]        │  ← action buttons
│                                         │
│  Config:                                │
│  ┌─────────────────────────────────┐    │
│  │ { "server": { "wireUrl": ... }  │    │  ← JSON textarea (resizable)
│  │   "rule": { "global": true }    │    │
│  └─────────────────────────────────┘    │
│  Preset: [▼ Select]     [Connect]       │  ← preset select + connect btn
│                                         │
│  ── Platform ─────────────────────────  │
│  [UDID] [Clipboard Write] [Clip Read]   │
│  [Open URL: ________] [Locale: ▼]      │
│                                         │
│  ── Desktop Only ─────────────────────  │  ← hidden when !__TAURI__
│  [PID] [Reinstall Service]              │
│  [Check Update] [Upload Logs: ___]      │
│                                         │
│  ── Logs ───────────────── [Clear] ──   │
│  12:03:45 [API] _k2.run('status')       │
│  → { state: "connected", running: true }│
│  12:03:47 [EVENT] vpnStateChange        │
│  → { state: "connecting" }              │
└─────────────────────────────────────────┘
```

### Styling

- Same dark theme as existing debug.html (`#1a1a2e` bg, `#e0e0e0` text)
- Same log color coding: `[API]` blue, `[EVENT]` green, `[ERROR]` red
- Platform badge color: Tauri = blue, Capacitor = green, Standalone = gray
- Mobile-friendly touch targets (min 44px height buttons)
- JSON textarea: monospace font, min-height 120px, resizable

## Acceptance Criteria

- AC1: `debug.html` loads and detects platform (Tauri/Capacitor/Standalone), displays correct badge (v1)
- AC2: `_k2.run('status')` button returns and displays normalized StatusResponseData (v1)
- AC3: `_k2.run('version')` button returns version info (v1)
- AC4: Entering valid ClientConfig JSON and clicking Connect calls `_k2.run('up', config)`, response logged (v1)
- AC5: `_k2.run('down')` button calls disconnect, response logged (v1)
- AC6: `_platform.getUdid()` returns non-empty string (v1)
- AC7: Clipboard write/read round-trips correctly (v1)
- AC8: Desktop-only section visible only when `window.__TAURI__` present (v1)
- AC9: `_platform.getPid()` returns numeric PID on desktop (v1)
- AC10: JSON textarea persists across page reloads via localStorage (v1)
- AC11: Preset selector populates textarea with valid ClientConfig JSON (v1)
- AC12: Clear button empties log area (v1)
- AC13: Page polls for `window._k2` on load, shows status while waiting (v1)
- AC14: Works in Vite dev server and production build on all platforms (v1)

## Testing Strategy

- **Manual testing on device/desktop**: Primary — this IS the test tool (v1)
- **Vite build verification**: `yarn build` produces `dist/debug.html` (v1)
- **No unit tests**: Pure DOM manipulation, no business logic. The page itself is the test tool. (v1)

## Deployment & CI/CD

- Replaces existing `debug.html` content in-place — no Vite config changes needed (v1)
- `cap sync` and Tauri build both pick it up automatically (v1)
- Available in all builds (dev + release) (v1)

## Impact Analysis

- **Affected files**: `webapp/debug.html` (rewrite content) (v1)
- **Scope**: Small — single file rewrite, no config changes (v1)
- **Risk**: None — replaces debug tool with better debug tool, no production runtime impact (v1)
- **Migration**: Old K2Plugin-specific debug page replaced. If raw K2Plugin testing needed, use Xcode/Android Studio debuggers. (v1)

## File Manifest

| File | Action | Description |
|------|--------|-------------|
| `webapp/debug.html` | Rewrite | Unified debug page with platform detection |
