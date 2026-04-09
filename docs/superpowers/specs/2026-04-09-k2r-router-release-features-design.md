# k2r Router Release — Features Design

## Overview

Ship the complete k2r router product: gateway plan pricing, router device management (MAC allowlist), OTA self-update, and all supporting UI/API/admin changes. This spec builds on the existing k2r build & distribution spec (2026-04-08) which covers binary embedding, CI, and install scripts.

**Target:** `feat/k2r-router-release` branch.

## Terminology

| Term (中文) | English | Code identifier | Definition |
|-------------|---------|-----------------|------------|
| 设备 | Device | `Device`, `MaxDevice` | Hardware that authenticates with Center (desktop/mobile/router). Subject to `User.MaxDevice` limit. |
| 路由器接入设备 | RouterDevice | `MaxRouterDevice`, `router-device-allowlist` | LAN device connecting through router's TPROXY proxy. Subject to `User.MaxRouterDevice` limit. Identified by MAC address. |
| MAC 白名单 | RouterDevice Allowlist | `router-device-allowlist` | Locally stored list of allowed MAC addresses on the router. |

**Key distinction:** A router is one **Device** (occupies 1 `MaxDevice` slot). The phones/laptops connecting through the router are **RouterDevices** (subject to `MaxRouterDevice` quota). These are independent dimensions.

---

## 1. Data Model Changes

### 1.1 Plan Model

```go
type Plan struct {
    // existing fields unchanged
    ID          uint64
    PID         string    // "1m", "1y", "router-monthly-5", etc.
    Label       string
    Price       uint64    // cents
    OriginPrice uint64    // cents
    Month       int
    Highlight   *bool
    IsActive    *bool

    // new fields
    ProductType    string `gorm:"type:varchar(20);not null;default:'personal'"` // "personal" | "gateway"
    MaxDevice      int    `gorm:"not null;default:5"`                           // login device limit
    MaxRouterDevice int   `gorm:"not null;default:0"`                           // router device limit (gateway only, 0=unlimited)
}
```

- `ProductType` separates product lines. API filters by this field.
- `MaxDevice` enables future per-plan device tiers for personal plans. Existing plans default to 5 (backward compatible).
- `MaxRouterDevice` only meaningful for gateway plans. Personal plans have 0 (not applicable).

### 1.2 User Model

```go
type User struct {
    // existing fields unchanged
    MaxDevice int `gorm:"not null;default:5"`

    // new field
    MaxRouterDevice int `gorm:"not null;default:0"` // 0 = no router subscription
}
```

When a user purchases a plan, `applyOrderToTargetUsers()` writes `plan.MaxDevice` → `user.MaxDevice` and `plan.MaxRouterDevice` → `user.MaxRouterDevice`.

### 1.3 Device Model

```go
type Device struct {
    // existing fields unchanged
    UDID        string
    UserID      uint64
    AppPlatform string // remains "linux" for routers (system fact)
    AppArch     string

    // new field
    IsGateway bool `gorm:"not null;default:false"` // router device flag
}
```

`IsGateway` is an independent dimension from `AppPlatform`. Enables cross-query: `platform=linux AND is_gateway=true`.

### 1.4 Database Migration

| Table | Field | Type | Default | Note |
|-------|-------|------|---------|------|
| `plans` | `product_type` | `VARCHAR(20)` | `'personal'` | Existing plans auto-get `'personal'` |
| `plans` | `max_device` | `INT` | `5` | Matches current hardcoded default |
| `plans` | `max_router_device` | `INT` | `0` | 0 = not applicable for personal |
| `users` | `max_router_device` | `INT` | `0` | 0 = no router subscription |
| `devices` | `is_gateway` | `BOOLEAN` | `false` | All existing devices get `false` |

GORM AutoMigrate handles all additions. No manual SQL required.

### 1.5 Example Gateway Plans

| PID | Label | Month | MaxRouterDevice | Price |
|-----|-------|-------|-----------------|-------|
| `router-monthly-5` | 路由器月付·5设备 | 1 | 5 | TBD |
| `router-yearly-5` | 路由器年付·5设备 | 12 | 5 | TBD |
| `router-monthly-15` | 路由器月付·15设备 | 1 | 15 | TBD |
| `router-yearly-15` | 路由器年付·15设备 | 12 | 15 | TBD |
| `router-monthly-unlimited` | 路由器月付·不限 | 1 | 0 | TBD |
| `router-yearly-unlimited` | 路由器年付·不限 | 12 | 0 | TBD |

