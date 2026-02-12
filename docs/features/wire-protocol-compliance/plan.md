# Execution Plan: Wire Protocol Compliance

## Meta

| Field | Value |
|-------|-------|
| Feature | wire-protocol-compliance |
| Spec | docs/features/wire-protocol-compliance/spec.md |
| Complexity | Moderate (13 files, new abstractions) |
| Tasks | 6 (5 parallel + 1 integration) |
| Prerequisite | Go 1.24+ (current: Go 1.25) |

## Dependency Graph

```
T1 (ECH Core) ───────┐
T2 (Auth Lifecycle) ──┤
T3 (UDP Resilience) ──┼──→ T6 (Server Integration)
T4 (QUIC UDP FB) ─────┤
T5 (uTLS + WS) ──────┘
```

- **T1-T5**: All parallel (zero file overlap)
- **T6**: Depends on T1 + T5 (needs ECH detection + WS client)

## Tasks

---

### T1: ECH Core — Detection + Key Generation

**Branch**: `wpc/t1-ech-core`
**Files**: `wire/ech.go`, `wire/ech_test.go`, `config/config.go`
**AC Coverage**: AC1, AC2, AC3, AC4, AC7

#### Summary

Replace `PeekClientHelloSNI` with `PeekClientHello` that returns `ClientHelloInfo{SNI, HasECH}`. Add ECH HPKE key generation and loading. Add `ECHConfig` to `ServerConfig`.

#### TDD Steps

**RED**:
1. Write `TestPeekClientHello_WithECH` — build ClientHello with `0xfe0d` extension, assert `HasECH=true` (AC1)
2. Write `TestPeekClientHello_WithoutECH` — existing ClientHello, assert `HasECH=false` (AC2)
3. Write `TestPeekClientHello_BackwardCompat` — verify SNI extraction still works
4. Write `TestGenerateECHKey` — generate key pair, assert key bytes non-empty (AC3)
5. Write `TestLoadECHKey` — write key to temp file, load, verify round-trip (AC4)
6. Write `TestServerConfig_ECH` — parse YAML with `ech` section (AC7)

**GREEN**:
1. Define `ClientHelloInfo` struct with `SNI` and `HasECH` fields
2. Refactor `PeekClientHelloSNI` → `PeekClientHello`:
   - Same extension scanning loop, add `extType == 0xfe0d` check
   - Return `ClientHelloInfo` instead of bare string
3. Add `GenerateECHKey(publicName string) (tls.EncryptedClientHelloKey, []byte, error)`:
   - Uses Go 1.24+ `crypto/ecdh` P-256 + `crypto/tls` ECH key types
   - Returns private key struct + public ECH config bytes
4. Add `SaveECHKey(path string, key tls.EncryptedClientHelloKey, config []byte) error`
5. Add `LoadECHKey(path string) (tls.EncryptedClientHelloKey, []byte, error)`
6. Add `ECHConfig` struct to `config.go`:
   ```go
   type ECHConfig struct {
       KeyPath    string `yaml:"key_path"`
       PublicName string `yaml:"public_name"`
   }
   ```
7. Add `ECH ECHConfig` field to `ServerConfig`

**REFACTOR**:
1. Update `buildTestClientHello` to accept optional ECH extension flag
2. Deprecate `PeekClientHelloSNI` (keep as wrapper calling `PeekClientHello` for any external callers)
3. Add `EnsureECHKey(path, publicName string)` that auto-generates if missing, loads if present

#### Verification

```bash
go test ./wire/ -run TestPeekClientHello
go test ./wire/ -run TestGenerateECHKey
go test ./wire/ -run TestLoadECHKey
go test ./config/ -run TestServerConfig_ECH
```

---

### T2: Auth Lifecycle — Hybrid Renewal + Cleanup

**Branch**: `wpc/t2-auth-lifecycle`
**Files**: `wire/auth.go`, `wire/auth_test.go`
**AC Coverage**: AC15, AC16, AC17, AC18

#### Summary

Fix `respondTicket` to auto-renew when ticket < 50% TTL. Add `StartCleanup` background goroutine to `TicketStore`.

#### TDD Steps

