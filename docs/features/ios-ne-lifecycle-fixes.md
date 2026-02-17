# Feature: iOS NE Lifecycle & Engine Safety Fixes

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | ios-ne-lifecycle-fixes                   |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-17                               |
| Updated   | 2026-02-18                               |

## Version History

| Version | Date       | Summary                                                          |
|---------|------------|------------------------------------------------------------------|
| v1      | 2026-02-17 | Initial: iOS NE 流程审计修复 + Go engine 竞态安全 |

## Overview

iOS VPN 连接流程全链路审计后发现的真实 bug 修复。涵盖两个层面：

1. **iOS NE 层**（K2Plugin.swift + PacketTunnelProvider.swift）：EventBridge 双重 cancel
   覆盖错误信息、disconnect 不等待 NE 拆除、状态映射不准确、VPN profile 无过滤
2. **Go engine 层**（engine.go）：Start()/Stop() 竞态窗口导致隧道泄露（桌面端为主）

前置依赖：mobile-vpn-ios (v1, implemented)
相关 spec：vpn-error-reconnect (v2, draft) — 状态合约和错误合成在该 spec 范围

## Problem

### P0: EventBridge 双重 `cancelTunnelWithError` 覆盖错误信息

`engine.fail()` 调用链：
```
fail() → handler.OnError(err.Error())     → Swift: cancelTunnelWithError(error)  // 第一次，有 error
      → setState(StateDisconnected)
        → handler.OnStateChange("disconnected") → Swift: cancelTunnelWithError(nil)  // 第二次，nil
```

第二次 `cancelTunnelWithError(nil)` 覆盖了第一次传递的 error 对象。iOS 系统收到的
最终 error 为 nil，K2Plugin 在 `.disconnected` 通知中从 `NEVPNConnection` 拿不到错误。

错误仍可通过 App Group `vpnError` key 读取（`OnError` 先写了），但 Apple 推荐的
`NEVPNConnection.error` 属性为 nil，不符合标准 NE 错误传播模式。

**置信度**: 95% — 代码路径确认

### P1: `disconnect()` 不等待 NE 拆除

```swift
@objc func disconnect(_ call: CAPPluginCall) {
    vpnManager?.connection.stopVPNTunnel()
    call.resolve()  // 立即 resolve，NE 还在关闭
}
```

`stopVPNTunnel()` 是异步的。JS 层收到成功响应后可能立即发起 `connect()`，此时
NE 进程仍在拆除中。`startVPNTunnel` 在 session 处于 `.disconnecting` 状态时
可能静默失败或产生未定义行为。

UI 层的乐观状态 + 按钮禁用降低了用户快速重连的概率，但 programmatic 调用（如自动
重连逻辑）没有此保护。

**置信度**: 90%

### P2: `mapVPNStatus` 将 `disconnecting` 映射为 `disconnected`

```swift
static func mapVPNStatus(_ status: NEVPNStatus) -> String {
    switch status {
    // ...
    default: return "disconnected"  // disconnecting、invalid、reasserting 全部走这里
    }
}
```

`NEVPNStatus.disconnecting` 是一个有意义的瞬态。映射为 `"disconnected"` 会导致：
- 断开过程中 UI 短暂显示"已断开"然后再变回真正断开
- `isDisconnected` 为 true → 连接按钮提前可用

**置信度**: 90%

### P3: `loadAllFromPreferences` 不过滤 bundle ID

```swift
NETunnelProviderManager.loadAllFromPreferences { managers, error in
    let manager = managers?.first ?? NETunnelProviderManager()
    // ...
}
```

如果用户安装了其他 VPN app（或 Kaitu 旧版本创建了不同 profile），`managers?.first`
可能返回错误的 VPN 配置。应过滤 `providerBundleIdentifier == "io.kaitu.PacketTunnelExtension"`。

**置信度**: 70%

### P4: Engine Start()/Stop() 竞态窗口（桌面端为主）

