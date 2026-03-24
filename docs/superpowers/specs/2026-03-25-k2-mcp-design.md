# k2-mcp: Customer-Facing MCP Server

**Date**: 2026-03-25
**Status**: Draft
**Location**: `k2app/mcp/`

## Overview

Standalone Go binary (`k2-mcp`) that exposes k2 tunnel operations and Kaitu account management as MCP tools. Designed for customer AI agents (Claude Code, Cursor, etc.) to manage their VPN tunnel and subscription programmatically.

**Not** kaitu-center (internal ops). This is the customer-facing counterpart.

## Architecture

```
AI Agent ←stdio→ k2-mcp ──HTTP──→ Center API (auth, servers, plans, orders)
                         ──HTTP──→ k2 daemon localhost:1777 (connect, disconnect, status)
```

- **Transport**: stdio (standard MCP protocol)
- **SDK**: `github.com/modelcontextprotocol/go-sdk/mcp` v1.4.1+ (official Go SDK, stable, requires Go 1.25+)
- **Distribution**: Built as Go binary, bundled into Tauri desktop app as sidecar (same pattern as k2 binary)

## MCP Tools (8 total)

### 1. `login`

Authenticate with Kaitu account using email and password.

**Input**:
```json
{
  "email": "string — user email address",
  "password": "string — account password"
}
```

**Backend call**: `POST {center_api}/api/auth/login/password`

Request body:
```json
{
  "email": "<email>",
  "password": "<password>",
  "udid": "<generated-device-uuid>",
  "remark": "k2-mcp",
  "platform": "mcp"
}
```

**Output** (success):
```json
{
  "email": "user@example.com"
}
```

Note: `DataAuthResult` only contains `accessToken`, `refreshToken`, `issuedAt`. Subscription expiry is NOT available from the login response — it comes from `GET /api/user` (`DataUser.ExpiredAt`). The `account_info` tool provides this.

**Side effects**:
- Stores `accessToken` and `refreshToken` in process memory
- Persists tokens to `~/.kaitu/mcp-session.json` for session continuity across restarts
- Generates a stable UDID per machine (hash of hostname + MAC), stored in `~/.kaitu/mcp-udid`

**Token refresh**: Automatic. Center API always returns HTTP 200; auth failure is indicated by JSON `code: 401` in the response body. When any API call gets `code: 401`, k2-mcp calls `POST /api/auth/refresh` with the stored refresh token. If refresh fails, returns error instructing AI to call `login` again.

### 2. `account_info`

Get current account status and subscription details.

**Input**: none

**Backend call**: `GET {center_api}/api/user` with `Authorization: Bearer <token>`

**Output**:
```json
{
  "email": "user@example.com",
  "plan_expires_at": "2026-04-25T00:00:00Z",
  "is_active": true,
  "device_count": 2,
  "device_limit": 5,
  "invite_code": "ABC123"
}
```

Fields derived from Center API response:
- `email` ← `loginIdentifies[type=email].value`
- `plan_expires_at` ← `expiredAt` (Unix → ISO8601)
- `is_active` ← `expiredAt > now()`
- `device_count` ← `deviceCount`
- `device_limit` ← hardcoded 5 (Center API does not return this; update here if server default changes)
- `invite_code` ← `inviteCode.code`

**Requires auth**: Yes. Returns error if not logged in.

### 3. `list_plans`

Get available subscription plans with pricing.

**Input**: none

**Backend call**: `GET {center_api}/api/plans` (public, no auth required)

**Output**:
```json
{
  "plans": [
    {
      "id": "pro_month",
      "name": "Pro Monthly",
      "price_cents": 999,
      "original_price_cents": 1299,
      "months": 1,
      "highlight": true
    },
    {
      "id": "pro_year",
      "name": "Pro Yearly",
      "price_cents": 9999,
      "original_price_cents": 12990,
      "months": 12,
      "highlight": false
    }
  ]
}
```

Fields mapped from Center API:
- `id` ← `pid`
- `name` ← `label`
- `price_cents` ← `price`
- `original_price_cents` ← `originPrice`
- `months` ← `month`
- `highlight` ← `highlight`

Only plans with `isActive=true` are returned.

### 4. `subscribe`

Create an order and get a payment URL.

**Input**:
```json
{
  "plan_id": "string — plan ID from list_plans (e.g. 'pro_month')",
  "campaign_code": "string? — optional discount code"
}
```

**Backend call**: `POST {center_api}/api/user/orders` with `Authorization: Bearer <token>`

Request body:
```json
{
  "preview": false,
  "plan": "<plan_id>",
  "campaignCode": "<campaign_code or omitted>",
  "forMyself": true
}
```

