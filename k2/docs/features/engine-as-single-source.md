# Feature: Engine as Single Source of Truth

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | engine-as-single-source                  |
| Version   | v1                                       |
| Status    | draft                                    |
| Created   | 2026-02-23                               |
| Updated   | 2026-02-23                               |

## Version History

| Version | Date       | Summary                                                |
|---------|------------|--------------------------------------------------------|
| v1      | 2026-02-23 | Initial: unify daemon/mobile around engine state machine |

## Product Requirements

- Engine 是所有平台的唯一隧道生命周期管理者，不存在并行状态机 (v1)
- 所有平台（Desktop/iOS/Android）使用统一的状态名称 (v1)
- Daemon 退化为 Engine 的 HTTP 薄壳，不再独立跟踪 state/error/connectedAt (v1)
- 现有 HTTP API 契约保持向后兼容 (v1)
- DNS hijack、网络切换、PID 监控等平台特有行为不受影响 (v1)

## Background

### 现状问题

Engine (`engine/engine.go`) 设计为统一的隧道生命周期管理器，具备完整的状态机、EventHandler 事件系统、错误分类。但 Daemon 没有使用这些能力：

```
Mobile ✓:  Native App → mobile.Engine → engine.Engine (EventHandler 注册, engine 管状态)
Desktop ✗: CLI → daemon.Daemon → engine.Engine (EventHandler 未注册, daemon 自管状态)
```

**两套并行状态机：**

| 维度 | Daemon | Engine |
|------|--------|--------|
| 状态名 | `stopped` / `connecting` / `connected` | `disconnected` / `connecting` / `connected` |
| 错误 | `daemon.lastError` | `engine.lastError` |
| 时间戳 | `daemon.connectedAt` | `engine.connectedAt` |
| 状态查询 | `daemon.statusInfo()` | `engine.StatusJSON()` |

**后果：**
1. Engine 异步失败（Start 返回后）时 daemon 不知道——状态不同步
2. Engine 的 `reconnecting` 事件（OnNetworkChanged 触发）daemon 无感知
3. 状态名不统一（`stopped` vs `disconnected`），前端需要适配两套
4. 新增 engine 状态/事件需要在 daemon 中重复实现

### 各平台运行周期对比

```
┌─────────────────────────────────────────────────────┐
│ Desktop TUN (k2 daemon as system service)           │
│                                                     │
│ Lifecycle owner: launchd/systemd                    │
│ TUN FD: -1 (engine 自创建 utun0)                    │
│ DNS hijack: ✓ dnsHandler 拦截 port 53               │
│ Anti-loop: DNSExclude 排除 DNS/server IP 的 TUN 路由  │
│ DirectDial: ✓ 绑定物理网卡绕过 TUN                   │
│ NetworkMonitor: ✓ sing-tun DefaultInterfaceMonitor  │
│ Auto-reconnect: ✓ 持久化状态 <1h 自动重连            │
│ PID monitor: ✓ 父进程退出自动断开                    │
│ Hot reload: ✓ SIGHUP / API reload                   │
│ Root: ✓ required                                    │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│ Desktop Proxy (k2 daemon, no TUN)                   │
│                                                     │
│ Lifecycle owner: launchd/systemd                    │
│ TUN FD: N/A (SOCKS5 on 127.0.0.1:1080)             │
│ DNS hijack: ✗ 无 port 53 访问                       │
│ DirectDial: N/A                                     │
│ NetworkMonitor: ✗                                   │
│ Root: ✗                                             │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│ iOS (NEPacketTunnelProvider)                         │
│                                                     │
│ Lifecycle owner: iOS NetworkExtension               │
│ TUN FD: >=0 (系统提供)                               │
│ DNS hijack: ✓ 同一 dnsHandler                       │
│ Anti-loop: 系统管 (不需 DNSExclude)                  │
│ DirectDial: ✗                                       │
│ NetworkMonitor: ✗ (NWPathMonitor 直调 OnNetworkChanged) │
│ Root: N/A (沙盒)                                    │
└─────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────┐
│ Android (VpnService)                                │
│                                                     │
│ Lifecycle owner: Android VpnService                 │
│ TUN FD: >=0 (VpnService.Builder.establish())        │
│ DNS hijack: ✓ 同一 dnsHandler                       │
│ Anti-loop: 系统管                                    │
│ DirectDial: ✗                                       │
│ NetworkMonitor: ✗ (ConnectivityManager 直调 OnNetworkChanged) │
│ Root: N/A                                           │
└─────────────────────────────────────────────────────┘
```