```go
func (e *Engine) Start(cfg Config) error {
    e.mu.Lock()
    e.setState(StateConnecting)
    e.mu.Unlock()
    // ← 长时间操作，e.cancel/e.prov 均为 nil →
    // Stop() 可进入，设 StateDisconnected，但无法 cancel Start()
    e.mu.Lock()
    e.cancel = cancel  // 才写入
    e.setState(StateConnected)
    e.mu.Unlock()
}
```

窗口期内 `Stop()` 看到 `state = connecting, cancel = nil`：
1. 跳过 cancel（nil）
2. 设置 `state = disconnected`
3. Start() 继续完成 → 写回 `state = connected`
4. 隧道运行中但用户认为已断开，且无法再停止

iOS NE 系统序列化 startTunnel/stopTunnel，此竞态在 iOS 上概率极低（15%）。
桌面 daemon HTTP handler 可并发，概率高（85%）。

**置信度**: iOS 15%, Desktop 85%

### P5: NE Settings 硬编码

```swift
let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: "10.0.0.1")
settings.ipv4Settings = NEIPv4Settings(addresses: ["10.0.0.2"], subnetMasks: ["255.255.255.0"])
settings.dnsSettings = NEDNSSettings(servers: ["1.1.1.1", "8.8.8.8"])
settings.mtu = 1400
```

IP、DNS、MTU 全部硬编码。当前所有服务器用相同隧道 IP 段，暂无实际问题。
但未来不同服务器可能需要不同 DNS 或 MTU（如自建 DNS、低 MTU 线路）。

**置信度**: 当前 30%，未来 70%

## Product Requirements

- PR1: 错误断连时 error 信息完整传播到主 App，不被 nil 覆盖 (v1)
- PR2: disconnect 操作等待 NE 实际拆除后再 resolve (v1)
- PR3: NE 状态映射覆盖 disconnecting 瞬态 (v1)
- PR4: VPN profile 加载过滤 bundle ID，避免误操作其他 VPN (v1)
- PR5: Engine Start/Stop 竞态安全（跨平台） (v1)
- PR6: NE settings 支持从 config 动态读取（前向兼容） (v1)

## Technical Decisions

### TD1: EventBridge `onStateChange` 不再调用 `cancelTunnelWithError`

**修复 P0**。`cancelTunnelWithError` 只在 `onError` 中调用（有 error 时）。
`onStateChange("disconnected")` 改为调用 `cancelTunnelWithError(nil)` **仅当没有
preceding error 时**（即正常 Stop 触发的 disconnect）。

实现方式：EventBridge 维护一个 `hasReportedError` 标记。`onError` 设置标记并
cancel(error)。`onStateChange("disconnected")` 检查标记：已设置则跳过（error 路径
已经 cancel 过了），未设置则 cancel(nil)（正常断开路径）。

```swift
class EventBridge: NSObject, MobileEventHandlerProtocol {
    weak var provider: PacketTunnelProvider?
    private var hasReportedError = false

    func onError(_ message: String?) {
        guard let message = message else { return }
        hasReportedError = true
        UserDefaults(suiteName: kAppGroup)?.set(message, forKey: "vpnError")
        let error = NSError(domain: "io.kaitu", code: 100,
                           userInfo: [NSLocalizedDescriptionKey: message])
        provider?.cancelTunnelWithError(error)
    }

    func onStateChange(_ state: String?) {
        guard let state = state else { return }
        if state == "disconnected" && !hasReportedError {
            provider?.cancelTunnelWithError(nil)
        }
        // Reset for next connection cycle
        if state == "connecting" {
            hasReportedError = false
        }
    }
}
```

**Why 不改 engine.go**: k2/ 是 read-only submodule，且 `fail()` 的行为（先 OnError
再 setState）是合理的 — 问题在 Swift 消费端如何处理两个回调。

### TD2: disconnect 等待 `.disconnected` 状态后 resolve

**修复 P1**。`disconnect()` 调用 `stopVPNTunnel()` 后，注册一次性
`NEVPNStatusDidChange` 观察者，等待 `.disconnected` 状态后再 `call.resolve()`。
设置 5s 超时兜底。