**RED**:
1. Write `TestAuthHandler_TicketRenewal_Fresh` — ticket at 90% TTL, expect same token returned (AC15)
2. Write `TestAuthHandler_TicketRenewal_Expiring` — ticket at 20% TTL, expect NEW token returned (AC16)
3. Write `TestTicketStore_StartCleanup_Tickets` — expired ticket removed after cleanup interval (AC17)
4. Write `TestTicketStore_StartCleanup_GraceTokens` — expired grace token removed after cleanup interval (AC18)

**GREEN**:
1. Modify `respondTicket` signature to accept `deviceID`:
   ```go
   func (h *AuthHandler) respondTicket(w http.ResponseWriter, token, deviceID string) {
       remaining := h.store.Remaining(token)
       total := int(h.store.ticketTTL.Seconds())
       if remaining < total/2 {
           // Auto-renew
           ticket, err := h.store.Issue(deviceID, true)
           // ... respond with new ticket
       }
       // ... respond with current ticket
   }
   ```
2. Expose `ticketTTL` as needed (or add `TTLSeconds() int` method)
3. Add `StartCleanup(interval time.Duration)` to `TicketStore`:
   ```go
   func (s *TicketStore) StartCleanup(interval time.Duration) {
       go func() {
           ticker := time.NewTicker(interval)
           for range ticker.C {
               s.cleanup()
           }
       }()
   }

   func (s *TicketStore) cleanup() {
       s.mu.Lock()
       defer s.mu.Unlock()
       now := time.Now()
       for token, ticket := range s.byToken {
           if now.After(ticket.ExpiresAt) {
               delete(s.byToken, token)
               // also clean byDevice if this was the current token
           }
       }
       for token, expiry := range s.graceTokens {
           if now.After(expiry) {
               delete(s.graceTokens, token)
           }
       }
   }
   ```

**REFACTOR**:
1. Extract renewal threshold as constant (`ticketRenewalThreshold = 0.5`)
2. Ensure cleanup goroutine is stoppable (accept `context.Context` or `chan struct{}`)

#### Verification

```bash
go test ./wire/ -run TestAuthHandler_TicketRenewal
go test ./wire/ -run TestTicketStore_StartCleanup
```

---

### T3: UDP Resilience — MaxSize + Cleanup

**Branch**: `wpc/t3-udp-resilience`
**Files**: `wire/udp.go`, `wire/udp_test.go`
**AC Coverage**: AC19, AC20

#### Summary

Enforce `maxSize` in `UDPSessionMap.Store`. Add `StartCleanup` background goroutine.

#### TDD Steps

**RED**:
1. Write `TestUDPSessionMap_MaxSize` — create map with maxSize=2, store 3 sessions, third rejected (AC19)
2. Write `TestUDPSessionMap_StartCleanup` — store session with short idle TTL, verify removed after cleanup interval (AC20)

**GREEN**:
1. Modify `Store` to return `bool`:
   ```go
   func (m *UDPSessionMap) Store(sessionID uint32, addr string) bool {
       m.mu.Lock()
       defer m.mu.Unlock()
       if _, exists := m.sessions[sessionID]; !exists && len(m.sessions) >= m.maxSize {
           return false // at capacity
       }
       m.sessions[sessionID] = &udpSession{addr: addr, lastSeen: time.Now()}
       return true
   }
   ```
   Note: updating an existing session always succeeds (not a new entry).
2. Add `StartCleanup(interval time.Duration)`:
   ```go
   func (m *UDPSessionMap) StartCleanup(interval time.Duration) {
       go func() {
           ticker := time.NewTicker(interval)
           for range ticker.C {
               m.Cleanup()
           }
       }()
   }
   ```

**REFACTOR**:
1. Make cleanup stoppable (context or done channel)
2. Update callers of `Store` to handle `false` return (log warning)

#### Verification

```bash
go test ./wire/ -run TestUDPSessionMap_MaxSize
go test ./wire/ -run TestUDPSessionMap_StartCleanup
```

---

### T4: QUIC UDP Stream Fallback

**Branch**: `wpc/t4-quic-udp-fallback`
**Files**: `wire/quic.go`, `wire/quic_test.go`
**AC Coverage**: AC21, AC22

