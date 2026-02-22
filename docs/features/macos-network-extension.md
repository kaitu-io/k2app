# Feature: macOS Network Extension

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | macos-network-extension                  |
| Version   | v1                                       |
| Status    | draft                                    |
| Created   | 2026-02-23                               |
| Updated   | 2026-02-23 (v1.1)                        |
| Depends   | mobile-vpn-ios (implemented), tauri-desktop-bridge (implemented), network-change-reconnect (implemented) |

## Version History

| Version | Date       | Summary                                                        |
|---------|------------|----------------------------------------------------------------|
| v1      | 2026-02-23 | Initial: macOS NE 替代 daemon+utun，解决 DNS 劫持问题           |
| v1.1    | 2026-02-23 | Runtime flow review: 修复 9 个设计缺陷（响应格式、异步桥接、UDID、status fallback 等） |

## Overview

macOS 上 sing-tun 没有 DNS hijack（Windows 有 WFP、Linux 有 nftables，macOS 无等价 API）。当前 macOS 的 daemon+utun 方案无法强制 DNS 走 TUN，导致中国网络环境下被 DNS 污染的域名（youtube.com 等）返回假 IP，VPN 连接后仍无法访问。

sing-tun 的设计意图是 macOS 使用 Network Extension（sing-box-for-apple 的生产 macOS 客户端证实了这一点）。NE 通过 `NEDNSSettings(matchDomains: [""])` 系统级强制所有 DNS 查询走指定服务器，通过 `NEIPv4Route.default()` 一条路由捕获所有流量。

本 feature 将 macOS VPN 栈从 daemon+utun 迁移到 NE App Extension 架构，复用 iOS 已有的 gomobile engine + DNS middleware 路径。Windows/Linux 保持 daemon 架构不变。

## Product Requirements

- PR1: macOS 连接 VPN 后，DNS 查询走 NE 指定的代理 DNS 服务器，被污染域名（youtube.com）正确解析 (v1)
- PR2: macOS 不再需要 root 权限安装 launchd service (v1)
- PR3: macOS Tauri app 通过 NE App Extension 管理 VPN 隧道（install/start/stop/status） (v1)
- PR4: webapp 层零变更 - invoke('daemon_exec') 接口不变，Rust 层路由到 NE (v1)
- PR5: Windows/Linux 保持 daemon HTTP 架构不变 (v1)
- PR6: gomobile xcframework 复用 iOS 的 engine + DNS middleware 路径 (v1)

## Technical Decisions

### TD1: App Extension（非 System Extension）

macOS NE 有两种形态：

| 维度 | App Extension | System Extension |
|------|--------------|-----------------|
| 分发 | App Store + DMG | 仅直接分发 |
| 安装 | 自动随 app | 用户在 System Settings 审批 |
| 权限 | `packet-tunnel-provider` | `packet-tunnel-provider-systemextension` |

选择 App Extension：Kaitu 走 App Store 分发，System Extension 不被接受。sing-box 的 SFM（Mac App Store 版）同样用 App Extension。 (v1)

### TD2: macOS 去掉 daemon，Windows/Linux 保留

```
迁移后 (macOS):
  Tauri App -> NEVPNManager -> NE Process (sandbox) -> gomobile engine -> utun

不变 (Windows/Linux):
  Tauri App -> HTTP :1777 -> k2 daemon (root) -> engine -> TUN
```

macOS 不再需要 k2 binary sidecar、launchd plist、osascript 提权安装。NE 由系统管理。 (v1)

### TD3: Swift 静态库 + Rust C FFI bridge

Rust 不能直接调用 NEVPNManager（Objective-C/Swift API）。方案：

1. Swift 静态库 `libk2_ne_helper.a`，暴露 C 接口
2. Rust 通过 `extern "C"` 调用
3. Tauri IPC 命令 `daemon_exec` 在 macOS 上路由到 NE，其他平台保留 HTTP daemon

