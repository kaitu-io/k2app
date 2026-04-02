# k2p ICE Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace k2p's custom NAT traversal (STUN/punch/relay) with standard ICE + TURN, keeping QUIC data plane.

**Architecture:** pion/ice handles all NAT traversal (STUN + TURN + connectivity checks). pion/turn embedded in k2s provides authenticated relay. QUIC runs over `ice.Conn` wrapped as `net.PacketConn`. Peer auth and k2v5 framing unchanged.

**Tech Stack:** pion/ice v4, pion/turn v4, apernet/quic-go (existing), gorilla/websocket (existing)

**Spec:** `docs/superpowers/specs/2026-04-01-k2p-ice-redesign.md`

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `k2p/ice.go` | ICE agent creation, `icePacketConn` adapter, STUN/TURN URL builder |
| `k2p/ice_test.go` | PoC: QUIC over ice.Conn on loopback, packet boundary verification |
| `signal/turn.go` | Embedded TURN server, TURN REST credential generation |
| `signal/turn_test.go` | Credential generation/verification, TURN allocation on loopback |
| `signal/session.go` | WS session routing (session ID ↔ Home/Away WS pairing) |
| `signal/session_test.go` | Multi-session routing, cleanup on disconnect |

### Modify Files

| File | Changes |
|------|---------|
| `k2p/config.go` | Remove `STUNServers`, `DirectDialer`, `RelayServer`. Add `TURNURLs`, `TURNUsername`, `TURNPassword` |
| `k2p/home.go` | Replace punch with ICE Accept. Remove NAT classify. Session-based candidate exchange |
| `k2p/away.go` | Replace punch+relay with ICE Dial. Remove relay fallback. Candidate exchange |
| `k2p/signal.go` | WS messages become candidate exchange. Remove `RequestConnect`. Add `TURNCreds` to `PeerInfo`. Add `ExchangeCandidates()` |
| `signal/handler.go` | Remove `/k2p/connect` and `/k2p/relay`. WS handler becomes session-based candidate router |
| `signal/store.go` | Remove `Addr`/`NATType` from `PeerEntry`/`PeerSnapshot` |
| `signal/types.go` | Simplify `RegisterRequest`. Add `TURNCredentials`, `WSMessage` types |
| `cmd/k2/peer_setup.go` | Remove STUN-related code |
| `cmd/k2/peer_run.go` | No changes needed (uses k2p.Listen which handles ICE internally) |
| `test/k2p_e2e_test.go` | Rewrite for ICE-based flow |
| `CLAUDE.md` | Update lock ordering graph (remove relay locks, note ICE agent) |

### Delete Files

| File | Lines | Replaced by |
|------|-------|-------------|
| `k2p/nat.go` | 103 | pion/ice |
| `k2p/nat_test.go` | 68 | pion/ice |
| `k2p/stun.go` | 75 | pion/ice |
| `k2p/stun_test.go` | 98 | pion/ice |
| `k2p/punch.go` | 228 | pion/ice |
| `k2p/punch_test.go` | 120 | pion/ice |
| `k2p/relay.go` | 79 | pion/turn |
| `k2p/relay_test.go` | 77 | pion/turn |
| `k2p/servers.go` | 16 | ICE default URLs in `ice.go` |
| `k2p/servers_test.go` | 21 | — |
| `signal/relay.go` | 152 | pion/turn |
| `signal/relay_test.go` | 76 | pion/turn |

---

## Confidence Gaps & Verification Strategy

Each gap below is addressed by a specific task before dependent code is written.

| Gap | Confidence | Addressed in |
|-----|-----------|--------------|
| `ice.Conn` → `net.PacketConn` wrapping preserves QUIC datagram boundaries | 8.5/10 | **Task 1** (PoC test) |
| QUIC handshake + streams work over `icePacketConn` | 8/10 | **Task 1** (PoC test) |
| pion/turn embedded server actually allocates relay on loopback | 9/10 | **Task 3** (PoC test) |
| WS session routing handles concurrent Aways + cleanup | 9/10 | **Task 5** (unit tests) |
| ICE agent lifecycle (close agent, no goroutine leaks) | 9/10 | **Task 7** (goleak test) |

---

### Task 1: PoC — QUIC over ice.Conn on Loopback

**Why first:** This is the highest-risk integration point. If `ice.Conn` → `PacketConn` → QUIC doesn't work, the entire design fails. Verify before writing anything else.

**Files:**
- Create: `k2p/ice.go`
- Create: `k2p/ice_test.go`

- [ ] **Step 1: Add pion/ice dependency**

```bash
cd /Users/david/projects/kaitu-io/k2
go get github.com/pion/ice/v4@v4.2.2
```

- [ ] **Step 2: Write `icePacketConn` adapter in `k2p/ice.go`**

```go
package k2p

import (
	"net"
	"time"

	"github.com/pion/ice/v4"
)

// icePacketConn adapts *ice.Conn (net.Conn) to net.PacketConn for QUIC transport.
// ice.Conn is backed by UDP — Read() preserves datagram boundaries.
// Same pattern as RelayConn (WriteTo ignores addr, ReadFrom returns synthetic addr).
type icePacketConn struct {
	conn     *ice.Conn
	peerAddr net.Addr
}

func newICEPacketConn(conn *ice.Conn) *icePacketConn {
	return &icePacketConn{
		conn:     conn,
		peerAddr: conn.RemoteAddr(),
	}
}

func (c *icePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, err := c.conn.Read(p)
	return n, c.peerAddr, err
}

func (c *icePacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	return c.conn.Write(p)
}

func (c *icePacketConn) Close() error                       { return c.conn.Close() }
func (c *icePacketConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *icePacketConn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *icePacketConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *icePacketConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }
```

- [ ] **Step 3: Write PoC test — QUIC dial/accept over ICE on loopback**

This test creates two ICE agents on loopback, connects them, wraps with `icePacketConn`, and runs a QUIC handshake + stream read/write. This validates the entire integration chain.

