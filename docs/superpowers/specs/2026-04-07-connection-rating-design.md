# Connection Rating Statistics + Auto-Ticket Hiding

## Problem

DisconnectFeedbackDialog asks users "good" or "bad" after each disconnect. Currently:
- "good" does nothing (closes dialog)
- "bad" creates a FeedbackTicket visible to the user in their ticket list

Issues:
1. Auto-generated tickets clutter the user's feedback list
2. "good" responses are lost — no data collected
3. No statistical view of connection quality trends
4. No user-side network environment data for diagnostics

## Goals

1. Record every good/bad rating with rich context (server, network environment, device)
2. Keep auto-generated tickets for ops investigation but hide them from users
3. Provide manager dashboard statistics to monitor and improve connection quality
4. Capture user-side network environment (public IP, ISP, router brand) for diagnostics

---

## Design

### 1. Client-Side Network Environment Probing

#### NetworkEnvironment Cache

Engine-level in-memory cache, updated on network change events:

```go
type NetworkEnvironment struct {
    PublicIP     string    // from ipinfo.io / ipify
    ISP          string    // from ipinfo.io
    City         string    // from ipinfo.io
    Country      string    // from ipinfo.io
    RouterBrand  string    // from gateway HTTP title parsing
    RouterModel  string    // from gateway HTTP title parsing
    GatewayIP    string    // from system routing table
    NetworkType  string    // wifi / cellular / ethernet
    ProbeTime    time.Time // when the probe completed
}
```

#### Trigger

`netCoordinator` receives a network change signal → spawns async goroutine → probes → writes to cache.

#### Probe Steps (all best-effort, any step may fail silently)

1. Read default gateway IP from system routing table
2. `GET http://{gateway}:80` — 1.5s timeout, read first 4KB, extract `<title>` → match against known brand signature table
3. `GET https://ipinfo.io/json` — 2s timeout, parse IP + ISP + city + country

#### Brand Signature Table (initial set, extensible)

| Pattern in `<title>` | Brand |
|---|---|
| `TL-`, `TP-LINK`, `tplink` | TP-LINK |
| `小米路由`, `miwifi`, `Xiaomi` | Xiaomi |
| `HUAWEI`, `HiLink` | Huawei |
| `ASUS`, `RT-` | ASUS |
| `LuCI`, `OpenWrt` | OpenWrt |
| `NETGEAR` | NETGEAR |
| `Linksys` | Linksys |
| `D-Link` | D-Link |
| `Mercury`, `MERCURY` | Mercury |
| `FAST`, `迅捷` | FAST |
| `Tenda`, `腾达` | Tenda |

#### Exposed API

- `engine.NetworkEnv() *NetworkEnvironment` — returns current cached value (may be nil)
- `appext/` exports for gomobile: `EngineNetworkEnv() string` (JSON-serialized)
- Desktop daemon: available via status or dedicated endpoint
- Bridge layer: `getNetworkEnv()` call on each platform

#### Platform Coverage

- Desktop (daemon): full support — Go has routing table + HTTP access
- iOS: full support — gomobile via appext, NetworkExtension has gateway access
- Android: full support — gomobile via appext, VpnService has network info access
- Standalone (web): no support — browser cannot probe LAN. `network` fields will be empty.

---

### 2. Connection Rating API

#### Endpoint: `POST /api/user/connection-rating`

Auth: required (Bearer token).

Request body:

```json
{
  "rating": "good",
  "feedbackId": "abc-123",
  "server": {
    "domain": "hk1.k2.kaitu.io",
    "name": "Hong Kong 1",
    "country": "HK",
    "source": "cloud"
  },
  "connection": {
    "durationSec": 3600,
    "ruleMode": "global",
    "os": "darwin",
    "appVersion": "0.4.2"
  },
  "network": {
    "publicIP": "223.5.5.5",
    "isp": "China Telecom",
    "city": "Shanghai",
    "country": "CN",
    "routerBrand": "TP-LINK",
    "routerModel": "TL-WR886N",
    "gatewayIP": "192.168.1.1",
    "networkType": "wifi"
  }
}
```

All `network` fields are optional (may be empty strings if probe failed or unavailable).

`feedbackId` is always generated. For "bad" ratings, the same `feedbackId` links to the auto-generated ticket and uploaded logs.

Response: standard `{ code: 0 }`.