C 接口:
- `k2ne_install()` - 安装 NE VPN 配置（async: DispatchSemaphore 阻塞等待 saveToPreferences 完成）
- `k2ne_start(config_json)` - 启动 VPN 隧道（若无 NE profile 则自动 install）
- `k2ne_stop()` - 停止 VPN 隧道（立即返回，NE 异步停止）
- `k2ne_status()` - 查询状态 (返回 ServiceResponse JSON envelope)
- `k2ne_set_state_callback(cb)` - 注册状态变化回调
- `k2ne_reinstall()` - 移除旧 NE profile 并重新安装（对应 admin_reinstall_service）

关键实现约束：
- 所有 NE async API（saveToPreferences, loadAllFromPreferences, sendProviderMessage）使用 DispatchSemaphore 阻塞等待，因 C FFI 函数必须同步返回 (v1.1)
- `k2ne_status()` 当 NE 进程未运行时 sendProviderMessage 会失败，必须 fallback 到 NEVPNStatus 映射（参考 iOS K2Plugin.swift mapVPNStatus 模式） (v1.1)
- 所有返回值必须包装为 `{"code":0,"message":"ok","data":{...}}` ServiceResponse 格式，与 daemon HTTP 响应一致，webapp 零变更 (v1.1)

Rust 侧 `#[cfg(target_os = "macos")]` 条件编译，webapp 零变更。 (v1)

### TD4: gomobile xcframework macOS target

当前 `gomobile bind -target=ios` 生成 iOS-only xcframework。macOS NE 需要 macOS slice。

- `gomobile bind -target=macos,ios` 生成 universal xcframework
- NE 进程使用 `FileDescriptor >= 0` 路径（engine.go line 167-181），自动启用 DNS middleware
- engine.go 无需改动 - mobile path 已有完整 DNS middleware pipeline

build tag: `tun_desktop.go` 用 `(darwin && !ios)`。macOS NE 走 fd 路径，不调用 tun_desktop.go（fd > 0 时 provider 不创建 TUN）。需验证 gomobile macOS target 的 build tag 行为。 (v1)

### TD5: PacketTunnelProvider 复用 iOS 实现

iOS 的 PacketTunnelProvider.swift（133 行）几乎原样复用：

- 相同：`setTunnelNetworkSettings` -> 获取 fd -> `engine.Start(configJSON, fd, cfg)`
- 相同：NEDNSSettings 配置、NEIPv4Route.default() 路由
- 相同：EventBridge 事件桥接
- 区别：App Group ID（`group.io.kaitu.desktop` vs `group.io.kaitu`）
- 区别：网络监听 - macOS NE 可用 NWPathMonitor（和 iOS 相同） (v1)

### TD6: NE 状态观察 -> Tauri 事件

Swift helper 注册 `NEVPNStatusDidChange` 通知，通过 C callback -> Rust -> Tauri event 传播到 webapp。

webapp 的 2s 轮询仍保留作为 ground truth（与 network-change-reconnect 决策一致），NE 状态通知仅用于加速 UI 响应。 (v1)

### TD7: ServiceResponse 格式兼容 (v1.1)

daemon `statusInfo()` 返回 `{state:"stopped", error:{code,message}, connected_at, config}`，包裹在 `{code:0, message:"ok", data:{...}}` 信封中。

engine `StatusJSON()` 返回 `{state:"disconnected", error:{code,message}, connected_at, wire_url}` — 原始 JSON 字符串，无信封。

关键差异：
- daemon 用 `"stopped"`，engine 用 `"disconnected"` → NE 路径天然返回 `"disconnected"`，`tauri-k2.ts` 的 `transformStatus` 会做 identity mapping（无害）
- daemon 包含 `"config"`，engine 包含 `"wire_url"` → webapp 不依赖这两个字段的存在，无影响
- daemon 有 HTTP `{code, message, data}` 信封，engine 无 → **Swift helper 必须包装信封** (v1.1)

