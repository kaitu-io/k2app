# k2 MCP Server — Go

Go MCP server for Claude Code, providing user-facing VPN tools via stdio transport.

## Commands

```bash
cd mcp && go test ./...          # Run all tests
cd mcp && go build -o k2-mcp .   # Build binary
```

## Tools

| Tool | Auth | Description |
|------|------|-------------|
| `send_code` | None | Send verification code to email |
| `login` | None | Log in with email + verification code |
| `account_info` | Auth | Current account info (email, membership, expiry) |
| `list_plans` | None | Available subscription plans (active only, USD prices) |
| `subscribe` | Auth | Create order, returns payment URL |
| `list_servers` | Auth | VPN server list (id, name, country, load) |
| `connect` | Auth + Daemon | Connect to VPN server by ID |
| `disconnect` | Daemon | Disconnect VPN |
| `status` | Daemon | Current VPN connection status |

## Architecture

```
mcp/
├── main.go                  # App struct, tool registration, env config
├── center_client.go         # HTTP client for Center API (envelope unwrap, 401 auto-refresh)
├── daemon_client.go         # HTTP client for k2 daemon (localhost:1777)
├── session.go               # Token persistence (~/.kaitu/mcp-session.json) + UDID generation
├── tool_login.go            # send_code + login + errorResult/successResult helpers
├── tool_account.go          # account_info
├── tool_plans.go            # list_plans (filters inactive, formats USD)
├── tool_subscribe.go        # subscribe (creates order via POST /api/user/orders)
├── tool_servers.go          # list_servers (caches server list, maps to clean output)
├── tool_connect.go          # connect (builds auth URL, calls daemon up)
├── tool_disconnect.go       # disconnect (calls daemon down)
├── tool_status.go           # status (daemon status + server name resolution)
└── *_test.go                # Tests for each tool + clients + session
```

## Key Patterns

- **Center API envelope**: All Center endpoints return `{code, message, data}`. `CenterClient.do()` unwraps and returns `CenterError` for non-zero codes.
- **Daemon API envelope**: Daemon also uses `{code, message, data}`. `DaemonClient.Status()` unwraps via `daemonEnvelope`.
- **401 auto-refresh**: `CenterClient.Get/Post` catch 401, call `tryRefresh()` with the refresh token, retry once.
- **Auth URL injection**: `connect` tool builds `k2v5://udid:token@host:port?...` from plain server URL + session credentials. Same pattern as webapp's `authService.buildTunnelUrl()`.
- **Session persistence**: `~/.kaitu/mcp-session.json` (0600 perms). Restored on startup. UDID stored separately in `~/.kaitu/mcp-udid`.
- **Tool output convention**: All tools return JSON via `successResult(v)` or `errorResult(msg)`. Prices formatted as USD strings (`$9.99`). Only active/relevant data exposed — no raw API pass-through.
- **Server cache**: `App.servers` cached with `serversMu` RWMutex. Used by `status` tool to resolve server name from URL.

## Environment Variables

| Var | Default | Purpose |
|-----|---------|---------|
| `KAITU_API_URL` | `https://api.kaitu.io` | Center API base URL |
| `K2_DAEMON_ADDR` | `127.0.0.1:1777` | k2 daemon address |
| `KAITU_SESSION_DIR` | `~/.kaitu` | Session file directory |

## Test Patterns

- `newTestApp(t, serverURL)` creates an `App` with a test Center client pointing at httptest server
- `textContent(t, result)` extracts text from `mcp.CallToolResult`
- Tests use `httptest.NewServer` to mock Center API with correct envelope format
- Daemon tests use a separate httptest server assigned to `app.daemon`

## Gotchas

- **Center API always HTTP 200**: Error state is in JSON `code` field, not HTTP status. `CenterClient` returns `*CenterError` for non-zero codes.
- **Daemon envelope**: Daemon wraps responses in `{code, message, data}` — must unwrap `data` before parsing `DaemonStatus`.
- **Auth URL format**: Server URLs from tunnel API have no credentials. MCP must inject `udid:token@` before passing to daemon `up`.
- **`expiredAt` is Unix timestamp**: Center API returns `expiredAt` as `int64`, not RFC3339 string.
- **Binary must be rebuilt**: After code changes, rebuild with `go build` and reconnect MCP in Claude Code (`/mcp`).

## Related Docs

- [Root Architecture](../CLAUDE.md)
- [Center API](../api/CLAUDE.md) — Backend endpoints consumed by CenterClient
- [Daemon API](../k2/docs/contracts/webapp-daemon-api.md) — POST /api/core actions
