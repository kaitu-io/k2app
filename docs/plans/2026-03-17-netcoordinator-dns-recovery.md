# netCoordinator: 网络状态协调器 + DNS 错误归属修正

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 解决 iOS/Android WiFi 断开后 VPN 无法自动恢复的问题。引擎不知道网络断了 → 持续无效重连 → iOS 杀死 NE → WiFi 恢复时无人响应。

**Architecture:** 两个独立修复：

1. **netCoordinator**（核心）：engine 内部网络状态协调器，融合 sing-tun 接口变化信号 + iOS/Android 平台精确网络状态信号。网络断时停止无效工作，网络恢复时全面重连。
2. **删除 DNS→ReportWireError**（架构修正）：DNS 失败是症状不是诊断。只有 wire 层（proxy dial、recovery probe）才应报告 wire 错误。

**Tech Stack:** Go (engine), Swift (iOS NE), Kotlin (Android VpnService), gomobile

---

## 架构原则

1. **Wire error 通道只传 wire 诊断**：`ReportWireError` 只应被 wire 层调用（proxy dial 失败、probe handshake 失败）。DNS 失败是应用层症状，不是传输层诊断。
2. **NetEvent 是引擎的网络感知接口**：8 个原始字段，gomobile 兼容，携带平台丰富信息，为未来精细化处理预留空间。
3. **DNS 系统配置管理在 provider 层独立运行**：macOS `dnsOverride` 通过 SCDynamicStore 监控 DHCP 刷新并自动 reapply，与 netCoordinator 互不干扰。

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `k2/engine/netmon.go` | Create | NetEvent 结构体 + netCoordinator：信号融合、去抖、networkUp 状态机 |
| `k2/engine/netmon_test.go` | Create | netCoordinator 单元测试 |
| `k2/engine/engine.go` | Modify | 集成 netCoordinator：创建/销毁/路由信号、OnNetEvent 方法、onHealthCritical 检查 networkUp |
| `k2/engine/dns_handler.go` | Modify | 删除 ReportWireError 调用，保留 counter 作为内部指标 |
| `k2/appext/appext.go` | Modify | 导出 NotifyNetEvent(*engine.NetEvent) gomobile 方法 |
| `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift` | Modify | 添加 NWPathMonitor 发送 NetEvent |
| `mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt` | Modify | onAvailable/onLost 改调 notifyNetEvent |

---

## Task 1: NetEvent + netCoordinator 核心

**Files:**
- Create: `k2/engine/netmon.go`
- Create: `k2/engine/netmon_test.go`

- [ ] **Step 1.1: 写 netCoordinator 测试**

