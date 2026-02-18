# Feature: k2 Wire Protocol — App Integration

## Meta

| Field | Value |
|-------|-------|
| Feature | k2-wire-protocol |
| Version | v1 |
| Status | implemented |
| Created | 2026-02-18 |
| Updated | 2026-02-18 |

## Overview

k2 是 Kaitu VPN 的 Go 核心引擎（独立 Git submodule），实现了 k2v5 composite tunnel 协议（QUIC/H3 首选 + TCP-WebSocket 回退）、内置抗审查能力（ECH、uTLS、TLS padding、PCC Vivace）、以及跨平台统一的隧道生命周期管理。

本 spec 聚焦 **k2app 如何集成 k2 核心**——Daemon HTTP API（桌面）、gomobile SDK（移动）、ClientConfig 契约、以及 bridge 层状态映射。k2 内部协议实现详见 k2 submodule 自有文档：

| 文档 | 内容 |
|------|------|
| `k2/AGENT.md` | 架构总览、包结构、核心接口 |
| `k2/wire/k2v5-protocol-spec.md` | k2v5 协议完整规范（URL scheme、framing、transport、auth、ECH、PCC） |
| `k2/docs/features/` | k2 独立 feature specs（cloud-webapp、mobile-sdk、private-ip-guard 等） |

## Product Requirements

### PR-1: Daemon HTTP API（桌面端）

桌面端通过 HTTP API（`127.0.0.1:1777`）控制隧道，Webapp 经 Tauri IPC `invoke('daemon_exec')` 间接调用（WebView 无法直接跨端口 fetch）。

4 个 action：

| Action | Params | Response |
|--------|--------|----------|
| `up` | `config` (ClientConfig JSON) + `pid` | - |
| `down` | - | - |
| `status` | - | `{state, connected_at, uptime_seconds, error, config}` |
| `version` | - | `{version, go, os, arch}` |

Daemon 状态值：`stopped` / `connecting` / `connected`。注意 daemon 用 `stopped` 而非 `disconnected`，bridge `transformStatus()` 负责映射。

辅助端点：`/ping`（健康检查）、`/metrics`（goroutines, heap）、`/api/device/udid`（设备 ID）。

Auto-reconnect：持久化状态到磁盘，daemon 启动后 5s 尝试用 saved config 重连（1h 内有效）。

### PR-2: gomobile SDK（移动端）

gomobile 包装 `engine.Engine`，只暴露兼容类型（string, int, int64）。通过 `EventHandler` 回调通知 native 层状态变化。

```go
// k2/mobile/mobile.go
func (e *Engine) Start(configJSON string, fd int, dataDir string) error
func (e *Engine) Stop() error
func (e *Engine) OnNetworkChanged()
func (e *Engine) StatusJSON() string
func (e *Engine) SetEventHandler(h EventHandler)
```

`EventHandler` 接口：`OnStateChange(state)` / `OnError(message)` / `OnStats(tx, rx)`——所有参数为 gomobile 兼容简单类型。

### PR-3: ClientConfig 契约

Go 和 TS 共享 `ClientConfig` 结构：

```go
type ClientConfig struct {
    Listen string      // daemon 监听地址，default "127.0.0.1:1777"
    Server string      // k2v5:// URL
    Mode   string      // "tun" or "proxy"
    Proxy  ProxyConfig
    DNS    DNSConfig
    Rule   RuleConfig  // rule URLs, cache dir, global flag
    Log    LogConfig
}
```

**JSON key 约定**：Go `json.Marshal` 输出 snake_case。JS/TS 期望 camelCase——bridge 层（K2Plugin.swift/kt）在边界做 remap。

**Webapp 组装最小 config**：Dashboard 只传 `server` + `rule.global`，Go 侧 `config.SetDefaults()` 填充默认值。

### PR-4: Desktop vs Mobile TUN 来源

| 平台 | FileDescriptor | TUN 来源 |
|------|---------------|---------|
| Desktop | `-1` | engine 自建 TUN 或 proxy provider |
| iOS | `>= 0` | PacketTunnelProvider 的 `packetFlow` KVC fd |
| Android | `>= 0` | VpnService 的 `establish()` fd |

Mobile path 额外插入 DNS middleware（拦截 port 53 UDP -> 本地解析）。

### PR-5: Network Change Recovery

移动端网络切换（WiFi <-> Cellular）时，`OnNetworkChanged()` 重置 wire transport 的缓存连接（`Resettable` 接口），下次 Dial 自动重建。过程中发送瞬态 `reconnecting` -> `connected` 状态通知。Engine state 保持 `StateConnected` 不变。

