# k2r Device Class Identity — Design

**Date**: 2026-05-22
**Branch target**: `feat/k2r-device-class-identity`
**Scope**: api/ + webapp/ + i18n. No changes to k2r Go binary or DB schema. Builds on the 2026-04-09 k2r tiered-plan spec.

## Problem

The 2026-04-09 k2r release spec assumes a server-side way to tell a router login from an app login: `Device.IsGateway`, `RouterRequired()`, and a per-class `checkDeviceLimitOrKick` branch all exist in `api/`. The wire signal designed to drive them — `X-App-Info: {"isGateway": true}` JSON header — is **never sent by any client** in the current codebase (verified by full-tree grep). As a result:

1. k2r logins are silently counted as **app devices**, consuming `MaxDevice` quota instead of `MaxRouterDevice`. Tier gating is moot.
2. A user can copy their phone's access token + UDID into `/etc/k2r/storage.json` on a router and obtain VPN service on the router under their phone tier — the canonical "regular user puts phone auth on router" abuse pattern.
3. The `X-App-Info` header is also redundant with `X-K2-Client` (both carry version/platform/arch), creating two header parsers that mix-and-overwrite each other in `fillDeviceAppInfo`.

We want a **single, clearly-located source of truth** for device class, propagated end-to-end, that defends against the copy-token abuse pattern at the architecture layer.

## Threat Model

Weak attacker only: ordinary users who don't reverse-engineer binaries or modify the webapp source. The specific scenario to block is **copying a phone-issued token+UDID onto a router** to get router functionality at app-tier price.

Not in scope: device attestation, hardware-bound tokens, PKI, license-key activation, defending against a user who rebuilds k2r from modified source.

## Solution Architecture

### A. One header, RFC 7231 User-Agent grammar, product token = class signal

Drop `X-App-Info` entirely. Extend `X-K2-Client` to encode device class in the product name:

```
X-K2-Client = product " " comment
product         = product-name "/" product-version
product-name    = "kaitu-" client-class
client-class    = "service" | "router"          ; extensible to future tokens
product-version = semver
comment         = "(" platform "; " arch [ "; " os-version [ "; " device-model ] ] ")"
```

Examples:

```
kaitu-service/0.4.5 (ios; arm64; iOS 17.4; iPhone15,2)
kaitu-service/0.4.5 (macos; arm64)
kaitu-service/0.4.5 (linux; amd64; Ubuntu 24.04; ThinkPad X1)   ; cmd/k2 Linux desktop
kaitu-router/0.4.5  (linux; arm64; OpenWrt 23.05; mt7620)       ; cmd/k2r router
```

Note Linux desktop (`cmd/k2`, also using `k2/webui/`) is `kaitu-service` because its
`webui/platform.go` sets `PlatformType="desktop"`. Only `cmd/k2r` sets
`PlatformType="gateway"`. The `linux` platform string in the comment does NOT signal
class — only the product token does.

Why `X-K2-Client` and not `User-Agent`: browser fetch/XHR puts `User-Agent` on the [forbidden header name list](https://fetch.spec.whatwg.org/#forbidden-header-name), so the webapp cannot override it. The server therefore must not trust `User-Agent` for any business logic. `X-K2-Client` is the custom header that carries the UA-grammar payload the webapp can actually set.

### B. Truth chain (single path)

```
[k2r binary]        gateway.go:141: PlatformType="gateway"  (hardcoded, unforgeable
                                                            without rebuilding k2r)
       │ HTTP GET /api/platform
       ↓
[webapp]            window._platform.platformType = "gateway"
       │ cloud-api.ts buildClientHeader()
       ↓
[HTTP request]      X-K2-Client: kaitu-router/{version} ({comment})
       │ ────────── only origination point for this header
       ↓
[Center]            parseClientHeader → AppInfo.ClientClass="router" → IsGateway=true
       │
       ├── First login: write Device.IsGateway = true  (lock-on-create)
       └── Every authenticated request: EnforceDeviceClass middleware compares
           parseClientHeader.IsGateway() against authContext.Device.IsGateway.
           Mismatch → 403002 + clear cookies.
```

**Single-source-of-truth invariants** (enforced by review, not runtime):

- The **only** place that constructs `X-K2-Client` is `webapp/src/services/cloud-api.ts buildClientHeader()`.
- The **only** place that decides server-side IsGateway from the header is `api/middleware.go parseClientHeader()`. All other code reads `Device.IsGateway` (DB).
- `Device.IsGateway` is written **only at device creation**, never updated thereafter.

### C. Server components

#### `api/middleware.go` — extend `AppInfo` and regex

```go
type AppInfo struct {
    ClientClass string  // "service" | "router"  — single source of class signal
    Version     string
    Platform    string
    Arch        string
    OSVersion   string
    DeviceModel string
}

func (a *AppInfo) IsGateway() bool { return a.ClientClass == "router" }

// Captures: 1=class, 2=version, 3=platform, 4=arch, 5=os_version?, 6=device_model?
var clientHeaderRegex = regexp.MustCompile(
    `^kaitu-(service|router)/([^\s]+)\s*\(([^;)]+);\s*([^;)]+)(?:;\s*([^;)]+))?(?:;\s*([^)]+))?\)$`)
```

