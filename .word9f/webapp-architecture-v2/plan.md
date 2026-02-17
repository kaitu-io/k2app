# Plan: Webapp Architecture v2 — Platform Separation

## Meta

| Field | Value |
|-------|-------|
| Feature | webapp-architecture-v2 |
| Spec | docs/features/webapp-architecture-v2.md |
| Date | 2026-02-17 |
| Complexity | complex |

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: window._k2 is pure VPN | test_k2_run_works, test_k2_has_no_api, test_k2_has_no_platform | F1 |
| AC2: window._platform is independent | test_platform_getUdid_returns_64hex, test_platform_storage_works, test_platform_updater_desktop_only | F1, F2 |
| AC3: Cloud API works without _k2.api | test_login_via_cloudApi, test_token_refresh_on_401, test_api_pages_work | F2, T3 |
| AC4: UDID format unified | test_desktop_udid_64hex, test_standalone_udid_from_daemon, test_device_identity_deleted | F1, F2 |
| AC5: Router exception works | test_router_udid_from_daemon, test_router_storage_webSecureStorage, test_router_login_succeeds | F2 |
| AC6: No regressions | test_yarn_build, test_yarn_test, test_vpn_connect_disconnect | T4 |

## Foundation Tasks

### F1: Type System + Global Interface Split

**Scope**: Rewrite the type definitions to split IK2 into IK2Vpn (pure VPN) + IPlatform (independent global). Update the global Window declaration. This is the foundation everything else depends on.

**Files**:
- webapp/src/types/kaitu-core.ts (rewrite IK2 -> IK2Vpn, keep IPlatform, add Window._platform)
- webapp/src/services/standalone-k2.ts (split injection: _k2 = pure VPN run method, _platform = standalone platform)
- webapp/src/main.tsx (update bootstrap: check _k2 and _platform separately)
- webapp/src/services/web-platform.ts (remove getWebUdid, getWebFingerprint; keep webPlatform object)
- webapp/src/utils/device-identity.ts (DELETE)
- webapp/src/services/__tests__/standalone-k2.test.ts (new: test split injection)
- webapp/src/services/__tests__/web-platform.test.ts (update: remove UDID tests)

**Depends on**: none

