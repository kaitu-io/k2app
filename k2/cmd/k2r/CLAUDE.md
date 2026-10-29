# cmd/k2r — Gateway CLI

Gateway binary for LAN routers/soft-routers (TPROXY interception). Linux only (`//go:build linux`). Headless: production builds always use `-tags nowebapp` (`k2/Makefile` `K2R_TAGS`, applied to all six `build-k2r*` targets) — no embedded webapp, control is over HTTP API + control-key auth (see `gateway/CLAUDE.md` "Control API Auth").

## Subcommands

```
k2r setup <URL>              First-time: generate config + install service + start + connect
k2r run [-c yaml]             Start gateway daemon (service entry point)

k2r up [URL|config.yaml]      Connect via gateway daemon (no args: reconnect saved config)
k2r down                      Disconnect
k2r status                    Connection status
k2r reset                     Unbind router: wipe credential, state, control key

k2r service install           Install as system service (systemd or OpenWRT procd)
k2r service uninstall         Remove system service and the firewall rules
k2r upgrade [--check]         Check/install updates

k2r version                   Version info
k2r help                      Usage info
```

## Global Flags

```
--json    Machine-readable JSON output
--quiet   Minimal output
--help    Help for any command
```

## Files

- `main.go` — Subcommand dispatch, global flags, `cmdRun`/`cmdSetup`/`cmdUp`/`cmdDown`/`cmdStatus`/`cmdReset`/`cmdService`
- `output.go` — Output layer (human vs JSON switching)
- `service_linux.go` — systemd unit + OpenWRT procd init script generation, install/uninstall (`uninstallThenClearRules`: service first, then `gateway.RemoveInterception`)
- `service_linux_test.go` — Tests for the OpenWRT procd script generation
- `lifecycle_test.go` — `stopOnSignal`, uninstall ordering
- `upgrade.go` — `k2r upgrade`: CLI-side `webui.Upgrader` (`newCliUpgrader`), CDN endpoints mirror the gateway daemon's `newK2rUpgrader`

## Pattern

Uses `flag.NewFlagSet` per subcommand, same as `cmd/k2` and `cmd/k2s`. `up`/`down`/`status`/`reset` are IPC calls via `gateway.NewClient(gateway.DefaultGatewayAddr)` to the running gateway daemon's HTTP API — not direct calls into the `gateway` package. `run` builds a `gateway.Gateway` directly (`gateway.New(...).Run(...)`) since it *is* the daemon process. Output uses `out.ok()`/`out.fail()`/`out.printResult()` for JSON/human switching.

## `run` and signals

`cmdRun` calls `stopOnSignal(g.Stop, …)` before `g.Run`: SIGTERM (procd / systemd stop, reboot) and SIGINT stop the gateway gracefully, which is what removes its firewall rules — see `gateway/CLAUDE.md`. A second signal exits at once. SIGINT is not claimed when the process started with it ignored (background job of a non-interactive shell); SIGHUP is not handled at all, so `nohup k2r run &` keeps working.

## `setup`

`cmdSetup` persists `k2r.yml`, installs/starts the service, then **issues an argument-less `up` over IPC** (`connectAfterSetup`, polls the daemon for up to 15 s). This is what makes "installed and started" true: the daemon only auto-connects from a persisted `state.json` of a previous session, never from a fresh `k2r.yml`, and when the service was already running (the install script runs `service install` before `setup`) the procd/systemd start is a no-op. Verified on stock OpenWrt 25.12 (2026-09-16): without the `up`, `k2r status` stayed `disconnected` after a clean install.

## `reset`

`cmdReset` tries the daemon IPC path first (`client.Do("reset", nil)`) — loopback origin is exempt from control-key auth (see `gateway/CLAUDE.md`), so no Bearer token is needed for the local CLI, and this path needs only loopback access, not root. If the daemon isn't reachable (`err != nil`), it falls back to `gateway.WipeBindingFiles()` to wipe credential/state/control-key files directly — that path does require root, since the files live under `/etc/k2r/`.
