package test

// End-to-end coverage for the UDP multi-destination / full-cone work.
//
// The per-layer tests that precede this file each stub out the layer below:
// wire/udp_multidest_test.go drives the client and server PacketConns against
// a fake datagram sender, and server/handler_udp_fullcone_test.go drives
// HandleUDP against a fake session. Neither proves the layers agree — a frame
// the client writes has never actually been parsed by the real server, and the
// destination the server derives has never actually come off a real datagram.
//
// These tests close that gap: a real wire.QUICClient UDP session, real QUIC
// datagrams, the real wire server session demux, the real
// server.ProxyHandler, and real UDP echo servers on loopback.
//
// Loopback destinations are only reachable because setupQUICEnv sets
// handler.AllowPrivateIP — the pre-existing, production-default-off test hook
// (server/handler.go). The SSRF guard itself is never relaxed here; its
// production posture is locked down by TestResolveUDPDest_BlocksPrivateIP and
// TestHandleUDP_PrivateIPDestBlockedMidSession in the server package.

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// e2eUDPTimeout is the explicit per-operation budget these tests use instead
// of blocking forever. Callers get it clamped to whatever room t.Deadline()
// leaves, so a `go test -timeout` shorter than the budget still produces the
// test's own diagnostic instead of a bare panic-on-timeout stack dump.
const e2eUDPTimeout = 3 * time.Second

func udpDeadline(t *testing.T) time.Time {
	t.Helper()
	want := time.Now().Add(e2eUDPTimeout)
	if d, ok := t.Deadline(); ok {
		// Leave slack so t.Fatalf/cleanup still run inside the harness deadline.
		capped := d.Add(-500 * time.Millisecond)
		if capped.Before(want) {
			if !capped.After(time.Now()) {
				t.Fatalf("no time left inside t.Deadline() (%v) to run this test", d)
			}
			return capped
		}
	}
	return want
}

// litAddr is a net.Addr whose String() is the literal it was built from. The
// wire client embeds addr.String() verbatim into the frame's addr field, so
// this guarantees the destination on the wire is byte-for-byte the echo
// server's address (a *net.UDPAddr would re-format it).
type litAddr string

func (a litAddr) Network() string { return "udp" }
func (a litAddr) String() string  { return string(a) }

// emptyAddr is a non-nil net.Addr that formats to "". Handing it to the client
// conn's WriteTo produces a frame with addr_len == 0 — byte-identical to what
// a pre-multi-dest client emits for a subsequent packet. This is the only way
// to put a legacy uplink frame on a real wire without shipping a legacy
// client; nothing in production ever constructs it.
type emptyAddr struct{}

func (emptyAddr) Network() string { return "udp" }
func (emptyAddr) String() string  { return "" }

// startTaggedUDPEcho starts a loopback UDP echo server that prefixes tag to
// every reply, so a packet's origin is recoverable from its payload alone.
// seenFrom reports the source address of the first packet it observes — i.e.
// the k2s session's egress socket, which the full-cone test needs.
func startTaggedUDPEcho(t *testing.T, tag string) (addr string, seenFrom <-chan net.Addr) {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo %s: %v", tag, err)
	}
	t.Cleanup(func() { pc.Close() })

	from := make(chan net.Addr, 1)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, src, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			select {
			case from <- src:
			default:
			}
			pc.WriteTo(append([]byte(tag), buf[:n]...), src)
		}
	}()
	return pc.LocalAddr().String(), from
}

// readUDPPacket reads one packet off the client session with an explicit
// deadline, returning payload and source. what labels the read in failures.
func readUDPPacket(t *testing.T, pc net.PacketConn, deadline time.Time, what string) (string, string) {
	t.Helper()
	if err := pc.SetReadDeadline(deadline); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 2048)
	n, from, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom (%s): %v", what, err)
	}
	src := "<nil>"
	if from != nil {
		src = from.String()
	}
	return string(buf[:n]), src
}

