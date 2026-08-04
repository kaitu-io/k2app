package server

import (
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	k2dns "github.com/kaitu-io/k2/core/dns"
)

// fakeUDPSession is a net.PacketConn standing in for a wire UDP session:
// uplink frames are fed via feed(), downlink frames land in out.
type fakeUDPSession struct {
	in   chan *fakePacket
	out  chan *fakePacket
	done chan struct{}
}

type fakePacket struct {
	payload []byte
	addr    net.Addr
}

type strAddr string

func (a strAddr) Network() string { return "udp" }
func (a strAddr) String() string  { return string(a) }

func newFakeUDPSession() *fakeUDPSession {
	return &fakeUDPSession{
		in:   make(chan *fakePacket, 8),
		out:  make(chan *fakePacket, 8),
		done: make(chan struct{}),
	}
}

func (s *fakeUDPSession) feed(payload string, addr string) {
	s.in <- &fakePacket{payload: []byte(payload), addr: strAddr(addr)}
}

func (s *fakeUDPSession) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case p := <-s.in:
		return copy(b, p.payload), p.addr, nil
	case <-s.done:
		return 0, nil, net.ErrClosed
	}
}

func (s *fakeUDPSession) WriteTo(b []byte, addr net.Addr) (int, error) {
	cp := make([]byte, len(b))
	copy(cp, b)
	select {
	case s.out <- &fakePacket{payload: cp, addr: addr}:
	default:
	}
	return len(b), nil
}

func (s *fakeUDPSession) Close() error {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	return nil
}
func (s *fakeUDPSession) LocalAddr() net.Addr                { return &net.UDPAddr{} }
func (s *fakeUDPSession) SetDeadline(time.Time) error        { return nil }
func (s *fakeUDPSession) SetReadDeadline(time.Time) error    { return nil }
func (s *fakeUDPSession) SetWriteDeadline(t time.Time) error { return nil }

// echoUDPServer starts a UDP echo listener and returns its addr + stop func.
func echoUDPServer(t *testing.T, tag string) (string, func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			pc.WriteTo(append([]byte(tag), buf[:n]...), from)
		}
	}()
	return pc.LocalAddr().String(), func() { pc.Close() }
}

// newTestProxyHandler builds a ProxyHandler that HandleUDP can actually run
// against: the zero-value literal `&ProxyHandler{AllowPrivateIP: true}` panics
// inside h.conns.track (nil map write) and would immediately bounce every
// session at the accepting gate (zero-value atomic.Bool is false). Route
// through the real constructor and only flip the one field the test needs.
func newTestProxyHandler() *ProxyHandler {
	h := NewProxyHandler()
	h.AllowPrivateIP = true
	return h
}

func TestHandleUDP_MultipleDestinationsOneSession(t *testing.T) {
	addrA, stopA := echoUDPServer(t, "A")
	defer stopA()
	addrB, stopB := echoUDPServer(t, "B")
	defer stopB()

	h := newTestProxyHandler()
	sess := newFakeUDPSession()

	go h.HandleUDP(sess, addrA)

	sess.feed("one", addrA)
	sess.feed("two", addrB)

	seen := map[string]bool{}
	deadline := time.After(3 * time.Second)
	for len(seen) < 2 {
		select {
		case p := <-sess.out:
			seen[string(p.payload)] = true
		case <-deadline:
			t.Fatalf("timeout, saw %v", seen)
		}
	}
	if !seen["Aone"] {
		t.Errorf("missing echo from A, saw %v", seen)
	}
	if !seen["Btwo"] {
		t.Errorf("missing echo from B, saw %v", seen)
	}
	sess.Close()
}

