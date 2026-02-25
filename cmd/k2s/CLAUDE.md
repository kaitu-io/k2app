# cmd/k2s — Server CLI

Server binary for VPS deployment.

## Subcommands

```
k2s                      Zero-config mode (auto-generates certs, ECH, auth, prints connect URL)
k2s -c <yaml>            Server with explicit config
k2s -t                   Test config and exit
k2s -s stop|reload       Send signal to running server
k2s setup                Install service + wait for connect URL + print it
k2s service install      Install as system service (root required)
k2s service uninstall    Remove system service
k2s demo-config          Print example server YAML to stdout
k2s version              Version info
```

## Files

- `main.go` — entry point, subcommand dispatch, top-level flags (`-c`, `-t`, `-s`, `-v`)
- `setup.go` — `k2s setup`: install service if needed, wait for connect-url.txt, print URL
- `service.go` — `k2s service`: install/uninstall system service
- `service_darwin.go` / `service_linux.go` / `service_windows.go` — Platform-specific service management
- `process.go` — PID file helpers
- `signal.go` / `signal_windows.go` — Signal dispatch (`-s stop|reload`)

## Architecture

Uses stdlib `flag.NewFlagSet` for CLI parsing. Delegates to `server/` package for actual server logic.

## Zero-Config Mode

```bash
k2s
```

No flags, no config file needed. Uses `config.DefaultServerConfig()`:
1. `cert_dir` defaults to `~/.k2s/` (Linux/macOS) or `%APPDATA%\k2s\` (Windows)
2. `setupAutoProvision` runs: certs → ECH → padding → auth → URL
3. Logs ready-to-use `k2v5://` connect URL to stderr

## Explicit Config Mode

```bash
k2s -c server.yaml
```

Loads `ServerConfig` from YAML. If `cert_dir` is set, runs auto-provisioning for missing assets.

## Service Mode

```bash
k2s service install      # Install
k2s setup                # Install + print connect URL (recommended)
```

Installs as system service (systemd/launchd), runs as daemon.