```go
// k2p/ice_test.go
package k2p

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"

	"github.com/apernet/quic-go"
	"github.com/kaitu-io/k2/wire"
	"github.com/pion/ice/v4"
)

// TestICE_QUICOverLoopback is the critical PoC: two ICE agents on loopback,
// QUIC handshake + stream round-trip over ice.Conn wrapped as PacketConn.
func TestICE_QUICOverLoopback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create two ICE agents (no STUN/TURN — host candidates only on loopback).
	agentA, err := ice.NewAgent(&ice.AgentConfig{
		NetworkTypes: []ice.NetworkType{ice.NetworkTypeUDP4},
	})
	if err != nil {
		t.Fatalf("create agent A: %v", err)
	}
	defer agentA.Close()

	agentB, err := ice.NewAgent(&ice.AgentConfig{
		NetworkTypes: []ice.NetworkType{ice.NetworkTypeUDP4},
	})
	if err != nil {
		t.Fatalf("create agent B: %v", err)
	}
	defer agentB.Close()

	// Exchange candidates directly (no signaling server needed for test).
	agentA.OnCandidate(func(c ice.Candidate) {
		if c != nil {
			agentB.AddRemoteCandidate(c)
		}
	})
	agentB.OnCandidate(func(c ice.Candidate) {
		if c != nil {
			agentA.AddRemoteCandidate(c)
		}
	})

	// Gather candidates.
	if err := agentA.GatherCandidates(); err != nil {
		t.Fatalf("gather A: %v", err)
	}
	if err := agentB.GatherCandidates(); err != nil {
		t.Fatalf("gather B: %v", err)
	}

	// Get credentials.
	ufragA, pwdA, _ := agentA.GetLocalUserCredentials()
	ufragB, pwdB, _ := agentB.GetLocalUserCredentials()

	// Connect: A dials, B accepts.
	connACh := make(chan *ice.Conn, 1)
	connBCh := make(chan *ice.Conn, 1)
	errCh := make(chan error, 2)

	go func() {
		conn, err := agentA.Dial(ctx, ufragB, pwdB)
		if err != nil {
			errCh <- err
			return
		}
		connACh <- conn
	}()
	go func() {
		conn, err := agentB.Accept(ctx, ufragA, pwdA)
		if err != nil {
			errCh <- err
			return
		}
		connBCh <- conn
	}()

	var connA, connB *ice.Conn
	for i := 0; i < 2; i++ {
		select {
		case c := <-connACh:
			connA = c
		case c := <-connBCh:
			connB = c
		case err := <-errCh:
			t.Fatalf("ICE connect: %v", err)
		case <-ctx.Done():
			t.Fatal("ICE connect timeout")
		}
	}

	// Wrap as PacketConn.
	pcA := newICEPacketConn(connA)
	pcB := newICEPacketConn(connB)

	// QUIC: B listens, A dials.
	certPair, err := wire.GenerateCertPair()
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	trB := &quic.Transport{Conn: pcB}
	defer trB.Close()
	tlsServerCfg := &tls.Config{
		NextProtos:   []string{"h3"},
		Certificates: []tls.Certificate{certPair.EC},
	}
	quicLn, err := trB.Listen(tlsServerCfg, wire.DefaultQUICConfig())
	if err != nil {
		t.Fatalf("QUIC listen: %v", err)
	}
	defer quicLn.Close()

	// Server: accept + read stream.
	serverDone := make(chan string, 1)
	go func() {
		qConn, err := quicLn.Accept(ctx)
		if err != nil {
			return
		}
		stream, err := qConn.AcceptStream(ctx)
		if err != nil {
			return
		}
		data, _ := io.ReadAll(stream)
		serverDone <- string(data)
	}()

	// Client: dial + write stream.
	trA := &quic.Transport{Conn: pcA}
	defer trA.Close()
	tlsClientCfg := &tls.Config{
		NextProtos:         []string{"h3"},
		InsecureSkipVerify: true,
	}
	// Use synthetic addr — icePacketConn.WriteTo ignores addr.
	qConn, err := trA.Dial(ctx, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, tlsClientCfg, wire.DefaultQUICConfig())
	if err != nil {
		t.Fatalf("QUIC dial over ICE: %v", err)
	}

	stream, err := qConn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	testPayload := "hello-quic-over-ice"
	stream.Write([]byte(testPayload))
	stream.Close()

	select {
	case got := <-serverDone:
		if got != testPayload {
			t.Errorf("server received %q, want %q", got, testPayload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to receive data")
	}

	qConn.CloseWithError(0, "test done")
}
```

- [ ] **Step 4: Run the PoC test**

```bash
go test -v -run TestICE_QUICOverLoopback ./k2p/
```

Expected: PASS — confirms QUIC handshake + stream works over ice.Conn.

**If this test fails:** The integration is broken and we need to investigate `ice.Conn.Read()` behavior. Check if Read() returns full datagrams or fragments. Check if `ice.Conn` implements additional interfaces that QUIC's transport probes for. Do NOT proceed to Task 2 until this passes.

- [ ] **Step 5: Commit**

```bash
git add k2p/ice.go k2p/ice_test.go go.mod go.sum
git commit -m "feat(k2p): PoC — icePacketConn adapter + QUIC-over-ICE loopback test"
```

---

### Task 2: Delete Custom NAT/Punch/Relay Code

**Why now:** With the PoC confirmed, remove the old code to avoid confusion. Tests that depend on deleted code will break — we'll rewrite them in later tasks.

**Files:**
- Delete: `k2p/nat.go`, `k2p/nat_test.go`
- Delete: `k2p/stun.go`, `k2p/stun_test.go`
- Delete: `k2p/punch.go`, `k2p/punch_test.go`
- Delete: `k2p/relay.go`, `k2p/relay_test.go`
- Delete: `k2p/servers.go`, `k2p/servers_test.go`
- Delete: `signal/relay.go`, `signal/relay_test.go`
- Modify: `k2p/home.go` — stub out to compile
- Modify: `k2p/away.go` — stub out to compile
- Modify: `k2p/config.go` — remove deleted-type references
- Modify: `signal/handler.go` — remove relay + connect routes

- [ ] **Step 1: Delete the files**

```bash
cd /Users/david/projects/kaitu-io/k2
rm k2p/nat.go k2p/nat_test.go
rm k2p/stun.go k2p/stun_test.go
rm k2p/punch.go k2p/punch_test.go
rm k2p/relay.go k2p/relay_test.go
rm k2p/servers.go k2p/servers_test.go
rm signal/relay.go signal/relay_test.go
```

- [ ] **Step 2: Update `k2p/config.go` — remove deleted fields**

Remove `STUNServers`, `DirectDialer`, `RelayServer` fields. Add ICE/TURN fields:

```go
type Config struct {
	HomeUDID     string
	SignalServer string
	Token        string
	WireCfg      wire.Config
	TicketStore  *wire.TicketStore
	TLSCert      *tls.Certificate
	PeerSecret   string
	Pin          string
	Networks     []string
	DNS          string
	RegToken     string
	SecretHash   string

	// ICE/TURN configuration (populated from signal lookup).
	TURNURLs     []string // TURN server URLs
	TURNUsername string   // TURN REST API username
	TURNPassword string   // TURN REST API password
}
```

- [ ] **Step 3: Stub `k2p/home.go` to compile**

Replace `Listen()` and `handleAwayConnection()` with TODO stubs that return errors:

```go
func Listen(ctx context.Context, cfg Config) (<-chan wire.Listener, error) {
	return nil, fmt.Errorf("k2p: Listen not yet reimplemented with ICE")
}
```

Delete `home_test.go` (tests depend on deleted punch types):
```bash
rm k2p/home_test.go
```

- [ ] **Step 4: Stub `k2p/away.go` to compile**

Replace `Dial()` with TODO stub:

```go
func Dial(ctx context.Context, cfg Config) (*ConnectResult, error) {
	return nil, fmt.Errorf("k2p: Dial not yet reimplemented with ICE")
}
```

Remove all helper functions (`dialQUICOverPunch`, `dialQUICOverRelay`, `buildPinTLSConfig`, `verifyPins`, `doPeerAuth`). Keep `ConnectResult` type and move `buildPinTLSConfig`, `verifyPins`, `doPeerAuth` to a new location since they're still needed.

Actually — `buildPinTLSConfig`, `verifyPins`, `doPeerAuth` are transport-independent (work at QUIC level, not punch level). **Append them to the existing `k2p/auth.go`** file (which already has `WriteAuthFrame`, `ReadAuthFrame`, `HashSecret`, `VerifySecret`, `PeerAuthListener`). Keep `ConnectResult` in `away.go`. The functions to move are:

```go
// Append to k2p/auth.go:

// buildPinTLSConfig creates a TLS config with optional certificate pin verification.
func buildPinTLSConfig(cfg Config) *tls.Config { /* copy from away.go */ }

// verifyPins checks that at least one certificate's SPKI hash matches an expected pin.
func verifyPins(rawCerts [][]byte, expected [][32]byte) error { /* copy from away.go */ }

// doPeerAuth sends a peer auth frame on a new QUIC stream and reads the response.
func doPeerAuth(ctx context.Context, conn *quic.Conn, secret string) error { /* copy from away.go */ }
```

Delete `away_test.go` (tests depend on deleted punch types):
```bash
rm k2p/away_test.go
```

- [ ] **Step 5: Update `signal/handler.go` — remove deleted routes**

Remove these lines from `NewHandler`:
```go
h.mux.HandleFunc("POST /k2p/connect", h.handleConnect)
h.mux.HandleFunc("GET /k2p/relay", h.relay.HandleRelay)
```

Remove `h.relay = NewRelay()` and the `relay *Relay` field from `Handler`.

Delete `handleConnect` method entirely.

- [ ] **Step 6: Update `signal/types.go` — remove `ConnectRequest` and `PunchNotify`**

Delete `ConnectRequest` and `PunchNotify` types from `signal/types.go`. These are replaced by WS session messages.

Note: `k2p/signal.go` has its own `ConnectRequest` and `PunchNotify` types — those will be updated in Task 6.

- [ ] **Step 7: Verify everything compiles**

```bash
go build ./k2p/... ./signal/... ./cmd/k2/...
```

Fix any remaining compile errors (imports of deleted types, etc.).

- [ ] **Step 8: Run existing tests that should still pass**

