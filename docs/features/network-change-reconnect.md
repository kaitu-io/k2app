# Feature: Network Change Detection & Reconnection Closure

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | network-change-reconnect                 |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-18                               |
| Updated   | 2026-02-18                               |
| Depends on | vpn-error-reconnect (implemented), config-driven-connect (implemented) |

## Version History

| Version | Date       | Summary                                                    |
|---------|------------|------------------------------------------------------------|
| v1      | 2026-02-18 | Initial: desktop sing-tun monitor + mobile event observability |

## Overview

`vpn-error-reconnect` 建立了跨平台状态合约和 wire 自愈机制（`OnNetworkChanged()` + `ResetConnections()`），
但审计发现 network change → reconnection 的闭环在所有平台上仍然断裂：

| Platform | Detection | Engine Call | Event Propagation | UI Visibility | Closed Loop? |
|----------|-----------|-------------|-------------------|---------------|--------------|
| Desktop  | Missing   | Never       | N/A               | N/A           | Broken       |
| iOS      | NWPathMonitor | OnNetworkChanged() | EventBridge drops reconnecting/connected | Unreachable | Half-broken |
| Android  | NetworkCallback | OnNetworkChanged() | JS event discarded (console.debug only)  | Unreachable | Last-mile broken |

**Root cause analysis:**

1. **Desktop**: daemon/engine 没有任何网络变化检测。QUIC 连接在网络切换后需要等 30s idle timeout 才被动恢复。
2. **iOS**: NE 进程内 `EventBridge.onStateChange` 只处理 `"connecting"` 和 `"disconnected"`，silently drop `"reconnecting"` 和 `"connected"`。
3. **Android**: `capacitor-k2.ts` 的 `vpnStateChange` listener 只做 `console.debug`，不更新 store。
4. **共性**: `reconnecting` 是微秒级同步事件，2s 轮询永远捕获不到。

**Architecture decision (via /word9f-scrum):**

经过对抗评估，决定 **不改变轮询架构**：
- Event-push hybrid 引入双通道时序一致性问题，复杂度不值得
- 轮询的自愈性（每 2s 拉取 ground truth）是被低估的资产
- 快速网络切换（<5s）用户无感知，不需要 reconnecting UI 闪现
- 长时间断连（>5s）会导致 error 状态，现有轮询能捕获

**本 feature 聚焦：补齐检测 + 修复可观测性（debug log），不引入事件驱动 store 更新。**

## Problem

### P0: Desktop 无网络变化检测

WiFi→Ethernet、VPN 网络路由变化、sleep/wake 后网络恢复 — daemon 的 engine 从不收到
`OnNetworkChanged()`。QUIC 连接缓存死连接，直到 `MaxIdleTimeout`（30s）后被动重建。
用户体验：网络切换后 VPN 隧道死亡 30 秒。

sing-tun v0.7.11 已提供完整的跨桌面平台 `NetworkUpdateMonitor` + `DefaultInterfaceMonitor`：
- macOS: `AF_ROUTE` socket
- Linux: netlink subscribe
- Windows: WinAPI `RegisterRouteChangeCallback` + `RegisterInterfaceChangeCallback`

k2 engine 从未接入这些 monitor。

### P1: iOS EventBridge 丢弃 reconnecting/connected

`PacketTunnelProvider.EventBridge.onStateChange` 只处理 `"connecting"` 和 `"disconnected"`。
当 `OnNetworkChanged()` 发出 `"reconnecting"` → `"connected"` 时，两个事件均被 silently drop。
虽然连接重建成功（功能正确），但没有任何日志记录，出问题时无法排查。

### P2: Android capacitor-k2.ts 丢弃 vpnStateChange 事件

`vpnStateChange` 事件从 Go engine → K2VpnService → K2Plugin → JS event 完整传递，
但 `capacitor-k2.ts` 的 listener 只做 `console.debug`。事件内容（state、时间戳）不被结构化记录。

### P3: Android 只监听 onAvailable，不监听 onLost

`K2VpnService.registerNetworkCallback()` 只 override `onAvailable`。
WiFi 断开且无 4G 时，`onNetworkChanged` 不被调用。Engine 保持 `connected`
但所有流量失败，直到新网络可用。增加 `onLost` 可以让 engine 提前清理。

## Solution

### Layer 0: Go Engine — NetworkChangeNotifier Interface (k2/ submodule)

**在 engine 包定义 interface，不引入 sing-tun 依赖：**

