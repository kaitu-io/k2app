# Feature: K2 Service Call Requirements

## Meta

| Field     | Value                          |
|-----------|--------------------------------|
| Feature   | k2-service-call-requirements   |
| Version   | v1                             |
| Status    | implemented                    |
| Created   | 2026-02-20                     |
| Updated   | 2026-02-20                     |
| Depends on | config-driven-connect          |

## Version History

| Version | Date       | Summary                                                           |
|---------|------------|-------------------------------------------------------------------|
| v1      | 2026-02-20 | Config structure alignment + PID monitoring (from TODO: k2-service-call-requirements) |

## Overview

统一 webapp 传给 k2 daemon 的 config 结构，使其与 Go 侧 CLI 的 YAML config 完全一致。同时补全桌面端 PID 传入机制，让 daemon 可以监控调用进程的生命周期。

**两个目标：**
1. **Config 结构统一**：消除 `server.wireUrl`（旧嵌套格式）与 `server: "string"`（Go 规范格式）的不一致
2. **PID 监控**：桌面版 `up` 请求携带 Tauri app PID，daemon 可在 app 退出时自动断开

## Current State

### Config 结构现状

Go 侧 `config.ClientConfig`（规范）：

```yaml
server: "k2v5://udid:token@host:443"
mode: tun
proxy:
  listen: "127.0.0.1:1080"
dns:
  direct: ["114.114.114.114:53"]
  proxy: ["8.8.8.8:53"]
rule:
  global: false
log:
  level: info
```

webapp `ClientConfig` TS 类型（已对齐 Go）：

```typescript
interface ClientConfig {
  server?: string;
  mode?: 'tun' | 'proxy';
  rule?: { global?: boolean };
  log?: { level?: string; output?: string };
  proxy?: { listen?: string };
  dns?: { direct?: string[]; proxy?: string[] };
}
```

**不一致的地方：**

| 位置 | 当前格式 | 规范格式 |
|------|---------|---------|
| `debug.html` preset/placeholder | `{ server: { wireUrl: "vless://..." } }` | `{ server: "vless://..." }` |
| `tauri-k2.test.ts` 测试数据 | `{ server: { wireUrl: 'test://url' } }` | `{ server: 'test://url' }` |

### PID 现状

- Go daemon `handleUp()` 已支持从 `params.pid` 读取 PID（`api.go:75`）
- Tauri bridge `tauri-k2.ts` 未传入 PID — `action === 'up'` 时只包装 `{ config: params }`
- Tauri Rust 侧有 `get_pid` IPC 命令可获取 daemon PID，但 app 自身的 PID 未暴露给 JS

### Config 结构目标（v1 定稿）

基于调研结论（sing-tun 不支持 TUN + proxy 双开，proxy 已是 SOCKS5 + HTTP CONNECT 混合端口），config 结构保持 mode 互斥，拆分 tun/proxy 为独立子配置：

```yaml
# === k2 client configuration (v1 target) ===

# Server connection URL (required)
server: "k2v5://udid:token@host:443?ech=...&pin=sha256:..."

# Traffic capture mode (mutually exclusive)
#   tun   — VPN mode, captures all traffic (needs root/sudo)
#   proxy — SOCKS5 + HTTP CONNECT mixed proxy, no root needed
mode: tun

# TUN mode settings (only used when mode: tun)
tun:
  ipv4: "10.0.0.2/24"
  ipv6: "fd00::2/128"

# Proxy mode settings (only used when mode: proxy)
# Single port, auto-detect protocol: SOCKS5 (0x05) or HTTP CONNECT (ASCII)
proxy:
  listen: "127.0.0.1:1080"

# k2rule routing engine
rule:
  global: false
  # rule_url: ""
  # geoip_url: ""
  # cache_dir: ""

# DNS resolver
dns:
  direct:
    - "114.114.114.114:53"
    - "223.5.5.5:53"
  proxy:
    - "8.8.8.8:53"
    - "1.1.1.1:53"

# Logging
log:
  level: info
  # output: stderr
```

### UI to Config 映射

| UI 位置 | 控件 | Config 字段 | 值 |
|---------|------|------------|-----|
| Dashboard > 节点列表 | Radio 选择 | `server` | tunnel URL string |
| Dashboard > Advanced > 模式 | Toggle TUN/Proxy | `mode` | `"tun"` or `"proxy"` |
| Dashboard > Advanced > 规则 | Toggle Global/Smart | `rule.global` | `true` or `false` |
| Dashboard > Advanced > 日志 | Toggle 4档 | `log.level` | `"error"` / `"warn"` / `"info"` / `"debug"` |
| 无 UI（高级/debug） | — | `tun.ipv4`, `tun.ipv6` | CIDR string |
| 无 UI（高级/debug） | — | `proxy.listen` | host:port string |
| 无 UI（高级/debug） | — | `dns.*` | DNS server arrays |

