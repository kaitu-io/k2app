# safego Crash Resilience Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 引入 `safego` 包消除生产环境 goroutine panic crash，同时修复审计发现的关键资源泄漏风险。

**Architecture:** 新建 `safego/` 包提供 `Go()` 和 `Loop()` 两个函数，分别处理 fire-and-forget 和 long-running goroutine 的 panic recovery。对现有 `go func()` 调用点按必要性分级替换，而不是盲目全替换。同时修复 `sniff/qtls.go` 的 panic 根因和 `wire/quic.go` 的 stream 泄漏。

**Tech Stack:** Go stdlib (`runtime/debug`, `log/slog`, `context`, `time`)

---

## 风险点必要性评估

在动手之前，对审计发现的每个风险点做必要性判断。**核心原则：recover 只是兜底，根治比兜底重要；资源问题不能靠 recover 解决。**

### 必须修复（根治 + safego 兜底）

| # | 风险 | 修复方式 | 理由 |
|---|------|---------|------|
| 1 | `sniff/qtls.go` 4个 panic() | **根治**：改成 return error | panic 是根因，recover 只是兜底。SniffQUIC 调用链天然支持 error return |
| 2 | `wire/quic.go:80,85` overflow stream Close() 缺 CancelRead | **根治**：加 CancelRead(0) | stream slot 泄漏 → MAX_STREAMS 耗尽。recover 无法修复资源泄漏 |
| 3 | `engine/health.go:102` healthMonitor goroutine 无 recover | **safego.Loop**：panic 后自动重启 | 必须活着的 long-running goroutine，死了 = 健康监控全丢 |
| 4 | `engine/recovery_probe.go:56` probe goroutine 无 recover | **safego.Go**：panic 后 log | fire-and-forget，死了无所谓，下次 trigger() 重建 |
| 5 | `wire/auth.go:133` TicketStore cleanup goroutine 无 recover | **safego.Loop**：panic 后自动重启 | 服务端 long-running，死了 = 内存泄漏 |

### 应该修复（safego 兜底即可）

| # | 风险 | 修复方式 | 理由 |
|---|------|---------|------|
| 6 | `provider/stack.go:31,41` sing-tun handler 无 recover | **safego.Go** | 每个连接一个 goroutine，panic 只影响单个连接 |
| 7 | `daemon/daemon.go:117` HTTP server goroutine 无 recover | **safego.Go** | fire-and-forget，HTTP server 自带 recover |
| 8 | `daemon/daemon.go:330` auto-reconnect goroutine 无 recover | **safego.Go** | 一次性延迟任务，panic 只影响自动重连 |
| 9 | `server/server.go:358-386` 5个 server.Run wg goroutine 无 recover | **safego.Go** | 有 wg.Wait 同步，但 panic 会导致 wg 永远不 Done |

### 不需要修复（已有保护或风险极低）

| # | 风险 | 理由 |
|---|------|------|
| 10 | `wire/quic.go:706,731,751,764,867,930` QUICServer 各 goroutine | **已有 `defer recover()`**，不需要 safego |
| 11 | `wire/tcpws.go:430,454,518` TCPWSServer 各 goroutine | **已有 `defer recover()`** |
| 12 | `server/server.go:420,446` acceptTCPLoop handler | **已有 `defer recover()`** |
| 13 | `core/tunnel.go:144` Close() 的 wg.Wait goroutine | 5s 超时保护，不会 panic |
| 14 | `core/tunnel.go:526,562` pipe/pipePacket goroutine | 纯 io.Copy，不会 panic（网络 I/O 返回 error 不 panic） |
| 15 | `wire/ech.go:288,334` ReverseProxy goroutine | 纯 io.Copy |
| 16 | `cmd/k2/process.go:89` signal handler | 只有 signal.Notify + select，不会 panic |
| 17 | `provider/dns_darwin.go:455` CFRunLoop stop goroutine | 只有 channel receive + C 调用 |
| 18 | `core/dns/racing.go:144` DNS racer goroutine | 一次性查询，有 context timeout 兜底 |

### goroutine 暴增评估

**safego.Go()** — 不会导致 goroutine 暴增：
- 它只是在现有 `go func()` 上加了 `defer recover()`，不额外创建 goroutine
- goroutine 生命周期不变：原来什么时候退出，现在还是什么时候退出

**safego.Loop()** — 需要注意：
- panic 后重启会创建新 goroutine 迭代，但旧的已经退出
- 加 1s backoff 防止 panic loop（每秒最多重启 1 次）
- 只用于 2 个地方（healthMonitor + TicketStore cleanup），这两个本来就只有 1 个实例
- **最坏情况**：代码有持续 panic 的 bug → 每秒重启 1 次 → 实际只有 1 个活跃 goroutine（旧的已退出）+ 每秒 1 条 error 日志。不会暴增

