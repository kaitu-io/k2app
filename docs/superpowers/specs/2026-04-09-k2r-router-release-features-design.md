# k2r Router Release — Features Design (v4: One Product, Tiered Plans)

## Overview

Ship router support as a premium feature within the existing Kaitu VPN product. No separate "gateway product" — instead, higher-tier plans unlock router access with LAN client quotas. This spec covers: tiered plan model, router device management (MAC allowlist), OTA self-update, and all supporting UI/API changes.

Builds on the existing k2r build & distribution spec (2026-04-08) for binary embedding, CI, and install scripts.

**Target:** `feat/k2r-router-release` branch.

**Product decision:** Kaitu VPN is ONE product. Router is a premium feature, not a separate product. Users upgrade their plan tier to unlock router access.

---

## Terminology

| Term (中文) | English | Code identifier | Definition |
|-------------|---------|-----------------|------------|
| 套餐 | Plan | `Plan` | A purchasable tier. Defines price, duration, device quota, router device quota. |
| 订单 | Order | `Order` | A purchase transaction. Immutable record. |
| 设备 | Device | `Device` | Hardware that authenticates with Center (desktop/mobile/router). Subject to `User.MaxDevice`. |
| 路由器接入设备 | RouterDevice | `router-device-allowlist` | LAN device connecting through router's TPROXY. Subject to `User.MaxRouterDevice`. Identified by MAC address. |
| MAC 白名单 | RouterDevice Allowlist | `router-device-allowlist` | Locally stored allowed MAC addresses on the router. |

**Key distinction:** A router is one **Device** (occupies 1 `MaxDevice` slot). The phones/laptops connecting through the router are **RouterDevices** (subject to `MaxRouterDevice` quota). These are independent concepts.

---

## 1. Tiered Plan Model

### 1.1 Three Quota Dimensions

| Field | Meaning | Default |
|-------|---------|---------|
| `MaxDevice` | app 设备数量（手机/电脑/平板）— 不含路由器 | 5 |
| `MaxRouterDevice` | 路由器登录数量上限 | 0 (无路由器) |
| `MaxLanClient` | 路由器 LAN 接入设备数量上限 | 0 (无路由器) |

三个维度互不干涉。`MaxDevice` 不包含路由器。路由器是独立的登录设备类型。

### 1.2 Product Tiers

| Tier | PID pattern | MaxDevice | MaxRouterDevice | MaxLanClient | Target |
|------|------------|-----------|-----------------|-------------|--------|
| 经济版 | `lite-1m`, `lite-1y` | 1 | 0 | 0 | 学生/轻度用户 |
| 基础版 | `basic-1m`, `basic-1y` | 3 | 0 | 0 | 个人用户 |
| 家庭版 | `family-1m`, `family-1y` | 5 | 1 | 10 | 家庭 |
| 公司版 | `business-1m`, `business-1y` | 10 | 1 | -1 (unlimited) | 小型办公 |

### 1.3 Upgrade Path

```
经济版 → 基础版 → 家庭版 → 公司版
  │        │        │        │
  1设备    3设备    5设备     10设备
                   +1路由器   +1路由器
                   10 LAN     ∞ LAN
```

The key upsell: **"全家翻墙无需配置，升级家庭版"**. Router access is the premium hook that drives upgrades.

### 1.4 Backward Compatibility

Existing plans (1m, 1y, 2y, 3y, etc.) get `MaxDevice=5, MaxRouterDevice=0, MaxLanClient=0` via migration defaults. Existing users are unaffected.

---

## 2. Data Model Changes

### 2.1 Plan Model (MODIFIED)

```go
type Plan struct {
    // existing fields unchanged
    ID          uint64
    PID         string
    Label       string
    Price       uint64
    OriginPrice uint64
    Month       int
    Highlight   *bool
    IsActive    *bool

    // new fields
    MaxDevice       int `gorm:"not null;default:5"`  // app 设备数量（不含路由器）
    MaxRouterDevice int `gorm:"not null;default:0"`  // 路由器登录数量上限 (0=不支持路由器)
    MaxLanClient    int `gorm:"not null;default:0"`  // 路由器 LAN 接入数量上限 (0=不支持, -1=无限)
}
```

