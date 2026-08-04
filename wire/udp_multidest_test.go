package wire

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apernet/quic-go"
)

// fakeDatagramSender captures frames written by quicUDPConn.
type fakeDatagramSender struct {
	sent [][]byte
}

func (f *fakeDatagramSender) SendDatagram(b []byte) error {
	cp := make([]byte, len(b))
	copy(cp, b)
	f.sent = append(f.sent, cp)
	return nil
}
func (f *fakeDatagramSender) LocalAddr() net.Addr { return &net.UDPAddr{} }

func newTestQUICUDPConn(sender datagramSender) *quicUDPConn {
	mux := &quicUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		done:        make(chan struct{}),
	}
	return &quicUDPConn{
		qconn:      sender,
		sessionID:  7,
		targetAddr: "1.1.1.1:443",
		mux:        mux,
		readCh:     make(chan *UDPFrame, 4),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
	}
}

func TestQUICUDPConn_WriteTo_PerPacketDest(t *testing.T) {
	sender := &fakeDatagramSender{}
	c := newTestQUICUDPConn(sender)

	// nil addr → session target
	if _, err := c.WriteTo([]byte("a"), nil); err != nil {
		t.Fatalf("WriteTo(nil): %v", err)
	}
	// explicit addr → that addr
	if _, err := c.WriteTo([]byte("b"), rawAddr("2.2.2.2:27015")); err != nil {
		t.Fatalf("WriteTo(addr): %v", err)
	}

	if len(sender.sent) != 2 {
		t.Fatalf("sent %d frames, want 2", len(sender.sent))
	}
	f0, err := ReadUDPFrame(newBytesReader(sender.sent[0]))
	if err != nil {
		t.Fatalf("parse frame 0: %v", err)
	}
	if f0.Addr != "1.1.1.1:443" {
		t.Errorf("frame 0 addr = %q, want 1.1.1.1:443", f0.Addr)
	}
	f1, err := ReadUDPFrame(newBytesReader(sender.sent[1]))
	if err != nil {
		t.Fatalf("parse frame 1: %v", err)
	}
	if f1.Addr != "2.2.2.2:27015" {
		t.Errorf("frame 1 addr = %q, want 2.2.2.2:27015", f1.Addr)
	}
}

func TestQUICUDPConn_ReadFrom_ReturnsFrameSource(t *testing.T) {
	c := newTestQUICUDPConn(&fakeDatagramSender{})

	// Frame carrying a real source (new server) → that source is returned.
	c.readCh <- &UDPFrame{SessionID: 7, Addr: "3.3.3.3:27015", Payload: []byte("x")}
	buf := make([]byte, 16)
	n, from, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if n != 1 || from.String() != "3.3.3.3:27015" {
		t.Fatalf("n=%d from=%v, want 1 / 3.3.3.3:27015", n, from)
	}

	// Frame with empty addr (old server) → falls back to session target.
	c.readCh <- &UDPFrame{SessionID: 7, Addr: "", Payload: []byte("y")}
	_, from2, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom 2: %v", err)
	}
	if from2.String() != "1.1.1.1:443" {
		t.Fatalf("from2 = %v, want 1.1.1.1:443", from2)
	}
}

func newBytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

func TestServerUDPConn_PerPacketAddrRoundTrip(t *testing.T) {
	sender := &fakeDatagramSender{}
	var inflight atomic.Int32
	c := &serverUDPConn{
		qconn:        sender,
		sessionID:    9,
		addr:         "1.1.1.1:443",
		log:          slog.Default(),
		readCh:       make(chan *UDPFrame, 4),
		done:         make(chan struct{}),
		sendInflight: &inflight,
	}

	// Uplink: a frame addressed to a second destination surfaces that dest.
	c.deliver(&UDPFrame{SessionID: 9, Addr: "2.2.2.2:27015", Payload: []byte("hi")})
	buf := make([]byte, 16)
	n, to, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if n != 2 || to.String() != "2.2.2.2:27015" {
		t.Fatalf("n=%d to=%v, want 2 / 2.2.2.2:27015", n, to)
	}

	// Uplink with empty addr (subsequent-packet optimization) → session addr.
	c.deliver(&UDPFrame{SessionID: 9, Addr: "", Payload: []byte("yo")})
	_, to2, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom 2: %v", err)
	}
	if to2.String() != "1.1.1.1:443" {
		t.Fatalf("to2 = %v, want 1.1.1.1:443", to2)
	}

	// Downlink: the real source is carried back in the frame addr.
	if _, err := c.WriteTo([]byte("pong"), rawAddr("3.3.3.3:27015")); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent %d frames, want 1", len(sender.sent))
	}
	f, err := ReadUDPFrame(newBytesReader(sender.sent[0]))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.Addr != "3.3.3.3:27015" {
		t.Errorf("downlink frame addr = %q, want 3.3.3.3:27015", f.Addr)
	}
}