**sniff/qtls.go 根治后**：
- SniffQUIC 改成 return error → HandleUDP 正常处理错误 → 不会 panic → safego.Go 的 recover 永远不触发
- 这是最理想的状态：根治消除 panic 源头，safego 只作为最后防线

---

## Task 1: 创建 safego 包

**Files:**
- Create: `safego/safego.go`
- Create: `safego/safego_test.go`

**Step 1: 写 safego.go**

```go
// Package safego provides panic-safe goroutine launchers.
//
// Go() wraps fire-and-forget goroutines with panic recovery.
// Loop() wraps long-running goroutines that must stay alive — panics are
// recovered and the function is restarted after a 1-second backoff.
//
// Usage pattern (replaces bare "go func()"):
//
//	safego.Go(func() { handleRequest(conn) })
//	safego.Loop(ctx, "healthMonitor", func(ctx context.Context) { monitor(ctx) })
package safego

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"
)

// Go runs fn in a new goroutine with panic recovery.
// If fn panics, the panic is logged with a stack trace and the goroutine exits.
// Use for fire-and-forget goroutines where a panic should not crash the process.
func Go(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("safego: panic recovered",
					"panic", r,
					"stack", string(debug.Stack()),
				)
			}
		}()
		fn()
	}()
}

// Loop runs fn repeatedly until ctx is cancelled. If fn panics, the panic
// is logged and fn is restarted after a 1-second backoff.
// Use for long-running goroutines that must stay alive (e.g. health monitors).
//
// fn should return when ctx is cancelled. If fn returns normally (no panic),
// Loop checks ctx and restarts only if ctx is still active — this handles
// unexpected early returns without spinning.
func Loop(ctx context.Context, name string, fn func(ctx context.Context)) {
	go func() {
		for ctx.Err() == nil {
			panicked := runWithRecover(name, func() { fn(ctx) })
			if !panicked {
				return // fn returned normally — don't restart
			}
			// Backoff before restart; respect cancellation.
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
}

// runWithRecover calls fn and recovers from panics. Returns true if fn panicked.
func runWithRecover(name string, fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			slog.Error("safego: panic in loop, will restart",
				"name", name,
				"panic", r,
				"stack", string(debug.Stack()),
			)
		}
	}()
	fn()
	return false
}
```

**Step 2: 写测试 safego_test.go**

```go
package safego

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestGo_PanicRecovery(t *testing.T) {
	done := make(chan struct{})
	Go(func() {
		defer close(done)
		panic("test panic")
	})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not complete")
	}
}

func TestGo_NormalExecution(t *testing.T) {
	var result int
	done := make(chan struct{})
	Go(func() {
		result = 42
		close(done)
	})
	<-done
	if result != 42 {
		t.Fatalf("expected 42, got %d", result)
	}
}

func TestLoop_PanicRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	callCount := 0

	Loop(ctx, "test-loop", func(ctx context.Context) {
		mu.Lock()
		callCount++
		n := callCount
		mu.Unlock()

		if n <= 2 {
			panic("deliberate panic")
		}
		// Third call: exit normally
		cancel()
	})

	// Wait for loop to finish (cancel + propagation)
	time.Sleep(3 * time.Second)
	mu.Lock()
	defer mu.Unlock()
	if callCount < 3 {
		t.Fatalf("expected at least 3 calls, got %d", callCount)
	}
}

func TestLoop_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan struct{})
	Loop(ctx, "test-cancel", func(ctx context.Context) {
		close(started)
		<-ctx.Done()
	})

	<-started
	cancel()
	// Loop should exit — no way to verify directly, but no goroutine leak
	time.Sleep(100 * time.Millisecond)
}

func TestLoop_NormalReturnStops(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var mu sync.Mutex
	callCount := 0

	Loop(ctx, "test-normal-return", func(ctx context.Context) {
		mu.Lock()
		callCount++
		mu.Unlock()
		// Return normally — Loop should NOT restart
	})

	time.Sleep(500 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if callCount != 1 {
		t.Fatalf("expected 1 call (no restart on normal return), got %d", callCount)
	}
}
```

**Step 3: 运行测试**

```bash
go test ./safego/... -v -race
```
Expected: PASS

**Step 4: Commit**

```bash
git add safego/
git commit -m "feat: add safego package for panic-safe goroutine launching"
```

---

## Task 2: 根治 sniff/qtls.go 的 panic

