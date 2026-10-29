# Config Layer (config/)

Client + server config types, YAML/JSON loading, defaults, validation, plus the k2subs `Subscription`, logging and PID helpers. Uses `gopkg.in/yaml.v3`.

## Files

- `config.go` — `ClientConfig` / `ServerConfig` and sub-types, `RouteConfig` + Via scheme constants, `LoadClient` / `LoadServer`, `ClientFromURL` / `ClientFromSubsURL`, `SetDefaults`, `DefaultServerConfig`, `DefaultClientDir` (`/etc/k2`, Windows `%ProgramData%\k2`)
- `validate.go` — `ValidateClient` (server URL, listen, mode, log level, reserved Via schemes), `ValidateServer`, `ValidateRouteCIDRConflicts`
- `subscription.go` — `Subscription` (k2subs:// parse, HTTP fetch, disk cache, weighted `Pick`, background refresh, credential rotation, control-key hash, `SlotBinding` manifest), `Tunnel`, `ScoreSource`
- `log.go` (+ `log_unix.go` / `log_windows.go` / `log_stderr_*.go`) — `SetupLogging` (slog + lumberjack, stderr redirected into the log file), `SetLogLevel` (runtime), `SetupCrashOutput`, `DefaultLogPath` (root → `/var/log/kaitu/` / `%ProgramData%`; non-root → user dir). Build-time default level via ldflags (`K2_BUILD_LOG_LEVEL`)
- `pid.go` (+ `pid_unix.go` / `pid_windows.go`) — `WritePID` / `ReadPID` / `IsProcessAlive`
- `search.go` — `SearchClientConfig` (cwd → user config dir → system dir) / `SearchServerConfig` (cwd → system dir)
- `anchor.go` — `RouterAnchorIP` (`10.17.79.1`) / `RouterAnchorPort` (1779): cross-repo contract with the webapp's ROUTER_ANCHOR, `gateway/anchor.go` DNAT and engine's anchor-direct route — change all together
- `defaults_mobile.go` — `ios || android`: overrides `DefaultTunIPv4/IPv6` to `10.0.0.2/24` / `fd00::2/64` (desktop: `198.18.0.7/15` / `fdfe:dcba:9876::7/64`)

## Client (client.yaml / JSON from the webapp)

There is **no top-level `server:`** — outbounds live only in `routes: [{via, match}]`. `ClientConfig.Server` is derived (`yaml:"-"`): the first `k2v5://` route, filled by `SetDefaults`.

```yaml
routes:
  - via: "k2v5://udid:token@host:443?ech=...&pin=sha256:..."   # or k2subs:// / k2p:// / direct / reject
    match: { all: true }        # rule.MatchConfig: preset/names/exclude, region, domain_suffix, ip_cidr, apps, app_paths, …
mode: tun                       # "tun" (default, needs root) or "proxy"
tun:   { ipv4: "198.18.0.7/15", ipv6: "fdfe:dcba:9876::7/64" }   # platform defaults
proxy: { listen: "127.0.0.1:1080" }
dns:
  direct: ["114.114.114.114:53", ...]   # default: 6 CN resolvers
  proxy:  ["8.8.8.8:53", ...]           # default: 6 global resolvers
listen: "127.0.0.1:1777"        # daemon HTTP API
pid_file: ""
block_quic: false               # true = ICMP-reject browser QUIC
cache_dir: ""                   # rule bundle cache ("" = platform default; set by the shell)
rule_sources: []                # override default rule download sources (rule.DefaultSources)
telemetry:
  rule_miss: { enabled: false, endpoint: "", country: "", client_version: "" }   # MUST stay off by default
client_version: ""              # app version, forwarded to the node via the metadata stream ("" = legacy/unknown)
log: { level: info, output: "" }   # output "" = DefaultLogPath (NOT stderr — service supervisors discard stderr)
```

Via values (`RouteConfig.Via`): `""`/`direct`, `reject`, `k2v5://`, `k2p://`, `k2subs://`. `device://` and `auto://` are **reserved** — `ValidateClient` rejects them and `buildOutboundMap` treats them as direct + WARN if validation is bypassed. Helpers: `ViaScheme`, `IsReservedVia`, `IsOutboundURL`, `IsAllProxy`.

## Server (server.yaml)

All fields optional. `k2s run` with no config uses `DefaultServerConfig()`.

```yaml
listen: ":443"
host: ""                        # external hostname/IP for connect URL (auto-detected if empty)
cert_dir: "/etc/k2s"            # default when tls.cert unset (Windows %ProgramData%\k2s): dual keypair (RSA+EC), ECH keys, auth
tls: { cert: /path/cert.pem, key: /path/key.pem }   # explicit certs (override cert_dir auto-generation)
ech: { provider: local }        # "local" = forge from Cloudflare DNS template; key_path / public_name are legacy
local_routes: {}                # SNI → target for non-ECH TCP relay
control_plane_routes:           # inner SNI → Center origin; omit = built-in default (k2.52j.me → Center)
  k2.52j.me: "k2.52j.me:443"
pcc: gfw
rate: 100000000
auth:
  users_file: ""                # udid:token per line
  remote_url: ""                # Center auth URL
  cache_ttl: 15m                # 0 → 15m (not "no cache")
  enforce_auth: false           # reject unauthenticated streams on both transports; env K2_ENFORCE_AUTH
usage_api_listen: "127.0.0.1:9099"   # "" disables; env K2_USAGE_API_LISTEN wins
usage_report_url: ""            # env K2_USAGE_REPORT_URL
node_cutoff_report: false       # LEGACY in-process usage reporter; env K2_NODE_CUTOFF_REPORT — only against a Center that serves verdicts
log: { level: info, output: stderr }
pid_file: ""
```

Env-only (`yaml:"-"`, never serialized/logged): `K2_NODE_SECRET` (NodeSecret), `K2_NODE_IPV4` (NodeIPv4; "" → runtime public-IP probe), `K2_SLACK_WEBHOOK_URL` (auth-fault alerting; unset = off), `K2_NODE_NAME`.

## Subscription locking

`Subscription.mu` is a standalone state lock — never nested with engine/daemon locks. Every callback (`onCredsRotate`, `onControlKeyHash`, `onSlotBindings`) is invoked OUTSIDE `mu`. `subscription.Manager` (package `subscription/`) takes its own `mu` only to snapshot sessions, then calls `Pick` (→ `Subscription.mu`) with it released.

## Testing

```bash
go test ./config/...
```