func TestHandleUDP_DownlinkCarriesRealSource(t *testing.T) {
	addrA, stopA := echoUDPServer(t, "A")
	defer stopA()

	h := newTestProxyHandler()
	sess := newFakeUDPSession()
	go h.HandleUDP(sess, addrA)
	sess.feed("ping", addrA)

	select {
	case p := <-sess.out:
		if p.addr == nil {
			t.Fatal("downlink addr is nil, want the echo server address")
		}
		if p.addr.String() != addrA {
			t.Fatalf("downlink addr = %v, want %v", p.addr, addrA)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for echo")
	}
	sess.Close()
}

// TestResolveUDPDest_BlocksPrivateIP locks down the exact function
// HandleUDP's per-packet uplink loop calls to police each new destination
// (task-4 review C1/I5): a private-IP destination must be rejected with
// blocked=true when AllowPrivateIP is false (the production default), and an
// ordinary public IP literal must resolve without ever touching the
// resolver.
func TestResolveUDPDest_BlocksPrivateIP(t *testing.T) {
	h := NewProxyHandler() // AllowPrivateIP defaults false — production posture.

	if _, blocked, err := h.resolveUDPDest("10.0.0.5:1234"); err == nil || !blocked {
		t.Fatalf("expected private-IP dest to be blocked, got blocked=%v err=%v", blocked, err)
	}

	ua, blocked, err := h.resolveUDPDest("8.8.8.8:53")
	if err != nil {
		t.Fatalf("expected public IP dest to resolve, got err=%v", err)
	}
	if blocked {
		t.Fatal("expected public IP dest not to be blocked")
	}
	if ua == nil || ua.String() != "8.8.8.8:53" {
		t.Fatalf("got %v, want 8.8.8.8:53", ua)
	}
}

// TestHandleUDP_PrivateIPDestBlockedMidSession is the end-to-end companion to
// TestResolveUDPDest_BlocksPrivateIP: a session that legitimately establishes
// against a public destination must still have its later private-IP
// destinations dropped by the per-packet loop, not just the one-time
// session-establishment check (task-4 review I5 — the prior test suite only
// ever exercised the session's first destination).
func TestHandleUDP_PrivateIPDestBlockedMidSession(t *testing.T) {
	addrPriv, stopPriv := echoUDPServer(t, "PRIV")
	defer stopPriv()

	// Sanity/positive control: prove the echo server actually answers when
	// addressed directly. Without this, "no echo observed through HandleUDP"
	// would be vacuously true regardless of whether blocking actually fired.
	probe, err := net.Dial("udp4", addrPriv)
	if err != nil {
		t.Fatalf("dial probe: %v", err)
	}
	if _, err := probe.Write([]byte("probe")); err != nil {
		t.Fatalf("probe write: %v", err)
	}
	probeBuf := make([]byte, 64)
	probe.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := probe.Read(probeBuf)
	probe.Close()
	if err != nil || string(probeBuf[:n]) != "PRIVprobe" {
		t.Fatalf("echo server sanity check failed: n=%d err=%v", n, err)
	}

	h := NewProxyHandler() // AllowPrivateIP defaults false — production posture.
	sess := newFakeUDPSession()

	// The session's nominal addr must itself clear the top-of-function
	// checkAddr gate. 8.8.8.8 is a public IP literal, so this is a
	// zero-network, deterministic pass — no real traffic is ever sent there.
	go h.HandleUDP(sess, "8.8.8.8:9")

	sess.feed("blocked", addrPriv)

	select {
	case p := <-sess.out:
		t.Fatalf("expected no echo for blocked private-IP dest, got %q from %v", p.payload, p.addr)
	case <-time.After(1 * time.Second):
		// Expected: dropped silently by the per-packet policy check, no
		// forwarding, no crash.
	}
	sess.Close()
}

// TestHandleUDP_ThirdPartySourceDelivered proves full-cone *filtering*, not
// just mapping: a packet from a peer the client never addressed must still
// be delivered downstream once the session has opened its egress port
// against any destination (task-4 review I5 — the earlier tests only proved
// endpoint-independent *mapping*, i.e. that one session's egress port serves
// multiple destinations the client *did* address).
func TestHandleUDP_ThirdPartySourceDelivered(t *testing.T) {
	// learned carries the k2s target socket's ephemeral egress address, as
	// observed by the first (client-addressed) peer it talks to.
	learned := make(chan net.Addr, 1)
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			select {
			case learned <- from:
			default:
			}
			pc.WriteTo(append([]byte("A"), buf[:n]...), from)
		}
	}()
	addrA := pc.LocalAddr().String()

	h := newTestProxyHandler()
	sess := newFakeUDPSession()
	go h.HandleUDP(sess, addrA)
	sess.feed("hello", addrA)

	select {
	case p := <-sess.out:
		if string(p.payload) != "Ahello" {
			t.Fatalf("got %q, want Ahello", p.payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for initial echo")
	}

	var k2sEgress net.Addr
	select {
	case k2sEgress = <-learned:
	case <-time.After(3 * time.Second):
		t.Fatal("did not learn k2s egress addr")
	}

	// A third party the client never addressed sends directly to the
	// session's egress port. Full cone (endpoint-independent filtering)
	// requires this to be accepted and delivered downstream, not dropped.
	thirdParty, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen thirdParty: %v", err)
	}
	defer thirdParty.Close()
	if _, err := thirdParty.WriteTo([]byte("surprise"), k2sEgress); err != nil {
		t.Fatalf("thirdParty write: %v", err)
	}

	select {
	case p := <-sess.out:
		if string(p.payload) != "surprise" {
			t.Fatalf("got %q, want surprise", p.payload)
		}
		if p.addr == nil || p.addr.String() != thirdParty.LocalAddr().String() {
			t.Fatalf("downlink addr = %v, want third-party addr %v", p.addr, thirdParty.LocalAddr())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for third-party packet delivery (full-cone filtering broken)")
	}
	sess.Close()
}

// TestHandleUDP_UnresolvableDestNegativeCached is the regression test for
// task-4 review C1: an unresolvable domain destination must be resolved
// against the upstream at most once per session, not once per packet.
// Without the negative cache, N packets to the same black-holed domain cost
// N upstream queries — and since DirectDNSClient races multiple configured
// servers per query, that fans out further, all from the node's own source
// IP. The cap on distinct destinations (maxUDPDestsPerSession) also silently
// stops working for this class of destination without it: a failed
// resolution was never recorded, so the destination table never grew.
func TestHandleUDP_UnresolvableDestNegativeCached(t *testing.T) {
	var upstreamCalls atomic.Int32
	blackhole, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen blackhole: %v", err)
	}
	defer blackhole.Close()
	go func() {
		buf := make([]byte, 512)
		for {
			_, _, err := blackhole.ReadFrom(buf)
			if err != nil {
				return
			}
			upstreamCalls.Add(1)
			// Never reply — this server exists only to count queries and
			// force every LookupHost call through the full resolve timeout.
		}
	}()

	h := newTestProxyHandler() // AllowPrivateIP=true: isolates this test to
	// the resolve-caching behavior, independent of the policy check covered
	// by TestResolveUDPDest_BlocksPrivateIP.
	h.resolver = NewResolver(h.dnsCache, k2dns.NewDirectDNSClient([]string{blackhole.LocalAddr().String()}))

	sess := newFakeUDPSession()
	// Session's nominal addr is a literal so the top-of-function resolveAddr
	// pre-check (unconditional, unrelated to this test) is a zero-network
	// no-op; the black-holed *domain* destination is only ever addressed via
	// per-packet `to`, exercising the uplink loop's own resolution path.
	go h.HandleUDP(sess, "127.0.0.1:1")

	const badDest = "unresolvable-negcache-test.invalid:9999"
	for i := 0; i < 5; i++ {
		sess.feed("x", badDest)
	}

	// Budget: one resolve attempt bounded by udpDestResolveTimeout (500ms),
	// plus slack for the other 4 packets to drain through the cached path.
	time.Sleep(udpDestResolveTimeout + 500*time.Millisecond)
	sess.Close()

	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 upstream query for 5 packets to the same unresolvable dest, got %d", got)
	}
}

