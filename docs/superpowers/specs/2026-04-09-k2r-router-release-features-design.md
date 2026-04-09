# k2r Router Release — Features Design

## Overview

Ship the complete k2r router product: Subscription architecture, gateway plan pricing, router device management (MAC allowlist), OTA self-update, and all supporting UI/API/admin changes. This spec builds on the existing k2r build & distribution spec (2026-04-08) which covers binary embedding, CI, and install scripts.

**Target:** `feat/k2r-router-release` branch.

---

## Terminology

| Term (中文) | English | Code identifier | Definition |
|-------------|---------|-----------------|------------|
| 产品 | Product | `ProductType` | A product line: `"personal"` (app版) or `"gateway"` (路由器版). String enum, not a table. |
| 套餐 | Plan | `Plan` | A purchasable offering belonging to a Product. Defines price, duration, quota. |
| 订阅 | Subscription | `Subscription` | A user's active entitlement for a Product. One per user per product. Source of truth for access and quota. |
| 订单 | Order | `Order` | A purchase transaction. Immutable record. Creates or renews a Subscription on payment. |
| 设备 | Device | `Device` | Hardware that authenticates with Center (desktop/mobile/router). Subject to Subscription quota. |
| 路由器接入设备 | RouterDevice | `router-device-allowlist` | LAN device connecting through router's TPROXY proxy. Subject to gateway Subscription quota. Identified by MAC address. |
| MAC 白名单 | RouterDevice Allowlist | `router-device-allowlist` | Locally stored list of allowed MAC addresses on the router. |

**Key distinction:** A router is one **Device** (occupies 1 device quota in gateway Subscription). The phones/laptops connecting through the router are **RouterDevices** (subject to gateway Subscription quota). These are independent concepts.

---

## 1. Entity Relationships

```
Product (string enum: "personal" | "gateway")
    │
    ├── 1:N  Plan (一个产品有多个套餐)
    │          │
    │          └── 1:N  Order (一个套餐被多次购买)
    │
    └── 1:1  Subscription (per User) (一个用户对一个产品最多一个订阅)

User
    ├── 1:N  Device (登录设备)
    ├── 1:N  Order (订单历史)
    └── 1:N  Subscription (每个 ProductType 最多1个)
```

### Lifecycle

```
User selects Plan (belongs to a Product)
    │
    ▼
Creates Order (records: who, which plan, how much, campaign)
    │
    ▼  payment confirmed
Creates or renews Subscription (for that Product)
    │
    ▼
Subscription grants access: expiry + quota
    │
    ▼
syncUserCache() updates User.ExpiredAt/MaxDevice (backward compat)
```

### Entity Responsibilities

| Entity | Responsibility | Mutability |
|--------|---------------|------------|
| **Product** | Defines a product line | Immutable enum |
| **Plan** | 商品货架: what to sell, at what price, with what quota | Admin-mutable |
| **Order** | 交易记录: purchase snapshot at time of payment | Immutable after creation |
| **Subscription** | 权益状态: can this user use this product now? | Updated on purchase/renewal |
| **User** | 身份: identity + auth. NOT subscription state. | Cache fields synced from Subscription |

---

## 2. Data Model Changes

### 2.1 Subscription Model (NEW)

```go
type Subscription struct {
    ID          uint64    `gorm:"primarykey" json:"id"`
    CreatedAt   time.Time `json:"createdAt"`
    UpdatedAt   time.Time `json:"updatedAt"`
    UserID      uint64    `gorm:"not null;uniqueIndex:idx_user_product" json:"userId"`
    ProductType string    `gorm:"type:varchar(20);not null;uniqueIndex:idx_user_product" json:"productType"` // "personal" | "gateway"
    PlanPID     string    `gorm:"type:varchar(30);not null" json:"planPid"`  // current plan
    ExpiredAt   int64     `gorm:"not null" json:"expiredAt"`                 // Unix seconds
    Quota       int       `gorm:"not null" json:"quota"`                     // personal=max devices, gateway=max router devices
}
```