func TestQUICUDPMux_CountsTxDrops(t *testing.T) {
	sender := &fakeDatagramSender{}
	c := newTestQUICUDPConn(sender)
	// Saturate the inflight budget so the next write is dropped.
	c.mux.sendInflight.Store(maxDatagramInflight)

	if _, err := c.WriteTo([]byte("x"), nil); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := c.mux.udpTxDrops.Load(); got != 1 {
		t.Fatalf("mux udpTxDrops = %d, want 1", got)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("sent %d frames, want 0 (dropped)", len(sender.sent))
	}
}

func TestQUICUDPMux_CountsRxDrops(t *testing.T) {
	sessionCh := make(chan *UDPFrame, 32)
	m := &quicUDPMux{
		sessions:    map[uint32]chan *UDPFrame{5: sessionCh},
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
	}
	// Saturate the session's read channel so the next dispatch is dropped.
	for i := 0; i < cap(sessionCh); i++ {
		sessionCh <- &UDPFrame{SessionID: 5}
	}

	m.dispatchFrame(&UDPFrame{SessionID: 5, Payload: []byte("x")})

	if got := m.udpRxDrops.Load(); got != 1 {
		t.Fatalf("mux udpRxDrops = %d, want 1", got)
	}
	if len(sessionCh) != cap(sessionCh) {
		t.Fatalf("sessionCh len = %d, want unchanged at cap %d (dropped, not delivered)", len(sessionCh), cap(sessionCh))
	}
	if got := m.udpRxDropsNoSession.Load(); got != 0 {
		t.Fatalf("mux udpRxDropsNoSession = %d, want 0 (the session exists)", got)
	}
}

// A frame for a session the client no longer knows must land in its OWN
// counter. Folding it into udpRxDrops would make that number scale with
// session churn — every closed session gets a short tail of late packets —
// and the canary reads udpRxDrops as "the user is losing packets".
func TestQUICUDPMux_CountsNoSessionDropsSeparately(t *testing.T) {
	m := &quicUDPMux{
		sessions:    map[uint32]chan *UDPFrame{},
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
	}

	m.dispatchFrame(&UDPFrame{SessionID: 9, Payload: []byte("late")})

	if got := m.udpRxDropsNoSession.Load(); got != 1 {
		t.Fatalf("mux udpRxDropsNoSession = %d, want 1", got)
	}
	if got := m.udpRxDrops.Load(); got != 0 {
		t.Fatalf("mux udpRxDrops = %d, want 0 (no live session lost a packet)", got)
	}
}

// The QUIC stream-UDP fallback used to drop silently: DIAG log lines only, no
// counter, so the heartbeat reported 0 for a path that was actively dropping.
func TestQUICStreamUDPMux_CountsRxDrops(t *testing.T) {
	sessionCh := make(chan *UDPFrame, 32)
	m := &quicStreamUDPMux{
		sessions:    map[uint32]chan *UDPFrame{5: sessionCh},
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
	}
	for i := 0; i < cap(sessionCh); i++ {
		sessionCh <- &UDPFrame{SessionID: 5}
	}

	m.dispatchFrame(&UDPFrame{SessionID: 5, Payload: []byte("x")})
	m.dispatchFrame(&UDPFrame{SessionID: 9, Payload: []byte("late")})

	if got := m.udpRxDrops.Load(); got != 1 {
		t.Errorf("stream mux udpRxDrops = %d, want 1", got)
	}
	if got := m.udpRxDropsNoSession.Load(); got != 1 {
		t.Errorf("stream mux udpRxDropsNoSession = %d, want 1", got)
	}
	if len(sessionCh) != cap(sessionCh) {
		t.Errorf("sessionCh len = %d, want unchanged at cap %d", len(sessionCh), cap(sessionCh))
	}
}

