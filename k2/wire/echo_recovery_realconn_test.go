package wire

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"
	tls "github.com/metacubex/utls"
)

// The tests in this file drive TransportManager.runProbeOnce — the QUIC
// recovery probe that arms quicEchoRecovered, the single gate for rolling
// tcpws back to QUIC — through a REAL *QUICClient against a REAL quic-go
// server on loopback. No synthetic error values: every error the probe
// classifies is whatever quic-go actually returns for the scripted server
// behavior. They pin the three server shapes the recovery gate has to tell
// apart:
//
//   - connection dies after a successful handshake  → NOT armed
//   - live connection, server predates echo (EOF)   → armed (backward compat)
//   - live connection, server answers echo          → armed
//
// Only the first changed with the ErrEchoNoConnection fix: before it, the
// echo error on a connection the server had already torn down was a plain
// non-timeout error, IsEchoUnsupported's fail-open default read it as "old
// server", echoOK stayed true and the probe approved a rollback onto a dead
// connection. The other two are the controls proving the gate still opens.

// startScriptedQUICServer boots a real QUIC listener and hands every accepted
// connection to serve. Returns the server address and a cleanup.
func startScriptedQUICServer(t *testing.T, serve func(conn *quic.Conn)) (*net.UDPAddr, func()) {
	t.Helper()
	cert := generateTestCert(t)

	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	serverTransport := &quic.Transport{Conn: serverUDP}
	ln, err := serverTransport.Listen(&tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		for {
			conn, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
			go serve(conn)
		}
	}()

	addr := serverUDP.LocalAddr().(*net.UDPAddr)
	return addr, func() {
		ln.Close()
		serverTransport.Close()
		serverUDP.Close()
	}
}

// answerMetadata completes the k2 metadata handshake on the connection's
// first stream exactly like a real server, so QUICClient.connect() succeeds.
func answerMetadata(conn *quic.Conn) bool {
	stream, err := conn.AcceptStream(context.Background())
	if err != nil {
		return false
	}
	br := bufio.NewReader(stream)
	frameType, _, err := ReadVarint(br)
	if err != nil || frameType != H3FrameTypeK2Proxy {
		return false
	}
	streamType, _, err := ReadStreamHeader(br)
	if err != nil || streamType != StreamTypeMeta {
		return false
	}
	if _, err := ReadMetadataFrame(br); err != nil {
		return false
	}
	_ = WriteStreamResponse(stream, nil)
	_ = WriteMetadataResponse(stream, "", false)
	stream.Close()
	return true
}

// newRecoveryProbeTM wires a real QUICClient (dialing addr) as the quic
// primary of a TransportManager sitting in tcpws fallback — the only state in
// which runProbeOnce does anything.
func newRecoveryProbeTM(t *testing.T, addr *net.UDPAddr) (*TransportManager, *QUICClient) {
	t.Helper()
	qc := NewQUICClient(Config{Host: "127.0.0.1", Port: addr.Port}, NewCookieJar())
	// A hostname SNI, or kuic drops the fingerprinted ClientHello and the
	// handshake fails — which would make the "not armed" assertions pass for
	// the wrong reason (see TestProbe_RollbackRequiresEchoNotJustHandshake).
	qc.SetTLSConfig(&tls.Config{ServerName: "localhost", InsecureSkipVerify: true, NextProtos: []string{"h3"}})
	t.Cleanup(func() { qc.Close() })

	tm := NewTransportManager()
	tm.SetPrimary(qc, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})
	tm.forceActiveForTest("tcpws")
	return tm, qc
}

// runRecoveryProbe runs one runProbeOnce, retrying (at most twice more) only
// when the probe's fresh QUIC handshake itself failed — which on this heavily
// loaded CI/dev hardware occasionally happens even on loopback (5s handshake
// budget). A failed handshake leaves quicEchoRecovered false for a reason
// unrelated to echo classification, so it is retried rather than asserted on;
// any echo-stage outcome is final.
func runRecoveryProbe(t *testing.T, tm *TransportManager, qc *QUICClient) {
	t.Helper()
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		tm.runProbeOnceForTest(ctx)
		cancel()
		if qc.ConsecConnFails() == 0 || attempt == 3 {
			return
		}
		t.Logf("attempt %d: probe handshake failed (load?), retrying", attempt)
	}
}