All gateway plans have `ProductType = "gateway"`, `MaxDevice = 1` (router itself occupies 1 device slot). The `Plan.MaxDevice` default of 5 applies to personal plans; gateway plans are explicitly created with `MaxDevice = 1` via admin. Prices are set by admin — TBD values are not spec gaps.

---

## 2. Plans API Changes

### 2.1 Public Plans Endpoint

```
GET /api/plans                          → returns personal plans (backward compatible)
GET /api/plans?product_type=gateway     → returns gateway plans
```

Implementation: add `product_type` query filter. Default to `"personal"` when absent.

### 2.2 Admin Plans Endpoints

Plans admin CRUD endpoints gain `ProductType`, `MaxDevice`, `MaxRouterDevice` fields in create/update requests. Admin plans list supports `product_type` filter.

### 2.3 Frontend Plan Type

```typescript
interface Plan {
    pid: string;
    label: string;
    price: number;
    originPrice: number;
    month: number;
    highlight: boolean;
    productType: 'personal' | 'gateway';
    maxDevice: number;
    maxRouterDevice: number;
}
```

---

## 3. Purchase Page Design

### 3.1 Platform-Based Plan Display

| Context | What shows | Tab UI |
|---------|-----------|--------|
| Webapp in desktop/mobile (`platformType: 'desktop'/'mobile'`) | Personal plans only | No tab |
| Webapp in router (`platformType: 'gateway'`) | Gateway plans only | No tab |
| Website (browser) | Both product lines | `[个人版] [路由器版]` tab switcher |

**Principle:** Embedded webapp shows only the product matching its platform. Website as independent entry shows all products.

### 3.2 Website Tab Behavior

- Default tab: 个人版
- URL parameter `?product=gateway` selects 路由器版 tab
- Tab switch triggers `getPlans({ product_type })` re-fetch
- Selected tab persists in URL (pushState) for shareability

### 3.3 Gateway Plan Card Layout

Gateway plans have two dimensions: **duration** (month) and **router device quota** (MaxRouterDevice). Cards are grouped by quota tier, sorted by duration within each tier:

```
── 5 台设备接入 ──
┌──────────────┐ ┌──────────────┐
│ 1个月        │ │ 1年          │
│ $x           │ │ $x   热门    │
│ 最多5台设备  │ │ 最多5台设备  │
└──────────────┘ └──────────────┘

── 15 台设备接入 ──
┌──────────────┐ ┌──────────────┐
│ 1个月        │ │ 1年          │
│ $x           │ │ $x           │
│ 最多15台设备 │ │ 最多15台设备 │
└──────────────┘ └──────────────┘

── 不限设备 ──
┌──────────────┐ ┌──────────────┐
│ 1个月        │ │ 1年          │
│ $x           │ │ $x           │
│ 不限设备数量 │ │ 不限设备数量 │
└──────────────┘ └──────────────┘
```

### 3.4 Gateway Membership Benefits

Distinct from personal plan benefits:

| Personal | Gateway |
|----------|---------|
| X 台设备同时使用 | X 台设备接入 / 不限设备 |
| 一个账号，全家共享 | 全家共享一键上网 |
| 全球智能节点 | 全球智能节点 |
| 免运维 | 透明代理无需配置 |
| 持续优化 | 持续优化 |
| 优先技术支持 | 优先技术支持 |

### 3.5 Gateway Purchase Flow

Same order creation flow as personal plans:
1. Select gateway plan → `CreateOrderRequest.Plan = "router-monthly-5"`
2. Preview → calculates price with optional campaign code
3. Pay → Wordgate redirect → webhook → `applyOrderToTargetUsers()` sets `MaxRouterDevice`

**Difference:** Member selection is hidden for gateway plans. Router subscription binds to the purchasing account only (no delegation).

---

## 4. RouterDevice Management (MAC Allowlist)

### 4.1 Storage

Stored locally on router in `/etc/k2r/storage.json` (existing encrypted storage):

```json
{
  "router-device-allowlist": "[{\"mac\":\"AA:BB:CC:DD:EE:FF\",\"remark\":\"iPhone\",\"addedAt\":1712600000}]",
  "router-device-allowlist-mode": "allowlist"
}
```