// TestHandleUDP_BadDomainDoesNotStallGoodDest is the regression test for
// task-4 review C2: HandleUDP's uplink loop is shared by every destination
// in a session, so an unbounded resolve for one bad destination used to
// serialize in front of every other in-flight destination's packets —
// reviewer's repro measured a 5.0026s delay for a packet to a perfectly good
// destination that was merely queued behind one black-holed domain. With
// udpDestResolveTimeout bounding that resolve, the same scenario must land
// well under 1s (bound is 500ms plus scheduling slack, nowhere near the old
// 5s DirectDNSClient budget).
func TestHandleUDP_BadDomainDoesNotStallGoodDest(t *testing.T) {
	blackhole, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen blackhole: %v", err)
	}
	defer blackhole.Close()
	go func() {
		buf := make([]byte, 512)
		for {
			if _, _, err := blackhole.ReadFrom(buf); err != nil {
				return
			}
			// Never reply.
		}
	}()

	addrGood, stopGood := echoUDPServer(t, "GOOD")
	defer stopGood()

	h := newTestProxyHandler()
	h.resolver = NewResolver(h.dnsCache, k2dns.NewDirectDNSClient([]string{blackhole.LocalAddr().String()}))

	sess := newFakeUDPSession()
	go h.HandleUDP(sess, addrGood)

	start := time.Now()
	sess.feed("bad", "black-hole-stall-test.invalid:9999")
	sess.feed("good", addrGood)

	select {
	case p := <-sess.out:
		elapsed := time.Since(start)
		if string(p.payload) != "GOODgood" {
			t.Fatalf("got %q, want GOODgood", p.payload)
		}
		if elapsed > 1*time.Second {
			t.Fatalf("good dest delayed %v behind bad domain resolve, want << 1s (old bug: ~5s)", elapsed)
		}
		t.Logf("good dest echo arrived after %v", elapsed)
	case <-time.After(4 * time.Second):
		t.Fatal("timeout waiting for good dest echo — bad domain resolve likely stalled the uplink loop")
	}
	sess.Close()
}