**TDD**:
- RED: Write failing tests for new interface contracts
  - test_IK2Vpn_has_only_run_method: IK2Vpn has run() only, no api, no platform
  - test_Window_has_k2_and_platform: Window type declares both _k2: IK2Vpn and _platform: IPlatform
  - test_standalone_injects_both_globals: ensureK2Injected() sets both window._k2 and window._platform
  - test_standalone_k2_has_no_api: window._k2.api is undefined after standalone injection
  - test_standalone_platform_has_storage: window._platform.storage is webSecureStorage
  - test_device_identity_deleted: import from utils/device-identity fails (file doesn't exist)
- GREEN: Implement type changes, rewrite standalone-k2.ts, update main.tsx bootstrap
- REFACTOR:
  - [MUST] Remove IK2Api interface (no longer needed)
  - [MUST] Update IK2 -> IK2Vpn rename across type file
  - [SHOULD] Clean up comments referencing old _k2.api pattern

**Acceptance**: window._k2 only has a VPN command method. window._platform has getUdid, storage, updater. device-identity.ts deleted. TypeScript compiles.

**Knowledge**: docs/knowledge/task-splitting.md — "Shared Infrastructure Must Complete Before Feature Tasks"

---

### F2: Cloud API Module + Auth Service Migration

**Scope**: Create the new webapp/src/services/cloud-api/ module that replaces _k2.api calls with direct HTTP calls. Migrate auth-service.ts from window._k2.platform to window._platform. Update UDID in standalone mode to fetch from daemon.

**Files**:
- webapp/src/services/cloud-api/client.ts (NEW: HTTP client with base URL, auth interceptor, 401 retry)
- webapp/src/services/cloud-api/endpoints.ts (NEW: typed API methods — auth, user, tunnel, purchase, invite, ticket)
- webapp/src/services/cloud-api/types.ts (NEW: request/response types)
- webapp/src/services/cloud-api/index.ts (NEW: barrel export)
- webapp/src/services/auth-service.ts (migrate: window._k2.platform -> window._platform)
- webapp/src/services/k2api.ts (rewrite: replace _k2.api calls with cloudApi calls, keep cache/401/402 handling)
- webapp/src/services/cache-store.ts (no changes, but owned here for cache integration)
- webapp/src/services/__tests__/cloud-api.test.ts (NEW: test HTTP client, auth interceptor, 401 retry)
- webapp/src/services/__tests__/auth-service.test.ts (update: mock window._platform instead of window._k2.platform)
- webapp/src/services/__tests__/k2api.test.ts (update: mock cloudApi instead of _k2.api)

**Depends on**: [F1]

**TDD**:
- RED: Write failing tests for cloud API client and auth migration
  - test_cloudApi_client_sends_auth_header: requests include Bearer token from authService
  - test_cloudApi_client_handles_401_refresh_retry: 401 triggers token refresh, retries original request
  - test_cloudApi_client_handles_401_refresh_fail_logout: refresh failure clears tokens + sets isAuthenticated=false
  - test_cloudApi_auth_login: cloudApi.auth.login(email, code, udid) returns tokens
  - test_cloudApi_user_info: cloudApi.user.getInfo() returns user data
  - test_authService_uses_platform_global: authService.getUdid() reads from window._platform.getUdid()
  - test_standalone_udid_from_daemon: standalone mode fetches UDID from /api/device/udid
  - test_router_udid_from_daemon: router mode uses same daemon UDID endpoint
  - test_router_storage_webSecureStorage: router _platform.storage is webSecureStorage
- GREEN: Implement cloud-api module, migrate auth-service, rewrite k2api
- REFACTOR:
  - [MUST] Ensure k2api cache/SWR logic is preserved during rewrite
  - [SHOULD] Remove _k2.api references from code comments
  - [SHOULD] Add JSDoc to cloud-api endpoints

**Acceptance**: cloudApi.auth.login() works. Token refresh on 401 works. authService.getUdid() uses window._platform. Standalone/router UDID from daemon. All existing k2api consumers still work (same return format).

**Knowledge**: docs/knowledge/architecture-decisions.md — antiblock entry URL resolution

---

## Feature Tasks

### T3: Consumer Migration (pages + components + hooks + stores)

**Scope**: Update all files that reference _k2.api, _k2.platform, or _k2.updater to use the new globals. Most consumers go through k2api() (already migrated in F2), so this task focuses on direct references.

**Files** (direct _k2.platform / _k2.updater references):
- webapp/src/hooks/useUpdater.ts (change window._k2?.updater -> window._platform?.updater)
- webapp/src/hooks/useEvaluation.ts (check: may reference _k2 directly)
- webapp/src/pages/SubmitTicket.tsx (_k2.platform.uploadServiceLogs -> _platform.uploadServiceLogs)
- webapp/src/pages/Dashboard.tsx (_k2.platform.uploadServiceLogs/os/version -> _platform.*)
- webapp/src/pages/Account.tsx (check: may reference _k2.platform)
- webapp/src/pages/DeveloperSettings.tsx (check: may reference _k2 directly)
- webapp/src/pages/FAQ.tsx (check: may reference _k2 directly)
- webapp/src/components/VersionItem.tsx (check: may reference _k2.platform.version)
- webapp/src/components/SpeedTest.tsx (check: may reference _k2 directly)
- webapp/src/components/CloudTunnelList.tsx (check: may reference _k2 directly)
- webapp/src/services/index.ts (update exports if needed)
- webapp/src/stores/vpn.store.ts (uses window._k2.core — should already be correct since core stays on _k2)
- webapp/src/core/polling.ts (uses window._k2.core — already correct)
- webapp/src/core/EXAMPLE.md (update documentation examples)

**Depends on**: [F1, F2]

**TDD**:
- RED: Write/update tests for migrated consumers
  - test_useUpdater_reads_platform_updater: useUpdater() reads from window._platform.updater
  - test_submitTicket_uses_platform: SubmitTicket calls window._platform.uploadServiceLogs
  - test_dashboard_uses_platform: Dashboard calls window._platform.uploadServiceLogs
  - test_api_pages_work: pages that use k2api() still function (integration smoke)
- GREEN: Update all direct references, run existing tests
- REFACTOR:
  - [MUST] Grep entire src/ for any remaining _k2.api, _k2.platform, _k2.updater references
  - [SHOULD] Update EXAMPLE.md documentation

**Acceptance**: Zero references to _k2.api, _k2.platform, or _k2.updater in src/ (except comments/docs). All components render. All hooks work.

---

### T4: CLAUDE.md + Documentation Update + Final Verification

**Scope**: Update all project documentation to reflect the new architecture. Run full build + test verification. Update webapp/CLAUDE.md hard rules (the old rules reference _k2.api and _k2.platform).

**Files**:
- webapp/CLAUDE.md (rewrite: Hard Rules, Architecture section, Modification Checklist)
- webapp/src/core/EXAMPLE.md (if not already updated in T3)
- webapp/src/vite-env.d.ts (check: may have _k2 type references)
- webapp/src/env.d.ts (check: may have _k2 type references)

**Depends on**: [T3]

**TDD**:
- RED: Verification checklist (not traditional unit tests)
  - test_yarn_build: cd webapp and yarn build succeeds
  - test_yarn_test: cd webapp and yarn test passes
  - test_no_old_k2_api_references: grep for _k2.api in src/ returns zero matches (excluding comments)
  - test_no_old_k2_platform_references: grep for _k2.platform in src/ returns zero matches
  - test_no_old_k2_updater_references: grep for _k2.updater in src/ returns zero matches
- GREEN: Fix any remaining issues found during verification
- REFACTOR:
  - [MUST] Ensure CLAUDE.md Hard Rules match new architecture
  - [SHOULD] Update feature spec status to implemented

**Acceptance**: yarn build succeeds. yarn test passes. CLAUDE.md reflects new _k2 (VPN only) + _platform (native capabilities) + cloudApi (internal module) architecture. Zero stale references.

---

## Execution Summary

```
F1 (Types + Global Split) ---- depends on: none
    |
    v
F2 (Cloud API + Auth Migration) ---- depends on: F1
    |
    v
T3 (Consumer Migration) ---- depends on: F1, F2
    |
    v
T4 (Docs + Verification) ---- depends on: T3
```

**Critical path**: F1 -> F2 -> T3 -> T4 (sequential, no parallelism possible)

**Why sequential**: Every task imports from the previous task's output:
- F2 imports new IK2Vpn/IPlatform types from F1
- T3 imports cloudApi from F2 and uses new globals from F1
- T4 verifies everything from T3

**Total files**: ~30 files (create 5, modify 20, delete 1, update docs 4)

**Risk areas**:
1. k2api.ts rewrite (F2) — highest risk, core API wrapper with cache/SWR/401 handling
2. Consumer migration (T3) — wide blast radius, but mostly mechanical find-and-replace
3. Standalone/router mode (F2) — must verify daemon UDID endpoint still works

## Out of Scope (Deferred)

These items from the spec require changes outside webapp and are deferred to separate tasks:

- **Desktop Tauri UDID Rust command** (desktop/src-tauri/src/platform.rs) — new Rust module for hardware UDID via SHA-256. Requires Tauri command registration.
- **Mobile K2Plugin SHA-256 normalization** — iOS identifierForVendor and Android ANDROID_ID need SHA-256 wrapping.
- **k2 daemon /api/device/udid removal** — Remove legacy endpoint from k2 Go submodule.
- **Router daemon build layer** — Add /api/platform/udid endpoint via build tag.

These are tracked in the spec (AC4, AC5) but require cross-repo changes. Current plan focuses on webapp-only changes that can be built and tested independently.