- `router-device-allowlist-mode`: `"open"` (all LAN devices proxied) or `"allowlist"` (only listed MACs)
- Default: `"open"` (new router installs unrestricted)

### 4.2 Gateway HTTP API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/router-devices` | GET | List LAN devices (ARP scan) + allowlist status + quota |
| `/api/router-devices/allow` | POST | Add MAC to allowlist `{mac, remark}` |
| `/api/router-devices/remove` | POST | Remove MAC from allowlist `{mac}` |
| `/api/router-devices/mode` | POST | Switch mode `{mode: "allowlist" \| "open"}` |

### 4.3 GET /api/router-devices Response

```json
{
  "mode": "allowlist",
  "maxRouterDevice": 15,
  "routerDevices": [
    {
      "mac": "AA:BB:CC:DD:EE:FF",
      "ip": "192.168.1.100",
      "hostname": "iPhone",
      "online": true,
      "allowed": true,
      "remark": "爸爸的手机"
    },
    {
      "mac": "11:22:33:44:55:66",
      "ip": "192.168.1.101",
      "hostname": "laptop",
      "online": true,
      "allowed": false,
      "remark": ""
    }
  ]
}
```

- `online`: present in ARP table (currently connected to LAN)
- `allowed`: present in allowlist
- `maxRouterDevice`: quota from Center, `0` = unlimited
- Allowlist entry count cannot exceed `maxRouterDevice` (when `maxRouterDevice > 0`)

### 4.4 LAN Device Discovery

ARP table scan to find devices on LAN:
- Read `/proc/net/arp` or `ip neigh show`
- Filter to configured LAN subnets only
- Resolve hostname via reverse DNS where available
- Merge with allowlist data (remark, allowed status)

### 4.5 nftables Enforcement

**Allowlist mode:**

```nft
table inet k2r {
    set allowed_router_devices {
        type ether_addr
        elements = { AA:BB:CC:DD:EE:FF, 11:22:33:44:55:66 }
    }
    chain tproxy_prerouting {
        # existing TPROXY rules ...
        ether saddr != @allowed_router_devices drop
    }
}
```

**Open mode:** The `ether saddr` rule is absent. All LAN traffic is proxied.

When allowlist changes, atomically update the nftables set (add/delete elements) without disrupting existing connections.

### 4.6 MaxRouterDevice Quota Enforcement

- k2r fetches `user.MaxRouterDevice` from Center at login and caches locally
- When `maxRouterDevice > 0` and user tries to add a MAC that would exceed quota → return error
- Enforcement is local only (gateway-side). Center does not validate MAC lists.

### 4.7 Webapp UI — Router Device Management Page

New page at `/router-devices` (gateway only, hidden on desktop/mobile):

```
┌─────────────────────────────────────────┐
│  路由器设备管理                          │
│                                          │
│  模式: [开放] [白名单]     配额: 3/15    │
│                                          │
│  ── 在线设备 ──                          │
│  ┌─────────────────────────────────────┐ │
│  │ 📱 iPhone (爸爸的手机)              │ │
│  │ AA:BB:CC:DD:EE:FF · 192.168.1.100  │ │
│  │ ✅ 已允许              [移除]       │ │
│  ├─────────────────────────────────────┤ │
│  │ 💻 laptop                           │ │
│  │ 11:22:33:44:55:66 · 192.168.1.101  │ │
│  │ ⛔ 未允许         [允许] [备注]     │ │
│  └─────────────────────────────────────┘ │
│                                          │
│  ── 离线设备（白名单中）──               │
│  ┌─────────────────────────────────────┐ │
│  │ 🖥 妈妈的iPad                       │ │
│  │ CC:DD:EE:FF:00:11                   │ │
│  │ ✅ 已允许              [移除]       │ │
│  └─────────────────────────────────────┘ │
└─────────────────────────────────────────┘
```

Navigation: Add to bottom tab bar or settings sub-page (gateway only).

---

## 5. OTA Self-Update

### 5.1 CDN Structure

```
kaitu/k2r/
├── LATEST                        # "0.4.3"
├── 0.4.3/
│   ├── k2r-linux-arm64
│   ├── k2r-linux-amd64
│   ├── k2r-linux-armv7
│   ├── k2r-linux-mipsle
│   └── checksums.txt             # SHA256
```

### 5.2 Update Flow