// TestHandleUDP_DestCapEvictsOldestNotFreezes covers task-4 review I3: once
// a session's distinct-destination cap (maxUDPDestsPerSession) is reached,
// a new destination must evict the least-recently-used entry and still get
// admitted — not be dropped for the rest of the session's life. A long-lived
// game/ICE session that rotates through more than maxUDPDestsPerSession
// peers over its lifetime (a 60s-idle-timeout session can live for hours)
// must keep being able to reach new ones.
func TestHandleUDP_DestCapEvictsOldestNotFreezes(t *testing.T) {
	h := newTestProxyHandler()
	sess := newFakeUDPSession()
	go h.HandleUDP(sess, "10.0.0.0:9999")

	// Fill the cap with distinct IP-literal destinations. None are real
	// listeners — with AllowPrivateIP=true, resolveUDPDest's IP-literal path
	// is pure address parsing (net.ResolveUDPAddr), no network I/O, so this
	// is fast and deterministic.
	for i := 0; i < maxUDPDestsPerSession; i++ {
		sess.feed("x", fmt.Sprintf("10.0.%d.%d:9999", i/256, i%256))
	}

	// One more distinct destination past the cap — this one is a real,
	// reachable echo server. Under the pre-fix hard-freeze behavior this
	// would be dropped forever (len(dests) > cap short-circuited every new
	// destination for the rest of the session). Under LRU eviction it must
	// be admitted.
	addrReal, stopReal := echoUDPServer(t, "R")
	defer stopReal()
	sess.feed("real", addrReal)

	select {
	case p := <-sess.out:
		if string(p.payload) != "Rreal" {
			t.Fatalf("got %q, want Rreal", p.payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: destination past the cap was never admitted (hard-freeze regression)")
	}
	sess.Close()
}

// ============================================================================
// Unsolicited-inbound visibility (branch final review I1)
// ============================================================================

func TestAddressedPeers_RefcountsAliasedDests(t *testing.T) {
	p := newAddressedPeers()
	peer := netip.MustParseAddrPort("203.0.113.9:27015")

	if p.addressed(peer) {
		t.Fatal("empty set reported the peer as addressed")
	}

	// A hostname dest and an IP-literal dest can resolve to the same peer.
	ua := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 9), Port: 27015}
	p.add(ua)
	p.add(ua)
	if !p.addressed(peer) {
		t.Fatal("peer not addressed after add")
	}

	// Evicting one of the two dest keys must not un-address the peer.
	p.release(ua)
	if !p.addressed(peer) {
		t.Fatal("peer un-addressed after releasing only one of two aliases")
	}
	p.release(ua)
	if p.addressed(peer) {
		t.Fatal("peer still addressed after releasing the last alias")
	}
}

