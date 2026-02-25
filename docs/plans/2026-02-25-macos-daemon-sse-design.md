# macOS Daemon Mode + SSE Event-Driven Status

Date: 2026-02-25
Branch: feat/macos-ne-system-extension
Status: Design approved, pending implementation plan

## Problem

1. macOS 当前硬编码为 NE (Network Extension) 模式，无法使用 daemon 模式
2. webapp 使用 2s 轮询获取 VPN 状态，延迟高且无法区分 service 可达性和 VPN 状态
3. k2 submodule EventHandler 接口已从 `OnStateChange`+`OnError` 变为 `OnStatus(statusJSON)`，PacketTunnelProvider.swift 需要迁移

## Design

### 1. macOS 双构建模式 — Cargo Feature Flag

编译期 feature flag 控制 macOS 使用 daemon 还是 NE 模式。

```toml
# desktop/src-tauri/Cargo.toml
[features]
default = []           # daemon 模式（同 Windows/Linux）
ne-mode = []           # Network Extension 模式
```

条件编译门控：`cfg(target_os = "macos")` → `cfg(all(target_os = "macos", feature = "ne-mode"))`

影响文件：
- `service.rs` — `daemon_exec`, `ensure_service_running`, `get_udid`, `admin_reinstall_service`
- `ne.rs` — 整个 `mod macos` 只在 `ne-mode` 下编译
- `main.rs` — NE callback 注册、NE 相关 setup

Makefile 双 target：
- `build-macos` — daemon 模式（默认，同 Win/Linux）
- `build-macos-sysext` — NE 模式（`--features ne-mode`）

构建产物差异：

| | daemon 模式 | NE 模式 |
|---|---|---|
| k2 sidecar binary | 有 | 无 |
| KaituTunnel.appex | 无 | 有 |
| libk2_ne_helper.a | 不链接 | 链接 |
| K2MobileMacOS.xcframework | 不需要 | 需要 |
| 签名复杂度 | 低 | 高 |

### 2. Event-Driven Status 架构

#### 2.1 两个独立关注点

| 关注点 | 含义 | 事件 |
|--------|------|------|
| **Service 可达性** | daemon 进程是否在运行 | `onServiceStateChange(available: boolean)` |
| **VPN 状态** | 隧道连接状态 | `onStatusChange(status: StatusResponseData)` |

#### 2.2 IK2Vpn 接口扩展

```typescript
// types/kaitu-core.ts
interface IK2Vpn {
  run<T = any>(action: string, params?: any): Promise<SResponse<T>>;

  // Service 可达性事件
  onServiceStateChange?(callback: (available: boolean) => void): () => void;

  // VPN 状态事件（实时推送）
  onStatusChange?(callback: (status: StatusResponseData) => void): () => void;
}
```

两个事件方法均为可选（`?`）。standalone 模式不实现 → 退化为 2s 轮询。

#### 2.3 Rust 侧：SSE Client → Tauri Event

新建 `desktop/src-tauri/src/status_stream.rs`。

daemon 模式下 Rust 维护一个到 `GET /api/events` 的 SSE 长连接：
- SSE 连接成功 → emit `service-state-changed { available: true }`
- SSE 收到 `event: status` → emit `vpn-status-changed { ...engine.Status }`
- SSE 断开 → emit `service-state-changed { available: false }` → 3s 后重连
- Heartbeat（daemon 每 15s 发送 `: heartbeat`）保持连接活性

NE 模式下：
- Service 可达性 = NE 配置已安装（`ensure_ne_installed()` 成功后恒为 true）
- VPN 状态 = `ne_state_callback` → `k2ne_status()` 补全完整 Status → emit event

#### 2.4 Tauri Event 名称

| Event | Payload | 来源 |
|-------|---------|------|
| `service-state-changed` | `{ available: boolean }` | SSE 连接状态 / NE 配置状态 |
| `vpn-status-changed` | engine.Status JSON | SSE event / NE callback |

#### 2.5 Bridge 层 (tauri-k2.ts)

```typescript
onServiceStateChange: (callback) => {
  let unlisten: (() => void) | null = null;
  listen<{ available: boolean }>('service-state-changed', (e) => {
    callback(e.payload.available);
  }).then(fn => { unlisten = fn; });
  return () => { unlisten?.(); };
},

onStatusChange: (callback) => {
  let unlisten: (() => void) | null = null;
  listen<any>('vpn-status-changed', (e) => {
    callback(transformStatus(e.payload));
  }).then(fn => { unlisten = fn; });
  return () => { unlisten?.(); };
},
```

#### 2.6 Webapp 消费层

新模块 `core/status-source.ts` 替代 `core/polling.ts`：

```typescript
export function useStatusSource(options) {
  useEffect(() => {
    // 桌面/移动：event-driven，无轮询
    if (window._k2?.onStatusChange && window._k2?.onServiceStateChange) {
      const unsubStatus = window._k2.onStatusChange(onStatusChange);
      const unsubService = window._k2.onServiceStateChange(onServiceStateChange);

      // 弥合订阅前空窗期：一次性主动查询
      window._k2.run('status').then(resp => {
        if (resp.code === 0 && resp.data) onStatusChange(resp.data);
      });

      return () => { unsubStatus(); unsubService(); };
    }

    // Standalone/web：退化为 2s 轮询
    return startPollingFallback(options);
  }, []);
}
```