```go
// k2/engine/netmon_test.go
package engine

import (
	"sync/atomic"
	"testing"
)

type mockActions struct {
	reconnectCount atomic.Int32
	probeStopCount atomic.Int32
}

func (m *mockActions) doReconnect() { m.reconnectCount.Add(1) }
func (m *mockActions) stopProbe()   { m.probeStopCount.Add(1) }

func TestNetCoordinator_Unavailable_StopsReconnects(t *testing.T) {
	mock := &mockActions{}
	nc := newNetCoordinator(mock.doReconnect, mock.stopProbe)

	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "nwpath"})

	if nc.isNetworkUp() {
		t.Error("networkUp should be false")
	}
	if mock.probeStopCount.Load() != 1 {
		t.Error("probe should be stopped")
	}

	// Interface changed while down → no reconnect
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "singtun"})
	if mock.reconnectCount.Load() != 0 {
		t.Error("should not reconnect when network is down")
	}
}

func TestNetCoordinator_Available_TriggersReconnect(t *testing.T) {
	mock := &mockActions{}
	nc := newNetCoordinator(mock.doReconnect, mock.stopProbe)

	nc.handleEvent(&NetEvent{Signal: SignalUnavailable})
	nc.handleEvent(&NetEvent{Signal: SignalAvailable, InterfaceName: "en0", IsWifi: true})

	if !nc.isNetworkUp() {
		t.Error("networkUp should be true")
	}
	if mock.reconnectCount.Load() != 1 {
		t.Error("should reconnect on network recovery")
	}
}

func TestNetCoordinator_DuplicateAvailable_Ignored(t *testing.T) {
	mock := &mockActions{}
	nc := newNetCoordinator(mock.doReconnect, mock.stopProbe)

	// Network already up (default) — no-op
	nc.handleEvent(&NetEvent{Signal: SignalAvailable})
	if mock.reconnectCount.Load() != 0 {
		t.Error("duplicate available should not reconnect")
	}
}

func TestNetCoordinator_Changed_ReconnectsWhenUp(t *testing.T) {
	mock := &mockActions{}
	nc := newNetCoordinator(mock.doReconnect, mock.stopProbe)

	nc.handleEvent(&NetEvent{Signal: SignalChanged, InterfaceName: "pdp_ip0", IsCellular: true})

	if mock.reconnectCount.Load() != 1 {
		t.Error("interface change should trigger reconnect")
	}
}

func TestNetCoordinator_Changed_DebounceRapidFire(t *testing.T) {
	mock := &mockActions{}
	nc := newNetCoordinator(mock.doReconnect, mock.stopProbe)

	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "singtun"})
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "singtun"}) // rapid

	if mock.reconnectCount.Load() != 1 {
		t.Errorf("should debounce, got %d", mock.reconnectCount.Load())
	}
}

func TestNetCoordinator_DuplicateUnavailable_Ignored(t *testing.T) {
	mock := &mockActions{}
	nc := newNetCoordinator(mock.doReconnect, mock.stopProbe)

	nc.handleEvent(&NetEvent{Signal: SignalUnavailable})
	nc.handleEvent(&NetEvent{Signal: SignalUnavailable})

	if mock.probeStopCount.Load() != 1 {
		t.Error("duplicate unavailable should not stop probe again")
	}
}
```

- [ ] **Step 1.2: 运行测试确认 FAIL**

Run: `cd k2 && go test ./engine/ -run TestNetCoordinator -v`
Expected: FAIL — `NetEvent` not defined

- [ ] **Step 1.3: 实现 NetEvent + netCoordinator**

