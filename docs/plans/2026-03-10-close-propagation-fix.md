# Close Propagation Systematic Fix

Date: 2026-03-10

## Problem

k2 tunnel 的 close 传播有 5 个系统性缺陷，导致连接残留、流式传输截断、UDP 会话泄漏。

| # | 问题 | 位置 | 影响 |
|---|------|------|------|
| 1 | `pipe()` 无 half-close | `core/tunnel.go` | TX 完成后 full Close 杀死 RX，大下载/流式传输被截断 |
| 2 | `quicStreamConn.Close()` 太激进 | `wire/quic.go` | `CancelRead(0)` 立即杀读方向，pipe() TX goroutine 调它后 RX 数据丢失 |
| 3 | 服务端无连接注册表 | `server/` | fire-and-forget goroutine，无法主动清理 |
| 4 | UDP 会话无 idle timeout（桌面端） | `wire/quic.go`, `wire/tcpws.go` | 对端消失后 session 永不释放 |
| 5 | 服务端 UDP handler 固定 30s timeout | `server/handler.go` | 和客户端不对齐 |

## Approach

渐进修复（Option A），每个修复独立验证，不引入新抽象层。

## Part 1: pipe() half-close + quicStreamConn 拆分 + closeOnce 幂等

**Files**: `core/tunnel.go`, `wire/quic.go`, `wire/tcpws.go`

### Close 调用链分析

修改前必须理解完整的 close 调用链。以 client handleTCPProxy 为例：

```
handleTCPProxy(local, dest):
    remote = dialer.DialTCP(dest)
    defer remote.Close()              ← ③ final close
    pipe(local, remote):
        goroutine: copy(remote←local) → remote.???  ← ① pipe 内 close
        main:      copy(local←remote) → local.???   ← ② pipe 内 close
        <-done
    // pipe 返回后 defer 执行

HandleTCP 外层:
    defer conn.Close()                ← ④ local 再次 Close
```

server HandleTCP 同样结构：

```
HandleTCP(conn, addr):
    defer conn.Close()                ← ④ wire conn final close
    target = net.DialTimeout(addr)
    defer target.Close()              ← ③ target final close
    pipe(conn, target):
        goroutine: copy(target←conn) → target.???   ← ①
        main:      copy(conn←target) → conn.???     ← ②
```

**关键发现**：两端 handler 都已有 `defer remote.Close()`。pipe() 内不需要负责 final close——defer 兜底。这使得 pipe() 内可以安全地只用 `CloseWrite()`。

### 修改后的完整 close 序列

```
① pipe goroutine: remote.CloseWrite()  → 只发 FIN，RX 继续收数据
② pipe main:      local.CloseWrite()   → RX 完成后发 FIN
   <-done (等 goroutine 退出)
   pipe 返回
③ defer:           remote.Close()       → CancelRead + Close，释放 stream slot
④ defer:           local.Close()        → 第二次 close（closeOnce 保证幂等）
```

### closeWriter 接口

在 `core/tunnel.go` 和 `server/handler.go` 各定义（不导出）：

```go
type closeWriter interface {
    CloseWrite() error
}
```

### pipe() 改造

两个方向都改为 CloseWrite()，full close 由调用方 defer 负责：

```go
func pipe(a, b net.Conn, tx, rx *atomic.Int64) {
    done := make(chan struct{})
    go func() {
        defer close(done)
        bp := tcpBufPool.Get().(*[]byte)
        n, _ := io.CopyBuffer(b, a, *bp)
        tcpBufPool.Put(bp)
        if tx != nil { tx.Add(n) }
        // Half-close: send FIN only, let peer know we're done sending
        if cw, ok := b.(closeWriter); ok {
            cw.CloseWrite()
        } else {
            b.Close()  // fallback for types without half-close
        }
    }()
    bp := tcpBufPool.Get().(*[]byte)
    n, _ := io.CopyBuffer(a, b, *bp)
    tcpBufPool.Put(bp)
    if rx != nil { rx.Add(n) }
    // Half-close: send FIN only
    if cw, ok := a.(closeWriter); ok {
        cw.CloseWrite()
    } else {
        a.Close()
    }
    <-done
    // Full close is caller's responsibility via defer
}
```

### quicStreamConn 拆分 + closeOnce

CloseWrite 和 Close 分离。Close 用 sync.Once 保证幂等（因为 pipe 内 CloseWrite 后，defer 还会调 Close）：

```go
type quicStreamConn struct {
    quic.Stream
    br        *bufio.Reader
    local     net.Addr
    remote    net.Addr
    closeOnce sync.Once
    closeErr  error
}

func (c *quicStreamConn) CloseWrite() error {
    return c.Stream.Close()     // FIN only, no CancelRead
}

func (c *quicStreamConn) Close() error {
    c.closeOnce.Do(func() {
        c.Stream.CancelRead(0)     // release stream slot
        c.closeErr = c.Stream.Close()
    })
    return c.closeErr
}
```