```swift
@objc func disconnect(_ call: CAPPluginCall) {
    guard let manager = vpnManager else {
        call.resolve()
        return
    }

    let observer = NotificationCenter.default.addObserver(
        forName: .NEVPNStatusDidChange,
        object: manager.connection,  // 限定到自己的 connection
        queue: .main
    ) { notification in
        guard let conn = notification.object as? NEVPNConnection,
              conn.status == .disconnected else { return }
        // observer 在 block 内被 capture → 需要外部引用来 remove
        call.resolve()
    }

    // Timeout fallback
    DispatchQueue.main.asyncAfter(deadline: .now() + 5) {
        NotificationCenter.default.removeObserver(observer)
        call.resolve()  // CAPPluginCall 多次 resolve 是安全的（只第一次生效）
    }

    manager.connection.stopVPNTunnel()
}
```

### TD3: mapVPNStatus 增加 disconnecting 映射

**修复 P2**。

```swift
static func mapVPNStatus(_ status: NEVPNStatus) -> String {
    switch status {
    case .connected: return "connected"
    case .connecting: return "connecting"
    case .disconnecting: return "disconnecting"
    case .disconnected: return "disconnected"
    case .reasserting: return "reconnecting"
    default: return "disconnected"
    }
}
```

webapp 的 `ServiceState` 已包含 `"disconnecting"` 和 `"reconnecting"`。
`reasserting` 是 iOS 系统的"VPN 重建中"状态，映射为 `"reconnecting"` 语义正确。

### TD4: loadAllFromPreferences 过滤 bundle ID

**修复 P3**。

```swift
private func loadVPNManager(completion: ((NETunnelProviderManager?) -> Void)? = nil) {
    NETunnelProviderManager.loadAllFromPreferences { [weak self] managers, error in
        let manager = managers?.first(where: {
            ($0.protocolConfiguration as? NETunnelProviderProtocol)?
                .providerBundleIdentifier == "io.kaitu.PacketTunnelExtension"
        }) ?? NETunnelProviderManager()
        self?.vpnManager = manager
        completion?(manager)
    }
}
```

### TD5: Engine Start 使用 context 取消保护

**修复 P4**。在 `setState(StateConnecting)` 时就创建 context 并保存 cancel，
使 Stop() 在任何时刻都能取消进行中的 Start()。Start() 的长时间操作检查 context。

```go
func (e *Engine) Start(cfg Config) error {
    e.mu.Lock()
    if e.state != StateDisconnected {
        e.mu.Unlock()
        return fmt.Errorf("engine: already started")
    }
    ctx, cancel := context.WithCancel(context.Background())
    e.cancel = cancel  // 立即保存，Stop() 随时可用
    e.wireUrl = cfg.WireURL
    e.lastError = ""
    e.setState(StateConnecting)
    e.mu.Unlock()

    // 长时间操作...
    wireCfg, err := wire.ParseURL(cfg.WireURL)
    if err != nil {
        cancel()
        return e.fail(err)
    }

    // 检查是否已被 Stop() 取消
    if ctx.Err() != nil {
        return fmt.Errorf("engine: cancelled during start")
    }

    // ... build transports, provider, tunnel ...

    if err := prov.Start(ctx, handler); err != nil {
        cancel()
        tm.Close()
        return e.fail(err)
    }

    e.mu.Lock()
    // 再次检查 — Stop() 可能在 prov.Start 期间取消了 context
    if ctx.Err() != nil {
        e.mu.Unlock()
        prov.Close()
        tm.Close()
        return fmt.Errorf("engine: cancelled during start")
    }
    e.tunnel = tunnel
    e.tm = tm
    e.prov = prov
    e.setState(StateConnected)
    e.mu.Unlock()
    return nil
}
```

`fail()` 也需要清理 `e.cancel`：
```go
func (e *Engine) fail(err error) error {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.lastError = err.Error()
    if e.cancel != nil {
        e.cancel()
        e.cancel = nil
    }
    if e.handler != nil {
        e.handler.OnError(err.Error())
    }
    e.setState(StateDisconnected)
    return err
}
```

