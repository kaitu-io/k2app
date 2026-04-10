# CLI Layer (cmd/k2/)

Entry point: `main.go` — `os.Args` dispatch, no framework.

## Subcommands

```
k2 up [URL|config.yaml]         Connect via background daemon
k2 down                         Disconnect
k2 status                       Connection status

k2 setup <URL>                  First-time: generate config + install service + start
k2 run                          Start daemon (service entry point)

k2 config test [-c file]        Test configuration
k2 config show                  Print example YAML

k2 service install              Install as system service
k2 service uninstall            Remove system service
k2 upgrade [--check]            Self-update
  healthcheck               External health check (internal, used by launchd)

k2 version                      Version + commit info
k2 help [command]               Help for any command
```

## Global Flags

```
--json    Machine-readable JSON output
--quiet   Minimal output
--help    Help for any command
```

## Files

- `main.go` — Subcommand dispatch, global flags (--json, --quiet, --help)
- `output.go` — Output layer (human vs JSON switching)
- `ctl.go` — `k2 up/down/status`: IPC to daemon HTTP API
- `ctl_hint_darwin.go` / `ctl_hint_linux.go` / `ctl_hint_windows.go` — Platform "daemon not running" hints
- `setup.go` — `k2 setup`: first-time config + service install
- `service.go` — `k2 service`: install/uninstall system service
- `service_darwin.go` — macOS launchd plist + watchdog plist
- `service_linux.go` — Linux systemd unit (OpenWRT handled by cmd/k2r)
- `service_windows.go` — Windows Service (SCM)
- `process.go` — Daemon start, PID monitoring
- `upgrade.go` — `k2 upgrade`: self-update binary from CDN
- `healthcheck.go` — `k2 healthcheck`: external watchdog for launchd (ping daemon, failure counting, DNS cleanup + kill)
- `healthcheck_unix.go` / `healthcheck_windows.go` — Platform process management (isProcessAlive, killProcess)
- `healthcheck_test.go` — Unit tests for health evaluation logic

## Pattern

Each subcommand file uses `flag.NewFlagSet` for its flags. IPC commands use `daemon.NewClient("127.0.0.1:1777")` to talk to the daemon's HTTP API. Output uses `out.ok()`/`out.fail()`/`out.printf()` for JSON/human switching.
