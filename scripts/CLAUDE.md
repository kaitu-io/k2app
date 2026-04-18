# scripts — Build, Deploy, and Test Helpers

Shell helpers orchestrated by the root `Makefile`. Used by local development, CI, and release automation.

## Windows k2 Test Workflow

Test the k2 Go tunnel against the HK k2v5 test server from Windows. Configs live at repo root (`k2-test-config.yml`, `k2-test-proxy-config.yml`); scripts here.

**1. Build k2 binary** (from Git Bash, no admin):

```bash
cd k2 && GOOS=windows GOARCH=amd64 go build -tags nowebapp \
  -o ../desktop/src-tauri/binaries/k2-x86_64-pc-windows-msvc.exe ./cmd/k2
```

**2. Start daemon** (requires admin — TUN mode creates a virtual NIC):

```powershell
.\scripts\start-k2-admin.ps1     # PowerShell; auto-elevates via UAC
```

Starts the daemon in the foreground using `k2-test-config.yml` (TUN mode, global routing, debug logs to `C:\Users\david\k2-debug.log`). Press Ctrl+C to stop.

**3. Control from Git Bash** (no admin needed; daemon must be running):

```bash
./scripts/test-k2-ctl.sh up       # Connect (sends UP to daemon API)
./scripts/test-k2-ctl.sh status   # Connection status JSON
./scripts/test-k2-ctl.sh down     # Disconnect
./scripts/test-k2-ctl.sh logs     # Tail debug log
./scripts/test-k2-ctl.sh test     # Connectivity tests (IP, Google, YouTube, speed)
./scripts/test-k2-ctl.sh debug    # Set log level = debug
./scripts/test-k2-ctl.sh info     # Set log level = info
```

**4. Daemon API** (port 1778 for test, 1777 for the installed app):

```bash
curl -s http://127.0.0.1:1778/ping
curl -s -X POST http://127.0.0.1:1778/api/core -d '{"action":"status"}'
```

**Config files:**

- `k2-test-config.yml` — TUN mode (admin required, full VPN, exercises `HandleUDP`/QUIC)
- `k2-test-proxy-config.yml` — Proxy mode (no admin, SOCKS5 on `:1080`, TCP only)

## Related Docs

- [Root Architecture](../CLAUDE.md)
- [k2 Core](../k2/CLAUDE.md)
- [Desktop Shell](../desktop/CLAUDE.md)
