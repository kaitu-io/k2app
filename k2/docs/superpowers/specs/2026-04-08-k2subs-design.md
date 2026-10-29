# k2subs:// Subscription URL Design

Date: 2026-04-08

## Overview

`k2subs://` is a subscription URL scheme for the k2 client. It points to an HTTPS endpoint that returns a weighted list of `k2v5://` tunnel URLs. The client periodically fetches this endpoint to keep its tunnel list up to date, enabling server migration, scaling, and failover without client-side config changes.

## URL Format

```
k2subs://user:pass@host/path?refresh=1800
```

| Part | Purpose |
|------|---------|
| `user:pass` | HTTP Basic Auth for fetching + injected as UDID:Token into all child tunnel URLs |
| `host/path` | Replace `k2subs` with `https` to get the fetch endpoint |
| `refresh` | Client-side fallback refresh interval in seconds (default 1800). Overridden by server response. |

No other query parameters. The URL is designed to be short and copy-pasteable.

## HTTP Response Format

```
GET https://host/path
Authorization: Basic base64(user:pass)
```

```json
{
  "tunnels": [
    {"url": "k2v5://server-tokyo:443?ech=...&pin=...", "weight": 50},
    {"url": "k2v5://server-hk:443?ech=...",            "weight": 30},
    {"url": "k2v5://server-sg:443?ech=...",             "weight": 0}
  ],
  "refresh": 600
}
```

### Fields

**tunnels[]**:
- `url` (required): k2v5:// connection descriptor. MUST NOT contain userinfo — credentials are injected from the k2subs URL. If a child URL contains userinfo, the k2subs credentials override it.
- `weight` (optional, default 1): Weighted random selection ratio. `weight=0` means failover-only — the server is not selected under normal conditions, only when all non-zero servers are exhausted.

**refresh** (optional): Server-controlled refresh interval in seconds.

### Refresh Priority

The effective refresh interval is determined by (highest priority first):

1. Server response `"refresh"` field — evaluated on every fetch, so the server can dynamically adjust
2. Client URL `?refresh=` parameter — used when server response omits the field
3. Default: 1800 seconds (30 minutes)

Example scenario:
- Normal operation: server returns `"refresh": 1800` (30min)
- During server migration: server returns `"refresh": 60` (1min) — clients automatically accelerate polling
- Migration complete: server reverts to `"refresh": 1800`

## Authentication

Single credential, two uses:

1. **Fetch**: HTTP Basic Auth header when requesting the subscription endpoint
2. **Tunnel connection**: Injected as `user:pass@` userinfo into each returned k2v5:// URL

This means the subscription server and tunnel servers share the same user identity system. One k2subs URL = one user identity + one server list.

## Selection Strategy

**Weighted random** distributes clients across servers proportionally:
- `weight=50` server is selected with probability 50/(50+30) = 62.5% (excluding weight=0)
- 1000 clients naturally spread across servers by weight ratio

**Failover on connection failure**:
1. Pick a server by weighted random from available list
2. Connection fails → pick again from remaining servers (exclude failed ones), re-weighted
3. All non-zero weight servers failed → try weight=0 backup servers
4. All servers failed → return error, engine enters retry loop (clears exclude list on next cycle)

## Subscription Structure

Independent module (`config/subscription.go`), no dependency on engine/wire/daemon.

```go
type Subscription struct {
    // Immutable after construction
    raw       string            // original k2subs:// URL
    endpoint  string            // https:// endpoint
    creds     string            // user:pass extracted from URL
    refresh   time.Duration     // client-side fallback refresh interval
    cacheFile string            // disk cache path

    // State (mu-protected)
    mu        deadlock.Mutex
    tunnels   []Tunnel          // current tunnel list
    fetchedAt time.Time         // last successful fetch time
    interval  time.Duration     // effective refresh interval (from last fetch)

    cancel    context.CancelFunc
}

type Tunnel struct {
    URL    string // k2v5://user:pass@host:port?... (credentials already injected)
    Weight int    // weighted random ratio, 0 = failover-only
}
```

### Core Methods