- `(UserID, ProductType)` unique — one subscription per user per product
- `Quota` semantics defined by `ProductType`:
  - personal: max login devices (replaces `User.MaxDevice`)
  - gateway: max router devices
- Renewal = UPDATE `ExpiredAt` (extend), not INSERT
- Plan upgrade = UPDATE `PlanPID` + `Quota` + `ExpiredAt`

### 2.2 Plan Model (MODIFIED)

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
    ProductType string `gorm:"type:varchar(20);not null;default:'personal'"` // "personal" | "gateway"
    Quota       int    `gorm:"not null;default:5"`                           // personal=max devices, gateway=max router devices, 0=unlimited
}
```

- `Plan.Quota` → written to `Subscription.Quota` on purchase
- Existing plans: `ProductType='personal'`, `Quota=5` (matches current hardcoded MaxDevice=5)

### 2.3 Device Model (MODIFIED)

```go
type Device struct {
    // existing fields unchanged
    // new field
    IsGateway bool `gorm:"not null;default:false"` // router device flag
}
```

### 2.4 User Model — Cache Fields

**Existing fields kept as denormalized cache, NOT source of truth:**

```go
type User struct {
    // existing — becomes cache, synced from personal Subscription
    ExpiredAt int64 `gorm:"not null"`       // cache of personal Subscription.ExpiredAt
    MaxDevice int   `gorm:"not null;default:5"` // cache of personal Subscription.Quota
}
```

No new fields on User. Gateway state lives exclusively in Subscription.

### 2.5 Cache Sync

```go
// Called after every Subscription write (create/update)
func syncUserCache(tx *gorm.DB, userID uint64) error {
    var sub Subscription
    err := tx.Where("user_id = ? AND product_type = 'personal'", userID).First(&sub).Error
    if err == gorm.ErrRecordNotFound {
        return nil // no personal subscription, don't touch User
    }
    if err != nil {
        return err
    }
    return tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
        "expired_at": sub.ExpiredAt,
        "max_device": sub.Quota,
    }).Error
}
```

**Purpose:** Untouched code (workers, EDM, admin queries not modified in this release) continues to read `User.ExpiredAt` / `User.MaxDevice` without breaking. All new and modified code reads Subscription directly.

### 2.6 Source of Truth Migration

Code touched in this release reads Subscription (source of truth):

| Code path | Before | After |
|-----------|--------|-------|
| `ProRequired` middleware | `user.ExpiredAt > now` | `Subscription WHERE product_type='personal' AND expired_at > now` |
| `api_auth.go` device limit | `user.MaxDevice` | `Subscription.Quota WHERE product_type='personal'` |
| `GatewayProRequired` (new) | — | `Subscription WHERE product_type='gateway' AND expired_at > now` |
| `applyOrderToTargetUsers` | writes `user.ExpiredAt`, `user.MaxDevice` | writes Subscription + `syncUserCache()` |
| `api_user.go` profile | returns `user.ExpiredAt` | returns `user.Subscriptions[]` (+ cache fields for compat) |
| Workers/EDM (untouched) | reads `user.ExpiredAt` | unchanged — reads synced cache |

### 2.7 Data Migration

On deploy, populate Subscription table from existing User data:

```sql
INSERT INTO subscriptions (user_id, product_type, plan_pid, expired_at, quota, created_at, updated_at)
SELECT id, 'personal', '', expired_at, max_device, NOW(), NOW()
FROM users
WHERE expired_at > 0;
```

Existing users get a personal Subscription matching their current state. `plan_pid = ''` (unknown — historical orders didn't track this in User).

### 2.8 Database Schema Changes Summary

| Table | Field | Type | Default | Note |
|-------|-------|------|---------|------|
| `subscriptions` | (new table) | — | — | See 2.1 |
| `plans` | `product_type` | `VARCHAR(20)` | `'personal'` | Existing plans → personal |
| `plans` | `quota` | `INT` | `5` | Existing plans → 5 devices |
| `devices` | `is_gateway` | `BOOLEAN` | `false` | All existing → false |

No new fields on `users` table.

### 2.9 Example Gateway Plans

| PID | Label | Month | Quota | Price |
|-----|-------|-------|-------|-------|
| `router-monthly-5` | 路由器月付·5设备 | 1 | 5 | TBD |
| `router-yearly-5` | 路由器年付·5设备 | 12 | 5 | TBD |
| `router-monthly-15` | 路由器月付·15设备 | 1 | 15 | TBD |
| `router-yearly-15` | 路由器年付·15设备 | 12 | 15 | TBD |
| `router-monthly-unlimited` | 路由器月付·不限 | 1 | 0 | TBD |
| `router-yearly-unlimited` | 路由器年付·不限 | 12 | 0 | TBD |

All gateway plans: `ProductType="gateway"`, `Quota` = router device limit. Prices set by admin — TBD values are not spec gaps.

---

## 3. Plans API Changes

### 3.1 Public Plans Endpoint

```
GET /api/plans                          → returns personal plans (backward compatible)
GET /api/plans?product_type=gateway     → returns gateway plans
```

Default to `"personal"` when `product_type` absent.

### 3.2 Admin Plans Endpoints

Plans admin CRUD gains `ProductType` and `Quota` fields. Admin plans list supports `product_type` filter.

### 3.3 Frontend Plan Type

```typescript
interface Plan {
    pid: string;
    label: string;
    price: number;
    originPrice: number;
    month: number;
    highlight: boolean;
    productType: 'personal' | 'gateway';
    quota: number; // personal=max devices, gateway=max router devices, 0=unlimited
}
```

### 3.4 Frontend Subscription Type

```typescript
interface Subscription {
    id: number;
    productType: 'personal' | 'gateway';
    planPid: string;
    expiredAt: number;
    quota: number;
}
```

---

## 4. Purchase Page Design

### 4.1 Platform-Based Plan Display

| Context | What shows | Tab UI |
|---------|-----------|--------|
| Webapp in desktop/mobile (`platformType: 'desktop'/'mobile'`) | Personal plans only | No tab |
| Webapp in router (`platformType: 'gateway'`) | Gateway plans only | No tab |
| Website (browser) | Both product lines | `[个人版] [路由器版]` tab switcher |

**Principle:** Embedded webapp shows only the product matching its platform. Website as independent entry shows all products.

### 4.2 Website Tab Behavior

- Default tab: 个人版
- URL parameter `?product=gateway` selects 路由器版 tab
- Tab switch triggers `getPlans({ product_type })` re-fetch
- Selected tab persists in URL (pushState) for shareability

### 4.3 Gateway Plan Card Layout

Gateway plans have two dimensions: **duration** (month) and **router device quota** (Quota). Cards are grouped by quota tier, sorted by duration within each tier:

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

### 4.4 Gateway Membership Benefits

| Personal | Gateway |
|----------|---------|
| X 台设备同时使用 | X 台设备接入 / 不限设备 |
| 一个账号，全家共享 | 全家共享一键上网 |
| 全球智能节点 | 全球智能节点 |
| 免运维 | 透明代理无需配置 |
| 持续优化 | 持续优化 |
| 优先技术支持 | 优先技术支持 |

### 4.5 Gateway Purchase Flow

Same order creation flow as personal plans:
1. Select gateway plan → `CreateOrderRequest.Plan = "router-monthly-5"`
2. Preview → calculates price with optional campaign code
3. Pay → Wordgate redirect → webhook → creates/renews gateway Subscription

**Difference:** Member selection is hidden for gateway plans. Router subscription binds to the purchasing account only (no delegation).

### 4.6 Order Processing Changes

`applyOrderToTargetUsers()` changes from writing User fields to writing Subscription:

```go
func applyOrderToTargetUsers(tx *gorm.DB, order *Order, userIDs []uint64) error {
    plan := order.GetPlan()
    for _, uid := range userIDs {
        // Upsert Subscription (create or renew)
        var sub Subscription
        err := tx.Where("user_id = ? AND product_type = ?", uid, plan.ProductType).First(&sub).Error
        if err == gorm.ErrRecordNotFound {
            // New subscription
            sub = Subscription{
                UserID:      uid,
                ProductType: plan.ProductType,
                PlanPID:     plan.PID,
                ExpiredAt:   calcExpiry(plan.Month),
                Quota:       plan.Quota,
            }
            tx.Create(&sub)
        } else {
            // Renew: extend expiry, update quota if plan changed
            sub.ExpiredAt = extendExpiry(sub.ExpiredAt, plan.Month)
            sub.PlanPID = plan.PID
            sub.Quota = plan.Quota
            tx.Save(&sub)
        }
        // Sync cache for backward compat
        syncUserCache(tx, uid)
    }
    return nil
}
```

---

## 5. Subscription Expiry Behavior

### 5.1 Personal Subscription Expired

No change from current behavior:
- `ProRequired` middleware returns 402 (PaymentRequired)
- User sees "授权已过期" prompt with purchase link
- VPN connection refused

### 5.2 Gateway Subscription Expired

- k2r checks gateway Subscription on every `up` (connect) action via Center API
- **Expired → connection refused**, same as personal. User sees expiry prompt in webapp at `http://router-ip:1779`
- **Already connected when expired:** existing connection continues until next disconnect/reconnect. No mid-session kill.
- RouterDevice allowlist remains intact (local storage). Re-subscribing restores access without re-adding MACs.

