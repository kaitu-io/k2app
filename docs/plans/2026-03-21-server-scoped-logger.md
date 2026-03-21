# Server-Side Scoped Logger — Implementation Plan

## Context

调试 feedback ticket #50 时发现：服务端 HandleUDP 日志没有 client IP（remote），无法判断 `target->client forwarding` 属于哪个客户端。同时服务端有 ~80 个日志点手动拼接 `"remote", conn.RemoteAddr()`，不一致、易遗漏。

目标：用 `slog.With()` 层级 logger 替代手动字段拼接，确保每条服务端日志自动带 `remote`（连接级）和 `sid`/`streamID`（session 级）。

## 设计原则

- **不新增跨层耦合**：不注入 UDID，不添加 ConnIdentityProvider
- **不改接口签名**：`UDPHandler.HandleUDP(conn net.PacketConn, addr string)` 不变
- **用 optional interface 传递 logger**：`wire.UDPSessionInfo.Logger()`
- **全覆盖**：QUIC server + TCP-WS server + handler 层，所有日志点

## 关联 ID 字段

| 字段 | 来源 | 层级 | 说明 |
|------|------|------|------|
| `remote` | `conn.RemoteAddr()` | 连接级 | 客户端 IP:port |
| `sid` | `frame.SessionID` | UDP session 级 | UDP relay session ID |
| `addr` | `frame.Addr` / handler param | session 级 | 目标地址 |
| `streamID` | `stream.StreamID()` | TCP stream 级 | QUIC stream ID |

## 改动清单

### Step 1: wire/wire.go — 扩展 UDPSessionInfo

在已有的 `UDPSessionInfo` 接口增加 `Logger()` 方法：

```go
type UDPSessionInfo interface {
    UDPSessionID() uint32
    UDPRemoteAddr() string
    Logger() *slog.Logger
}
```

需要 `import "log/slog"`。

### Step 2: wire/quic.go — connUDPState 加 log 字段

```go
type connUDPState struct {
    // ...existing...
    log *slog.Logger // connection-scoped logger (remote)
}
```

### Step 3: wire/quic.go — handleConn 创建 connLog 并传播

`handleConn` 创建 connLog，存入 state，传入 handleStream/handleDatagrams：

```go
func (s *QUICServer) handleConn(conn *quic.Conn) {
    connLog := slog.With("remote", conn.RemoteAddr())
    state := &connUDPState{
        sessions: make(map[uint32]*serverUDPConn),
        overflow: &udpOverflowStream{qconn: conn},
        done:     make(chan struct{}),
        log:      connLog,
    }
    // AcceptStream loop: 用 connLog
    // handleDatagrams: 签名加 log 参数或从 state 取
}
```

handleConn 内所有日志（AcceptStream loop、panic recovery、cleanup）从 `slog.XXX("...", "remote", conn.RemoteAddr())` 改为 `connLog.XXX("...")`。

### Step 4: wire/quic.go — handleStream 用 connLog

`handleStream` 当前签名: `handleStream(qconn *quic.Conn, stream *quic.Stream, state *connUDPState)`

从 `state.log` 取 connLog，创建 streamLog：

```go
func (s *QUICServer) handleStream(qconn *quic.Conn, stream *quic.Stream, state *connUDPState) {
    log := state.log.With("streamID", stream.StreamID())
    // 所有日志用 log
}
```

涉及日志点（~8 个）：
- `quic.go:1012` handleStream read frame type failed
- `quic.go:1017` unknown frame type
- `quic.go:1025` read header failed
- `quic.go:1030` parsed
- `quic.go:1041` TCP queued
- `quic.go:1049` unknown stream type
- `quic.go:1090` panic in HandleUDP (stream)
- `quic.go:980` accepted stream
- `quic.go:984` panic in handleStream

### Step 5: wire/quic.go — handleDatagrams 用 connLog

从 `state.log` 取 connLog。涉及日志点（~4 个）：
- `quic.go:1103` handleDatagrams started
- `quic.go:1109` DIAG: datagram-recv-exit
- `quic.go:1116` datagram parse failed
- `quic.go:1119` datagram received（DEBUG，保留 sessionID+addr）
- `quic.go:1133` max UDP sessions reached

### Step 6: wire/quic.go — deliverToSession 创建 sessLog

```go
sessLog := state.log.With("sid", frame.SessionID, "addr", frame.Addr)
sess = &serverUDPConn{
    // ...existing...
    log: sessLog,
}
```

`new UDP session` 日志改用 `sessLog.Info("QUICServer: new UDP session", "handlerSet", handler != nil)`。

### Step 7: wire/quic.go — serverUDPConn 用 sessLog

`serverUDPConn` 已有 `log *slog.Logger` 字段。实现 `Logger()` 方法：

```go
func (c *serverUDPConn) Logger() *slog.Logger { return c.log }
```

