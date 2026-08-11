package wire

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"
	smux "github.com/xtaci/smux/v2"
)

// Enforce coverage matrix — one test per client-reachable egress entry.
//
// Every enforce bypass so far has been an *entry* nobody had a test for, not a
// gate that was tested and wrong. So this file enumerates the entries rather
// than the code paths: for each way a client can ask a node to move bytes, a
// connection that never sent metadata must move none of them under enforce.
//
//	QUIC     datagram              → quic_enforce_datagram_test.go
//	QUIC     stream TCP  (0x01)    → quic_enforce_integration_test.go
//	QUIC     stream UDP  (0x02)    → here
//	QUIC     overflow    (0x03)    → here
//	TCP-WS   stream TCP  (0x01)    → tcpws_enforce_test.go
//	TCP-WS   stream UDP  (0x02)    → here
//
// The structural twin of this list is TestAdmissionGate_EgressSinksAreGated,
// which fails if a *new* entry reaches egress without the admission gate.

// openRawUDPStream opens a proxy stream of the given type on a metadata-less
// QUIC connection and pushes one framed UDP payload at it — the attacker model:
// a peer that completes the QUIC handshake and then simply never authenticates.
func openRawUDPStream(t *testing.T, conn *quic.Conn, streamType byte) *quic.Stream {
	t.Helper()
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := WriteH3ProxyFrameType(stream); err != nil {
		t.Fatalf("write frame type: %v", err)
	}
	if err := WriteStreamHeader(stream, streamType, ""); err != nil {
		t.Fatalf("write stream header: %v", err)
	}
	if err := WriteSmuxUDPFrame(stream, 1, "8.8.8.8:53", []byte("ping")); err != nil {
		t.Fatalf("write UDP frame: %v", err)
	}
	return stream
}

// TestIntegration_QUICEnforce_UnauthenticatedStreamUDPRejected covers the
// stream-based UDP fallback (StreamType 0x02) — the path DialUDP uses when the
// peer has no datagram support, and one that the datagram gate does not cover.
func TestIntegration_QUICEnforce_UnauthenticatedStreamUDPRejected(t *testing.T) {
	serverAddr, cleanup := startDatagramEchoServer(t,
		func(token, udid, mode string) AuthVerdict {
			if udid == "good-device" && token == "valid-token" {
				return AuthVerdict{OK: true}
			}
			return AuthVerdict{Code: 401}
		},
		true, // enforce
	)
	defer cleanup()

	conn, connCleanup := dialRawQUIC(t, serverAddr)
	defer connCleanup()

	stream := openRawUDPStream(t, conn, StreamTypeUDP)
	defer stream.Close()

	// The echo, if the server wrongly proxied, comes back on the same stream.
	stream.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	if frame, err := ReadSmuxUDPFrame(bufio.NewReader(stream)); err == nil {
		t.Fatalf("SECURITY REGRESSION: unauthenticated stream-UDP proxied through an enforce=on node (got %q)", frame.Payload)
	}
}

// TestIntegration_QUICEnforce_UnauthenticatedOverflowRejected covers the client
// overflow stream (StreamType 0x03), which shares the datagram session map. Its
// replies come back as datagrams, not on the stream.
func TestIntegration_QUICEnforce_UnauthenticatedOverflowRejected(t *testing.T) {
	serverAddr, cleanup := startDatagramEchoServer(t,
		func(token, udid, mode string) AuthVerdict {
			if udid == "good-device" && token == "valid-token" {
				return AuthVerdict{OK: true}
			}
			return AuthVerdict{Code: 401}
		},
		true, // enforce
	)
	defer cleanup()

	conn, connCleanup := dialRawQUIC(t, serverAddr)
	defer connCleanup()

	stream := openRawUDPStream(t, conn, StreamTypeUDPOverflow)
	defer stream.Close()

	rctx, rcancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer rcancel()
	data, err := conn.ReceiveDatagram(rctx)
	if err != nil {
		return // dropped, as required
	}
	if frame, err := ReadUDPFrame(bytes.NewReader(data)); err == nil {
		t.Fatalf("SECURITY REGRESSION: unauthenticated overflow-stream UDP proxied through an enforce=on node (got %q)", frame.Payload)
	}
}

// TestIntegration_QUICEnforce_PermissiveServesStreamUDP is the rollout guard for
// the same entry: with enforce off, an unauthenticated stream-UDP session is
// still served, so flipping the flag remains the only thing that cuts off
// legacy clients.
func TestIntegration_QUICEnforce_PermissiveServesStreamUDP(t *testing.T) {
	serverAddr, cleanup := startDatagramEchoServer(t,
		func(token, udid, mode string) AuthVerdict { return AuthVerdict{Code: 401} }, // reject everyone
		false, // permissive
	)
	defer cleanup()

	conn, connCleanup := dialRawQUIC(t, serverAddr)
	defer connCleanup()

	stream := openRawUDPStream(t, conn, StreamTypeUDP)
	defer stream.Close()

	stream.SetReadDeadline(time.Now().Add(2 * time.Second))
	frame, err := ReadSmuxUDPFrame(bufio.NewReader(stream))
	if err != nil {
		t.Fatalf("permissive mode must serve unauthenticated stream-UDP (rollout safety): %v", err)
	}
	if string(frame.Payload) != "ping" {
		t.Fatalf("echo mismatch: got %q", frame.Payload)
	}
}