**关键观察：** Engine.Start() 已经通过 `Config.FileDescriptor` 和 `Config.Mode` 正确分流所有平台。DNS hijack（`dnsHandler`）在所有 TUN 模式下共享。唯一的平台差异在 Config 组装层（DirectDial、NetworkMonitor、DNSExclude）——这些属于 daemon 调用侧的职责，不需要进入 engine 内部。

## Technical Decisions

### TD1: Daemon 注册为 Engine 的 EventHandler

Daemon 实现 `engine.EventHandler` 接口，在 engine.Start 前注册。Engine 的状态变更（connecting/connected/disconnected/reconnecting）和错误通过回调通知 daemon，daemon 不再自行管理状态。

### TD2: 统一状态名为 Engine 的命名

| Before (Daemon) | After (Engine) |
|-----------------|----------------|
| `stopped`       | `disconnected` |
| `connecting`    | `connecting`   |
| `connected`     | `connected`    |
| (无)            | `reconnecting` |

HTTP API 的 `state` 字段值从 `stopped` 变为 `disconnected`。这是 **breaking change**，但 k2app 前端在同一仓库内，可同步更新。

### TD3: Daemon statusInfo() 委托给 Engine

```go
// Before: daemon 从自己的字段构建
func (d *Daemon) statusInfo() map[string]any {
    return map[string]any{
        "state": d.state,              // daemon 自管
        "connected_at": d.connectedAt, // daemon 自管
        "error": d.lastError,          // daemon 自管
        "config": d.lastConfig,
    }
}

// After: daemon 合并 engine 状态 + daemon 独有字段
func (d *Daemon) statusInfo() map[string]any {
    info := d.engine.StatusMap()      // engine 提供 state, connected_at, uptime_seconds, error, wire_url
    info["config"] = d.lastConfig     // daemon 独有
    return info
}
```

Engine 新增 `StatusMap() map[string]any` 方法（返回与 StatusJSON 相同的结构但不序列化），避免 daemon 做 JSON 反序列化。

### TD4: 删除 Daemon 的并行状态字段

从 `Daemon` struct 中移除：
- `state string` → 用 `d.engine.Status()`
- `connectedAt time.Time` → engine 内部管理
- `lastError *engine.EngineError` → engine 内部管理

保留 daemon 独有字段：
- `lastConfig *config.ClientConfig` — auto-reconnect + reload + status 展示
- `monitorPID int` — PID 监控
- `engine *engine.Engine` — engine 实例引用
- `engineCtx/engineCnl` — PID 监控的 context

### TD5: Config 组装职责不变

Desktop 的 config 组装（DirectDial、NetworkMonitor、DNSExclude、RuleConfig）逻辑留在 daemon。Mobile 的 config 组装留在 mobile.Start()。这是调用侧的职责——不同平台确实需要不同的 config 字段，不应强行统一。

Engine.Config 保持现有设计：足够灵活接受所有平台的差异，但不关心差异的来源。

### TD6: mobile.Engine 保持现有 wrapper

gomobile 无法导出依赖包的接口/struct。mobile.Engine 和 mobile.EventHandler 的 1:1 wrapper 是 gomobile 的硬约束，不是设计缺陷。保留不动。

### TD7: doUp/doDown 简化