```go
// k2/engine/network.go (new file)
package engine

// NetworkChangeNotifier detects network interface changes and notifies the engine.
// Implementations are platform-specific: sing-tun DefaultInterfaceMonitor for desktop,
// NWPathMonitor (iOS) and ConnectivityManager (Android) via gomobile for mobile.
type NetworkChangeNotifier interface {
    // Start begins monitoring. Callback will be called when default interface changes.
    Start(callback func()) error
    // Close stops monitoring and releases resources.
    Close() error
}
```

**Engine.Config 增加可选字段：**

```go
// k2/engine/engine.go — Config struct
type Config struct {
    // ... existing fields ...
    NetworkMonitor NetworkChangeNotifier // optional, nil on mobile
}
```

**Engine 启动时注册 callback：**

```go
func (e *Engine) Start() error {
    // ... existing start logic ...

    // Wire up network monitor if provided
    if e.cfg.NetworkMonitor != nil {
        e.cfg.NetworkMonitor.Start(func() {
            e.OnNetworkChanged()
        })
    }
    return nil
}

func (e *Engine) Stop() {
    // ... existing stop logic ...
    if e.cfg.NetworkMonitor != nil {
        e.cfg.NetworkMonitor.Close()
    }
}
```

### Layer 1: Daemon — sing-tun Monitor Adapter (k2/ submodule)

**daemon 包实现 adapter，桥接 sing-tun → engine interface：**

```go
// k2/daemon/network_monitor.go (new file)
package daemon

import (
    "github.com/sagernet/sing-tun"
    "github.com/sagernet/sing/common/control"
    "k2/engine"
)

type singTunMonitor struct {
    networkMonitor  tun.NetworkUpdateMonitor
    interfaceMonitor tun.DefaultInterfaceMonitor
    callback         func()
}

func NewNetworkMonitor() (engine.NetworkChangeNotifier, tun.DefaultInterfaceMonitor, error) {
    netMon, err := tun.NewNetworkUpdateMonitor((*nopLogger)(nil))
    if err != nil {
        return nil, nil, err  // returns ErrInvalid on unsupported platforms
    }
    ifaceMon, err := tun.NewDefaultInterfaceMonitor(netMon, (*nopLogger)(nil), tun.DefaultInterfaceMonitorOptions{})
    if err != nil {
        netMon.Close()
        return nil, nil, err
    }
    m := &singTunMonitor{
        networkMonitor:   netMon,
        interfaceMonitor: ifaceMon,
    }
    // Return both: notifier for engine, ifaceMonitor for tun.Options
    return m, ifaceMon, nil
}

func (m *singTunMonitor) Start(callback func()) error {
    m.interfaceMonitor.RegisterCallback(func(defaultInterface *control.Interface, flags int) {
        if callback != nil {
            callback()
        }
    })
    if err := m.networkMonitor.Start(); err != nil {
        return err
    }
    return m.interfaceMonitor.Start()
}

func (m *singTunMonitor) Close() error {
    m.interfaceMonitor.Close()
    return m.networkMonitor.Close()
}
```

**daemon 通过 MonitorFactory 注入（testable pattern）：**

```go
// k2/daemon/daemon.go
type Daemon struct {
    EngineStarter  func(engine.Config) (*engine.Engine, error)
    MonitorFactory func() (engine.NetworkChangeNotifier, any, error)
}

// doUp uses factory (nil → defaultMonitorFactory → NewNetworkMonitor)
// Non-fatal on failure: logs warn, engine starts without monitor
```

**关键约束：`ifaceMonitor` 必须同时传给 engine callback 和 `tun.Options.InterfaceMonitor`。**
sing-tun 调用 `RegisterMyInterface(tunName)` 排除 TUN 自身接口变化，防止自触发。

### Layer 2: iOS EventBridge — Debug Logging (mobile/ios/)

**改 silently drop 为 debug log：**

```swift
// PacketTunnelProvider.swift — EventBridge.onStateChange
func onStateChange(_ state: String?) {
    guard let state = state else { return }
    if state == "connecting" {
        hasReportedError = false
    } else if state == "disconnected" {
        if hasReportedError { return }
        provider?.cancelTunnelWithError(nil)
    } else {
        // Log transient states for debug observability
        NSLog("[K2:NE] transient state: \(state)")
    }
}
```

不传递 reconnecting 到 App 进程，不影响 UI。仅添加日志用于排查。

### Layer 3: Android capacitor-k2.ts — Structured Debug Logging (webapp/)

**改 console.debug 为结构化日志：**

