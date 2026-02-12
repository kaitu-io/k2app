# cmd/k2s — Server CLI

Server binary for VPS deployment.

## Subcommands

```
k2s run                  Zero-config mode (auto-generates certs, ECH, auth, prints connect URL)
k2s run -s <yaml>        Server with explicit config
k2s run --install        Install as system service
k2s status               Show server status
k2s version              Version info
```

## Files

- `main.go` — entry point, subcommand dispatch
- `cmd_run.go` — run subcommand (zero-config default, explicit config, daemon, install)
- `cmd_version.go` — version display

## Architecture

Uses stdlib `flag.NewFlagSet` for CLI parsing. Delegates to `server/` package for actual server logic.

## Zero-Config Mode

```bash
k2s run
```

No flags, no config file needed. Uses `config.DefaultServerConfig()`:
1. `cert_dir` defaults to `~/.k2s/` (Linux/macOS) or `%APPDATA%\k2s\` (Windows)
2. `setupAutoProvision` runs: certs → ECH → padding → auth → URL
3. Logs ready-to-use `k2v5://` connect URL to stderr

## Explicit Config Mode

```bash
k2s run -s server.yaml
```

Loads `ServerConfig` from YAML. If `cert_dir` is set, runs auto-provisioning for missing assets.

## Service Mode

```bash
k2s run --install
sudo systemctl start k2s   # Linux
sudo launchctl start k2s   # macOS
```

Installs as system service (systemd/launchd), runs as daemon.