```go
// k2/engine/netmon.go
package engine

import (
	"log/slog"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// Signal constants for NetEvent.Signal.
const (
	SignalAvailable   = "available"   // platform: network is reachable
	SignalUnavailable = "unavailable" // platform: network is unreachable
	SignalChanged     = "changed"     // interface changed (sing-tun or platform)
)

const eventDebounce = 500 * time.Millisecond

// NetEvent describes a network state change from the platform layer.
// All fields are primitives for gomobile compatibility.
// Platform bridges (iOS NWPathMonitor, Android ConnectivityManager,
// sing-tun DefaultInterfaceMonitor) construct this from native callbacks.
type NetEvent struct {
	// Signal identifies what happened: "available", "unavailable", or "changed".
	Signal string

	// InterfaceName is the active network interface (e.g. "en0", "wlan0", "pdp_ip0").
	// Empty if unknown or Signal is "unavailable".
	InterfaceName string

	// InterfaceIndex is the OS-level interface index. 0 if unknown.
	InterfaceIndex int

	// IsWifi indicates WiFi or wired Ethernet connection.
	IsWifi bool

	// IsCellular indicates cellular data connection.
	IsCellular bool

	// HasIPv4 indicates the current path supports IPv4.
	HasIPv4 bool

	// HasIPv6 indicates the current path supports IPv6.
	HasIPv6 bool

	// Source identifies which monitor generated this event.
	// One of: "nwpath" (iOS), "connectivity" (Android), "singtun" (desktop), "manual".
	Source string
}

// netCoordinator fuses network signals from multiple sources
// (sing-tun DefaultInterfaceMonitor + platform APIs) into engine actions.
//
// Three scenarios:
//   - Network lost: stop futile reconnects and probe
//   - Network returned: full reconnect + reset
//   - Interface changed (network stayed up): reconnect wire
type netCoordinator struct {
	mu         deadlock.Mutex
	networkUp  bool
	lastSignal time.Time

	onReconnect func() // called outside lock
	onStopProbe func() // called outside lock
}

func newNetCoordinator(onReconnect, onStopProbe func()) *netCoordinator {
	return &netCoordinator{
		networkUp:   true, // assume up at start (engine just connected)
		onReconnect: onReconnect,
		onStopProbe: onStopProbe,
	}
}

func (nc *netCoordinator) handleEvent(event *NetEvent) {
	nc.mu.Lock()
	now := time.Now()

	switch event.Signal {
	case SignalUnavailable:
		if !nc.networkUp {
			nc.mu.Unlock()
			return
		}
		nc.networkUp = false
		nc.lastSignal = now
		nc.mu.Unlock()

		slog.Warn("netmon: network unavailable — suspending reconnects",
			"source", event.Source)
		if nc.onStopProbe != nil {
			nc.onStopProbe()
		}

	case SignalAvailable:
		wasDown := !nc.networkUp
		nc.networkUp = true
		if !wasDown {
			nc.mu.Unlock()
			return // duplicate
		}
		nc.lastSignal = now
		nc.mu.Unlock()

		slog.Info("netmon: network available — full reconnect",
			"interface", event.InterfaceName,
			"wifi", event.IsWifi,
			"cellular", event.IsCellular,
			"source", event.Source)
		if nc.onReconnect != nil {
			nc.onReconnect()
		}

	case SignalChanged:
		if !nc.networkUp {
			nc.mu.Unlock()
			slog.Debug("netmon: interface changed but network is down, ignoring",
				"source", event.Source)
			return
		}
		if now.Sub(nc.lastSignal) < eventDebounce {
			nc.mu.Unlock()
			return
		}
		nc.lastSignal = now
		nc.mu.Unlock()

		slog.Info("netmon: interface changed — reconnect",
			"interface", event.InterfaceName,
			"source", event.Source)
		if nc.onReconnect != nil {
			nc.onReconnect()
		}

	default:
		nc.mu.Unlock()
		slog.Warn("netmon: unknown signal", "signal", event.Signal)
	}
}

// isNetworkUp returns the last known network state.
func (nc *netCoordinator) isNetworkUp() bool {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	return nc.networkUp
}
```

- [ ] **Step 1.4: 运行测试确认 PASS**

Run: `cd k2 && go test ./engine/ -run TestNetCoordinator -v`
Expected: all 6 tests PASS

- [ ] **Step 1.5: Commit**

```bash
cd k2 && git add engine/netmon.go engine/netmon_test.go
git commit -m "feat(engine): add NetEvent struct + netCoordinator for network signal fusion"
```

---

## Task 2: 删除 dnsHandler → ReportWireError（架构修正）

**Files:**
- Modify: `k2/engine/dns_handler.go:185-196`

**原则**：DNS 失败是症状，不是 wire 层诊断。wire error 通道只应传输 wire 层的诊断结果（proxy dial 失败、probe handshake 失败）。

- [ ] **Step 2.1: 修改 recordDNSFailure**

将 `k2/engine/dns_handler.go` 中的 `recordDNSFailure` 方法：

```go
func (h *dnsHandler) recordDNSFailure(err error) {
	if h.engine == nil {
		return
	}
	if h.consecutiveFails.Add(1) == dnsWireErrorThreshold {
		slog.Warn("dns: consecutive failure threshold reached",
			"threshold", dnsWireErrorThreshold,
			"lastErr", err.Error(),
		)
		h.engine.ReportWireError(fmt.Errorf("DNS: %d consecutive failures: %w",
			dnsWireErrorThreshold, err))
	}
}
```

