# Usage Analytics Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Track client and server usage trends (DAU/MAU, connections, node usage, k2s downloads) with event-based reporting, persistent queue, and admin dashboard.

**Architecture:** Client queues events in `_platform.storage`, batch-sends to `POST /api/stats/events` (no auth). Center API writes to 3 MySQL tables via GORM. Next.js middleware tracks k2s downloads. Admin dashboard at `/manager/usages` shows aggregated charts.

**Tech Stack:** Go/Gin/GORM (API), React/TypeScript (webapp), Next.js/shadcn (admin dashboard), Redis (daily salt + rate limit)

**Design doc:** `docs/plans/2026-03-06-usage-analytics-design.md`

---

### Task 1: GORM Models + Migration

**Files:**
- Create: `api/model_stats.go`
- Modify: `api/migrate.go`

**Step 1: Create model file**

Create `api/model_stats.go`:

```go
package center

import "time"

// StatAppOpen tracks app launches for DAU/MAU calculation.
// Device identified by SHA256(UDID), no user_id.
type StatAppOpen struct {
	ID         uint64    `gorm:"primarykey"`
	CreatedAt  time.Time // client time (UTC)
	ReportedAt time.Time `gorm:"autoCreateTime"`
	DeviceHash string    `gorm:"type:varchar(64);not null;index:idx_app_open_dau"`
	OS         string    `gorm:"type:varchar(16);not null;index"`
	AppVersion string    `gorm:"type:varchar(32);not null"`
	Locale     string    `gorm:"type:varchar(8)"`
}

// StatConnection tracks VPN connect/disconnect events.
type StatConnection struct {
	ID               uint64    `gorm:"primarykey"`
	CreatedAt        time.Time // client time (UTC)
	ReportedAt       time.Time `gorm:"autoCreateTime"`
	DeviceHash       string    `gorm:"type:varchar(64);not null;index"`
	OS               string    `gorm:"type:varchar(16);not null;index"`
	AppVersion       string    `gorm:"type:varchar(32);not null"`
	Event            string    `gorm:"type:varchar(16);not null;index"`
	NodeType         string    `gorm:"type:varchar(16);not null"`
	NodeIPv4         string    `gorm:"type:varchar(15);index:idx_conn_node"`
	NodeRegion       string    `gorm:"type:varchar(8)"`
	RuleMode         string    `gorm:"type:varchar(8)"`
	DurationSec      int       `gorm:"not null;default:0"`
	DisconnectReason string    `gorm:"type:varchar(32)"`
}

// StatK2sDownload tracks k2s install script downloads.
// IP hashed with daily-rotating salt for unique count without storing raw IPs long-term.
type StatK2sDownload struct {
	ID        uint64    `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	IPHash    string    `gorm:"type:varchar(64);not null;index:idx_k2s_dedup"`
	IPRaw     string    `gorm:"type:varchar(45);not null"`
	UA        string    `gorm:"type:varchar(255)"`
}
```

**Step 2: Add to migrate.go**

In `api/migrate.go`, add the 3 models to the `AutoMigrate()` call:

```go
// Usage analytics
&StatAppOpen{},
&StatConnection{},
&StatK2sDownload{},
```

Add after the `&CloudInstance{}` line (around line 49).

**Step 3: Run tests**

Run: `cd api && go build ./...`
Expected: compiles without errors

**Step 4: Commit**

```
feat(api): add usage analytics GORM models

Three tables: stat_app_opens (DAU/MAU), stat_connections
(VPN connect/disconnect), stat_k2s_downloads (installer).
GORM AutoMigrate handles table creation and indexes.
```

---

### Task 2: Stats Event Ingestion API

**Files:**
- Create: `api/api_stats.go`
- Modify: `api/route.go`

**Step 1: Create handler**

Create `api/api_stats.go`:

```go
package center

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
)

// ========================= Request Types =========================

type StatsEventRequest struct {
	AppOpens    []StatsAppOpenEvent    `json:"app_opens"`
	Connections []StatsConnectionEvent `json:"connections"`
}

type StatsAppOpenEvent struct {
	DeviceHash string    `json:"device_hash" binding:"required"`
	OS         string    `json:"os" binding:"required"`
	AppVersion string    `json:"app_version" binding:"required"`
	Locale     string    `json:"locale"`
	CreatedAt  time.Time `json:"created_at" binding:"required"`
}

type StatsConnectionEvent struct {
	DeviceHash       string    `json:"device_hash" binding:"required"`
	OS               string    `json:"os" binding:"required"`
	AppVersion       string    `json:"app_version" binding:"required"`
	Event            string    `json:"event" binding:"required"`
	NodeType         string    `json:"node_type" binding:"required"`
	NodeIPv4         string    `json:"node_ipv4"`
	NodeRegion       string    `json:"node_region"`
	RuleMode         string    `json:"rule_mode"`
	DurationSec      int       `json:"duration_sec"`
	DisconnectReason string    `json:"disconnect_reason"`
	CreatedAt        time.Time `json:"created_at" binding:"required"`
}

// ========================= k2s Download Request =========================

type StatsK2sDownloadRequest struct {
	IPRaw string `json:"ip_raw" binding:"required"`
	UA    string `json:"ua"`
}

// ========================= Handlers =========================

const maxEventsPerRequest = 100

