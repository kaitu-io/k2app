# Plan: webapp-antiblock

**Spec**: `docs/features/webapp-antiblock.md` (v3)
**Complexity**: Simple (<7 files, sequential deps, no refactoring)
**Strategy**: Single branch, no worktrees

---

## T1: Add `/api` CORS middleware — private origin only (server-side)

**Branch**: `feat/webapp-antiblock`
**Depends on**: (none)
**Files**:
- MODIFY: `api/middleware.go`
- MODIFY: `api/route.go`

### RED

1. **`TestApiCORSMiddleware_LocalhostOriginAllowed`** — GET `/api/plans` with `Origin: http://localhost:1420` returns `Access-Control-Allow-Origin: http://localhost:1420` + `Access-Control-Allow-Credentials: true`
2. **`TestApiCORSMiddleware_LoopbackOriginAllowed`** — GET with `Origin: http://127.0.0.1:1777` returns echo + credentials
3. **`TestApiCORSMiddleware_RFC1918_10_Allowed`** — GET with `Origin: http://10.0.0.1` returns echo + credentials
4. **`TestApiCORSMiddleware_RFC1918_172_Allowed`** — GET with `Origin: http://172.16.0.1` returns echo + credentials
5. **`TestApiCORSMiddleware_RFC1918_192_Allowed`** — GET with `Origin: http://192.168.1.1` returns echo + credentials
6. **`TestApiCORSMiddleware_CapacitorOriginAllowed`** — GET with `Origin: capacitor://localhost` returns echo + credentials
7. **`TestApiCORSMiddleware_PublicOriginRejected`** — GET with `Origin: https://evil.com` returns NO `Access-Control-Allow-Origin` header
8. **`TestApiCORSMiddleware_PreflightReturns204`** — OPTIONS `/api/plans` with private origin returns 204 + CORS headers
9. **`TestApiCORSMiddleware_RFC1918_172_32_Rejected`** — GET with `Origin: http://172.32.0.1` returns NO CORS (outside 172.16-31 range)
10. **`TestAppCORSMiddleware_Unchanged`** — `/app/*` still returns specific whitelisted origin (not private-origin logic)

### GREEN

1. Add `isPrivateOrigin(origin string) bool` to `api/middleware.go`:
   - Parse origin URL to extract hostname
   - Match: `localhost`, `127.0.0.1` (any port, http/https)
   - Match: `capacitor://localhost`
   - Match RFC 1918: `10.x.x.x`, `172.16-31.x.x`, `192.168.x.x` (any port, http/https)
   - Reject everything else
2. Add `ApiCORSMiddleware()` to `api/middleware.go`:
   - If `Origin` header present AND `isPrivateOrigin(origin)`:
     - `Access-Control-Allow-Origin: <origin>` (echo back)
     - `Access-Control-Allow-Credentials: true`
     - `Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS`
     - `Access-Control-Allow-Headers: Content-Type, Authorization, X-CSRF-Token`
     - `Access-Control-Max-Age: 86400`
   - If OPTIONS → 204, abort
   - Otherwise `c.Next()`
3. Mount on `/api` group in `api/route.go`:
   ```go
   api.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), ApiCORSMiddleware())
   ```

### REFACTOR

1. `[MUST]` Existing API tests still pass
2. `[SHOULD]` Verify `/app/*` admin CORS behavior unchanged

---

## T2: Add antiblock module + wire into cloudApi (client-side)

**Branch**: `feat/webapp-antiblock` (same branch)
**Depends on**: T1
**Files**:
- NEW: `webapp/src/services/antiblock.ts`
- MODIFY: `webapp/src/services/cloud-api.ts`
- MODIFY: `webapp/src/services/index.ts`
- NEW: `webapp/src/services/__tests__/antiblock.test.ts`

### RED