#### 2.7 数据流总览

```
daemon 模式:
  k2 daemon SSE /api/events
    → Rust reqwest SSE client (status_stream.rs)
      → 连接成功: emit service-state-changed(true)
      → status event: emit vpn-status-changed(status)
      → 连接断开: emit service-state-changed(false), 3s 重连
    → Tauri event → JS listen → transformStatus → store

NE 模式:
  engine OnStatus → appext eventBridge → gomobile → Swift PacketTunnelProvider
    → NE state callback → ne.rs → k2ne_status() 补全
      → emit vpn-status-changed(full_status)
    → service-state-changed: ensure_ne_installed 后恒 true

Standalone (web):
  无 event 支持 → 退化为 2s 轮询
    → _k2.run('status') 成功 = service 可达 + VPN 状态
    → _k2.run('status') 失败 = service 不可达
```

### 3. EventHandler 迁移 (PacketTunnelProvider.swift)

k2 gomobile `AppextEventHandlerProtocol` 已变：
- 旧：`onStateChange(_ state: String?)` + `onError(_ message: String?)`
- 新：`onStatus(_ statusJSON: String?)`

EventBridge 迁移要点：
- `onStatus` 原子投递 state+error，消除旧版双回调 race（`onError` + `onStateChange("disconnected")` 双重 `cancelTunnelWithError`）
- 解析 statusJSON 为 `EngineStatus { state, error: { code, message }? }`
- `state == "disconnected" && error != nil` → 写 App Group + cancelTunnelWithError
- `state == "disconnected" && error == nil` → 正常断开 cancelTunnelWithError(nil)
- 其他 state → NSLog（transient states）

`handleAppMessage` fallback: `"stopped"` → `"disconnected"`

### 4. 完整变更清单

#### P0 — macOS daemon 模式恢复 + SSE

| # | 文件 | 变更 |
|---|------|------|
| 1 | `Cargo.toml` | 添加 `ne-mode` feature |
| 2 | `service.rs` | `cfg(target_os = "macos")` → `cfg(all(target_os = "macos", feature = "ne-mode"))` 所有 macOS 特化分支 |
| 3 | `ne.rs` | 同上条件编译门控 |
| 4 | `main.rs` | NE callback/setup 改为 `cfg(feature = "ne-mode")`；daemon 模式启动 SSE listener |
| 5 | `status_stream.rs` (新) | Rust SSE client → Tauri event (`service-state-changed` + `vpn-status-changed`) |
| 6 | `Makefile` | `build-macos`（daemon 默认）+ `build-macos-sysext`（NE） |
| 7 | `build-macos.sh` | `--ne-mode` 参数支持，传递 `--features ne-mode` |

#### P1 — Event-Driven Webapp + EventHandler 迁移

| # | 文件 | 变更 |
|---|------|------|
| 8 | `types/kaitu-core.ts` | IK2Vpn 添加 `onServiceStateChange?` + `onStatusChange?` |
| 9 | `tauri-k2.ts` | 实现 `onServiceStateChange` + `onStatusChange`（listen Tauri event） |
| 10 | `core/status-source.ts` (新) | event-driven 状态源，退化为轮询 |
| 11 | `core/polling.ts` | 保留 `pollStatusOnce()` 导出，`useStatusPolling` 标记 deprecated |
| 12 | `KaituTunnel/PacketTunnelProvider.swift` | EventBridge: `onStateChange`+`onError` → `onStatus(statusJSON)` |
| 13 | 同上 | `handleAppMessage` "status" fallback: `"stopped"` → `"disconnected"` |
| 14 | `tauri-k2.ts` | `transformStatus` 注释更新（k2 >= f7e1655 不再发送 "stopped"） |
| 15 | `ne.rs` | NE callback emit 统一为 `vpn-status-changed`，payload 用完整 Status JSON |

#### P2 — 移动端（单独分支）

| # | 文件 | 变更 |
|---|------|------|
| 16 | `mobile/ios/.../PacketTunnelProvider.swift` | EventBridge 同 #12 迁移 |
| 17 | `mobile/android/.../K2VpnService.kt` | `onStateChange`+`onError` → `onStatus(statusJSON)` |
| 18 | `mobile/plugins/.../K2Plugin.kt` | 适配新接口 |
| 19 | `capacitor-k2.ts` | 实现 `onServiceStateChange` + `onStatusChange` |

### 5. 不变的部分

- `_k2.run('status')` 保留（初始状态查询 + 手动刷新 + standalone 退化）
- `ensure_service_running` 保留（Rust 侧启动时检查，daemon 模式同 Win/Linux）
- `transformStatus()` 保留（bridge 层标准化，仅注释更新）
- `pollStatusOnce()` 保留（手动刷新场景）
- `AuthGate` 保留（startup 门控，用 `_k2.run('status')` 检查）