Single product token; trailing `$` anchors prevent extra products from parsing through. Unknown `kaitu-iot/...` returns `nil` → caller returns 422003.

#### `api/middleware.go` — split `fillDeviceAppInfo`

Replace the single function with two:

```go
// createDeviceWithAppInfo: invoked only by login/register handlers when creating
// a fresh Device row. Writes IsGateway from the header — this is the one
// moment device class is decided.
func createDeviceWithAppInfo(c *gin.Context, device *Device)

// refreshDeviceAppInfo: invoked on subsequent authenticated requests. Updates
// version/platform/arch/os_version/device_model only. NEVER touches IsGateway.
func refreshDeviceAppInfo(c *gin.Context, device *Device)
```

This prevents a later request with a different class signal from silently flipping the persisted class.

**Call site migration**:
- `api_auth.go` `login` / `login_password` (both fresh-device branch and device-transfer recreate branch at `api_auth.go:235` and `:799`) → use `createDeviceWithAppInfo`.
- The existing `updateDeviceAppInfo` call in `handleJWTAuth` (middleware.go ~line 369) → replace with `refreshDeviceAppInfo`. Same signature, never writes IsGateway.

#### `api/middleware.go` — `isGatewayRequest` becomes a thin wrapper

```go
func isGatewayRequest(c *gin.Context) bool {
    info := parseClientHeader(c.GetHeader("X-K2-Client"))
    return info != nil && info.IsGateway()
}
```

Remove all `X-App-Info` parsing code (both branches in current file).

#### Login handler — validate unknown class token

`isGatewayRequest` returns `false` for any non-`router` token, including a malformed or
unknown one like `kaitu-iot/...`. If the login handler used `isGatewayRequest` alone,
an unknown-class client would silently register as a service device, only to be
rejected by `EnforceDeviceClass` on every subsequent request — an unrecoverable
state.

Add an explicit check in `login` / `login_password` **before** `checkDeviceLimitOrKick`:

```go
header := c.GetHeader("X-K2-Client")
if header != "" && parseClientHeader(header) == nil {
    Error(c, ErrorInvalidClientClass, "invalid client class token")
    return
}
```

Header absence remains legal (legacy client compatibility); only header-present-but-unparseable returns 422003.

#### `api/middleware.go` — new `EnforceDeviceClass`

Mounted on the auth chain after `AuthRequired`, before `ProRequired`:

```go
func EnforceDeviceClass() gin.HandlerFunc {
    return func(c *gin.Context) {
        ctx := getAuthContext(c)
        if ctx == nil || ctx.Device == nil {
            // Web-cookie auth has no device binding — bypass.
            c.Next()
            return
        }
        header := c.GetHeader("X-K2-Client")
        // Legacy-client bypass: header absent means pre-rollout client OR
        // a path that cannot send custom headers (e.g. WebSocket upgrade).
        // Either way, no class assertion is made — pass through. The
        // server-side persisted Device.IsGateway remains authoritative.
        if header == "" {
            c.Next()
            return
        }
        info := parseClientHeader(header)
        if info == nil {
            // Header present but unparseable / unknown class — strict reject.
            Error(c, ErrorInvalidClientClass, "invalid client class token")
            c.Abort()
            return
        }
        if info.IsGateway() != ctx.Device.IsGateway {
            log.Warnf(c, "device class mismatch: udid=%s db_class=%s header_class=%s remote=%s ua=%q",
                ctx.Device.UDID, classStr(ctx.Device.IsGateway), classStr(info.IsGateway()),
                c.ClientIP(), c.Request.UserAgent())
            clearAuthCookies(c)
            Error(c, ErrorDeviceClassMismatch, "device class mismatch")
            c.Abort()
            return
        }
        c.Next()
    }
}
```