func TestAddressedPeers_UnmapsIPv4In6(t *testing.T) {
	p := newAddressedPeers()
	// net.ResolveUDPAddr may hand back a 16-byte 4-in-6 IPv4 while
	// ReadFromUDPAddrPort on an AF_INET socket yields a 4-byte one. They must
	// compare equal, or every reply would look unsolicited.
	p.add(&net.UDPAddr{IP: net.ParseIP("::ffff:203.0.113.9"), Port: 27015})
	if !p.addressed(netip.MustParseAddrPort("203.0.113.9:27015")) {
		t.Fatal("4-in-6 destination did not match its unmapped source form")
	}
}

func TestAddressedPeers_NilDestIsNoop(t *testing.T) {
	p := newAddressedPeers()
	p.add(nil)     // negative-cache entry
	p.release(nil) // evicting a negative-cache entry
	if len(p.m) != 0 {
		t.Fatalf("nil dest produced %d entries, want 0", len(p.m))
	}
}

// Full-cone egress accepts inbound from any source, and HandleUDP meters
// every byte it reads from the target against the user's device quota — so a
// third party spraying a session's egress port burns that user's quota. The
// branch decision is to keep metering everything (filtering to addressed
// sources would let a client farm free downlink from an unaddressed port),
// but the exposure must be observable, so the session's closing log line
// carries how much of its tx nobody asked for.
func TestHandleUDP_UnsolicitedInboundIsCounted(t *testing.T) {
	var buf bytes.Buffer
	var bufMu sync.Mutex
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &bufMu},
		&slog.HandlerOptions{Level: slog.LevelInfo})))

	learned := make(chan net.Addr, 1)
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer pc.Close()
	go func() {
		b := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFrom(b)
			if err != nil {
				return
			}
			select {
			case learned <- from:
			default:
			}
			pc.WriteTo(append([]byte("A"), b[:n]...), from)
		}
	}()
	addrA := pc.LocalAddr().String()

	h := newTestProxyHandler()
	sess := newFakeUDPSession()
	handlerDone := make(chan struct{})
	go func() { h.HandleUDP(sess, addrA); close(handlerDone) }()
	sess.feed("hello", addrA)

	select {
	case <-sess.out: // the solicited echo — must NOT be counted
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for initial echo")
	}

	var k2sEgress net.Addr
	select {
	case k2sEgress = <-learned:
	case <-time.After(3 * time.Second):
		t.Fatal("did not learn k2s egress addr")
	}

	thirdParty, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen thirdParty: %v", err)
	}
	defer thirdParty.Close()
	payload := []byte("0123456789")
	if _, err := thirdParty.WriteTo(payload, k2sEgress); err != nil {
		t.Fatalf("thirdParty write: %v", err)
	}
	select {
	case <-sess.out:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for third-party packet delivery")
	}

	sess.Close()
	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("HandleUDP did not return")
	}

	bufMu.Lock()
	logs := buf.String()
	bufMu.Unlock()

	want := []string{
		"srcUnsolicitedPkts=1",
		fmt.Sprintf("srcUnsolicitedBytes=%d", len(payload)),
	}
	for _, w := range want {
		if !strings.Contains(logs, w) {
			t.Errorf("session log missing %q — unsolicited inbound is invisible.\nlogs:\n%s", w, logs)
		}
	}
}