改为：

```go
func (h *dnsHandler) recordDNSFailure(err error) {
	if h.engine == nil {
		return
	}
	count := h.consecutiveFails.Add(1)
	if count == dnsWireErrorThreshold {
		// Log for diagnostics, but do NOT report as wire error.
		// DNS failure is a symptom (local network down, remote DNS broken, etc.),
		// not a wire transport diagnosis. Wire errors come from proxy dial and probe.
		slog.Warn("dns: consecutive failure threshold reached",
			"threshold", dnsWireErrorThreshold,
			"lastErr", err.Error(),
		)
	}
}
```

- [ ] **Step 2.2: 添加 ResetCounter 方法**

在 `recordDNSSuccess()` 之后添加：

```go
// ResetCounter resets the consecutive DNS failure counter.
// Called on network recovery — old failures from the outage are stale.
func (h *dnsHandler) ResetCounter() {
	if prev := h.consecutiveFails.Swap(0); prev > 0 {
		slog.Debug("dns: failure counter reset", "previousFails", prev)
	}
}
```

- [ ] **Step 2.3: 运行 engine 测试确认无回归**

Run: `cd k2 && go test ./engine/ -v -count=1`
Expected: PASS

- [ ] **Step 2.4: Commit**

```bash
cd k2 && git add engine/dns_handler.go
git commit -m "fix(engine): remove DNS→ReportWireError — DNS failure is symptom not wire diagnosis"
```

---

## Task 3: 集成 netCoordinator 到 Engine

**Files:**
- Modify: `k2/engine/engine.go`

- [ ] **Step 3.1: Engine struct 添加字段**

在 Engine struct 中 `probe *recoveryProbe` 之后添加：

```go
	netCoord *netCoordinator
	dnsH     *dnsHandler // for counter reset on network recovery
```

- [ ] **Step 3.2: Start() 保存 dnsHandler 引用**

找到创建 dnsHandler 的行（约 line 291）：
```go
if err := tunnel.StartWithHandler(ctx, &dnsHandler{inner: tunnel, dns: dnsMW, ctx: ctx, engine: e}); err != nil {
```
改为：
```go
dh := &dnsHandler{inner: tunnel, dns: dnsMW, ctx: ctx, engine: e}
if err := tunnel.StartWithHandler(ctx, dh); err != nil {
```

- [ ] **Step 3.3: Start() 创建 netCoordinator**

在 commit 区域，`e.probe = newRecoveryProbe(tm, e)` 之后添加：

```go
e.dnsH = dh
e.netCoord = newNetCoordinator(
	func() { e.doNetworkReconnect() },
	func() {
		e.mu.Lock()
		p := e.probe
		e.mu.Unlock()
		if p != nil {
			p.stop()
		}
	},
)
```

- [ ] **Step 3.4: Start() 修改 NetworkMonitor 回调**

找到：
```go
if err := cfg.NetworkMonitor.Start(func() { e.OnNetworkChanged() }); err != nil {
```
改为：
```go
if err := cfg.NetworkMonitor.Start(func() {
	e.mu.Lock()
	nc := e.netCoord
	e.mu.Unlock()
	if nc != nil {
		nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "singtun"})
	}
}); err != nil {
```

- [ ] **Step 3.5: Stop() 清理**

在 `e.probe = nil` 之后添加：
```go
e.netCoord = nil
e.dnsH = nil
```

- [ ] **Step 3.6: 添加 doNetworkReconnect 方法**

在 `OnNetworkChanged()` 附近添加：

