# CLI Layer (cmd/k2/)

Entry point: `main.go` — `os.Args` dispatch, no framework.

## Subcommands

```
k2 up [URL or config.yaml] [--pid PID]   Connect (auto-manages daemon)
k2 up                                    Reconnect with saved config
k2 down                                  Disconnect
k2 status                                Show connection status
k2 run                                   Daemon mode (service entry point)
k2 run -c <config.yaml>                  Client foreground (needs sudo)
k2 run -s <server.yaml>                  Server foreground (VPS)
k2 version                               Version info
```

## Files

- `main.go` — Subcommand dispatch (`switch cmd`), `printUsage()`
- `cmd_up.go` — `k2 up`: IPC client + auto-install daemon via ServiceManager
- `cmd_down.go` — `k2 down`: IPC client
- `cmd_status.go` — `k2 status`: IPC client
- `cmd_run.go` — `k2 run`: daemon / `-c` client / `-s` server foreground
- `cmd_version.go` — `k2 version`: prints version + commit

## Pattern

Each `cmd_*.go` uses `flag.NewFlagSet` for its flags. IPC commands use `daemon.NewClient("127.0.0.1:1777")` to talk to the daemon's HTTP API.