### 5.3 No Gateway Subscription

- k2r can still be installed and configured
- Attempting to connect returns 402 with purchase prompt
- RouterDevice management UI still accessible (for pre-configuration)

---

## 6. RouterDevice Management (MAC Allowlist)

### 6.1 Storage

Stored locally on router in `/etc/k2r/storage.json` (existing encrypted storage):

```json
{
  "router-device-allowlist": "[{\"mac\":\"AA:BB:CC:DD:EE:FF\",\"remark\":\"iPhone\",\"addedAt\":1712600000}]",
  "router-device-allowlist-mode": "allowlist"
}
```

- `router-device-allowlist-mode`: `"open"` (all LAN devices proxied) or `"allowlist"` (only listed MACs)
- Default: `"open"` (new router installs unrestricted)

### 6.2 Gateway HTTP API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/router-devices` | GET | List LAN devices (ARP scan) + allowlist status + quota |
| `/api/router-devices/allow` | POST | Add MAC to allowlist `{mac, remark}` |
| `/api/router-devices/remove` | POST | Remove MAC from allowlist `{mac}` |
| `/api/router-devices/mode` | POST | Switch mode `{mode: "allowlist" \| "open"}` |

### 6.3 GET /api/router-devices Response

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
- `maxRouterDevice`: quota from gateway Subscription, `0` = unlimited
- Allowlist entry count cannot exceed `maxRouterDevice` (when > 0)