```go
// Before: daemon 手动设置 connecting/connected/stopped
func (d *Daemon) doUp(cfg *config.ClientConfig, pid int) error {
    d.closeTunnel()
    d.setStateLocked(StateConnecting)  // daemon 设状态
    eng, err := starter(engineCfg)     // engine 也设了一遍
    d.setStateLocked(StateConnected)   // daemon 再设
}

// After: daemon 只管 config 和 PID, engine 管状态
func (d *Daemon) doUp(cfg *config.ClientConfig, pid int) error {
    d.closeTunnel()
    d.lastConfig = cfg
    eng := engine.New()
    eng.SetEventHandler(d)             // daemon 收回调
    err := eng.Start(engineCfg)        // engine 自己 connecting → connected
    if err != nil { return err }
    d.engine = eng
    // PID monitor 用独立 context
}
```

## Design

### 架构分层定义

```
┌──────────────────────────────────────────────────────────────┐
│ Platform Shell (daemon/ mobile/)                             │
│                                                              │
│   职责: 平台入口 + Config 组装 + 平台特有功能                  │
│   daemon: HTTP API, service mgmt, auto-reconnect, reload     │
│   mobile: gomobile binding, EventHandler bridge              │
│                                                              │
│   规则: 不管隧道状态, 不管连接生命周期                          │
│         只做 Config 组装 + 把 engine 的状态/事件透传给调用方     │
├──────────────────────────────────────────────────────────────┤
│ Engine (engine/)                                             │
│                                                              │
│   职责: 唯一的隧道生命周期管理者 (single source of truth)      │
│   拥有: state machine, EventHandler, error classification     │
│   组装: wire → provider → tunnel → DNS middleware             │
│                                                              │
│   规则: 所有状态查询/变更必须经过 engine                        │
│         Platform Shell 通过 EventHandler 被动接收状态           │
│         engine 不知道也不关心调用者是 daemon 还是 mobile         │
├──────────────────────────────────────────────────────────────┤
│ Core (core/ + core/dns/)                                     │
│                                                              │
│   职责: L4 隧道数据面 + DNS pipeline                          │
│   无状态: 不管生命周期, 只处理连接和 DNS 查询                   │
│   ClientTunnel: Provider → Router → wire.Dialer              │
│   DNS Middleware: 13-step pipeline (cache/filter/route/upstream) │
├──────────────────────────────────────────────────────────────┤
│ Provider (provider/)                                         │
│                                                              │
│   职责: 平台 packet I/O 抽象 (TUN/Proxy → L4 connections)    │
│   TUN desktop: 自创建 utun0, gVisor stack                    │
│   TUN mobile: 平台提供 FD, gVisor stack                      │
│   Proxy: SOCKS5 on 127.0.0.1:1080, 无 TUN                   │
├──────────────────────────────────────────────────────────────┤
│ Wire (wire/)                                                 │
│                                                              │
│   职责: k2v5 协议传输 (Dialer/Listener)                      │
│   QUIC+H3 primary / TCP-WS+smux fallback / TransportManager │
└──────────────────────────────────────────────────────────────┘
```

### 资源归属规则

| 资源 | 创建者 | 使用者 | 销毁者 | 说明 |
|------|--------|--------|--------|------|
| TransportManager | engine | core (via Dialer) | engine.Stop() | engine 内部创建+销毁 |
| ClientTunnel | engine | engine | engine.Stop() (via provider.Close) | engine 内部创建+销毁 |
| DNS Middleware | engine | dnsHandler | engine.Stop() (via context cancel) | engine 内部创建+销毁 |
| Provider (TUN/Proxy) | engine | core (via Start) | engine.Stop() | engine 内部创建+销毁 |
| DirectDialer | **daemon** | engine (传入 Config) | **daemon 不管** (engine 不 close) | 调用方创建, 生命周期跟进程 |
| NetworkMonitor | **daemon** | engine (传入 Config) | **engine.Stop()** | daemon 创建, engine 销毁 |
| InterfaceMonitor | **daemon** (同 NetworkMonitor) | provider (type assertion) | **随 NetworkMonitor** | daemon 创建, 透传给 provider |

