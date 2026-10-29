# cmd/k2s — Server CLI

Server binary for VPS deployment.

## Subcommands

```
k2s run [--foreground] [-c yaml]    Start server (auto-config if no config)
k2s setup                           Interactive setup wizard
k2s setup --port N --username U --no-firewall  Fully automated setup
k2s user add <username>             Add user (prints connect URL)
k2s user list                       List users
k2s user del <username>             Delete user
k2s user reset <username>           Regenerate token

k2s config test [-c file]           Test configuration
k2s config show                     Print example YAML

k2s service install                 Install as system service
k2s service uninstall               Remove system service
k2s upgrade [--check]               Check/install updates

k2s version                         Version info
k2s help                            Usage info
```

## Global Flags

```
--json    Machine-readable JSON output
--quiet   Minimal output
--help    Help for any command
```

## Files

- `main.go` — Subcommand dispatch, global flags, `cmdRun`, `cmdConfig`
- `output.go` — Output layer (human vs JSON switching)
- `setup.go` — `k2s setup`: wizard + non-interactive mode
- `wizard.go` — Interactive prompts (port, username)
- `user.go` — `k2s user`: add/list/del/reset with --json support
- `upgrade.go` — `k2s upgrade`: fetches the JSON manifest (`/k2s-cloudfront.latest.json`, beta: `/beta/k2s-cloudfront.latest.json`) from the `releaseEndpoints` CDNs → `version` + per-platform `binaries` URL + `sha256:` `checksums`, verifies, replaces the binary. Channel persisted in `<cert_dir>/update-channel`. Published by `.github/workflows/release-k2s.yml` on a `v*-k2s` tag
- `service.go` — `k2s service`: install/uninstall
- `service_darwin.go` / `service_linux.go` / `service_windows.go` — Platform service management
- `process.go` — PID helpers, `IsRoot()`

## Pattern

Uses `flag.NewFlagSet` per subcommand. Delegates to `server/` package for actual server logic. Output uses `out.ok()`/`out.fail()` for JSON/human switching.

## Zero-Config Mode

`k2s run` with no config file: uses `config.DefaultServerConfig()`, auto-provisions certs/ECH/auth, logs connect URL.

## Non-Interactive Setup

`k2s setup --port 443 --username alice --no-firewall --json` runs fully automated, returns JSON result.