**Files:**
- Modify: `sniff/qtls.go:36-51` (quicAEADAESGCM)
- Modify: `sniff/qtls.go:83-98` (quicHKDFExpandLabel)
- Modify: `sniff/quic.go` (SniffQUIC — 调用这两个函数的地方)

**为什么根治而不是 safego 兜底：** panic 是从 sing-box 移植来的代码风格（库代码用 panic 表示不可能的情况）。但在 k2 的使用场景中，输入来自网络包 — 畸形 QUIC Initial 包完全有可能触发这些 panic。改成 return error 是正确的做法。

**Step 1: 修改 quicAEADAESGCM 返回 error**

改函数签名 `func quicAEADAESGCM(key, nonceMask []byte) cipher.AEAD` → `func quicAEADAESGCM(key, nonceMask []byte) (cipher.AEAD, error)`

```go
func quicAEADAESGCM(key, nonceMask []byte) (cipher.AEAD, error) {
	if len(nonceMask) != 12 {
		return nil, fmt.Errorf("wrong nonce length: %d", len(nonceMask))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ret := &xorNonceAEAD{aead: aead}
	copy(ret.nonceMask[:], nonceMask)
	return ret, nil
}
```

**Step 2: 修改 quicHKDFExpandLabel 返回 error**

改签名 `func quicHKDFExpandLabel(...) []byte` → `func quicHKDFExpandLabel(...) ([]byte, error)`

```go
func quicHKDFExpandLabel(hash crypto.Hash, secret, context []byte, label string, length int) ([]byte, error) {
	b := make([]byte, 3, 3+6+len(label)+1+len(context))
	binary.BigEndian.PutUint16(b, uint16(length))
	b[2] = uint8(6 + len(label))
	b = append(b, []byte("tls13 ")...)
	b = append(b, []byte(label)...)
	b = b[:3+6+len(label)+1]
	b[3+6+len(label)] = uint8(len(context))
	b = append(b, context...)
	out := make([]byte, length)
	n, err := hkdf.Expand(hash.New, secret, b).Read(out)
	if err != nil || n != length {
		return nil, fmt.Errorf("HKDF-Expand-Label failed: read %d/%d: %w", n, length, err)
	}
	return out, nil
}
```

**Step 3: 更新 sniff/quic.go 中的调用方**

找到所有调用 `quicAEADAESGCM` 和 `quicHKDFExpandLabel` 的地方，添加 error 检查。`SniffQUIC` 已经返回 `(string, error)`，所以 error 自然向上传播。需要添加 `"fmt"` 到 import。

**Step 4: 运行测试**

```bash
go test ./sniff/... -v -race
```
Expected: PASS

**Step 5: Commit**

```bash
git add sniff/
git commit -m "fix(sniff): replace panics with error returns in QUIC crypto helpers

Malformed QUIC Initial packets from the network could trigger panics in
quicAEADAESGCM and quicHKDFExpandLabel (ported from sing-box). Since input
is untrusted network data, these should return errors, not panic."
```

---

## Task 3: 修复 QUIC overflow stream CancelRead 泄漏

**Files:**
- Modify: `wire/quic.go:80,85` (init error paths)
- Modify: `wire/quic.go:106-112` (close method)

**为什么根治而不是 safego 兜底：** stream slot 泄漏是资源问题，recover 无法修复。

**Step 1: 修复 init() 中的错误路径**

将 `stream.Close()` 替换为同时调用 `CancelRead` + `Close`：

```go
// 在 init() 的两个错误路径中：
if err := WriteH3ProxyFrameType(stream); err != nil {
	stream.CancelRead(0)
	stream.Close()
	o.initErr = fmt.Errorf("wire: write overflow H3 frame: %w", err)
	return o.initErr
}
if err := WriteStreamHeader(stream, StreamTypeUDPOverflow, ""); err != nil {
	stream.CancelRead(0)
	stream.Close()
	o.initErr = fmt.Errorf("wire: write overflow header: %w", err)
	return o.initErr
}
```

**Step 2: 修复 close() 方法**

```go
func (o *udpOverflowStream) close() {
	o.initMu.Lock()
	defer o.initMu.Unlock()
	if o.stream != nil {
		o.stream.CancelRead(0)
		o.stream.Close()
	}
}
```

**Step 3: 运行测试**

```bash
go test ./wire/... -v -race -short
```
Expected: PASS

**Step 4: Commit**

```bash
git add wire/quic.go
git commit -m "fix(wire): add CancelRead to overflow stream close paths

Without CancelRead, the receive half of the stream never completes,
leaking a QUIC stream slot. Over time this exhausts MAX_STREAMS and
blocks all new stream opens."
```

