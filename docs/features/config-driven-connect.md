# Feature: Config-Driven Connect

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | config-driven-connect                    |
| Version   | v2                                       |
| Status    | draft                                    |
| Created   | 2026-02-16                               |
| Updated   | 2026-02-18                               |
| Depends on | mobile-webapp-bridge, mobile-vpn-ios, mobile-vpn-android |

## Version History

| Version | Date       | Summary                                                        |
|---------|------------|----------------------------------------------------------------|
| v1      | 2026-02-16 | Initial: replace wireUrl passthrough with structured ClientConfig |
| v2      | 2026-02-18 | Config as single source of truth: _platform persistence, config.store, UI visualization |

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

---

# v2: Config 唯一真理 — 持久化 + UI 可视化

## v2 Overview

(v2, webapp-only evolution. Go daemon/CLI/mobile K2Plugin unchanged.)

v1 建立了 config-driven connect 模式（替代 wireUrl passthrough）。但 webapp 中 config 是临时的 — rule mode、DNS mode 存在 `useState` 中，刷新即丢；config 在 `assembleConfig()` 中即时拼装，无持久化。

v2 让 `ClientConfig` 成为 webapp 的**唯一真理**（single source of truth）：
- 通过 `_platform.secureStorage` 跨平台持久化（不用 localStorage）
- 独立 `config.store.ts` 管理完整配置生命周期
- Dashboard UI 控件直接读写 configStore，设置变更即时持久化
- 所有用户可配置字段（rule、dnsMode、mode、log.level）均有 UI 入口

**设计原则：webapp 即配置编辑器**。和 WireGuard/Clash 一样 — UI 上的每个开关对应 config 中的一个字段，改了就存，连接时直接用。

## v2 Current State (v1 implemented)

| 项目 | v1 现状 | 问题 |
|------|---------|------|
| rule mode | `useState('chnroute')` in Dashboard | 刷新丢失，默认值从 appConfig 硬编码 |
| DNS mode | `useState('fake-ip')` in Dashboard | 刷新丢失 |
| mode (tun/proxy) | 未暴露 UI | 只能通过 Go 默认值 |
| log level | 无 | 只能通过 Go 默认值 |
| server | 选择后即用，不存 | 无法重连上次服务器 |
| assembleConfig() | Dashboard 中 ad-hoc 拼装 | 散落在页面组件里，非集中管理 |
| 存储 | 无持久化 | 每次打开 app 回到默认 |

## v2 Product Requirements

- PR9: ClientConfig 持久化存储在 `_platform.secureStorage`，所有平台统一
- PR10: 独立 `config.store.ts` 管理 config 的加载、更新、保存
- PR11: Dashboard UI 控件（rule、dnsMode、mode、log.level）读写 configStore，不再用 useState
- PR12: 设置变更即时持久化（不需要"保存"按钮）
- PR13: App 重启后恢复上次配置（包括 server，支持重连）
- PR14: 连接成功后 server 字段写入 config（记住上次连接的服务器）

## v2 Technical Decisions

### TD11: ClientConfig v2 Schema (唯一真理)

一个类型，贯穿存储→UI→连接全流程：