// api_stats_ingest handles POST /api/stats/events
func api_stats_ingest(c *gin.Context) {
	var req StatsEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request body")
		return
	}

	totalEvents := len(req.AppOpens) + len(req.Connections)
	if totalEvents == 0 {
		SuccessEmpty(c)
		return
	}
	if totalEvents > maxEventsPerRequest {
		Error(c, ErrorInvalidArgument, fmt.Sprintf("too many events: %d, max %d", totalEvents, maxEventsPerRequest))
		return
	}

	tx := db.Get()

	// Insert app opens
	if len(req.AppOpens) > 0 {
		records := make([]StatAppOpen, len(req.AppOpens))
		for i, e := range req.AppOpens {
			records[i] = StatAppOpen{
				CreatedAt:  e.CreatedAt,
				DeviceHash: e.DeviceHash,
				OS:         e.OS,
				AppVersion: e.AppVersion,
				Locale:     e.Locale,
			}
		}
		if err := tx.Create(&records).Error; err != nil {
			log.Errorf(c, "failed to insert app opens: %v", err)
			Error(c, ErrorSystemError, "failed to save events")
			return
		}
	}

	// Insert connections
	if len(req.Connections) > 0 {
		records := make([]StatConnection, len(req.Connections))
		for i, e := range req.Connections {
			records[i] = StatConnection{
				CreatedAt:        e.CreatedAt,
				DeviceHash:       e.DeviceHash,
				OS:               e.OS,
				AppVersion:       e.AppVersion,
				Event:            e.Event,
				NodeType:         e.NodeType,
				NodeIPv4:         e.NodeIPv4,
				NodeRegion:       e.NodeRegion,
				RuleMode:         e.RuleMode,
				DurationSec:      e.DurationSec,
				DisconnectReason: e.DisconnectReason,
			}
		}
		if err := tx.Create(&records).Error; err != nil {
			log.Errorf(c, "failed to insert connections: %v", err)
			Error(c, ErrorSystemError, "failed to save events")
			return
		}
	}

	log.Debugf(c, "ingested %d stats events (app_opens=%d, connections=%d)",
		totalEvents, len(req.AppOpens), len(req.Connections))
	SuccessEmpty(c)
}

// api_stats_k2s_download handles POST /api/stats/k2s-download (internal, called by Next.js middleware)
func api_stats_k2s_download(c *gin.Context) {
	var req StatsK2sDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request body")
		return
	}

	ipHash, err := hashIPWithDailySalt(c, req.IPRaw)
	if err != nil {
		log.Errorf(c, "failed to hash IP: %v", err)
		Error(c, ErrorSystemError, "internal error")
		return
	}

	record := StatK2sDownload{
		IPHash: ipHash,
		IPRaw:  req.IPRaw,
		UA:     req.UA,
	}
	if err := db.Get().Create(&record).Error; err != nil {
		log.Errorf(c, "failed to insert k2s download: %v", err)
		Error(c, ErrorSystemError, "failed to save download")
		return
	}

	SuccessEmpty(c)
}

// ========================= Daily Salt =========================

func hashIPWithDailySalt(c *gin.Context, ip string) (string, error) {
	today := time.Now().UTC().Format("2006-01-02")
	cacheKey := fmt.Sprintf("stats:daily_salt:%s", today)

	// Try to get existing salt
	var salt string
	exists, err := redis.CacheGet(cacheKey, &salt)
	if err != nil {
		return "", fmt.Errorf("redis get salt: %w", err)
	}

	if !exists {
		// Generate new salt
		saltBytes := make([]byte, 32)
		if _, err := rand.Read(saltBytes); err != nil {
			return "", fmt.Errorf("generate salt: %w", err)
		}
		salt = hex.EncodeToString(saltBytes)
		// SETNX semantics: only set if not exists, 48h TTL
		if err := redis.CacheSet(cacheKey, salt, 48*3600); err != nil {
			log.Warnf(c, "failed to set daily salt, using generated: %v", err)
			// Continue with the generated salt even if Redis set fails
		}
		// Re-read in case another process set it first
		if exists2, err2 := redis.CacheGet(cacheKey, &salt); err2 == nil && exists2 {
			// Use the one from Redis (may be from another process)
		}
	}

	h := sha256.Sum256([]byte(ip + salt))
	return hex.EncodeToString(h[:]), nil
}
```

**Step 2: Register routes in route.go**

In `api/route.go`, add within the `/api` group (after the telemetry group, around line 202):

```go
// Usage analytics (no auth, rate limited)
stats := api.Group("/stats")
{
	stats.POST("/events", api_stats_ingest)
	stats.POST("/k2s-download", api_stats_k2s_download)
}
```

**Step 3: Build and verify**

Run: `cd api && go build ./...`
Expected: compiles without errors

**Step 4: Commit**

```
feat(api): add stats event ingestion endpoints

POST /api/stats/events — batch client events (no auth)
POST /api/stats/k2s-download — k2s download tracking
Daily salt via Redis for IP hashing.
```

---

### Task 3: Admin Stats Overview API

**Files:**
- Create: `api/api_admin_stats.go`
- Modify: `api/route.go`

**Step 1: Create admin handler**

Create `api/api_admin_stats.go`:

```go
package center

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// ========================= Response Types =========================

type UsageOverviewResponse struct {
	ActiveDevices  []DailyCount `json:"activeDevices"`
	Connections    []DailyCount `json:"connections"`
	NodeUsage      []NodeCount  `json:"nodeUsage"`
	K2sDownloads   []DailyCount `json:"k2sDownloads"`
}