// TestRunProbeOnce_ConnDiesAfterHandshake_NotArmed: the server completes the
// QUIC handshake AND the k2 metadata handshake (so runProbeOnce's fresh
// connect() succeeds and it proceeds to echo), then tears the whole
// connection down the moment the echo stream arrives. That is a node that
// accepts and immediately drops — the recovery probe must not approve a
// rollback onto it.
//
// Mutation-verified: deleting the `conn.Context().Err() != nil → echoNotProbed`
// relabel in QUICClient.RunEchoProbe makes all three echo rounds read as
// "unsupported" and this test fails with quicEchoRecovered armed — i.e. it
// reproduces the pre-fix behavior over a real connection.
func TestRunProbeOnce_ConnDiesAfterHandshake_NotArmed(t *testing.T) {
	var metaOK, echoStreams atomic.Int32
	addr, cleanup := startScriptedQUICServer(t, func(conn *quic.Conn) {
		if !answerMetadata(conn) {
			return
		}
		metaOK.Add(1)
		if _, err := conn.AcceptStream(context.Background()); err != nil {
			return
		}
		echoStreams.Add(1)
		conn.CloseWithError(0, "server going away")
	})
	defer cleanup()

	tm, qc := newRecoveryProbeTM(t, addr)
	runRecoveryProbe(t, tm, qc)

	// Preconditions: the probe really reached the echo stage on a connection
	// whose handshake succeeded — otherwise "not armed" is the handshake-
	// failure early return and proves nothing about the echo classification.
	if metaOK.Load() != 1 {
		t.Fatalf("precondition: metadata handshakes = %d, want 1 (connect must succeed)", metaOK.Load())
	}
	if echoStreams.Load() != 1 {
		t.Fatalf("precondition: echo streams seen by server = %d, want 1", echoStreams.Load())
	}
	if !tm.FallbackActive() {
		t.Fatal("precondition: fallback must still be active (result would be discarded otherwise)")
	}

	if tm.quicEchoRecovered.Load() {
		t.Fatal("quicEchoRecovered armed although the server killed the connection after the handshake — rollback onto a dead QUIC connection")
	}
}

// TestRunProbeOnce_PreEchoServerLiveConn_Armed: the backward-compat control.
// The server predates StreamTypeEcho: for each echo stream it parses the
// header, hits handleStream's `default:` and Close()s the stream (FIN) — the
// connection stays up. The client reads EOF on a live connection, which is
// "unsupported", and an unsupported server must still be eligible for
// rollback (otherwise every user on an old node is stranded on tcpws).
// Must NOT be relabelled not-probed: conn.Context() is still alive.
func TestRunProbeOnce_PreEchoServerLiveConn_Armed(t *testing.T) {
	var echoStreams atomic.Int32
	addr, cleanup := startScriptedQUICServer(t, func(conn *quic.Conn) {
		if !answerMetadata(conn) {
			return
		}
		for {
			stream, err := conn.AcceptStream(context.Background())
			if err != nil {
				return
			}
			echoStreams.Add(1)
			br := bufio.NewReader(stream)
			ReadVarint(br)
			ReadStreamHeader(br)
			stream.Close()
		}
	})
	defer cleanup()

	tm, qc := newRecoveryProbeTM(t, addr)
	runRecoveryProbe(t, tm, qc)

	if !tm.quicEchoRecovered.Load() {
		t.Fatalf("pre-echo server on a live connection must still arm quicEchoRecovered (backward compat); echo rounds seen by server = %d", echoStreams.Load())
	}
	if got := echoStreams.Load(); got != 3 {
		t.Fatalf("echo rounds seen by server = %d, want 3", got)
	}
	// And the classification really came from a live connection.
	qc.mu.Lock()
	c := qc.conn
	qc.mu.Unlock()
	if c == nil || c.Context().Err() != nil {
		t.Fatal("the probed connection must still be alive — the unsupported verdict has to come from a live connection")
	}
}

// TestRunProbeOnce_EchoCapableServer_Armed: the positive control with the
// production QUICServer (which answers echo). Three real echo round trips
// succeed and the rollback gate opens.
func TestRunProbeOnce_EchoCapableServer_Armed(t *testing.T) {
	client, _, cleanup := setupQUICPair(t)
	defer cleanup()

	tm := NewTransportManager()
	tm.SetPrimary(client, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})
	tm.forceActiveForTest("tcpws")
	runRecoveryProbe(t, tm, client)

	if !tm.quicEchoRecovered.Load() {
		t.Fatal("echo-capable server answered all rounds, but quicEchoRecovered is not armed")
	}
	if d := tm.evaluateSelector(time.Now()); d != channelSwitchToQUIC {
		t.Fatalf("evaluateSelector = %v, want channelSwitchToQUIC", d)
	}
}

// TestQUICRunEchoProbe_ResetThenLazyRedial: the echo-probe-loop shape over a
// real connection. A live probe succeeds; Reset drops the cached connection
// and the next probe is not-probed (not unsupported); the next lazy dial
// (connect, as DialTCP would do) rebuilds it and echo succeeds again — so the
// not-probed state is transient and self-heals without any engine action.
func TestQUICRunEchoProbe_ResetThenLazyRedial(t *testing.T) {
	client, _, cleanup := setupQUICPair(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.RunEchoProbe(ctx); err != nil {
		t.Fatalf("echo on live conn: %v", err)
	}

	client.Reset()
	err := client.RunEchoProbe(ctx)
	if !IsEchoNotProbed(err) || IsEchoUnsupported(err) {
		t.Fatalf("after Reset: err=%v notProbed=%v unsupported=%v, want notProbed only", err, IsEchoNotProbed(err), IsEchoUnsupported(err))
	}

	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("lazy redial: %v", err)
	}
	if err := client.RunEchoProbe(ctx); err != nil {
		t.Fatalf("echo after lazy redial: %v", err)
	}
}