```typescript
// src/types/client-config.ts — 唯一定义，全局引用

/**
 * ClientConfig — webapp 的唯一配置真理
 *
 * 字段与 Go config.ClientConfig JSON tags 对齐。
 * 存储在 _platform.secureStorage，UI 控件直接读写。
 * 连接时整体传给 _k2.run('up', config)。
 */
export interface ClientConfig {
  /** k2v5:// wire URL — 上次连接的服务器（连接成功后写入） */
  server?: string;

  /** 代理模式 — "tun"(系统全局) | "proxy"(SOCKS5 代理) */
  mode?: 'tun' | 'proxy';

  /** 路由规则 */
  rule?: {
    /** true=全局代理, false=智能分流(chnroute) */
    global?: boolean;
  };

  /** DNS 解析模式 — "fake-ip" | "real-ip" */
  dns_mode?: 'fake-ip' | 'real-ip';

  /** 日志配置 */
  log?: {
    /** "debug" | "info" | "warn" | "error" */
    level?: string;
  };
}

/** 各字段的 webapp 侧默认值（Go 侧 SetDefaults 也会兜底） */
export const CLIENT_CONFIG_DEFAULTS: Required<Pick<ClientConfig, 'mode' | 'dns_mode'>> & {
  rule: Required<NonNullable<ClientConfig['rule']>>;
  log: Required<NonNullable<ClientConfig['log']>>;
} = {
  mode: 'tun',
  rule: { global: false },
  dns_mode: 'fake-ip',
  log: { level: 'info' },
};
```

**字段设计决策：**

| 字段 | JSON key | 来源 | UI 控件 | 默认值 |
|------|----------|------|---------|--------|
| server | `server` | 用户选择服务器 + 连接成功后存入 | 无直接控件（服务器列表选择） | 无（首次需选） |
| mode | `mode` | 用户偏好 | ToggleButtonGroup | `'tun'` |
| rule.global | `rule.global` | 用户偏好 | ToggleButtonGroup | `false` (智能分流) |
| dns_mode | `dns_mode` | 用户偏好 | ToggleButtonGroup | `'fake-ip'` |
| log.level | `log.level` | 用户偏好 | Select | `'info'` |

**不存储的字段（由 Go SetDefaults 填充）：**
- `proxy.listen` — 内部地址，用户无需配置
- `dns.direct` / `dns.proxy` — DNS 服务器列表，用默认值
- `k2v4` — 协议参数，服务端下发
- `ipv6` / `insecure` — 平台/调试相关

**`dns_mode` 用 snake_case 的原因：** 和 Go `config.ClientConfig` 的 `json:"dns_mode"` tag 对齐。发送给 daemon 时不需要任何 key 转换 — 存的就是发的。

### TD12: _platform.secureStorage 持久化

```typescript
const CONFIG_STORAGE_KEY = 'k2.vpn.config';

// 读取
async function loadConfig(): Promise<ClientConfig> {
  const stored = await window._platform.storage.get<ClientConfig>(CONFIG_STORAGE_KEY);
  return stored ?? {};
}

// 保存
async function saveConfig(config: ClientConfig): Promise<void> {
  await window._platform.storage.set(CONFIG_STORAGE_KEY, config);
}
```

**设计要点：**
- 单 key 原子读写 — 不拆分字段，避免部分更新的一致性问题
- 空 config `{}` 是合法的 — 表示"全部用默认值"
- `secureStorage` 在所有平台用 AES-256-GCM 加密（设备指纹派生密钥）
- 不用 TTL — config 永久有效
- 不用 Zustand persist middleware — 自己控制 load/save 时机，因为 `_platform` 是 async 的且需要等待注入

### TD13: config.store.ts — 配置状态管理

```typescript
// src/stores/config.store.ts
import { create } from 'zustand';
import type { ClientConfig } from '../types/client-config';
import { CLIENT_CONFIG_DEFAULTS } from '../types/client-config';

interface ConfigStore {
  /** 完整配置（唯一真理） */
  config: ClientConfig;

  /** 是否已从 storage 加载完成 */
  loaded: boolean;

  /**
   * 启动时从 _platform.secureStorage 加载
   * 在 initializeAllStores() 中调用
   */
  loadConfig: () => Promise<void>;

  /**
   * 更新配置字段并自动持久化
   * Dashboard UI 控件调用此方法
   *
   * @example updateConfig({ rule: { global: true } })
   * @example updateConfig({ dns_mode: 'real-ip' })
   */
  updateConfig: (partial: Partial<ClientConfig>) => Promise<void>;

  /**
   * 构建连接用的完整 config
   * 合并存储的偏好 + 运行时 server URL
   *
   * @param serverUrl 选择的服务器 wireUrl（可选，不传则用上次的 server）
   */
  buildConnectConfig: (serverUrl?: string) => ClientConfig;

  // ── 便捷 getter（避免 UI 层深入访问 config 内部结构） ──

  /** 当前 rule mode: 'global' | 'chnroute' */
  readonly ruleMode: 'global' | 'chnroute';
  /** 当前 DNS mode */
  readonly dnsMode: 'fake-ip' | 'real-ip';
  /** 当前代理模式 */
  readonly mode: 'tun' | 'proxy';
  /** 当前日志级别 */
  readonly logLevel: string;
}
```