**Behavior summary**:

| `X-K2-Client` value | Result |
|---|---|
| absent (legacy / WebSocket) | bypass — no class assertion made; `Device.IsGateway` unchanged |
| present, parses to known class, matches DB | next() |
| present, parses to known class, mismatches DB | 403002 + clear cookies (core defense) |
| present but unparseable or unknown class | 422003 |

The legacy bypass closes the door to the obvious copy-token attack (which goes through the webapp that *always* sends the header), while preserving compatibility with:
- pre-rollout clients still in the wild,
- WebSocket connections that cannot send custom headers in the upgrade request,
- any future non-webapp tooling.

Tightening this bypass (e.g. requiring header for all auth-bound requests after a sunset date) is tracked in the rollout plan.

#### `api/route.go` — wire-up

Mount `EnforceDeviceClass()` after `AuthRequired()` on every route group whose handlers
expect a device context. Explicit list (matching `api/CLAUDE.md` route group table):

| Route group | Mount EnforceDeviceClass? | Rationale |
|---|---|---|
| `/api/tunnels` | ✅ yes | Device-bound (AuthRequired + Pro + Device) |
| `/api/relays` | ✅ yes | Same chain |
| `/api/user/*` | ✅ yes | Profile / devices / membership |
| `/api/strategy/*` | ✅ yes | Auth + Device |
| `/api/telemetry/*` | ✅ yes | Auth + Device |
| `/api/device-logs` | ✅ yes | Auth + Device |
| `/api/router/*` (new) | ✅ yes | New router-only routes |
| `/api/invite/*` | ✅ yes | Auth (Device populated when present) |
| `/api/wallet/*` | ❌ no | Web-portal flow, no device assertion |
| `/api/retailer/*` | ❌ no | `X-Access-Key` auth, no device |
| `/api/issues/*` | ❌ no | Anonymous-ish issue proxy |
| `/api/feedback-tickets` | ❌ no | Anonymous submission allowed |
| `/api/auth/*`, `/api/plans`, `/api/app/config`, `/api/ech/config`, `/api/ca` | ❌ no | Unauthenticated |
| `/app/*` admin | ❌ no | Admin-only, no device class concept |
| `/slave/*`, `/csr/*` | ❌ no | Non-user auth |

For groups marked "no", the bypass-on-`Device==nil` inside the middleware would already handle them safely, but we omit the mount entirely to keep the per-request cost zero and to make route intent explicit.

Mount `RouterRequired()` on at least one router-meaningful endpoint to make the entitlement real. Smallest viable endpoint: `GET /api/router/quota` returning `{maxRouterDevice, maxLanClient}` — this is what k2r needs to enforce its LAN allowlist size locally. (The actual LAN MAC allowlist remains on the router, per the 2026-04-09 spec.)

#### `api/api_router_quota.go` — new file

```go
// GET /api/router/quota — returns MaxLanClient for the authenticated router.
// Auth chain: AuthRequired → EnforceDeviceClass → ProRequired → RouterRequired
func api_router_quota(c *gin.Context) {
    user := ReqUser(c)
    q := user.Quota()
    Success(c, gin.H{
        "maxRouterDevice": q.MaxRouterDevice,
        "maxLanClient":    q.MaxLanClient,
    })
}
```

#### `api/response.go` — new error codes

