# Plan: k2api Legacy Cleanup

## Meta

| Field | Value |
|-------|-------|
| Feature | k2api-cleanup |
| Spec | docs/features/k2api-cleanup.md |
| Date | 2026-02-17 |
| Complexity | moderate |

## Note

Depends-on graph is sequential (F1 then T2). Single branch recommended per task-splitting knowledge.

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: k2api fully deleted | test_no_k2api_imports (consumer-migration) | T2 |
| AC2: All call sites use cloudApi | test_no_k2api_imports (consumer-migration) | T2 |
| AC3: Login uses correct path | test_login_auto_saves_tokens | F1 (cloudApi) + T2 (callers) |
| AC4: Token auto-management | test_login_auto_saves_tokens, test_logout_auto_clears | F1 |
| AC5: 401 refresh fixed | test_401_null_refresh_token_skips_request, test_401_concurrent_shares_refresh | F1 |
| AC6: Cache independent | Inline in T2 cached callers (cacheStore already tested separately) | T2 |
| AC7: No regressions | yarn build + yarn test + tsc --noEmit | T2 (final gate) |

## Foundation Tasks

### F1: Rewrite cloud-api.ts

**Scope**: Add get/post methods, auth path auto-save, fix 401 handler.

**Files**:
- `webapp/src/services/cloud-api.ts` (modify)
- `webapp/src/services/__tests__/cloud-api.test.ts` (modify)

**Depends on**: none

**TDD**:

- RED: Write failing tests for new behaviors
  - `test_get_convenience_method` -- cloudApi.get('/api/x') delegates to request('GET', '/api/x')
  - `test_post_convenience_method` -- cloudApi.post('/api/x', body) delegates to request('POST', '/api/x', body)
  - `test_login_auto_saves_tokens` -- POST /api/auth/login with code=0 auto-calls authService.setTokens()
  - `test_register_auto_saves_tokens` -- POST /api/auth/register with code=0 auto-saves
  - `test_logout_auto_clears` -- POST /api/auth/logout with code=0 auto-calls authService.clearTokens() + cacheStore.clear()
  - `test_non_auth_path_does_not_save_tokens` -- GET /api/user/info does NOT call setTokens
  - `test_401_null_refresh_token_skips_request` -- refreshToken is null, no refresh fetch sent, returns 401 immediately + clears auth
  - `test_401_concurrent_shares_refresh` -- two concurrent 401s trigger only one refresh HTTP call

- GREEN: Implement in cloud-api.ts:
  1. Add get/post convenience methods (thin wrappers over request)
  2. After successful response in request(): detect auth paths, auto-save tokens; detect logout, auto-clear
  3. In _handle401: add null refreshToken guard (return 401, clear auth, skip HTTP)
  4. In _handle401: module-level _refreshPromise lock for concurrent 401 dedup
  5. Extract _doRefresh(refreshToken) as separate method returning Promise (boolean)
  6. Retry uses this.request() (recursive, auto-injects new token from storage)

- REFACTOR:
  - [MUST] Auth path constants (AUTH_PATHS, LOGOUT_PATH) at module top level
  - [SHOULD] Improve method naming consistency

**Acceptance**:
- 8 new tests pass + 7 existing tests still pass
- cloudApi.get() and cloudApi.post() work
- Login/register/refresh auto-save tokens
- Null refreshToken does not trigger HTTP
- Concurrent 401s share single refresh

---

## Feature Tasks

### T2: Migrate all callers + delete k2api

**Scope**: Replace every k2api() call with cloudApi.get/post. Three caller categories: auth (remove manual setTokens), cached (use cacheStore directly), simple (mechanical). Then delete k2api.

**Files** (modify -- auth callers):
- `webapp/src/components/LoginDialog.tsx`
- `webapp/src/components/EmailLoginForm.tsx`
- `webapp/src/components/PasswordDialog.tsx`