**Store 行为：**

```
loadConfig()
  ├── _platform.secureStorage.get('k2.vpn.config')
  ├── set({ config: stored ?? {}, loaded: true })
  └── 若 storage 为空 → config = {}（全部用默认值）

updateConfig({ rule: { global: true } })
  ├── 深合并: newConfig = deepMerge(current, partial)
  ├── set({ config: newConfig })
  └── _platform.secureStorage.set('k2.vpn.config', newConfig)
      └── 异步，但不阻塞 UI（fire-and-forget with error log）

buildConnectConfig(serverUrl?)
  ├── base = { ...CLIENT_CONFIG_DEFAULTS }
  ├── merged = deepMerge(base, this.config)
  ├── if serverUrl → merged.server = serverUrl
  └── return merged  // 完整 config，可直接传给 _k2.run('up', config)
```

**getter 映射（UI 友好）：**

```typescript
get ruleMode() {
  return this.config.rule?.global ? 'global' : 'chnroute';
}
get dnsMode() {
  return this.config.dns_mode ?? CLIENT_CONFIG_DEFAULTS.dns_mode;
}
get mode() {
  return this.config.mode ?? CLIENT_CONFIG_DEFAULTS.mode;
}
get logLevel() {
  return this.config.log?.level ?? CLIENT_CONFIG_DEFAULTS.log.level;
}
```

### TD14: Store 初始化时序

```
main.tsx:
  1. Sentry init
  2. i18next init
  3. Platform detection → inject _k2 + _platform
  4. initializeAllStores()
     ├── layoutStore.init()
     ├── configStore.loadConfig()  ← NEW: 从 _platform 加载 config
     ├── authStore.init()
     └── vpnStore.init()
  5. render(<App />)
```

`configStore.loadConfig()` 必须在 `vpnStore.init()` 之前 — vpn store 可能需要 config 做自动重连。

### TD15: Dashboard UI 改造 — useState → configStore

**Before (v1):**
```typescript
// Dashboard.tsx — 散落的 volatile state
const [activeRuleType, setActiveRuleType] = useState<string>(
  getCurrentAppConfig().features.proxyRule.defaultValue
);
const [activeDnsMode, setActiveDnsMode] = useState<string>('fake-ip');

const assembleConfig = useCallback(() => {
  const config: Record<string, any> = {};
  if (selectedCloudTunnel?.url) config.server = selectedCloudTunnel.url;
  config.rule = { global: activeRuleType === 'global' };
  return config;
}, [selectedCloudTunnel, activeRuleType]);
```

**After (v2):**
```typescript
// Dashboard.tsx — 从 configStore 读写
const { ruleMode, dnsMode, mode, updateConfig, buildConnectConfig } = useConfigStore();

// UI 控件直接更新 store（自动持久化）
const handleRuleTypeChange = (type: string) => {
  updateConfig({ rule: { global: type === 'global' } });
};
const handleDnsModeChange = (mode: string) => {
  updateConfig({ dns_mode: mode as 'fake-ip' | 'real-ip' });
};
const handleModeChange = (mode: string) => {
  updateConfig({ mode: mode as 'tun' | 'proxy' });
};
const handleLogLevelChange = (level: string) => {
  updateConfig({ log: { level } });
};

// 连接时 — 从 store 构建完整 config
const handleConnect = () => {
  const config = buildConnectConfig(selectedCloudTunnel?.url);
  _k2.run('up', config);
  // 连接成功后存 server
  updateConfig({ server: selectedCloudTunnel?.url });
};
```