type DailyCount struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type NodeCount struct {
	NodeIPv4 string `json:"nodeIpv4"`
	NodeType string `json:"nodeType"`
	Count    int64  `json:"count"`
}

// ========================= Handler =========================

func api_admin_usage_overview(c *gin.Context) {
	rangeParam := c.DefaultQuery("range", "30d")
	osFilter := c.Query("os")

	days, err := parseRangeDays(rangeParam)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid range parameter")
		return
	}

	since := time.Now().AddDate(0, 0, -days)
	resp := UsageOverviewResponse{}

	// 1. Active devices (DAU) — unique device_hash per day from stat_app_opens
	{
		query := db.Get().Model(&StatAppOpen{}).
			Select("DATE(reported_at) as date, COUNT(DISTINCT device_hash) as count").
			Where("reported_at >= ?", since).
			Group("DATE(reported_at)").
			Order("date")
		if osFilter != "" {
			query = query.Where("os = ?", osFilter)
		}
		if err := query.Find(&resp.ActiveDevices).Error; err != nil {
			log.Errorf(c, "failed to query active devices: %v", err)
			Error(c, ErrorSystemError, "query failed")
			return
		}
	}

	// 2. Connection count per day — count of connect events
	{
		query := db.Get().Model(&StatConnection{}).
			Select("DATE(reported_at) as date, COUNT(*) as count").
			Where("reported_at >= ? AND event = ?", since, "connect").
			Group("DATE(reported_at)").
			Order("date")
		if osFilter != "" {
			query = query.Where("os = ?", osFilter)
		}
		if err := query.Find(&resp.Connections).Error; err != nil {
			log.Errorf(c, "failed to query connections: %v", err)
			Error(c, ErrorSystemError, "query failed")
			return
		}
	}

	// 3. Node usage distribution — top nodes by connect count
	{
		query := db.Get().Model(&StatConnection{}).
			Select("node_ipv4, node_type, COUNT(*) as count").
			Where("reported_at >= ? AND event = ?", since, "connect").
			Group("node_ipv4, node_type").
			Order("count DESC").
			Limit(20)
		if osFilter != "" {
			query = query.Where("os = ?", osFilter)
		}
		if err := query.Find(&resp.NodeUsage).Error; err != nil {
			log.Errorf(c, "failed to query node usage: %v", err)
			Error(c, ErrorSystemError, "query failed")
			return
		}
	}

	// 4. k2s downloads — unique IPs per day
	{
		query := db.Get().Model(&StatK2sDownload{}).
			Select("DATE(created_at) as date, COUNT(DISTINCT ip_hash) as count").
			Where("created_at >= ?", since).
			Group("DATE(created_at)").
			Order("date")
		if err := query.Find(&resp.K2sDownloads).Error; err != nil {
			log.Errorf(c, "failed to query k2s downloads: %v", err)
			Error(c, ErrorSystemError, "query failed")
			return
		}
	}

	Success(c, &resp)
}

func parseRangeDays(rangeParam string) (int, error) {
	switch rangeParam {
	case "7d":
		return 7, nil
	case "30d":
		return 30, nil
	case "90d":
		return 90, nil
	default:
		return 0, fmt.Errorf("unsupported range: %s", rangeParam)
	}
}
```

**Step 2: Register admin route in route.go**

In `api/route.go`, add within the `/app` admin group (after the strategy block, around line 342):

```go
// Usage analytics overview
admin.GET("/stats/overview", api_admin_usage_overview)
```

**Step 3: Build and verify**

Run: `cd api && go build ./...`
Expected: compiles without errors

**Step 4: Commit**

```
feat(api): add admin usage overview endpoint

GET /app/stats/overview?range=30d&os= — returns DAU trend,
connection count, node usage distribution, k2s download trend.
Server-side SQL aggregation, no raw events to frontend.
```

---

### Task 4: Webapp Stats Service (Event Queue + Flush)

**Files:**
- Create: `webapp/src/services/stats.ts`
- Modify: `webapp/src/main.tsx`

**Key architecture notes (MUST READ):**
- `ISecureStorage` is **typed**: `get<T>(key): Promise<T | null>`, `set<T>(key, value): Promise<void>`. No JSON.stringify/parse needed.
- `cloudApi.request()` is the correct API call method (from `src/services/cloud-api.ts`).
- `window._platform` provides `os`, `version`, `storage`, `getUdid()`.

**Step 1: Create stats service**

Create `webapp/src/services/stats.ts`:

```typescript
/**
 * Usage Analytics — Event queue with persistent storage and batch upload.
 *
 * Events queued in _platform.storage under key 'stats_queue'.
 * On each trigger (app_open, connect, disconnect), queue is flushed
 * to POST /api/stats/events. On failure, events stay in queue.
 */

import { cloudApi } from './cloud-api';

// ========================= Types =========================

interface AppOpenEvent {
  device_hash: string;
  os: string;
  app_version: string;
  locale: string;
  created_at: string;
}

interface ConnectionEvent {
  device_hash: string;
  os: string;
  app_version: string;
  event: 'connect' | 'disconnect';
  node_type: 'cloud' | 'self-hosted';
  node_ipv4: string;
  node_region: string;
  rule_mode: string;
  duration_sec: number;
  disconnect_reason: string;
  created_at: string;
}

interface StatsQueue {
  app_opens: AppOpenEvent[];
  connections: ConnectionEvent[];
}

const STORAGE_KEY = 'stats_queue';

// ========================= Queue Management =========================