### 6.4 LAN Device Discovery

Platform-aware discovery with layered fallback:

```
Detect /usr/bin/ubus?
  ├─ Yes (OpenWrt) → ubus call dhcp ipv4leases + ubus call hostapd.wlan0 get_clients
  │                   Rich data: MAC, IP, hostname, WiFi signal/rate
  └─ No (standard Linux)
       ├─ dnsmasq lease file exists? → parse /var/lib/misc/dnsmasq.leases (or /tmp/dhcp.leases)
       │                               Data: MAC, IP, hostname, lease expiry
       └─ fallback → ip neigh show
                      Data: MAC, IP, state (REACHABLE/STALE/etc.), no hostname
```

**OpenWrt `ubus` advantages over raw ARP:**
- Structured JSON output (no text parsing)
- Hostname from DHCP lease (not unreliable reverse DNS)
- WiFi metadata (signal strength, connection rate) — future use
- Covers all clients including wired + wireless

**Implementation:**
- Auto-detect platform at gateway startup, cache detection result
- All backends implement same interface: `[]LanDevice{MAC, IP, Hostname, Online}`
- Filter discovered devices to configured LAN subnets only
- Merge with allowlist data (remark, allowed status)

### 6.5 nftables Enforcement

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

### 6.6 Quota Enforcement & Refresh