**Output** (success):
```json
{
  "order_id": "ord_abc123",
  "payment_url": "https://payment.wordgate.com/order/123",
  "amount_cents": 799,
  "original_amount_cents": 999,
  "discount_cents": 200,
  "plan": "Pro Monthly"
}
```

**Requires auth**: Yes.

**AI behavior note**: The AI should present the `payment_url` to the user to complete payment in their browser. Payment is not completed within MCP.

### 5. `list_servers`

Get available VPN servers for connection.

**Input**: none

**Backend call**: `GET {center_api}/api/tunnels/k2v5` with `Authorization: Bearer <token>` and `X-UDID: <stored-udid>`

**Output**:
```json
{
  "servers": [
    {
      "id": 123,
      "name": "US East 1",
      "domain": "server.example.com",
      "country": "US",
      "region": "us-east-1",
      "traffic_usage_percent": 60.5,
      "bandwidth_usage_percent": 45.2,
      "server_url": "k2v5://server.example.com:443?..."
    }
  ]
}
```

Fields mapped from Center API (`DataSlaveTunnel`):
- `id` ← `id`
- `name` ← `name` (falls back to `node.name`)
- `domain` ← `domain`
- `country` ← `node.country`
- `region` ← `node.region`
- `traffic_usage_percent` ← `node.trafficUsagePercent` (0-100, replaces deprecated `load`)
- `bandwidth_usage_percent` ← `node.bandwidthUsagePercent` (0-100)
- `server_url` ← `serverUrl` (only present for k2v5 protocol)

Note: `node.load` is deprecated in the Center API. Use `trafficUsagePercent` and `bandwidthUsagePercent` instead.

Middleware chain: `AuthRequired()` + `ProRequired()` + `DeviceAuthRequired()` — requires active subscription and `X-UDID` header.

**Requires auth**: Yes. Returns error with `"subscription expired"` if Center API responds with `code: 402`.

### 6. `connect`

Connect to a VPN server.

**Input**:
```json
{
  "server_id": "number — server ID from list_servers"
}
```

**Flow**:
1. Use cached server list if available and not expired; otherwise call Center API `GET /api/tunnels/k2v5` to refresh
2. Resolve `server_id` → `server_url` from the (cached) server list
3. Ping k2 daemon: `GET http://{daemon_addr}/ping` with 2s timeout. If unreachable, return error with actionable instructions
4. Call k2 daemon: `POST http://{daemon_addr}/api/core`

Request body to daemon (`config.ClientConfig` JSON — daemon unmarshals into `config.ClientConfig` struct, then calls `config.SetDefaults()` to fill missing fields):
```json
{
  "action": "up",
  "params": {
    "config": {
      "server": "<server_url from Center API>"
    }
  }
}
```

The `server` field maps to `config.ClientConfig.Server` (JSON tag: `"server"`). The daemon calls `config.SetDefaults(cfg)` which fills `mode: "tun"`, DNS upstreams, and other defaults. Only the `server` URL is required.

**Output** (success):
```json
{
  "state": "connecting",
  "server": "US East 1"
}
```

**Error outputs** (all use MCP `isError: true`):
- Daemon not running: `{"error": "k2 daemon is not running. Start it with 'k2' or install as a service with 'k2 service install'."}`
- Subscription expired: `{"error": "subscription expired, please subscribe first"}`
- Server ID not found: `{"error": "server 999 not found, call list_servers to see available servers"}`
- Daemon connect failed: `{"error": "connect failed: <daemon error message>"}`

**Requires auth**: Yes (to resolve server_id via Center API).

### 7. `disconnect`

Disconnect the VPN tunnel.

**Input**: none

**Backend call**: `POST http://127.0.0.1:1777/api/core`

Request body:
```json
{
  "action": "down"
}
```

**Output**:
```json
{
  "status": "disconnected"
}
```

**Daemon not running**: Returns success (already disconnected).

**Requires auth**: No (daemon API is unauthenticated).

### 8. `status`

Get current VPN connection status.

**Input**: none

**Backend call**: `POST http://127.0.0.1:1777/api/core`

Request body:
```json
{
  "action": "status"
}
```

**Daemon API response** (`POST /api/core` action `"status"`) returns:
```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "state": "connected",
    "connected_at": "2026-03-25T14:30:00Z",
    "uptime_seconds": 3600,
    "config": { "server": "k2v5://...", ... },
    "error": { "code": 503, "message": "server unreachable" }
  }
}
```

k2-mcp transforms this into a simplified output:

**Output** (connected):
```json
{
  "state": "connected",
  "server": "US East 1",
  "uptime_seconds": 3600
}
```

