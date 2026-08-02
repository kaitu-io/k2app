# scripts — Build, Deploy, and Test Helpers

Shell helpers orchestrated by the root `Makefile`. Used by local development, CI, and release automation.

## What lives here

Most scripts are invoked through a `make` target, not directly — check the root
`Makefile` for the canonical invocation before running one by hand.

| Group | Scripts |
|-------|---------|
| Dev servers | `dev-standalone.sh`, `dev-macos.sh`, `dev-windows.sh`, `dev-openwrt.sh` |
| Build | `build-k2.sh`, `build-k2-standalone.sh`, `build-macos.sh`, `build-mobile-{ios,android}.sh`, `build-openwrt.sh` |
| Publish / deploy | `publish-desktop.sh`, `publish-mobile.sh`, `publish-k2.sh`, `publish-docker.sh`, `deploy-center.sh` |
| Brand purity gates | `check-desktop-brand-purity.sh`, `check-mobile-brand-purity.sh`, `apply-ios-brand.sh` |
| Verification | `test_build.sh` (full build check — the count is computed at runtime, not fixed), `check-embed-size.sh` |
| iOS device | `detect-ios-device.sh`, `deploy-ios-device.sh`, `ios-logs.sh` |
| Misc | `sync-version.sh`, `sync-adb-tools.sh`, `k2-quick-diag.sh`, `antiblock-cursor.sh` |
| CI (`ci/`) | `notify-slack.sh`, `upload-release.sh`, `push-secrets.sh`, `setup-secrets.sh`, `generate-k2s-manifest.js`, `windows-sign-preflight.sh`, plus `macos/` + `windows/` subdirs |

Node-side ops scripts are **not** here — they live in `docker/scripts/`
(`provision-node.sh`, `auto-update.sh`, …). See root `CLAUDE.md`.

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

**Config files** (repo root):

- `k2-test-config.yml` — TUN mode (admin required, full VPN, exercises `HandleUDP`/QUIC).
  **Gitignored — it is not in a fresh checkout, and `start-k2-admin.ps1` hard-requires it
  (line 8), so step 2 fails until you create it.** Copy `k2-test-proxy-config.yml` and change
  `mode: proxy` → `mode: tun`, drop the `proxy:` block; keep `listen: "127.0.0.1:1778"`. It holds
  a live `k2v5://` credential, which is why it stays out of git — get one from a real tunnel
  rather than reusing a stale committed sample.
- `k2-test-proxy-config.yml` — Proxy mode (no admin, SOCKS5 on `:1080`, TCP only). Committed.

**`start-k2-admin.ps1` hardcodes the log path** `C:\Users\david\k2-debug.log` — edit it for
your machine, or `test-k2-ctl.sh logs` tails a file the daemon never wrote.

## Related Docs

- [Root Architecture](../CLAUDE.md)
- [k2 Core](../k2/CLAUDE.md)
- [Desktop Shell](../desktop/CLAUDE.md)
