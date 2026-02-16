# Feature: Unified Engine + Rule Mode

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | unified-engine                           |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-16                               |
| Updated   | 2026-02-16                               |

## Version History

| Version | Date       | Summary                                                  |
|---------|------------|----------------------------------------------------------|
| v1      | 2026-02-16 | Initial: unified engine, rule mode, mobile storage       |

## Overview

Extract the 80% duplicated tunnel assembly logic from desktop `daemon/tunnel.go` (`BuildTunnel()`) and mobile `mobile/mobile.go` (`Engine.Start()`) into a shared `engine/` package. The desktop daemon becomes a thin HTTP shell over Engine, and the mobile wrapper becomes a gomobile type-adapter layer. During this unification, also resolve the missing k2rule storage directory and rule-mode switching on mobile.

## Product Requirements

- PR1: Desktop and mobile use the same Engine to assemble tunnels, eliminating 80% code duplication
- PR2: Desktop daemon becomes a thin HTTP shell over Engine; it no longer assembles tunnels itself
- PR3: Users can switch routing rule mode on mobile: global (proxy all traffic) and smart (GeoIP-based split routing)
- PR4: Rule mode switching requires disconnect and reconnect; hot-switching is not supported
- PR5: Rule mode selection is persisted to native local storage and survives app restarts
- PR6: The Go Engine has a writable storage directory so k2rule can download GeoIP/domain rule files to local cache
- PR7: iOS dual-process architecture (App + NE) shares the same storage directory via App Group container
- PR8: In smart mode, rule files are downloaded on first connect (k2rule lazy load) without blocking connection establishment
- PR9: The webapp `connect(wireUrl)` interface signature remains unchanged; K2Plugin internally appends the rule parameter

## Technical Decisions

### TD1: New `k2/engine/` shared package

Engine is the single tunnel lifecycle manager. Core API:

```go
// engine/engine.go
type Engine struct { ... }

type Config struct {
    WireURL        string
    FileDescriptor int              // -1 = self-created TUN (desktop), >=0 = platform-provided (mobile)
    DataDir        string           // k2rule cache directory
    RuleMode       string           // "global" | "smart", default "global"
    DirectDialer   *directdial.Dialer // desktop only, mobile passes nil
    PreferIPv6     bool             // desktop only
    Mode           string           // "tun" | "proxy", default "tun"
    ProxyListen    string           // proxy mode listen address
    DNSExclude     []netip.Addr     // desktop route exclusion
    RuleConfig     *k2rule.Config   // full k2rule config; when nil, auto-built from RuleMode+DataDir
}

func New() *Engine
func (e *Engine) Start(cfg Config) error
func (e *Engine) Stop() error
func (e *Engine) StatusJSON() string
func (e *Engine) SetEventHandler(h EventHandler)
```

Rationale: The Config struct contains optional fields for all platform differences. gomobile cannot use this API directly (structs are not supported), so the `mobile/` package provides a gomobile-friendly thin wrapper.

### TD2: `k2/mobile/mobile.go` becomes a gomobile thin wrapper

```go
// mobile/mobile.go — type adaptation only
type Engine struct {
    inner *engine.Engine
}

func NewEngine() *Engine {
    return &Engine{inner: engine.New()}
}

func (e *Engine) Start(url string, fd int, dataDir string) error {
    return e.inner.Start(engine.Config{
        WireURL:        url,
        FileDescriptor: fd,
        DataDir:        dataDir,
        // RuleMode is auto-parsed from URL query
    })
}
```

gomobile exports only `Start(string, int, string)` -- three primitive-type parameters.

### TD3: `k2/daemon/` uses `engine.Engine` directly

```go
// daemon/daemon.go — doUp() refactored
func (d *Daemon) doUp(wireURL, configPath string, pid int) error {
    cfg := d.buildEngineConfig(wireURL, configPath)
    eng := engine.New()
    if err := eng.Start(cfg); err != nil { ... }
    d.engine = eng
    ...
}
```

`daemon/tunnel.go`'s `BuildTunnel()` is replaced by `engine.Start()` and can be deleted.
The daemon retains only: HTTP API, state persistence, auto-reconnect, and process monitoring.

### TD4: Unified tunnel assembly flow in Engine

Engine.Start() internal flow:

```
1. k2rule Init (from Config.RuleMode + DataDir or Config.RuleConfig)
2. wire.ParseURL(WireURL)
3. [If PreferIPv6 && wireCfg.IPv6 available] replace Host
4. [If DirectDialer != nil] bind to transport
5. Build Transports (QUIC + TCPWS)
6. Build Router
7. Build Provider:
   - fd >= 0 -> NewTUNProvider(fd)
   - fd == -1 && Mode == "proxy" -> NewProxyProvider
   - fd == -1 && Mode == "tun" -> NewTUNProvider(self-created, DNSExclude)
8. Build ClientTunnel
9. Start:
   - fd >= 0 (mobile) -> prov.Start(ctx, &dnsHandler{...})  // DNS middleware interception
   - fd < 0 (desktop) -> tunnel.Start(ctx)                   // route exclusion
```

Desktop-only features (DirectDialer, IPv6 preference, proxy mode, DNS exclusion) are injected via optional Config fields; mobile leaves them unset and they are skipped.

### TD5: `rule=smart|global` appended to URL by K2Plugin native layer

The webapp `connect(wireUrl)` signature remains unchanged. K2Plugin reads the locally stored ruleMode before initiating connection and appends `&rule=xxx` to wireUrl.

### TD6: Engine obtains RuleMode from URL or Config

Priority: `Config.RuleConfig` (desktop full config) > `Config.RuleMode` > URL query `rule=` > default `global`.