```
Gateway startup or periodic check (every 6 hours) or manual trigger
    │
    ▼
GET kaitu/k2r/LATEST → compare with current version
    │
    ├─ no update → {hasUpdate: false}
    │
    ▼ has update
{hasUpdate: true, currentVersion, newVersion}
    │
    ▼ user confirms (or auto-update if configured)
Download k2r-linux-{arch} → /tmp/k2r-update
    │
Verify SHA256 against checksums.txt
    │
Atomic replace /usr/bin/k2r (write tmp + rename)
    │
Restart service (systemctl restart k2r / /etc/init.d/k2r restart)
```

### 5.3 Gateway HTTP API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/updater/check` | POST | Check for update. Returns `{hasUpdate, currentVersion, newVersion}` |
| `/api/updater/apply` | POST | Download + verify + replace + restart |
| `/api/updater/status` | GET | SSE stream: `{stage, progress, error}` |

Stage enum: `downloading` → `verifying` → `replacing` → `restarting` → `done` / `error`

### 5.4 Webapp Integration

Gateway's `IPlatform.updater` is now implemented (previously `undefined`):

```typescript
const gatewayUpdater: IUpdater = {
    isUpdateReady: false,
    updateInfo: null,
    isChecking: false,
    error: null,
    channel: 'stable',

    checkForUpdates()   // → POST /api/updater/check
    applyUpdate()       // → POST /api/updater/apply + listen SSE progress
    dismissUpdate()     // → dismiss UI prompt
}
```

The existing webapp updater UI (settings page update check, update banner) works out of the box. No new UI components needed.

**Previous design said "hide updater for gateway" — this is now reversed: gateway has updater enabled.**

---

## 6. Gateway Device Registration

### 6.1 Auth Request

k2r includes `isGateway: true` in the `X-App-Info` header:

```json
{"version": "0.4.2", "platform": "linux", "arch": "arm64", "isGateway": true}
```

### 6.2 Middleware Change

`middleware.go` parses `isGateway` from `X-App-Info` and sets `Device.IsGateway = true` during device registration/update in both OTP and password auth paths.

---

## 7. Webapp Conditional Rendering

Based on `window._platform.platformType === 'gateway'`:

| Component | Desktop/Mobile | Gateway |
|-----------|---------------|---------|
| Updater | Tauri/Capacitor updater | Gateway updater (Section 5) |
| Service reinstall | Show | Hide |
| ADB install helper | Show (desktop) | Hide |
| Proxy mode toggle | Show | Hide (always TPROXY) |
| TUN mode toggle | Show | Hide |
| Router device management | Hide | Show (Section 4) |
| LAN/DNS settings | Hide | Show |
| Interceptor status | Hide | Show (nftables/iptables) |
| Purchase plans | Personal plans | Gateway plans |
| Member selection (purchase) | Show | Hide |

---

## 8. Website Changes

### 8.1 Install Page — Router Tab

`kaitu.io/install` adds a 「路由器」tab:

Content:
- One-line install command: `wget -qO- https://kaitu.io/i/k2r | sh`
- Supported architectures (aarch64/x86_64/armv7/mipsle)
- OpenWrt / soft-router / NAS installation guide
- Post-install: access `http://<router-ip>:1779`

### 8.2 Purchase Page — Product Tabs

`kaitu.io/purchase` adds `[个人版] [路由器版]` tab switcher. Default: 个人版. URL param `?product=gateway` selects router tab. Each tab fetches plans with corresponding `product_type`.

---

## 9. Admin Dashboard Changes

### 9.1 Plan Management

- Plans table adds columns: `ProductType`, `MaxDevice`, `MaxRouterDevice`
- Create/edit form adds these fields
- Filter by `product_type` dropdown

### 9.2 Device Statistics

- `api_admin_device_stats.go` adds `is_gateway` dimension
- Admin dashboard can filter by gateway / non-gateway devices

---

## 10. CI

- Uncomment `v*` tag auto-trigger in `.github/workflows/release-openwrt.yml`
- Verify CDN upload path `kaitu/k2r/{VERSION}/` + `LATEST` + `checksums.txt`

---

## i18n Keys (New)

### Webapp — purchase namespace