### PR-6: Smart Routing

两种路由模式：
- **global**（`rule.global=true`）：所有流量走 proxy
- **smart**（`rule.global=false`）：GeoIP 分流，国内 direct，国外 proxy

规则优先级：`RuleConfig > RuleMode > URL query rule= > default "global"`

## Technical Decisions

### TD-1: Bridge transformStatus() 映射

Daemon/Engine 的状态值不直接透传到 webapp。每个 bridge 必须实现 `transformStatus()`：

| 原始状态 | 映射后 | 说明 |
|---------|--------|------|
| `stopped` | `disconnected` | daemon 用 stopped |
| `disconnected` + error | `error` | bridge 合成 |
| `connected_at` | `startAt` | 字段重命名 |
| `retrying` | 补全 | 来自 error 响应 |

详见 [dashboard-vpn-ui.md](dashboard-vpn-ui.md) TD3。

### TD-2: Tauri IPC 替代直接 HTTP

WebView（`localhost:14580`）无法 fetch daemon（`localhost:1777`）——跨端口无 CORS。解决方案：Tauri IPC `invoke('daemon_exec')` -> Rust 侧用 reqwest 调用 daemon API。

### TD-3: 移动端 QUIC 内存限制

iOS Network Extension 有 ~50MB 内存限制。`MobileQUICConfig()` 将 QUIC 窗口缩半（Stream 4MB / Connection 8MB），控制 QUIC 开销在 ~8MB 内。

## Key Files

### k2app Integration

| File | Description |
|------|-------------|
| `webapp/src/core/k2-bridge.ts` | `getK2()`, `waitForK2()` -- 获取 `window._k2` |
| `webapp/src/core/tauri-k2.ts` | Tauri bridge: `_k2.run()` via IPC `invoke('daemon_exec')`, `transformStatus()` |
| `webapp/src/core/capacitor-k2.ts` | Capacitor bridge: K2Plugin -> `_k2.run()` + `_platform`, `transformStatus()` |
| `webapp/src/services/control-types.ts` | `ServiceState`, `StatusResponseData`, `ClientConfig` TS types |
| `desktop/src-tauri/src/daemon.rs` | Rust `daemon_exec` IPC handler: reqwest -> daemon HTTP API |
| `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift` | iOS native bridge: NEVPNManager -> gomobile Engine |
| `mobile/plugins/k2-plugin/android/src/main/java/.../K2Plugin.kt` | Android native bridge |

### k2 Core (submodule, reference only)

| File | Description |
|------|-------------|
| `k2/daemon/daemon.go` | Daemon HTTP API server: 4 actions, auto-reconnect, state persistence |
| `k2/engine/engine.go` | Engine: unified lifecycle manager, Start/Stop/OnNetworkChanged |
| `k2/mobile/mobile.go` | gomobile adapter: simple-type API surface |
| `k2/config/config.go` | ClientConfig/ServerConfig: universal config contract |
| `k2/wire/transport.go` | TransportManager: QUIC-preferred + TCP-WS fallback, Resettable |
| `k2/core/tunnel.go` | ClientTunnel: L4 proxy, Router, bidirectional pipe |

## Acceptance Criteria

### Daemon API Integration

- [x] Tauri IPC `daemon_exec` correctly proxies all 4 actions to daemon HTTP API
- [x] CORS on daemon allows `tauri://localhost` and `https://tauri.localhost`
- [x] `status` response includes `config` field with active ClientConfig when connected
- [x] Auto-reconnect on daemon restart (saved config, within 1h)

### gomobile Integration

- [x] `mobile.Engine.Start(configJSON, fd, dataDir)` parses JSON and delegates to engine
- [x] `EventHandler` bridge: mobile interface -> engine interface transparent passthrough
- [x] `OnNetworkChanged()` emits `reconnecting` -> resets connections -> emits `connected`
- [x] `StatusJSON()` returns JSON-serialized status for K2Plugin

### Bridge transformStatus()

- [x] Daemon `"stopped"` mapped to webapp `"disconnected"`
- [x] `disconnected + error` synthesized to `"error"` state
- [x] `connected_at` remapped to `startAt` (Unix seconds)
- [x] Both Tauri and Capacitor bridges implement transformStatus()

### ClientConfig Contract

- [x] Webapp assembles minimal config (server + rule.global), Go fills defaults
- [x] Go snake_case keys remapped to JS camelCase at native bridge boundary
- [x] Rule mode priority chain works: RuleConfig > RuleMode > URL query > default