**Output** (disconnected):
```json
{
  "state": "disconnected"
}
```

**Output** (error state):
```json
{
  "state": "disconnected",
  "error": "server unreachable",
  "error_code": 503
}
```

Fields derived from daemon response:
- `state` ← `data.state` (one of: `disconnected`, `connecting`, `connected`, `reconnecting`, `paused`)
- `server` ← resolved from `data.config.server` domain against cached server list; falls back to raw domain if no cache
- `uptime_seconds` ← `data.uptime_seconds` (only present when connected)
- `error` ← `data.error.message` (only present on error)
- `error_code` ← `data.error.code` (only present on error)

Note: `tx_mb`/`rx_mb` are NOT available from the status endpoint. They come from the SSE `stats` event stream (`GET /api/events`) or `GET /api/stats` history. k2-mcp does not expose traffic stats in v1 to keep the tool simple.

**Daemon not running**: Returns `{"state": "disconnected"}`.

**Requires auth**: No.

## Session Management

### Token Storage

```
~/.kaitu/
  mcp-session.json    ← {accessToken, refreshToken, issuedAt, email}
  mcp-udid            ← stable device UUID (generated once)
```

**Startup flow**:
1. Read `mcp-session.json` if exists
2. Validate token by calling `GET /api/user`
3. If valid → session restored, no login needed
4. If 401 → try refresh token
5. If refresh fails → clear session, tools that require auth return "please login first"

**UDID lifecycle**: Generated once on first run using `sha256(hostname + first-non-loopback-MAC)[:16]`, hex-encoded, and stored in `~/.kaitu/mcp-udid`. All subsequent startups read from this file. Hardware identifiers are never re-sampled — the file is the source of truth after initial generation.

### Server List Cache

k2-mcp caches the server list in memory after first `list_servers` call. Cache is used by:
- `connect` — to resolve server_id → server_url
- `status` — to resolve server domain → friendly name

Cache expires after 5 minutes. No disk persistence.

## Error Handling

All tools return MCP `TextContent` with JSON. Errors use `isError: true` on the `CallToolResult`.

### Error Categories

| Scenario | Center API `code` | Tool Response | isError |
|----------|------------------|--------------|---------|
| Not logged in | 401 | `{"error": "not logged in, please call login first"}` | true |
| Subscription expired | 402 | `{"error": "subscription expired", "expires_at": "..."}` | true |
| Invalid input | 422 | `{"error": "plan_id is required"}` | true |
| Invalid campaign code | 400001 | `{"error": "invalid campaign code"}` | true |
| Network error to Center API | — | `{"error": "cannot reach Kaitu servers, check network"}` | true |
| Daemon not running | — | `{"error": "k2 daemon is not running..."}` | true |
| Daemon connect failed | — | `{"error": "connect failed: <daemon message>"}` | true |
| Other Center API error | varies | `{"error": "<message from API>"}` | true |

Success responses do NOT include a `success` field — success is indicated by `isError: false` (MCP default) on the `CallToolResult`. Only the JSON payload fields are present.

### Token Auto-Refresh

Transparent to tools. Center API always returns HTTP 200 with error state in JSON `code` field. When any Center API call returns `code: 401` (not logged in):
1. Call `POST /api/auth/refresh` with stored refresh token
2. If success (refresh returns new tokens) → retry original request with new token, update `mcp-session.json`
3. If fail (refresh token also expired) → clear session file, return `{"error": "session expired, please login again"}` with `isError: true`

## Project Structure

```
k2app/mcp/
  go.mod               ← module github.com/kaitu-io/k2app/mcp
  go.sum
  main.go              ← MCP server entry point (stdio transport, tool registration)
  center_client.go     ← Center API HTTP client (base URL, auth headers, error mapping)
  session.go           ← Token storage, UDID generation, auto-refresh logic
  tool_login.go        ← login tool
  tool_account.go      ← account_info tool
  tool_plans.go        ← list_plans tool
  tool_subscribe.go    ← subscribe tool
  tool_servers.go      ← list_servers tool (+ in-memory cache)
  tool_connect.go      ← connect tool (daemon ping + server resolution + up)
  tool_disconnect.go   ← disconnect tool
  tool_status.go       ← status tool (+ server name resolution)
```

### Dependencies

```
github.com/modelcontextprotocol/go-sdk v1.4.1+   ← official MCP SDK (stdio transport, generics-based tool schema inference)
```