#### Summary

When QUIC datagrams are not supported, `QUICClient.DialUDP` falls back to opening a QUIC stream with smux-framed UDP. Server `QUICServer.handleStream` accepts `StreamTypeUDP`.

#### TDD Steps

**RED**:
1. Write `TestQUICServer_HandleStream_UDP` — send stream with `StreamTypeUDP` header + smux-framed UDP, verify server dispatches to UDPHandler (AC22)
2. Write `TestQUICClient_DialUDP_StreamFallback` — mock QUIC conn that returns error on `SendDatagram`, verify stream-based path used (AC21)

**GREEN**:
1. In `QUICServer.handleStream`, extend the `streamType` switch:
   ```go
   switch streamType {
   case StreamTypeTCP:
       // existing TCP handling
   case StreamTypeUDP:
       s.handleUDPStream(qconn, stream, br)
   default:
       stream.Close()
   }
   ```
2. Add `QUICServer.handleUDPStream` — reuse same pattern as `TCPWSServer.handleUDPStream` (read smux-framed UDP, dispatch to handler)
3. In `QUICClient.DialUDP`, detect datagram support:
   ```go
   func (c *QUICClient) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
       conn, err := c.connect(ctx)
       if err != nil {
           return nil, err
       }
       // Try datagram path first
       if c.datagramsSupported(conn) {
           return c.dialUDPDatagram(conn, addr)
       }
       // Fallback: stream-based UDP
       return c.dialUDPStream(ctx, conn, addr)
   }
   ```
4. `dialUDPStream` opens a QUIC stream, writes `StreamTypeUDP` header, returns a `PacketConn` backed by smux-framed UDP reads/writes on that stream (similar to `smuxUDPConn`)

**REFACTOR**:
1. Extract shared UDP stream mux logic between QUIC stream fallback and TCP-WS into a common helper if code duplication is significant
2. Add `datagramsSupported` check (try `SendDatagram` with empty or check QUIC transport params)

#### Verification

```bash
go test ./wire/ -run TestQUICServer_HandleStream_UDP
go test ./wire/ -run TestQUICClient_DialUDP_StreamFallback
```

---

### T5: uTLS + WebSocket Client

**Branch**: `wpc/t5-utls-websocket`
**Files**: `wire/tcpws.go`, `wire/tcpws_test.go`
**AC Coverage**: AC8, AC9, AC10, AC11, AC13
**New Dep**: WebSocket library (`nhooyr.io/websocket` or `gorilla/websocket`)

#### Summary

Replace `tls.Client()` with `utls.UClient()` in `TCPWSClient.connect`. Add WebSocket upgrade to `/k2v5/tunnel` after TLS handshake.

#### TDD Steps

**RED**:
1. Write `TestTCPWSClient_uTLS_Fingerprint` — verify `utls.UClient` is used, not `tls.Client` (AC8, AC9)
   - Use a test TLS server that inspects the ClientHello and verifies fingerprint
   - Test with `chrome`, `firefox`, `safari` fingerprint values
2. Write `TestTCPWSClient_uTLS_WithECH` — verify ECH config list is passed through uTLS (AC10)
3. Write `TestTCPWSClient_WSUpgrade` — verify client sends WebSocket upgrade to `/k2v5/tunnel` (AC11)
   - Test server accepts WS upgrade, verifies path is `/k2v5/tunnel`
4. Write `TestTCPWSClient_FullFlow_WS` — TCP+uTLS+WS+smux, open stream, echo data (AC13)

**GREEN**:
1. Add WebSocket library dependency:
   ```bash
   go get nhooyr.io/websocket  # or gorilla/websocket — choose based on net.Conn wrapper availability
   ```