NE helper 的 `k2ne_status()` 必须：
1. 尝试 `sendProviderMessage("status")` 获取 engine StatusJSON
2. 成功：包装为 `{"code":0,"message":"ok","data": <engine json>}`
3. 失败（NE 未运行）：从 `NEVPNStatus` 映射状态，返回 `{"code":0,"message":"ok","data":{"state":"disconnected"}}` (v1.1)

### TD8: macOS UDID 来源 (v1.1)

daemon 有 `/api/device/udid` 端点返回 UDID。NE 模式无 daemon。

方案：Rust 侧在 macOS 上通过 `IOPlatformExpertDevice` 读取硬件 UUID（`IOPlatformUUID`），或使用 `sysctl kern.uuid`。不经过 NE 也不经过 daemon。`get_udid` IPC 命令在 macOS NE 模式下直接从 Rust 返回。 (v1.1)

### TD9: macOS 启动流程替换 (v1.1)

daemon 模式的启动流程：`ensure_service_running(version)` → ping → version check → osascript install

NE 模式的启动流程：
1. `k2ne_install()` — 检查是否已有 NE profile
2. 已有且有效 → 跳过
3. 无 profile → `saveToPreferences()` → 系统弹出 "允许 VPN 配置" 对话框
4. 无需 version check — appex 与 app 同版本，系统保证一致性

`ensure_service_running` 在 macOS 上替换为 `ensure_ne_installed`，不需要 version 参数。 (v1.1)

### TD10: version action 处理 (v1.1)

daemon `handleVersion()` 返回 `{version, go, os, arch}`。engine 无 version 接口。

NE 模式：`ne_action("version")` 直接从 Rust 侧返回 app version（`env!("CARGO_PKG_VERSION")`），不调用 NE 进程。因 appex 与 app 同版本，无 daemon 版本不匹配问题。 (v1.1)

## Design

### 架构图

```
+--------------------------------------------------+
|  macOS Tauri App (Kaitu.app)                      |
|  +-----------+  +---------------------------+    |
|  | WebView   |  | Rust main.rs              |    |
|  | (webapp)  |--| ne.rs (Swift FFI bridge)  |    |
|  +-----------+  +------------+--------------+    |
|                              | C FFI              |
|                 +------------v--------------+    |
|                 | libk2_ne_helper.a          |    |
|                 | NEVPNManagerWrapper.swift  |    |
|                 +------------+--------------+    |
+------------------------------+-------------------+
|  Kaitu.app/Contents/PlugIns/ |                    |
|  KaituTunnel.appex           | (App Extension)    |
|  +---------------------------v-----------------+ |
|  | PacketTunnelProvider.swift                   | |
|  |  +-- K2Mobile.xcframework (gomobile)        | |
|  |  +-- engine.Start(configJSON, fd, cfg)      | |
|  |  +-- NEDNSSettings(matchDomains: [""])      | |
|  |  +-- NEIPv4Route.default()                  | |
|  |  +-- NWPathMonitor -> engine.OnNetworkChanged| |
|  +---------------------------------------------+ |
+---------------------------------------------------+
```

### VPN 控制流

1. **App Startup** (v1.1):
   - macOS: `main.rs setup()` → `#[cfg(macos)]` → `ensure_ne_installed()` → Rust 调 `k2ne_install()` → Swift `loadAllFromPreferences` (DispatchSemaphore 阻塞) → 若无 profile → `saveToPreferences` → 系统弹窗 "允许 VPN 配置"
   - Windows/Linux: 保持 `ensure_service_running(version)` → ping → version check → admin install

2. **Install**: 首次启动 Tauri app → Swift helper → NETunnelProviderManager.saveToPreferences() → 系统弹窗让用户允许 VPN 配置