```go
// 402 family — payment / tier required
ErrorPlanNoRouter        ErrorCode = 402001 // 套餐不支持路由器

// 403 family — access control
ErrorRouterDeviceLimit   ErrorCode = 403001 // 路由器登录数量已达上限
ErrorDeviceClassMismatch ErrorCode = 403002 // 设备身份与历史注册类型不符

// 422 family — invalid input
ErrorInvalidClientClass  ErrorCode = 422003 // X-K2-Client 携带未知 client-class token
```

Update `checkDeviceLimitOrKick` to return these specific codes instead of the generic `ErrorPaymentRequired` / `ErrorForbidden` it currently uses. `RouterRequired()` returns `ErrorPlanNoRouter` instead of `ErrorPaymentRequired`.

### D. Client component

#### `webapp/src/services/cloud-api.ts` — single-point header build

```ts
function buildClientHeader(): string | null {
    const p = window._platform;
    if (!p?.version || !p?.os) return null;
    const cls = p.platformType === 'gateway' ? 'router' : 'service';
    const tail = [p.os, p.arch || 'unknown']
        .concat(p.osVersion ? [p.osVersion] : [])
        .concat(p.deviceModel ? [p.deviceModel] : [])
        .join('; ');
    return `kaitu-${cls}/${p.version} (${tail})`;
}
```

This is the **only** place in the codebase that constructs `X-K2-Client`. Other modules MUST NOT replicate this logic. Enforced by code review.

#### `webapp/src/services/cloud-api.ts` — 403002 side effect

In the response handler (alongside the existing 401 → logout path):

```ts
if (json.code === 403002) {
    await authService.logout();
    // existing logout side effects: clear stored tokens, openLoginDialog()
}
```

This is the **only** new client-side side effect. All other new codes (402001, 403001, 422003) are pure display; the calling page handles them via existing `getErrorMessage()` flow.

#### `webapp/src/utils/errorCode.ts` — register new codes

```ts
export const ERROR_CODES = {
    // ...existing...
    PLAN_NO_ROUTER:        402001,
    ROUTER_DEVICE_LIMIT:   403001,
    DEVICE_CLASS_MISMATCH: 403002,
    INVALID_CLIENT_CLASS:  422003,
} as const;

// getErrorMessage()
case 402001: return t('auth.planNoRouter');
case 403001: return t('auth.routerLimitReached');
case 403002: return t('auth.deviceClassMismatch');
case 422003: return t('auth.invalidClientClass');
```

#### i18n — new keys across all 7 locales

| Key | zh-CN |
|---|---|
| `auth.planNoRouter` | 当前套餐不支持路由器，请升级至家庭版 |
| `auth.routerLimitReached` | 路由器登录数已达上限，请先在原设备退出 |
| `auth.deviceClassMismatch` | 设备类型不匹配，已自动退出登录，请重新登录 |
| `auth.invalidClientClass` | 客户端版本不兼容，请升级 |

Translations follow the existing webapp i18n process: add to `zh-CN` first, then en-US / ja / zh-TW / zh-HK / en-AU / en-GB.

## Data Flow Scenarios

| # | Scenario | header sent | DB.IsGateway | Result |
|---|---|---|---|---|
| 1 | Family-tier user, first login on k2r | `kaitu-router/...` | (new=true) | 200, Device created with IsGateway=true |
| 2 | Basic-tier user, first login on k2r | `kaitu-router/...` | — | 402001 |
| 3 | Family-tier user, second login on k2r while one router slot occupied | `kaitu-router/...` | — | 403001 (router slot full) |
| 4 | Basic-tier user, copies phone token+UDID into router storage | `kaitu-router/...` (webapp adds automatically) | `false` (phone was registered as service) | **403002 + clear cookies + redirect to login**. Core defense. |
| 5 | Phone token+UDID used by phone normally | `kaitu-service/...` | `false` | 200 (consistent) |
| 6 | Pre-rollout app on any platform, header absent | (absent) | `false` (existing) | 200 (legacy compatibility) |
| 7 | Family→basic downgrade, existing router Device still authenticated | `kaitu-router/...` | `true` | EnforceDeviceClass passes; `/api/router/quota` returns 402001 (RouterRequired denies). `/api/tunnels` still serves — router becomes a generic app device on the network side. |
| 8 | UDID transfer across users (login with same UDID, different email) | `kaitu-router/...` | (delete-and-recreate path in `api_auth.go:235`) | New Device created with current header's class — class can change across owners (correct because effectively different device). |
| 9 | Future client sends `kaitu-iot/0.4.5 (...)` to current server | `kaitu-iot/...` | — | 422003 (unknown class). Old server doesn't silently accept new classes. |