// Same for the TCP-WS fallback — the state a user is most likely to be
// stuttering in, and the one that reported nothing at all.
func TestSmuxUDPMux_CountsRxDrops(t *testing.T) {
	sessionCh := make(chan *UDPFrame, 32)
	m := &smuxUDPMux{
		sessions:    map[uint32]chan *UDPFrame{5: sessionCh},
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
	}
	for i := 0; i < cap(sessionCh); i++ {
		sessionCh <- &UDPFrame{SessionID: 5}
	}

	m.dispatchFrame(&UDPFrame{SessionID: 5, Payload: []byte("x")})
	m.dispatchFrame(&UDPFrame{SessionID: 9, Payload: []byte("late")})

	if got := m.udpRxDrops.Load(); got != 1 {
		t.Errorf("smux mux udpRxDrops = %d, want 1", got)
	}
	if got := m.udpRxDropsNoSession.Load(); got != 1 {
		t.Errorf("smux mux udpRxDropsNoSession = %d, want 1", got)
	}
	if len(sessionCh) != cap(sessionCh) {
		t.Errorf("sessionCh len = %d, want unchanged at cap %d", len(sessionCh), cap(sessionCh))
	}
}

// TCPWSClient.WireStats returned a bare WireStats{Transport: "tcpws"}, so a
// heartbeat sampled during a QUIC fallback reported udpRxDrops=0 regardless of
// what the mux was doing. That is the structural zero this branch has to stop
// shipping, because canary triage reads exactly that field.
func TestTCPWSClient_WireStats_ReportsUDPDrops(t *testing.T) {
	c := &TCPWSClient{}

	// No mux yet (no UDP session ever opened): zeros, and no panic.
	if s := c.WireStats(); s.Transport != "tcpws" || s.UDPRxDrops != 0 {
		t.Fatalf("WireStats with no mux = %+v, want tcpws with zero drops", s)
	}

	mux := &smuxUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
	}
	mux.udpRxDrops.Store(7)
	mux.udpRxDropsNoSession.Store(3)
	c.udpMux = mux

	s := c.WireStats()
	if s.UDPRxDrops != 7 {
		t.Errorf("WireStats.UDPRxDrops = %d, want 7", s.UDPRxDrops)
	}
	if s.UDPRxDropsNoSession != 3 {
		t.Errorf("WireStats.UDPRxDropsNoSession = %d, want 3", s.UDPRxDropsNoSession)
	}
	// Not a gap: smux writes fail loudly rather than dropping, so there is no
	// uplink drop to report on this transport.
	if s.UDPTxDrops != 0 {
		t.Errorf("WireStats.UDPTxDrops = %d, want 0 (no lossy send path on smux)", s.UDPTxDrops)
	}
}

// ============================================================================
// quicStreamUDPConn / quicStreamServerUDPConn (QUIC stream-UDP fallback path,
// used whenever datagram negotiation fails — see QUICClient.DialUDP fallback
// to dialUDPStream). Same per-packet dest/src contract as quicUDPConn above,
// but writeFrame writes directly to a *quic.Stream (concrete type), so a real
// loopback QUIC stream pair is needed instead of a fake sender.
// ============================================================================

// setupQUICStreamPair returns a connected client/server *quic.Stream pair over
// a real loopback QUIC connection, for testing frame-level protocols that
// write/read directly on *quic.Stream (quicStreamUDPMux, quicStreamWriter).
func setupQUICStreamPair(t *testing.T) (clientStream, serverStream *quic.Stream, cleanup func()) {
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
	serverAddr := serverUDP.LocalAddr().(*net.UDPAddr)

	clientUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	clientTransport := &quic.Transport{Conn: clientUDP}

	serverConnCh := make(chan *quic.Conn, 1)
	serverErrCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			serverErrCh <- err
			return
		}
		serverConnCh <- conn
	}()

	clientConn, err := clientTransport.Dial(context.Background(), serverAddr, &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}

	var serverConn *quic.Conn
	select {
	case serverConn = <-serverConnCh:
	case err := <-serverErrCh:
		t.Fatalf("server accept: %v", err)
	}

	// AcceptStream blocks until the peer actually sends a frame on the
	// stream — OpenStreamSync alone only reserves a local stream ID and
	// doesn't make the stream visible to the remote side. So the accept
	// must run concurrently with (and complete after) the client's first
	// write, which we do here as a priming handshake: the same H3 frame
	// type + stream header that dialUDPStream/handleUDPStream exchange in
	// production, consumed here so it doesn't leak into the UDP frames the
	// test reads/writes afterward.
	ssCh := make(chan *quic.Stream, 1)
	ssErrCh := make(chan error, 1)
	go func() {
		ss, err := serverConn.AcceptStream(context.Background())
		if err != nil {
			ssErrCh <- err
			return
		}
		ssCh <- ss
	}()

	cs, err := clientConn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteH3ProxyFrameType(cs); err != nil {
		t.Fatal(err)
	}
	if err := WriteStreamHeader(cs, StreamTypeUDP, ""); err != nil {
		t.Fatal(err)
	}

	var ss *quic.Stream
	select {
	case ss = <-ssCh:
	case err := <-ssErrCh:
		t.Fatalf("server accept stream: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server AcceptStream")
	}

	br := bufio.NewReader(ss)
	if _, _, err := ReadVarint(br); err != nil {
		t.Fatalf("read H3 frame type: %v", err)
	}
	if _, _, err := ReadStreamHeader(br); err != nil {
		t.Fatalf("read stream header: %v", err)
	}

	cleanup = func() {
		cs.Close()
		ss.Close()
		clientConn.CloseWithError(0, "")
		serverConn.CloseWithError(0, "")
		clientTransport.Close()
		serverTransport.Close()
	}
	return cs, ss, cleanup
}