**删除项：**
- `useState(activeRuleType)` → 用 `configStore.ruleMode`
- `useState(activeDnsMode)` → 用 `configStore.dnsMode`
- `assembleConfig()` → 用 `configStore.buildConnectConfig()`
- `appConfig.features.proxyRule.defaultValue` 作为 rule 默认值 → 用 `CLIENT_CONFIG_DEFAULTS`

### TD16: 重连流程

```
App 重启 → configStore.loadConfig() → config.server 有值
  → vpnStore 自动重连逻辑: buildConnectConfig()（不传 serverUrl，用存储的 server）
  → _k2.run('up', config)
```

server 在连接**成功后**才写入 config — 避免存入无效 server。若连接失败，config.server 保持上次成功的值（或为空）。

### TD17: Status 不回写 configStore

status 轮询返回的 `config` 字段（从 daemon/K2Plugin）**不回写** configStore。原因：
- Go `SetDefaults()` 会填充所有字段（dns 服务器列表、proxy 地址等），回写会把用户"未设置=使用默认"的意图覆盖为显式值
- 下次连接应该用用户的 minimal config + Go 的最新默认值，而非上次 Go 填充过的 full config
- configStore 代表**用户意图**，status config 代表**运行时实际值**

如需展示当前运行配置（如"当前连接的服务器"），从 vpnStore 的 status 中读取，不从 configStore 读。

## v2 Architecture

### 数据流（v2 完整）

```
┌─────────────────────────────────────────────────────────────────────┐
│                    _platform.secureStorage                          │
│                    key: 'k2.vpn.config'                             │
│                    { server, mode, rule, dns_mode, log }            │
└──────────────────────────┬──────────────────────────────────────────┘
                           │ load (app start)
                           │ save (on every change)
                           ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    configStore (Zustand)                             │
│                    唯一真理 — 用户配置意图                            │
│                                                                      │
│  ┌─── getter ───┐    ┌── action ──┐    ┌── action ──────────┐       │
│  │ ruleMode     │    │ updateConfig│    │ buildConnectConfig │       │
│  │ dnsMode      │    │ (auto-save)│    │ (merge + defaults) │       │
│  │ mode         │    └─────┬──────┘    └────────┬───────────┘       │
│  │ logLevel     │          │                     │                   │
│  └──────┬───────┘          │                     │                   │
└─────────┼──────────────────┼─────────────────────┼───────────────────┘
          │ read             │ write                │ read+merge
          ▼                  ▼                      ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    Dashboard UI                                      │
│                                                                      │
│  [Rule: 全局 | 智能]  →  updateConfig({ rule: { global } })         │
│  [DNS:  FakeIP | RealIP] → updateConfig({ dns_mode })               │
│  [Mode: TUN | Proxy]    → updateConfig({ mode })                    │
│  [Log:  debug|info|...]  → updateConfig({ log: { level } })         │
│                                                                      │
│  [连接按钮] → buildConnectConfig(selectedServer.url)                 │
│              → _k2.run('up', config)                                 │
│              → success → updateConfig({ server })                    │
└─────────────────────────────────────────────────────────────────────┘
```

### 与 v1 的差异

```
v1:
  useState(ruleType)  ──→  assembleConfig()  ──→  _k2.run('up', config)
  useState(dnsMode)  ─┘    (ad-hoc, volatile)

v2:
  _platform.storage ──→ configStore ──→ buildConnectConfig() ──→ _k2.run('up', config)
                         ↑ ↓                                        │
                    Dashboard UI                               success → save server
                    (读写 configStore)
```

