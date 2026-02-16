# Feature: Config-Driven Connect

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | config-driven-connect                    |
| Version   | v1                                       |
| Status    | draft                                    |
| Created   | 2026-02-16                               |
| Updated   | 2026-02-16                               |
| Depends on | mobile-webapp-bridge, mobile-vpn-ios, mobile-vpn-android |

## Version History

| Version | Date       | Summary                                                        |
|---------|------------|----------------------------------------------------------------|
| v1      | 2026-02-16 | Initial: replace wireUrl passthrough with structured ClientConfig |

## Overview

Replace the opaque `connect(wireUrl)` passthrough with structured `connect(config)` across all layers. The webapp becomes the config assembler — merging Cloud API server params with user preferences into a complete `ClientConfig` — matching the classic VPN client pattern where GUI = config editor (WireGuard, V2Ray, Clash).

**Current state**: Webapp is a dumb pipe. Cloud API returns `wireUrl` (opaque string), webapp passes it unchanged to k2. The `wireUrl` string leaks through 5 layers (API → doUp → buildEngineConfig → state persistence → auto-reconnect) as two parallel string fields (`wireURL`, `configPath`). User preferences (rule mode) are injected via side-channel hack: `setRuleMode()` writes to native storage, native `connect()` reads it back and appends `&rule=` to the URL.

**Target state**: `*config.ClientConfig` is the universal currency. Webapp assembles it, daemon accepts it, state persists it, mobile receives it. Two representations only: `ClientConfig` (API boundary) and `engine.Config` (engine boundary). All string-based wireURL/configPath passthrough eliminated.

## Context

- Go `config.ClientConfig` already defines the full config structure (server, mode, dns, rule, proxy, log) — but only has YAML tags, no JSON tags
- `config.ClientFromURL(wireURL)` and `config.LoadClient(configPath)` both produce `ClientConfig` — these become CLI-side parsers
- `daemon.engineConfigFromClientConfig()` already maps `ClientConfig` → `engine.Config` — this is the right abstraction
- `daemon.buildEngineConfig(wireURL, configPath)` is the smell: it does input parsing + config mapping in one function, when input parsing belongs at the API boundary
- `daemon.doUp(wireURL, configPath, pid)` takes two strings that are just two representations of `ClientConfig` — should take `*ClientConfig` directly
- `persistedState` saves `wireURL` + `configPath` separately — fragile (`configPath` may not exist after restart) and redundant
- `mobile.Engine.Start(url, fd, dataDir)` receives bare URL with zero config — this is why `setRuleMode` hack exists
- Product has not been released — zero backward compatibility needed

## Product Requirements

- PR1: Webapp assembles full ClientConfig from server params + user preferences
- PR2: `connect(config)` replaces `connect(wireUrl)` in VpnClient interface
- PR3: Rule mode is a config field, not a separate API — `setRuleMode()` eliminated
- PR4: User preferences (rule mode, future: DNS, log level) stored in webapp, merged into config at connect time
- PR5: Desktop daemon accepts only JSON config via API — `wire_url` and `config_path` params deleted
- PR6: Mobile K2Plugin accepts JSON config, passes to engine — no native-side preference injection
- PR7: `getConfig()` returns full structured config, not wireUrl/configPath strings
- PR8: CLI resolves input (URL or YAML file) into ClientConfig before sending to daemon

## Technical Decisions

### TD1: ClientConfig JSON Schema

The TypeScript `ClientConfig` mirrors Go's `config.ClientConfig` exactly:

```typescript
interface ClientConfig {
  server: string;        // k2v5:// wire URL (from Cloud API)
  mode?: string;         // "tun" | "proxy" — default "tun"
  proxy?: {
    listen?: string;     // default "127.0.0.1:1080"
  };
  dns?: {
    direct?: string[];   // default ["114.114.114.114:53", "223.5.5.5:53"]
    proxy?: string[];    // default ["8.8.8.8:53", "1.1.1.1:53"]
  };
  rule?: {
    global?: boolean;    // true = proxy all traffic, false = smart routing
  };
  log?: {
    level?: string;      // "debug" | "info" | "warn" | "error" — default "info"
  };
}
```

**Design principle**: One config struct, three representations:
- **Go**: `config.ClientConfig` (YAML tags for CLI, JSON tags for API)
- **TypeScript**: `ClientConfig` (webapp assembles, passes to connect)
- **YAML**: `config.yml` (CLI users edit directly)

The Go struct is the source of truth. TypeScript type is a projection. All optional fields use k2's `setClientDefaults()` — webapp only sets what the user explicitly configured.