// TestIntegration_QUICEnforce_NilValidatorAuthenticatesNobody: a nil validator
// means no auth backend is configured. In permissive mode that authenticates
// everyone (the verdict only feeds attribution), but under enforce it must
// authenticate nobody — otherwise a misconfigured node reports enforcement it
// does not perform, which is worse than no gate at all.
func TestIntegration_QUICEnforce_NilValidatorAuthenticatesNobody(t *testing.T) {
	serverAddr, cleanup := startDatagramEchoServer(t, nil, true) // no validator, enforce
	defer cleanup()

	client := NewQUICClient(Config{
		Host:  "127.0.0.1",
		Port:  serverAddr.Port,
		UDID:  "some-device",
		Token: "some-token",
	}, nil)
	client.tlsConfig = buildInsecureTLSConfig()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pconn, err := client.DialUDP(ctx, "8.8.8.8:53")
	if err != nil {
		return // rejected outright, as required
	}
	defer pconn.Close()

	if _, err := pconn.WriteTo([]byte("ping"), nil); err != nil {
		return
	}
	buf := make([]byte, 4096)
	pconn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	if _, _, err := pconn.ReadFrom(buf); err == nil {
		t.Fatal("SECURITY REGRESSION: enforce=on with no validator configured proxied traffic — the node claims enforcement it does not perform")
	}
}

// startTCPWSUDPEchoServer wires a TCP-WS server with a UDP echo handler over
// net.Pipe and hands back the raw client end, so a test can drive smux directly
// without TCPWSClient's metadata stream.
func startTCPWSUDPEchoServer(t *testing.T, validator Validator, enforce bool) (net.Conn, func()) {
	t.Helper()
	clientConn, serverConn := net.Pipe()

	server := NewTCPWSServer()
	server.SetValidator(validator)
	server.SetEnforce(enforce)
	server.adm.authWait = 300 * time.Millisecond
	server.HandleConn(serverConn)

	ctx, cancel := context.WithCancel(context.Background())
	go server.ServeUDP(ctx, UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		buf := make([]byte, 4096)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if _, err := conn.WriteTo(buf[:n], nil); err != nil {
				return
			}
		}
	}))

	return clientConn, func() {
		cancel()
		server.Close()
		clientConn.Close()
		serverConn.Close()
	}
}

// rawSmuxUDPEchoes drives a metadata-less smux session: open a UDP proxy stream,
// send one framed payload, and report whether the server echoed it back.
func rawSmuxUDPEchoes(t *testing.T, clientConn net.Conn) bool {
	t.Helper()
	sess, err := smux.Client(clientConn, DefaultSmuxConfig())
	if err != nil {
		t.Fatalf("smux client: %v", err)
	}
	defer sess.Close()

	stream, err := sess.OpenStream()
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.Close()

	if err := WriteStreamHeader(stream, StreamTypeUDP, ""); err != nil {
		t.Fatalf("write stream header: %v", err)
	}
	if err := WriteSmuxUDPFrame(stream, 1, "8.8.8.8:53", []byte("ping")); err != nil {
		t.Fatalf("write UDP frame: %v", err)
	}

	stream.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	frame, err := ReadSmuxUDPFrame(bufio.NewReader(stream))
	if err != nil {
		return false
	}
	return string(frame.Payload) == "ping"
}

// TestTCPWSEnforce_UnauthenticatedStreamUDPRejected covers TCP-WS UDP egress.
// TCP-WS is the transport that matters most on censored networks (UDP blocked),
// so an ungated UDP entry here is not a corner case — it is the main road.
func TestTCPWSEnforce_UnauthenticatedStreamUDPRejected(t *testing.T) {
	clientConn, cleanup := startTCPWSUDPEchoServer(t,
		func(token, udid, mode string) AuthVerdict {
			if udid == "good" && token == "valid" {
				return AuthVerdict{OK: true}
			}
			return AuthVerdict{Code: 401}
		},
		true, // enforce
	)
	defer cleanup()

	if rawSmuxUDPEchoes(t, clientConn) {
		t.Fatal("SECURITY REGRESSION: unauthenticated TCP-WS UDP proxied through an enforce=on node")
	}
}

// TestTCPWSEnforce_PermissiveServesStreamUDP is the matching rollout guard.
func TestTCPWSEnforce_PermissiveServesStreamUDP(t *testing.T) {
	clientConn, cleanup := startTCPWSUDPEchoServer(t,
		func(token, udid, mode string) AuthVerdict { return AuthVerdict{Code: 401} }, // reject everyone
		false, // permissive
	)
	defer cleanup()

	if !rawSmuxUDPEchoes(t, clientConn) {
		t.Fatal("permissive mode must serve unauthenticated TCP-WS UDP (rollout safety)")
	}
}