## Error Handling Matrix

| Code | Trigger | Where | Clear token | Webapp behavior |
|---|---|---|---|---|
| 402001 | `Quota().MaxRouterDevice==0` AND (router login attempt OR `/api/router/*` access) | `checkDeviceLimitOrKick` (login) + `RouterRequired()` (post-auth) | No | Open purchase page, highlight family tier |
| 403001 | `routerCount >= MaxRouterDevice` | `checkDeviceLimitOrKick` **only — fires at new device registration**; once a device row exists it holds a slot, so post-login refreshes cannot trigger this | No | Show "router slot full", instruct user to logout original router |
| 403002 | header class ≠ persisted `Device.IsGateway` | `EnforceDeviceClass` middleware (every authenticated request) | **Yes** | `authService.logout()` + `openLoginDialog()` + show mismatch message |
| 422003 | `X-K2-Client` present but unparseable / unknown class | `EnforceDeviceClass` middleware + `/api/auth/login` handler (pre-create gate) | No | Sentry report + show "client incompatible, please upgrade" |

## Server Logging (DIAG-style, for kaitu-support ticket triage)

```go
// 402001
log.Infof(c, "plan no router: user=%d tier=%s remote=%s", user.ID, user.Tier, c.ClientIP())

// 403001
log.Warnf(c, "router slot full: user=%d count=%d max=%d", user.ID, routerCount, quota.MaxRouterDevice)

// 403002  — copy-token abuse fingerprint
log.Warnf(c, "device class mismatch: udid=%s db_class=%s header_class=%s remote=%s ua=%q",
    device.UDID, classStr(device.IsGateway), classStr(claimed), c.ClientIP(), c.Request.UserAgent())

// 422003
log.Warnf(c, "invalid client class: header=%q remote=%s", header, c.ClientIP())
```

## Testing Matrix

### Server unit tests (`api/middleware_test.go`)

`parseClientHeader` coverage:

| Test | Input | Expect |
|---|---|---|
| `TestParseClientHeader_Service` | `kaitu-service/0.4.5 (ios; arm64)` | `{ClientClass:"service", IsGateway:false, Version:"0.4.5", Platform:"ios", Arch:"arm64"}` |
| `TestParseClientHeader_Router` | `kaitu-router/0.4.5 (linux; arm64; OpenWrt 23.05; mt7620)` | `{ClientClass:"router", IsGateway:true, OSVersion:"OpenWrt 23.05", DeviceModel:"mt7620"}` |
| `TestParseClientHeader_UnknownClass` | `kaitu-iot/0.4.5 (linux; arm64)` | `nil` |
| `TestParseClientHeader_Empty` | `""` | `nil` |
| `TestParseClientHeader_Malformed` | `kaitu-router 0.4.5 linux arm64` | `nil` |
| `TestParseClientHeader_ExtraProducts` | `kaitu-router/0.4.5 OpenWrt/23.05 (linux; arm64)` | `nil` |
| `TestParseClientHeader_VersionWithSuffix` | `kaitu-service/0.4.0-beta.1 (macos; arm64)` | `Version:"0.4.0-beta.1"` |

### Server mock DB tests (`api/api_auth_test.go`)

`SetupMockDB(t)` + login transactions:

| Test | header | user.Tier | existing device(UDID) | Expect |
|---|---|---|---|---|
| `TestLogin_RouterRegister_FamilyTier` | router | family | none | 200, Device{IsGateway:true} created |
| `TestLogin_RouterRegister_BasicTier_Rejected` | router | basic | none | code=402001 |
| `TestLogin_RouterSlotFull` | router | family | 1 router exists (MaxRouterDevice=1) | code=403001 |
| `TestLogin_AppRegister_KicksOldestApp` | service | basic | 3 app devices (MaxDevice=3) | 200, oldest app device deleted |
| `TestLogin_DeviceTransfer_CrossClass` | router | family | same UDID held by other user, IsGateway=false | delete old, create new with IsGateway=true |
| `TestLogin_NoHeader_LegacyClient` | absent | basic | none | 200, IsGateway=false |
| `TestLogin_UnknownClassToken_Rejected` | `kaitu-iot/0.4.5 (...)` | basic | none | code=422003, **no Device row created** |
| `TestLogin_MalformedHeader_Rejected` | `kaitu-router 0.4.5 ...` | basic | none | code=422003 |