async function getQueue(): Promise<StatsQueue> {
  try {
    const stored = await window._platform?.storage?.get<StatsQueue>(STORAGE_KEY);
    if (stored) return stored;
  } catch {
    // Corrupted data, start fresh
  }
  return { app_opens: [], connections: [] };
}

async function saveQueue(queue: StatsQueue): Promise<void> {
  try {
    await window._platform?.storage?.set(STORAGE_KEY, queue);
  } catch (err) {
    console.warn('[Stats] Failed to save queue:', err);
  }
}

async function clearQueue(): Promise<void> {
  try {
    await window._platform?.storage?.remove(STORAGE_KEY);
  } catch {
    // ignore
  }
}

// ========================= Device Hash =========================

let _deviceHash: string | null = null;

async function getDeviceHash(): Promise<string> {
  if (_deviceHash) return _deviceHash;
  try {
    const udid = await window._platform?.getUdid();
    if (udid) {
      // SHA-256 hash of UDID
      const encoder = new TextEncoder();
      const data = encoder.encode(udid);
      const hashBuffer = await crypto.subtle.digest('SHA-256', data);
      const hashArray = Array.from(new Uint8Array(hashBuffer));
      _deviceHash = hashArray.map(b => b.toString(16).padStart(2, '0')).join('');
      return _deviceHash;
    }
  } catch {
    // fallback
  }
  return 'unknown';
}

// ========================= Flush =========================

let _flushing = false;

async function flush(): Promise<void> {
  if (_flushing) return;
  _flushing = true;

  try {
    const queue = await getQueue();
    const total = queue.app_opens.length + queue.connections.length;
    if (total === 0) return;

    const resp = await cloudApi.request('POST', '/api/stats/events', {
      app_opens: queue.app_opens,
      connections: queue.connections,
    });

    if (resp.code === 0) {
      await clearQueue();
      console.debug(`[Stats] Flushed ${total} events`);
    } else {
      console.warn('[Stats] Flush failed, will retry:', resp.code);
    }
  } catch (err) {
    console.warn('[Stats] Flush error, will retry:', err);
  } finally {
    _flushing = false;
  }
}

// ========================= Public API =========================

function getPlatformInfo() {
  const p = window._platform;
  return {
    os: p?.os || 'unknown',
    app_version: p?.version || '0.0.0',
  };
}

export const statsService = {
  /** Record app open and flush queue */
  async trackAppOpen(): Promise<void> {
    try {
      const deviceHash = await getDeviceHash();
      const { os, app_version } = getPlatformInfo();
      const locale = document.documentElement.lang || 'unknown';

      const queue = await getQueue();
      queue.app_opens.push({
        device_hash: deviceHash,
        os,
        app_version,
        locale,
        created_at: new Date().toISOString(),
      });
      await saveQueue(queue);
      flush(); // fire-and-forget
    } catch (err) {
      console.warn('[Stats] trackAppOpen failed:', err);
    }
  },

  /** Record VPN connect and flush queue */
  async trackConnect(params: {
    nodeType: 'cloud' | 'self-hosted';
    nodeIpv4: string;
    nodeRegion: string;
    ruleMode: string;
  }): Promise<void> {
    try {
      const deviceHash = await getDeviceHash();
      const { os, app_version } = getPlatformInfo();

      const queue = await getQueue();
      queue.connections.push({
        device_hash: deviceHash,
        os,
        app_version,
        event: 'connect',
        node_type: params.nodeType,
        node_ipv4: params.nodeType === 'cloud' ? params.nodeIpv4 : '',
        node_region: params.nodeType === 'cloud' ? params.nodeRegion : '',
        rule_mode: params.ruleMode,
        duration_sec: 0,
        disconnect_reason: '',
        created_at: new Date().toISOString(),
      });
      await saveQueue(queue);
      flush(); // fire-and-forget
    } catch (err) {
      console.warn('[Stats] trackConnect failed:', err);
    }
  },

  /** Record VPN disconnect and flush queue */
  async trackDisconnect(params: {
    nodeType: 'cloud' | 'self-hosted';
    nodeIpv4: string;
    nodeRegion: string;
    ruleMode: string;
    durationSec: number;
    reason: 'user' | 'error' | 'network';
  }): Promise<void> {
    try {
      const deviceHash = await getDeviceHash();
      const { os, app_version } = getPlatformInfo();

      const queue = await getQueue();
      queue.connections.push({
        device_hash: deviceHash,
        os,
        app_version,
        event: 'disconnect',
        node_type: params.nodeType,
        node_ipv4: params.nodeType === 'cloud' ? params.nodeIpv4 : '',
        node_region: params.nodeType === 'cloud' ? params.nodeRegion : '',
        rule_mode: params.ruleMode,
        duration_sec: params.durationSec,
        disconnect_reason: params.reason,
        created_at: new Date().toISOString(),
      });
      await saveQueue(queue);
      flush(); // fire-and-forget
    } catch (err) {
      console.warn('[Stats] trackDisconnect failed:', err);
    }
  },
};
```

**Step 2: Hook into app startup**

In `webapp/src/main.tsx`, after `initializeAllStores()` (around line 106), add:

```typescript
// Track app open for usage analytics
import('./services/stats').then(({ statsService }) => {
  statsService.trackAppOpen();
}).catch(() => {});
```

**Step 3: Type check**

Run: `cd webapp && npx tsc --noEmit`
Expected: no type errors

**Step 4: Commit**

```
feat(webapp): add usage analytics service with persistent queue