### 2.2 User Model (MODIFIED)

```go
type User struct {
    // existing fields — ExpiredAt and MaxDevice stay as-is
    ExpiredAt int64 `gorm:"index"`      // membership expiry (unchanged)
    MaxDevice int   `gorm:"default:5"`  // app 设备数量（不含路由器）(unchanged, now set from Plan)

    // new fields
    MaxRouterDevice int    `gorm:"not null;default:0"`          // 路由器登录数量上限
    MaxLanClient    int    `gorm:"not null;default:0"`          // LAN 接入数量上限 (0=无路由器, -1=无限)
    PlanPID         string `gorm:"type:varchar(30);default:''"` // 当前套餐 PID
}
```

**No Subscription table.** One product, one user = one set of entitlement fields. User table IS the subscription.

**Three quotas are independent dimensions:**
- `MaxDevice` = app 设备（手机/电脑/平板），不含路由器
- `MaxRouterDevice` = 路由器登录数量（当前所有套餐最多 1 台）
- `MaxLanClient` = 路由器上可以接入的 LAN 设备数量

### 2.3 Device Model (MODIFIED)

```go
type Device struct {
    // existing fields unchanged
    // new field
    IsGateway bool `gorm:"not null;default:false"` // router device flag
}
```

### 2.4 Database Schema Changes

| Table | Field | Type | Default | Note |
|-------|-------|------|---------|------|
| `plans` | `max_device` | `INT` | `5` | Existing plans get 5 |
| `plans` | `max_router_device` | `INT` | `0` | Existing plans get 0 |
| `plans` | `max_lan_client` | `INT` | `0` | Existing plans get 0 |
| `users` | `max_router_device` | `INT` | `0` | Existing users get 0 |
| `users` | `max_lan_client` | `INT` | `0` | Existing users get 0 |
| `users` | `plan_pid` | `VARCHAR(30)` | `''` | Existing users get empty |
| `devices` | `is_gateway` | `BOOLEAN` | `false` | Existing devices get false |

GORM AutoMigrate handles all additions. No data migration script needed — defaults are backward compatible.

---

## 3. Order Processing Changes

### 3.1 applyOrderToTargetUsers

When an order is paid, `applyOrderToTargetUsers` currently extends `User.ExpiredAt` via `addProExpiredDays`. Changes:

```go
// Before addProExpiredDays call, set quota fields:
user.MaxDevice = plan.MaxDevice
user.MaxRouterDevice = plan.MaxRouterDevice
user.MaxLanClient = plan.MaxLanClient
user.PlanPID = plan.PID
// addProExpiredDays calls tx.Save(user) — all fields written atomically
```

**Note:** `addProExpiredDays` (line 65) calls `tx.Save(user)` which writes ALL user fields. So we just set the fields before that Save call — they'll be persisted automatically.

### 3.2 Plan Upgrade Semantics

When user buys a higher-tier plan:
- `ExpiredAt` is extended (existing behavior)
- `MaxDevice` is updated to new plan's value
- `MaxRouterDevice` is updated to new plan's value
- `PlanPID` is updated

When user renews same plan:
- `ExpiredAt` is extended
- Quotas unchanged (same plan)

---

## 4. Auth & Middleware Changes

### 4.1 ProRequired — UNCHANGED

```go
func ProRequired() gin.HandlerFunc {
    // Exactly as before: checks user.IsExpired()
    // No changes needed
}
```

### 4.2 RouterRequired — NEW

```go
// RouterRequired checks that user has router access (MaxRouterDevice > 0)
func RouterRequired() gin.HandlerFunc {
    return func(c *gin.Context) {
        user := ReqUser(c)
        if user == nil {
            Error(c, ErrorNotLogin, "authentication failed")
            c.Abort()
            return
        }
        if user.IsExpired() {
            Error(c, ErrorPaymentRequired, "membership expired")
            c.Abort()
            return
        }
        if user.MaxRouterDevice == 0 {
            Error(c, ErrorPaymentRequired, "router access requires Family plan or above")
            c.Abort()
            return
        }
        c.Next()
    }
}
```

### 4.3 Device Limit — Split by Device Type

