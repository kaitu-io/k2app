# Feature: Tauri Desktop Bridge

**Status**: Implemented
**Combines**: `tauri-webview-cannot-fetch-external-https` + `tauri-desktop-missing-k2-globals-injection`

## Problem

Tauri v2 desktop app has two blocking issues that make the webapp completely non-functional:

1. **No globals injection**: `main.rs` doesn't inject `window._k2` or `window._platform`. Webapp falls back to `standalone-k2.ts` which uses relative fetch to `/core` — but neither Vite dev server (`:1420`) nor tauri-plugin-localhost (`:14580`) proxy to the daemon (`:1777`). VPN control is 100% broken.

2. **External HTTPS fetch blocked**: WebKit WKWebView rejects all `fetch()` calls to external HTTPS URLs (CloudFront, 52j.me) from the localhost HTTP origin. Cloud API is 100% broken — no server list, no user info, no auth.

**Net result**: Tauri desktop builds render the webapp shell but nothing works.

## Root Cause Analysis

### Daemon communication (issue 1)
- `standalone-k2.ts` fetches `/core` (relative path) → goes to Vite or localhost plugin → 404
- Even `http://127.0.0.1:1777/api/core` (absolute) would fail — different port = cross-origin, daemon has no CORS headers
- **Fix**: Route daemon calls through Tauri IPC → Rust → reqwest to `127.0.0.1:1777`

### External HTTPS (issue 2)
- Webapp origin is `http://localhost:1420` (dev) or `http://127.0.0.1:14580` (prod)
- WebKit enforces CORS for cross-origin HTTPS requests from HTTP origin
- Cloud API server sends CORS headers for private origins, but CloudFront CDN (antiblock) may not
- **Fix**: Use `@tauri-apps/plugin-http` — makes HTTP requests from Rust side, bypassing webview restrictions entirely

### Platform misidentification (secondary)
- Standalone fallback sets `os: 'web'`, `isDesktop: false` — wrong for Tauri desktop
- **Fix**: Inject correct platform metadata from Rust

## Solution

**Tauri-aware globals injection from webapp side**, detecting `window.__TAURI__` (already available via `withGlobalTauri: true`):

```
main.tsx detection:
  if (window.__TAURI__)  → import tauri-k2.ts → inject Tauri-specific _k2 + _platform
  else if no globals     → import standalone-k2.ts → inject web fallbacks (existing)
```

### _k2 (VPN control) — via Tauri IPC
```
window._k2.run('up', config)
  → @tauri-apps/api invoke('daemon_exec', {action, params})
  → Rust: reqwest POST http://127.0.0.1:1777/api/core
  → JSON response back to JS
```

### _platform — hybrid Tauri + web
```
window._platform = {
  os: detected from Rust (macos/windows/linux),
  isDesktop: true,
  isMobile: false,
  version: app version from Rust,
  storage: webSecureStorage (reuse existing — works fine in WebView),
  getUdid: invoke('get_udid') → Rust → daemon /api/device/udid,
  openExternal: invoke('open_external') → Rust → tauri::shell::open,
  updater: existing updater module (already wired via IPC),
  ...
}
```

### Cloud API — HTTP plugin fetch override
```
cloudApi.request() uses fetch()
  → window.fetch is overridden in Tauri to route external URLs through HTTP plugin
  → Local URLs (127.0.0.1, /) use native fetch
  → External HTTPS → @tauri-apps/plugin-http fetch → Rust reqwest → response
```

### Capability file
Production capability file with `http:default` (scoped) + `core:default` + existing plugin permissions.

## Acceptance Criteria

- **AC1**: Cloud API requests succeed from Tauri webview (server list loads, user info fetches)
- **AC2**: VPN control works (`_k2.run('up', config)` connects, `status` polls, `down` disconnects)
- **AC3**: Platform detected correctly (`os: 'macos'/'windows'`, `isDesktop: true`)
- **AC4**: Auth flow works (login, token storage, token refresh, logout)
- **AC5**: Standalone web fallback still works (no regression for router/web mode)
- **AC6**: Dev mode works (`yarn tauri dev` with Vite HMR)
- **AC7**: `getK2Source()` returns `'tauri'` when running in Tauri

## Files Affected

### Rust (desktop/src-tauri/)
- `Cargo.toml` — add `tauri-plugin-http`, `tauri-plugin-shell`
- `src/main.rs` — register plugins, add IPC commands
- `capabilities/default.json` — NEW: production capability file

### Webapp (webapp/)
- `package.json` — add `@tauri-apps/api`, `@tauri-apps/plugin-http`, `@tauri-apps/plugin-shell`
- `src/services/tauri-k2.ts` — NEW: Tauri-specific _k2 + _platform injection
- `src/main.tsx` — add `__TAURI__` detection before standalone fallback
- `src/services/standalone-k2.ts` — update `getK2Source()` to detect Tauri

## Non-Goals

- Rewriting cloudApi module (fetch override is transparent)
- Adding Tauri-specific storage plugin (webSecureStorage works fine in WKWebView)
- Mobile changes (Capacitor bridge already works)
- Vite dev proxy (not needed if IPC handles daemon communication)