// TestE2E_UDP_MultiDest_OneSession is the regression test for the original
// bug: one client source port addressing several peers. A game client uses a
// single socket for the game server, the matchmaker and a STUN reflector; the
// pre-fix client dropped every packet's real destination and delivered all of
// them to whatever the session was opened against, so B's traffic silently
// went to A.
//
// Both echoes must come back, and each must be attributed to the peer that
// actually sent it — a client that cannot tell A's reply from B's cannot
// demultiplex, even if the bytes arrive.
func TestE2E_UDP_MultiDest_OneSession(t *testing.T) {
	env := setupQUICEnv(t)
	defer env.Close()

	addrA, _ := startTaggedUDPEcho(t, "A")
	addrB, _ := startTaggedUDPEcho(t, "B")

	deadline := udpDeadline(t)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	// One session, opened against A.
	pc, err := env.quicClient.DialUDP(ctx, addrA)
	if err != nil {
		t.Fatalf("DialUDP(%s): %v", addrA, err)
	}
	defer pc.Close()

	if _, err := pc.WriteTo([]byte("one"), litAddr(addrA)); err != nil {
		t.Fatalf("WriteTo A: %v", err)
	}
	if _, err := pc.WriteTo([]byte("two"), litAddr(addrB)); err != nil {
		t.Fatalf("WriteTo B: %v", err)
	}

	// Payload -> observed source. Datagram ordering is not guaranteed, so
	// collect both before asserting.
	got := make(map[string]string, 2)
	for len(got) < 2 {
		payload, src := readUDPPacket(t, pc, deadline, "echo")
		if prev, dup := got[payload]; dup {
			t.Fatalf("duplicate payload %q (first from %s, again from %s); collected so far: %v",
				payload, prev, src, got)
		}
		got[payload] = src
	}

	if src, ok := got["Aone"]; !ok {
		t.Errorf("no echo from A: collected %v", got)
	} else if src != addrA {
		t.Errorf("A's echo attributed to %s, want %s (collected %v)", src, addrA, got)
	}
	if src, ok := got["Btwo"]; !ok {
		t.Errorf("no echo from B — B's packet was misdelivered to A: collected %v", got)
	} else if src != addrB {
		t.Errorf("B's echo attributed to %s, want %s (collected %v)", src, addrB, got)
	}
}

// TestE2E_UDP_FullCone_ThirdPartyReply proves endpoint-independent
// *filtering*, not just mapping. A connected DialUDP egress socket (the
// pre-fix server) is a symmetric NAT: the kernel drops anything arriving from
// a peer other than the one it was dialed against, which is exactly the
// packet a NAT-punching game or ICE agent depends on.
//
// The third party here has never been addressed by this session; its packet
// must still reach the client, carrying its own source.
func TestE2E_UDP_FullCone_ThirdPartyReply(t *testing.T) {
	env := setupQUICEnv(t)
	defer env.Close()

	addrA, seenFromA := startTaggedUDPEcho(t, "A")

	deadline := udpDeadline(t)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	pc, err := env.quicClient.DialUDP(ctx, addrA)
	if err != nil {
		t.Fatalf("DialUDP(%s): %v", addrA, err)
	}
	defer pc.Close()

	// Open the session so the server allocates its egress socket.
	if _, err := pc.WriteTo([]byte("hello"), litAddr(addrA)); err != nil {
		t.Fatalf("WriteTo A: %v", err)
	}
	if payload, src := readUDPPacket(t, pc, deadline, "initial echo"); payload != "Ahello" || src != addrA {
		t.Fatalf("initial echo = %q from %s, want %q from %s", payload, src, "Ahello", addrA)
	}

	// The egress port is whatever A saw as the source — that is the k2s
	// session's socket, learned the same way a real peer would learn it.
	var egress net.Addr
	select {
	case egress = <-seenFromA:
	case <-time.After(time.Until(deadline)):
		t.Fatal("echo server A never reported the k2s egress address")
	}

	thirdParty, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen third party: %v", err)
	}
	defer thirdParty.Close()

	if _, err := thirdParty.WriteTo([]byte("surprise"), egress); err != nil {
		t.Fatalf("third party WriteTo %v: %v", egress, err)
	}

	payload, src := readUDPPacket(t, pc, deadline, "third-party packet (full-cone filtering)")
	if payload != "surprise" {
		t.Errorf("third-party payload = %q, want %q", payload, "surprise")
	}
	if want := thirdParty.LocalAddr().String(); src != want {
		t.Errorf("third-party packet attributed to %s, want %s", src, want)
	}
}