Device limit checking splits into two paths:

```go
// app 设备登录 (IsGateway=false)
if appDeviceCount >= user.MaxDevice { kick oldest app device }

// 路由器登录 (IsGateway=true)
if user.MaxRouterDevice == 0 { reject: "套餐不支持路由器" }
if routerDeviceCount >= user.MaxRouterDevice { reject: "路由器数量已达上限" }
```

The existing device limit code in `api_auth.go` (line ~291, ~776) currently counts ALL devices. Must change to count only `WHERE is_gateway = false` for app device login, and count only `WHERE is_gateway = true` for router login.

### 4.4 Gateway Device Registration

k2r sends `X-App-Info` JSON header with `isGateway: true`. `fillDeviceAppInfo` parses it and sets `Device.IsGateway = true`.

---

## 5. Plans API Changes

### 5.1 GET /api/plans — UNCHANGED

Returns all active plans. Frontend renders them as tier cards. No `product_type` filter needed — one product.

### 5.2 Frontend Plan Type

```typescript
interface Plan {
    pid: string;
    label: string;
    price: number;
    originPrice: number;
    month: number;
    highlight: boolean;
    maxDevice: number;       // app 设备数量（不含路由器）
    maxRouterDevice: number; // 路由器登录数量上限 (0=无路由器)
    maxLanClient: number;    // LAN 接入数量上限 (0=无路由器, -1=无限)
}
```

### 5.3 Admin Plans CRUD

Admin creates plans with `MaxDevice` and `MaxRouterDevice` fields. No `ProductType` needed.

---

## 6. Purchase Page Design

### 6.1 One Unified Purchase Page

No tabs, no product switching. All tiers shown on one page:

```
┌─────────────────────────────────────────────────────┐
│  选择套餐                                            │
│                                                      │
│  ── 月付 ──                                          │
│  ┌──────┐ ┌──────┐ ┌──────────┐ ┌──────────┐       │
│  │个人版│ │专业版│ │ 家庭版   │ │ 旗舰版   │       │
│  │3设备 │ │5设备 │ │ 5设备    │ │ 10设备   │       │
│  │      │ │      │ │+路由器10 │ │+路由器∞  │       │
│  │$x/月 │ │$y/月 │ │ $z/月   │ │ $w/月    │       │
│  └──────┘ └──────┘ └──────────┘ └──────────┘       │
│                                                      │
│  ── 年付 (省 XX%) ──                                 │
│  ┌──────┐ ┌──────┐ ┌──────────┐ ┌──────────┐       │
│  │个人版│ │专业版│ │ 家庭版   │ │ 旗舰版   │       │
│  │$xx/年│ │$yy/年│ │ $zz/年  │ │ $ww/年   │       │
│  └──────┘ └──────┘ └──────────┘ └──────────┘       │
│                                                      │
│  会员权益 (根据选中档位动态显示)                      │
│                                                      │
│  ▸ 选中个人版: 3台设备 / 全球节点 / 免运维            │
│  ▸ 选中家庭版: 5台设备 + 10台路由器接入 / 全球节点   │
│                                                      │
│  优惠码 / 支付按钮                                   │
└─────────────────────────────────────────────────────┘
```

### 6.2 Plan Card — Show Router Badge

Plans with `maxRouterDevice > 0` show router icon and client count:

```tsx
{plan.maxRouterDevice !== 0 && (
  <Chip
    icon={<RouterIcon />}
    label={plan.maxRouterDevice === -1 ? "路由器∞" : `路由器${plan.maxRouterDevice}台`}
    size="small"
    color="primary"
  />
)}
```

### 6.3 Dynamic Membership Benefits

Benefits change based on selected plan tier:

| Plan selected | Benefits shown |
|--------------|----------------|
| 个人版/专业版 | N 台设备 / 全球节点 / 免运维 / 持续优化 / 优先支持 |
| 家庭版/旗舰版 | N 台设备 + M 台路由器接入 / 全家共享一键上网 / 透明代理 / 全球节点 / ... |

### 6.4 Webapp vs Website

| Platform | Behavior |
|----------|----------|
| Webapp (all platforms) | One unified purchase page, all tiers |
| Website | Same — one unified page, all tiers |

