# User Experience Design: One-Command Install & Usage

## Goal

Non-technical user can deploy k2s server + connect k2 client in under 2 minutes, with zero configuration files, zero decisions, zero technical concepts.

## User Journey

### Server (VPS, 1 minute)

```bash
curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2s
k2s run
# → prints connect URL, exits. Service runs in background.
```

### Client (laptop, 30 seconds)

```bash
curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2
k2 up k2v5://...   # paste URL from server
```

### Daily use

```bash
k2 up       # reconnect (URL saved)
k2 down     # disconnect
k2 status   # check connection
```

## Design Decisions

### 1. Config directory: `/etc/k2s/` and `/etc/k2/`

System services use `/etc/`, not `~/`. Both k2 and k2s run as root (LaunchDaemon/systemd).

| Platform | Server | Client |
|----------|--------|--------|
| Linux    | `/etc/k2s/` | `/etc/k2/` |
| macOS    | `/etc/k2s/` | `/etc/k2/` |
| Windows  | `%ProgramData%\k2s\` | `%ProgramData%\k2\` |

### 2. `k2s run` smart mode

`k2s run` (no flags) does everything:

1. If service not installed → install systemd/launchd service
2. If service not running → start service
3. Wait for `/etc/k2s/connect-url.txt` to appear
4. Print connect URL
5. Exit (service continues in background)

Repeated `k2s run` → just prints the URL.

`k2s run --foreground` → current foreground behavior (dev/debug).

### 3. Connect URL persistence

Server writes connect URL to `/etc/k2s/connect-url.txt` on startup. This file is read by `k2s run` to display the URL without parsing logs.

### 4. Install script

Single script installs k2 or k2s (one at a time) to `/usr/local/bin/`. Pass `k2` or `k2s` as argument. Auto-detects OS (linux/darwin) and arch (amd64/arm64). Downloads from GitHub Releases with CDN fallback.

### 5. Demo configs update

Fix stale `client.demo.yml` (remove `sni=www.microsoft.com`). Update `server.demo.yml` for zero-config era.

## Code Changes Required

1. `config/config.go` — `defaultCertDir()` → `/etc/k2s/`
2. `server/server.go` — save connect URL to file
3. `cmd/k2s/cmd_run.go` — smart mode: detect/install/start service + print URL
4. `daemon/` — add k2s service management (systemd/launchd for server)
5. `cmd/k2/client.demo.yml` — fix stale URL
6. `cmd/k2s/server.demo.yml` — update format
7. `docs/guide.md` — complete rewrite
8. `docs/install.sh` — one-liner install script