### Server mock DB tests (`api/middleware_test.go` — EnforceDeviceClass)

| Test | Device.IsGateway | header class | Expect |
|---|---|---|---|
| `TestEnforceDeviceClass_ConsistentApp` | false | service | next() |
| `TestEnforceDeviceClass_ConsistentRouter` | true | router | next() |
| `TestEnforceDeviceClass_Mismatch_AppOnRouterDevice` | true | service | code=403002 + cookies cleared |
| `TestEnforceDeviceClass_Mismatch_RouterOnAppDevice` | false | router | code=403002 + cookies cleared + warn log |
| `TestEnforceDeviceClass_NoHeader_LegacyAppDevice` | false | absent | next() |
| `TestEnforceDeviceClass_NoHeader_LegacyRouterDevice` | true | absent | next() (legacy bypass) |
| `TestEnforceDeviceClass_WebCookie_NoDevice` | n/a (UDID="") | service | next() (cookie-only bypass) |
| `TestEnforceDeviceClass_UnknownClass` | false | iot | code=422003 |

### Server integration test (`api/login_flow_e2e_test.go` — append)

`skipIfNoConfig(t)`. Real MySQL:

| Test | Flow |
|---|---|
| `TestLoginFlow_RouterFullCycle` | family user → register router → `/api/user/info` with router header passes → swap to service header → `/api/user/info` returns 403002 |
| `TestLoginFlow_PlanDowngrade` | family user → register router → admin changes tier to basic → `/api/router/quota` returns 402001 (class still matches; RouterRequired denies) |

### Webapp Vitest (`webapp/src/services/__tests__/cloud-api.test.ts` — append)

| Test | platformType | Expected `X-K2-Client` |
|---|---|---|
| `should send kaitu-router on gateway platform` | gateway | `kaitu-router/0.4.5 (linux; arm64)` |
| `should send kaitu-service on macos desktop` | desktop (`os: 'macos'`) | `kaitu-service/0.4.5 (macos; arm64)` |
| `should send kaitu-service on linux desktop (cmd/k2)` | desktop (`os: 'linux'`) | `kaitu-service/0.4.5 (linux; amd64)` — distinct from `kaitu-router/...` even though platform=linux |
| `should send kaitu-service on mobile with extended fields` | mobile | `kaitu-service/0.4.5 (ios; arm64; iOS 17.4; iPhone15,2)` |
| `should send kaitu-service on web platform` | web | `kaitu-service/0.4.5 (windows; amd64)` |
| `should not send header when _platform missing` | undefined | header absent |
| `should call logout on 403002 response` | mock returns 403002 | `authService.logout()` invoked once |
| `should NOT call logout on 403001/402001/422003` | mock returns each | `authService.logout()` not invoked |

### E2E verification (manual, documented in PR description)

1. macOS desktop login → Network panel shows `X-K2-Client: kaitu-service/...`
2. iOS app login → `X-K2-Client: kaitu-service/0.4.5 (ios; arm64; iOS ...; iPhone...)`
3. k2r router login → `X-K2-Client: kaitu-router/...` and Center log shows `IsGateway=true` on device creation
4. **Copy-token defense check**: log in on phone normally, manually export token+UDID from keychain/localStorage, paste into k2r `/etc/k2r/storage.json`, open router IP in browser. Expect: any API call returns 403002, webapp redirects to login, Center log shows `device class mismatch`.
5. **Quota gate check**: basic-tier user attempts k2r login → 402001 → webapp opens purchase page.
6. **Downgrade check**: family user registers router, admin downgrades tier to basic, user accesses `/api/router/quota` → 402001. `/api/tunnels` continues to work (no router-specific gate).