func newTestQUICStreamUDPMux(stream *quic.Stream) *quicStreamUDPMux {
	return &quicStreamUDPMux{
		stream:      stream,
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
	}
}

func TestQUICStreamUDPConn_WriteTo_PerPacketDest(t *testing.T) {
	clientStream, serverStream, cleanup := setupQUICStreamPair(t)
	defer cleanup()

	mux := newTestQUICStreamUDPMux(clientStream)
	c := &quicStreamUDPConn{
		mux:        mux,
		sessionID:  7,
		targetAddr: "1.1.1.1:443",
		readCh:     make(chan *UDPFrame, 4),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
	}

	// nil addr → session target
	if _, err := c.WriteTo([]byte("a"), nil); err != nil {
		t.Fatalf("WriteTo(nil): %v", err)
	}
	// explicit addr → that addr
	if _, err := c.WriteTo([]byte("b"), rawAddr("2.2.2.2:27015")); err != nil {
		t.Fatalf("WriteTo(addr): %v", err)
	}

	br := bufio.NewReader(serverStream)
	f0, err := ReadSmuxUDPFrame(br)
	if err != nil {
		t.Fatalf("parse frame 0: %v", err)
	}
	if f0.Addr != "1.1.1.1:443" {
		t.Errorf("frame 0 addr = %q, want 1.1.1.1:443", f0.Addr)
	}
	f1, err := ReadSmuxUDPFrame(br)
	if err != nil {
		t.Fatalf("parse frame 1: %v", err)
	}
	if f1.Addr != "2.2.2.2:27015" {
		t.Errorf("frame 1 addr = %q, want 2.2.2.2:27015", f1.Addr)
	}
}

func TestQUICStreamUDPConn_ReadFrom_ReturnsFrameSource(t *testing.T) {
	mux := newTestQUICStreamUDPMux(nil) // no write in this test — real stream not needed
	c := &quicStreamUDPConn{
		mux:        mux,
		sessionID:  7,
		targetAddr: "1.1.1.1:443",
		readCh:     make(chan *UDPFrame, 4),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
	}

	// Frame carrying a real source (new server) → that source is returned.
	c.readCh <- &UDPFrame{SessionID: 7, Addr: "3.3.3.3:27015", Payload: []byte("x")}
	buf := make([]byte, 16)
	n, from, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if n != 1 || from.String() != "3.3.3.3:27015" {
		t.Fatalf("n=%d from=%v, want 1 / 3.3.3.3:27015", n, from)
	}

	// Frame with empty addr (old server / subsequent-packet optimization) →
	// falls back to session target.
	c.readCh <- &UDPFrame{SessionID: 7, Addr: "", Payload: []byte("y")}
	_, from2, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom 2: %v", err)
	}
	if from2.String() != "1.1.1.1:443" {
		t.Fatalf("from2 = %v, want 1.1.1.1:443", from2)
	}
}

