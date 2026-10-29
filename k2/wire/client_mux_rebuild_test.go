package wire

// Regression guards for the UDP relay mux rebuild contract.
//
// smuxUDPMux's design comment (tcpws.go) has promised since commit 907f724:
//
//	"Consumers naturally unwind; TCPWSClient rebuilds a fresh mux on next
//	 DialUDP."
//
// The unwind half was implemented (smuxUDPConn.ReadFrom selects on mux.done);
// the rebuild half never was. 907f724 introduced a new mux lifecycle state
// ("dead") that writeFrame sets on any write failure, but DialUDP's cache
// lookup was never taught to read it — and the cached mux is only cleared
// when the whole smux *session* dies, which a stream-level failure does not
// cause.
//
// Field consequence (ticket #3420, Android 0.4.7, 2026-08-03): 4 real write
// failures produced 3309 rejected UDP sends across one 91-minute session.
// Every proxy DNS query failed instantly with errStreamDead while the
// heartbeat still reported health=healthy — "connected, but nothing loads".

import (
	"context"
	"net"
	"testing"
	"time"
)

// startUDPEcho serves one echo response per UDP session on the given server.
func startUDPEcho(t *testing.T, server *TCPWSServer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go server.ServeUDP(ctx, UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		buf := make([]byte, 512)
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		_, _ = conn.WriteTo(buf[:n], nil)
	}))
	// Give ServeUDP time to install its accept loop.
	time.Sleep(50 * time.Millisecond)
}

// roundTrip sends payload and waits for the echo, proving the relay stream
// actually carries traffic end-to-end (not merely that WriteTo returned nil).
func roundTrip(t *testing.T, pc net.PacketConn, payload string) {
	t.Helper()
	if _, err := pc.WriteTo([]byte(payload), nil); err != nil {
		t.Fatalf("WriteTo(%q): %v", payload, err)
	}
	if err := pc.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 512)
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom after %q: %v", payload, err)
	}
	if got := string(buf[:n]); got != payload {
		t.Fatalf("echo = %q, want %q", got, payload)
	}
}

// TestTCPWSClient_DialUDP_RebuildsMuxAfterDeath is the primary guard: once the
// UDP relay mux is dead, the next DialUDP must hand back a conn that actually
// works, end-to-end, against a real TCPWSServer.
//
// Before the fix this failed at the rebuild assertion — the cached dead mux
// was handed out forever, so every subsequent proxy DNS query died instantly
// for the remaining life of the smux session.
func TestTCPWSClient_DialUDP_RebuildsMuxAfterDeath(t *testing.T) {
	client, server, cleanup := setupTCPWSPair(t)
	defer cleanup()
	startUDPEcho(t, server)

	pc1, err := client.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("first DialUDP: %v", err)
	}
	defer pc1.Close()
	roundTrip(t, pc1, "before-death")

	mux1 := pc1.(*smuxUDPConn).mux

	// Kill the mux exactly the way writeFrame's failure path does
	// (dead flag + stream close), without waiting out the 5s write deadline.
	mux1.markDead()

	pc2, err := client.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP after mux death: %v", err)
	}
	defer pc2.Close()

	mux2 := pc2.(*smuxUDPConn).mux
	if mux2 == mux1 {
		t.Fatal("DialUDP returned the dead mux — cache was not evicted")
	}
	if mux2.dead.Load() {
		t.Fatal("DialUDP returned a mux that is already dead")
	}

	// The real assertion: the rebuilt stream carries traffic. This also
	// proves the server accepts a second UDP relay stream on the same smux
	// session (its session table is per-stream local state).
	roundTrip(t, pc2, "after-rebuild")
}

// TestTCPWSClient_DialUDP_ReusesLiveMux guards the other direction: the fix
// must not turn every DialUDP into a fresh smux stream. All UDP sessions
// share one mux by design; per-dial streams would leak server goroutines.
func TestTCPWSClient_DialUDP_ReusesLiveMux(t *testing.T) {
	client, server, cleanup := setupTCPWSPair(t)
	defer cleanup()
	startUDPEcho(t, server)

	pc1, err := client.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP #1: %v", err)
	}
	defer pc1.Close()
	pc2, err := client.DialUDP(context.Background(), "9.9.9.9:53")
	if err != nil {
		t.Fatalf("DialUDP #2: %v", err)
	}
	defer pc2.Close()

	c1, c2 := pc1.(*smuxUDPConn), pc2.(*smuxUDPConn)
	if c1.mux != c2.mux {
		t.Fatal("DialUDP rebuilt the mux while the existing one was alive")
	}
	if c1.sessionID == c2.sessionID {
		t.Fatalf("both conns got sessionID %d — allocator not shared", c1.sessionID)
	}
}

// TestQUICClient_DialStreamUDP_RebuildsMuxAfterDeath is the QUIC-side mirror.
// quicStreamUDPMux carries UDP whenever datagrams are unavailable, and its
// cache had the identical defect: only a full connection reset cleared it, so
// a stream-level death stranded every later UDP send.
func TestQUICClient_DialStreamUDP_RebuildsMuxAfterDeath(t *testing.T) {
	client, server, cleanup := setupQUICPair(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.ServeUDP(ctx, UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		buf := make([]byte, 512)
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		_, _ = conn.WriteTo(buf[:n], nil)
	}))
	time.Sleep(50 * time.Millisecond)

	pc1, err := client.DialStreamUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("first DialStreamUDP: %v", err)
	}
	defer pc1.Close()
	roundTrip(t, pc1, "before-death")

	mux1 := pc1.(*quicStreamUDPConn).mux
	mux1.markDead()

	pc2, err := client.DialStreamUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialStreamUDP after mux death: %v", err)
	}
	defer pc2.Close()

	mux2 := pc2.(*quicStreamUDPConn).mux
	if mux2 == mux1 {
		t.Fatal("DialStreamUDP returned the dead mux — cache was not evicted")
	}
	if mux2.dead.Load() {
		t.Fatal("DialStreamUDP returned a mux that is already dead")
	}
	roundTrip(t, pc2, "after-rebuild")
}

// TestSmuxUDPMux_ReadLoopExitMarksDead covers the server-initiated half: when
// the peer closes the UDP relay stream, readLoop exits. Until it also marks
// the mux dead, writes keep being accepted into a stream that is already gone
// (in #3420 the peer's EOF landed at 01:45:28 and writes only started failing
// at 01:45:50 — 22 seconds of silently discarded DNS queries).
func TestSmuxUDPMux_ReadLoopExitMarksDead(t *testing.T) {
	serverStream, clientStream, cleanup := setupSmuxPair(t)
	defer cleanup()

	mux := newSmuxUDPMux(serverStream)
	mux.register(1) // starts readLoop

	// Peer goes away.
	_ = clientStream.Close()

	deadline := time.After(2 * time.Second)
	for {
		if mux.dead.Load() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("readLoop exited but mux was never marked dead")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