#### New Table: `connection_ratings`

```sql
CREATE TABLE connection_ratings (
  id             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  created_at     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  user_id        BIGINT UNSIGNED NOT NULL,
  rating         VARCHAR(8) NOT NULL,          -- 'good' / 'bad'
  feedback_id    VARCHAR(36) NOT NULL,
  server_domain  VARCHAR(128) NOT NULL DEFAULT '',
  server_name    VARCHAR(64) NOT NULL DEFAULT '',
  server_country VARCHAR(8) NOT NULL DEFAULT '',
  server_source  VARCHAR(16) NOT NULL DEFAULT '',
  duration_sec   INT NOT NULL DEFAULT 0,
  rule_mode      VARCHAR(16) NOT NULL DEFAULT '',
  os             VARCHAR(32) NOT NULL DEFAULT '',
  app_version    VARCHAR(32) NOT NULL DEFAULT '',
  public_ip      VARCHAR(45) NOT NULL DEFAULT '',
  isp            VARCHAR(128) NOT NULL DEFAULT '',
  user_city      VARCHAR(64) NOT NULL DEFAULT '',
  user_country   VARCHAR(8) NOT NULL DEFAULT '',
  router_brand   VARCHAR(64) NOT NULL DEFAULT '',
  router_model   VARCHAR(128) NOT NULL DEFAULT '',
  gateway_ip     VARCHAR(45) NOT NULL DEFAULT '',
  network_type   VARCHAR(16) NOT NULL DEFAULT '',
  INDEX idx_created_at (created_at),
  INDEX idx_user_id (user_id),
  INDEX idx_rating (rating),
  INDEX idx_server_domain (server_domain)
);
```

---

### 3. Auto-Generated Ticket Hiding

#### Schema Change

Add column to `feedback_tickets`:

```sql
ALTER TABLE feedback_tickets ADD COLUMN auto_generated TINYINT(1) NOT NULL DEFAULT 0;
```

#### Write Path

`DisconnectFeedbackDialog` — when "bad" is clicked, create ticket with `auto_generated: true` in request body.

`api_create_ticket` handler — read `auto_generated` from request, set on `FeedbackTicket` model.

#### Read Path — User APIs

`api_user_list_tickets`: add `WHERE auto_generated = 0` to query.

`api_user_unread_count`: add `WHERE auto_generated = 0` to query.

#### Read Path — Admin APIs

No filtering. Admin sees all tickets. `auto_generated` column visible in admin ticket list for context.

---

### 4. Frontend Changes (webapp)

#### DisconnectFeedbackDialog

Current flow:
- "good" → close dialog, no API call
- "bad" → close dialog, upload logs, create ticket, register device-log, slack notify

New flow:
- **"good"** → close dialog, submit `POST /api/user/connection-rating` with `rating: "good"` + connection info + network env
- **"bad"** → close dialog, show thank-you toast, then fire-and-forget:
  1. Upload logs (unchanged)
  2. Create ticket with `auto_generated: true` (unchanged except new field)
  3. Register device-log (unchanged)
  4. Slack notify (unchanged)
  5. Submit `POST /api/user/connection-rating` with `rating: "bad"` + connection info + network env (new)

#### Bridge Layer

Add `getNetworkEnv()` to bridge interface:
- `tauri-k2.ts`: call daemon API or IPC
- `capacitor-k2.ts`: call K2Plugin method
- `standalone-k2.ts`: return empty object (no LAN access in browser)

#### Connection Store

`LastConnectionInfo` — no change needed. Network env is fetched separately from bridge at submit time.

---

### 5. Manager Dashboard

#### Overview Tab — New KPI Card

Add "Connection Quality" card alongside existing 4 KPI cards:
- Main number: **好评率** (good rate %) for last 7 days
- Sub-text: total ratings count
- Mini sparkline: 7-day daily good rate trend
- Delta arrow: change vs previous 7 days (↑ green / ↓ red)

#### New Tab: "Connection Quality"

Six sections:

1. **Good Rate Trend** — 30-day line chart, daily aggregation. Y-axis: good rate %. Hover shows total/good/bad counts.

2. **By Server** — Table sorted by good rate ascending (worst first).
   Columns: server name, country, total, good, bad, good rate.

3. **By User Network (ISP)** — Table sorted by good rate ascending.
   Columns: ISP, country, total, good, good rate.