---

## Task 4: 应用 safego 到 engine 层

**Files:**
- Modify: `engine/health.go:100-114` (healthMonitor.start)
- Modify: `engine/recovery_probe.go:56` (probe.trigger)

**Step 1: healthMonitor.start 改用 safego.Loop**

```go
import "github.com/kaitu-io/k2/safego"

func (hm *healthMonitor) start() {
	hm.wg.Add(1)
	// safego.Loop 不合适——healthMonitor 用 wg 跟踪生命周期，
	// 而 Loop 创建的 goroutine 不在 wg 控制下。改用 safego.Go + 内部 recover-restart。
	safego.Go(func() {
		defer hm.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-hm.done:
				return
			case <-ticker.C:
				hm.sampleSafe()
			}
		}
	})
}

// sampleSafe wraps sample() with panic recovery so the health monitor loop survives.
func (hm *healthMonitor) sampleSafe() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("health: sample panic recovered", "panic", r,
				"stack", string(debug.Stack()))
		}
	}()
	hm.sample()
}
```

**注意**：这里不用 `safego.Loop`，因为 healthMonitor 有自己的 `wg.Wait` 生命周期管理。如果用 Loop，`wg.Done()` 在 panic 时会被调用，但 Loop 重启的新 goroutine 没有 `wg.Add(1)`。所以用 `safego.Go` + 方法级 recover（`sampleSafe`），保持原有 goroutine 活着。

**资源影响**：零。goroutine 数量不变（始终 1 个），panic 后 goroutine 继续下一个 ticker tick。

**Step 2: recoveryProbe.trigger 改用 safego.Go**

```go
import "github.com/kaitu-io/k2/safego"

func (p *recoveryProbe) trigger() {
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.mu.Unlock()

	safego.Go(func() { p.run(ctx) })
}
```

**资源影响**：零。goroutine 生命周期不变。

**Step 3: 运行测试**

```bash
go test ./engine/... -v -race -short
```
Expected: PASS

**Step 4: Commit**

```bash
git add engine/health.go engine/recovery_probe.go
git commit -m "fix(engine): add panic recovery to healthMonitor and recoveryProbe

healthMonitor.sample() calls into sniff/ via WireStats path — a panic
there would kill the health monitoring goroutine silently. Use per-sample
recover so the goroutine survives.

recoveryProbe.run() wraps with safego.Go for consistency."
```

---

## Task 5: 应用 safego 到 provider/stack.go（sing-tun handler）

**Files:**
- Modify: `provider/stack.go:29-47` (NewConnectionEx, NewPacketConnectionEx)

**Step 1: 替换 go func()**

```go
import "github.com/kaitu-io/k2/safego"

func (h *handlerAdapter) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	dest := destination.String()
	safego.Go(func() {
		h.handler.HandleTCP(conn, dest)
		if onClose != nil {
			onClose(nil)
		}
	})
}

func (h *handlerAdapter) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	dest := destination.String()
	safego.Go(func() {
		h.handler.HandleUDP(&singPacketConn{conn: conn}, dest)
		if onClose != nil {
			onClose(nil)
		}
	})
}
```

**资源影响**：零。这是最高频的调用点（每个 TCP/UDP 连接一个 goroutine），但 safego.Go 只加了一个 defer recover，不增加 goroutine 数量。

**重要**：onClose 回调在 defer 中仍然会被调用（Go 的 defer 在 panic recover 后执行），所以 sing-tun 的资源清理不受影响。但需要确认：如果 handler.HandleTCP panic 了，recover 会先执行，然后 `onClose(nil)` 正常执行吗？

答案：**不会**。`safego.Go` 的 recover 在外层 goroutine，而 `onClose(nil)` 在 `fn()` 内部但在 panic 之后。panic 后 `fn()` 的执行中断，`onClose` 不会被调用。

**修正**：需要确保 onClose 在 panic 后也被调用：

```go
func (h *handlerAdapter) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	dest := destination.String()
	safego.Go(func() {
		defer func() {
			if onClose != nil {
				onClose(nil)
			}
		}()
		h.handler.HandleTCP(conn, dest)
	})
}
```

**Step 2: 运行测试**

```bash
go test ./provider/... -v -race -short
```
Expected: PASS

**Step 3: Commit**

```bash
git add provider/stack.go
git commit -m "fix(provider): add panic recovery to sing-tun handler goroutines

A panic in HandleTCP/HandleUDP would crash the process. With safego.Go,
the panic is logged and the goroutine exits cleanly. The onClose callback
is deferred to ensure sing-tun resource cleanup even on panic."
```

---

## Task 6: 应用 safego 到 daemon 层