2. Modify `TCPWSClient.connect()`:
   ```go
   // 1. TCP dial (unchanged)
   conn, err = d.DialContext(ctx, "tcp", c.serverAddr)

   // 2. uTLS handshake (replaces tls.Client)
   var clientHelloID utls.ClientHelloID
   if c.tlsConfig != nil {
       tlsCfg = c.tlsConfig.Clone()
       clientHelloID = utls.HelloChrome_120_PQ // default for test
   } else {
       tlsCfg, clientHelloID, err = BuildClientTLSConfig(c.cfg)
   }
   uconn := utls.UClient(conn, &utls.Config{
       ServerName:                     tlsCfg.ServerName,
       InsecureSkipVerify:             tlsCfg.InsecureSkipVerify,
       EncryptedClientHelloConfigList: tlsCfg.EncryptedClientHelloConfigList,
   }, clientHelloID)
   if err := uconn.HandshakeContext(ctx); err != nil {
       conn.Close()
       return nil, fmt.Errorf("wire: uTLS handshake: %w", err)
   }
   conn = uconn

   // 3. WebSocket upgrade
   wsConn, err := websocketUpgrade(ctx, conn, c.cfg.Host)
   if err != nil {
       conn.Close()
       return nil, fmt.Errorf("wire: WS upgrade: %w", err)
   }
   conn = wsConn // net.Conn-compatible wrapper

   // 4. smux session (unchanged)
   sess, err := smux.Client(conn, DefaultSmuxConfig())
   ```
3. Implement `websocketUpgrade(ctx, conn, host) (net.Conn, error)`:
   - Sends WS upgrade request to `/k2v5/tunnel`
   - Returns `net.Conn`-compatible wrapper around WS connection

**REFACTOR**:
1. Extract uTLS config building from connect() into helper
2. Make WS path configurable (default `/k2v5/tunnel`)
3. Handle `SetTestConn` — when test conn is set, skip both uTLS and WS (direct smux)

#### Verification

```bash
go test ./wire/ -run TestTCPWSClient_uTLS
go test ./wire/ -run TestTCPWSClient_WS
go test ./wire/ -run TestTCPWSClient_FullFlow_WS
```

---

### T6: Server Integration — ECH Routing + WS Accept

**Branch**: `wpc/t6-server-integration`
**Files**: `server/server.go`, `server/server_test.go`
**Depends on**: T1 (ECH detection + key loading), T5 (WS client for integration testing)
**AC Coverage**: AC5, AC6, AC12, AC14

#### Summary

Wire up ECH-based routing in `handleRawTCP`. Add WebSocket upgrade accept in `handleK2V5`. Load ECH keys on server startup.

#### TDD Steps

**RED**:
1. Write `TestServer_ECHRouting_ECHConnection` — client with ECH → handled as k2v5 (AC5)
2. Write `TestServer_ECHRouting_NonECHConnection` — client without ECH → reverse proxied (AC6)
3. Write `TestServer_WSUpgrade` — client sends WS upgrade to `/k2v5/tunnel`, server accepts (AC12)
4. Write `TestServer_AuthWithoutWS` — POST /k2v5/auth still works without WS upgrade (AC14)

**GREEN**:
1. Modify `New()` to load ECH keys:
   ```go
   if cfg.ECH.KeyPath != "" {
       echKey, echConfig, err := wire.EnsureECHKey(cfg.ECH.KeyPath, cfg.ECH.PublicName)
       if err != nil {
           return nil, fmt.Errorf("ECH key: %w", err)
       }
       s.TLSConfig.EncryptedClientHelloKeys = []tls.EncryptedClientHelloKey{echKey}
       s.echConfig = echConfig // store for client distribution
   }
   ```
2. Modify `handleRawTCP` to use `PeekClientHello`:
   ```go
   info, err := wire.PeekClientHello(hello)
   if err != nil {
       conn.Close()
       return
   }
   if !info.HasECH {
       // Non-ECH → reverse proxy to outer SNI website
       wire.ReverseProxy(conn, hello, info.SNI+":443")
       return
   }
   // ECH → TLS handshake with ECH decryption → k2v5
   wrapped := &bufferedConn{Conn: conn, br: br}
   s.handleK2V5(wrapped)
   ```
3. Modify `handleK2V5` to handle WS upgrade:
   ```go
   if isHTTPMethod(first) {
       // Check if it's WS upgrade or auth
       if isWSUpgrade(first, br) {
           wsConn := acceptWebSocket(tlsConn, br)
           s.tcpwsSrv.HandleConn(wsConn)
           return
       }
       s.handleAuth(&bufferedConn{Conn: tlsConn, br: br})
       return
   }
   ```