```go
// doNetworkReconnect performs a full reconnect with DNS counter reset
// and stale error clearing. Called by netCoordinator on network recovery
// or interface change.
func (e *Engine) doNetworkReconnect() {
	slog.Info("engine: network reconnect")

	// Reset DNS failure counter — old failures are stale after network change.
	e.mu.Lock()
	dh := e.dnsH
	e.mu.Unlock()
	if dh != nil {
		dh.ResetCounter()
	}

	if !e.reconnect() {
		return
	}

	// Clear non-client wire errors — network errors are stale after network change.
	e.mu.Lock()
	handler := e.handler
	if e.lastError != nil && e.lastError.Category != CategoryClient {
		slog.Debug("engine: network change cleared stale error",
			"prevCode", e.lastError.Code,
			"prevCategory", e.lastError.Category,
		)
		e.lastError = nil
	}
	s := e.buildStatusLocked()
	e.mu.Unlock()
	if handler != nil {
		handler.OnStatus(s)
	}
}
```

- [ ] **Step 3.7: 修改 OnNetworkChanged 路由通过 coordinator**

```go
func (e *Engine) OnNetworkChanged() {
	e.mu.Lock()
	nc := e.netCoord
	e.mu.Unlock()
	if nc != nil {
		nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "singtun"})
		return
	}
	// Fallback when netCoord not initialized.
	e.doNetworkReconnect()
}
```

- [ ] **Step 3.8: 添加 OnNetEvent 方法**

```go
// OnNetEvent receives a network state change from the platform layer.
// Called from appext (gomobile bridge) with platform-specific information.
func (e *Engine) OnNetEvent(event *NetEvent) {
	e.mu.Lock()
	nc := e.netCoord
	e.mu.Unlock()
	if nc != nil {
		nc.handleEvent(event)
	}
}
```

- [ ] **Step 3.9: 修改 onHealthCritical 检查 networkUp**

```go
func (e *Engine) onHealthCritical() {
	// Skip reconnect if network is known to be down — avoids futile work.
	e.mu.Lock()
	nc := e.netCoord
	e.mu.Unlock()
	if nc != nil && !nc.isNetworkUp() {
		slog.Debug("engine: health critical but network is down, skipping reconnect")
		return
	}

	slog.Warn("engine: health critical, reconnecting")
	if !e.reconnect() {
		return
	}

	e.mu.Lock()
	handler := e.handler
	s := e.buildStatusLocked()
	e.mu.Unlock()
	if handler != nil {
		handler.OnStatus(s)
	}
}
```

- [ ] **Step 3.10: 运行 engine 全部测试**

Run: `cd k2 && go test ./engine/ -v -count=1`
Expected: all PASS

- [ ] **Step 3.11: Commit**

```bash
cd k2 && git add engine/engine.go
git commit -m "feat(engine): integrate netCoordinator — route signals, gate health reconnect"
```

---

## Task 4: gomobile 导出

**Files:**
- Modify: `k2/appext/appext.go`

- [ ] **Step 4.1: 添加 NotifyNetEvent 到 appext.Engine**

在 `OnNetworkChanged()` 方法之后添加：

```go
// NotifyNetEvent receives a network state change from the platform layer.
// Call from iOS NWPathMonitor or Android ConnectivityManager.
// event.Signal: "available", "unavailable", or "changed".
// See engine.NetEvent for field documentation.
func (e *Engine) NotifyNetEvent(event *engine.NetEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("appext: panic in NotifyNetEvent", "panic", r, "stack", string(debug.Stack()))
		}
	}()
	e.inner.OnNetEvent(event)
}
```

- [ ] **Step 4.2: 运行 appext 测试**

Run: `cd k2 && go test ./appext/ -v`
Expected: PASS

- [ ] **Step 4.3: Commit**

```bash
cd k2 && git add appext/appext.go
git commit -m "feat(appext): export NotifyNetEvent for gomobile"
```

---

## Task 5: iOS NE 集成

**Files:**
- Modify: `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift`

- [ ] **Step 5.1: 添加 NWPathMonitor**

顶部添加 import：
```swift
import Network
```

class 属性区域添加：
```swift
private var pathMonitor: NWPathMonitor?
```