**注意**: 此修改在 k2/ submodule 中。需要 k2 repo 单独提交。

### TD6: NE Settings 从 configJSON 解析（前向兼容）

**修复 P5**。从 configJSON 解析 tunnel settings，有值则用，无值则保持当前默认值。
这是非破坏性的前向兼容 — 现有 config 无此字段时行为不变。

```swift
struct TunnelSettings: Codable {
    var tunAddress: String?    // default: "10.0.0.2"
    var tunMask: String?       // default: "255.255.255.0"
    var dns: [String]?         // default: ["1.1.1.1", "8.8.8.8"]
    var mtu: Int?              // default: 1400
}

// In startTunnel:
let tunnelSettings: TunnelSettings? = parseTunnelSettings(from: configJSON)

let addr = tunnelSettings?.tunAddress ?? "10.0.0.2"
let mask = tunnelSettings?.tunMask ?? "255.255.255.0"
let dns = tunnelSettings?.dns ?? ["1.1.1.1", "8.8.8.8"]
let mtu = tunnelSettings?.mtu ?? 1400
```

Go 侧 `config.ClientConfig` 暂不添加 tunnel settings 字段 — 等真正需要
不同配置的服务器出现时再加。Swift 侧先做好解析准备。

### TD7: NEVPNStatusDidChange observer 限定 object

**附带修复**。当前 observer 使用 `object: nil`（接收所有 VPN connection 的事件），
改为 `object: manager.connection`（只接收自己的 VPN 事件）。

```swift
statusObserver = NotificationCenter.default.addObserver(
    forName: .NEVPNStatusDidChange,
    object: vpnManager?.connection,  // 限定到 Kaitu VPN connection
    queue: .main
) { [weak self] notification in
    // ...
}
```

## Scope

### In Scope

- [ ] TD1: EventBridge `hasReportedError` 标记，防止双重 cancel (PacketTunnelProvider.swift)
- [ ] TD2: disconnect() 等待 `.disconnected` + 5s 超时 (K2Plugin.swift)
- [ ] TD3: mapVPNStatus 增加 disconnecting/reasserting 映射 (K2Plugin.swift)
- [ ] TD4: loadAllFromPreferences 过滤 providerBundleIdentifier (K2Plugin.swift)
- [ ] TD5: Engine Start() 立即保存 cancel + context 检查 (k2/engine/engine.go)
- [ ] TD6: NE settings 从 configJSON 解析，默认值兜底 (PacketTunnelProvider.swift)
- [ ] TD7: NEVPNStatusDidChange observer 限定 object (K2Plugin.swift)

### Out of Scope

- Engine `fail()` 持锁调 handler "死锁" — 已验证不是真正死锁（cancelTunnelWithError 异步，defer Unlock 保证释放）
- `vpnManager` 同步问题 — Capacitor JS bridge 单线程序列化，实际竞争概率极低
- App Group configJSON 写入时序 — `providerConfiguration` 在 saveToPreferences 后已持久化，NE 重连有效
- Error code 分类（570 硬编码）— 属于 vpn-error-reconnect spec 范围
- vpnError 事件不上报 UI — 属于 vpn-error-reconnect spec 范围
- StateConnected 在 wire 握手前触发 — 设计取舍（lazy connect 减少 connecting 延迟）

## Acceptance Criteria

### AC1: 错误断连保留完整 error 信息
- Given: Engine 连接失败触发 `fail(error)`
- When: `onError` 和 `onStateChange("disconnected")` 依次被调用
- Then: `cancelTunnelWithError` 只调用一次（带 error），App Group `vpnError` 有值，K2Plugin 能读取到错误信息 (v1)

### AC2: 正常断开不触发 error cancel
- Given: 用户主动断开 VPN
- When: Engine `Stop()` → `onStateChange("disconnected")`
- Then: `cancelTunnelWithError(nil)` 被调用（正常断开），App Group 无 `vpnError` (v1)

