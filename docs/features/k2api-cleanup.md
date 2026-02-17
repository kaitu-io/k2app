# Feature: k2api Legacy Cleanup — Simplify Cloud API Layer

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | k2api-cleanup                            |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-17                               |
| Updated   | 2026-02-17                               |
| Depends on | webapp-architecture-v2                  |

## Version History

| Version | Date       | Summary                                                        |
|---------|------------|----------------------------------------------------------------|
| v1      | 2026-02-17 | Initial: delete k2api wrapper, simplify cloudApi, fix 401 flow |

## Overview

k2api is a legacy wrapper from the `window._k2.api` era. webapp-architecture-v2 already migrated Cloud API from `_k2.api` to the standalone `cloudApi` module, but k2api still exists as an intermediary — 25+ call sites use `k2api().exec('api_request', { method, path, body })` redundantly. Additionally, cloudApi's 401 refresh flow has bugs (null refreshToken sent, concurrent races).

**Goal**: Delete k2api wrapper. All call sites use cloudApi directly. Token management and 401 handling unified inside cloudApi.

## Problem

1. **Redundant indirection**: `k2api().exec('api_request', { method, path, body })` simply forwards 1:1 to `cloudApi.request(method, path, body)`. 25+ call sites pay this overhead for nothing.

2. **Legacy wrong path**: LoginDialog uses `k2api().exec('login', params)` which routes to `cloudApi.request('POST', '/api/core', params)` instead of `/api/auth/login`. EmailLoginForm already uses the correct path.

3. **Scattered token saving**: LoginDialog manually calls `authService.setTokens()` (line 180), k2api's `handleAuthSuccess` auto-saves (line 87), cloudApi's refresh saves (line 118) — three places doing the same thing.

4. **401 handling bugs**:
   - `_handle401` doesn't check null refreshToken, sends `{"refreshToken":null}` to server
   - Concurrent 401s have no lock, multiple requests refresh simultaneously (race)
   - cloudApi and k2api both handle 401 (both clear tokens), redundant

5. **Cache coupling**: k2api bundles HTTP requests with caching. 20 call sites that don't need caching are forced through k2api anyway.

## Design

### 1. Delete k2api, use cloudApi directly

```
BEFORE:
  k2api().exec('api_request', { method: 'GET', path: '/api/user/info' })
  k2api({ cache: {...} }).exec('api_request', { method: 'GET', path: '/api/plans' })
  k2api().exec('login', { email, code, udid })   // BUG: goes to /api/core

AFTER:
  cloudApi.get('/api/user/info')
  cloudApi.get('/api/plans')
  cloudApi.post('/api/auth/login', { email, verificationCode, udid })
```

Add `.get()` / `.post()` convenience methods to cloudApi:

```typescript
const cloudApi = {
  async request<T>(method, path, body?): Promise<SResponse<T>> { /* existing */ },
  async get<T>(path: string): Promise<SResponse<T>> { return this.request('GET', path); },
  async post<T>(path: string, body?: unknown): Promise<SResponse<T>> { return this.request('POST', path, body); },
};
```

### 2. Independent caching

cacheStore stays as standalone module, decoupled from k2api. The 5 call sites that need caching compose it directly:

```typescript
const cached = cacheStore.get<DataUser>('user_info');
if (cached) return { code: 0, data: cached };
const response = await cloudApi.get<DataUser>('/api/user/info');
if (response.code === 0) cacheStore.set('user_info', response.data, { ttl: 300 });
```

Call sites using cache (5 total): useUser, useAppConfig, Purchase, CloudTunnelList, MemberSelection.

### 3. Auth token unified in cloudApi

cloudApi auto-detects auth responses and saves/clears tokens internally:

```typescript
// Inside cloud-api.ts
const AUTH_PATHS = ['/api/auth/login', '/api/auth/register', '/api/auth/refresh'];
const LOGOUT_PATH = '/api/auth/logout';

// After successful response in request():
if (isAuthPath(path) && response.code === 0 && response.data) {
  const token = response.data.token || response.data.accessToken;
  if (token) await authService.setTokens({ accessToken: token, refreshToken: response.data.refreshToken });
}
if (isLogoutPath(path) && response.code === 0) {
  await authService.clearTokens();
  cacheStore.clear();
}
```