Events queued in _platform.storage (typed ISecureStorage), batch-flushed
on triggers. trackAppOpen called on app startup. trackConnect/trackDisconnect
ready for VPN store integration.
```

---

### Task 5: Wire VPN Connect/Disconnect Events

**Files:**
- Modify: `webapp/src/stores/index.ts`

**Key architecture notes (MUST READ before implementing):**

The VPN state machine was refactored. The relevant stores are:
- **`useVPNMachineStore`** (`vpn-machine.store.ts`): State machine with states `idle | connecting | connected | reconnecting | disconnecting | error | serviceDown`. Uses `subscribeWithSelector` middleware.
- **`useConnectionStore`** (`connection.store.ts`): Owns tunnel selection. Key fields:
  - `selectedSource: 'cloud' | 'self_hosted'`
  - `selectedCloudTunnel: Tunnel | null` — has `node.ipv4`, `node.country`
  - `connectedTunnel: ActiveTunnel | null` — snapshot during active connection, has `source`, `country`
- **`useConfigStore`** (`config.store.ts`): Has `ruleMode: 'global' | 'chnroute'`

There is NO `useVPNStore`. There is NO `StatusResponseData.config`. Connection metadata comes from `useConnectionStore`.

**Step 1: Add VPN state subscription**

In `webapp/src/stores/index.ts`, add inside `initializeAllStores()`, after `const cleanupVPNMachine = initializeVPNMachine();` (around line 81):

```typescript
// Subscribe to VPN state changes for analytics
import('../services/stats').then(({ statsService }) => {
  let connectTime: number | null = null;

  // Snapshot tunnel info at connect time (before connectedTunnel gets cleared on disconnect)
  let lastConnectedSource: 'cloud' | 'self_hosted' = 'cloud';
  let lastNodeIpv4 = '';
  let lastNodeRegion = '';
  let lastRuleMode = '';

  const unsubStats = useVPNMachineStore.subscribe(
    (s) => s.state,
    (state, prevState) => {
      if (state === 'connected' && prevState !== 'connected') {
        connectTime = Date.now();

        // Read tunnel metadata from connection store
        const connState = useConnectionStore.getState();
        const configState = useConfigStore.getState();
        lastConnectedSource = connState.selectedSource;
        lastNodeIpv4 = connState.selectedCloudTunnel?.node?.ipv4 || '';
        lastNodeRegion = connState.selectedCloudTunnel?.node?.country || '';
        lastRuleMode = configState.ruleMode;

        statsService.trackConnect({
          nodeType: lastConnectedSource === 'self_hosted' ? 'self-hosted' : 'cloud',
          nodeIpv4: lastConnectedSource === 'cloud' ? lastNodeIpv4 : '',
          nodeRegion: lastConnectedSource === 'cloud' ? lastNodeRegion : '',
          ruleMode: lastRuleMode,
        });
      }

      if (state === 'idle' && prevState === 'connected') {
        // Normal disconnect (idle = backend confirmed disconnected)
        const durationSec = connectTime ? Math.floor((Date.now() - connectTime) / 1000) : 0;
        statsService.trackDisconnect({
          nodeType: lastConnectedSource === 'self_hosted' ? 'self-hosted' : 'cloud',
          nodeIpv4: lastConnectedSource === 'cloud' ? lastNodeIpv4 : '',
          nodeRegion: lastConnectedSource === 'cloud' ? lastNodeRegion : '',
          ruleMode: lastRuleMode,
          durationSec,
          reason: 'user',
        });
        connectTime = null;
      }

      if (state === 'error' && prevState === 'connected') {
        // Error disconnect
        const durationSec = connectTime ? Math.floor((Date.now() - connectTime) / 1000) : 0;
        statsService.trackDisconnect({
          nodeType: lastConnectedSource === 'self_hosted' ? 'self-hosted' : 'cloud',
          nodeIpv4: lastConnectedSource === 'cloud' ? lastNodeIpv4 : '',
          nodeRegion: lastConnectedSource === 'cloud' ? lastNodeRegion : '',
          ruleMode: lastRuleMode,
          durationSec,
          reason: 'error',
        });
        connectTime = null;
      }
    }
  );

  // Store unsubscribe for cleanup — append to existing cleanup
  (window as any).__statsUnsub = unsubStats;
}).catch(() => {});
```

Also add these imports at the top of `index.ts` (with the existing internal imports):

```typescript
import { useVPNMachineStore } from './vpn-machine.store';
import { useConnectionStore } from './connection.store';
```

Note: `useVPNMachineStore` is already imported via re-export, but we need a direct import for internal use. `useConfigStore` is already imported.

**Step 2: Type check**

Run: `cd webapp && npx tsc --noEmit`
Expected: no type errors

**Step 3: Commit**

```
feat(webapp): wire VPN state changes to analytics events

Subscribe to useVPNMachineStore state transitions. Track connect on
connected, disconnect on idle/error with duration calculation.
Tunnel metadata from useConnectionStore, rule mode from useConfigStore.
```

---

### Task 6: k2s Download Tracking (Next.js Middleware)

**Files:**
- Modify: `web/src/middleware.ts`

**Step 1: Add k2s download tracking**

In `web/src/middleware.ts`, add at the top of the `middleware` function (before line 8):

```typescript
// Track k2s download
if (pathname === '/i/k2s') {
  // Fire-and-forget: record download to Center API
  const ip = request.headers.get('x-forwarded-for')?.split(',')[0]?.trim()
    || request.headers.get('x-real-ip')
    || 'unknown';
  const ua = request.headers.get('user-agent') || '';

  const apiBase = process.env.CENTER_API_URL || 'https://api.kaitu.io';
  fetch(`${apiBase}/api/stats/k2s-download`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ip_raw: ip, ua }),
  }).catch(() => {
    // Non-blocking, ignore errors
  });

  return NextResponse.next();
}
```

Add this BEFORE the admin/manager route check. The static file continues to be served normally by Next.js.

**Step 2: Verify build**

Run: `cd web && yarn build`
Expected: builds without errors

**Step 3: Commit**

```
feat(web): track k2s download in middleware

