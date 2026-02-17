# Feature: Webapp Architecture v2 — Platform Separation

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | webapp-architecture-v2                   |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-17                               |
| Updated   | 2026-02-18                               |
| Depends on | config-driven-connect                   |

## Version History

| Version | Date       | Summary                                                        |
|---------|------------|----------------------------------------------------------------|
| v1      | 2026-02-17 | Initial: split window._k2 into VPN + platform, remove api proxy, unify UDID |

## Overview

`window._k2` currently mixes three unrelated concerns: VPN tunnel control, cloud API proxy, and platform capabilities. k2 has been refactored to be a pure VPN engine — it no longer handles cloud API, auth, or device management. The webapp's interface should reflect this reality.

**Current state**: `window._k2` = `{ core, api, platform, updater? }`. `_k2.api.exec('login')` routes login through the VPN daemon, but the daemon doesn't serve `/api/exec`. `_k2.platform.getUdid()` generates a browser-based UDID that differs from the daemon's hardware UDID. Three platforms produce three different UDID formats. The `updater` (Tauri app updater) sits under `_k2` despite having nothing to do with VPN.

**Target state**: Two independent globals — `window._k2` (pure VPN) and `window._platform` (native capabilities). Cloud API is a regular webapp module, not an injected global. All platforms produce the same 64-char hex UDID via SHA-256 normalization. `/api/device/udid` removed from k2 daemon.

## Context

- k2 daemon has exactly 4 real endpoints: `/ping`, `/metrics`, `POST /api/core` (up/down/status/get_config/version), `GET /*` (SPA). The 5th endpoint `/api/device/udid` is a legacy holdover that doesn't belong.
- webapp migration deleted the old `api/cloud.ts` (token refresh, 401 retry) and created `k2api.ts` wrapping `_k2.api.exec()` — but `_k2.api` currently has no working backend.
- UDID is used only at login (cloud API device binding). k2 VPN engine doesn't need it.
- Router (OpenWRT) mode is a special case where the daemon is the only native process — it must provide UDID and the browser provides webSecureStorage.

## Design

### 1. Global Interface Split

```
BEFORE:
window._k2 = {
  core: IK2Core,        // VPN control
  api: IK2Api,          // Cloud API proxy (broken, shouldn't exist)
  platform: IPlatform,  // Native capabilities (not k2's job)
  updater?: IUpdater,   // App updater (not k2's job)
}

AFTER:
window._k2 = {
  exec(action, params): Promise<SResponse>
  // Only: up, down, status, get_config, version
}

window._platform = {
  os, isDesktop, isMobile, version,
  getUdid(): Promise<string>,     // 64-hex SHA-256
  storage: ISecureStorage,
  updater?: IUpdater,             // Desktop app updater
  openExternal?, writeClipboard?, readClipboard?,
  syncLocale?, getLocale?, exit?,
  debug?, warn?, uploadServiceLogs?,
  nativeExec?, getPid?,
}

// Cloud API: webapp internal module (NOT a global)
// import { cloudApi } from '@/services/cloud-api'
```

### 2. Platform Injection Per Environment

| Environment | `window._k2` provider | `window._platform` provider |
|---|---|---|
| **Desktop (Tauri)** | Rust -> HTTP `POST /api/core` to daemon | Tauri commands: UDID from Rust (IOPlatformUUID/machine-id/MachineGuid), storage via tauri-plugin-store, updater via tauri-plugin-updater |
| **Mobile (Capacitor)** | K2Plugin -> gomobile engine | K2Plugin: UDID from native API (identifierForVendor/ANDROID_ID) + SHA-256, storage via Keychain/EncryptedPrefs |
| **Router (OpenWRT)** | HTTP `POST /api/core` to daemon | **Special exception**: UDID from daemon build layer (reads /etc/machine-id), storage from webSecureStorage (pure JS) |

### 3. UDID Unification

All platforms output **64-character lowercase hex** (SHA-256):

```
SHA-256(raw_platform_id + ":k2") -> 64 hex chars

Desktop (Tauri Rust):
  macOS:   SHA-256(IOPlatformUUID + ":k2")
  Windows: SHA-256(MachineGuid + ":k2")
  Linux:   SHA-256(/etc/machine-id + ":k2")

Mobile (K2Plugin native):
  iOS:     SHA-256(identifierForVendor + ":k2")
  Android: SHA-256(ANDROID_ID + ":k2")

Router (daemon build layer):
  Linux:   SHA-256(/etc/machine-id + ":k2")
```

The `:k2` salt and SHA-256 normalization ensure:
- Uniform 64-char output regardless of input format
- Raw hardware IDs never exposed to frontend
- Same algorithm across all platforms (deterministic, verifiable)

### 4. Cloud API Layer

New webapp module replacing `_k2.api`:

```
webapp/src/services/cloud-api/
  client.ts           // HTTP client: base URL resolution (antiblock), auth interceptor
  endpoints/          // Typed API methods: auth, user, tunnel, purchase, invite, ticket
  types.ts            // Request/response types
```

