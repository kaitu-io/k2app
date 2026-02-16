# Plan: Mobile Debug Tool

## Meta

| Field | Value |
|-------|-------|
| Feature | mobile-debug |
| Spec | docs/features/mobile-debug.md |
| Date | 2026-02-16 |
| Complexity | simple |

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: debug.html loads without JS errors | build_produces_debug_html (build check) + manual_device_load | T1 |
| AC2: checkReady() logs result | manual_device_checkReady | T1 |
| AC3: getVersion() returns version info | manual_device_getVersion | T1 |
| AC4: getUDID() returns device ID | manual_device_getUDID | T1 |
| AC5: Connect triggers tunnel + events | manual_device_connect | T1 |
| AC6: Disconnect stops tunnel + events | manual_device_disconnect | T1 |
| AC7: getStatus() returns state + metadata | manual_device_getStatus | T1 |
| AC8: getConfig() returns stored wireUrl | manual_device_getConfig | T1 |
| AC9: vpnError displays in red | manual_device_vpnError | T1 |
| AC10: wireUrl persists via localStorage | manual_device_persistence | T1 |
| AC11: Preset selector populates input | manual_device_presets | T1 |
| AC12: Clear button empties log | manual_device_clear | T1 |
| AC13: Settings 5-tap opens debug page | test_settings_debug_tap_counter | T2 |
| AC14: Works in dev + production build | build_produces_debug_html + manual_dev_server_check | T1 |

> Note: AC2-AC12 are manual on-device verification — this tool IS the test harness for the native bridge. AC1/AC14 have automated build checks. AC13 has a unit test.

## Feature Tasks

### T1: Create debug.html + Vite multi-page config

**Scope**: Create the standalone debug page with all K2Plugin API buttons, event listeners, color-coded log area, wireUrl persistence, and preset selector. Configure Vite to build it as a second entry point.

**Files**:
- `webapp/debug.html` (create) — standalone HTML page with inline `<script>` and `<style>`
- `webapp/vite.config.ts` (modify) — add `build.rollupOptions.input` for multi-page

**Depends on**: none

**TDD**:
- RED: Verify build currently produces only `dist/index.html`:
  - `build_produces_only_index_html`: run `yarn build`, assert `dist/index.html` exists, `dist/debug.html` does NOT exist
- GREEN: Create `webapp/debug.html` with full implementation:
  - HTML structure: sticky header with wireUrl input + preset `<select>`, 3 button rows (info/status/action), log area with clear button
  - Inline `<script>`: K2Plugin access via `window.Capacitor.Plugins.K2Plugin`, all 7 methods wired to buttons, `vpnStateChange` + `vpnError` event listeners on DOMContentLoaded, `appendLog(type, label, data)` helper with timestamp + color coding + JSON pretty-print, localStorage read/write for `debug_wireUrl`
  - Inline `<style>`: mobile-friendly layout, sticky top, scrollable log, color classes for API/EVENT/ERROR
  - Modify `vite.config.ts`: add `build: { rollupOptions: { input: { main: resolve(__dirname, 'index.html'), debug: resolve(__dirname, 'debug.html') } } }`
- REFACTOR:
  - [SHOULD] Review log area scroll-to-bottom behavior (auto-scroll on new entries)

**Acceptance**:
- `yarn build` produces both `dist/index.html` and `dist/debug.html`
- `debug.html` contains no React/import dependencies — pure inline JS
- Opening `debug.html` in a browser shows the full UI (buttons disabled without Capacitor, but layout renders)
- AC1, AC2-AC12, AC14 validated via build check + manual device testing

**Knowledge**: docs/knowledge/architecture-decisions.md (Capacitor plugin loading convention)

---

### T2: Add Settings hidden entry point

**Scope**: Add a 5-tap counter on the version number in Settings page that navigates to `/debug.html`.

**Files**:
- `webapp/src/pages/Settings.tsx` (modify) — add click handler with counter + navigation

**Depends on**: [T1]

**TDD**:
- RED: Write failing test for Settings debug entry:
  - `test_settings_debug_tap_counter`: render Settings, click version text 4 times → no navigation, click 5th time → `window.location.href` set to `/debug.html`
  - `test_settings_tap_counter_resets`: render Settings, click 3 times, wait 3 seconds, click 3 more → no navigation (counter resets after timeout)
- GREEN: Add to Settings.tsx:
  - `useState(0)` for tap counter
  - `useRef` for timeout ID (reset counter after 2s of no taps)
  - onClick handler on version `<span>`: increment counter, if counter === 5 → `window.location.href = '/debug.html'`
  - Reset counter after 2s inactivity via setTimeout
- REFACTOR:
  - [SHOULD] Extract tap counter logic to a custom hook if reused elsewhere

**Acceptance**:
- Unit tests pass: `yarn test -- Settings`
- Tapping version number 5 times in rapid succession navigates to debug page
- Tapping slowly (>2s gaps) does not trigger navigation
- AC13 validated

## Manual Device Verification Checklist

After T1+T2 complete, run on a real iOS/Android device with gomobile build:

1. Open app → Settings → tap version 5 times → debug page loads (AC13)
2. Check Ready button → log shows `{ ready: true, version: "..." }` (AC2)
3. Get Version button → log shows version/go/os/arch (AC3)
4. Get UDID button → log shows non-empty string (AC4)
5. Enter wireUrl → reload page → wireUrl still in input (AC10)
6. Select preset → input populated (AC11)
7. Connect → log shows `[EVENT] vpnStateChange connecting` then `connected` (AC5)
8. Get Status → log shows state + connectedAt + uptimeSeconds (AC7)
9. Get Config → log shows wireUrl (AC8)
10. Disconnect → log shows `[EVENT] vpnStateChange stopped` (AC6)
11. Clear button → log area empty (AC12)
12. (If triggerable) vpnError → red log entry (AC9)