## Out of Scope

- Device attestation / hardware-bound tokens (would require Secure Enclave / TPM integration).
- License-key activation flow for router (the 2026-04-09 spec leaves this open for future).
- Encoding device class into the UDID itself (defense-in-depth option discussed during brainstorming, deferred because platformType is already an unforgeable signal from the k2r binary).
- JWT claim binding for class (would be necessary if header-trust is insufficient; not needed against the chosen weak-attacker threat model).
- Migration of existing `Device.IsGateway=false` rows for users who already ran k2r before this rollout — they'll be forced to re-login on next client upgrade, which is acceptable.
- Defending against a user who rebuilds k2r from modified source code with platformType="desktop".

## File Change Summary

### Backend (api/)

| File | Change |
|---|---|
| `middleware.go` | Extend `AppInfo` with `ClientClass`. Replace single `clientHeaderRegex` with new regex covering both product tokens. Add `IsGateway()` method on AppInfo. Split `fillDeviceAppInfo` into `createDeviceWithAppInfo` (writes IsGateway) and `refreshDeviceAppInfo` (never writes IsGateway). Remove all `X-App-Info` parsing. Rewrite `isGatewayRequest` as thin wrapper. Add `EnforceDeviceClass()` middleware. Add `classStr` helper. |
| `response.go` | Add `ErrorPlanNoRouter=402001`, `ErrorRouterDeviceLimit=403001`, `ErrorDeviceClassMismatch=403002`, `ErrorInvalidClientClass=422003`. |
| `logic_auth.go` | `checkDeviceLimitOrKick` returns the new specific codes instead of generic 402/403. |
| `route.go` | Mount `EnforceDeviceClass()` after `AuthRequired()` for device-bound chains. Register new `GET /api/router/quota` under `AuthRequired → EnforceDeviceClass → ProRequired → RouterRequired`. |
| `api_router_quota.go` (new) | Handler returning `{maxRouterDevice, maxLanClient}`. |
| `api_auth.go` | `login` and `login_password` paths use `createDeviceWithAppInfo` instead of `fillDeviceAppInfo` for the create branch. |
| Tests | New tests per testing matrix above. |

### Frontend (webapp/)

| File | Change |
|---|---|
| `src/services/cloud-api.ts` | `buildClientHeader()` reads `platformType` and emits `kaitu-{service\|router}`. Add 403002 → `authService.logout()` side effect. |
| `src/utils/errorCode.ts` | Register 4 new codes, add 4 `getErrorMessage` cases. |
| `src/i18n/locales/*/auth.json` | 4 new keys × 7 locales. |
| Tests | New Vitest cases per testing matrix. |

### Out

- No DB schema change (all fields already exist from 2026-04-09 spec).
- No k2r Go binary change.
- No mobile / desktop native bridge change.

## Rollout Plan

1. Server change ships first (parses both `kaitu-service` and `kaitu-router`; treats missing header as legacy app). Existing clients keep working.
2. Webapp change ships next (`buildClientHeader` reads `platformType`). At this point: new app installs send proper class; new k2r installs send `kaitu-router`; older clients with cached webapp keep sending nothing → still legacy-compatible.
3. After webapp rolls out, run a Center log audit for `device class mismatch` events. Investigate any spike; expected baseline is "0 except occasional copy-token attempts".
4. After 30 days, consider tightening the legacy bypass in `EnforceDeviceClass` (currently allows absent header to pass against any device). Track separately, not in this spec.

## Verification Before Merge

- All new tests pass.
- **Full regression**: `cd api && go test ./...` (with config.yml) and `cd webapp && yarn test` both clean — confirm no existing test broke. Pay special attention to `api_auth_test.go` device-transfer paths (line 235 / 799 region) — the `fillDeviceAppInfo` → `createDeviceWithAppInfo` rename must preserve transfer-recreate semantics.
- `cd webapp && npx tsc --noEmit` clean.
- Manual E2E checklist 1–6 above run on at least macOS + one mobile + one k2r.
- Existing webapp builds still log in successfully against the new server (no-header legacy path is intentional).
- Center log audit during canary: zero unexpected 403002 events from regular users (a small number from copy-token testing is expected).