```json
{
  "purchase": {
    "features": {
      "routerDeviceAccess": "最多 {{count}} 台设备接入",
      "routerDeviceUnlimited": "不限设备数量",
      "routerDeviceAccessDesc": "全家共享一键上网",
      "transparentProxy": "透明代理无需配置",
      "transparentProxyDesc": "LAN 设备自动代理，无需逐台配置"
    }
  }
}
```

### Webapp — new router-device namespace

```json
{
  "routerDevice": {
    "title": "路由器设备管理",
    "modeOpen": "开放",
    "modeAllowlist": "白名单",
    "quota": "配额",
    "quotaDisplay": "{{used}}/{{max}}",
    "quotaUnlimited": "不限",
    "online": "在线设备",
    "offline": "离线设备（白名单中）",
    "allowed": "已允许",
    "notAllowed": "未允许",
    "allow": "允许",
    "remove": "移除",
    "remark": "备注",
    "addRemark": "添加备注",
    "quotaExceeded": "已达到设备配额上限",
    "confirmRemove": "确认从白名单中移除此设备？",
    "switchToAllowlist": "切换到白名单模式后，只有白名单中的设备可以接入",
    "switchToOpen": "切换到开放模式后，所有 LAN 设备均可接入"
  }
}
```

### Website — purchase namespace

```json
{
  "purchase": {
    "productPersonal": "个人版",
    "productGateway": "路由器版",
    "routerDeviceAccess": "最多 {count} 台设备接入",
    "routerDeviceUnlimited": "不限设备数量"
  }
}
```

---

## Out of Scope

- Per-router-device traffic statistics (nftables counters per source MAC)
- Multi-account on single router (multiple k2v5 connections)
- ipk native OpenWrt package format
- Linux desktop OTA (Tauri AppImage already has tauri-plugin-updater)
- Router device list sync to Center (allowlist is local-only)

---

## File Change Summary

### Backend (api/)

| File | Change |
|------|--------|
| `model.go` | Add `Plan.ProductType`, `Plan.MaxDevice`, `Plan.MaxRouterDevice`, `User.MaxRouterDevice`, `Device.IsGateway` |
| `type.go` | Add fields to `DataPlan`, `AppInfo` |
| `api_plan.go` | Add `product_type` query filter |
| `api_admin_plan.go` | Add new fields to create/update |
| `api_order.go` | `applyOrderToTargetUsers()` writes `MaxDevice` + `MaxRouterDevice` |
| `middleware.go` | Parse `isGateway` from `X-App-Info`, set `Device.IsGateway` |
| `api_admin_device_stats.go` | Add `is_gateway` dimension |
| `response.go` | No change (reuse existing error codes) |

### k2 submodule (gateway/)

| File | Change |
|------|--------|
| `gateway/api.go` | Add `/api/router-devices`, `/api/updater/*` endpoints |
| `gateway/router_device.go` | New: allowlist CRUD, ARP scan, nftables set management |
| `gateway/updater.go` | New: CDN check, download, verify, replace, restart |
| `gateway/intercept_nft.go` | Add `allowed_router_devices` set, conditional MAC filter rule |
| `gateway/intercept_ipt.go` | Equivalent iptables MAC filter |

### Webapp (webapp/)

| File | Change |
|------|--------|
| `src/pages/Purchase.tsx` | Filter plans by `platformType` → `product_type` |
| `src/pages/RouterDevices.tsx` | New: router device management page |
| `src/services/gateway-k2.ts` | Add updater implementation, router-device API calls |
| `src/services/api-types.ts` | Add `productType`, `maxDevice`, `maxRouterDevice` to Plan type |
| `src/components/MembershipBenefits.tsx` | Gateway-specific benefits |
| `src/i18n/locales/*/purchase.json` | Gateway plan feature text |
| `src/i18n/locales/*/routerDevice.json` | New namespace |
| Various components | `platformType === 'gateway'` conditional rendering |

### Website (web/)

| File | Change |
|------|--------|
| `src/app/[locale]/purchase/PurchaseClient.tsx` | Add product tab switcher, gateway plan display |
| `src/app/[locale]/install/` | Add router tab |
| `src/app/(manager)/manager/plans/page.tsx` | Add ProductType/MaxDevice/MaxRouterDevice columns |
| `src/lib/api.ts` | Add `product_type` param to `getPlans()` |
| `messages/*/purchase.json` | Gateway product labels |

### CI

| File | Change |
|------|--------|
| `.github/workflows/release-openwrt.yml` | Uncomment `v*` tag trigger |