**No tab switching needed.** The website purchase page is identical to webapp's tier layout.

### 6.5 Gateway-Specific Entry Point

When user accesses purchase from k2r webapp (`platformType === 'gateway'`) and their current plan has `maxRouterDevice === 0`:
- Show a banner: "当前套餐不支持路由器，请升级到家庭版或旗舰版"
- Auto-highlight the 家庭版 plan (first tier with router access)

---

## 7. RouterDevice Management (MAC Allowlist)

**Unchanged from previous spec version.** Sections 6.1-6.7 of the previous spec apply as-is. The only difference: quota comes from `User.MaxRouterDevice` instead of `Subscription.Quota`.

### 7.1 Storage

Local on router in `/etc/k2r/storage.json`. Modes: `"open"` (default) or `"allowlist"`.

### 7.2 Gateway HTTP API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/router-devices` | GET | List LAN devices + allowlist status + quota |
| `/api/router-devices/allow` | POST | Add MAC `{mac, remark}` |
| `/api/router-devices/remove` | POST | Remove MAC `{mac}` |
| `/api/router-devices/mode` | POST | Switch mode `{mode}` |

### 7.3 Quota Source

k2r authenticates with Center → receives user profile including `MaxLanClient`. Cached locally. Refreshed on each status poll. This is the LAN client allowlist limit.

### 7.4 LAN Device Discovery

Platform-aware: ubus (OpenWrt) → dnsmasq leases → ip neigh fallback.

### 7.5 nftables Enforcement

Allowlist mode: `ether saddr != @allowed_router_devices drop` in prerouting chain.

### 7.6 DNS Redirect

When `dns_redirect: true`: nftables redirect port 53 to k2r DNS resolver. Essential for smart routing.

### 7.7 Webapp UI — Router Device Management Page

New page at `/router-devices` (gateway only). Shows mode toggle, quota, online/offline device lists, allow/remove/remark actions.

---

## 8. OTA Self-Update

**Unchanged from previous spec version.** CDN check, download, SHA256 verify, backup to .bak, atomic replace, service restart. Persistent `UpdateState` for crash recovery.

---

## 9. Gateway Device Registration

k2r sends `X-App-Info: {"version":"0.4.2","platform":"linux","arch":"arm64","isGateway":true}`. `fillDeviceAppInfo` parses and sets `Device.IsGateway = true`.

---

## 10. Subscription Expiry Behavior

### 10.1 Membership Expired

Same as current: `ProRequired` returns 402, user sees "授权已过期" with purchase link.

### 10.2 Router Access on Expired Membership

Router is a feature of the membership. **Membership expired = no router access.** k2r checks expiry on every connect attempt.

### 10.3 Plan Without Router Access

User on 个人版/专业版 tries to use k2r → `RouterRequired` returns 402: "router access requires Family plan or above". User sees upgrade prompt.

---

## 11. Webapp Conditional Rendering

Based on `window._platform.platformType === 'gateway'`:

| Component | Desktop/Mobile | Gateway |
|-----------|---------------|---------|
| Updater | Tauri/Capacitor updater | Gateway updater |
| Service reinstall | Show | Hide |
| ADB install helper | Show (desktop) | Hide |
| Proxy mode toggle | Show | Hide (always TPROXY) |
| TUN mode toggle | Show | Hide |
| Router device management | Hide | Show |
| LAN/DNS settings | Hide | Show |
| Interceptor status | Hide | Show (nftables/iptables) |
| Purchase plans | All tiers | All tiers (highlight Family+) |
| Member selection | Show | Hide |

---

## 12. Website Changes

### 12.1 Install Page — Router Tab

`kaitu.io/install` adds 「路由器」tab with one-line install command and architecture info.

### 12.2 Purchase Page — Unified Tiers

One page, all tiers. Plans with `maxRouterDevice > 0` show router badge. No tab switching.

---

## 13. Admin Dashboard Changes

### 13.1 Plan Management

Plans table adds columns: `MaxDevice`, `MaxRouterDevice`. Create/edit form adds these fields. No `ProductType` filter needed.

### 13.2 Device Statistics

`api_admin_device_stats.go` adds `is_gateway` dimension.