### TD2: Cloud API Unchanged — wireUrl Becomes config.server

Cloud API continues to return `wireUrl` in the server list. Webapp maps it to `config.server`:

```typescript
// Before (passthrough)
const wireUrl = selectedServer.wireUrl;
vpnClient.connect(wireUrl);

// After (config assembly)
const config: ClientConfig = {
  server: selectedServer.wireUrl,    // wireUrl → config.server
  rule: { global: userPrefs.ruleMode === 'global' },
};
vpnClient.connect(config);
```

**Why not change Cloud API**: The server list endpoint serves multiple client versions. Since `wireUrl` maps directly to `config.server` (identical to config.yml's `server:` field), no API change is needed.

### TD3: Daemon API — Config Only

Desktop daemon's `up` action accepts only `config` (JSON ClientConfig):

```
POST /api/core
{ "action": "up", "params": { "config": { "server": "k2v5://...", "rule": { "global": false } } } }
```

No `wire_url` param. No `config_path` param. Product is unreleased — zero backward compat needed.

`handleUp` parses the `config` JSON object into `*config.ClientConfig`, then calls `doUp(cfg, pid)`:

```go
func (d *Daemon) handleUp(w http.ResponseWriter, params map[string]any) {
    configRaw, ok := params["config"]
    if !ok {
        // Fall back to saved config for reconnect
        d.mu.RLock()
        cfg := d.lastConfig
        d.mu.RUnlock()
        if cfg == nil {
            writeJSON(w, Response{Code: 510, Message: "no config"})
            return
        }
        d.doUp(cfg, pid)
        return
    }
    // Parse config JSON → *config.ClientConfig
    data, _ := json.Marshal(configRaw)
    var cfg config.ClientConfig
    json.Unmarshal(data, &cfg)
    config.SetDefaults(&cfg)
    d.doUp(&cfg, pid)
}
```

### TD4: Daemon Internals — ClientConfig Throughout

**`doUp` signature**: `doUp(wireURL, configPath string, pid int)` → `doUp(cfg *config.ClientConfig, pid int)`

**`buildEngineConfig` eliminated**: This function did two jobs — (1) parse input form into ClientConfig, (2) map ClientConfig → engine.Config. Job 1 moves to API boundary. Job 2 is `engineConfigFromClientConfig()` which already exists. `buildEngineConfig` is deleted entirely.

**Daemon fields**: `lastWireURL string` + `lastConfigPath string` → `lastConfig *config.ClientConfig`. One field, one type.

**`statusInfo()`**: No longer returns `wire_url`/`config_path` strings. Status response contains state, uptime, error only. Config available via `getConfig()`.

**`handleGetConfig`**: Returns `d.lastConfig` as JSON (full `ClientConfig`), not `wire_url`/`config_path` strings.

```go
// Before
func (d *Daemon) doUp(wireURL, configPath string, pid int) error {
    d.lastWireURL = wireURL
    d.lastConfigPath = configPath
    engineCfg, err := d.buildEngineConfig(wireURL, configPath)
    // ...
}

// After
func (d *Daemon) doUp(cfg *config.ClientConfig, pid int) error {
    d.lastConfig = cfg
    engineCfg := d.engineConfigFromClientConfig(cfg, directdial.New())
    // ...
}
```

### TD5: State Persistence — Save ClientConfig

`persistedState` saves the resolved `ClientConfig` instead of wireURL/configPath strings:

```go
// Before
type persistedState struct {
    WireURL    string    `json:"wire_url,omitempty"`
    ConfigPath string    `json:"config_path,omitempty"`
    State      string    `json:"state"`
    Timestamp  time.Time `json:"timestamp"`
}

// After
type persistedState struct {
    Config    *config.ClientConfig `json:"config,omitempty"`
    State     string               `json:"state"`
    Timestamp time.Time            `json:"timestamp"`
}
```

**Why this matters**: `configPath` is fragile — the file may not exist after restart, or its contents may have changed. Saving the resolved `ClientConfig` captures exactly what was used, enabling reliable auto-reconnect.

`tryAutoReconnect` reads saved config directly:
```go
if err := d.doUp(st.Config, 0); err != nil { ... }
```

### TD6: CLI — Resolve Before Sending

CLI resolves input into `ClientConfig` before sending to daemon API:

```go
// Before
if strings.Contains(arg, "://") {
    params["wire_url"] = arg
} else {
    params["config_path"] = absPath
}

// After
var cfg *config.ClientConfig
if strings.Contains(arg, "://") {
    cfg = config.ClientFromURL(arg)
} else {
    cfg, err = config.LoadClient(absPath)
}
params["config"] = cfg
```

The daemon doesn't need to know whether the user typed a URL or a file path. It receives `ClientConfig` — one type, one path.

### TD7: Mobile Engine.Start — JSON Config String

gomobile restricts parameters to primitive types (string, int, int64, bool). `Engine.Start()` changes from positional params to a JSON config string:

```go
// Before
func (e *Engine) Start(url string, fd int, dataDir string) error

// After
func (e *Engine) Start(configJSON string, fd int, dataDir string) error
```

`configJSON` is a JSON-serialized `ClientConfig`. The mobile wrapper parses it and maps to `engine.Config`:

```go
func (e *Engine) Start(configJSON string, fd int, dataDir string) error {
    var cfg config.ClientConfig
    if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
        return fmt.Errorf("parse config: %w", err)
    }
    config.SetDefaults(&cfg)
    return e.inner.Start(engine.Config{
        WireURL:        cfg.Server,
        FileDescriptor: fd,
        DataDir:        dataDir,
        RuleMode:       modeFromRuleConfig(cfg.Rule),
        Mode:           cfg.Mode,
        // ...
    })
}
```

This gives mobile the same config-driven path as desktop — rule mode, DNS, proxy settings all flow through config instead of being hardcoded or hacked via URL query params.

### TD8: Eliminate setRuleMode Hack

**Current flow (hack)**:
```
User toggles rule mode
  → webapp calls setRuleMode("smart")
  → K2Plugin writes "smart" to SharedPreferences/UserDefaults
  → User taps Connect
  → webapp calls connect(wireUrl)
  → K2Plugin reads ruleMode from storage, appends &rule=smart to wireUrl
  → Passes modified URL to Engine.Start()
```

**New flow (clean)**:
```
User toggles rule mode
  → webapp stores preference locally (zustand/localStorage)
  → User taps Connect
  → webapp assembles config { server: wireUrl, rule: { global: false } }
  → calls connect(config)
  → K2Plugin passes config JSON to Engine.Start()
  → Engine reads rule.global from config directly
```

Eliminated:
- `setRuleMode()` in K2Plugin (Swift, Kotlin, TS definition, web stub)
- `setRuleMode()` in VpnClient interface and NativeVpnClient
- Native-side preference storage for ruleMode (SharedPreferences, UserDefaults)
- URL query `&rule=` append hack in K2Plugin.connect()

### TD9: getConfig Returns Full Config

`getConfig()` response changes from `{ wireUrl?: string }` to full `ClientConfig`:

```typescript
// Desktop: daemon returns d.lastConfig (the ClientConfig used for current/last connection)
// Mobile: K2Plugin returns the config JSON passed to last connect()
```

This enables the webapp to display current active config (server, rule mode, DNS) and pre-fill the config for reconnect.

### TD10: User Preferences Storage

Webapp stores user preferences in its existing storage (localStorage via zustand persist):

```typescript
// New store or extension of existing vpn.store
interface VpnPreferences {
  ruleMode: 'global' | 'smart';
  // Future: customDns, logLevel, etc.
}
```

At connect time, webapp merges:
```typescript
function buildConfig(server: Server, prefs: VpnPreferences): ClientConfig {
  return {
    server: server.wireUrl,
    rule: { global: prefs.ruleMode === 'global' },
  };
}
```

No native-side preference storage for VPN config. All preferences live in webapp's domain.

## Architecture

### Data Flow (new)

```
Cloud API                          User Preferences
  │ servers[].wireUrl                 │ ruleMode, dns, ...
  │                                   │ (localStorage / zustand)
  └──────────┬────────────────────────┘
             │
             ▼
     webapp: buildConfig()
             │
             ▼ ClientConfig JSON
    ┌────────┴────────────┐
    │                     │
    ▼                     ▼
  Desktop               Mobile
  POST /api/core        K2Plugin.connect({ config })
  { config: {...} }       │
    │                     ▼
    ▼                   K2Plugin (Swift/Kotlin)
  daemon.doUp(cfg)        │ pass configJSON through
    │                     ▼
    ▼                   mobile.Engine.Start(configJSON, fd, dataDir)
  engineConfigFrom-       │
  ClientConfig(cfg)       ▼
    │                   parse JSON → engine.Config
    ▼                     │
  engine.Start(cfg)       ▼
                        engine.Start(cfg)
```

### Elimination Map

```
DELETED (daemon internals):
  daemon.lastWireURL         → daemon.lastConfig *config.ClientConfig
  daemon.lastConfigPath      → (merged into lastConfig)
  daemon.buildEngineConfig() → (eliminated, was unnecessary intermediary)
  persistedState.WireURL     → persistedState.Config *config.ClientConfig
  persistedState.ConfigPath  → (merged into Config)
  statusInfo() wire_url      → (removed from status, use getConfig())
  statusInfo() config_path   → (removed from status, use getConfig())

DELETED (API):
  handleUp wire_url param    → handleUp config param
  handleUp config_path param → (merged into config)
  handleGetConfig wire_url   → handleGetConfig returns ClientConfig

DELETED (CLI):
  cmd_up wire_url param      → cmd_up config param (CLI resolves input first)
  cmd_up config_path param   → (merged into config)

DELETED (mobile):
  setRuleMode() Swift/Kotlin → (eliminated)
  setRuleMode() TS/web stub  → (eliminated)
  &rule= URL append hack     → (eliminated)
  SharedPreferences ruleMode → (eliminated)
  UserDefaults ruleMode      → (eliminated)

DELETED (webapp):
  connect(wireUrl: string)   → connect(config: ClientConfig)
  setRuleMode?() method      → (eliminated)
  VpnConfig.wireUrl          → VpnConfig = ClientConfig
```

### File Changes

```
k2/ (submodule)
  config/config.go           ADD json tags to ClientConfig + sub-structs
  config/config.go           EXPORT setClientDefaults → SetDefaults
  mobile/mobile.go           CHANGE Start(url,fd,dataDir) → Start(configJSON,fd,dataDir)
  daemon/api.go              CHANGE handleUp: config JSON only, CHANGE handleGetConfig: return ClientConfig
  daemon/daemon.go           CHANGE doUp(wireURL,configPath,pid) → doUp(cfg,pid)
  daemon/daemon.go           DELETE buildEngineConfig(), DELETE lastWireURL/lastConfigPath
  daemon/state.go            CHANGE persistedState: Config *ClientConfig (not wireURL+configPath)
  cmd/k2/cmd_up.go           CHANGE: resolve URL/YAML → ClientConfig before sending

webapp/
  src/vpn-client/types.ts    ADD ClientConfig type, CHANGE connect(config), CHANGE VpnConfig
  src/vpn-client/http-client.ts  CHANGE connect to send { config: {...} }
  src/vpn-client/native-client.ts  CHANGE connect to pass config, REMOVE setRuleMode
  src/vpn-client/mock-client.ts  UPDATE to new connect signature
  src/stores/vpn.store.ts    CHANGE connect(wireUrl) → connect(config)
  src/pages/Dashboard.tsx     Assemble config from server + prefs

mobile/plugins/k2-plugin/
  src/definitions.ts         CHANGE connect({ config }), REMOVE setRuleMode
  ios/Plugin/K2Plugin.swift  CHANGE connect to pass configJSON, REMOVE setRuleMode + URL hack
  android/.../K2Plugin.kt    CHANGE connect to pass configJSON, REMOVE setRuleMode + URL hack
```

## Acceptance Criteria

### Config Assembly

- AC1: `ClientConfig` TypeScript type mirrors Go `config.ClientConfig` (server, mode, proxy, dns, rule, log)
- AC2: Webapp assembles config from `server.wireUrl` + user preferences at connect time
- AC3: Omitted fields use k2's defaults (dns, mode, proxy, log)

### Desktop Daemon

- AC4: Daemon `up` action accepts `{ config: {...} }` param (JSON ClientConfig) as the only input
- AC5: `wire_url` and `config_path` params deleted from daemon API
- AC6: `doUp(cfg *config.ClientConfig, pid int)` — no wireURL/configPath string params
- AC7: `buildEngineConfig()` deleted — `engineConfigFromClientConfig()` called directly
- AC8: `getConfig()` returns full `ClientConfig` JSON
- AC9: Auto-reconnect uses saved `ClientConfig` from `persistedState.Config`
- AC10: `persistedState` saves `*config.ClientConfig`, not wireURL/configPath strings

### CLI

- AC11: CLI resolves URL → `ClientConfig` via `config.ClientFromURL()` before sending to daemon
- AC12: CLI resolves YAML file → `ClientConfig` via `config.LoadClient()` before sending to daemon
- AC13: CLI sends `{ "config": {...} }` to daemon API, not `wire_url`/`config_path`

### Mobile

- AC14: `K2Plugin.connect({ config })` passes config JSON to `Engine.Start()`
- AC15: `Engine.Start(configJSON, fd, dataDir)` parses JSON and applies defaults
- AC16: iOS NE receives config JSON through `providerConfiguration`
- AC17: Android VpnService receives config JSON through Intent extra

### Elimination

- AC18: `setRuleMode()` removed from K2Plugin (Swift, Kotlin, TS definition, web stub)
- AC19: `setRuleMode()` removed from VpnClient interface and NativeVpnClient
- AC20: No `&rule=` URL query append in K2Plugin connect
- AC21: No native-side ruleMode storage (SharedPreferences, UserDefaults) for VPN config

### Config Round-Trip

- AC22: `connect(config)` → `getConfig()` returns equivalent config
- AC23: Go `ClientConfig` has JSON tags on all fields (+ sub-structs)
- AC24: JSON config and YAML config.yml produce identical `ClientConfig` for same inputs

## Testing Strategy

### Automated

- **Go unit tests**: `config.ClientConfig` JSON round-trip (marshal → unmarshal → equal)
- **Go unit tests**: `config.ClientConfig` JSON ↔ YAML equivalence for same inputs
- **Go unit tests**: `mobile.Engine.Start(configJSON)` parses valid/invalid JSON
- **Go unit tests**: `daemon.handleUp()` accepts config JSON, rejects wire_url/config_path
- **Go unit tests**: `daemon.doUp(cfg, pid)` accepts `*config.ClientConfig` directly
- **Go unit tests**: `persistedState` JSON round-trip with embedded `*config.ClientConfig`
- **Webapp unit tests**: `buildConfig(server, prefs)` assembles correct ClientConfig
- **Webapp unit tests**: `HttpVpnClient.connect(config)` sends `{ config: {...} }` body
- **Webapp unit tests**: `NativeVpnClient.connect(config)` calls K2Plugin with config
- **Webapp unit tests**: VpnClient interface no longer has `setRuleMode()`

### Manual Integration

- **Desktop end-to-end**: Connect via webapp with rule mode "smart", verify engine uses smart routing
- **Mobile end-to-end**: Connect on iOS + Android with rule mode config, verify no `&rule=` in URL
- **Config round-trip**: Connect, call getConfig(), verify returned config matches what was sent
- **CLI URL**: `k2 up k2v5://...` → daemon receives ClientConfig with server field set
- **CLI config file**: `k2 up config.yml` → daemon receives ClientConfig from YAML
- **Auto-reconnect**: Kill daemon, restart, verify reconnects with saved ClientConfig (not wireUrl)
- **Default handling**: Connect with minimal config (server only), verify DNS/rule/mode defaults applied

## Deployment & CI/CD

Requires coordinated changes across k2 submodule + webapp + mobile plugins:

```
1. k2 submodule: Add JSON tags, export SetDefaults, change mobile.Start, update daemon API+doUp+state, update CLI
2. webapp: New ClientConfig type, update VpnClient, update stores/pages
3. K2Plugin: Update Swift/Kotlin connect(), remove setRuleMode()
4. gomobile bind: Rebuild xcframework + AAR with new Start signature
5. cap sync + native build
```

All changes are breaking (no backward compat). Ship as single coordinated release.

## Key Files

| File | Role |
|------|------|
| `k2/config/config.go` | ClientConfig struct (source of truth) — add JSON tags |
| `k2/mobile/mobile.go` | gomobile wrapper — Start(configJSON, fd, dataDir) |
| `k2/daemon/api.go` | HTTP API — handleUp accepts config JSON only |
| `k2/daemon/daemon.go` | doUp(cfg, pid) — delete buildEngineConfig, lastWireURL, lastConfigPath |
| `k2/daemon/state.go` | persistedState — save *ClientConfig |
| `k2/cmd/k2/cmd_up.go` | CLI — resolve input → ClientConfig before sending |
| `webapp/src/vpn-client/types.ts` | ClientConfig TS type + VpnClient interface |
| `webapp/src/vpn-client/http-client.ts` | Desktop connect sends { config: {...} } |
| `webapp/src/vpn-client/native-client.ts` | Mobile connect passes config |
| `webapp/src/stores/vpn.store.ts` | connect(config) + user preferences |
| `mobile/plugins/k2-plugin/src/definitions.ts` | K2Plugin TS — connect({ config }) |
| `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift` | iOS native — config JSON passthrough |
| `mobile/plugins/k2-plugin/android/.../K2Plugin.kt` | Android native — config JSON passthrough |