// TestIsQUICConnClosedErr_RealQerrValues pins the producer-side predicate
// over the real quic error types (wrapped the way EchoProbe wraps them) and
// its interaction with echoNotProbed: connection-level closes become
// not-probed, the two timeout-shaped connection errors pass through as
// failures, and stream-level outcomes (the pre-echo server's signature) are
// never matched.
func TestIsQUICConnClosedErr_RealQerrValues(t *testing.T) {
	wrap := func(e error) error { return fmt.Errorf("echo: read response: %w", e) }
	cases := []struct {
		name          string
		err           error
		closed        bool
		wantNotProbed bool
	}{
		{"remote application close", wrap(&quic.ApplicationError{Remote: true, ErrorMessage: "bye"}), true, true},
		{"local application close", wrap(&quic.ApplicationError{ErrorMessage: "connection reset"}), true, true},
		{"transport error", wrap(&quic.TransportError{Remote: true, ErrorCode: quic.InternalError}), true, true},
		{"stateless reset", wrap(&quic.StatelessResetError{}), true, true},
		{"version negotiation", wrap(&quic.VersionNegotiationError{}), true, true},
		{"idle timeout", wrap(&quic.IdleTimeoutError{}), true, false},
		{"handshake timeout", wrap(&quic.HandshakeTimeoutError{}), true, false},
		{"stream reset (not conn-level)", wrap(&quic.StreamError{StreamID: 4, ErrorCode: 0, Remote: true}), false, false},
		{"EOF (pre-echo server)", wrap(io.EOF), false, false},
		{"unexpected EOF", wrap(io.ErrUnexpectedEOF), false, false},
	}
	for _, c := range cases {
		if got := isQUICConnClosedErr(c.err); got != c.closed {
			t.Errorf("%s: isQUICConnClosedErr = %v, want %v", c.name, got, c.closed)
		}
		if !c.closed {
			continue
		}
		got := echoNotProbed(c.err)
		if IsEchoNotProbed(got) != c.wantNotProbed {
			t.Errorf("%s: echoNotProbed → notProbed=%v, want %v (%v)", c.name, IsEchoNotProbed(got), c.wantNotProbed, got)
		}
		if !c.wantNotProbed && IsEchoUnsupported(got) {
			t.Errorf("%s: timeout-shaped close must stay a failure, but reads unsupported", c.name)
		}
	}
}

// TestQUICRunEchoProbe_RealIdleTimeoutStaysFailure: a real quic-go idle
// timeout (peer goes silent mid-echo, short MaxIdleTimeout, no keepalive) is
// connection-level AND kills conn.Context(), so RunEchoProbe takes the
// relabel branch — and must still hand back a strike-worthy failure, not
// not-probed or unsupported. This is the black-hole case echo exists to catch
// (ticket #3463).
func TestQUICRunEchoProbe_RealIdleTimeoutStaysFailure(t *testing.T) {
	ln, addr, cleanupServer := echoTestServer(t)
	defer cleanupServer()
	done := make(chan struct{})
	defer close(done)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		if _, err := conn.AcceptStream(context.Background()); err != nil {
			return
		}
		<-done // never answer: client sees silence until its idle timer fires
	}()

	clientUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	clientTransport := &quic.Transport{Conn: clientUDP}
	defer clientUDP.Close()
	defer clientTransport.Close()
	cfg := DefaultQUICConfig()
	cfg.MaxIdleTimeout = 300 * time.Millisecond
	cfg.KeepAlivePeriod = 0
	conn, err := clientTransport.Dial(context.Background(), addr,
		&tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h3"}}, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Drive the production RunEchoProbe on this connection.
	qc := NewQUICClient(Config{Host: "127.0.0.1", Port: addr.Port}, nil)
	qc.mu.Lock()
	qc.conn = conn
	qc.mu.Unlock()
	defer func() {
		qc.mu.Lock()
		qc.conn = nil
		qc.mu.Unlock()
		qc.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = qc.RunEchoProbe(ctx)

	var idle *quic.IdleTimeoutError
	if !errors.As(err, &idle) {
		t.Fatalf("precondition: want a real *quic.IdleTimeoutError, got %v", err)
	}
	if conn.Context().Err() == nil {
		t.Fatal("precondition: idle timeout must have killed the connection context")
	}
	if IsEchoNotProbed(err) || IsEchoUnsupported(err) {
		t.Fatalf("idle timeout relabelled: notProbed=%v unsupported=%v (%v) — a black-holed path would stop striking",
			IsEchoNotProbed(err), IsEchoUnsupported(err), err)
	}
}
