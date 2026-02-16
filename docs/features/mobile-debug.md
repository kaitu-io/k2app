# Feature: Mobile Debug Tool

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | mobile-debug                             |
| Version   | v1                                       |
| Status    | draft                                    |
| Created   | 2026-02-16                               |
| Updated   | 2026-02-16                               |

## Version History

| Version | Date       | Summary                                              |
|---------|------------|------------------------------------------------------|
| v1      | 2026-02-16 | Initial: standalone debug page for mobile VPN bridge |

## Product Requirements

- PR1: Standalone debug page (`debug.html`) for testing k2 gomobile + native bridge correctness on mobile (v1)
- PR2: Pure HTML+JS, zero React/Store/Auth dependencies — isolates native layer from webapp layer (v1)
- PR3: Covers the complete VPN connection chain: ready → version → config → UDID → connect → status → events → disconnect (v1)
- PR4: Live scrolling log with color-coded entries for API calls, events, and errors (v1)
- PR5: wireUrl input with localStorage persistence and preset URL quick-select (v1)
- PR6: Accessible from Settings page via hidden entry (tap version number 5 times) (v1)
- PR7: Available in both dev and release builds (v1)

## Technical Decisions

- **Architecture**: Vite multi-page entry (`rollupOptions.input`) — `debug.html` alongside `index.html` in `webapp/`. Same build pipeline, zero Capacitor config changes. Capacitor bridge is WebView-level, available on any page within the same webDir. (v1)
- **API surface**: K2Plugin VPN methods only. Excludes update series (checkWebUpdate, checkNativeUpdate, applyWebUpdate, downloadNativeUpdate, installNativeUpdate) — those are an independent feature chain. (v1)
- **No framework**: debug.html uses vanilla JS + DOM manipulation. No React, no bundler transforms beyond Vite's HTML entry handling. This ensures the debug page works even if webapp bootstrap is broken. (v1)
- **Navigation model**: `window.location.href = '/debug.html'` from webapp. Full page navigation — React app unmounts, debug page loads fresh. Return via browser back or manual navigation. React re-initialization on return is acceptable for a debug tool. (v1)
- **Persistence**: wireUrl saved to localStorage key `debug_wireUrl`. Preset URLs defined as a JS array in debug.html. (v1)

## K2Plugin API Coverage

Methods exposed in debug UI (v1):

| Method | Purpose | UI Element |
|--------|---------|------------|
| `checkReady()` | Verify gomobile core loaded | Button → log result |
| `getVersion()` | Show gomobile build info (version, go, os, arch) | Button → log result |
| `getConfig()` | Read current wireUrl from native storage | Button → log result |
| `getUDID()` | Device identifier for server log correlation | Button → log result |
| `connect(wireUrl)` | Start VPN tunnel via K2Plugin | Button (uses wireUrl input) |
| `disconnect()` | Stop VPN tunnel | Button |
| `getStatus()` | Poll current VPN state + metadata | Button → log JSON |

Event listeners auto-registered on page load (v1):

| Event | Purpose |
|-------|---------|
| `vpnStateChange` | Real-time state transitions (stopped/connecting/connected) |
| `vpnError` | Error messages from native/gomobile layer |

## UI Design

### Layout (v1)

```
┌─────────────────────────────────────┐
│  K2 Mobile Debug                    │  ← sticky header
│                                     │
│  wireUrl: [________________] [▼]    │  ← input + preset select
│                                     │
│  [Ready] [Version] [UDID]          │  ← info buttons row
│  [Config] [Status]                  │  ← status buttons row
│  [Connect] [Disconnect]            │  ← action buttons row
│                                     │
│  Logs                    [Clear]    │  ← log header
├─────────────────────────────────────┤
│  12:03:45 [API] checkReady          │  ← scrollable log area
│  → { ready: true, version: "0.4.0" }│
│  12:03:47 [API] connect             │
│  → wireUrl: "vless://..."           │
│  12:03:48 [EVENT] vpnStateChange    │
│  → { state: "connecting" }          │
│  12:03:49 [EVENT] vpnStateChange    │
│  → { state: "connected" }           │
│  12:04:10 [ERROR] vpnError          │
│  → { message: "timeout" }           │
└─────────────────────────────────────┘
```

### Log color coding (v1)

- `[API]` — blue (#3b82f6) — active K2Plugin method calls
- `[EVENT]` — green (#22c55e) — passive event listener callbacks
- `[ERROR]` — red (#ef4444) — vpnError events and caught exceptions
- Response data — gray, JSON pretty-printed (2-space indent)

### Interaction details (v1)

- wireUrl input: `<input type="text">` with `<select>` for presets. Selecting a preset fills the input. Input change saves to localStorage.
- All buttons call K2Plugin directly via `window.Capacitor.Plugins.K2Plugin.methodName()`.
- Each call wraps in try/catch — success logs response, catch logs error in red.
- Event listeners registered once on `DOMContentLoaded`, auto-log all events.

## Acceptance Criteria

- AC1: `debug.html` loads in Capacitor WebView and displays all UI elements without JS errors (v1)
- AC2: `checkReady()` button returns `{ ready: true, version: "x.y.z" }` and logs to output area (v1)
- AC3: `getVersion()` returns valid version info with go/os/arch fields (v1)
- AC4: `getUDID()` returns a non-empty device identifier string (v1)
- AC5: Entering a valid wireUrl and clicking Connect triggers VPN tunnel start; `vpnStateChange` events appear in log showing `connecting` → `connected` transition (v1)
- AC6: Clicking Disconnect stops tunnel; `vpnStateChange` event shows `stopped` state (v1)
- AC7: `getStatus()` returns current VPN state with metadata (connectedAt, uptimeSeconds when connected) (v1)
- AC8: `getConfig()` returns the wireUrl stored in native preferences (v1)
- AC9: `vpnError` events display in red in the log area (v1)
- AC10: wireUrl persists across page reloads via localStorage (v1)
- AC11: Preset URL selector populates wireUrl input field (v1)
- AC12: Clear button empties the log area (v1)
- AC13: Debug page accessible from Settings via tapping version number 5 times (v1)
- AC14: Debug page works in both Vite dev server (livereload) and production build (v1)

## Testing Strategy

- **Manual testing on device**: Primary validation method — this is a debug tool for validating the native bridge, so its own testing is inherently manual/on-device. (v1)
- **Vite build verification**: `yarn build` succeeds and produces both `dist/index.html` and `dist/debug.html`. (v1)
- **No unit tests**: Pure DOM manipulation with no business logic worth unit-testing. The page itself IS the test tool. (v1)

## Deployment & CI/CD

- `debug.html` included in standard `yarn build` output via Vite multi-page config (v1)
- `cap sync` copies it to native projects automatically — no pipeline changes needed (v1)
- Available in all builds (dev + release). No build-time exclusion. (v1)

## Impact Analysis

- **Affected modules**: `webapp/vite.config.ts` (add multi-page input), `webapp/debug.html` (new file), `webapp/src/pages/Settings.tsx` (hidden entry point) (v1)
- **Scope**: Small — 1 new HTML file, 2 minor edits to existing files (v1)
- **Risk**: None — additive change, does not modify any existing runtime behavior (v1)

## File Manifest

| File | Action | Description |
|------|--------|-------------|
| `webapp/debug.html` | Create | Standalone debug page with inline JS |
| `webapp/vite.config.ts` | Modify | Add `rollupOptions.input` for multi-page |
| `webapp/src/pages/Settings.tsx` | Modify | Add hidden 5-tap entry to debug page |