在 `startTunnel` 中，`engine started successfully` 日志之后、`completionHandler(nil)` 之前添加：
```swift
                self.startPathMonitor()
```

class 末尾添加：

```swift
    // MARK: - Network Path Monitor

    private func startPathMonitor() {
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] path in
            guard let self = self else { return }
            let event = EngineNetEvent()
            event.source = "nwpath"
            event.isWifi = path.usesInterfaceType(.wifi) || path.usesInterfaceType(.wiredEthernet)
            event.isCellular = path.usesInterfaceType(.cellular)
            event.hasIPv4 = path.supportsIPVersion(.v4)
            event.hasIPv6 = path.supportsIPVersion(.v6)
            if let iface = path.availableInterfaces.first {
                event.interfaceName = iface.name
                event.interfaceIndex = iface.index
            }
            if path.status == .satisfied {
                event.signal = "available"
                logger.info("pathMonitor: available iface=\(event.interfaceName) wifi=\(event.isWifi) cell=\(event.isCellular)")
                NativeLogger.shared.log("INFO", "pathMonitor: available iface=\(event.interfaceName)")
            } else {
                event.signal = "unavailable"
                logger.info("pathMonitor: unavailable (status=\(path.status))")
                NativeLogger.shared.log("INFO", "pathMonitor: unavailable")
            }
            self.engine?.notifyNetEvent(event)
        }
        monitor.start(queue: .main)
        pathMonitor = monitor
    }

    private func stopPathMonitor() {
        pathMonitor?.cancel()
        pathMonitor = nil
    }
```

在 `stopTunnel` 中，`engine?.stop()` 之前添加：
```swift
        stopPathMonitor()
```

- [ ] **Step 5.2: Commit**

```bash
git add mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift
git commit -m "feat(ios): add NWPathMonitor feeding NetEvent to engine"
```

---

## Task 6: Android 集成

**Files:**
- Modify: `mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt`

- [ ] **Step 6.1: 修改 registerNetworkCallback**

将 `onAvailable` 中的：
```kotlin
                    engine?.onNetworkChanged()
```
改为：
```kotlin
                    val caps = cm.getNetworkCapabilities(network)
                    val lp = cm.getLinkProperties(network)
                    val event = EngineNetEvent().apply {
                        signal = "available"
                        interfaceName = lp?.interfaceName ?: ""
                        isWifi = caps?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true
                        isCellular = caps?.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) == true
                        hasIPv4 = lp?.linkAddresses?.any { it.address is java.net.Inet4Address } == true
                        hasIPv6 = lp?.linkAddresses?.any { it.address is java.net.Inet6Address } == true
                        source = "connectivity"
                    }
                    engine?.notifyNetEvent(event)
```

将 `onLost` 中的：
```kotlin
            engineExecutor.execute {
                engine?.onNetworkChanged()
            }
```
改为：
```kotlin
            engineExecutor.execute {
                val event = EngineNetEvent().apply {
                    signal = "unavailable"
                    source = "connectivity"
                }
                engine?.notifyNetEvent(event)
            }
```

注意：`onAvailable` 中的 `engine?.wake()` 保持不变（内存压力恢复逻辑不变）。

- [ ] **Step 6.2: Commit**

```bash
git add mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt
git commit -m "feat(android): send NetEvent with rich info instead of bare onNetworkChanged"
```

---

## Task 7: gomobile 构建验证

- [ ] **Step 7.1: 验证 iOS xcframework 构建**

Run: `cd k2 && make appext-ios`
Expected: 构建成功

- [ ] **Step 7.2: 验证 Android AAR 构建**

Run: `cd k2 && make appext-android`
Expected: 构建成功

- [ ] **Step 7.3: 运行完整测试套件**

Run: `cd k2 && go test -short ./...`
Expected: all PASS

---

## Task 8: 更新文档