SDK API pattern used:
```go
server := mcp.NewServer(&mcp.Implementation{Name: "k2-mcp", Version: "v1.0.0"}, nil)

type ConnectInput struct {
    ServerID int `json:"server_id" jsonschema:"server ID from list_servers"`
}

mcp.AddTool(server, &mcp.Tool{
    Name:        "connect",
    Description: "Connect to a VPN server",
}, func(ctx context.Context, req *mcp.CallToolRequest, input ConnectInput) (*mcp.CallToolResult, any, error) {
    // ... implementation
})

server.Run(context.Background(), &mcp.StdioTransport{})
```

No other external dependencies. HTTP client uses `net/http` stdlib.

### Concurrency

MCP servers may receive concurrent tool calls. Shared state (session tokens, server list cache) is protected by `sync.RWMutex`. Concurrent `list_servers` calls may trigger duplicate Center API requests (acceptable — no dedup needed for v1).

## Build & Distribution

### Build

```makefile
# In k2app/Makefile
build-mcp:
	cd mcp && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../desktop/src-tauri/binaries/k2-mcp-$(TARGET_TRIPLE)
```

Cross-compile for all desktop targets:
- `k2-mcp-x86_64-apple-darwin`
- `k2-mcp-aarch64-apple-darwin`
- `k2-mcp-x86_64-pc-windows-msvc.exe`
- `k2-mcp-aarch64-pc-windows-msvc.exe`
- `k2-mcp-x86_64-unknown-linux-gnu`
- `k2-mcp-aarch64-unknown-linux-gnu`

Universal macOS binary via `lipo` (same as k2 binary).

### Tauri Sidecar

Add to `desktop/src-tauri/tauri.conf.json`:
```json
{
  "bundle": {
    "externalBin": [
      "binaries/k2",
      "binaries/k2-mcp"
    ]
  }
}
```

Tauri resolves the correct platform binary automatically.

### Standalone Use

k2-mcp also works standalone (without Tauri) for headless/server scenarios:

```json
{
  "mcpServers": {
    "k2": {
      "command": "/path/to/k2-mcp",
      "env": {
        "KAITU_API_URL": "https://api.kaitu.io"
      }
    }
  }
}
```

## Configuration

k2-mcp uses environment variables (no config file):

| Env Var | Default | Description |
|---------|---------|-------------|
| `KAITU_API_URL` | `https://api.kaitu.io` | Center API base URL |
| `K2_DAEMON_ADDR` | `127.0.0.1:1777` | k2 daemon HTTP address |
| `KAITU_SESSION_DIR` | `~/.kaitu` | Directory for session/UDID files |

## Security Considerations

1. **Tokens on disk**: `mcp-session.json` contains JWT tokens. File permissions set to `0600` on creation.
2. **Password in transit**: Sent over HTTPS to Center API. Never logged, never stored.
3. **Daemon API**: Unauthenticated, localhost-only. k2-mcp trusts this is safe (same-machine assumption).
4. **UDID stability**: Generated once from hardware identifiers on first run, then stored to file. The file is the source of truth — hardware identifiers are never re-sampled. Treated as a device fingerprint by Center API.

## Typical AI Agent Workflow

```
User: "Connect me to VPN"

AI calls: login(email="user@example.com", password="***")
  → {email: "user@example.com"}

AI calls: list_servers()
  → [{id: 1, name: "Tokyo", traffic_usage_percent: 30}, {id: 2, name: "US East", traffic_usage_percent: 60}]

AI calls: connect(server_id=1)
  → {state: "connecting", server: "Tokyo"}

AI calls: status()
  → {state: "connected", server: "Tokyo", uptime_seconds: 5}

AI: "Connected to Tokyo server. Everything looks good."
```

```
User: "I need to renew my subscription"

AI calls: account_info()
  → {email: "user@example.com", plan_expires_at: "2026-03-30", is_active: true}

AI calls: list_plans()
  → [{id: "pro_month", name: "Pro Monthly", price_cents: 999}, ...]

AI: "Your plan expires March 30. Here are the options: ..."

User: "Go with yearly"

AI calls: subscribe(plan_id="pro_year")
  → {payment_url: "https://payment.wordgate.com/order/456", amount_cents: 9999}

AI: "Here's your payment link: https://payment.wordgate.com/order/456 ($99.99/year)"
```

## Out of Scope

- **Mobile**: MCP is desktop-only (AI agents run on desktop). Mobile users use the app UI.
- **Multi-account**: One session at a time. Login overwrites previous session.
- **Payment completion detection**: k2-mcp does not poll for payment status. User can call `account_info` after paying to verify.
- **Server auto-selection**: AI chooses based on `list_servers` output (load, region). No built-in "best server" logic.
- **k2 daemon management**: k2-mcp does not start/stop the daemon process. It assumes the daemon is running (or returns actionable error).