### 13.3 User Detail — Device List

User detail shows all devices including routers (with IsGateway badge). Shows current plan tier.

---

## 14. CI

- Uncomment `v*` tag auto-trigger in `.github/workflows/release-openwrt.yml`
- Verify CDN upload path

---

## i18n Keys (New)

### Webapp — purchase namespace additions

```json
{
  "purchase": {
    "features": {
      "routerDeviceAccess": "最多 {{count}} 台路由器接入设备",
      "routerDeviceUnlimited": "不限路由器接入设备",
      "routerDeviceAccessDesc": "全家共享一键上网",
      "transparentProxy": "透明代理无需配置",
      "transparentProxyDesc": "LAN 设备自动代理，无需逐台配置"
    },
    "upgradeForRouter": "当前套餐不支持路由器，请升级到家庭版",
    "tierBasic": "个人版",
    "tierPro": "专业版",
    "tierFamily": "家庭版",
    "tierUltimate": "旗舰版"
  }
}
```

### Webapp — routerDevice namespace (NEW)

Same as previous spec version — 20+ keys for device management UI.

---

## Out of Scope

- Per-router-device traffic statistics
- Multi-account on single router
- ipk native OpenWrt package format
- Home hub (LAN auto-discovery, app uses router tunnel) — future spec
- Family management via app (manage router allowlist from phone) — future spec
- Automatic OTA rollback (manual recovery via SSH)

---

## File Change Summary

### Backend (api/)

| File | Change |
|------|--------|
| `model.go` | Add `Plan.MaxDevice/MaxRouterDevice/MaxLanClient`, `User.MaxRouterDevice/MaxLanClient/PlanPID`, `Device.IsGateway` |
| `type.go` | Update `DataPlan` (add 3 quota fields), update `DataUser` (add maxRouterDevice, maxLanClient, planPid), `AppInfo` (add isGateway) |
| `api_plan.go` | Update DataPlan construction to include new fields |
| `api_admin_plan.go` | Add MaxDevice/MaxRouterDevice to CRUD |
| `logic_member.go` | `applyOrderToTargetUsers` also writes MaxDevice, MaxRouterDevice, PlanPID |
| `middleware.go` | Add `RouterRequired()`, update `fillDeviceAppInfo` for isGateway |
| `api_user.go` | Profile returns maxRouterDevice, planPid |
| `api_admin_device_stats.go` | Add `is_gateway` dimension |
| `migrate.go` | Add new fields to AutoMigrate (GORM handles column additions) |

### k2 submodule (gateway/)

| File | Change |
|------|--------|
| `gateway/intercept_nft.go` | Rewrite with google/nftables. Add MAC set, DNS redirect |
| `gateway/intercept_ipt.go` | Add MAC filter + DNS redirect to iptables fallback |
| `gateway/discovery.go` | New: platform-aware LAN device discovery |
| `gateway/router_device.go` | New: allowlist CRUD, API handlers, quota enforcement |
| `gateway/updater.go` | New: OTA self-updater with state recovery |
| `gateway/api.go` | Register new endpoints |

### Webapp (webapp/)

| File | Change |
|------|--------|
| `src/services/api-types.ts` | Plan adds maxDevice, maxRouterDevice |
| `src/pages/Purchase.tsx` | Tier-based display, dynamic benefits, gateway upgrade banner |
| `src/pages/RouterDevices.tsx` | New: router device management page |
| `src/components/MembershipBenefits.tsx` | Dynamic benefits based on selected plan tier |
| `src/services/gateway-k2.ts` | Updater + router-device actions |
| `src/i18n/locales/*/purchase.json` | Tier names + router feature text |
| `src/i18n/locales/*/routerDevice.json` | New namespace |

### Website (web/)

| File | Change |
|------|--------|
| `src/app/[locale]/purchase/PurchaseClient.tsx` | Tier-based display (no tabs needed) |
| `src/app/[locale]/install/InstallClient.tsx` | Router tab |
| `src/app/(manager)/manager/plans/page.tsx` | MaxDevice/MaxRouterDevice columns |
| `src/lib/api.ts` | Update Plan type |
| `messages/*/purchase.json` | Tier names + router badges |