**Files** (modify -- cached callers):
- `webapp/src/hooks/useUser.ts`
- `webapp/src/hooks/useAppConfig.ts`
- `webapp/src/pages/Purchase.tsx`
- `webapp/src/components/CloudTunnelList.tsx`
- `webapp/src/components/MemberSelection.tsx`

**Files** (modify -- simple callers):
- `webapp/src/pages/Account.tsx`
- `webapp/src/pages/Devices.tsx`
- `webapp/src/pages/InviteHub.tsx`
- `webapp/src/pages/Issues.tsx`
- `webapp/src/pages/IssueDetail.tsx`
- `webapp/src/pages/UpdateLoginEmail.tsx`
- `webapp/src/pages/SubmitTicket.tsx`
- `webapp/src/pages/ProHistory.tsx`
- `webapp/src/pages/MyInviteCodeList.tsx`
- `webapp/src/pages/DeviceInstall.tsx`
- `webapp/src/pages/MemberManagement.tsx`
- `webapp/src/components/RetailerStatsOverview.tsx`
- `webapp/src/components/WithdrawDialog.tsx`
- `webapp/src/hooks/useInviteCodeActions.ts`
- `webapp/src/hooks/useShareLink.ts`

**Files** (delete):
- `webapp/src/services/k2api.ts`
- `webapp/src/services/__tests__/k2api-v2.test.ts`

**Files** (modify -- cleanup):
- `webapp/src/services/index.ts`
- `webapp/src/services/__tests__/consumer-migration.test.ts`

**Depends on**: [F1]

**TDD**:

- RED: Update consumer-migration test to enforce no k2api usage
  - `test_no_k2api_imports` -- grep codebase for k2api imports, expect zero
  - `test_no_api_request_pattern` -- grep for 'api_request' string, expect zero
  - `test_no_manual_setTokens_in_components` -- grep components/pages for authService.setTokens, expect zero (only cloud-api.ts should call it)

- GREEN: Migrate callers in four waves:

  Wave 1 -- Auth callers (3 files):
  - LoginDialog: k2api('login', ...) -> cloudApi.post('/api/auth/login', ...) + delete manual setTokens block
  - EmailLoginForm: k2api('api_request', {path:'/api/auth/login'}) -> cloudApi.post('/api/auth/login', ...) + delete manual setTokens
  - PasswordDialog: k2api('api_request', {path:'/api/auth/password-login'}) -> cloudApi.post(...)

  Wave 2 -- Cached callers (5 files):
  - Replace k2api({ cache: {...} }).exec(...) with: cacheStore.get() check + cloudApi.get/post() + cacheStore.set() on success
  - Each file: useUser, useAppConfig, Purchase, CloudTunnelList, MemberSelection

  Wave 3 -- Simple callers (15 files):
  - Mechanical: k2api().exec('api_request', {method:'GET', path:X}) -> cloudApi.get(X)
  - Mechanical: k2api().exec('api_request', {method:'POST', path:X, body:Y}) -> cloudApi.post(X, Y)

  Wave 4 -- Cleanup:
  - Delete k2api.ts and k2api-v2.test.ts
  - Remove k2api exports from services/index.ts
  - Update consumer-migration.test.ts: remove k2api tests, add new no-k2api assertions

- REFACTOR:
  - [MUST] Verify yarn build succeeds
  - [MUST] Verify tsc --noEmit passes
  - [MUST] Verify yarn test passes
  - [SHOULD] Remove k2api references in store comments (auth.store.ts, vpn.store.ts)
  - [SHOULD] Update errorHandler.ts JSDoc example referencing k2api

**Acceptance**:
- Zero k2api imports in codebase
- Zero 'api_request' strings in codebase
- Zero manual authService.setTokens() outside cloud-api.ts
- LoginDialog login goes to /api/auth/login (not /api/core)
- All cached callers work with direct cacheStore
- yarn build + yarn test + tsc --noEmit all pass

**Knowledge**: task-splitting.md -- entry-point files are merge conflict hotspots (services/index.ts handle last)