Engine internal logic:
```go
if cfg.RuleConfig != nil {
    k2rule.Init(cfg.RuleConfig)  // desktop: full k2rule config
} else {
    // mobile: build from RuleMode + DataDir
    isGlobal := ruleMode != "smart"
    k2rule.Init(&k2rule.Config{
        IsGlobal: isGlobal, GlobalTarget: k2rule.TargetProxy,
        CacheDir: cfg.DataDir,
    })
}
```

### TD7: Storage directories

| Platform | dataDir                                                        |
|----------|----------------------------------------------------------------|
| Desktop  | Not passed via DataDir; controlled via RuleConfig.CacheDir (~/.cache/k2rule/) |
| iOS      | App Group container (`group.io.kaitu`) + `/k2`                 |
| Android  | `context.filesDir.absolutePath`                                |

### TD8: ruleMode persistence

| Platform | Storage location                                               |
|----------|----------------------------------------------------------------|
| iOS      | `UserDefaults(suiteName: kAppGroup)` key `ruleMode`            |
| Android  | `SharedPreferences("k2vpn")` key `ruleMode`                   |
| Desktop  | Not applicable (controlled by `rule.global` field in config.yaml) |

### TD9: iOS NE cold start

The ruleMode is already embedded in the wireUrl within providerConfiguration. On NE cold start, the rule parameter is parsed directly from wireUrl without needing to read UserDefaults separately.

## Acceptance Criteria

### Engine

- AC1: New `k2/engine/` package containing Engine struct + Config struct + Start/Stop/StatusJSON
- AC2: Engine.Start() implements the unified tunnel assembly flow: k2rule -> transport -> router -> provider -> tunnel
- AC3: fd >= 0 uses the platform TUN fd + DNS middleware interception mode
- AC4: fd == -1 self-creates TUN (desktop) + route exclusion mode
- AC5: Config.DirectDialer != nil binds to transport
- AC6: Config.PreferIPv6 == true checks and replaces wireCfg.Host
- AC7: Config.Mode == "proxy" uses ProxyProvider

### Desktop

- AC8: `daemon/tunnel.go`'s `BuildTunnel()` deleted; daemon.doUp() uses engine.Engine instead
- AC9: Existing daemon HTTP API (up/down/status/version/get_config/ping) behavior unchanged
- AC10: Daemon auto-reconnect, state persistence, and process monitoring logic unchanged
- AC11: `cargo test` (desktop Rust) + `yarn test` (webapp) pass

### Mobile

- AC12: `mobile/mobile.go` becomes a gomobile thin wrapper over `engine.Engine`
- AC13: `Engine.Start(url, fd, dataDir)` -- gomobile exports three primitive-type parameters

### Rule Mode

- AC14: Engine parses `rule=smart|global` from URL query, defaults to `global`
- AC15: `rule=smart` -> k2rule `IsGlobal: false` + CacheDir; `rule=global` -> `IsGlobal: true`
- AC16: K2Plugin.swift adds `setRuleMode`; connect() appends `&rule=xxx`
- AC17: K2Plugin.kt adds `setRuleMode`; startVpnService() appends `&rule=xxx`
- AC18: PacketTunnelProvider passes App Group `/k2` path as dataDir
- AC19: K2VpnService passes `filesDir` as dataDir
- AC20: definitions.ts + native-client.ts add setRuleMode declaration and implementation
- AC21: Dashboard UI calls setRuleMode then disconnects and reconnects to apply

## Testing Strategy

- **Engine package unit tests**: Test Config combinations (mobile/desktop/proxy) for assembly logic
- **Daemon regression tests**: api_test.go verifies up/down/status behavior unchanged
- **Mobile integration tests**: Manual verification of iOS/Android setRuleMode + connect + dataDir
- **iOS NE cold start**: Kill process, let system auto-reconnect, verify rule parameter is correctly passed
- **Desktop end-to-end**: `make dev` to verify connect/disconnect/reconnect/auto-reconnect full flow

## Deployment & CI/CD

- gomobile bind must rebuild AAR/xcframework
- CI must ensure `go test ./engine/...` passes
- No CI pipeline changes; reuses existing pipeline

## Impact Analysis

- New files:
  - `k2/engine/engine.go` -- Engine struct, Start(), Stop(), StatusJSON()
  - `k2/engine/config.go` -- Config struct + defaults
  - `k2/engine/dns_handler.go` -- dnsHandler (moved from mobile/mobile.go)
  - `k2/engine/event.go` -- EventHandler interface + state constants
- Refactored files:
  - `k2/daemon/daemon.go` -- Daemon.doUp() uses engine.Engine
  - `k2/daemon/tunnel.go` -- BuildTunnel() deleted
  - `k2/mobile/mobile.go` -- becomes gomobile thin wrapper over engine.Engine
- Modified files:
  - `K2Plugin.swift` -- setRuleMode() + connect() URL assembly
  - `K2Plugin.kt` -- setRuleMode() + startVpnService() URL assembly
  - `PacketTunnelProvider.swift` -- passes dataDir to engine.start()
  - `K2VpnService.kt` -- passes dataDir to engine.start()
  - `definitions.ts` -- setRuleMode declaration
  - `native-client.ts` -- setRuleMode proxy call
- Unchanged:
  - `wire.ParseURL()` -- already ignores unknown parameters
  - `config/config.go` -- ClientConfig/RuleConfig unchanged
  - `provider/tun_*.go` -- unchanged
  - `directdial/` -- unchanged
  - `daemon/api.go` -- HTTP handlers unchanged (doUp/doDown signatures unchanged)
  - `daemon/state.go` -- persistence logic unchanged
  - `webapp/connect()` signature unchanged
- Scope: large -- 4 new files, 3 refactored, 6 modified