- [ ] **Step 8.1: 更新 k2/engine/CLAUDE.md**

Files 部分添加：
```
- `netmon.go` — `NetEvent` (gomobile-compatible network event struct, 8 primitive fields) + `netCoordinator` (fuses sing-tun + platform signals into engine actions: suppress reconnect when network down, full reconnect on recovery, debounce rapid signals).
- `netmon_test.go` — netCoordinator unit tests
```

State Machine 部分添加：
```
- `OnNetEvent(event)`: routes through netCoordinator — "unavailable" stops probe, "available" triggers doNetworkReconnect, "changed" reconnects if network is up
```

Interfaces 部分添加：
```
- `NetEvent`: Network state change struct (Signal, InterfaceName, InterfaceIndex, IsWifi, IsCellular, HasIPv4, HasIPv6, Source) — constructed by platform layer, consumed by netCoordinator
```

- [ ] **Step 8.2: 更新根 CLAUDE.md**

Key Conventions 部分添加：
```
- **netCoordinator 网络信号融合**: Engine 内部 `netCoordinator` 融合 sing-tun 接口变化（`SignalChanged`）+ 平台精确状态（iOS NWPathMonitor `SignalAvailable/Unavailable`、Android ConnectivityManager）。网络断开时停止无效重连和 probe，网络恢复时全面重连 + 重置 DNS counter + 清除 stale wire error。Desktop 只走 sing-tun `SignalChanged` 路径，行为不变。`NetEvent` 结构体携带 8 个原始字段（gomobile 兼容），为未来精细化处理（WiFi/蜂窝区分、IPv4/IPv6 选择）预留空间。
- **DNS 失败不是 wire 错误**: `dnsHandler.recordDNSFailure()` 不调用 `ReportWireError`。DNS 失败是症状（本地网络断、服务端 DNS 挂），不是 wire 传输层诊断。只有 proxy dial 失败和 recovery probe 失败才报告 wire error。
```

Domain Vocabulary 部分添加：
```
- **netCoordinator** — Engine 内部网络状态协调器，融合 sing-tun + 平台 API 信号，区分"网络断了"/"网络恢复"/"接口变了"三种场景
- **NetEvent** — 网络状态变化事件结构体（Signal + 7 个平台信息字段），由平台层构造，gomobile 导出为 EngineNetEvent
```

- [ ] **Step 8.3: Commit**

```bash
git add k2/engine/CLAUDE.md CLAUDE.md
git commit -m "docs: add netCoordinator + NetEvent + DNS error fix to CLAUDE.md"
```

---

## 验证矩阵

| 场景 | 修复前 | 修复后 |
|------|--------|--------|
| iOS WiFi 断 → NE 活着 → WiFi 恢复 | 无效重连 → iOS 杀 NE → 永远断开 | NWPathMonitor unavailable → 停止重连 → available → reconnect → 恢复 ✅ |
| iOS WiFi→蜂窝 | sing-tun 触发 → reconnect ✅ | 同左 + NWPathMonitor 补充信息（IsCellular=true） ✅ |
| Android WiFi 断 → 恢复 | onLost/onAvailable → onNetworkChanged | onLost → unavailable → 停止重连 → onAvailable → available → reconnect ✅ |
| Desktop WiFi 断 → 恢复 | sing-tun → OnNetworkChanged ✅ | sing-tun → SignalChanged → reconnect（行为不变） ✅ |
| 服务端 DNS 挂（wire 正常） | DNS 30次 → error 570 → 误诊为 wire 错误 | DNS 失败只记日志，wire 正常无 error → 正确 ✅ |
| macOS DHCP 刷新 DNS | dnsOverride.reapply() 自动修复 ✅ | 不变，provider 层独立运行 ✅ |
| 同接口 DHCP 续租换 IP | sing-tun 不触发 ❌ | iOS NWPathMonitor / Android onCapabilitiesChanged 触发 SignalChanged ✅ |