**Files:**
- Modify: `daemon/daemon.go:117` (HTTP server goroutine)
- Modify: `daemon/daemon.go:330` (auto-reconnect goroutine)

**Step 1: HTTP server goroutine**

```go
import "github.com/kaitu-io/k2/safego"

// 在 Run() 中：
safego.Go(func() {
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		errCh <- err
	}
})
```

**资源影响**：零。这个 goroutine 只有 1 个实例，而且 net/http 的 Server 内部已经有 per-request recover。

**Step 2: auto-reconnect goroutine**

```go
safego.Go(func() {
	select {
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		slog.Info("auto-reconnect: cancelled during startup delay")
		return
	}
	if d.State() == engine.StateConnected {
		return
	}
	if err := d.doUp(st.Config, 0); err != nil {
		slog.Error("auto-reconnect failed", "err", err)
	}
})
```

**资源影响**：零。一次性 goroutine，最多 1 个实例。

**Step 3: 运行测试**

```bash
go test ./daemon/... -v -race -short
```
Expected: PASS

**Step 4: Commit**

```bash
git add daemon/daemon.go
git commit -m "fix(daemon): add panic recovery to HTTP server and auto-reconnect goroutines"
```

---

## Task 7: 应用 safego 到 server 层

**Files:**
- Modify: `server/server.go:358-386` (5 个 Server.Run 的 wg goroutine)
- Modify: `wire/auth.go:133` (TicketStore.StartCleanup)

**Step 1: server.Run 中的 5 个 goroutine**

这些 goroutine 有 `wg.Add(1)` / `wg.Done()` 管理。不能用 `safego.Go` 因为 recover 后 `wg.Done()` 不会被调用 → wg.Wait() 永久阻塞。

正确做法：在内部加 recover，不用 safego：

```go
// 这些 goroutine 已经有 defer wg.Done()，panic 后 defer 链正常执行。
// 但 acceptTCPLoop/ServeUDP 里调用的 handler goroutine 已经有 recover。
// server.Run 的 5 个 goroutine 只是调度器（accept loop），不处理业务逻辑。
// 它们 panic 的概率极低（只有 ctx/channel 操作）。
// 结论：不改。已有 wg.Done 保证退出，内部的 handler goroutine 已有 recover。
```

**跳过 server.Run 的 5 个 goroutine — 风险极低，改动收益不大。**

**Step 2: TicketStore.StartCleanup 改用 safego**

```go
import "github.com/kaitu-io/k2/safego"

func (s *TicketStore) StartCleanup(interval time.Duration, done chan struct{}) {
	safego.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.cleanupSafe()
			case <-done:
				return
			}
		}
	})
}

func (s *TicketStore) cleanupSafe() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("auth: cleanup panic recovered", "panic", r)
		}
	}()
	s.cleanup()
}
```

**资源影响**：零。goroutine 数量不变（1 个），panic 后 goroutine 继续下一个 tick。

**Step 3: 运行测试**

```bash
go test ./wire/... -v -race -short
go test ./server/... -v -race -short
```
Expected: PASS

**Step 4: Commit**

```bash
git add wire/auth.go
git commit -m "fix(wire): add panic recovery to TicketStore cleanup goroutine

If cleanup panics, the goroutine dies silently and expired tickets
accumulate forever. Wrap with per-tick recover so the loop survives."
```

---

## Task 8: 全量测试 + 质量门

**Step 1: 运行完整测试套件**

```bash
make quick-check
```
Expected: PASS (go vet + golangci-lint + go test -short -race)

**Step 2: 确认 goroutine 泄漏检测通过**

```bash
go test -race -count=1 ./safego/... ./engine/... ./wire/... ./daemon/... ./provider/... ./core/... ./server/...
```
Expected: PASS (goleak 在 TestMain 中检测 goroutine 泄漏)

**Step 3: Commit (如有 lint 修复)**

---

## 资源影响总结

| 修改点 | goroutine 增量 | 内存增量 | 备注 |
|--------|---------------|---------|------|
| safego.Go (所有调用点) | 0 | ~0 (defer 栈帧) | 只加 defer recover，不增加 goroutine |
| healthMonitor sampleSafe | 0 | ~0 | 同上 |
| TicketStore cleanupSafe | 0 | ~0 | 同上 |
| sniff/qtls.go 去 panic | 0 | 0 | 减少了 crash 风险 |
| overflow stream CancelRead | 0 | **减少** (回收 stream slot) | 修复泄漏 |

**结论**：所有修改的 goroutine 数量增量为 0，内存增量约为 0。safego 包本身无状态，不持有任何资源。