```go
// Parse k2subs:// URL, load disk cache if available.
func NewSubscription(raw string, cacheDir string) (*Subscription, error)

// Fetch latest tunnel list. httpClient provided by caller (controls direct vs proxy).
func (s *Subscription) Fetch(ctx context.Context, httpClient *http.Client) error

// Weighted random selection, excluding failed URLs.
func (s *Subscription) Pick(exclude []string) (Tunnel, error)

// Return current cached tunnel list.
func (s *Subscription) Tunnels() []Tunnel

// Start background periodic refresh. clientFn allows caller to switch direct/proxy.
func (s *Subscription) StartRefresh(ctx context.Context, clientFn func() *http.Client)

// Stop background refresh.
func (s *Subscription) Close()
```

**Why httpClient is caller-provided**: Subscription does not know about "direct" vs "proxy" — that is an engine-layer concept. The engine passes a direct HTTP client for the initial fetch, and a factory function for background refresh that can try direct first, then fall back to fetching through the active tunnel.

## Reliability

**Disk cache**:
- On every successful fetch → write to `{cacheDir}/subs-{hash}.json` (hash = first 8 bytes of URL's SHA-256)
- `NewSubscription()` loads cache on construction → even if the first fetch fails (offline restart), `Pick()` has data
- Cache has no expiration — stale data is better than no data

**Fetch failure tolerance**:
- Failed fetch never clears the in-memory tunnel list; `Pick()` continues using old data
- Only a successful fetch replaces the list and updates the disk cache

**Pick failure tolerance**:
- All non-zero weight servers excluded → fall back to weight=0 backup servers
- All servers excluded → return error

## Engine Integration

### Startup

```
engine.Start()
  ├─ Discover via = k2subs:// in routes
  ├─ sub := NewSubscription(url, cacheDir)
  ├─ sub.Fetch(ctx, directHTTPClient)
  │   ├─ Success → proceed
  │   └─ Failure → check disk cache
  │       ├─ Cache exists → proceed with cached data
  │       └─ No cache → Start() returns error
  ├─ tunnel := sub.Pick(nil)
  ├─ wireCfg := wire.ParseURL(tunnel.URL)
  ├─ resolveDialAddr → Happy Eyeballs
  ├─ wire.RaceTransport → QUIC/TCP-WS race
  ├─ connect
  └─ sub.StartRefresh(ctx, clientFactory)
```

### Reconnect (failover)

```
Current connection drops → engine reconnect logic:
  ├─ sub.Pick(exclude=[failed URLs])
  ├─ wire.ParseURL → connect
  └─ If all exhausted → error → engine retry loop (clears exclude)
```

### Background Refresh

```
Every {effective refresh interval}:
  ├─ clientFactory() → httpClient (engine decides direct vs proxy)
  ├─ sub.Fetch(ctx, client)
  │   ├─ Success → update in-memory + disk cache + effective interval
  │   └─ Failure → keep old data, retry next interval
  └─ Do NOT interrupt current connection
      New list takes effect on next reconnect/failover
```

## Configuration

k2subs:// replaces k2v5:// in the `via` field of routes:

```yaml
routes:
  - via: k2subs://alice:tok@relay.example.com/subs/team-a?refresh=1800
    match: {all: true}
```

Combined with other routes:

```yaml
routes:
  - via: k2subs://alice:tok@relay.example.com/subs/team-a
    match: {all: true}
  - via: direct
    match: {domain_suffix: [".cn"]}
```

Multiple k2subs routes are supported — each creates an independent Subscription instance:

```yaml
routes:
  - via: k2subs://alice:tok@relay.example.com/subs/proxy
    match: {all: true}
  - via: k2subs://alice:tok@relay.example.com/subs/streaming
    match: {domain_suffix: [".netflix.com"]}
```

## CLI Changes

### Removed: `k2 connect`

The `k2 connect` command is removed. Its functionality is fully covered by `k2 up`:

- `k2 connect <URL>` → `k2 up <URL>`
- `k2 up` provides daemon-managed reconnect, state persistence, and HTTP API — capabilities that `connect` lacked

Files deleted: `cmd/k2/connect.go`
Files modified: `cmd/k2/main.go` (remove dispatch case), `cmd/k2/CLAUDE.md` (update docs)

### `k2 setup` with k2subs

`k2 setup k2subs://...` stores the k2subs URL as-is in config.yml. It does NOT fetch or expand the subscription at setup time — fetching happens at `engine.Start()`.

### `k2 status` extension

When connected via a k2subs subscription, `k2 status` shows:
- Subscription source (host/path)
- Current server (the one connected)
- Available servers count
- Last refresh time