func TestQuicStreamServerUDPConn_PerPacketAddrRoundTrip(t *testing.T) {
	clientStream, serverStream, cleanup := setupQUICStreamPair(t)
	defer cleanup()

	c := &quicStreamServerUDPConn{
		writer:    &quicStreamWriter{stream: serverStream},
		sessionID: 9,
		addr:      "1.1.1.1:443",
		log:       slog.Default(),
		readCh:    make(chan *UDPFrame, 4),
		done:      make(chan struct{}),
	}

	// Uplink: a frame addressed to a second destination surfaces that dest.
	c.deliver(&UDPFrame{SessionID: 9, Addr: "2.2.2.2:27015", Payload: []byte("hi")})
	buf := make([]byte, 16)
	n, to, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if n != 2 || to.String() != "2.2.2.2:27015" {
		t.Fatalf("n=%d to=%v, want 2 / 2.2.2.2:27015", n, to)
	}

	// Uplink with empty addr (subsequent-packet optimization) → session addr.
	c.deliver(&UDPFrame{SessionID: 9, Addr: "", Payload: []byte("yo")})
	_, to2, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom 2: %v", err)
	}
	if to2.String() != "1.1.1.1:443" {
		t.Fatalf("to2 = %v, want 1.1.1.1:443", to2)
	}

	// Downlink: the real source is carried back in the frame addr.
	if _, err := c.WriteTo([]byte("pong"), rawAddr("3.3.3.3:27015")); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	br := bufio.NewReader(clientStream)
	f, err := ReadSmuxUDPFrame(br)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.Addr != "3.3.3.3:27015" {
		t.Errorf("downlink frame addr = %q, want 3.3.3.3:27015", f.Addr)
	}
}

// QUICClient.WireStats folds the stream-UDP mux's drop counters into the
// connection's totals — sessions on a datagram-less server run there, and
// their drops belong to the same connection. Nothing exercised that merge:
// replacing its body with `_ = sm` left the whole wire package green, which
// is the "green test never reached its target" shape. It also sits on exactly
// the number canary triage reads, so a broken merge during a datagram→stream
// fallback would surface as a structural 0 with nobody the wiser.
func TestQUICClient_WireStats_FoldsStreamMuxDrops(t *testing.T) {
	newMuxMaps := func() (map[uint32]chan *UDPFrame, map[uint32]chan struct{}, map[uint32]*atomic.Int64) {
		return make(map[uint32]chan *UDPFrame), make(map[uint32]chan struct{}), make(map[uint32]*atomic.Int64)
	}

	sessions, sessionDone, lastActive := newMuxMaps()
	dgram := &quicUDPMux{
		sessions: sessions, sessionDone: sessionDone, lastActive: lastActive,
		done: make(chan struct{}),
	}
	dgram.udpTxDrops.Store(2)
	dgram.udpRxDrops.Store(4)
	dgram.udpRxDropsNoSession.Store(1)

	c := &QUICClient{}
	c.udpMux = dgram

	// Datagram path only: totals are the datagram mux's.
	s := c.WireStats()
	if s.Transport != "quic" {
		t.Fatalf("Transport = %q, want quic", s.Transport)
	}
	if s.UDPRxDrops != 4 || s.UDPRxDropsNoSession != 1 || s.UDPTxDrops != 2 {
		t.Fatalf("datagram-only stats = tx %d / rx %d / noSess %d, want 2 / 4 / 1",
			s.UDPTxDrops, s.UDPRxDrops, s.UDPRxDropsNoSession)
	}

	// A datagram-less server pushed sessions onto the stream mux, and it has
	// been dropping. Those drops must show up in the connection's totals.
	sessions, sessionDone, lastActive = newMuxMaps()
	stream := &quicStreamUDPMux{
		sessions: sessions, sessionDone: sessionDone, lastActive: lastActive,
		done: make(chan struct{}),
	}
	stream.udpRxDrops.Store(5)
	stream.udpRxDropsNoSession.Store(3)
	dgram.streamMux = stream

	s = c.WireStats()
	if s.UDPRxDrops != 9 {
		t.Errorf("UDPRxDrops = %d, want 9 (4 datagram + 5 stream) — stream-mux drops are not folded in", s.UDPRxDrops)
	}
	if s.UDPRxDropsNoSession != 4 {
		t.Errorf("UDPRxDropsNoSession = %d, want 4 (1 datagram + 3 stream)", s.UDPRxDropsNoSession)
	}
	// Unchanged: the stream mux has no lossy send path, so it contributes no
	// TX drops. See the per-transport table on WireStats.
	if s.UDPTxDrops != 2 {
		t.Errorf("UDPTxDrops = %d, want 2 (stream mux contributes none)", s.UDPTxDrops)
	}
}

// A QUICClient with no UDP mux at all (no UDP session ever opened) must report
// zeros rather than panic — the nil guards in WireStats are load-bearing on
// every heartbeat taken before the first UDP session.
func TestQUICClient_WireStats_NoMux(t *testing.T) {
	c := &QUICClient{}
	s := c.WireStats()
	if s.Transport != "quic" || s.UDPRxDrops != 0 || s.UDPRxDropsNoSession != 0 || s.UDPTxDrops != 0 {
		t.Fatalf("WireStats with no mux = %+v, want quic with zero drops", s)
	}
}