- k2r fetches gateway Subscription quota from Center at login
- Quota cached locally in storage
- **Refresh mechanism:** k2r re-fetches Subscription on every `status` poll (existing SSE/poll cycle). If quota changed (user upgraded plan), local cache updates immediately.
- When `maxRouterDevice > 0` and allowlist is full → add returns error `"quotaExceeded"`
- If user downgrades (quota shrinks below current allowlist size) → existing allowlist entries are NOT auto-pruned. User must manually remove. New additions blocked until under quota.

### 6.7 Webapp UI — Router Device Management Page

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

## 7. OTA Self-Update

### 7.1 CDN Structure

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

### 7.2 Update Flow

```
Gateway startup or periodic check (every 6 hours) or manual trigger from webapp
    │
    ▼
GET kaitu/k2r/LATEST → compare with current version
    │
    ├─ no update → {hasUpdate: false}
    │
    ▼ has update
{hasUpdate: true, currentVersion, newVersion}
    │
    ▼ user confirms via webapp UI
Download k2r-linux-{arch} → /tmp/k2r-update-{version}
    │
Verify SHA256 against checksums.txt
    │
Backup current binary: cp /usr/bin/k2r /usr/bin/k2r.bak
    │
Atomic replace: mv /tmp/k2r-update-{version} /usr/bin/k2r && chmod +x
    │
Restart service (systemctl restart k2r / /etc/init.d/k2r restart)
```

### 7.3 Rollback Strategy

