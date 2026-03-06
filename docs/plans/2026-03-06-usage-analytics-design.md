# Usage Analytics Design

Date: 2026-03-06

Track client and server usage trends: DAU/MAU, connection count, duration, node usage, k2s downloads.

## Data Model (GORM)

### `StatAppOpen` — DAU/MAU

```go
type StatAppOpen struct {
    ID         uint64    `gorm:"primarykey"`
    CreatedAt  time.Time                                                           // client time (UTC)
    ReportedAt time.Time `gorm:"autoCreateTime"`                                   // server receive time
    DeviceHash string    `gorm:"type:varchar(64);not null;index:idx_app_open_dau"`  // SHA256(UDID)
    OS         string    `gorm:"type:varchar(16);not null;index"`                   // macos/windows/linux/ios/android
    AppVersion string    `gorm:"type:varchar(32);not null"`
    Locale     string    `gorm:"type:varchar(8)"`
}
```

Key indexes:
- `idx_app_open_dau`: `(device_hash)` — DAU `COUNT(DISTINCT)` queries
- `os`: OS filter

DAU/MAU query: `SELECT DATE(reported_at), COUNT(DISTINCT device_hash) FROM stat_app_opens WHERE reported_at BETWEEN ? AND ? GROUP BY DATE(reported_at)`

### `StatConnection` — Connection Analytics

```go
type StatConnection struct {
    ID               uint64    `gorm:"primarykey"`
    CreatedAt        time.Time                                                              // client time (UTC)
    ReportedAt       time.Time `gorm:"autoCreateTime"`                                      // server receive time
    DeviceHash       string    `gorm:"type:varchar(64);not null;index"`                      // SHA256(UDID)
    OS               string    `gorm:"type:varchar(16);not null;index"`
    AppVersion       string    `gorm:"type:varchar(32);not null"`
    Event            string    `gorm:"type:varchar(16);not null;index"`                      // connect / disconnect
    NodeType         string    `gorm:"type:varchar(16);not null"`                             // cloud / self-hosted
    NodeIPv4         string    `gorm:"type:varchar(15);index:idx_conn_node"`                  // cloud node IPv4; empty for self-hosted
    NodeRegion       string    `gorm:"type:varchar(8)"`                                       // hk/jp etc; empty for self-hosted
    RuleMode         string    `gorm:"type:varchar(8)"`                                       // global / smart
    DurationSec      int       `gorm:"not null;default:0"`                                    // disconnect only
    DisconnectReason string    `gorm:"type:varchar(32)"`                                      // user/error/network; disconnect only
}
```

Key indexes:
- `event`: filter connect vs disconnect
- `idx_conn_node`: `(node_ipv4)` — node usage distribution
- `device_hash`: connection count per device

### `StatK2sDownload` — Server Install Script Downloads

```go
type StatK2sDownload struct {
    ID        uint64    `gorm:"primarykey"`
    CreatedAt time.Time `gorm:"autoCreateTime"`
    IPHash    string    `gorm:"type:varchar(64);not null;index:idx_k2s_dedup"` // SHA256(ip + daily_salt)
    IPRaw     string    `gorm:"type:varchar(45);not null"`                     // raw IP for geo analysis
    UA        string    `gorm:"type:varchar(255)"`                             // User-Agent
}
```

Key indexes:
- `idx_k2s_dedup`: `(ip_hash)` — daily unique dedup queries

## Privacy / De-identification

- Client events use `SHA256(UDID)` as device identifier, not reversible, no user_id
- Cloud nodes: report IPv4 (our own infrastructure, not sensitive)
- Self-hosted nodes: report only `"self-hosted"`, no node IP/domain/identifier
- k2s downloads: `SHA256(ip + daily_salt)` for daily unique dedup, salt rotates daily

## Supplementary Design

### Daily Salt Management

- Store in Redis: key `stats:daily_salt:YYYY-MM-DD`, value = random 32-byte hex
- TTL: 48 hours (covers timezone edge cases)
- Generated on first request of each day (lazy init with `SETNX`)