## Product Requirements

- **PR1**: webapp 传给 daemon 的 config 结构必须与 Go CLI YAML config 完全一致
- **PR2**: 消除所有 `server.wireUrl` 旧格式引用（debug.html、测试文件）
- **PR3**: 桌面端 `up` 请求携带 app PID，daemon 可监控进程存活
- **PR4**: config 新增 `tun` 子配置（ipv4, ipv6），为 TUN 参数化做准备
- **PR5**: `proxy` 子配置保持现有语义（SOCKS5 + HTTP CONNECT 混合端口）
- **PR6**: debug.html 的 preset 和编辑器与规范 config 结构对齐
- **PR7**: Dashboard 选择 proxy 模式时，显示提示信息：(1) 代理模式不自动接管流量，需手动配置应用 (2) HTTP 和 SOCKS5 共用地址 127.0.0.1:1080
- **PR8**: 前端 `config.store` 是 VPN 设置的唯一真理源（single source of truth）。每次设置变更立即持久化到 `_platform.storage`，连接时从 store 组装最终 config

## Technical Decisions

### TD1: PID 传入路径

Tauri bridge (`tauri-k2.ts`) 在 `action === 'up'` 时，JS 层注入当前进程 PID：

```
webapp: _k2.run('up', config)
  -> tauri-k2.ts: invoke('get_pid') -> pid
                  invoke('daemon_exec', { action: 'up', params: { config, pid } })
    -> service.rs: daemon_exec() 透明转发 -> POST { action: 'up', params: { config, pid: 12345 } }
      -> daemon api.go: handleUp() reads params.pid (already supported)
```

PID 来源：`invoke('get_pid')` → Rust `std::process::id()` 获取 Tauri app 进程 PID。在 JS bridge 层注入到 wrapped params 中。`service.rs` 不做额外处理，保持纯转发。

### TD2: Config 结构变更范围

**Go 侧 `config.ClientConfig`**（k2 子模块）：
- 新增 `Tun TunConfig` 字段（ipv4, ipv6）
- `Proxy` 已存在，无需改动
- `Mode` 字段保持 string（"tun" / "proxy"），不变

```go
type TunConfig struct {
    IPv4 string `yaml:"ipv4" json:"ipv4"`
    IPv6 string `yaml:"ipv6" json:"ipv6"`
}

type ClientConfig struct {
    Listen string      `yaml:"listen" json:"listen"`
    Server string      `yaml:"server" json:"server"`
    Mode   string      `yaml:"mode"   json:"mode"`
    Tun    TunConfig   `yaml:"tun"    json:"tun"`     // NEW
    Proxy  ProxyConfig `yaml:"proxy"  json:"proxy"`
    DNS    DNSConfig   `yaml:"dns"    json:"dns"`
    Rule   RuleConfig  `yaml:"rule"   json:"rule"`
    Log    LogConfig   `yaml:"log"    json:"log"`
}
```

**webapp `ClientConfig` TS 类型**：
- 新增 `tun?: { ipv4?: string; ipv6?: string }`
- 其余不变

**daemon to engine 映射**：
- `cfg.Tun.IPv4` / `cfg.Tun.IPv6` -> engine.Config 中相应字段（需在 engine.Config 新增或传给 provider）

### TD3: PID 注入在 JS bridge 层通过 _platform 完成

`tauri-k2.ts` 的 `_k2.run` 在 `action === 'up'` 时通过 `_platform.getPid()` 获取 PID：

```typescript
// In tauri-k2.ts _k2.run():
if (action === 'up' && params) {
  const pid = await window._platform?.getPid?.();
  wrappedParams = { config: params, ...(pid != null && { pid }) };
}
```

遵循宪法规则：bridge 层通过 `_platform` 接口获取平台能力，不直接调用 `invoke`（PID 获取）。`service.rs` 保持纯转发。webapp 调用层完全无感知，bridge 接口 `_k2.run('up', config)` 签名不变。

### TD4: debug.html 对齐

将 debug.html 的 preset 和 placeholder 从旧格式改为规范格式：

```javascript
// Before (旧)
{ server: { wireUrl: 'vless://...' }, rule: { global: true } }

// After (规范)
{ server: 'vless://...', mode: 'tun', rule: { global: true } }
```

