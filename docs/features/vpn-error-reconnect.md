# Feature: VPN State Contract, Error Handling & Reconnection

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | vpn-error-reconnect                      |
| Version   | v2                                       |
| Status    | implemented                              |
| Created   | 2026-02-17                               |
| Updated   | 2026-02-17                               |

## Version History

| Version | Date       | Summary                                                          |
|---------|------------|------------------------------------------------------------------|
| v1      | 2026-02-17 | Initial: cross-platform error synthesis + reconnection           |
| v2      | 2026-02-17 | Rewrite: discovered full state contract breakage across all platforms |

## Overview

全平台 VPN 状态合约修复 + 错误处理 + 网络重连。

根本问题：webapp 定义了 6 种 `ServiceState`，但两个后端（daemon、engine）只产出 3 种，
且名字不一致（daemon 用 `"stopped"`，engine 用 `"disconnected"`）。两个 bridge 层
（Tauri、Capacitor）的转换行为也不对称——Tauri 纯透传，Capacitor 做了部分转换但缺少
错误合成。结果是全平台的 `isDisconnected`、`isError` 等派生状态均有问题。

## Problem

### P0: 状态合约断裂

```
后端实际产出              Webapp ServiceState 类型        实际匹配
──────────────────        ────────────────────────        ──────────
Daemon:  "stopped"        "disconnected"                  ✗ 不匹配
Engine:  "disconnected"   "disconnected"                  ✓
Both:    "connecting"     "connecting"                     ✓
Both:    "connected"      "connected"                      ✓
Nobody:                   "reconnecting"                   从未产出
Nobody:                   "error"                          从未产出
UI only:                  "disconnecting"                  仅乐观状态
```

**桌面端影响：** daemon 返回 `state: "stopped"` → Tauri bridge 纯透传 → VPN Store
收到 `"stopped"` → `isDisconnected = ("stopped" === "disconnected")` → **永远 false**。

桌面 app 表面能用，是因为各组件的 `default`/`else` 分支意外兜底了 `"stopped"` 这个
未知值。但这导致：
- `isDisconnected && !hasTunnelSelected` 的按钮禁用逻辑失效（按钮永远可点击）
- `handleToggleConnection` 中 `!isDisconnected` 永远为 true → 断开状态下点击走断开逻辑

**移动端影响：** engine 返回 `state: "disconnected"` → `isDisconnected` 正确为 true。
但连接失败时 `state` 仍是 `"disconnected"` 而非 `"error"` → 错误信息丢失。

### P1: 两个 Bridge 行为不对称

| | Tauri Bridge | Capacitor Bridge |
|--|-------------|-----------------|
| 有 `transformStatus()`？ | 否，纯透传 | 是 |
| 状态归一化？ | 否（`"stopped"` 原样传出） | 部分（`state` 透传，`error` 包装为 `ControlError`） |
| 错误合成？ | 否 | 否（`state` 保持 `"disconnected"`） |
| `retrying` | 后端无此字段，不存在 | 硬编码 `false` |
| `networkAvailable` | 后端无此字段，不存在 | 硬编码 `true` |
| `running` | 后端有此字段？取决于 daemon | 合成 `state === 'connecting' \|\| 'connected'` |

### P2: 网络切换隧道静默死亡

WiFi→4G → socket 路由失效 → QUIC 30s keepalive 超时 → `QUICClient.conn` 缓存死连接
→ 所有新 stream 复用死连接 → 隧道事实死亡但 engine 仍报 `"connected"`。

所有平台均受影响，移动端更频繁。

### P3: Dashboard error 交互缺失

`handleToggleConnection` 没有 `error` 状态分支。

## Solution

### Layer 0: 统一状态合约

**定义：** Bridge 层是 webapp 与后端之间的合约翻译层。所有 bridge 必须输出统一的
`StatusResponseData`，后端的原始 state 值不得穿透到 webapp。

#### 状态归属表（修复后）

| ServiceState | 来源 | 产出者 |
|-------------|------|--------|
| `disconnected` | 后端 | Daemon `"stopped"` → bridge 归一化; Engine `"disconnected"` → 直传 |
| `connecting` | 后端 | 两个后端均产出 |
| `connected` | 后端 | 两个后端均产出 |
| `error` | Bridge 合成 | `disconnected + lastError` → bridge 合成为 `"error"` |
| `reconnecting` | Bridge 合成 | 未来：wire 自愈期间由 engine EventHandler 产出，bridge 传递 |
| `disconnecting` | UI 乐观状态 | `setOptimisticState('disconnecting')` — 仅存在于 VPN Store `localState` |

