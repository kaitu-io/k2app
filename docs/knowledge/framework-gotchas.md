# Framework Gotchas

Lessons learned from dependency integration.

## sing-tun gVisor Stack Connection Ownership (l4-architecture, 2026-02-12)

**Issue**: sing-tun's gVisor stack owns TCP/UDP connections. Handler must not close them prematurely.

**Symptom**: Connections die immediately, gVisor panics with "conn already closed".

**Root Cause**: Handler goroutine closes `net.Conn` while gVisor stack still expects to manage it.

**Solution**: Let handler goroutine own connection, defer close in handler:
```go
func (t *ClientTunnel) HandleTCP(conn net.Conn, dest string) {
    defer conn.Close()  // Safe: handler owns connection
    // ... forward data
}
```

**Validated in**: `provider/stack.go`, `core/tunnel.go`

**Source**: l4-architecture feature, T3 (Provider redesign)

---

## smux DefaultConfig vs Custom Config (l4-architecture, 2026-02-12)

**Issue**: smux has conservative defaults (256 streams, 4MB buffer). For VPN use, needs tuning.

**Default**:
```go
config := smux.DefaultConfig()
// MaxReceiveBuffer = 4MB
// MaxStreamBuffer = 64KB
```

**k2 tuning** (wire/tcpws.go):
```go
config := smux.DefaultConfig()
config.MaxReceiveBuffer = 16 * 1024 * 1024  // 16MB
config.MaxStreamBuffer = 1024 * 1024         // 1MB
config.KeepAliveInterval = 30 * time.Second
config.KeepAliveTimeout = 90 * time.Second
```

**Reason**: VPN traffic can burst (video, downloads). Small buffers cause backpressure.

**Validated in**: `wire/tcpws_test.go` (throughput tests with large transfers)

**Source**: l4-architecture feature, T2 (TCP-WS implementation)

---

## QUIC Datagram vs Stream for UDP (l4-architecture, 2026-02-12)

**Issue**: QUIC datagrams are unreliable, not all servers support them.

**Decision**: Try datagrams first, fall back to stream-based UDP framing.

```go
func (c *QUICClient) DialUDP(ctx, addr) (net.PacketConn, error) {
    if c.conn.ConnectionState().SupportsDatagrams {
        return c.newDatagramConn(addr), nil
    }
    // Fallback: stream-based UDP
    return c.newStreamUDPConn(addr), nil
}
```

**Validated in**: `wire/quic_test.go` (TestQUICClient_DialUDP_DatagramFallback)

**Trade-off**: Stream-based UDP adds latency but works everywhere.

**Source**: l4-architecture feature, T1 (QUIC implementation)

---

## net.Pipe Synchronous Blocking (l4-architecture, 2026-02-12)

**Issue**: `net.Pipe()` writes block until reads happen on the other end. Easy to deadlock.

**Symptom**: Test hangs, goroutines stuck in `Write()`.

**Example**:
```go
// WRONG: deadlocks if write exceeds buffer
local, remote := net.Pipe()
remote.Write(data)  // Blocks until local.Read() happens
// But local.Read() never called = deadlock
```

**Solution**: Always read/write in separate goroutines:
```go
local, remote := net.Pipe()
go func() {
    remote.Write(data)
}()
buf := make([]byte, len(data))
local.Read(buf)
```

**Validated in**: Multiple tests using pipe for connection mocking.

**Source**: Documented in AGENT.md, validated during testing.

---

## bufio.Reader Required for Stream Protocol (l4-architecture, 2026-02-12)

**Issue**: `ReadStreamHeader`/`ReadStreamResponse` use `io.ByteReader.ReadByte()`. `net.Conn` does not implement this.

**Symptom**: Compilation error `net.Conn does not implement io.ByteReader`.

**Root Cause**: HTTP/3 framing uses single-byte reads. `net.Conn` only has `Read([]byte)`.

**Solution**: Wrap connection with `bufio.Reader`:
```go
conn, _ := dialer.DialTCP(ctx, addr)
br := bufio.NewReader(conn)
header, err := wire.ReadStreamHeader(br)
```

**Validated in**: `wire/stream.go`, `wire/quic.go`, `wire/tcpws.go`

**Pattern**: Always wrap wire protocol connections before reading frames.

**Source**: l4-architecture feature, T1/T2 (wire implementations)

---

## k2rule GeoIP Path Requirement (l4-architecture, 2026-02-12)

**Issue**: k2rule v0.1.1-go treated GeoIP as optional but caused nil panics when rules used `GEOIP,CN`.

**Old behavior**:
```go
k2rule.LoadGeoIP(mmdbPath)  // If path empty, skip — but later Match() panics
```

**v0.3.0-go fix**:
```go
k2rule.Init(&k2rule.Config{
    Rules:     rulesYAML,
    GeoIPPath: mmdbPath,  // If empty + rules use GEOIP = error at init
})
```

**Validation**: Init fails fast with clear error if rules need GeoIP but path missing.

**Pattern**: Fail early at init, not at runtime during Match().

**Source**: l4-architecture feature, T6 (k2rule upgrade)

---

## sing-tun AutoRoute vs Manual Route (l4-architecture, 2026-02-12)

**Issue**: sing-tun can auto-configure routes or require manual setup. macOS/Linux differ.

**macOS**:
```go
tun.New(Options{
    AutoRoute: true,  // Uses system routing table
})
```

**Linux**:
```go
tun.New(Options{
    AutoRoute:   true,
    AutoRedirect: true,  // Also configures iptables
})
```

**Windows**:
```go
tun.New(Options{
    StrictRoute: true,  // Prevents DNS leak
})
```

**OpenWRT**:
```go
tun.New(Options{
    AutoRoute: true,
    // sing-tun detects OpenWRT, uses nftables instead of iptables
})
```

**Validated in**: `provider/tun_desktop.go` (platform-specific Options)

**Pattern**: Each platform has different route management. sing-tun abstracts this.

**Source**: l4-architecture feature, T3 (Provider cross-platform)
