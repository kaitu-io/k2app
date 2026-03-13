# CLI Layer (cmd/k2/)

Entry point: `main.go` — `os.Args` dispatch, no framework.

## Subcommands

```
k2                                   Daemon mode (service entry point, no subcommand)
k2 -c <config.yaml>                 Daemon with explicit config
k2 -t                               Test config and exit
k2 -s stop|reload                   Send signal to running daemon
k2 ctl up [URL|config.yaml] [--pid] Connect tunnel (IPC to daemon)
k2 ctl up                           Reconnect with saved config
k2 ctl down                         Disconnect
k2 ctl status                       Connection status JSON
k2 setup [URL]                      Generate config + install service + start
k2 setup -c <config.yaml>           Copy config + install service + start
k2 service install                  Install as system service (launchd/systemd/Windows)
k2 service uninstall                Remove system service
k2 upgrade [--check]                Self-update from releases.kaitu.io
k2 demo-config                      Print example client YAML to stdout
k2 android-install [--url URL]      Interactive Android APK install via USB (gadb)
k2 version                          Version + commit info
```

## Files

- `main.go` — Subcommand dispatch (`switch cmd`), top-level flags (`-c`, `-t`, `-s`, `-v`)
- `ctl.go` — `k2 ctl`: IPC client (up/down/status via daemon HTTP API)
- `ctl_hint_darwin.go` / `ctl_hint_linux.go` / `ctl_hint_windows.go` — Platform-specific "daemon not running" hints
- `setup.go` — `k2 setup`: first-time config generation + service install
- `service.go` — `k2 service`: install/uninstall system service
- `upgrade.go` — `k2 upgrade`: self-update binary from CDN
- `process.go` — PID monitoring helpers
- `signal.go` / `signal_windows.go` — Signal dispatch (`-s stop|reload`)
- `android_install.go` — `k2 android-install`: interactive CLI for USB APK install (adb detect → download → push → pm install)

## Pattern

Each subcommand file uses `flag.NewFlagSet` for its flags. IPC commands use `daemon.NewClient("127.0.0.1:1777")` to talk to the daemon's HTTP API.