**原则：engine.Config 传入的外部资源，如果 engine 在 Start() 中接管了（存到 struct 字段），则由 engine 负责 Close。调用方创建后不应再操作该资源。**

### 架构对比

**Before:**
```
Desktop:
  CLI → HTTP → Daemon ──[builds config]──→ Engine.Start()
                 ├── state: stopped/connecting/connected  (daemon 自管)
                 ├── lastError  (daemon 自管)
                 └── connectedAt  (daemon 自管)
               Engine
                 ├── state: disconnected/connecting/connected  (engine 自管, daemon 不看)
                 ├── lastError  (engine 自管, daemon 不看)
                 └── EventHandler: nil  (未注册)

Mobile:
  Native → mobile.Engine → Engine.Start()
                             ├── state: disconnected/connecting/connected
                             └── EventHandler: eventBridge → native callback
```

**After:**
```
Desktop:
  CLI → HTTP → Daemon ──[builds config]──→ Engine.Start()
                 ├── lastConfig  (daemon 独有)
                 ├── monitorPID  (daemon 独有)
                 └── implements EventHandler  ←── Engine callback
               Engine
                 ├── state: disconnected/connecting/connected  (唯一 source of truth)
                 ├── lastError  (唯一 source of truth)
                 └── EventHandler: daemon  (已注册)

Mobile:
  Native → mobile.Engine → Engine.Start()  (不变)
                             └── EventHandler: eventBridge → native callback
```

### DNS Hijack 数据流（所有 TUN 平台共享，不受影响）

```
App DNS Query (port 53)
  → TUN device (utun0 / iOS FD / Android FD)
    → gVisor stack (IP → L4)
      → dnsHandler.HandleUDP(conn, "x.x.x.x:53")
        → port == 53? YES
          → dns.Middleware.HandleQuery(rawBytes)
            → 13-step pipeline (cache/filter/k2rule/upstream/SetTmpRule)
          → conn.WriteTo(response)
        → port != 53
          → ClientTunnel.HandleUDP → Router → direct/proxy
```

此流程在 engine.Start() 内部通过 `tunnel.StartWithHandler(ctx, &dnsHandler{...})` 组装。
Desktop TUN 和 Mobile TUN 走完全相同的代码路径。Proxy 模式走 `tunnel.Start(ctx)` 无 DNS 拦截。
本次重构不触碰此逻辑。

### 网络切换处理（不受影响）

```
Desktop:
  sing-tun InterfaceMonitor → daemon.NewNetworkMonitor
    → callback → engine.OnNetworkChanged()
      → EventHandler.OnStateChange("reconnecting")  ← daemon 现在能收到了
      → tm.Reset()
      → EventHandler.OnStateChange("connected")

Mobile:
  NWPathMonitor / ConnectivityManager
    → mobile.Engine.OnNetworkChanged()
      → engine.OnNetworkChanged()  (同上)
```

重构后，daemon 注册为 EventHandler，首次能感知 `reconnecting` 状态。

### 修改文件

| File | Change | Risk |
|------|--------|------|
| `engine/engine.go` | 新增 `StatusMap() map[string]any` | Low — 纯新增 |
| `daemon/daemon.go` | 删除 `state`/`connectedAt`/`lastError`，实现 EventHandler，简化 doUp/doDown/closeTunnel/statusInfo | **High** — 核心重构 |
| `daemon/api.go` | `handleStatus` 用新 statusInfo()，状态名从 stopped→disconnected | Medium |
| `daemon/state.go` | 持久化状态使用 engine 状态名 | Low |
| `daemon/reload.go` | `d.State()` 委托 engine | Low |
| `daemon/daemon_test.go` | 适配新状态名、mock 更新 | Medium |
| `daemon/api_test.go` | 适配新状态名 | Low |
| `daemon/reload_test.go` | 适配新状态名 | Low |
| `daemon/state_test.go` | 状态名更新 | Low |