1. **`test_decrypt_roundtrip`** — encrypt with known key → decrypt → matches original
2. **`test_decrypt_wrong_key_returns_null`** — wrong key → null (no throw)
3. **`test_decrypt_tampered_payload_returns_null`** — tampered GCM tag → null
4. **`test_cache_hit_skips_fetch`** — localStorage has cached entry → returns immediately
5. **`test_all_cdn_fail_returns_default`** — all CDN fail → DEFAULT_ENTRY
6. **`test_background_refresh_on_cache_hit`** — cache hit triggers background script injection
7. **`test_cdn_sources_are_github_urls`** — CDN_SOURCES contain jsDelivr + statically.io
8. **`test_no_atob_in_source`** — source code has no `atob(` calls
9. **`test_key_is_64_hex`** — DECRYPTION_KEY matches `/^[0-9a-f]{64}$/`
10. **`test_default_entry_is_plain_url`** — DEFAULT_ENTRY starts with `https://`
11. **`test_cloud_api_uses_absolute_url`** — cloudApi.request() calls fetch with `entry + path`
12. **`test_cloud_api_refresh_uses_absolute_url`** — 401 refresh also uses `entry + path`

Tests 1-10: copy from `webapp2/src/api/__tests__/antiblock.test.ts`, adjust imports.
Tests 11-12: new tests for cloudApi integration.

### GREEN

1. Copy `webapp2/src/api/antiblock.ts` → `webapp/src/services/antiblock.ts` (verbatim)
2. Modify `webapp/src/services/cloud-api.ts`:
   - Import `resolveEntry` from `./antiblock`
   - `request()`: `const entry = await resolveEntry(); fetch(entry + path, ...)`
   - `_handle401()` refresh: `const entry = await resolveEntry(); fetch(entry + '/api/auth/refresh', ...)`
   - `_handle401()` retry: `fetch(entry + path, ...)`
3. Add export to `webapp/src/services/index.ts`

### REFACTOR

1. `[SHOULD]` Verify existing cloud-api tests still pass (mock resolveEntry to return `''`)
2. `[SHOULD]` Verify existing k2api tests still pass
3. `[MUST]` `yarn build` succeeds with zero TypeScript errors
4. `[MUST]` All tests pass: `cd webapp && npx vitest run`

---

## AC Coverage

| AC | Test | Task |
|----|------|------|
| AC1: cloudApi uses resolveEntry() | `test_cloud_api_uses_absolute_url` | T2 |
| AC2: 401 refresh uses resolveEntry() | `test_cloud_api_refresh_uses_absolute_url` | T2 |
| AC3: antiblock 14 tests pass | tests 1-10 + 11-12 | T2 |
| AC4: existing tests unaffected | REFACTOR steps 1-2 | T2 |
| AC5: yarn build succeeds | REFACTOR step 3 | T2 |
| AC6: localhost origin → CORS + credentials | `TestApiCORSMiddleware_LocalhostOriginAllowed` | T1 |
| AC7: RFC 1918 origin → CORS + credentials | `TestApiCORSMiddleware_RFC1918_*_Allowed` (3 tests) | T1 |
| AC8: capacitor://localhost → CORS | `TestApiCORSMiddleware_CapacitorOriginAllowed` | T1 |
| AC9: public origin → no CORS | `TestApiCORSMiddleware_PublicOriginRejected` | T1 |
| AC10: OPTIONS preflight → 204 | `TestApiCORSMiddleware_PreflightReturns204` | T1 |
| AC11: `/app/*` admin CORS unchanged | `TestAppCORSMiddleware_Unchanged` | T1 |

---

## Execution Summary

```
T1 (server-side private-origin CORS, ~20 min)
  ├─ Add isPrivateOrigin() to middleware.go
  ├─ Add ApiCORSMiddleware() to middleware.go
  ├─ Mount on /api group in route.go
  └─ Add 10 Go tests
        │
        ▼
T2 (client-side antiblock, ~30 min)
  ├─ Copy antiblock.ts (verbatim from webapp2)
  ├─ Copy + adjust antiblock.test.ts
  ├─ Modify cloud-api.ts (3 fetch sites)
  ├─ Add 2 integration tests for cloudApi
  ├─ Update index.ts export
  └─ Verify all tests + build
```

Single branch `feat/webapp-antiblock`. T1 → T2 sequential (same branch, no worktrees).