**规则：`reconnecting` 和 `disconnecting` 永远不会从后端直接产出。**
- `reconnecting`：由 engine 的 `EventHandler.OnStateChange("reconnecting")` 信号产出，
  bridge 传递给 webapp。这是一个瞬态信号（wire 重建中），不是 engine 的持久 state。
- `disconnecting`：纯前端乐观状态。后端没有此概念（`doDown` 是同步的）。

#### Tauri Bridge: 添加 `transformStatus()`

```ts
// tauri-k2.ts — 新增
function transformStatus(raw: any): StatusResponseData {
  // State normalization: daemon uses "stopped", webapp expects "disconnected"
  let state: string = raw.state ?? 'disconnected';
  if (state === 'stopped') {
    state = 'disconnected';
  }

  // Error synthesis: disconnected + error → error
  let error: ControlError | undefined;
  if (raw.error) {
    error = { code: 570, message: raw.error };
    if (state === 'disconnected') {
      state = 'error';
    }
  }

  // connected_at → startAt (Unix seconds)
  let startAt: number | undefined;
  if (raw.connected_at) {
    startAt = Math.floor(new Date(raw.connected_at).getTime() / 1000);
  }

  return {
    state: state as ServiceState,
    running: state === 'connecting' || state === 'connected',
    networkAvailable: true,
    startAt,
    error,
    retrying: false,
  };
}
```

然后在 `run()` 的 `status` 路径中使用：

```ts
run: async <T = any>(action: string, params?: any): Promise<SResponse<T>> => {
  const response = await invoke<ServiceResponse>('daemon_exec', { action, params: params ?? null });
  if (action === 'status' && response.code === 0 && response.data) {
    response.data = transformStatus(response.data);
  }
  return { code: response.code, message: response.message, data: response.data as T };
}
```

#### Capacitor Bridge: 补全 `transformStatus()`

```ts
// capacitor-k2.ts — 修改现有函数
function transformStatus(raw: any): StatusResponseData {
  let state: string = raw.state ?? 'disconnected';

  let error: ControlError | undefined;
  if (raw.error) {
    error = { code: 570, message: raw.error };
    // Error synthesis: disconnected + error → error
    if (state === 'disconnected') {
      state = 'error';
    }
  }

  let startAt: number | undefined;
  if (raw.connectedAt) {
    startAt = Math.floor(new Date(raw.connectedAt).getTime() / 1000);
  }

  return {
    state: state as ServiceState,
    running: state === 'connecting' || state === 'connected',
    networkAvailable: true,
    startAt,
    error,
    retrying: false,
  };
}
```

变更点（vs 当前）：新增 `if (state === 'disconnected') { state = 'error'; }` 合成。

#### Error 清除时机

- 用户重连：`_k2.run('up')` → engine 进入 `"connecting"` → `state` 不再是 disconnected → error 自然消失
- 用户断开：`_k2.run('down')` → engine `Stop()` 清除 `lastError` → `state = "disconnected"`, no error → 正常
- Wire 自愈成功：engine 回到 `"connected"` → error 消失
- Wire 自愈失败：engine `fail()` → `"disconnected" + lastError` → bridge 合成 `"error"`

### Layer 1: Wire Self-Healing (Go engine, 所有平台)

#### 1a. `OnNetworkChanged()` API

`k2/engine/engine.go` 新增方法：

```go
func (e *Engine) OnNetworkChanged() {
    e.mu.Lock()
    defer e.mu.Unlock()
    if e.state != StateConnected {
        return
    }
    e.handler.OnStateChange("reconnecting")  // 瞬态信号，不改变 e.state
    e.wire.ResetConnections()                 // 清除缓存的死连接
    e.handler.OnStateChange("connected")      // wire lazy reconnect，报告恢复
}
```

`k2/mobile/mobile.go` 导出：

```go
func (e *Engine) OnNetworkChanged() {
    e.inner.OnNetworkChanged()
}
```

#### 1b. Wire `ResetConnections()`