```bash
go test -short ./k2p/... ./signal/...
```

Expected: `k2p/ice_test.go` passes, `k2p/url_test.go` passes, `k2p/auth_test.go` passes, `k2p/networks_test.go` passes, `signal/store_test.go` passes, `signal/handler_test.go` has some failures (connect/relay tests reference deleted routes — those tests need updating in handler_test.go).

Fix `signal/handler_test.go`:
- `TestHandler_ConcurrentConnect` — delete (tests deleted endpoint)
- Other tests — keep (claim/register/lookup still exist)

Update `TestHandler_ClaimAndRegisterAndLookup` — remove assertions on `Addr` and `NATType` (these fields will be removed from `RegisterRequest` in Task 4).

Actually, leave the `Addr`/`NATType` field removal for Task 4 where we modify the store. For now, just ensure compilation.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "refactor(k2p): delete custom NAT/punch/relay code (replaced by ICE+TURN)"
```

---

### Task 3: Embedded TURN Server in Signal Package

**Files:**
- Create: `signal/turn.go`
- Create: `signal/turn_test.go`

- [ ] **Step 1: Add pion/turn dependency**

```bash
go get github.com/pion/turn/v4@v4.1.4
```

- [ ] **Step 2: Write failing test — TURN credential generation**

```go
// signal/turn_test.go
package signal

import (
	"testing"
	"time"
)

func TestTURNCredentials_Generate(t *testing.T) {
	secret := "test-shared-secret"
	udid := "home-abc"

	creds := GenerateTURNCredentials(secret, udid, 1*time.Hour)

	if creds.Username == "" {
		t.Fatal("expected non-empty username")
	}
	if creds.Password == "" {
		t.Fatal("expected non-empty password")
	}
	if creds.TTL != 3600 {
		t.Errorf("TTL = %d, want 3600", creds.TTL)
	}
}