改造 `close()` 和 `WriteTo` 中的日志（~3 个日志点）用 `c.log`。

### Step 8: wire/quic.go — handleUDPOverflowStream 用 connLog

`handleUDPOverflowStream(br, state)` 从 state.log 取 connLog。涉及 ~2 个日志点。

### Step 9: wire/quic.go — quicStreamServerUDPConn 加 log

`quicStreamServerUDPConn`（handleUDPStream 路径）也需要加 `log` 字段。
但这条路径没有 `conn.RemoteAddr()`（是 QUIC stream，不是 connection）。
handleUDPStream 从 handleStream 调用，handleStream 有 state → 有 connLog。

需要把 state 传入 handleUDPStream（当前没传）。

改签名：`handleUDPStream(stream *quic.Stream, br *bufio.Reader, connLog *slog.Logger)`

### Step 10: wire/tcpws.go — TCP-WS server 路径

TCP-WS 有类似的结构：`HandleConn → handleSession → handleStream → handleUDPStream`

问题：`HandleConn(conn net.Conn)` 有 `conn.RemoteAddr()`，但 smux session 之后就丢了。

改造：
1. `HandleConn` / `HandleSession` 创建 connLog
2. 传入 `handleSession`（改签名加 `log *slog.Logger`）
3. `handleSession` → `handleStream` → `handleUDPStream` 逐层传递

涉及类型：
- `smuxServerUDPConn` 加 `log *slog.Logger` 字段 + `remoteAddr string`
- 实现 `UDPSessionInfo` 接口（`UDPSessionID`, `UDPRemoteAddr`, `Logger`）
- `smuxStreamConn` 加 `remote net.Addr` 字段（TCP 代理路径，让 HandleTCP 能拿到 remote）

TCP-WS 涉及日志点（~10 个）：
- `tcpws.go:597` panic in handleSession
- `tcpws.go:621` panic in handleStream
- `tcpws.go:685` panic in HandleUDP
- 以及 smuxServerUDPConn 的 close/WriteTo 等

### Step 11: server/handler.go — HandleUDP 用 scoped logger

```go
func (h *ProxyHandler) HandleUDP(conn net.PacketConn, addr string) {
    log := slog.With("addr", addr)
    if info, ok := conn.(wire.UDPSessionInfo); ok {
        log = info.Logger()
    }
    // 所有日志用 log
}
```

涉及日志点（~15 个）：HandleUDP start/done + 中间所有 forwarding/error + handleDNSUDP 内的 DNS DIAG 日志。

### Step 12: server/handler.go — HandleTCP 用 scoped logger

```go
func (h *ProxyHandler) HandleTCP(conn net.Conn, addr string) {
    log := slog.With("remote", conn.RemoteAddr(), "addr", addr)
    // 所有日志用 log
}
```

注意：`quicStreamConn` 已有 `RemoteAddr()`，`smuxStreamConn` 需要补上。

涉及日志点（~8 个）。

### Step 13: server/server.go — acceptTCPLoop 补 remote

```go
slog.Debug("server: AcceptTCP got stream", "addr", addr, "remote", conn.RemoteAddr())
```

涉及 ~3 个日志点。

## 不在范围内

- 客户端日志（client-side quicUDPConn 等）— 客户端不需要 remote 归因
- 基础设施日志（server startup, cert gen, ECH 等）— 非请求级别
- UDID 注入 — 通过 auth 日志的 remote 字段自然关联
- echo.go — 仅 3 个 DEBUG 日志，收益低，暂不改

## TCP-WS smuxStreamConn remote 的解决

当前 `smuxStreamConn` 没有 `RemoteAddr()`（smux.Stream 不提供）。`HandleTCP` 调 `conn.RemoteAddr()` 会拿到 smux 内部地址而非客户端 IP。

解法：`handleStream` 中创建 `smuxStreamConn` 时传入 remote（从 connLog 获取或显式传参）。

```go
// tcpws.go handleStream — 当前
sc := &smuxStreamConn{Stream: stream, br: br}

// 改为
sc := &smuxStreamConn{Stream: stream, br: br, remoteAddr: connRemote}
```

`smuxStreamConn` 加 `remoteAddr net.Addr` + 覆盖 `RemoteAddr()` 方法。

## 验证

1. `cd k2 && go build ./server/ ./wire/` — 编译通过
2. `cd k2 && go vet ./server/ ./wire/` — 无 warning
3. `cd k2 && go test -short ./server/ ./wire/` — 所有测试通过
4. 部署到 jiangxi 节点（3.105.182.165），查看 k2v5 日志确认：
   - 每条日志都带 `remote=`
   - HandleUDP/HandleTCP 日志带 `sid=` / `streamID=`
   - `grep remote=<any-client-ip> k2s.log` 能找到完整链路