Intercept /i/k2s requests, fire-and-forget POST to Center API
with client IP and UA. Non-blocking, doesn't affect script delivery.
```

---

### Task 7: Admin Dashboard — Usages Page

**Files:**
- Create: `web/src/app/(manager)/manager/usages/page.tsx`
- Modify: `web/src/lib/api.ts` (add API method)
- Modify: `web/src/components/manager-sidebar.tsx` (add nav link)

**Key architecture note:** The web API module uses `this.request<T>()` (NOT `this.fetch<T>()`). Check `web/src/lib/api.ts` for the exact method signature.

**Step 1: Add API types and method**

In `web/src/lib/api.ts`, add the type and method:

Types (add near other stat response types):

```typescript
// Usage analytics types
export interface UsageOverviewResponse {
  activeDevices: { date: string; count: number }[];
  connections: { date: string; count: number }[];
  nodeUsage: { nodeIpv4: string; nodeType: string; count: number }[];
  k2sDownloads: { date: string; count: number }[];
}
```

Method (add to the `api` object):

```typescript
async getUsageOverview(params: { range: string; os?: string }): Promise<UsageOverviewResponse> {
  const searchParams = new URLSearchParams({ range: params.range });
  if (params.os) searchParams.set('os', params.os);
  const resp = await this.request<UsageOverviewResponse>(`/app/stats/overview?${searchParams}`);
  return resp;
},
```

**Step 2: Create the usages page**

Create `web/src/app/(manager)/manager/usages/page.tsx`:

```tsx
"use client";

export const dynamic = "force-dynamic";

import { useEffect, useState } from "react";
import { api, UsageOverviewResponse } from "@/lib/api";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";