If the new binary fails to start (service doesn't become healthy within 30 seconds):

1. Init system (systemd/procd) detects crash and restarts
2. New binary crashes again → init system gives up after respawn limit
3. **Manual recovery:** `mv /usr/bin/k2r.bak /usr/bin/k2r && service k2r restart`
4. `/usr/bin/k2r.bak` is always preserved — only overwritten on next successful update

**Automatic rollback (future iteration):** A watchdog that checks service health post-restart and auto-reverts to `.bak` if unhealthy. Out of scope for this release — manual recovery via SSH is acceptable for router users.

### 7.4 Gateway HTTP API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/updater/check` | POST | Check for update. Returns `{hasUpdate, currentVersion, newVersion}` |
| `/api/updater/apply` | POST | Backup + download + verify + replace + restart |
| `/api/updater/status` | GET | SSE stream: `{stage, progress, error}` |

Stage enum: `downloading` → `verifying` → `backing-up` → `replacing` → `restarting` → `done` / `error`

### 7.5 Webapp Integration

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

Existing webapp updater UI (settings page update check, update banner) works out of the box. No new UI components needed.

**Previous design said "hide updater for gateway" — this is now reversed: gateway has updater enabled.**

---

## 8. Gateway Device Registration

### 8.1 Auth Request

k2r includes `isGateway: true` in the `X-App-Info` header:

```json
{"version": "0.4.2", "platform": "linux", "arch": "arm64", "isGateway": true}
```

### 8.2 Middleware Change

`middleware.go` parses `isGateway` from `X-App-Info` and sets `Device.IsGateway = true` during device registration/update in both OTP and password auth paths.

---

## 9. Webapp Conditional Rendering

Based on `window._platform.platformType === 'gateway'`:

| Component | Desktop/Mobile | Gateway |
|-----------|---------------|---------|
| Updater | Tauri/Capacitor updater | Gateway updater (Section 7) |
| Service reinstall | Show | Hide |
| ADB install helper | Show (desktop) | Hide |
| Proxy mode toggle | Show | Hide (always TPROXY) |
| TUN mode toggle | Show | Hide |
| Router device management | Hide | Show (Section 6) |
| LAN/DNS settings | Hide | Show |
| Interceptor status | Hide | Show (nftables/iptables) |
| Purchase plans | Personal plans | Gateway plans |
| Member selection (purchase) | Show | Hide |

---

## 10. Website Changes

### 10.1 Install Page — Router Tab

`kaitu.io/install` adds a 「路由器」tab:

- One-line install command: `wget -qO- https://kaitu.io/i/k2r | sh`
- Supported architectures (aarch64/x86_64/armv7/mipsle)
- OpenWrt / soft-router / NAS installation guide
- Post-install: access `http://<router-ip>:1779`

### 10.2 Purchase Page — Product Tabs

`kaitu.io/purchase` adds `[个人版] [路由器版]` tab switcher. Default: 个人版. URL param `?product=gateway` selects router tab. Each tab fetches plans with corresponding `product_type`.

---

## 11. Admin Dashboard Changes

### 11.1 Plan Management

- Plans table adds columns: `ProductType`, `Quota`
- Create/edit form adds these fields
- Filter by `product_type` dropdown

### 11.2 User Detail

- User detail page shows Subscriptions list (productType, planPid, expiredAt, quota)
- Admin can view both personal and gateway subscriptions

### 11.3 Device Statistics

- `api_admin_device_stats.go` adds `is_gateway` dimension
- Admin dashboard can filter by gateway / non-gateway devices

---

## 12. CI

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
- Automatic OTA rollback (manual recovery via SSH for this release)

---

## File Change Summary

### Backend (api/)

| File | Change |
|------|--------|
| `model.go` | Add `Subscription` model, `Plan.ProductType`, `Plan.Quota`, `Device.IsGateway` |
| `type.go` | Add `DataSubscription`, update `DataPlan`, `DataUser` (add subscriptions), `AppInfo` (add isGateway) |
| `api_plan.go` | Add `product_type` query filter |
| `api_admin_plan.go` | Add new fields to create/update |
| `api_order.go` / `logic_order.go` | Rewrite `applyOrderToTargetUsers()` → write Subscription + syncUserCache |
| `middleware.go` | `ProRequired` reads Subscription; parse `isGateway` from `X-App-Info` |
| `api_user.go` | Profile returns subscriptions list |
| `api_auth.go` | Device limit reads Subscription.Quota |
| `api_admin_device_stats.go` | Add `is_gateway` dimension |
| `route.go` | Add subscription-related admin routes if needed |

### k2 submodule (gateway/)

| File | Change |
|------|--------|
| `gateway/api.go` | Add `/api/router-devices`, `/api/updater/*` endpoints |
| `gateway/router_device.go` | New: allowlist CRUD, ARP scan, nftables set management |
| `gateway/updater.go` | New: CDN check, download, verify, backup, replace, restart |
| `gateway/intercept_nft.go` | Add `allowed_router_devices` set, conditional MAC filter rule |
| `gateway/intercept_ipt.go` | Equivalent iptables MAC filter |

### Webapp (webapp/)

| File | Change |
|------|--------|
| `src/pages/Purchase.tsx` | Filter plans by `platformType` → `product_type` |
| `src/pages/RouterDevices.tsx` | New: router device management page |
| `src/services/gateway-k2.ts` | Add updater implementation, router-device API calls |
| `src/services/api-types.ts` | Add `Plan.productType`, `Plan.quota`, `Subscription` type |
| `src/components/MembershipBenefits.tsx` | Gateway-specific benefits |
| `src/i18n/locales/*/purchase.json` | Gateway plan feature text |
| `src/i18n/locales/*/routerDevice.json` | New namespace |
| Various components | `platformType === 'gateway'` conditional rendering |

### Website (web/)

| File | Change |
|------|--------|
| `src/app/[locale]/purchase/PurchaseClient.tsx` | Add product tab switcher, gateway plan display |
| `src/app/[locale]/install/` | Add router tab |
| `src/app/(manager)/manager/plans/page.tsx` | Add ProductType/Quota columns |
| `src/lib/api.ts` | Add `product_type` param to `getPlans()`, add `Subscription` type |
| `messages/*/purchase.json` | Gateway product labels |

### CI

| File | Change |
|------|--------|
| `.github/workflows/release-openwrt.yml` | Uncomment `v*` tag trigger |