### TD5: 测试数据对齐

`tauri-k2.test.ts` 中的测试数据同步更新：

```typescript
// Before
const config = { server: { wireUrl: 'test://url' } };

// After
const config = { server: 'test://url' };
```

### TD6: 前端 Config Store 作为唯一真理源

Config 生命周期：

```
loadConfig() → _platform.storage.get('k2.vpn.config') → config state
updateConfig(partial) → deepMerge → set state → _platform.storage.set() (立即持久化)
buildConnectConfig(serverUrl) → CLIENT_CONFIG_DEFAULTS + stored config + serverUrl → 最终 ClientConfig
```

**原则：**
- Daemon 不存储用户偏好 — 每次 `up` 请求携带完整 config
- `status` 返回的 `config` 字段仅反映当前运行态配置，不写回 store
- 前端 store 是 mode、rule、log、dns、proxy 等用户偏好的唯一来源
- server URL 不持久化到 config store（由 tunnel 选择决定，连接时临时注入）

**已有实现（config.store.ts）：**
- `useConfigStore` Zustand store + `_platform.storage` 持久化
- `updateConfig()` fire-and-forget 异步写入
- `buildConnectConfig()` 合并 defaults + stored + server URL
- Computed getters: `ruleMode`, `mode`, `logLevel`

### TD7: Proxy 模式 UI 提示

选择 proxy 模式时，ToggleButtonGroup 下方显示提示卡片：
- 灰色背景卡片，说明需要手动配置应用使用代理
- 高亮显示共用地址 `127.0.0.1:1080`（HTTP 和 SOCKS5 同端口）
- 选择 TUN 模式时显示简短提示："自动接管所有系统流量，无需额外配置"

## Key Files

| File | Role | Change |
|------|------|--------|
| `k2/config/config.go` | Go ClientConfig 定义 | 新增 `TunConfig` struct + `Tun` field |
| `k2/cmd/k2/client.demo.yml` | CLI demo config | 新增 `tun:` section |
| `k2/daemon/daemon.go` | daemon to engine 映射 | 传递 tun ipv4/ipv6 到 engine config |
| `k2/engine/config.go` | engine Config 定义 | 可能新增 TUN 地址字段 |
| `desktop/src-tauri/src/service.rs` | Tauri daemon_exec IPC | 无改动（纯转发） |
| `webapp/src/services/tauri-k2.ts` | Tauri JS bridge | `up` 时注入 `pid` 到 wrapped params |
| `webapp/src/types/client-config.ts` | TS ClientConfig 类型 | 新增 `tun?` 字段 |
| `webapp/debug.html` | 调试页面 | 修复 `server.wireUrl` 为 `server` |
| `webapp/src/services/__tests__/tauri-k2.test.ts` | Tauri bridge 测试 | 修复测试数据格式 |
| `webapp/src/stores/config.store.ts` | Config 持久化 store | 已实现，无需改动（文档化） |

## Acceptance Criteria

- **AC1**: `debug.html` preset 使用 `{ server: "vless://..." }` 格式，不再有 `server.wireUrl`
- **AC2**: `tauri-k2.test.ts` 测试数据使用 `{ server: 'url' }` 格式，测试通过
- **AC3**: 桌面端 `up` 请求的 HTTP body 中包含 `params.pid`（值为 Tauri app 进程 PID）
- **AC4**: Go `config.ClientConfig` 包含 `Tun TunConfig` 字段（ipv4, ipv6）
- **AC5**: `client.demo.yml` 包含 `tun:` section 及默认值
- **AC6**: webapp `ClientConfig` TS 类型包含 `tun?: { ipv4?: string; ipv6?: string }`
- **AC7**: `mode: "proxy"` 时 proxy provider 行为不变（SOCKS5 + HTTP CONNECT 混合端口）
- **AC8**: `mode: "tun"` 时 tun config 的 ipv4/ipv6 传递到 engine/provider 层
- **AC9**: 现有 `cargo test` 和 `vitest` 测试全部通过
- **AC10**: PID 注入对 webapp 层透明 -- bridge 接口 `_k2.run('up', config)` 签名不变
- **AC11**: Dashboard 选择 proxy 模式时显示提示卡片，包含"需手动配置"说明和 `127.0.0.1:1080` 地址
- **AC12**: Dashboard 选择 TUN 模式时显示"自动接管所有系统流量"提示
- **AC13**: `config.store` 每次 `updateConfig()` 后数据立即持久化到 `_platform.storage`
- **AC14**: `buildConnectConfig()` 合并 defaults + stored config + server URL，daemon 每次收到完整 config