```typescript
// capacitor-k2.ts — vpnStateChange listener
K2Plugin.addListener('vpnStateChange', (event: any) => {
    console.debug('[K2:Capacitor] vpnStateChange:', event.state,
        event.connectedAt ? `connectedAt=${event.connectedAt}` : '');
});
```

不更新 VPN store。事件仅用于 debug log。Store 更新继续由 2s polling 驱动。

### Layer 4: Android K2VpnService — Add onLost Callback (mobile/android/)

```kotlin
// K2VpnService.kt — registerNetworkCallback
val callback = object : ConnectivityManager.NetworkCallback() {
    override fun onAvailable(network: Network) {
        pendingNetworkChange?.let { mainHandler.removeCallbacks(it) }
        val runnable = Runnable { engine?.onNetworkChanged() }
        pendingNetworkChange = runnable
        mainHandler.postDelayed(runnable, 500)
    }
    override fun onLost(network: Network) {
        Log.d(TAG, "Network lost, clearing cached connections")
        pendingNetworkChange?.let { mainHandler.removeCallbacks(it) }
        engine?.onNetworkChanged()  // Immediate, no debounce — clear dead connections fast
    }
}
```

`onLost` 不 debounce — 网络已断，立即清理死连接。下次 `onAvailable` 时 lazy dial 重建。

## Scope

### In Scope

- k2 engine: `NetworkChangeNotifier` interface + Config 字段 + lifecycle wiring
- k2 daemon: sing-tun monitor adapter + tun.Options.InterfaceMonitor 注入
- k2 tun_desktop.go: 接受 InterfaceMonitor 参数
- iOS EventBridge: reconnecting/connected 改为 NSLog
- Android capacitor-k2.ts: vpnStateChange 结构化 debug log
- Android K2VpnService: 增加 onLost callback

### Out of Scope

- Event-push hybrid 架构（scrum 评估已否决）
- reconnecting UI 显示（快速切换无需 UI 反馈）
- VPN store 事件驱动更新（保持 2s polling）
- retrying / networkAvailable 真实数据流（future consideration）
- OpenWrt 网络检测（sing-tun netlink 在 OpenWrt 可能受限，需单独验证）

## Technical Decisions

| Decision | Rationale | Source |
|----------|-----------|--------|
| Engine 定义 `NetworkChangeNotifier` interface，不直接依赖 sing-tun | 依赖边界：engine 包不应 import 第三方 tun 库。daemon 做适配。 | scrum #2, Platform Specialist |
| Monitor 和 tun.Options 共享同一实例 | sing-tun `RegisterMyInterface(tunName)` 排除 TUN 自身路由变化，防止自触发。分离实例会丢失此保护。 | scrum #2, Reliability Engineer |
| 保持 2s 轮询为唯一 UI 状态源 | Event-push hybrid 引入双通道时序一致性问题（debounce 逻辑为单源设计）。轮询自愈性（每 poll 拉 ground truth）是更简单可靠的方案。 | scrum #1, System Architect + Reliability Engineer |
| 不显示 reconnecting UI | 快速网络切换（<5s）用户无感知，闪现反而制造焦虑。长时间断连由 error 状态 + 轮询覆盖。 | scrum #1, UX Designer |
| Monitor 创建失败是 non-fatal | 某些环境（容器、特殊 Linux）可能不支持 route socket。降级到原有的被动超时恢复。 | v1 |
| iOS/Android 事件仅做 debug log，不进 store | 避免建成半吊子事件通道。未来需要事件架构时整体设计。 | scrum #1, System Architect |
| Android onLost 不 debounce | 网络已断，立即清理死连接比等待更优。onAvailable 保持 500ms debounce 防 flapping。 | v1 |

## Acceptance Criteria

### AC1: Desktop macOS 网络切换自动重连
- Given: macOS 上 VPN 已连接
- When: WiFi→Ethernet 切换（默认路由接口变化）
- Then: sing-tun monitor 检测到变化 → `engine.OnNetworkChanged()` → wire reset → 连接在 <3s 内恢复
- Verify: 2s polling 下一轮显示 `connected`（对比之前 30s 超时）

### AC2: Desktop Windows 网络切换自动重连
- Given: Windows 上 VPN 已连接
- When: 网络接口变化
- Then: 同 AC1

### AC3: Desktop Linux 网络切换自动重连
- Given: Linux 上 VPN 已连接
- When: 默认路由接口变化
- Then: 同 AC1