`QUICClient` / `TCPWSClient`：
- 关闭 `c.conn` / `c.session`
- 置 nil → 下次 `connect()` lazy 重建
- TUN fd 不受影响（内核接口，不绑定物理网络）

### Layer 2: Platform NetworkCallback

#### Android — ConnectivityManager.NetworkCallback

```kotlin
// K2VpnService.kt
private var networkCallback: ConnectivityManager.NetworkCallback? = null

private fun registerNetworkCallback() {
    val cm = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
    val cb = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) {
            Log.d(TAG, "Network available, triggering reconnect")
            engine?.onNetworkChanged()
        }
    }
    val request = NetworkRequest.Builder()
        .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
        .build()
    cm.registerNetworkCallback(request, cb)
    networkCallback = cb
}

private fun unregisterNetworkCallback() {
    networkCallback?.let {
        val cm = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        cm.unregisterNetworkCallback(it)
    }
    networkCallback = null
}
```

在 `startVpn()` 的 `engine?.start()` 成功后调用 `registerNetworkCallback()`，
在 `stopVpn()` 中调用 `unregisterNetworkCallback()`。

#### iOS — NWPathMonitor

```swift
// PacketTunnelProvider.swift
private let pathMonitor = NWPathMonitor()

func startMonitoringNetwork() {
    pathMonitor.pathUpdateHandler = { [weak self] path in
        if path.status == .satisfied {
            self?.engine?.onNetworkChanged()
        }
    }
    pathMonitor.start(queue: .global(qos: .utility))
}

func stopMonitoringNetwork() {
    pathMonitor.cancel()
}
```

NE 进程中运行（PacketTunnelProvider），与 app 进程独立。

#### Desktop

不动。daemon 已有 auto-reconnect 机制（`tryAutoReconnect`，基于持久化 state）。
未来可利用 sing-tun `DefaultInterfaceMonitor`，但不在本 feature 范围。

### Layer 3: Webapp Dashboard Error UX

#### 3a. `handleToggleConnection` 补全 error 分支

```ts
const handleToggleConnection = useCallback(async () => {
  // Guard: no tunnel selected
  if ((isDisconnected || isError) && !activeTunnelInfo.domain) return;

  try {
    if (isError && !isRetrying) {
      // Error state: engine is already disconnected, reconnect directly
      setOptimisticState('connecting');
      const config = assembleConfig();
      await window._k2.run('up', config);
    } else if (!isDisconnected) {
      // Connected/connecting/reconnecting/retrying: disconnect
      setOptimisticState('disconnecting');
      await window._k2.run('down');
    } else {
      // Disconnected: connect
      setOptimisticState('connecting');
      const config = assembleConfig();
      await window._k2.run('up', config);
    }
  } catch (err) {
    console.error('Connection operation failed', err);
    setOptimisticState(null);
  }
}, [...]);
```

注意 guard 条件也要加 `isError`：error 状态下未选服务器也不能操作。

#### 3b. ConnectionButton — 已有 error 路径，无需修改

Bridge 修复后自动生效：
- `isError = true` → error 视觉
- `statusText` → `t('common:status.error')`
- 按钮可点击 → 触发 3a 的 error 分支

#### 3c. VPN Store — 无需修改

`computeDerivedState()` 的所有逻辑是正确的，问题在数据源。bridge 修复后：
- `isDisconnected` — 桌面端从 false 修正为 true（`"stopped"` 归一化为 `"disconnected"`）
- `isError` — 全平台从永远 false 修正为正确触发
- `shouldDebounce` — 当 `reconnecting` 信号产出后自动激活
- `isRetrying` — 当前 `false` 是正确的（无自动重试），未来 wire 自愈后可更新

## Scope

### In Scope

- [ ] Tauri bridge: 新增 `transformStatus()`（`stopped→disconnected`、error 合成）
- [ ] Capacitor bridge: `transformStatus()` 补全 error 合成
- [ ] Dashboard `handleToggleConnection` error 分支
- [ ] Dashboard guard 条件修复（`isError` 加入判断）
- [ ] Go engine `OnNetworkChanged()` + gomobile 导出
- [ ] Wire `ResetConnections()`（QUICClient + TCPWSClient）
- [ ] Android `K2VpnService` NetworkCallback
- [ ] iOS `PacketTunnelProvider` NWPathMonitor
- [ ] Tauri bridge 测试更新（`"stopped"` → `"disconnected"` 归一化验证）
- [ ] Capacitor bridge 测试更新（error 合成验证）

