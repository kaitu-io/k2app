# cmd/k2s — Server CLI

Server binary for VPS deployment.

## Subcommands

```
k2s run              Start server daemon (called by service manager)
k2s run -s <yaml>    Server foreground mode
k2s run --install     Install as system service
k2s status           Show server status
k2s version          Version info
```

## Files

- `main.go` — entry point, subcommand dispatch
- `cmd_run.go` — run subcommand (daemon start, foreground, install)
- `cmd_version.go` — version display

## Architecture

Uses stdlib `flag.NewFlagSet` for CLI parsing. Delegates to `server/` package for actual server logic.

## Foreground Mode

```bash
k2s run -s server.yaml
```

Starts server in current terminal:
1. Parse ServerConfig from YAML
2. Create server.Server with TLS config
3. Start TLS listener, ECH routing, auth handler
4. Accept QUIC/TCP-WS connections
5. Handle TCP/UDP proxy requests

## Service Mode

```bash
k2s run --install
sudo systemctl start k2s   # Linux
sudo launchctl start k2s   # macOS
```

Installs as system service (systemd/launchd), runs as daemon.