func TestTURNCredentials_Validate(t *testing.T) {
	secret := "test-shared-secret"
	udid := "home-abc"

	creds := GenerateTURNCredentials(secret, udid, 1*time.Hour)

	// Valid credentials should pass.
	if !ValidateTURNCredentials(secret, creds.Username, creds.Password) {
		t.Error("expected valid credentials to pass")
	}

	// Wrong password should fail.
	if ValidateTURNCredentials(secret, creds.Username, "wrong") {
		t.Error("expected wrong password to fail")
	}

	// Wrong secret should fail.
	if ValidateTURNCredentials("other-secret", creds.Username, creds.Password) {
		t.Error("expected wrong secret to fail")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test -run TestTURN -v ./signal/
```

Expected: FAIL (functions not defined).

- [ ] **Step 4: Implement TURN credential generation**

```go
// signal/turn.go
package signal

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TURNCredentials holds temporary TURN credentials for ICE relay.
type TURNCredentials struct {
	URLs     []string `json:"urls"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	TTL      int      `json:"ttl"` // seconds
}

// GenerateTURNCredentials creates time-limited TURN REST API credentials.
// Format: username = "expiry_unix:udid", password = base64(HMAC-SHA1(username, secret)).
// This is the standard TURN REST API format (RFC-compliant, same as coturn, Twilio, Xirsys).
func GenerateTURNCredentials(sharedSecret, udid string, ttl time.Duration) TURNCredentials {
	expiry := time.Now().Add(ttl).Unix()
	username := fmt.Sprintf("%d:%s", expiry, udid)

	mac := hmac.New(sha1.New, []byte(sharedSecret))
	mac.Write([]byte(username))
	password := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return TURNCredentials{
		Username: username,
		Password: password,
		TTL:      int(ttl.Seconds()),
	}
}

// ValidateTURNCredentials verifies TURN REST API credentials against the shared secret.
// Returns false if expired or HMAC mismatch.
func ValidateTURNCredentials(sharedSecret, username, password string) bool {
	// Parse expiry from username ("expiry:udid").
	parts := strings.SplitN(username, ":", 2)
	if len(parts) != 2 {
		return false
	}
	expiry, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix() > expiry {
		return false
	}

	// Recompute HMAC.
	mac := hmac.New(sha1.New, []byte(sharedSecret))
	mac.Write([]byte(username))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(password), []byte(expected))
}

// TURNAuthHandler returns a pion/turn AuthHandler that validates TURN REST API
// credentials using the shared secret. pion/turn's AuthHandler signature is:
//   func(username, realm string, srcAddr net.Addr) (key []byte, ok bool)
// It must return turn.GenerateAuthKey(username, realm, password) where password
// is recomputed from the username via HMAC.
func TURNAuthHandler(sharedSecret string) func(username, realm string, srcAddr net.Addr) ([]byte, bool) {
	return func(username, realm string, srcAddr net.Addr) ([]byte, bool) {
		// Recompute password from username (TURN REST API: password = HMAC-SHA1(username, secret)).
		mac := hmac.New(sha1.New, []byte(sharedSecret))
		mac.Write([]byte(username))
		password := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		// Check expiry from username ("expiry:udid").
		parts := strings.SplitN(username, ":", 2)
		if len(parts) != 2 {
			return nil, false
		}
		expiry, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || time.Now().Unix() > expiry {
			return nil, false
		}

		// Return MD5(username:realm:password) — the key format pion/turn expects.
		return turn.GenerateAuthKey(username, realm, password), true
	}
}
```

- [ ] **Step 5: Run tests**

```bash
go test -run TestTURN -v ./signal/
```

Expected: PASS.

- [ ] **Step 6: Write PoC test — TURN server on loopback allocates relay**

```go
func TestTURNServer_Loopback(t *testing.T) {
	// Start a TURN server on loopback.
	udpListener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	turnAddr := udpListener.LocalAddr().String()

	sharedSecret := "test-secret"

	turnServer, err := turn.NewServer(turn.ServerConfig{
		Realm: "k2p",
		// Use TURNAuthHandler — it recomputes the HMAC from the username,
		// so timestamps don't need to match (no regeneration race).
		AuthHandler: TURNAuthHandler(sharedSecret),
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: udpListener,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP("127.0.0.1"),
				Address:      "0.0.0.0",
			},
		}},
	})
	if err != nil {
		t.Fatalf("start TURN server: %v", err)
	}
	defer turnServer.Close()

	// Create an ICE agent with TURN.
	// Generate credentials ONCE, then pass to ICE agent.
	creds := GenerateTURNCredentials(sharedSecret, "test", 1*time.Hour)

	// pion/ice v4 uses pion/stun/v3 for URL parsing.
	// TURN URL must include credentials.
	turnURL, err := stun.ParseURI("turn:" + turnAddr + "?transport=udp")
	if err != nil {
		t.Fatalf("parse TURN URI: %v", err)
	}
	turnURL.Username = creds.Username
	turnURL.Password = creds.Password

	agent, err := ice.NewAgent(&ice.AgentConfig{
		Urls:         []*stun.URI{turnURL},
		NetworkTypes: []ice.NetworkType{ice.NetworkTypeUDP4},
		// Only gather relay candidates to force TURN usage.
		CandidateTypes: []ice.CandidateType{ice.CandidateTypeRelay},
	})
	if err != nil {
		t.Fatalf("create ICE agent: %v", err)
	}
	defer agent.Close()

	// Verify we get a relay candidate.
	relayCh := make(chan bool, 1)
	agent.OnCandidate(func(c ice.Candidate) {
		if c != nil && c.Type() == ice.CandidateTypeRelay {
			relayCh <- true
		}
	})
	agent.GatherCandidates()

	select {
	case <-relayCh:
		// Success: TURN allocation worked.
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for relay candidate — TURN allocation failed")
	}
}
```

**If this test fails:** Check that pion/turn's auth flow matches TURN REST API format. `TURNAuthHandler` recomputes the HMAC password from the username (no timestamp race). `turn.GenerateAuthKey` computes `MD5(username:realm:password)`. Debug by adding logging to the auth handler. Also check that pion/stun URI parsing correctly sets username/password on the `stun.URI` struct.

- [ ] **Step 7: Run TURN PoC test**

```bash
go test -run TestTURNServer -v ./signal/
```

Expected: PASS — relay candidate gathered via TURN allocation.

- [ ] **Step 8: Commit**

```bash
git add signal/turn.go signal/turn_test.go go.mod go.sum
git commit -m "feat(signal): embedded TURN server with REST API credentials"
```

---

### Task 4: Simplify Signal Store & Types

**Files:**
- Modify: `signal/types.go`
- Modify: `signal/store.go`
- Modify: `signal/store_test.go`
- Modify: `signal/handler.go`
- Modify: `signal/handler_test.go`

- [ ] **Step 1: Update `signal/types.go`**

Remove `Addr`, `NATType` from `RegisterRequest`. Remove `ConnectRequest` and `PunchNotify` (already done in Task 2 if not). Add WS message types:

```go
type RegisterRequest struct {
	UDID     string   `json:"udid"`
	Networks []string `json:"networks,omitempty"`
	DNS      string   `json:"dns,omitempty"`
}

// WSMessage is a WebSocket message for ICE candidate exchange.
type WSMessage struct {
	Type      string `json:"type"`                // "session-start", "credentials", "candidate", "gathering-done", "session-end"
	Session   string `json:"session,omitempty"`    // session ID
	Ufrag     string `json:"ufrag,omitempty"`      // ICE ufrag
	Pwd       string `json:"pwd,omitempty"`        // ICE pwd
	Candidate string `json:"candidate,omitempty"`  // ICE candidate string
}

type LookupResponse struct {
	UDID     string            `json:"udid"`
	Networks []string          `json:"networks"`
	DNS      string            `json:"dns"`
	Online   bool              `json:"online"`
	TURN     *TURNCredentials  `json:"turn,omitempty"`
}
```

- [ ] **Step 2: Update `signal/store.go` — remove Addr/NATType from PeerEntry and PeerSnapshot**

Remove `Addr`, `NATType` fields from `PeerEntry` and `PeerSnapshot`. Update `Update()` method to not set these fields. Update `Get()` to not copy them.

- [ ] **Step 3: Update `signal/handler.go`**

Update `handleRegister` — don't read `Addr`/`NATType` from request.

Update `handleLookup` — don't return `Addr`/`NATType`. Add TURN credentials to response:

```go
func (h *Handler) handleLookup(w http.ResponseWriter, r *http.Request) {
	udid := r.PathValue("udid")
	entry, ok := h.store.Get(udid)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	resp := LookupResponse{
		UDID:     entry.UDID,
		Networks: entry.Networks,
		DNS:      entry.DNS,
		Online:   h.store.IsOnline(udid),
	}

	// Add TURN credentials if configured.
	if h.turnSecret != "" {
		creds := GenerateTURNCredentials(h.turnSecret, udid, 1*time.Hour)
		creds.URLs = h.turnURLs
		resp.TURN = &creds
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
```

Add `turnSecret`, `turnURLs`, and `router` fields to `Handler`. Update `NewHandler` signature:

```go
// NewHandler creates a Handler backed by store and registers all routes.
// turnSecret and turnURLs configure TURN credential generation in lookup responses.
// Pass empty turnSecret to disable TURN credentials.
func NewHandler(store *Store, turnSecret string, turnURLs []string) *Handler {
	h := &Handler{
		mux:         http.NewServeMux(),
		store:       store,
		turnSecret:  turnSecret,
		turnURLs:    turnURLs,
		router:      NewSessionRouter(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	h.mux.HandleFunc("POST /k2p/claim", h.handleClaim)
	h.mux.HandleFunc("POST /k2p/register", h.handleRegister)
	h.mux.HandleFunc("GET /k2p/lookup/{udid}", h.handleLookup)
	h.mux.HandleFunc("GET /k2p/ws", h.handleWS)
	return h
}
```

**Important**: All existing tests calling `NewHandler(store)` must be updated to `NewHandler(store, "", nil)`. This includes `signal/handler_test.go` and `test/k2p_e2e_test.go`.

- [ ] **Step 4: Update tests**

Update `signal/store_test.go` — remove `Addr`/`NATType` assertions from `TestStore_Update_ValidToken`.

Update `signal/handler_test.go` — remove `Addr`/`NATType` from `RegisterRequest` in tests, remove assertions on these fields in lookup tests. Remove `TestHandler_ConcurrentConnect` (deleted endpoint).

- [ ] **Step 5: Run tests**

```bash
go test -v ./signal/...
```

Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add signal/
git commit -m "refactor(signal): simplify store — remove addr/nat_type, add TURN creds to lookup"
```

---

### Task 5: WS Session Routing

**Files:**
- Create: `signal/session.go`
- Create: `signal/session_test.go`
- Modify: `signal/handler.go` — rewrite WS handler

- [ ] **Step 1: Write failing tests for session routing**

```go
// signal/session_test.go
package signal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestSession_HomeAwayExchange verifies candidate exchange via WS.
func TestSession_HomeAwayExchange(t *testing.T) {
	h := NewHandler(NewStore(), "", nil) // no TURN for this test
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Claim + register Home.
	claimRR := doJSON(t, h, http.MethodPost, "/k2p/claim", ClaimRequest{UDID: "sess-home"})
	if claimRR.Code != 200 {
		t.Fatalf("claim: %d", claimRR.Code)
	}
	var cr ClaimResponse
	json.NewDecoder(claimRR.Body).Decode(&cr)

	// Home WS.
	wsBase := "ws" + srv.URL[4:]
	homeWS, _, err := websocket.DefaultDialer.Dial(
		wsBase+"/k2p/ws?udid=sess-home&token="+cr.RegToken+"&role=home", nil)
	if err != nil {
		t.Fatalf("home WS: %v", err)
	}
	defer homeWS.Close()

	// Away WS.
	awayWS, _, err := websocket.DefaultDialer.Dial(
		wsBase+"/k2p/ws?udid=sess-home&role=away", nil)
	if err != nil {
		t.Fatalf("away WS: %v", err)
	}
	defer awayWS.Close()

	// Home should receive session-start.
	homeWS.SetReadDeadline(time.Now().Add(2 * time.Second))
	var startMsg WSMessage
	if err := homeWS.ReadJSON(&startMsg); err != nil {
		t.Fatalf("home read session-start: %v", err)
	}
	if startMsg.Type != "session-start" {
		t.Errorf("type = %q, want session-start", startMsg.Type)
	}
	if startMsg.Session == "" {
		t.Fatal("expected non-empty session ID")
	}
	sessionID := startMsg.Session

	// Away sends candidate → Home receives it.
	awayWS.WriteJSON(WSMessage{
		Type:      "candidate",
		Session:   sessionID,
		Candidate: "candidate:1 1 udp 2130706431 192.168.1.1 5000 typ host",
	})

	homeWS.SetReadDeadline(time.Now().Add(2 * time.Second))
	var candMsg WSMessage
	if err := homeWS.ReadJSON(&candMsg); err != nil {
		t.Fatalf("home read candidate: %v", err)
	}
	if candMsg.Type != "candidate" {
		t.Errorf("type = %q, want candidate", candMsg.Type)
	}
	if candMsg.Candidate == "" {
		t.Error("expected non-empty candidate")
	}

	// Home sends candidate → Away receives it.
	homeWS.WriteJSON(WSMessage{
		Type:      "candidate",
		Session:   sessionID,
		Candidate: "candidate:2 1 udp 2130706431 192.168.1.2 6000 typ host",
	})

	awayWS.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := awayWS.ReadJSON(&candMsg); err != nil {
		t.Fatalf("away read candidate: %v", err)
	}
	if candMsg.Candidate == "" {
		t.Error("expected non-empty candidate from home")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -run TestSession -v ./signal/
```

- [ ] **Step 3: Implement session routing in `signal/session.go`**

```go
// signal/session.go
package signal

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/gorilla/websocket"
	"github.com/sasha-s/go-deadlock"
)

// session pairs a Home WS and an Away WS for ICE candidate exchange.
type session struct {
	id   string
	home *wsConn // Home's write-serialized WS
	away *wsConn // Away's write-serialized WS
}

// SessionRouter manages WS sessions for ICE candidate exchange.
// Each Away connection creates a session. Messages are forwarded
// between the paired Home and Away WS connections.
//
// Write serialization: Each WS conn has a dedicated writeMu (deadlock.Mutex)
// to prevent concurrent gorilla/websocket writes (gorilla requires external
// serialization for concurrent writers). Home WS may receive messages from
// multiple Away sessions concurrently, so writeMu is per-conn, not per-session.
type SessionRouter struct {
	mu       deadlock.Mutex
	sessions map[string]*session   // session ID → session
	homeWS   map[string]*wsConn    // UDID → Home WS
}

// wsConn wraps a websocket.Conn with a write mutex for serialization.
type wsConn struct {
	conn    *websocket.Conn
	writeMu deadlock.Mutex
}

func (w *wsConn) WriteJSON(v any) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	return w.conn.WriteJSON(v)
}

func NewSessionRouter() *SessionRouter {
	return &SessionRouter{
		sessions: make(map[string]*session),
		homeWS:   make(map[string]*wsConn),
	}
}

// SetHome registers the Home WS for a UDID.
func (r *SessionRouter) SetHome(udid string, ws *websocket.Conn) {
	r.mu.Lock()
	r.homeWS[udid] = &wsConn{conn: ws}
	r.mu.Unlock()
}

// ClearHome removes the Home WS for a UDID.
func (r *SessionRouter) ClearHome(udid string) {
	r.mu.Lock()
	delete(r.homeWS, udid)
	r.mu.Unlock()
}

// CreateSession creates a new session for an Away connecting to a Home UDID.
// Sends a "session-start" message to Home's WS. Returns the session ID.
func (r *SessionRouter) CreateSession(udid string, awayWS *websocket.Conn) (string, error) {
	id := generateSessionID()

	r.mu.Lock()
	home := r.homeWS[udid]
	awayWrapped := &wsConn{conn: awayWS}
	r.sessions[id] = &session{id: id, home: home, away: awayWrapped}
	r.mu.Unlock()

	if home == nil {
		return "", fmt.Errorf("signal: home %q not connected", udid)
	}

	// Notify Home of new session (write-serialized via wsConn.WriteJSON).
	msg := WSMessage{Type: "session-start", Session: id}
	return id, home.WriteJSON(msg)
}

// Route forwards a WSMessage to the other side of the session.
// Write-serialized via wsConn.WriteJSON — safe for concurrent calls.
func (r *SessionRouter) Route(senderRole string, msg WSMessage) {
	r.mu.Lock()
	sess, ok := r.sessions[msg.Session]
	r.mu.Unlock()
	if !ok {
		return
	}

	var target *wsConn
	if senderRole == "home" {
		target = sess.away
	} else {
		target = sess.home
	}
	if target != nil {
		target.WriteJSON(msg) //nolint:errcheck
	}
}

// CleanupSession removes a session and sends session-end to the other side.
func (r *SessionRouter) CleanupSession(sessionID string, disconnectedRole string) {
	r.mu.Lock()
	sess, ok := r.sessions[sessionID]
	if ok {
		delete(r.sessions, sessionID)
	}
	r.mu.Unlock()

	if !ok {
		return
	}

	// Notify the other side that the session ended.
	endMsg := WSMessage{Type: "session-end", Session: sessionID}
	if disconnectedRole == "away" && sess.home != nil {
		sess.home.WriteJSON(endMsg) //nolint:errcheck
	} else if disconnectedRole == "home" && sess.away != nil {
		sess.away.WriteJSON(endMsg) //nolint:errcheck
	}
}

func generateSessionID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
```

- [ ] **Step 4: Rewrite WS handler in `signal/handler.go`**

Replace the old `handleWS` with a new version that supports `role=home` and `role=away`:

```go
func (h *Handler) handleWS(w http.ResponseWriter, r *http.Request) {
	udid := r.URL.Query().Get("udid")
	role := r.URL.Query().Get("role")
	token := r.URL.Query().Get("token")

	if role == "home" {
		// Home: validate token, upgrade WS, register in store + router.
		if !h.store.ValidateToken(udid, token) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		conn, err := h.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		h.store.SetWS(udid, conn)
		h.router.SetHome(udid, conn)
		defer func() {
			h.store.SetWS(udid, nil)
			h.router.ClearHome(udid)
			conn.Close()
		}()

		// Read loop: forward messages from Home to Away sessions.
		for {
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				break
			}
			h.router.Route("home", msg)
		}

	} else if role == "away" {
		// Away: upgrade WS, create session, exchange candidates.
		conn, err := h.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		sessionID, err := h.router.CreateSession(udid, conn)
		if err != nil {
			conn.WriteJSON(WSMessage{Type: "error"})
			return
		}
		defer h.router.CleanupSession(sessionID, "away")

		// Send session ID to Away so it knows which session to use.
		conn.WriteJSON(WSMessage{Type: "session-start", Session: sessionID})

		// Read loop: forward messages from Away to Home.
		for {
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				break
			}
			msg.Session = sessionID // enforce session ID
			h.router.Route("away", msg)
		}

	} else {
		http.Error(w, "bad request: role=home|away required", http.StatusBadRequest)
	}
}
```

Update `Handler` struct to include `router *SessionRouter`. Update `NewHandler` to create it.

- [ ] **Step 5: Run tests**

```bash
go test -v ./signal/...
```

- [ ] **Step 6: Commit**

```bash
git add signal/
git commit -m "feat(signal): WS session routing for ICE candidate exchange"
```

---

### Task 6: Rewrite k2p/signal.go — Client-Side Candidate Exchange

**Files:**
- Modify: `k2p/signal.go`
- Modify: `k2p/signal_test.go`

- [ ] **Step 1: Update `PeerInfo` — add TURN credentials**

```go
type PeerInfo struct {
	UDID     string            `json:"udid"`
	Networks []string          `json:"networks"`
	DNS      string            `json:"dns"`
	Online   bool              `json:"online"`
	TURN     *TURNCredentials  `json:"turn,omitempty"`
}

type TURNCredentials struct {
	URLs     []string `json:"urls"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	TTL      int      `json:"ttl"`
}
```

- [ ] **Step 2: Replace `ListenNotify` and `RequestConnect` with `ExchangeCandidates`**

Delete `ListenNotify` and `RequestConnect` methods.

Add WS message type (mirrors signal package):
```go
type WSMessage struct {
	Type      string `json:"type"`
	Session   string `json:"session,omitempty"`
	Ufrag     string `json:"ufrag,omitempty"`
	Pwd       string `json:"pwd,omitempty"`
	Candidate string `json:"candidate,omitempty"`
}
```

Add `ConnectHome` for Away (opens WS, returns session for candidate exchange):
```go
func (c *SignalClient) ConnectHome(ctx context.Context, homeUDID string) (*CandidateExchange, error) {
	base := c.baseURL
	base = strings.Replace(base, "https://", "wss://", 1)
	base = strings.Replace(base, "http://", "ws://", 1)
	wsURL := base + "/k2p/ws?udid=" + homeUDID + "&role=away"

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("k2p: away WS dial: %w", err)
	}

	// Read session-start message.
	var startMsg WSMessage
	if err := conn.ReadJSON(&startMsg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("k2p: read session-start: %w", err)
	}

	return &CandidateExchange{
		conn:    conn,
		session: startMsg.Session,
		inCh:    make(chan WSMessage, 16),
	}, nil
}
```

Add `ListenHome` for Home (wraps existing WS, listens for session-start messages):
```go
func (c *SignalClient) ListenHome(ctx context.Context, udid string) (<-chan *CandidateExchange, error) {
	// Opens the Home role WS, reads session-start messages,
	// returns a channel of CandidateExchange objects.
	// Each CandidateExchange handles one Away session.
}
```

Add `CandidateExchange` type with read pump goroutine:
```go
// CandidateExchange handles ICE candidate exchange over a WS session.
// The read pump goroutine reads from WS and fans out to inCh (candidates/credentials)
// and credsCh (first credentials message). Close() stops the read pump.
//
// Two creation modes:
// - Away-side (newCandidateExchange): owns the WS conn, Close() closes it.
// - Home-side (created by ListenHome): shares Home's WS conn, Close() only
//   signals done channel — does NOT close the shared conn.
type CandidateExchange struct {
	conn      *websocket.Conn
	session   string
	inCh      chan WSMessage      // all incoming messages (candidates, credentials, etc.)
	credsCh   chan WSMessage      // first credentials message (ufrag+pwd)
	done      chan struct{}       // closed on Close()
	writeMu   deadlock.Mutex     // serializes WS writes
	ownsConn  bool               // true = Close() closes conn; false = shared conn
}

// newCandidateExchange creates a CandidateExchange and starts the read pump goroutine.
func newCandidateExchange(conn *websocket.Conn, session string) *CandidateExchange {
	e := &CandidateExchange{
		conn:     conn,
		session:  session,
		inCh:     make(chan WSMessage, 16),
		credsCh:  make(chan WSMessage, 1),
		done:     make(chan struct{}),
		ownsConn: true, // Away-side exchange owns the WS conn.
	}
	// Read pump: reads from WS, dispatches to channels.
	safego.Go(func() {
		defer close(e.inCh)
		for {
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			// Fan out credentials to dedicated channel (consumed by waitForCredentials).
			if msg.Type == "credentials" {
				select {
				case e.credsCh <- msg:
				default: // already received credentials
				}
			}
			select {
			case e.inCh <- msg:
			case <-e.done:
				return
			}
		}
	})
	return e
}

func (e *CandidateExchange) Session() string { return e.session }

func (e *CandidateExchange) SendCandidate(candidate string) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	return e.conn.WriteJSON(WSMessage{
		Type:      "candidate",
		Session:   e.session,
		Candidate: candidate,
	})
}

func (e *CandidateExchange) SendCredentials(ufrag, pwd string) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	return e.conn.WriteJSON(WSMessage{
		Type:    "credentials",
		Session: e.session,
		Ufrag:   ufrag,
		Pwd:     pwd,
	})
}

func (e *CandidateExchange) Incoming() <-chan WSMessage { return e.inCh }

func (e *CandidateExchange) Close() error {
	select {
	case <-e.done:
	default:
		close(e.done)
	}
	// Only close the WS conn if this exchange owns it (Away-side).
	// Home-side exchanges share the Home WS — closing it would break all sessions.
	if e.ownsConn {
		return e.conn.Close()
	}
	return nil
}

// waitForCredentials blocks until the remote peer sends its ICE credentials.
func waitForCredentials(ctx context.Context, exchange *CandidateExchange) (ufrag, pwd string, err error) {
	select {
	case msg := <-exchange.credsCh:
		return msg.Ufrag, msg.Pwd, nil
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}
```

Add `ListenHome` for Home side — opens WS, reads session-start messages, returns channel of exchanges:
```go
// ListenHome opens the Home WS and returns a channel of CandidateExchange objects.
// Each CandidateExchange represents one Away peer wanting to connect.
// The Home's WS read loop demuxes messages: session-start creates new exchanges,
// candidate/credentials messages are routed to the matching exchange.
func (c *SignalClient) ListenHome(ctx context.Context, udid string) (<-chan *CandidateExchange, error) {
	base := c.baseURL
	base = strings.Replace(base, "https://", "wss://", 1)
	base = strings.Replace(base, "http://", "ws://", 1)
	wsURL := base + "/k2p/ws?udid=" + udid + "&token=" + c.token + "&role=home"

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("k2p: home WS dial: %w", err)
	}

	ch := make(chan *CandidateExchange, 4)

	// Home-side demux: one WS conn, multiple sessions.
	// session-start → create exchange, send to ch.
	// candidate/credentials → route to matching exchange.
	safego.Go(func() {
		defer close(ch)
		defer conn.Close()
		exchanges := make(map[string]*CandidateExchange) // session → exchange

		for {
			if ctx.Err() != nil {
				return
			}
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			switch msg.Type {
			case "session-start":
				// New Away wants to connect. Create a CandidateExchange.
				// Home's exchange shares the same WS conn — ownsConn=false
				// so Close() won't close the shared conn.
				ex := &CandidateExchange{
					conn:     conn,
					session:  msg.Session,
					inCh:     make(chan WSMessage, 16),
					credsCh:  make(chan WSMessage, 1),
					done:     make(chan struct{}),
					ownsConn: false, // shared Home WS — do NOT close on exchange.Close()
				}
				exchanges[msg.Session] = ex
				select {
				case ch <- ex:
				case <-ctx.Done():
					return
				}
			case "candidate", "credentials", "gathering-done":
				ex, ok := exchanges[msg.Session]
				if !ok {
					continue
				}
				if msg.Type == "credentials" {
					select {
					case ex.credsCh <- msg:
					default:
					}
				}
				select {
				case ex.inCh <- msg:
				case <-ex.done:
					delete(exchanges, msg.Session)
				}
			case "session-end":
				if ex, ok := exchanges[msg.Session]; ok {
					close(ex.inCh)
					delete(exchanges, msg.Session)
				}
			}
		}
	})

	return ch, nil
}
```

- [ ] **Step 3: Update signal_test.go**

Delete tests for `RequestConnect` and `ListenNotify`. Write new tests for `ConnectHome` and `CandidateExchange` using httptest + WS.

- [ ] **Step 4: Run tests**

```bash
go test -v ./k2p/...
```

- [ ] **Step 5: Commit**

```bash
git add k2p/signal.go k2p/signal_test.go
git commit -m "feat(k2p): signal client — ICE candidate exchange over WS"
```

---

### Task 7: ICE Agent Helper + Default URLs

**Files:**
- Modify: `k2p/ice.go` — add agent creation helper, default STUN/TURN URLs

- [ ] **Step 1: Add `NewICEAgent` helper**

```go
// DefaultSTUNURLs returns public STUN server URIs for ICE candidate gathering.
func DefaultSTUNURLs() []*stun.URI {
	urls := []string{
		"stun:stun.l.google.com:19302",
		"stun:stun1.l.google.com:19302",
		"stun:stun.cloudflare.com:3478",
		"stun:stun.miwifi.com:3478",
		"stun:stun.chat.bilibili.com:3478",
	}
	var result []*stun.URI
	for _, u := range urls {
		parsed, err := stun.ParseURI(u)
		if err == nil {
			result = append(result, parsed)
		}
	}
	return result
}

// NewICEAgent creates an ICE agent configured with STUN and optional TURN servers.
func NewICEAgent(cfg Config) (*ice.Agent, error) {
	urls := DefaultSTUNURLs()

	// Add TURN if credentials provided.
	for _, turnURL := range cfg.TURNURLs {
		parsed, err := stun.ParseURI(turnURL)
		if err != nil {
			continue
		}
		parsed.Username = cfg.TURNUsername
		parsed.Password = cfg.TURNPassword
		urls = append(urls, parsed)
	}

	return ice.NewAgent(&ice.AgentConfig{
		Urls:         urls,
		NetworkTypes: []ice.NetworkType{ice.NetworkTypeUDP4},
	})
}
```

- [ ] **Step 2: Add goleak test for ICE agent cleanup**

```go
func TestICEAgent_NoGoroutineLeak(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)

	agent, err := NewICEAgent(Config{})
	if err != nil {
		t.Fatal(err)
	}
	// Close immediately — verify no leaked goroutines.
	agent.Close()
}
```

- [ ] **Step 3: Run tests**

```bash
go test -v -run TestICEAgent ./k2p/
```

- [ ] **Step 4: Commit**

```bash
git add k2p/ice.go k2p/ice_test.go
git commit -m "feat(k2p): ICE agent helper with default STUN URLs + goroutine leak test"
```

---

### Task 8: Rewrite `k2p/away.go` — ICE-Based Dial

**Files:**
- Modify: `k2p/away.go`
- Create: `k2p/away_test.go`

- [ ] **Step 1: Implement `Dial()` with ICE**

```go
func Dial(ctx context.Context, cfg Config) (*ConnectResult, error) {
	startTime := time.Now()

	// 1. Lookup Home.
	signal := NewSignalClient(cfg.SignalServer, cfg.Token)
	homeInfo, err := signal.Lookup(ctx, cfg.HomeUDID)
	if err != nil {
		return nil, fmt.Errorf("k2p: away: lookup: %w", err)
	}

	// 2. Apply TURN credentials from lookup.
	if homeInfo.TURN != nil {
		cfg.TURNURLs = homeInfo.TURN.URLs
		cfg.TURNUsername = homeInfo.TURN.Username
		cfg.TURNPassword = homeInfo.TURN.Password
	}

	// 3. Create ICE agent.
	agent, err := NewICEAgent(cfg)
	if err != nil {
		return nil, fmt.Errorf("k2p: away: create ICE agent: %w", err)
	}

	// 4. Open WS for candidate exchange.
	exchange, err := signal.ConnectHome(ctx, cfg.HomeUDID)
	if err != nil {
		agent.Close()
		return nil, fmt.Errorf("k2p: away: connect signal: %w", err)
	}

	// 5. Wire ICE candidate callbacks to WS.
	agent.OnCandidate(func(c ice.Candidate) {
		if c != nil {
			exchange.SendCandidate(c.Marshal())
		}
	})

	// 6. Start receiving remote candidates from Home.
	// The CandidateExchange read pump goroutine (started by newCandidateExchange)
	// reads from WS and dispatches to inCh. We consume candidates here.
	// Credentials are automatically routed to credsCh by the read pump.
	safego.Go(func() {
		for msg := range exchange.Incoming() {
			if msg.Type == "candidate" {
				c, err := ice.UnmarshalCandidate(msg.Candidate)
				if err == nil {
					agent.AddRemoteCandidate(c)
				}
			}
		}
	})

	// 7. Send our credentials.
	ufrag, pwd, _ := agent.GetLocalUserCredentials()
	exchange.SendCredentials(ufrag, pwd)

	// 8. Gather candidates.
	agent.GatherCandidates()

	// 9. Wait for Home's credentials, then ICE Dial.
	homeUfrag, homePwd, err := waitForCredentials(ctx, exchange)
	if err != nil {
		agent.Close()
		exchange.Close()
		return nil, fmt.Errorf("k2p: away: wait for credentials: %w", err)
	}
	iceConn, err := agent.Dial(ctx, homeUfrag, homePwd)
	if err != nil {
		agent.Close()
		exchange.Close()
		return nil, fmt.Errorf("k2p: away: ICE dial: %w", err)
	}

	// 10. QUIC over ICE.
	pc := newICEPacketConn(iceConn)
	tr := &quic.Transport{Conn: pc}
	tlsCfg := buildPinTLSConfig(cfg)
	peerAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} // synthetic
	quicConn, err := tr.Dial(ctx, peerAddr, tlsCfg, wire.DefaultQUICConfig())
	if err != nil {
		tr.Close()
		agent.Close()
		return nil, fmt.Errorf("k2p: away: QUIC dial: %w", err)
	}

	// 11. Peer auth.
	if err := doPeerAuth(ctx, quicConn, cfg.PeerSecret); err != nil {
		quicConn.CloseWithError(1, "auth failed")
		tr.Close()
		agent.Close()
		return nil, err
	}

	// 12. Detect transport type.
	transportType := detectTransportType(agent)

	slog.Info("DIAG: p2p-connected",
		"peer", cfg.HomeUDID,
		"method", transportType,
		"dialMs", time.Since(startTime).Milliseconds())

	exchange.Close() // signal WS no longer needed

	return &ConnectResult{
		Dialer:        wire.NewQUICClientFromConn(quicConn, tr, cfg.WireCfg, cfg.TicketStore),
		TransportType: transportType,
	}, nil
}

func detectTransportType(agent *ice.Agent) string {
	pair, err := agent.GetSelectedCandidatePair()
	if err != nil || pair == nil {
		return "p2p-unknown"
	}
	switch pair.Local.Type() {
	case ice.CandidateTypeHost:
		return "p2p-lan"
	case ice.CandidateTypeServerReflexive:
		return "p2p-direct"
	case ice.CandidateTypeRelay:
		return "p2p-relay"
	default:
		return "p2p-unknown"
	}
}
```

- [ ] **Step 2: Write unit test for `detectTransportType`**

- [ ] **Step 3: Run tests**

```bash
go test -v ./k2p/...
```

- [ ] **Step 4: Commit**

```bash
git add k2p/away.go k2p/away_test.go
git commit -m "feat(k2p): Away.Dial with ICE — candidate exchange + QUIC + peer auth"
```

---

### Task 9: Rewrite `k2p/home.go` — ICE-Based Listen

**Files:**
- Modify: `k2p/home.go`
- Create: `k2p/home_test.go`

- [ ] **Step 1: Implement `Listen()` with ICE**

```go
func Listen(ctx context.Context, cfg Config) (<-chan wire.Listener, error) {
	// 1. Register metadata (no STUN needed — ICE handles NAT internally).
	signal := NewSignalClient(cfg.SignalServer, cfg.RegToken)
	err := signal.Register(ctx, RegisterRequest{
		UDID:     cfg.HomeUDID,
		Networks: cfg.Networks,
		DNS:      cfg.DNS,
	})
	if err != nil {
		return nil, fmt.Errorf("k2p: home: register: %w", err)
	}

	// 2. Keepalive: re-register every 25s.
	safego.Go(func() { keepaliveLoop(ctx, signal, cfg) })

	// 3. Listen for Away sessions via WS.
	sessionCh, err := signal.ListenHome(ctx, cfg.HomeUDID)
	if err != nil {
		return nil, fmt.Errorf("k2p: home: listen: %w", err)
	}

	listenerCh := make(chan wire.Listener, 4)

	safego.Go(func() {
		defer close(listenerCh)
		for {
			select {
			case <-ctx.Done():
				return
			case exchange, ok := <-sessionCh:
				if !ok {
					return
				}
				safego.Go(func() {
					listener, err := handleAwaySession(ctx, cfg, exchange)
					if err != nil {
						slog.Warn("DIAG: ice-failed", "session", exchange.Session(), "err", err)
						return
					}
					select {
					case listenerCh <- listener:
					case <-ctx.Done():
						listener.Close()
					}
				})
			}
		}
	})

	return listenerCh, nil
}

func handleAwaySession(ctx context.Context, cfg Config, exchange *CandidateExchange) (wire.Listener, error) {
	defer exchange.Close()

	// 1. Create ICE agent.
	agent, err := NewICEAgent(cfg)
	if err != nil {
		return nil, fmt.Errorf("k2p: home: create ICE agent: %w", err)
	}

	// 2. Wire candidates.
	agent.OnCandidate(func(c ice.Candidate) {
		if c != nil {
			exchange.SendCandidate(c.Marshal())
		}
	})

	safego.Go(func() {
		for msg := range exchange.Incoming() {
			if msg.Type == "candidate" {
				c, err := ice.UnmarshalCandidate(msg.Candidate)
				if err == nil {
					agent.AddRemoteCandidate(c)
				}
			}
		}
	})

	// 3. Send credentials + gather.
	ufrag, pwd, _ := agent.GetLocalUserCredentials()
	exchange.SendCredentials(ufrag, pwd)
	agent.GatherCandidates()

	// 4. Wait for Away's credentials, then ICE Accept.
	awayUfrag, awayPwd, err := waitForCredentials(ctx, exchange)
	if err != nil {
		agent.Close()
		return nil, fmt.Errorf("k2p: home: wait for credentials: %w", err)
	}
	iceConn, err := agent.Accept(ctx, awayUfrag, awayPwd)
	if err != nil {
		agent.Close()
		return nil, fmt.Errorf("k2p: home: ICE accept: %w", err)
	}

	// 5. QUIC Listen over ICE.
	pc := newICEPacketConn(iceConn)
	tr := &quic.Transport{Conn: pc}

	var tlsCert tls.Certificate
	if cfg.TLSCert != nil {
		tlsCert = *cfg.TLSCert
	} else {
		certPair, err := wire.GenerateCertPair()
		if err != nil {
			tr.Close()
			agent.Close()
			return nil, fmt.Errorf("k2p: home: generate cert: %w", err)
		}
		tlsCert = certPair.EC
	}

	tlsCfg := &tls.Config{
		NextProtos:   []string{"h3"},
		Certificates: []tls.Certificate{tlsCert},
	}
	quicLn, err := tr.Listen(tlsCfg, wire.DefaultQUICConfig())
	if err != nil {
		tr.Close()
		agent.Close()
		return nil, fmt.Errorf("k2p: home: QUIC listen: %w", err)
	}

	transportType := detectTransportType(agent)
	slog.Info("DIAG: ice-connected", "session", exchange.Session(), "method", transportType)

	// 6. Peer auth (same as before — at QUIC stream level).
	if cfg.SecretHash != "" {
		// Accept QUIC conn, verify auth, return authenticated listener.
		conn, err := quicLn.Accept(ctx)
		if err != nil {
			quicLn.Close()
			return nil, fmt.Errorf("k2p: home: accept QUIC: %w", err)
		}
		authCtx, authCancel := context.WithTimeout(ctx, 10*time.Second)
		stream, err := conn.AcceptStream(authCtx)
		authCancel()
		if err != nil {
			conn.CloseWithError(1, "auth stream failed")
			quicLn.Close()
			return nil, fmt.Errorf("k2p: home: accept auth stream: %w", err)
		}
		stream.SetDeadline(time.Now().Add(10 * time.Second))
		secret, err := ReadAuthFrame(stream)
		stream.SetDeadline(time.Time{})
		if err != nil {
			stream.Close()
			conn.CloseWithError(1, "auth read failed")
			quicLn.Close()
			return nil, fmt.Errorf("k2p: home: read auth: %w", err)
		}
		if !VerifySecret(secret, cfg.SecretHash) {
			stream.Write([]byte{AuthRejected})
			stream.Close()
			conn.CloseWithError(1, "auth rejected")
			quicLn.Close()
			return nil, fmt.Errorf("k2p: home: peer auth rejected")
		}
		stream.Write([]byte{AuthOK})
		stream.Close()
		return wire.NewQUICServerFromConn(conn, quicLn, cfg.TicketStore), nil
	}

	return wire.NewQUICServerFromListener(quicLn, cfg.TicketStore), nil
}
```

- [ ] **Step 2: Write integration test**

Test on loopback: two ICE agents, QUIC + auth, verify wire.Listener works.

- [ ] **Step 3: Run tests**

```bash
go test -v ./k2p/...
```

- [ ] **Step 4: Commit**

```bash
git add k2p/home.go k2p/home_test.go
git commit -m "feat(k2p): Home.Listen with ICE — session-based candidate exchange + QUIC"
```

---

### Task 10: E2E Test — Full Flow with Signal Server

**Files:**
- Modify: `test/k2p_e2e_test.go`

- [ ] **Step 1: Rewrite `TestK2P_E2E_DirectPunch` → `TestK2P_E2E_ICEDirect`**

Replace with: signal server + two ICE agents on loopback + QUIC + peer auth. No STUN servers needed (host candidates on loopback). No TURN needed (direct connectivity on loopback).

The test structure:
1. Start signal server (httptest)
2. Claim UDID
3. Home: `Listen()` with host-only ICE
4. Away: `Dial()` with host-only ICE
5. Verify: `result.TransportType == "p2p-lan"` (host candidate on loopback)
6. Verify: Home received wire.Listener

- [ ] **Step 2: Rewrite auth rejection + pin rejection tests**

Same structure as before but using ICE connectivity instead of direct QUIC dial.

- [ ] **Step 3: Add relay test — `TestK2P_E2E_TURNRelay`**

Start embedded TURN server alongside signal server. Configure ICE agents with `CandidateTypes: relay only`. Verify `result.TransportType == "p2p-relay"`.

This test validates the TURN fallback path end-to-end — the path that was broken in the old design.

- [ ] **Step 4: Run all e2e tests**

```bash
go test -v -timeout 60s ./test/...
```

- [ ] **Step 5: Commit**

```bash
git add test/k2p_e2e_test.go
git commit -m "test(k2p): e2e tests rewritten for ICE — direct + relay + auth rejection"
```

---

### Task 11: Update CLI + k2s TURN Integration + CLAUDE.md + Cleanup

**Files:**
- Modify: `cmd/k2/peer_setup.go` — remove STUN-related code
- Modify: `cmd/k2s/` — embed TURN server startup
- Modify: `CLAUDE.md` — update lock ordering graph

- [ ] **Step 1: Update `peer_setup.go`**

Remove references to STUN servers. The setup flow doesn't need STUN anymore (ICE handles it internally at runtime).

Remove the `STUNServers` field from any config construction.

- [ ] **Step 1b: Embed TURN server in k2s**

k2s needs to start a pion/turn server alongside its HTTP/TLS listener. Add to k2s startup:

```go
// In cmd/k2s setup or server startup:
// 1. Generate or load TURN shared secret (stored in server config YAML).
// 2. Start UDP listener for TURN on configurable port (default 3478).
// 3. Create TURN server with TURNAuthHandler.
// 4. Pass turnSecret and turnURLs to signal.NewHandler.

udpListener, _ := net.ListenPacket("udp4", fmt.Sprintf(":%d", turnPort))
turnSrv, _ := turn.NewServer(turn.ServerConfig{
    Realm:       "k2p",
    AuthHandler: signal.TURNAuthHandler(turnSharedSecret),
    PacketConnConfigs: []turn.PacketConnConfig{{
        PacketConn: udpListener,
        RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
            RelayAddress: net.ParseIP(publicIP), // from config or auto-detect
            Address:      "0.0.0.0",
        },
    }},
})
defer turnSrv.Close()

turnURLs := []string{fmt.Sprintf("turn:%s:%d?transport=udp", publicIP, turnPort)}
handler := signal.NewHandler(store, turnSharedSecret, turnURLs)
```

For public IP auto-detection, use `wire.ProbePublicIP()` (already exists in wire/ip.go). If it fails, require explicit config. Add `turn_port`, `turn_shared_secret`, and optional `public_ip` to k2s config YAML.

- [ ] **Step 2: Update CLAUDE.md lock ordering graph**

Remove:
```
signal.Relay.mu (state)
RelayConn.mu (serialization, standalone — protects ws.WriteMessage)
```

Replace with:
```
signal.SessionRouter.mu (state)
```

Add note about ICE agent managing its own concurrency internally (pion/ice goroutines are not in our lock ordering — they're library-internal).

- [ ] **Step 3: Update DIAG events table in CLAUDE.md**

Remove: `DIAG: nat-classify`, `DIAG: punch-start`, `DIAG: punch-ok`, `DIAG: punch-fail`, `DIAG: p2p-relay-fallback`

Add: `DIAG: ice-connecting`, `DIAG: ice-connected`, `DIAG: ice-failed`, `DIAG: ice-disconnected`, `DIAG: turn-allocation`

- [ ] **Step 4: Final test run**

```bash
go test -short -race ./k2p/... ./signal/... ./test/...
```

All tests must pass with `-race`.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore(k2p): update CLI, CLAUDE.md, cleanup for ICE redesign"
```

---

### Task 12: Full Integration Verification

- [ ] **Step 1: Run all project tests**

```bash
go test -short -race -timeout 300s ./...
```

Verify no regressions in wire/, core/, engine/, daemon/, etc.

- [ ] **Step 2: Run k2p + signal tests without -short**

```bash
go test -race -timeout 120s ./k2p/... ./signal/... ./test/...
```

All integration tests pass.

- [ ] **Step 3: Verify build targets**

```bash
go build ./cmd/k2 && go build ./cmd/k2s
```

Both binaries build cleanly.

- [ ] **Step 4: Final commit if any fixes needed**

---

## Summary

| Task | What | Risk addressed |
|------|------|---------------|
| 1 | PoC: QUIC over ice.Conn | **Critical**: validates entire design |
| 2 | Delete old code | Clean slate |
| 3 | TURN server | **Critical**: validated relay allocation |
| 4 | Simplify store/types | Remove unused fields |
| 5 | WS session routing | Candidate exchange infra |
| 6 | Signal client rewrite | Client-side candidate exchange |
| 7 | ICE agent helper | Default URLs + goroutine leak test |
| 8 | Away.Dial rewrite | Full Away flow |
| 9 | Home.Listen rewrite | Full Home flow |
| 10 | E2E tests | End-to-end validation incl. TURN relay |
| 11 | CLI + docs cleanup | Consistency |
| 12 | Full verification | No regressions |