4. Start cleanup goroutines in `Run()`:
   ```go
   s.store.StartCleanup(5 * time.Minute)
   ```

**REFACTOR**:
1. Extract routing decision into `routeConnection(info ClientHelloInfo, conn ...)` for testability
2. Add logging for routing decisions (ECH→k2v5, no-ECH→reverse-proxy)
3. Integration test with real ECH key: client with ECH config → server decrypts → inner SNI verified

#### Verification

```bash
go test ./server/ -run TestServer_ECHRouting
go test ./server/ -run TestServer_WS
go test ./server/ -run TestServer_AuthWithoutWS
```

---

## AC Coverage Map

| AC | Test | Task |
|----|------|------|
| AC1 | `TestPeekClientHello_WithECH` | T1 |
| AC2 | `TestPeekClientHello_WithoutECH` | T1 |
| AC3 | `TestGenerateECHKey` | T1 |
| AC4 | `TestLoadECHKey` | T1 |
| AC5 | `TestServer_ECHRouting_ECHConnection` | T6 |
| AC6 | `TestServer_ECHRouting_NonECHConnection` | T6 |
| AC7 | `TestServerConfig_ECH` | T1 |
| AC8 | `TestTCPWSClient_uTLS_Fingerprint` | T5 |
| AC9 | `TestTCPWSClient_uTLS_Fingerprint` (multi-fp) | T5 |
| AC10 | `TestTCPWSClient_uTLS_WithECH` | T5 |
| AC11 | `TestTCPWSClient_WSUpgrade` | T5 |
| AC12 | `TestServer_WSUpgrade` | T6 |
| AC13 | `TestTCPWSClient_FullFlow_WS` | T5 |
| AC14 | `TestServer_AuthWithoutWS` | T6 |
| AC15 | `TestAuthHandler_TicketRenewal_Fresh` | T2 |
| AC16 | `TestAuthHandler_TicketRenewal_Expiring` | T2 |
| AC17 | `TestTicketStore_StartCleanup_Tickets` | T2 |
| AC18 | `TestTicketStore_StartCleanup_GraceTokens` | T2 |
| AC19 | `TestUDPSessionMap_MaxSize` | T3 |
| AC20 | `TestUDPSessionMap_StartCleanup` | T3 |
| AC21 | `TestQUICClient_DialUDP_StreamFallback` | T4 |
| AC22 | `TestQUICServer_HandleStream_UDP` | T4 |

All 22 ACs covered. Zero gaps.

## Execution Order

### Phase 1 (Parallel): T1, T2, T3, T4, T5

All five tasks have zero file overlap. Execute simultaneously in separate worktrees.

| Task | Branch | Files | Est. Complexity |
|------|--------|-------|----------------|
| T1 | `wpc/t1-ech-core` | wire/ech.go, wire/ech_test.go, config/config.go | High (new Go 1.24 APIs) |
| T2 | `wpc/t2-auth-lifecycle` | wire/auth.go, wire/auth_test.go | Low |
| T3 | `wpc/t3-udp-resilience` | wire/udp.go, wire/udp_test.go | Low |
| T4 | `wpc/t4-quic-udp-fallback` | wire/quic.go, wire/quic_test.go | Medium |
| T5 | `wpc/t5-utls-websocket` | wire/tcpws.go, wire/tcpws_test.go | High (new dep + 2 features) |

### Phase 2 (Sequential): T6

After T1 and T5 merge to master.

| Task | Branch | Files | Est. Complexity |
|------|--------|-------|----------------|
| T6 | `wpc/t6-server-integration` | server/server.go, server/server_test.go | Medium |

## Merge Order

```
T2 → master  (independent, merge when ready)
T3 → master  (independent, merge when ready)
T4 → master  (independent, merge when ready)
T1 → master  (independent, merge when ready)
T5 → master  (independent, merge when ready)
T6 → master  (after T1 + T5 merged)
```

T2, T3, T4 are lowest risk — merge first to reduce outstanding branches.