### smuxStreamConn + closeOnce

smux 的 `Stream.Close()` 本身就是 write-only。加 closeOnce 保证 defer 重复调用安全：

```go
type smuxStreamConn struct {
    *smux.Stream
    br        *bufio.Reader
    closeOnce sync.Once
}

func (c *smuxStreamConn) CloseWrite() error {
    return c.Stream.Close()     // smux Close = write-only FIN
}

func (c *smuxStreamConn) Close() error {
    c.closeOnce.Do(func() {
        c.Stream.Close()
    })
    return nil
}
```

### 效果

大文件下载：client 请求发完 → pipe TX goroutine 调 `CloseWrite()` → 只发 FIN → RX 继续收 response 直到 EOF → pipe 返回 → `defer remote.Close()` 释放 stream slot。无数据丢失。

## Part 2: Server-side pipe() half-close

**Files**: `server/handler.go`

和 Part 1 完全相同的 pipe() 改造。server 的 `defer` 结构已存在：

```go
func (h *ProxyHandler) HandleTCP(conn net.Conn, addr string) {
    defer conn.Close()         // ← final close for wire conn
    target, err := net.DialTimeout("tcp4", addr, h.DialTimeout)
    defer target.Close()       // ← final close for target
    pipe(conn, target)         // ← 内部只用 CloseWrite
}
```

`net.TCPConn` 天然支持 `CloseWrite()`（Go stdlib），不需要额外适配。

`net.TCPConn.Close()` 在 `CloseWrite()` 之后调用是安全的（Go stdlib 保证）。

HandleUDP 不变。

## Part 3: Server connection registry

**Files**: `server/handler.go`, `server/server.go`

Add `connTracker` to `ProxyHandler` (same pattern as client `core/tunnel.go`):

```go
type ProxyHandler struct {
    DialTimeout    time.Duration
    AllowPrivateIP bool
    conns          connTracker   // map[io.Closer]struct{} + deadlock.Mutex
}
```

HandleTCP/HandleUDP register both wire conn and target conn:

```go
func (h *ProxyHandler) HandleTCP(conn net.Conn, addr string) {
    defer conn.Close()
    h.conns.add(conn)
    defer h.conns.remove(conn)

    target, err := net.DialTimeout("tcp4", addr, h.DialTimeout)
    if err != nil { ... }
    defer target.Close()
    h.conns.add(target)
    defer h.conns.remove(target)

    pipe(conn, target)
}
```

Server shutdown calls `handler.Close()` → `conns.closeAll()` → all blocked `io.CopyBuffer` return → goroutines exit.

Not adding: connection limits (QUIC MAX_STREAMS suffices), per-connection timeout, metrics.

## Part 4: UDP session idle timeout

**Files**: `core/tunnel.go`, `server/handler.go`, `wire/quic.go`, `wire/tcpws.go`

Unify UDP idle timeout to 60s across all layers:

**Client `pipePacket()`**: Replace platform-differentiated `udpIdleTimeoutSec` with unified 60s constant. iOS 15s was too short (DNS-over-QUIC), desktop infinite was leaking. iOS memory impact to be tuned separately if needed.

**Server `HandleUDP`**: Change 30s read deadline to 60s, aligned with client.

**Wire layer session map cleanup**: Add idle session reaper to `quicUDPMux` and `smuxUDPMux`. Goroutine scans every 30s, removes sessions idle >60s:

```go
func (m *quicUDPMux) reapIdleSessions() {
    for {
        select {
        case <-time.After(30 * time.Second):
            m.mu.Lock()
            for id, sess := range m.sessions {
                if time.Since(sess.lastActive) > 60*time.Second {
                    sess.close()
                    delete(m.sessions, id)
                }
            }
            m.mu.Unlock()
        case <-m.done:
            return
        }
    }
}
```

Update `deliverToSession` to set `sess.lastActive = time.Now()` on each packet delivery. Use `atomic.Int64` storing unix timestamp to avoid lock contention on high-frequency UDP paths.

Not adding: per-session configurable timeout, protocol-specific timeout (VoIP/gaming have activity within 60s).

## Implementation Order

1. Part 1 (pipe + quicStreamConn + closeOnce) — highest impact, fixes data truncation
2. Part 2 (server pipe) — same fix, server side
3. Part 3 (server connTracker) — enables graceful shutdown
4. Part 4 (UDP idle timeout) — resource cleanup

Each part is independently testable and deployable.

## Confidence

| Part | Score | Rationale |
|------|-------|-----------|
| 1 | 9/10 | Close sequence verified end-to-end: pipe() CloseWrite → defer full Close → closeOnce idempotent |
| 2 | 9/10 | Same pattern as Part 1, net.TCPConn.CloseWrite() is stdlib-guaranteed safe |
| 3 | 9/10 | Proven pattern from client-side connTracker |
| 4 | 7/10 | iOS memory impact needs separate tuning; reaper lock contention mitigated by atomic.Int64 |
| **Overall** | **9/10** | |
