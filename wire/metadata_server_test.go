package wire

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/apernet/quic-go"
	smux "github.com/xtaci/smux/v2"
)

// startRawTCPWSServer wires a TCPWSServer over net.Pipe and hands back a raw
// smux client session, so tests drive the metadata stream by hand.
func startRawTCPWSServer(t *testing.T, validator Validator, enforce bool) (*smux.Session, *TCPWSServer, func()) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	server := NewTCPWSServer()
	server.SetValidator(validator)
	server.SetEnforce(enforce)
	server.adm.authWait = 300 * time.Millisecond
	server.HandleConn(serverConn)

	sess, err := smux.Client(clientConn, DefaultSmuxConfig())
	if err != nil {
		t.Fatalf("smux client: %v", err)
	}
	return sess, server, func() {
		sess.Close()
		server.Close()
		clientConn.Close()
		serverConn.Close()
	}
}

// sendRawMetadataSmux writes a metadata frame by hand and returns a reader on
// the server's response.
func sendRawMetadataSmux(t *testing.T, sess *smux.Session, meta MetadataFrame) (*bufio.Reader, *smux.Stream) {
	t.Helper()
	stream, err := sess.OpenStream()
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := WriteStreamHeader(stream, StreamTypeMeta, ""); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if err := WriteMetadataFrame(stream, meta); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	stream.SetReadDeadline(time.Now().Add(2 * time.Second))
	return bufio.NewReader(stream), stream
}

// TestTCPWSMetadata_EnforceExplicitRejectIsCoded: Center's 402 must reach the
// client as a 0x02 coded response, not be swallowed into a generic
// "unauthorized" (spec §5.2: the client turns it into *StreamError{402}).
func TestTCPWSMetadata_EnforceExplicitRejectIsCoded(t *testing.T) {
	sess, _, cleanup := startRawTCPWSServer(t, vReject(402), true)
	defer cleanup()

	br, stream := sendRawMetadataSmux(t, sess, MetadataFrame{UDID: "d", Token: "t"})
	defer stream.Close()
	ok, code, _, err := ReadStreamResponse(br)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if ok || code != 402 {
		t.Fatalf("got ok=%v code=%d, want coded rejection 402", ok, code)
	}
}

// TestTCPWSMetadata_EnforceUndeterminedRejectsLegacy: "cannot determine" under
// enforce rejects (same as today) with the legacy 0x01 form — it must NOT
// invent a code the server never gave (402/403). This uncoded rejection still
// wraps to ErrStreamRejected on the client and the engine keeps its existing
// 401 semantics for it (ClassifyError -> 401); the point of the legacy form is
// only that a Center outage is never mislabeled as 402/403.
func TestTCPWSMetadata_EnforceUndeterminedRejectsLegacy(t *testing.T) {
	sess, _, cleanup := startRawTCPWSServer(t, vUnreachable(), true)
	defer cleanup()

	br, stream := sendRawMetadataSmux(t, sess, MetadataFrame{UDID: "d", Token: "t"})
	defer stream.Close()
	ok, code, _, err := ReadStreamResponse(br)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if ok || code != 0 {
		t.Fatalf("got ok=%v code=%d, want legacy (0x01, code 0) rejection", ok, code)
	}
}

// TestTCPWSMetadata_PermissiveAlwaysOK: during rollout the server must answer
// OK whatever the verdict — no client may fail connect() on credential
// problems before enforce is flipped (spec §5.2).
func TestTCPWSMetadata_PermissiveAlwaysOK(t *testing.T) {
	for name, v := range map[string]Validator{"rejected": vReject(401), "unreachable": vUnreachable()} {
		t.Run(name, func(t *testing.T) {
			sess, _, cleanup := startRawTCPWSServer(t, v, false)
			defer cleanup()
			br, stream := sendRawMetadataSmux(t, sess, MetadataFrame{UDID: "d", Token: "t"})
			defer stream.Close()
			ok, _, _, err := ReadStreamResponse(br)
			if err != nil || !ok {
				t.Fatalf("permissive must answer OK: ok=%v err=%v", ok, err)
			}
			if cookie, err := ReadMetadataResponse(br); err != nil || cookie != "" {
				t.Fatalf("kv after OK: cookie=%q err=%v", cookie, err)
			}
		})
	}
}