### AC4: Monitor 不自触发
- Given: sing-tun monitor 和 tun.Options.InterfaceMonitor 使用同一实例
- When: TUN 接口创建/路由设置
- Then: monitor 不触发 `OnNetworkChanged()`（TUN 已被 `RegisterMyInterface` 排除）

### AC5: Monitor 创建失败 graceful 降级
- Given: 平台不支持 network monitor（如 ErrInvalid）
- When: daemon 启动
- Then: 日志 warn，engine 正常工作，无 monitor callback。降级为 30s idle timeout 恢复。

### AC6: iOS EventBridge 日志可观测
- Given: iOS VPN 已连接
- When: 网络切换触发 `OnNetworkChanged()` → EventBridge 收到 `"reconnecting"` + `"connected"`
- Then: NSLog 输出 `[K2:NE] transient state: reconnecting` 和 `[K2:NE] transient state: connected`

### AC7: Android vpnStateChange 结构化日志
- Given: Android VPN 已连接
- When: 网络切换触发状态事件
- Then: JS console 输出包含 state 值和 connectedAt（如有）

### AC8: Android onLost 清理死连接
- Given: Android VPN 已连接
- When: WiFi 断开且无 4G
- Then: `onLost` 立即调用 `onNetworkChanged()`（无 debounce），清理 QUIC 死连接
- When: 4G 恢复
- Then: `onAvailable` 500ms debounce 后 `onNetworkChanged()`，lazy dial 重建连接

### AC9: 不引入事件驱动 store 更新
- Given: 任意平台网络切换
- When: 事件到达 JS（Android）或被 EventBridge 处理（iOS）
- Then: VPN store 不被事件直接更新。状态变更仅通过 2s polling 反映。

## Testing Strategy

### Go engine (k2/ submodule)
- Unit test: `NetworkChangeNotifier` mock → engine.Start() 注册 callback → mock 触发 → verify `OnNetworkChanged()` 被调用
- Unit test: engine.Config.NetworkMonitor = nil → engine 正常启动，无 panic
- Unit test: engine.Stop() 调用 monitor.Close()

### Daemon adapter
- Integration test: `NewNetworkMonitor()` 在 macOS/Linux/Windows 返回有效 monitor
- Integration test: monitor + tun.Options 使用同一 `DefaultInterfaceMonitor` 实例

### Mobile
- Manual test: iOS — 切换网络后查看设备 Console，确认 NSLog 输出
- Manual test: Android — 切换网络后查看 logcat / WebView console，确认事件日志
- Manual test: Android — 飞行模式开关后确认连接恢复

### Desktop E2E
- Manual test: macOS — WiFi→Ethernet 切换，观察 VPN 恢复时间（<3s vs 之前 30s）
- Manual test: Windows — 网络切换恢复

## Deployment & CI/CD

- k2 submodule PR: engine interface + daemon adapter + tun_desktop.go 修改
- k2app PR: iOS EventBridge log + Android capacitor-k2.ts log + K2VpnService onLost
- CI: existing `cargo test` + `yarn test` + `go test ./...` cover regressions
- gomobile rebind required after k2 submodule update (for Android onLost changes)

## Impact Analysis

| Module | Change | Scope |
|--------|--------|-------|
| k2/engine/ | New `NetworkChangeNotifier` interface + Config field + lifecycle | Small — additive, no existing behavior changed |
| k2/daemon/ | New `network_monitor.go` adapter + startup injection | Small — new file + 10 lines in daemon startup |
| k2/provider/tun_desktop.go | Accept `InterfaceMonitor` in Options | Minimal — 1 field added |
| mobile/ios/PacketTunnelExtension/ | EventBridge else branch: NSLog | Minimal — 1 line |
| webapp/src/services/capacitor-k2.ts | vpnStateChange log format | Minimal — 1 line |
| mobile/android/K2VpnService.kt | onLost callback in NetworkCallback | Small — 5 lines |

**Total scope: Small.** Majority of changes are in k2/ submodule (requires maintainer).
Webapp and mobile changes are minimal (log format only).

## Future Considerations

- **Event-push architecture**: If real-time stats (bandwidth, latency) are needed, re-evaluate event channel design holistically. Do not build incrementally on debug log listeners. (scrum #1)
- **OpenWrt network monitor**: sing-tun netlink works on standard Linux but may need testing on OpenWrt. Track as separate validation.
- **Sleep/wake detection**: sing-tun monitor handles route changes but doesn't explicitly detect sleep/wake cycles. macOS `NSWorkspace.willSleepNotification` and Windows `WM_POWERBROADCAST` may be needed for aggressive session cleanup.