3. **Start**: webapp `_k2.run('up', config)` → `invoke('daemon_exec', {action:'up', params:{config, pid}})` → Rust `#[cfg(macos)]` → `ne_action("up", params)` → Swift `k2ne_start(configJSON)` → `loadAllFromPreferences` (semaphore) → `startVPNTunnel(options: ["configJSON": ...])` → NE process → PacketTunnelProvider.startTunnel() → gomobile engine.Start(configJSON, fd, cfg)
   - 注意: `pid` 参数在 NE 模式下忽略（NE 由系统管理生命周期，不需要 PID 监控） (v1.1)
   - 注意: 若无 NE profile，`k2ne_start()` 先自动调用 `k2ne_install()` (v1.1)

4. **Status**: webapp 2s polling → `_k2.run('status')` → Rust `ne_action("status")` → Swift `k2ne_status()`:
   - 尝试 `sendProviderMessage("status")` → NE handleAppMessage → engine.StatusJSON()
   - 成功: 包装为 `{code:0, message:"ok", data: <engine status>}` (v1.1)
   - 失败（NE 未运行）: fallback 到 `NEVPNStatus` 映射 → `{code:0, message:"ok", data: {state:"disconnected"}}` (v1.1)
   - `tauri-k2.ts` `transformStatus()` 处理两种情况均正常：engine 返回 `"disconnected"` 走 identity mapping

5. **Stop**: webapp `_k2.run('down')` → Rust `ne_action("down")` → Swift `k2ne_stop()` → `stopVPNTunnel()` → 立即返回 `{code:0, message:"disconnecting"}` → NE 异步停止 → polling 检测到 `disconnected`

6. **Version**: webapp → Rust `ne_action("version")` → 直接返回 `{code:0, data:{version: CARGO_PKG_VERSION, os:"macos"}}` — 不调 NE (v1.1)

7. **UDID**: webapp `_platform.getUdid()` → `invoke('get_udid')` → Rust `#[cfg(macos)]` → `sysctl kern.uuid` 或 IOPlatformUUID → 直接返回，不经 NE/daemon (v1.1)

8. **Reinstall**: webapp `_platform.reinstallService()` → `invoke('admin_reinstall_service')` → Rust `#[cfg(macos)]` → `k2ne_reinstall()` → 移除旧 NE profile → 重新安装。无 osascript/admin 密码 (v1.1)

### 文件结构

```
desktop/src-tauri/
  src/
    ne.rs                    # macOS NE Rust FFI bridge (新增)
    ne_helper/               # Swift 静态库项目 (新增)
      K2NEHelper.swift       # NEVPNManagerWrapper
      k2_ne_helper.h         # C 接口头文件
    service.rs               # 保留 Windows/Linux 的 HTTP daemon 逻辑
    main.rs                  # cfg 分支: macOS 用 ne.rs, 其他用 service.rs

desktop/src-tauri/
  KaituTunnel/               # macOS App Extension target (新增)
    PacketTunnelProvider.swift  # 复用 iOS 实现
    Info.plist
    KaituTunnel.entitlements

k2/
  mobile/mobile.go           # 不变 - gomobile wrapper
  engine/engine.go           # 不变 - FileDescriptor >= 0 路径已有 DNS middleware

scripts/
  build-macos.sh             # 需修改: 增加 gomobile macOS target + NE 签名
```

### 签名与 Entitlements

| Target | Bundle ID | Entitlements |
|--------|-----------|-------------|
| Kaitu.app | `io.kaitu.desktop` | `packet-tunnel-provider` + App Group |
| KaituTunnel.appex | `io.kaitu.desktop.tunnel` | `packet-tunnel-provider` + App Group |
| App Group | `group.io.kaitu.desktop` | - |

### 迁移路径

1. PKG 升级安装：preinstall 脚本检测旧 launchd service 并卸载
2. 首次启动新版：Tauri app 检测无 NE 配置 -> 自动 install -> 用户允许 VPN
3. Windows/Linux：完全不受影响，继续 daemon + HTTP 路径

