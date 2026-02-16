# Feature: Mobile VPN — iOS

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | mobile-vpn-ios                           |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-14                               |
| Updated   | 2026-02-17                               |

## Version History

| Version | Date       | Summary                                                        |
|---------|------------|----------------------------------------------------------------|
| v1      | 2026-02-14 | Initial: K2Plugin.swift + NE 双进程 + gomobile xcframework     |

## Overview

iOS VPN 全栈实现：K2Plugin.swift（Capacitor 插件）+ PacketTunnelExtension（NE 进程运行
gomobile Engine）+ 签名/entitlements + xcframework 构建 + CI/CD 到 App Store。

核心架构决策是 NE 双进程模型——App 进程运行 Capacitor/Webapp，NE 进程运行 Go Engine，
两者通过 NEVPNManager IPC + App Group UserDefaults 通信。

前置依赖：mobile-webapp-bridge（NativeVpnClient + K2Plugin TS 定义）

## Product Requirements

- PR1: K2Plugin.swift 实现完整 Capacitor 插件方法（checkReady, connect, disconnect, getStatus, getVersion, getUDID, getConfig, setRuleMode）
- PR2: PacketTunnelExtension 在独立 NE 进程运行 gomobile Engine
- PR3: NE 通过 `setTunnelNetworkSettings` 完成后获取 TUN fd（非之前）
- PR4: App Group UserDefaults 用于 NE ↔ App 共享状态（wireUrl, vpnError）
- PR5: `NEVPNStatusDidChange` 作为唯一状态真相源（不在 UserDefaults 写独立状态）
- PR6: 错误传播链：NE 写 vpnError → cancelTunnelWithError → 系统通知 → K2Plugin 读 App Group
- PR7: IPv6 默认路由捕获，防止 DNS 泄漏（Engine 丢弃 IPv6 包）
- PR8: `cancelTunnelWithError()` 在每个 NE 错误/断连时必须调用
- PR9: gomobile bind 生成 K2Mobile.xcframework，复制到 Xcode 项目
- PR10: CI 自动构建并上传到 App Store Connect

## Technical Decisions

### TD1: NE 双进程架构

```
App Process                          NE Process (sandbox)
┌─────────────────────┐              ┌─────────────────────┐
│ Capacitor + Webapp  │              │ PacketTunnelProvider │
│ K2Plugin.swift      │──NEVPNMgr──→│ gomobile Engine      │
│   NEVPNStatusDidChange ←──system──│   EventHandler       │
│   App Group ←────────────────────→│   App Group          │
└─────────────────────┘              └─────────────────────┘
```

App 进程不运行 Go Engine。K2Plugin 通过 NEVPNManager API 控制 NE：
- `startVPNTunnel(options:)` 传递 wireUrl
- `sendProviderMessage("status")` RPC 查询状态（5s 超时，fallback 到 NEVPNStatus 映射）
- `connection.stopVPNTunnel()` 停止

### TD2: TUN fd 获取时序

**必须** 在 `setTunnelNetworkSettings` completion 回调之后获取 fd：
```swift
setTunnelNetworkSettings(settings) { error in
    // 只有在这里才能安全获取 fd
    let fd = self.packetFlow.value(forKey: "socket") as! Int32
    engine.start(wireUrl, fd: fd, dataDir: dataDir)
}
```
在 completion 之前获取会得到无效 fd。

### TD3: cancelTunnelWithError 必须调用

NE 每次错误或断连都必须调用 `cancelTunnelWithError()`，否则系统不知道隧道已失败，
App 进程不会收到 `NEVPNStatusDidChange` 通知。

### TD4: gomobile Swift API

gomobile 生成的 Swift 方法使用 `throws` 模式，不是 NSError out-parameter：
```swift
do {
    try engine.start(wireUrl, fd: fd, dataDir: dataDir)
} catch {
    // handle error
}
```

### TD5: 签名与证书

| Item | Value |
|------|-------|
| Bundle ID (app) | `io.kaitu` |
| Bundle ID (NE) | `io.kaitu.PacketTunnelExtension` |
| App Group | `group.io.kaitu` |
| Team | Wordgate LLC (NJT954Q3RH) |
| Min iOS | 16.0 |
| 签名 | 复用旧 kaitu Apple Developer 账号现有证书和 provisioning profiles |

### TD6: Podfile 配置

```ruby
platform :ios, '16.0'
use_frameworks! :linkage => :static

target 'App' do
  # Capacitor pods (自动生成)
  pod 'K2Plugin', :path => '../../plugins/k2-plugin'
end

target 'PacketTunnelExtension' do
  use_frameworks! :linkage => :static
  # NE 不需要 Capacitor — 只需 gomobile xcframework
end
```

## Acceptance Criteria

- AC1: PacketTunnelExtension 用系统提供的 fd 启动 Engine
- AC2: handleAppMessage 路由 "status" 到 Engine.StatusJSON()
- AC3: NEVPNStatusDidChange 事件传播到 webapp
- AC4: App Group UserDefaults 用于 NE ↔ App 共享状态
- AC5: Codesign 有效，entitlements 正确（NE + App Group，device + simulator 分开）
- AC6: NE extension Info.plist 有 CFBundleExecutable 和 CFBundleVersion
- AC7: `gomobile bind -target=ios` 生成 K2Mobile.xcframework
- AC8: `xcodebuild archive` 生成 .xcarchive
- AC9: CI 构建并上传到 App Store Connect

## Deployment & CI/CD

构建流程：
```
gomobile bind -target=ios → K2Mobile.xcframework
  → cp 到 mobile/ios/App/
  → cap sync ios
  → xcodebuild archive → .xcarchive
  → xcodebuild export → .ipa
  → xcrun altool upload → App Store Connect
```

CI (`.github/workflows/build-mobile.yml` iOS job)：
- Runner: macos-latest
- 签名: secrets 中的 Apple 证书 + API key
- 触发: v* tag push 或手动 dispatch
- 产物: .xcarchive + .ipa artifact，自动上传 App Store Connect

## Testing Strategy

- Manual on-device testing: Primary validation — NE dual-process model requires real device (simulator lacks NE support)
- `xcrun devicectl device install app` for real device deployment
- PacketTunnelProvider lifecycle verified via Console.app (NE process logs)
- K2Plugin.swift tested via `debug.html` page (mobile-debug feature)
- Xcode build verification: `xcodebuild archive` must succeed with valid codesign
- CI validation: `build-mobile.yml` iOS job must produce uploadable .ipa

## Key Files

| File | Lines | Role |
|------|-------|------|
| `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift` | ~455 | Capacitor 插件（VPN 控制 + 更新） |
| `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift` | ~133 | NE 进程：Engine 生命周期 + 事件桥接 |
| `mobile/ios/App/App/AppDelegate.swift` | ~11 | 最小 AppDelegate |
| `mobile/ios/App/App/App.entitlements` | — | NE + App Group (device) |
| `mobile/ios/App/App/App.simulator.entitlements` | — | App Group only (simulator) |
| `mobile/ios/App/PacketTunnelExtension/PacketTunnelExtension.entitlements` | — | NE + App Group |
| `mobile/ios/App/Podfile` | — | iOS 16.0, static linking |
| `scripts/build-mobile-ios.sh` | — | iOS 构建脚本 |