**不动的文件：**
- `engine/config.go` — 不变
- `engine/dns_handler.go` — 不变
- `engine/event.go` — 不变
- `engine/error.go` — 不变
- `mobile/` — 不变
- `provider/` — 不变
- `core/` — 不变

### Daemon EventHandler 实现

```go
// daemon implements engine.EventHandler
func (d *Daemon) OnStateChange(state string) {
    slog.Info("state transition", "state", state)
    // Persist for auto-reconnect.
    if state == engine.StateConnected {
        saveState(persistedState{Config: d.lastConfig, State: state, Timestamp: time.Now()})
    } else if state == engine.StateDisconnected {
        saveState(persistedState{State: state, Timestamp: time.Now()})
    }
}

func (d *Daemon) OnError(message string) {
    slog.Error("engine error", "err", message)
}

func (d *Daemon) OnStats(txBytes, rxBytes int64) {
    // Future: expose via /metrics or WebSocket.
}
```

### 迁移策略：增量式

1. **Phase 1**: Engine 新增 `StatusMap()`
2. **Phase 2**: Daemon 实现 EventHandler，注册到 engine
3. **Phase 3**: Daemon 的 statusInfo() 委托给 engine.StatusMap()
4. **Phase 4**: 删除 daemon 并行状态字段
5. **Phase 5**: 更新 daemon 测试

每个 phase 可独立提交，编译+测试通过。

## Acceptance Criteria

- AC1: `daemon.Daemon` 不再有 `state string` 字段，状态查询委托给 `engine.Status()` (v1)
- AC2: `daemon.Daemon` 不再有 `connectedAt` 和 `lastError` 字段 (v1)
- AC3: Daemon 实现 `engine.EventHandler` 接口，在 engine.Start 前通过 `SetEventHandler(d)` 注册 (v1)
- AC4: HTTP API `status` 返回的 state 值为 `disconnected`/`connecting`/`connected`（不再是 `stopped`）(v1)
- AC5: HTTP API `status` 返回包含 engine 的 `connected_at`/`uptime_seconds`/`error`/`wire_url` + daemon 的 `config` (v1)
- AC6: 桌面 TUN DNS hijack 行为不变——port 53 经 dnsHandler → Middleware pipeline (v1)
- AC7: 网络切换时 daemon 能通过 EventHandler 收到 `reconnecting` → `connected` 状态变更 (v1)
- AC8: Auto-reconnect 逻辑不变（持久化状态, <1h 重连）(v1)
- AC9: PID 监控逻辑不变 (v1)
- AC10: Config hot-reload 逻辑不变 (v1)
- AC11: Mobile SDK 行为完全不变 (v1)
- AC12: `go build ./...` 编译通过 (v1)
- AC13: `go test ./...` 全部通过 (v1)

## Testing Strategy

- `go test ./engine/...` — 现有 engine lifecycle/EventHandler/DNS handler 测试覆盖核心逻辑 (v1)
- `go test ./daemon/...` — 更新 daemon 测试：验证 EventHandler 注册、状态委托、API 返回新状态名 (v1)
- `go test ./mobile/...` — 确认 mobile 测试不受影响 (v1)
- `go test -race ./...` — race detector 验证并发安全 (v1)
- E2E: `go test ./test/...` — 验证完整隧道建立/断开流程 (v1)

## Deployment & CI/CD

- 无新依赖 (v1)
- k2app 前端需同步更新：`stopped` → `disconnected` 状态映射 (v1)
- 持久化状态文件格式兼容：旧状态文件中 `state: "connected"` 仍可识别，`state: "stopped"` 在加载时映射为 `disconnected` (v1)