## v2 File Changes

```
webapp/
  src/types/client-config.ts       NEW: ClientConfig 类型 + CLIENT_CONFIG_DEFAULTS
  src/stores/config.store.ts       NEW: configStore (load/update/buildConnect)
  src/stores/index.ts              ADD: export useConfigStore, initializeAllStores 加入 configStore.loadConfig()
  src/pages/Dashboard.tsx          CHANGE: 删除 useState(activeRuleType/activeDnsMode)，
                                          删除 assembleConfig()，
                                          改用 configStore 读写
                                   ADD: mode 切换 UI、log.level 选择 UI
  src/services/control-types.ts    可能 REMOVE ConfigResponseData 中重复的类型（清理）
```

## v2 Acceptance Criteria

### Config Store

- AC25: `config.store.ts` 存在，使用 Zustand 管理 `ClientConfig`
- AC26: `loadConfig()` 从 `_platform.secureStorage.get('k2.vpn.config')` 加载
- AC27: `updateConfig(partial)` 深合并后自动调用 `_platform.secureStorage.set()` 持久化
- AC28: `buildConnectConfig(serverUrl?)` 返回完整 config（defaults + stored + runtime server）
- AC29: 便捷 getter（ruleMode, dnsMode, mode, logLevel）返回有默认值兜底的当前值

### 持久化

- AC30: 存储 key 为 `k2.vpn.config`，使用 `_platform.secureStorage`（非 localStorage 直接访问）
- AC31: App 重启后 config 恢复（rule mode、DNS mode、mode、log level、server 均保持）
- AC32: 设置变更即时持久化（无"保存"按钮）
- AC33: 所有平台（Tauri/Capacitor/Standalone）使用相同的存储路径

### UI 可视化

- AC34: Dashboard rule mode toggle 读写 configStore（非 useState）
- AC35: Dashboard DNS mode toggle 读写 configStore（非 useState）
- AC36: Dashboard 新增 mode (tun/proxy) 切换控件
- AC37: Dashboard 新增 log level 选择控件
- AC38: `assembleConfig()` 函数删除，被 `buildConnectConfig()` 替代
- AC39: `useState(activeRuleType)` 和 `useState(activeDnsMode)` 删除

### 初始化 & 重连

- AC40: `configStore.loadConfig()` 在 `initializeAllStores()` 中调用，早于 `vpnStore.init()`
- AC41: 连接成功后 server URL 写入 config 并持久化
- AC42: Status 返回的 config 不回写 configStore

### 唯一真理原则

- AC43: webapp 中无任何 config 相关的 `useState` — 全部从 configStore 读取
- AC44: 无任何 Zustand persist middleware — 持久化由 configStore 通过 `_platform` 显式控制
- AC45: `ClientConfig` 类型定义在 `src/types/client-config.ts`，全局唯一引用

## v2 Testing Strategy

### Automated

- **config.store unit test**: loadConfig → updateConfig → buildConnectConfig round-trip
- **config.store unit test**: updateConfig 调用 `_platform.secureStorage.set`（mock 验证）
- **config.store unit test**: buildConnectConfig 正确合并 defaults + stored + serverUrl
- **config.store unit test**: getter 有默认值兜底（config 为空对象时）
- **Dashboard unit test**: rule toggle 调用 `updateConfig({ rule: { global } })`
- **Dashboard unit test**: 连接按钮调用 `buildConnectConfig(selectedServer.url)`

### Manual Integration

- **持久化**: 修改 rule mode → 刷新页面 → 验证 rule mode 保持
- **跨平台**: Tauri + Capacitor + Standalone 三个平台配置均持久化
- **重连**: 连接服务器 → 关闭 app → 重新打开 → 验证 server 记住
- **默认值**: 清除 storage → 打开 app → 验证所有字段使用默认值
- **连接失败不存 server**: 连接失败 → 验证 config.server 未被覆盖