LoginDialog / EmailLoginForm remove manual `setTokens` calls.

### 4. Fix 401 refresh

```typescript
// cloud-api.ts
let _refreshPromise: Promise<boolean> | null = null;

async _handle401<T>(method, path, body): Promise<SResponse<T>> {
  // 1. Null guard — no refreshToken, give up immediately
  const refreshToken = await authService.getRefreshToken();
  if (!refreshToken) {
    await authService.clearTokens();
    useAuthStore.setState({ isAuthenticated: false });
    return { code: 401, message: 'No refresh token' };
  }

  // 2. Concurrency lock — multiple 401s share one refresh
  if (!_refreshPromise) {
    _refreshPromise = this._doRefresh(refreshToken)
      .finally(() => { _refreshPromise = null; });
  }
  const success = await _refreshPromise;

  if (!success) {
    return { code: 401, message: 'Unauthorized' };
  }

  // 3. Retry with new token (request() auto-injects from storage)
  return this.request<T>(method, path, body);
}
```

k2api's 401/402 handling fully deleted. cloudApi is the sole 401 handler.

### 5. Fix LoginDialog

```typescript
// Before (BUG: goes to /api/core)
const response = await k2api().exec<AuthResult>('login', {
  email, verificationCode, udid, remark, inviteCode, language
});
// Manual setTokens below...

// After
const response = await cloudApi.post<AuthResult>('/api/auth/login', {
  email, verificationCode, udid, remark, inviteCode, language
});
// Tokens auto-saved by cloudApi
```

## Changes Summary

| Layer | Action | Files |
|-------|--------|-------|
| **Delete** | Delete k2api.ts | `webapp/src/services/k2api.ts` |
| **Delete** | Delete k2api tests | `webapp/src/services/__tests__/k2api-v2.test.ts` |
| **Delete** | Remove k2api exports | `webapp/src/services/index.ts` |
| **Modify** | cloudApi: add get/post, auth path auto-save, fix 401 | `webapp/src/services/cloud-api.ts` |
| **Modify** | 25+ call sites migrate to cloudApi.get/post | All pages + hooks + components |
| **Modify** | LoginDialog: use cloudApi.post, remove manual setTokens | `webapp/src/components/LoginDialog.tsx` |
| **Modify** | EmailLoginForm: remove manual setTokens | `webapp/src/components/EmailLoginForm.tsx` |
| **Modify** | 5 cache call sites use cacheStore directly | useUser, useAppConfig, Purchase, CloudTunnelList, MemberSelection |
| **Keep** | cacheStore standalone module unchanged | `webapp/src/services/cache-store.ts` |
| **Keep** | authService unchanged | `webapp/src/services/auth-service.ts` |
| **Modify** | Update consumer-migration tests | `webapp/src/services/__tests__/consumer-migration.test.ts` |

## Acceptance Criteria

### AC1: k2api fully deleted
- `k2api.ts` file does not exist
- No `k2api` references in code (except git history)
- `import { k2api }` causes compile error

### AC2: All call sites use cloudApi
- 25+ call sites migrated to `cloudApi.get()` / `cloudApi.post()`
- No `'api_request'` string literals remain
- No `.exec(` calls remain (in services layer)

### AC3: Login uses correct path
- LoginDialog calls `cloudApi.post('/api/auth/login', ...)` not `/api/core`
- EmailLoginForm calls `cloudApi.post('/api/auth/login', ...)` not `/api/core`
- Neither has manual `setTokens` calls

### AC4: Token auto-management
- Login/register/refresh success: cloudApi auto-saves tokens
- Logout success: cloudApi auto-clears tokens + cache
- No call site manually calls `authService.setTokens()` (only cloudApi internally)

### AC5: 401 refresh fixed
- Null refreshToken: no refresh request sent, immediately clear auth
- Concurrent 401s share single refresh request (lock mechanism)
- cloudApi is sole 401 handler (no secondary handling)

### AC6: Cache independent
- cacheStore exists standalone, no k2api dependency
- 5 cache call sites work correctly (TTL/SWR/allowExpired)

### AC7: No regressions
- `yarn build` succeeds
- `yarn test` passes
- `npx tsc --noEmit` no type errors
- Login -> API calls -> 401 refresh -> logout full flow works