// TestTCPWSMetadata_AttributionRequiresVerdict is the udid 收口 gate: a
// connection whose metadata FAILED validation must land in the empty-udid
// bucket, never impersonate the self-reported device (spec §5.3).
func TestTCPWSMetadata_AttributionRequiresVerdict(t *testing.T) {
	sess, server, cleanup := startRawTCPWSServer(t, vReject(401), false) // permissive: still served
	defer cleanup()

	br, stream := sendRawMetadataSmux(t, sess, MetadataFrame{UDID: "spoofed-victim", Token: "junk"})
	defer stream.Close()
	if ok, _, _, err := ReadStreamResponse(br); err != nil || !ok {
		t.Fatalf("permissive must serve: ok=%v err=%v", ok, err)
	}

	// Open a TCP proxy stream and inspect the device holder AcceptTCP hands out.
	proxy, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	if err := WriteStreamHeader(proxy, StreamTypeTCP, "example.com:80"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := server.AcceptTCP(ctx)
	if err != nil {
		t.Fatalf("AcceptTCP: %v", err)
	}
	defer conn.Close()
	sc, ok := conn.(*smuxStreamConn)
	if !ok {
		t.Fatalf("AcceptTCP returned %T, want *smuxStreamConn", conn)
	}
	if got := sc.dev.get(); got != "" {
		t.Fatalf("SPOOFING REGRESSION: failed-auth connection attributed to %q, want empty-udid bucket", got)
	}
}

// TestTCPWSMetadata_AttributionSetOnOK: the positive twin — a validated
// connection is attributed to its udid (accounting must keep working).
func TestTCPWSMetadata_AttributionSetOnOK(t *testing.T) {
	sess, server, cleanup := startRawTCPWSServer(t, vOK(), false)
	defer cleanup()

	br, stream := sendRawMetadataSmux(t, sess, MetadataFrame{UDID: "real-device", Token: "t"})
	defer stream.Close()
	if ok, _, _, err := ReadStreamResponse(br); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}

	proxy, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	if err := WriteStreamHeader(proxy, StreamTypeTCP, "example.com:80"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := server.AcceptTCP(ctx)
	if err != nil {
		t.Fatalf("AcceptTCP: %v", err)
	}
	defer conn.Close()
	if got := conn.(*smuxStreamConn).dev.get(); got != "real-device" {
		t.Fatalf("validated connection attribution = %q, want real-device", got)
	}
}

// startRawQUICServer boots a real QUIC listener with the given validator and
// returns the server plus dial address, so tests can inspect AcceptTCP output.
func startRawQUICServer(t *testing.T, validator Validator, enforce bool) (*QUICServer, *net.UDPAddr, func()) {
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
	server := NewQUICServer()
	server.SetValidator(validator)
	server.SetEnforce(enforce)
	server.adm.authWait = 300 * time.Millisecond
	server.Listen(ln)
	addr := serverUDP.LocalAddr().(*net.UDPAddr)
	return server, addr, func() {
		server.Close()
		ln.Close()
		serverTransport.Close()
		serverUDP.Close()
	}
}

// sendRawMetadataQUIC opens a metadata stream on a raw QUIC conn by hand.
func sendRawMetadataQUIC(t *testing.T, conn *quic.Conn, meta MetadataFrame) (*bufio.Reader, *quic.Stream) {
	t.Helper()
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteH3ProxyFrameType(stream); err != nil {
		t.Fatal(err)
	}
	if err := WriteStreamHeader(stream, StreamTypeMeta, ""); err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadataFrame(stream, meta); err != nil {
		t.Fatal(err)
	}
	stream.SetReadDeadline(time.Now().Add(2 * time.Second))
	return bufio.NewReader(stream), stream
}

// TestQUICMetadata_EnforceExplicitRejectIsCoded — QUIC twin of the TCP-WS test.
func TestQUICMetadata_EnforceExplicitRejectIsCoded(t *testing.T) {
	_, addr, cleanup := startRawQUICServer(t, vReject(403), true)
	defer cleanup()
	conn, connCleanup := dialRawQUIC(t, addr)
	defer connCleanup()

	br, stream := sendRawMetadataQUIC(t, conn, MetadataFrame{UDID: "d", Token: "t"})
	defer stream.Close()
	ok, code, _, err := ReadStreamResponse(br)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if ok || code != 403 {
		t.Fatalf("got ok=%v code=%d, want coded rejection 403", ok, code)
	}
}

// TestQUICMetadata_AttributionRequiresVerdict — QUIC twin of the udid 收口 gate.
func TestQUICMetadata_AttributionRequiresVerdict(t *testing.T) {
	server, addr, cleanup := startRawQUICServer(t, vReject(401), false)
	defer cleanup()
	conn, connCleanup := dialRawQUIC(t, addr)
	defer connCleanup()

	br, stream := sendRawMetadataQUIC(t, conn, MetadataFrame{UDID: "spoofed-victim", Token: "junk"})
	defer stream.Close()
	if ok, _, _, err := ReadStreamResponse(br); err != nil || !ok {
		t.Fatalf("permissive must serve: ok=%v err=%v", ok, err)
	}

	proxy, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	if err := WriteH3ProxyFrameType(proxy); err != nil {
		t.Fatal(err)
	}
	if err := WriteStreamHeader(proxy, StreamTypeTCP, "example.com:80"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	acc, _, err := server.AcceptTCP(ctx)
	if err != nil {
		t.Fatalf("AcceptTCP: %v", err)
	}
	defer acc.Close()
	if got := acc.(*quicStreamConn).dev.get(); got != "" {
		t.Fatalf("SPOOFING REGRESSION: failed-auth QUIC connection attributed to %q", got)
	}
}

// TestQUICMetadata_AttributionSetOnOK: the positive twin — a validated QUIC
// connection is attributed to its udid (accounting must keep working). Mirrors
// TestTCPWSMetadata_AttributionSetOnOK.
func TestQUICMetadata_AttributionSetOnOK(t *testing.T) {
	server, addr, cleanup := startRawQUICServer(t, vOK(), false)
	defer cleanup()
	conn, connCleanup := dialRawQUIC(t, addr)
	defer connCleanup()

	br, stream := sendRawMetadataQUIC(t, conn, MetadataFrame{UDID: "real-device", Token: "t"})
	defer stream.Close()
	if ok, _, _, err := ReadStreamResponse(br); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}

	proxy, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	if err := WriteH3ProxyFrameType(proxy); err != nil {
		t.Fatal(err)
	}
	if err := WriteStreamHeader(proxy, StreamTypeTCP, "example.com:80"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	acc, _, err := server.AcceptTCP(ctx)
	if err != nil {
		t.Fatalf("AcceptTCP: %v", err)
	}
	defer acc.Close()
	if got := acc.(*quicStreamConn).dev.get(); got != "real-device" {
		t.Fatalf("validated QUIC connection attribution = %q, want real-device", got)
	}
}

// TestMetadataResponse_OldClientSafe_QUIC: an OLD client reads exactly one
// status byte after a NEW server's [0x00][kv] response, then Close()s. The
// unread kv must be silently discarded — no error, and the connection must
// remain fully usable (spec §5.1: verified for both transports).
func TestMetadataResponse_OldClientSafe_QUIC(t *testing.T) {
	server, addr, cleanup := startRawQUICServer(t, vOK(), false)
	defer cleanup()
	conn, connCleanup := dialRawQUIC(t, addr)
	defer connCleanup()

	br, stream := sendRawMetadataQUIC(t, conn, MetadataFrame{UDID: "d", Token: "t"})
	// Old-client behavior: ReadStreamResponse (one status byte), then Close.
	ok, _, _, err := ReadStreamResponse(br)
	if err != nil || !ok {
		t.Fatalf("old client must read a clean OK: ok=%v err=%v", ok, err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("old-client Close after partial read errored: %v", err)
	}

	// The connection must still work: open a proxy stream and see it accepted.
	proxy, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatalf("connection unusable after discarding kv: %v", err)
	}
	defer proxy.Close()
	if err := WriteH3ProxyFrameType(proxy); err != nil {
		t.Fatal(err)
	}
	if err := WriteStreamHeader(proxy, StreamTypeTCP, "example.com:80"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	acc, _, err := server.AcceptTCP(ctx)
	if err != nil {
		t.Fatalf("proxy stream not accepted after old-client metadata exchange: %v", err)
	}
	acc.Close()
}

// TestMetadataResponse_OldClientSafe_TCPWS — the smux twin.
func TestMetadataResponse_OldClientSafe_TCPWS(t *testing.T) {
	sess, server, cleanup := startRawTCPWSServer(t, vOK(), false)
	defer cleanup()

	br, stream := sendRawMetadataSmux(t, sess, MetadataFrame{UDID: "d", Token: "t"})
	ok, _, _, err := ReadStreamResponse(br)
	if err != nil || !ok {
		t.Fatalf("old client must read a clean OK: ok=%v err=%v", ok, err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("old-client Close after partial read errored: %v", err)
	}

	proxy, err := sess.OpenStream()
	if err != nil {
		t.Fatalf("session unusable after discarding kv: %v", err)
	}
	defer proxy.Close()
	if err := WriteStreamHeader(proxy, StreamTypeTCP, "example.com:80"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	acc, _, err := server.AcceptTCP(ctx)
	if err != nil {
		t.Fatalf("proxy stream not accepted after old-client metadata exchange: %v", err)
	}
	acc.Close()
}