// TestE2E_UDP_EmptyAddrFrameCompat is the release gate for mixed-version
// fleets. The frame format already carried a per-frame addr before this
// branch, with addr_len == 0 meaning "same as the session's first target", so
// new and old peers are supposed to interoperate in both directions. That
// claim is what makes a staged rollout safe, and nothing else tests it.
func TestE2E_UDP_EmptyAddrFrameCompat(t *testing.T) {
	// Old client -> new server: an uplink frame with no addr must be
	// forwarded to the session's first destination, not dropped and not
	// mis-parsed as a destination named "".
	t.Run("OldClientUplink", func(t *testing.T) {
		env := setupQUICEnv(t)
		defer env.Close()

		addrA, _ := startTaggedUDPEcho(t, "A")

		deadline := udpDeadline(t)
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		defer cancel()

		pc, err := env.quicClient.DialUDP(ctx, addrA)
		if err != nil {
			t.Fatalf("DialUDP(%s): %v", addrA, err)
		}
		defer pc.Close()

		// The session's first frame must carry the addr — that is how the
		// server learns the session target, for old and new clients alike.
		if _, err := pc.WriteTo([]byte("establish"), litAddr(addrA)); err != nil {
			t.Fatalf("WriteTo (establish): %v", err)
		}
		if payload, src := readUDPPacket(t, pc, deadline, "establish echo"); payload != "Aestablish" || src != addrA {
			t.Fatalf("establish echo = %q from %s, want %q from %s", payload, src, "Aestablish", addrA)
		}

		// Legacy subsequent packet: addr_len == 0.
		if _, err := pc.WriteTo([]byte("legacy"), emptyAddr{}); err != nil {
			t.Fatalf("WriteTo (empty addr): %v", err)
		}
		payload, src := readUDPPacket(t, pc, deadline, "legacy-frame echo")
		if payload != "Alegacy" {
			t.Errorf("legacy-frame echo = %q, want %q — empty-addr uplink was not forwarded to the session target", payload, "Alegacy")
		}
		if src != addrA {
			t.Errorf("legacy-frame echo attributed to %s, want %s", src, addrA)
		}
	})

	// Old server -> new client: a downlink frame with no addr must make the
	// client fall back to the session target rather than surface an empty
	// source (which would break every caller that keys state on the source).
	t.Run("OldServerDownlink", func(t *testing.T) {
		// A destination that is never dialed: the stand-in server answers
		// from its own goroutine and never touches the network, so this only
		// has to be a syntactically valid, non-reserved address. TEST-NET-2.
		const sessionTarget = "198.51.100.7:27015"

		// Stand-in for a pre-multi-dest server: it replies with a nil source
		// addr, which serverUDPConn encodes as addr_len == 0.
		oldServer := wire.UDPHandlerFunc(func(conn net.PacketConn, addr string) {
			defer conn.Close()
			buf := make([]byte, 2048)
			for {
				n, _, err := conn.ReadFrom(buf)
				if err != nil {
					return
				}
				if _, err := conn.WriteTo(append([]byte("OLD"), buf[:n]...), nil); err != nil {
					return
				}
			}
		})

		env := setupQUICEnvUDP(t, oldServer)
		defer env.Close()

		deadline := udpDeadline(t)
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		defer cancel()

		pc, err := env.quicClient.DialUDP(ctx, sessionTarget)
		if err != nil {
			t.Fatalf("DialUDP(%s): %v", sessionTarget, err)
		}
		defer pc.Close()

		if _, err := pc.WriteTo([]byte("ping"), nil); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}

		payload, src := readUDPPacket(t, pc, deadline, "old-server reply")
		if payload != "OLDping" {
			t.Errorf("reply = %q, want %q", payload, "OLDping")
		}
		if src != sessionTarget {
			t.Errorf("empty-addr downlink attributed to %s, want fallback to session target %s", src, sessionTarget)
		}
	})
}