### Rate Limiting

- `POST /api/stats/events` has no auth, needs basic protection
- Rate limit: 10 requests per minute per IP (reuse existing Gin rate limit middleware or simple Redis counter)
- Payload size limit: max 100 events per request

### Data Retention

- Raw data retained for 1 year
- Monthly cron aggregates data older than 1 year into `stat_monthly_summary` (future, not in v1)
- For v1: no auto-cleanup, revisit when data exceeds 1M rows

### App Open Dedup

- No write-time dedup in v1 — multiple `app_open` per device per day is acceptable
- DAU queries use `COUNT(DISTINCT device_hash)` which handles this correctly
- If row volume becomes a concern, add daily dedup later

## Client Event Reporting

### Storage & Retry

- Events queued in `_platform.storage` under key `stats_queue`
- On trigger, batch-send all queued events; clear on 200 response
- On failure, retain queue for next trigger

### Trigger Points

1. App startup complete -> write `app_open` -> attempt flush
2. VPN connected -> write `connect` -> attempt flush
3. VPN disconnected -> write `disconnect` -> attempt flush

### API

```
POST /api/stats/events
Content-Type: application/json

{
  "app_opens": [
    {
      "device_hash": "a1b2c3...",
      "os": "macos",
      "app_version": "0.4.0-beta.1",
      "locale": "zh-CN",
      "created_at": "2026-03-06T10:00:00Z"
    }
  ],
  "connections": [
    {
      "device_hash": "a1b2c3...",
      "os": "macos",
      "app_version": "0.4.0-beta.1",
      "event": "disconnect",
      "node_type": "cloud",
      "node_ipv4": "103.x.x.x",
      "node_region": "hk",
      "rule_mode": "global",
      "duration_sec": 3600,
      "disconnect_reason": "user",
      "created_at": "2026-03-06T11:00:00Z"
    }
  ]
}
```

- No auth required (no user identity, device_hash is irreversible)
- Server returns 200 -> client clears sent events
- Failure -> retain queue, retry on next trigger
- Max 100 events per request, rate limited 10 req/min per IP

## k2s Download Tracking

Next.js middleware intercepts `/i/k2s` requests:

1. Extract client IP, compute `SHA256(ip + daily_salt)`
2. Async write to `stat_k2s_downloads` via Center API internal endpoint (non-blocking)
3. Serve static script as normal

Daily salt: fetched from Redis (`stats:daily_salt:YYYY-MM-DD`), lazy-generated via `SETNX`.

## Admin Dashboard

**Route**: `web/src/app/(manager)/usages/page.tsx`

### Charts

1. **Active Devices Trend** — line chart, DAU/WAU/MAU toggle (unique `device_hash` from `stat_app_opens`)
2. **Connection Count Trend** — line chart, daily `connect` event count
3. **Node Usage Distribution** — bar/pie chart, grouped by `node_ipv4` (cloud) and self-hosted
4. **k2s Download Trend** — line chart, daily unique IP downloads

### Filters

- Time range: 7d / 30d / 90d / custom
- OS filter

### API

```
GET /app/stats/overview?range=30d&os=
```

Server-side SQL aggregation, returns chart-ready data. No raw events sent to frontend.

## Implementation Modules

1. **Center API** (`api/`)
   - `model_stats.go` — 3 GORM models (AutoMigrate in `migrate.go`)
   - `api_stats.go` — `POST /api/stats/events` (no auth, rate limited)
   - `api_admin_stats.go` — `GET /app/stats/overview` (admin auth)
   - `route.go` — register new routes

2. **Webapp** (`webapp/`)
   - `src/services/stats.ts` — event queue (storage + flush logic)
   - Hook into app startup, VPN connect/disconnect to write events

3. **Web / Next.js** (`web/`)
   - `src/middleware.ts` — intercept `/i/k2s`, record download
   - `src/app/(manager)/usages/page.tsx` — admin dashboard with charts

4. **Migration**
   - GORM AutoMigrate handles table creation + indexes
   - Add 3 models to `migrate.go` `AutoMigrate()` call