### AC3: disconnect 等待 NE 拆除
- Given: VPN 已连接
- When: 调用 `disconnect()`
- Then: JS 层的 Promise 在 NE 实际到达 `.disconnected` 后才 resolve (v1)

### AC4: disconnect 超时兜底
- Given: NE 拆除超过 5 秒未完成
- When: 超时到达
- Then: 强制 resolve，不永远挂起 (v1)

### AC5: disconnecting 状态正确传播
- Given: VPN 正在断开中
- When: `getStatus()` 查询或 `NEVPNStatusDidChange` 事件到达
- Then: 返回 `state: "disconnecting"`（不是 `"disconnected"`） (v1)

### AC6: reasserting 映射为 reconnecting
- Given: iOS 系统检测到网络变化触发 VPN reasserting
- When: `NEVPNStatusDidChange` 报告 `.reasserting`
- Then: 映射为 `"reconnecting"` 传给 webapp (v1)

### AC7: VPN profile 过滤正确
- Given: 设备上有多个 NETunnelProviderManager（其他 VPN app）
- When: loadVPNManager 加载 profiles
- Then: 只选择 `providerBundleIdentifier == "io.kaitu.PacketTunnelExtension"` 的 profile (v1)

### AC8: Engine Stop 可取消进行中的 Start
- Given: Engine 正在 Start()（state = connecting）
- When: 调用 Stop()
- Then: Start() 被 context cancel 中断，不会写回 StateConnected，资源正确清理 (v1)

### AC9: NE settings 使用 config 值（如有）
- Given: configJSON 包含 tunnel settings（dns、mtu 等）
- When: PacketTunnelProvider 创建 NEPacketTunnelNetworkSettings
- Then: 使用 config 中的值；config 无此字段时使用默认值 (v1)

### AC10: NEVPNStatusDidChange 不接收其他 VPN 事件
- Given: 设备安装了其他 VPN app
- When: 其他 VPN app 状态变化
- Then: Kaitu K2Plugin 不触发 vpnStateChange 事件 (v1)

## Impact Analysis

### Affected Modules

| Module | Change | Risk |
|--------|--------|------|
| `PacketTunnelProvider.swift` | EventBridge 标记 + NE settings 解析 | Low — NE 进程独立，不影响主 App |
| `K2Plugin.swift` | disconnect 等待 + 状态映射 + profile 过滤 + observer 限定 | Medium — 核心 VPN 控制路径 |
| `k2/engine/engine.go` | Start() context 保护 + fail() cancel 清理 | Medium — 跨平台核心，需 k2 repo 提交 |

### Dependencies

| Dependency | Impact |
|-----------|--------|
| k2/ submodule | TD5 需要 engine.go 修改，k2 repo 单独 PR |
| gomobile bind | engine.go 修改后需重新 `gomobile bind` 生成 xcframework |
| vpn-error-reconnect spec | TD3 的 `disconnecting`/`reconnecting` 映射配合该 spec 的状态合约修复 |

## Testing Strategy

- PacketTunnelProvider EventBridge: 模拟 `fail()` 调用序列，验证 `cancelTunnelWithError` 只调用一次 — 手动设备测试 (v1)
- K2Plugin disconnect: 调用 disconnect 后验证 resolve 时序 — 手动设备测试 + debug.html (v1)
- mapVPNStatus: 可添加 Swift 单元测试验证所有 NEVPNStatus case 的映射 (v1)
- Engine Start/Stop race: `k2/engine/engine_test.go` 添加并发测试 — 一个 goroutine Start，另一个延迟 Stop，验证不泄露隧道 (v1)
- NE settings parsing: Swift 单元测试验证 JSON 解析 + 默认值兜底 (v1)
- 回归测试: 正常 connect/disconnect/reconnect 流程在 iOS 真机上验证 (v1)

## Key Files

| File | Role |
|------|------|
| `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift` | MODIFY: EventBridge 标记 + NE settings 动态化 |
| `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift` | MODIFY: disconnect 等待 + 状态映射 + profile 过滤 + observer 限定 |
| `k2/engine/engine.go` | MODIFY: Start() context 保护 + fail() cancel 清理 |
