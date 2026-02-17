# Feature: Control Types Alignment

## Meta

- Status: implemented
- Version: 2.0
- Created: 2026-02-17
- Updated: 2026-02-17
- Branch: w9f/control-types-alignment
- Depends: [webapp-stale-cleanup](./webapp-stale-cleanup.md) (broken UI removed first)

## Summary

`control-types.ts` was written for a Rust kaitu-service that doesn't exist. The file header references `rust/crates/kaitu-core/` — phantom sources. The Go daemon only has 5 actions (`up`, `down`, `status`, `get_config`, `version`), but the file defines ~25 actions. Many types are phantom (auth, storage, api_request) and correspond to nothing.

This spec aligns the webapp type system with the actual architecture (Go daemon only), removes the entire evaluation subsystem (daemon doesn't support `evaluate_tunnels`), deletes DeveloperSettings page (replaced by `debug.html`), and eliminates `get_config` action (fold into `status` response).

## Background — What Actually Exists

### Go daemon (`k2/daemon/api.go`) — current 5 actions

| Action | What it does |
|--------|-------------|
| `up` | Connect VPN with config (or reconnect with lastConfig) |
| `down` | Disconnect |
| `status` | Return connection state |
| `get_config` | Return lastConfig (echo of what `up` received) |
| `version` | Return version/go/os/arch |

Plus 3 non-action endpoints: `GET /ping`, `GET /metrics`, `GET /api/device/udid`

### Go daemon — target 4 actions (after this spec)

| Action | What it does |
|--------|-------------|
| `up` | Connect VPN with config |
| `down` | Disconnect |
| `status` | Return connection state **+ active config summary** |
| `version` | Return version/go/os/arch |

`get_config` eliminated — config info folded into `status` response.

### Webapp `_k2.run()` calls — current state (all broken calls marked)

| Call site | Action | Status |
|-----------|--------|--------|
| `Dashboard.tsx:370` | `start` | **Broken** — daemon has `up` |
| `Dashboard.tsx:366`, `Account.tsx:95` | `stop` | **Broken** — daemon has `down` |
| `vpn.store.ts:218` | `status` | OK |
| `Dashboard.tsx:88` | `get_config` | **Remove** — fold into status |
| `Dashboard.tsx:244` | `set_config` | **Broken** — 400 |
| `DeveloperSettings.tsx:110/120/134` | `set_config` | **Broken** — 400 (page being deleted) |
| `VersionItem.tsx:62` | `set_config` | **Broken** — 400 |
| `CloudTunnelList.tsx:104` | `set_config` | **Broken** — 400 |
| `useEvaluation.ts:90` | `evaluate_tunnels` | **Broken** — 400 |

## Changes

### 1. Fix control-types.ts file header

```
// Before:
// Canonical Rust sources:
// - rust/crates/kaitu-core/src/types.rs
// - rust/crates/kaitu-core/src/config.rs
// - rust/client/kaitu-control/src/actions.rs

// After:
// Type definitions for k2 daemon control protocol
// Canonical source: k2/daemon/api.go (Go daemon HTTP API)
```

### 2. Remove phantom type definitions from control-types.ts

**Auth types** (auth goes through cloudApi, not _k2.run):
- `RegisterDeviceParams`, `GetAuthCodeParams`, `LoginParams`
- `AuthStatusChangeData`, `GetAuthStatusParams`, `LogoutParams`

**Storage types** (storage is window._platform.storage):
- `StorageSetParams`, `StorageGetParams`, `StorageDeleteParams`

**API request types** (no daemon action):
- `ApiRequestParams`, `ApiRequestResponseData`

**Speedtest/fix_network types** (remnants after spec 1):
- `SpeedtestParams`, `SpeedtestResponseData`, `SpeedtestStatusType`
- `SpeedtestProgress`, `SpeedtestResult`, `SpeedtestStatusResponseData`
- `FixNetworkParams`

**Evaluation types** (daemon doesn't support, system being deleted):
- `TunnelInput`, `EvaluatedTunnelOutput`, `EvaluateTunnelsResponse`
- `EvaluateTunnelsParams`

**Metrics types** (daemon has GET /metrics endpoint, not action; zero imports outside this file):
- `ServiceMetrics`, `MemoryDump`, `ForceGCResult`

**Unused param/response wrappers**:
- `StartParams`, `StartResponseData`, `StopParams`, `StopResponseData`
- `StatusParams`, `GetConfigParams`, `SetConfigParams`
- `VersionParams`

**Config management removed**:
- `SetConfigParams` (no set_config action)
- `GetConfigParams` (get_config being eliminated)

### 3. Delete ActionParamsMap, ActionResponseMap, ControlAction

No code uses these for type-safe dispatch — `_k2.run()` accepts `action: string, params?: any`. Delete entirely.

### 4. Delete evaluation subsystem

`evaluate_tunnels` is called via `_k2.run()` but daemon doesn't support it — always returns 400.

**Delete files:**
- `webapp/src/hooks/useEvaluation.ts`
- `webapp/src/hooks/__tests__/useEvaluation.test.ts`
- `webapp/src/stores/evaluation.store.ts`
- `webapp/src/stores/__tests__/evaluation.store.test.ts`

**Clean references:**
- `webapp/src/components/CloudTunnelList.tsx` — remove `useEvaluation` + `useEvaluationStore` imports and usage
- `webapp/src/utils/tunnel-sort.ts` — review; if only used by evaluation, delete. If used elsewhere, keep but verify.
- `webapp/src/stores/index.ts` — remove evaluation store from `initializeAllStores()` if registered there

### 5. Delete DeveloperSettings page

Replaced by `debug.html` (already exists at `webapp/debug.html`).

**Delete files:**
- `webapp/src/pages/DeveloperSettings.tsx`

**Clean references:**
- `webapp/src/App.tsx` — remove route `<Route path="developer-settings" .../>` and import
- `webapp/src/pages/Account.tsx` — remove "Developer Settings" navigation entry
- `webapp/src/components/VersionItem.tsx` — remove 7-click easter egg that navigates to `/developer-settings` and the `set_config` call
- i18n `developer` namespace — check if only used by DeveloperSettings; if so, remove from all 7 locales

### 6. Remove all `set_config` calls

Daemon doesn't have `set_config`. All calls fail silently with 400.

| File | Call | Action |
|------|------|--------|
| `Dashboard.tsx:244` | Change proxy rule | Remove handler |
| `DeveloperSettings.tsx` | Multiple set_config calls | File being deleted (§5) |
| `VersionItem.tsx:62` | Set log level TRACE | Handled in §5 |
| `CloudTunnelList.tsx:104` | Set tunnel config | Remove set_config call |

### 7. Fix `start`/`stop` → `up`/`down`

Webapp action names don't match daemon.

- `Dashboard.tsx:370`: `'start'` → `'up'`
- `Dashboard.tsx:366`: `'stop'` → `'down'`
- `Account.tsx:95`: `'stop'` → `'down'`

### 8. Remove `get_config` calls, fold into `status`

**Webapp side:**
- `Dashboard.tsx:88` — remove `_k2.run('get_config')` call; read config from status response instead

**k2 daemon side (submodule change):**
- `k2/daemon/api.go` — remove `get_config` case from switch
- `k2/daemon/api.go` — `handleStatus()` response: include config summary fields (rule type, tunnel mode, VPN mode)
- Delete `handleGetConfig()` function

### 9. Clean kaitu-core.ts

Update IK2Vpn docstring to match final daemon actions:
```
 * Supported actions:
 * - up, down, status, version
```

Delete dead legacy types (lines 266-363) — none imported anywhere:
- `VPNState`, `VPNError`, `StatusResponseData`, `ConfigResponseData`
- `SetConfigParams`, `VersionResponseData`, `SimpleTunnel`
- `SpeedtestResponseData`, `SpeedtestStatusResponseData`, `LogSettingsResponseData`

### 10. Update documentation

**webapp/CLAUDE.md:**
- VPN Actions: `up`, `down`, `status`, `version`
- Remove `get_config`, `set_config`, `speedtest`, `fix_network`, `evaluate_tunnels` etc.
- Architecture diagram: remove phantom actions
- Stores list: remove evaluation store

**docs/contracts/webapp-daemon-api.md:**
- Remove `get_config` action section
- Remove `reconnect` action section (doesn't exist)
- Align remaining actions with daemon reality

**CLAUDE.md (root):**
- Stores list: remove evaluation

## What Stays in control-types.ts

After cleanup, the file is lean — only types with real imports:

| Type | Used by |
|------|---------|
| Error code constants (`ErrCode*`) | ServiceAlert, ConnectionNotification |
| `isNetworkError`, `isServerError`, `isVPNError`, `isAuthError` | ServiceAlert, ConnectionNotification |
| `getErrorI18nKey` | ConnectionNotification, CollapsibleConnectionSection |
| `ServiceState` | vpn.store |
| `ControlError` | ConnectionNotification, CollapsibleConnectionSection |
| `StatusResponseData` | vpn.store, polling |
| `ConfigResponseData` | Dashboard (from status response) |
| `RuleConfig`, `TunnelConfig`, `K2V4Config`, `TunnelMode`, `DNSMode` | Config UI |
| `LogConfig` | (review — may be unused after DeveloperSettings deleted) |
| `ComponentStatus`, `InitializationStatus` | AuthGate |

## Files Touched

| Category | Files | Action |
|----------|-------|--------|
| Types | `control-types.ts` | Remove ~20 phantom types, ActionParamsMap, fix header |
| Types | `kaitu-core.ts` | Fix docstring, delete 10 dead legacy types |
| Delete | `DeveloperSettings.tsx` | Entire page (debug.html replaces) |
| Delete | `useEvaluation.ts` + test | Evaluation hook |
| Delete | `evaluation.store.ts` + test | Evaluation store |
| Code | `App.tsx` | Remove developer-settings route |
| Code | `Account.tsx` | Remove developer-settings nav + `stop`→`down` |
| Code | `Dashboard.tsx` | `start`→`up`, `stop`→`down`, remove get_config/set_config |
| Code | `VersionItem.tsx` | Remove 7-click easter egg + set_config |
| Code | `CloudTunnelList.tsx` | Remove evaluation + set_config refs |
| Code | `stores/index.ts` | Remove evaluation store init |
| k2 | `k2/daemon/api.go` | Remove get_config, fold into status |
| Docs | `webapp/CLAUDE.md` | Update actions, stores, architecture |
| Docs | `webapp-daemon-api.md` | Align with daemon |
| Docs | Root `CLAUDE.md` | Update stores list |
| i18n | `developer` namespace (14 files) | Remove if only used by DeveloperSettings |
| **Total** | **~25 files** | |

## Acceptance Criteria

- [ ] AC1: `control-types.ts` header references Go daemon, not Rust
- [ ] AC2: No phantom auth/storage/api_request/evaluation types in control-types.ts
- [ ] AC3: `ActionParamsMap`, `ActionResponseMap`, `ControlAction` deleted
- [ ] AC4: No `set_config` calls anywhere in webapp
- [ ] AC5: No `get_config` calls anywhere in webapp
- [ ] AC6: Webapp uses `up`/`down` not `start`/`stop`
- [ ] AC7: DeveloperSettings page deleted, no route, no navigation
- [ ] AC8: Evaluation subsystem deleted (hook, store, tests, types)
- [ ] AC9: `kaitu-core.ts` has no dead types below line 265
- [ ] AC10: k2 daemon `status` response includes config summary
- [ ] AC11: k2 daemon `get_config` action removed
- [ ] AC12: `webapp/CLAUDE.md` matches reality
- [ ] AC13: `yarn tsc --noEmit` passes
- [ ] AC14: `yarn test` passes
- [ ] AC15: `cd k2 && go test ./daemon/...` passes

## Version History

- v1.0 (2026-02-17): Initial spec
- v2.0 (2026-02-17): Resolved open questions — delete evaluate_tunnels, get_config, DeveloperSettings; add k2 submodule changes
