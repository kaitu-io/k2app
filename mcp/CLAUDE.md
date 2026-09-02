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
| `account_info` | Auth | Email, plan expiry, `is_active`, `device_count`; `device_limit` is hardcoded `5` (`tool_account.go`) |
| `list_plans` | None | Active plans, USD prices — reads the legacy frozen `/api/plans`; successors are `/api/products/:product/plans` and `/api/tiers` (`api/route.go`). Brand-correctness of the legacy list: unverified |
| `subscribe` | Auth | Create order, returns payment URL |
| `list_servers` | Auth | VPN server list (id, name, country, load) |
| `connect` | Auth + Daemon | Connect to VPN server by ID |
| `disconnect` | Daemon | Disconnect VPN |
| `status` | Daemon | Current VPN connection status |

## Architecture

```
mcp/
├── main.go                  # App struct, tool registration, env config
├── center_client.go         # Center API client (envelope unwrap, envelope-401 auto-refresh)
├── daemon_client.go         # k2 daemon client — POST /api/core up/down/status
├── session.go               # ~/.kaitu/mcp-session.json + mcp-udid + Tauri session sharing
├── storage_crypto.go        # AES-256-GCM decrypt + HKDF key derivation (test vectors shared with Rust)
├── hwid_{darwin,linux,windows}.go  # getHardwareID — must match Rust machine-uid 0.5.4 byte-for-byte
├── tauri_storage.go         # Read + decrypt Tauri desktop storage.json → tokens + UDID
├── tool_login.go            # send_code + login + errorResult/successResult helpers
├── tool_account.go          # account_info + handleCenterError (401→"not logged in", 402→"subscription expired")
├── tool_plans.go            # list_plans (drops inactive, cents→"$9.99")
├── tool_subscribe.go        # subscribe (POST /api/user/orders)
├── tool_servers.go          # list_servers (GET /api/tunnels/k2v5, 5-min cache)
├── tool_connect.go / tool_disconnect.go / tool_status.go
└── *_test.go                # helpers newTestApp / textContent live in tool_login_test.go
```

## Key Patterns

- **Center API envelope**: All Center endpoints return `{code, message, data}`. `CenterClient.do()` unwraps and returns `CenterError` for non-zero codes.
- **Daemon contract** (`daemon_client.go`): `up` posts `{"action":"up","params":{"config":{"mode":"tun","routes":[{"via":url}]}}}` — there is no top-level `server` field; `status` reads `startAt` (unix seconds) / `uptimeSeconds` / `config.routes[].via` out of the `{code, message, data}` envelope. Both shapes were wrong until 2026-08-15 and failed silently (zero-value fields, empty server name); the field comments in `daemon_client.go` are the record.
- **Envelope-401 auto-refresh**: `CenterClient.Get/Post` treat envelope `code == 401` (not HTTP 401) as expired, call `tryRefresh()` (`POST /api/auth/refresh`), retry once. **Known issue, fix in progress on a separate branch**: Center's refresh overwrites `device.TokenIssueAt` (`api/api_auth.go`) and `validateToken` rejects any token whose claim differs (`api/logic_auth.go`) — so in Tauri-shared mode an MCP refresh kills the desktop's access+refresh tokens; the new pair lands in `mcp-session.json`, but `RestoreFromTauri` wins on the next start and reloads the dead desktop tokens.
- **Auth URL injection**: `connect` builds `k2v5://udid:token@host:port?...` from the plain tunnel URL (the tunnel API returns no credentials). Same pattern as webapp's `authService.buildTunnelUrl()`. **Known bug, fix in progress**: it uses `app.session.UDID()` (MCP's own 16-char id) even when the token came from Tauri, whose device is the 32-char hash — node auth rejects this as "UDID mismatch" (`api/slave_api_device_auth.go`). `tool_connect_test.go` only asserts the `:tok@host` suffix, so it does not catch it.
- **Session persistence**: `~/.kaitu/mcp-session.json` (0600 perms). Restored on startup. UDID in `~/.kaitu/mcp-udid` (16 hex chars, sha256(hostname+MAC)).
- **Tauri session sharing**: On startup MCP reads and decrypts Tauri's `storage.json` (`~/Library/Application Support/io.kaitu.desktop/`, `%APPDATA%\io.kaitu.desktop\`, `~/.local/share/io.kaitu.desktop/`). If it holds tokens they win over `mcp-session.json`, and the `X-UDID` header carries the desktop's 32-char hashed UDID (`hashUDID`, same as webapp). MCP never writes to Tauri's file. Consequences: a `login` done through MCP is shadowed on the next start while Tauri has tokens, and its body `udid` is the 16-char id, so it registers a second device. Tauri values are JSON double-encoded (`"\"hello\""`) — `readTauriStorage` strips the outer quotes. Windows `getHardwareID` must NOT pass `WOW64_64KEY`: on Server 2025 it returned a different `MachineGuid` than Rust `machine-uid`, breaking key derivation (`hwid_windows.go`).
- **Tool output convention**: All tools return JSON via `successResult(v)` or `errorResult(msg)`. Prices formatted as USD strings (`$9.99`). Only active/relevant data exposed — no raw API pass-through.
- **Silent successes**: `status` returns `{"state":"disconnected"}` whenever the daemon call errors; `disconnect` returns success even if `down` fails; `Up` gets `"connecting"` back for a bad config because the daemon's `doUp` is async — the 570 "no k2v5 outbound" only shows up later via `status`.
- **Server cache**: `App.servers` under `serversMu`, TTL 5 min (`serverCacheTTL`). `status` resolves the server name from the `routes[].via` hostname.

## Environment Variables

| Var | Default | Purpose |
|-----|---------|---------|
| `KAITU_API_URL` | `https://api.kaitu.io` | Center API base URL |
| `K2_DAEMON_ADDR` | `127.0.0.1:1777` | k2 daemon address |
| `KAITU_SESSION_DIR` | `~/.kaitu` | Session file directory |

## Gotchas

- **Center API always HTTP 200**: Error state is in JSON `code` field, not HTTP status. `CenterClient` returns `*CenterError` for non-zero codes.
- **No `X-K2-Brand` header**: `CenterClient` sends only `Authorization` + `X-UDID`, so Center resolves brand Host→header→`kaitu` (`api/brand.go`); overleap accounts get 403003 from `AuthRequired` (`api/middleware.go`). Root CLAUDE.md says clients must always send it.
- **`expiredAt` is Unix timestamp**: Center API returns `expiredAt` as `int64`, not RFC3339 string.
- **Binary must be rebuilt**: After code changes, rebuild with `go build` and reconnect MCP in Claude Code (`/mcp`). Where the server is registered for Claude Code (no `.mcp.json` in this repo): unverified.

## Related Docs

- [Root Architecture](../CLAUDE.md)
- [Center API](../api/CLAUDE.md) — Backend endpoints consumed by CenterClient
- [Daemon API](../k2/docs/contracts/webapp-daemon-api.md) — POST /api/core actions