- **Token management**: Uses `_platform.storage` for encrypted token persistence
- **UDID**: Uses `_platform.getUdid()` at login time only
- **Auth interceptor**: Inject Bearer token, handle 401 -> refresh -> retry -> logout
- **Antiblock**: Entry URL resolution built into client (transparent to callers)
- **Platform-agnostic**: Same code on Desktop, Mobile, Router — only uses fetch() + _platform

### 5. k2 Daemon Cleanup

Remove from k2:
- `GET /api/device/udid` endpoint (daemon/api.go)
- Optionally keep `cloud.UDID()` internally if wire protocol uses it, but don't expose via HTTP

Daemon keeps only:
- `GET /ping`
- `GET /metrics`
- `POST /api/core` (up, down, status, get_config, version)
- `GET /*` (SPA serving)

Router build adds (via build tag or wrapper binary):
- `GET /api/platform/udid` — reads /etc/machine-id -> SHA-256 -> 64 hex

### 6. Desktop UDID in Rust (New)

New Tauri command replaces daemon's UDID endpoint:

```
// desktop/src-tauri/src/platform.rs
#[tauri::command]
pub fn get_device_udid() -> String
  macOS: ioreg IOPlatformUUID -> SHA-256
  Windows: Registry MachineGuid -> SHA-256
  Linux: /etc/machine-id -> SHA-256
```

Injected into `window._platform.getUdid` via Tauri invoke bridge.

## Changes Summary

| Layer | Action | Files |
|-------|--------|-------|
| **Types** | Replace IK2 with IK2Vpn + IPlatform (separate globals) | webapp/src/types/kaitu-core.ts |
| **Webapp bootstrap** | Inject _k2 and _platform separately; remove _k2.api usage | webapp/src/main.tsx, webapp/src/services/standalone-k2.ts |
| **Cloud API** | New module: HTTP client + typed endpoints + auth interceptor | webapp/src/services/cloud-api/ (new) |
| **Auth service** | Use _platform.storage and _platform.getUdid() | webapp/src/services/auth-service.ts |
| **Login components** | Use cloudApi.auth.login() instead of _k2.api.exec('login') | webapp/src/components/LoginDialog.tsx, EmailLoginForm.tsx |
| **All pages using _k2.api** | Migrate to cloudApi calls | All pages that call k2api().exec('api_request', ...) |
| **UDID cleanup** | Delete device-identity.ts, remove from web-platform.ts | webapp/src/utils/device-identity.ts (delete) |
| **Tauri platform** | New Rust module for UDID + platform injection | desktop/src-tauri/src/platform.rs (new) |
| **Mobile K2Plugin** | SHA-256 normalize UDID output | mobile/plugins/k2-plugin/ios/, android/ |
| **k2 daemon** | Remove /api/device/udid endpoint | k2/daemon/api.go |
| **Router build** | Add platform UDID endpoint via build layer | k2/daemon/ or wrapper binary |

## Acceptance Criteria

### AC1: window._k2 is pure VPN
- `window._k2.exec('status')` works
- `window._k2.api` does not exist
- `window._k2.platform` does not exist

### AC2: window._platform is independent
- `window._platform.getUdid()` returns 64-char hex on all platforms
- `window._platform.storage.get/set/remove` works
- `window._platform.updater` exists on desktop, undefined on mobile

### AC3: Cloud API works without _k2.api
- Login succeeds using cloudApi.auth.login() with UDID from _platform.getUdid()
- Token refresh on 401 works automatically
- All existing API-dependent pages work (Devices, Purchase, InviteHub, etc.)

### AC4: UDID format unified
- Desktop Tauri: 64-char hex (SHA-256 of hardware ID)
- iOS: 64-char hex (SHA-256 of identifierForVendor)
- Android: 64-char hex (SHA-256 of ANDROID_ID)
- Router: 64-char hex (SHA-256 of /etc/machine-id)
- device-identity.ts deleted
- /api/device/udid removed from k2 daemon

### AC5: Router exception works
- Router mode: _platform.getUdid() fetches from daemon build layer endpoint
- Router mode: _platform.storage uses webSecureStorage (pure JS)
- Router mode: login with cloud API succeeds

### AC6: No regressions
- yarn build succeeds in webapp
- yarn test passes in webapp
- VPN connect/disconnect works on desktop
- Login flow works end-to-end

## Migration Notes

This is a breaking change to the webapp's global interface. All consumers of window._k2 must be updated:

1. `_k2.core.exec(action)` -> `_k2.exec(action)` (flatten)
2. `_k2.api.exec(action)` -> `cloudApi.method()` (replace with typed calls)
3. `_k2.platform.*` -> `_platform.*` (rename global)
4. `_k2.updater` -> `_platform.updater` (move)

Tauri injection script and Capacitor K2Plugin both need corresponding updates.