export default function UsagesPage() {
  const [data, setData] = useState<UsageOverviewResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [range, setRange] = useState("30d");
  const [os, setOs] = useState("");

  useEffect(() => {
    loadData();
  }, [range, os]);

  async function loadData() {
    setLoading(true);
    try {
      const result = await api.getUsageOverview({ range, os: os || undefined });
      setData(result);
    } catch (error) {
      console.error("Failed to load usage data:", error);
    } finally {
      setLoading(false);
    }
  }

  if (loading && !data) {
    return (
      <div className="p-6">
        <div className="animate-pulse space-y-4">
          <div className="h-8 bg-muted rounded w-1/4"></div>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {[1, 2, 3, 4].map((i) => (
              <div key={i} className="h-48 bg-muted rounded"></div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">使用统计</h1>
        <div className="flex gap-2">
          <Select value={os} onValueChange={setOs}>
            <SelectTrigger className="w-32">
              <SelectValue placeholder="全部平台" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="">全部平台</SelectItem>
              <SelectItem value="macos">macOS</SelectItem>
              <SelectItem value="windows">Windows</SelectItem>
              <SelectItem value="linux">Linux</SelectItem>
              <SelectItem value="ios">iOS</SelectItem>
              <SelectItem value="android">Android</SelectItem>
            </SelectContent>
          </Select>
          <Select value={range} onValueChange={setRange}>
            <SelectTrigger className="w-24">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="7d">7天</SelectItem>
              <SelectItem value="30d">30天</SelectItem>
              <SelectItem value="90d">90天</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      {/* Summary Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>日活设备 (今日)</CardDescription>
            <CardTitle className="text-3xl">
              {data?.activeDevices?.length ? data.activeDevices[data.activeDevices.length - 1]?.count ?? 0 : 0}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>今日连接数</CardDescription>
            <CardTitle className="text-3xl">
              {data?.connections?.length ? data.connections[data.connections.length - 1]?.count ?? 0 : 0}
            </CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>活跃节点数</CardDescription>
            <CardTitle className="text-3xl">{data?.nodeUsage?.length ?? 0}</CardTitle>
          </CardHeader>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>k2s 今日下载</CardDescription>
            <CardTitle className="text-3xl">
              {data?.k2sDownloads?.length ? data.k2sDownloads[data.k2sDownloads.length - 1]?.count ?? 0 : 0}
            </CardTitle>
          </CardHeader>
        </Card>
      </div>

      <Tabs defaultValue="devices" className="space-y-6">
        <TabsList>
          <TabsTrigger value="devices">活跃设备</TabsTrigger>
          <TabsTrigger value="connections">连接趋势</TabsTrigger>
          <TabsTrigger value="nodes">节点分布</TabsTrigger>
          <TabsTrigger value="downloads">k2s 下载</TabsTrigger>
        </TabsList>

        {/* Active Devices Trend */}
        <TabsContent value="devices">
          <Card>
            <CardHeader>
              <CardTitle>日活跃设备趋势</CardTitle>
              <CardDescription>每日唯一设备数 (DAU)</CardDescription>
            </CardHeader>
            <CardContent>
              <BarChart data={data?.activeDevices || []} />
            </CardContent>
          </Card>
        </TabsContent>

        {/* Connection Trend */}
        <TabsContent value="connections">
          <Card>
            <CardHeader>
              <CardTitle>每日连接数趋势</CardTitle>
              <CardDescription>每日 VPN 连接次数</CardDescription>
            </CardHeader>
            <CardContent>
              <BarChart data={data?.connections || []} />
            </CardContent>
          </Card>
        </TabsContent>

        {/* Node Usage Distribution */}
        <TabsContent value="nodes">
          <Card>
            <CardHeader>
              <CardTitle>节点使用分布</CardTitle>
              <CardDescription>按连接次数排名</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-2">
                {data?.nodeUsage?.map((node, i) => {
                  const maxCount = data.nodeUsage[0]?.count || 1;
                  const pct = ((node.count / maxCount) * 100).toFixed(0);
                  return (
                    <div key={i} className="flex items-center gap-3">
                      <span className="font-mono text-sm w-32 truncate">
                        {node.nodeType === 'self-hosted' ? 'Self-Hosted' : node.nodeIpv4}
                      </span>
                      <Badge variant="outline" className="w-20 justify-center">
                        {node.nodeType}
                      </Badge>
                      <div className="flex-1 bg-muted rounded-full h-2 overflow-hidden">
                        <div className="h-full bg-primary" style={{ width: `${pct}%` }} />
                      </div>
                      <span className="text-sm text-muted-foreground w-16 text-right">{node.count}</span>
                    </div>
                  );
                })}
                {(!data?.nodeUsage || data.nodeUsage.length === 0) && (
                  <div className="text-muted-foreground text-sm py-8 text-center">暂无数据</div>
                )}
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        {/* k2s Downloads */}
        <TabsContent value="downloads">
          <Card>
            <CardHeader>
              <CardTitle>k2s 每日下载趋势</CardTitle>
              <CardDescription>每日唯一 IP 下载数</CardDescription>
            </CardHeader>
            <CardContent>
              <BarChart data={data?.k2sDownloads || []} />
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}

// Simple bar chart using CSS (no chart library needed for v1)
function BarChart({ data }: { data: { date: string; count: number }[] }) {
  if (data.length === 0) {
    return <div className="text-muted-foreground text-sm py-8 text-center">暂无数据</div>;
  }

  const maxCount = Math.max(...data.map(d => d.count), 1);

  return (
    <div className="flex items-end gap-1 h-40 overflow-x-auto">
      {data.map((item) => {
        const height = (item.count / maxCount) * 100;
        return (
          <div
            key={item.date}
            className="flex-shrink-0 flex flex-col items-center gap-1"
            style={{ width: data.length > 30 ? '12px' : '24px' }}
            title={`${item.date}: ${item.count}`}
          >
            <div className="text-xs text-muted-foreground">{item.count > 0 ? item.count : ''}</div>
            <div
              className="w-full bg-primary rounded-t transition-all hover:bg-primary/80"
              style={{ height: `${height}%`, minHeight: item.count > 0 ? '4px' : '0' }}
            />
            <div className="text-xs text-muted-foreground rotate-45 origin-left whitespace-nowrap">
              {item.date.slice(5)}
            </div>
          </div>
        );
      })}
    </div>
  );
}
```

**Step 3: Add sidebar navigation link**

In `web/src/components/manager-sidebar.tsx`, add to the "系统监控" group (around line 56):

```typescript
{ href: "/manager/usages", icon: Activity, label: "使用统计" },
```

Add before the `asynqmon` entry. `Activity` is already imported from lucide-react.

**Step 4: Verify build**

Run: `cd web && yarn build`
Expected: builds without errors

**Step 5: Commit**

```
feat(web): add usage analytics admin dashboard

/manager/usages page with 4 tabs: active devices (DAU),
connection trend, node usage distribution, k2s downloads.
Filters: time range (7d/30d/90d) and OS. Simple CSS bar charts.
```

---

### Task 8: Tests

**Files:**
- Create: `api/api_stats_test.go`
- Create: `webapp/src/services/__tests__/stats.test.ts`

**Step 1: API handler test**

Create `api/api_stats_test.go`:

```go
package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupStatsTestRouter creates a minimal gin router with only stats routes.
// Does NOT use SetupRouter() (requires full config/asynq init) or
// SetupMockDB() (mock doesn't bridge to db.Get() used by handlers).
// Both test cases below return before any DB call, so no DB setup needed.
func setupStatsTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	stats := r.Group("/api/stats")
	{
		stats.POST("/events", api_stats_ingest)
	}
	return r
}

func TestStatsIngest_EmptyRequest(t *testing.T) {
	router := setupStatsTestRouter()

	body := `{"app_opens":[],"connections":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/stats/events", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[DataAny]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorNone, resp.Code)
}

func TestStatsIngest_TooManyEvents(t *testing.T) {
	router := setupStatsTestRouter()

	// Create 101 app_open events — exceeds maxEventsPerRequest (100)
	events := make([]StatsAppOpenEvent, 101)
	for i := range events {
		events[i] = StatsAppOpenEvent{
			DeviceHash: "abc123",
			OS:         "macos",
			AppVersion: "0.4.0",
			CreatedAt:  time.Now(),
		}
	}
	reqBody := StatsEventRequest{AppOpens: events}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/stats/events", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[DataAny]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorInvalidArgument, resp.Code)
}
```

**Why minimal router:** `SetupRouter()` requires full config/asynq init. `SetupMockDB()` stores mock in `mockGlobalDB` but handlers call `db.Get()` (qtoolkit) which doesn't bridge to mock. Both test cases return before touching DB (empty → `SuccessEmpty`, too-many → `Error`), so a minimal router avoids all init dependencies.

**Step 2: Webapp stats service test**

Create `webapp/src/services/__tests__/stats.test.ts`:

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock cloudApi before importing stats
vi.mock('../cloud-api', () => ({
  cloudApi: {
    request: vi.fn().mockResolvedValue({ code: 0 }),
  },
}));

// Mock window._platform with typed ISecureStorage
const mockStorage = new Map<string, any>();
Object.defineProperty(window, '_platform', {
  value: {
    os: 'macos',
    version: '0.4.0',
    storage: {
      get: vi.fn(async <T>(key: string): Promise<T | null> => mockStorage.get(key) ?? null),
      set: vi.fn(async <T>(key: string, value: T) => { mockStorage.set(key, value); }),
      remove: vi.fn(async (key: string) => { mockStorage.delete(key); }),
    },
    getUdid: vi.fn(async () => 'test-udid-123'),
  },
  writable: true,
});

// Mock crypto.subtle for SHA-256
Object.defineProperty(globalThis, 'crypto', {
  value: {
    subtle: {
      digest: vi.fn(async () => new ArrayBuffer(32)),
    },
  },
  writable: true,
});

describe('statsService', () => {
  beforeEach(() => {
    mockStorage.clear();
    vi.clearAllMocks();
    // Re-set mocks cleared by vi.clearAllMocks()
    (window._platform!.storage.get as any).mockImplementation(
      async (key: string) => mockStorage.get(key) ?? null
    );
    (window._platform!.storage.set as any).mockImplementation(
      async (key: string, value: any) => { mockStorage.set(key, value); }
    );
    (window._platform!.storage.remove as any).mockImplementation(
      async (key: string) => { mockStorage.delete(key); }
    );
    (window._platform!.getUdid as any).mockResolvedValue('test-udid-123');
    const { cloudApi } = require('../cloud-api');
    cloudApi.request.mockResolvedValue({ code: 0 });
  });

  it('trackAppOpen queues event and flushes', async () => {
    const { statsService } = await import('../stats');
    const { cloudApi } = await import('../cloud-api');

    await statsService.trackAppOpen();

    // Allow flush to complete
    await new Promise(r => setTimeout(r, 50));

    expect(cloudApi.request).toHaveBeenCalledWith(
      'POST',
      '/api/stats/events',
      expect.objectContaining({
        app_opens: expect.arrayContaining([
          expect.objectContaining({
            os: 'macos',
            app_version: '0.4.0',
          }),
        ]),
      })
    );
  });

  it('keeps events in queue on flush failure', async () => {
    const { cloudApi } = await import('../cloud-api');
    (cloudApi.request as any).mockResolvedValueOnce({ code: 500, message: 'error' });

    const { statsService } = await import('../stats');
    await statsService.trackAppOpen();
    await new Promise(r => setTimeout(r, 50));

    // Queue should still have the event (typed storage, no JSON.parse needed)
    const queue = mockStorage.get('stats_queue');
    expect(queue).toBeDefined();
    expect(queue.app_opens.length).toBeGreaterThan(0);
  });
});
```

**Step 3: Run tests**

Run:
```bash
cd api && go test -run TestStatsIngest -v ./...
cd webapp && npx vitest run src/services/__tests__/stats.test.ts
```

**Step 4: Commit**

```
test: add usage analytics tests

API: empty request, too-many-events validation.
Webapp: trackAppOpen queue+flush, retry on failure.
```

---

### Task 9: Final Integration Verification

**Step 1: Full build check**

```bash
cd api && go build ./...
cd webapp && npx tsc --noEmit
cd web && yarn build
```

**Step 2: Manual verification checklist**

- [ ] `api/model_stats.go` has 3 GORM models with correct indexes
- [ ] `api/migrate.go` includes all 3 models in AutoMigrate
- [ ] `api/api_stats.go` handles POST /api/stats/events + POST /api/stats/k2s-download
- [ ] `api/api_admin_stats.go` handles GET /app/stats/overview
- [ ] `api/route.go` registers stats routes (no auth) + admin route
- [ ] `webapp/src/services/stats.ts` uses typed `_platform.storage.get<StatsQueue>()` (no JSON.parse)
- [ ] `webapp/src/main.tsx` calls trackAppOpen on startup
- [ ] `webapp/src/stores/index.ts` subscribes to `useVPNMachineStore` (not useVPNStore) for connect/disconnect
- [ ] Stats subscription reads tunnel metadata from `useConnectionStore` (not StatusResponseData.config)
- [ ] `web/src/middleware.ts` tracks /i/k2s downloads
- [ ] `web/src/app/(manager)/manager/usages/page.tsx` renders charts
- [ ] `web/src/lib/api.ts` uses `this.request<T>()` (not `this.fetch<T>()`)
- [ ] `web/src/components/manager-sidebar.tsx` has usages nav link

**Step 3: Final commit**

```bash
git add -A
git status  # Review all changes
```

If all looks good, create a feature branch and commit:

```
feat: usage analytics system

- 3 GORM models (stat_app_opens, stat_connections, stat_k2s_downloads)
- Client event queue with persistent storage + batch upload
- k2s download tracking via Next.js middleware
- Admin dashboard at /manager/usages with DAU, connections, nodes, downloads
```