4. **By Router Brand** — Table sorted by good rate ascending. Empty router_brand grouped as "Unknown".
   Columns: brand, total, good, good rate.

5. **By Platform & Version** — Table sorted by good rate ascending.
   Columns: OS, app version, total, good, good rate.

6. **By User** — Top 50 users with lowest good rate (minimum 3 ratings to appear). Distinguishes "subjectively low raters" from "genuinely problematic".
   Columns: email, total, good, bad, good rate.

#### Statistics API: `GET /app/connection-ratings/statistics`

Auth: admin required.

Query params: `period=7d|30d|90d`

Response:

```json
{
  "summary": {
    "total": 1200,
    "good": 1020,
    "bad": 180,
    "goodRate": 0.85
  },
  "trend": [
    { "date": "2026-04-01", "total": 40, "good": 34, "bad": 6, "goodRate": 0.85 }
  ],
  "byServer": [
    { "domain": "hk1.k2.kaitu.io", "name": "Hong Kong 1", "country": "HK", "total": 200, "good": 150, "bad": 50, "goodRate": 0.75 }
  ],
  "byISP": [
    { "isp": "China Telecom", "country": "CN", "total": 300, "good": 240, "goodRate": 0.80 }
  ],
  "byRouter": [
    { "brand": "TP-LINK", "total": 100, "good": 85, "goodRate": 0.85 }
  ],
  "byPlatform": [
    { "os": "darwin", "appVersion": "0.4.2", "total": 150, "good": 130, "goodRate": 0.87 }
  ],
  "byUser": [
    { "userId": 123, "email": "user@example.com", "total": 10, "good": 3, "bad": 7, "goodRate": 0.30 }
  ]
}
```

`byUser` returns top 50 by lowest good rate, minimum 3 ratings.

#### Frontend API Method

```typescript
getConnectionRatingStatistics(period: '7d' | '30d' | '90d'): Promise<ConnectionRatingStatisticsResponse>
```

#### Overview Card API

Reuses the same endpoint with `period=7d`. Frontend extracts `summary` + `trend` for the KPI card.

---

## Files Changed

### k2 submodule (Go engine)
- `engine/network_env.go` — new: NetworkEnvironment struct, probe logic, brand signatures
- `engine/engine.go` — expose `NetworkEnv()` method
- `engine/net_coordinator.go` — trigger probe on network change
- `appext/appext.go` — export `EngineNetworkEnv()` for gomobile

### webapp (React)
- `src/components/DisconnectFeedbackDialog.tsx` — good also submits rating; bad adds `auto_generated: true` to ticket + submits rating
- `src/services/tauri-k2.ts` — add `getNetworkEnv()` bridge call
- `src/services/capacitor-k2.ts` — add `getNetworkEnv()` bridge call
- `src/services/standalone-k2.ts` — stub returning empty network env
- `src/types/kaitu-core.ts` — add `NetworkEnvironment` type
- `src/components/__tests__/DisconnectFeedbackDialog.test.tsx` — update tests

### desktop (Tauri)
- `src-tauri/` — expose network env query via IPC or daemon API

### api (Center backend)
- `model.go` — add `ConnectionRating` model, add `AutoGenerated` to `FeedbackTicket`
- `type.go` — add request/response types
- `api_connection_rating.go` — new: `POST /api/user/connection-rating` handler
- `api_admin_connection_rating.go` — new: `GET /app/connection-ratings/statistics` handler
- `api_ticket_reply.go` — filter `auto_generated = false` in user list/unread
- `api_ticket.go` — accept and store `auto_generated` field
- `route.go` — register new endpoints
- `migrate.go` — add `ConnectionRating` to auto-migrate, alter `FeedbackTicket`

### web (Manager dashboard)
- `src/app/(manager)/manager/page.tsx` — add Connection Quality KPI card to Overview
- `src/app/(manager)/manager/connection-quality/page.tsx` — new: Connection Quality tab page
- `src/lib/api.ts` — add `getConnectionRatingStatistics()` method
- `src/lib/types.ts` — add response types

---

## Not In Scope

- GeoIP server-side lookup (client handles all IP/ISP resolution)
- Light mode support (follows existing dark-only convention)
- Real-time / WebSocket stats updates (polling on page load is sufficient)
- Rating for auto-reconnects (only user-initiated disconnects)