## Acceptance Criteria

- AC1: macOS 连接 VPN 后，`dig youtube.com` 返回正确 IP（非污染 IP） (v1)
- AC2: macOS VPN 连接/断开不需要 root 密码提示 (v1)
- AC3: macOS NE App Extension 随 Kaitu.app 安装，无需额外步骤 (v1)
- AC4: webapp `_k2.run('up/down/status')` 在 macOS 上通过 NE 正常工作 (v1)
- AC5: macOS NEDNSSettings(matchDomains: [""]) 生效，所有 DNS 走代理 DNS 服务器 (v1)
- AC6: macOS NEIPv4Route.default() 捕获所有 IPv4 流量 (v1)
- AC7: gomobile `bind -target=macos` 生成可用的 xcframework (v1)
- AC8: NE 进程内 engine.Start() 走 FileDescriptor >= 0 路径，DNS middleware 启用 (v1)
- AC9: Windows/Linux 的 daemon HTTP 路径不受任何影响 (v1)
- AC10: Tauri build 成功产出包含 KaituTunnel.appex 的 DMG/PKG (v1)
- AC11: 升级安装自动清理旧 launchd service (v1)
- AC12: NEVPNStatusDidChange 状态变化通过 Tauri event 传播到 webapp (v1)
- AC13: NE 未运行时 `status` 查询 fallback 到 NEVPNStatus 映射，返回 `disconnected` (v1.1)
- AC14: NE helper 所有返回值包装为 ServiceResponse `{code, message, data}` 信封格式 (v1.1)
- AC15: macOS `get_udid` 不依赖 daemon，从系统 API 直接获取 (v1.1)
- AC16: macOS 启动时 `ensure_ne_installed` 替代 `ensure_service_running` (v1.1)

## Testing Strategy

- On-device manual testing: NE 需要真实 macOS 环境，simulator 不支持 NE (v1)
- DNS 验证: `dig youtube.com` / `dig google.com` 对比污染 IP vs 正确 IP (v1)
- Rust 单元测试: ne.rs 的 FFI bridge 逻辑（mock C 调用） (v1)
- Swift 单元测试: K2NEHelper.swift 的 NEVPNManager 封装 (v1)
- Tauri IPC 测试: daemon_exec 在 macOS 上正确路由到 NE (v1)
- 跨平台验证: Windows 构建不受 macOS NE 代码影响（cfg 隔离） (v1)
- 升级测试: 从旧版（daemon）升级到新版（NE）的迁移路径 (v1)

## Deployment & CI/CD

- `make build-macos`: 增加 gomobile macOS target 构建 + NE appex 签名 (v1)
- `release-desktop.yml`: CI 需要 macOS NE 签名证书（与现有证书同 team） (v1)
- PKG preinstall: 检测并卸载旧 launchd service (v1)
- Tauri config: tauri.conf.json 增加 NE appex bundle 配置 (v1)

## Key Files

| File | Role |
|------|------|
| `desktop/src-tauri/src/ne.rs` | Rust FFI bridge to Swift NE helper |
| `desktop/src-tauri/src/ne_helper/K2NEHelper.swift` | NEVPNManager wrapper (C FFI) |
| `desktop/src-tauri/KaituTunnel/PacketTunnelProvider.swift` | NE process: gomobile engine lifecycle |
| `desktop/src-tauri/KaituTunnel/Info.plist` | NE extension metadata |
| `desktop/src-tauri/src/service.rs` | Windows/Linux daemon HTTP (保留) |
| `desktop/src-tauri/src/main.rs` | cfg 平台分支 |
| `k2/mobile/mobile.go` | gomobile wrapper (不变) |
| `k2/engine/engine.go` | Engine with DNS middleware (不变) |
| `scripts/build-macos.sh` | 构建脚本 (修改) |