### Out of Scope

- Desktop sing-tun `DefaultInterfaceMonitor`（维持 daemon auto-reconnect）
- Go engine `StateError` 持久状态（bridge 合成已解决短期需求）
- `retrying` / `networkAvailable` 真实数据流（等 wire 自愈 + NetworkCallback 后第二期）
- 自动重连退避策略
- ConnectionButton error 文案优化（UX polish）

## Architecture Decisions

| Decision | Rationale |
|----------|-----------|
| Bridge 必须有 `transformStatus()`，禁止纯透传 | 后端 state 值不等于 webapp state 值。`"stopped"` ≠ `"disconnected"`。Bridge 是合约翻译层。 |
| 两个 bridge 使用相同的合成规则 | 统一状态合约。webapp 不应关心运行在哪个平台。 |
| `error` 由 bridge 合成，不改 engine | k2/ 是 read-only submodule，且 engine 的 3 态设计是正确的（简单、确定）。复杂性由 bridge 承担。 |
| `reconnecting` 是瞬态信号，不是持久状态 | Engine 内部 state 不变（仍是 `connected`），只通过 EventHandler 发出信号。Bridge 传递给 webapp。 |
| `disconnecting` 仅为乐观状态 | 后端 `doDown` 是同步的，无需 `disconnecting` 中间态。UI 用乐观状态提供即时反馈。 |
| 桌面端不做 wire 自愈 | Daemon 已有 auto-reconnect + state persistence。投入产出比不高。 |

## Acceptance Criteria

### AC1: 桌面端 `isDisconnected` 修正
- Given: daemon 返回 `state: "stopped"`
- When: VPN Store 计算派生状态
- Then: `isDisconnected` 为 true（不再是 false）

### AC2: 桌面端无 tunnel 时按钮禁用
- Given: 桌面端，未选择服务器
- When: VPN 处于断开状态
- Then: 连接按钮 disabled（之前因 `isDisconnected` 为 false 而失效）

### AC3: 移动端连接失败显示 error
- Given: Android/iOS 上连接 VPN
- When: 连接失败
- Then: ConnectionButton 显示 error 视觉 + 错误信息

### AC4: Error 状态点击重连
- Given: ConnectionButton 处于 error 状态
- When: 用户点击
- Then: 直接重连（不先 disconnect），按钮进入 connecting

### AC5: 成功连接后清除 error
- Given: error 状态
- When: 重连成功
- Then: 状态变为 connected，无错误信息

### AC6: 手动断开不触发 error
- Given: VPN 已连接
- When: 用户手动断开
- Then: 状态变为 disconnected（非 error）

### AC7: Android 网络切换自动重连
- Given: VPN 已连接
- When: WiFi→4G 切换
- Then: Wire 自动重建，连接恢复

### AC8: iOS 网络切换自动重连
- Given: VPN 已连接
- When: WiFi→4G 切换
- Then: NE 进程触发 wire 重连，VPN 保持连接

### AC9: 两个 Bridge 输出一致
- Given: 同样的后端原始数据（state + error）
- When: 经过各自 `transformStatus()`
- Then: 输出的 `StatusResponseData` 结构和语义一致

## Dependencies

| Dependency | Impact |
|-----------|--------|
| k2/ submodule | `OnNetworkChanged()` + wire `ResetConnections()` — 需要 k2 repo 修改 |
| gomobile bind | `OnNetworkChanged()` 导出给 iOS/Android |
| K2Plugin (Swift/Kotlin) | 调用 `engine.onNetworkChanged()` |

## Risk & Mitigation

| Risk | Mitigation |
|------|------------|
| Tauri bridge 加 `transformStatus()` 影响桌面端 | AC1/AC2 测试覆盖。`"stopped"→"disconnected"` 是语义修正，不改变用户可见行为（else 兜底已是 disconnected 表现）|
| Wire reset 期间丢包 | TUN buffer 缓冲数秒。重建应 <2s |
| NetworkCallback 频繁触发 | Debounce 500ms |
| `reconnecting` 瞬态信号时序 | 如果 wire rebuild 极快（<50ms），webapp 可能看不到 reconnecting。这是可接受的——用户无感知就是最好的体验 |
