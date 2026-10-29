package wire

import (
	"bytes"
	"context"
	tls "github.com/metacubex/utls"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"
)

// Datagram enforce coverage.
//
// The enforce gate originally hung only on handleStream's TCP/UDP-stream cases,
// leaving the QUIC *datagram* path — the PRIMARY UDP path (DialUDP prefers
// datagrams; stream-UDP is only the fallback) — and the client overflow stream
// completely ungated. Under enforce a client that simply never opens a metadata
// stream (so the connection is never torn by CloseWithError) could proxy UDP by
// sending raw datagrams. These tests pin the datagram gate at deliverToSession,
// the single choke point both the datagram and overflow paths funnel through.

// startDatagramEchoServer builds an enforce-configurable QUIC server whose UDP
// handler echoes each payload back. Only sessions the datagram gate admits ever
// reach the handler.
func startDatagramEchoServer(t *testing.T, validator Validator, enforce bool) (*net.UDPAddr, func()) {
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

	addr := serverUDP.LocalAddr().(*net.UDPAddr)
	cleanup := func() {
		cancel()
		server.Close()
		ln.Close()
		serverTransport.Close()
		serverUDP.Close()
	}
	return addr, cleanup
}

// dialRawQUIC establishes a bare QUIC connection (handshake only) WITHOUT
// sending a metadata stream — the attacker model the datagram gate must stop.
func dialRawQUIC(t *testing.T, serverAddr *net.UDPAddr) (*quic.Conn, func()) {
	t.Helper()
	clientUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	tr := &quic.Transport{Conn: clientUDP}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := tr.Dial(ctx, serverAddr, &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}, DefaultQUICConfig())
	if err != nil {
		tr.Close()
		clientUDP.Close()
		t.Fatalf("raw QUIC dial: %v", err)
	}
	return conn, func() {
		conn.CloseWithError(0, "")
		tr.Close()
		clientUDP.Close()
	}
}

// rawDatagramEchoes sends one framed UDP datagram on a metadata-less connection
// and reports whether the server echoed it back within the deadline.
func rawDatagramEchoes(t *testing.T, conn *quic.Conn) bool {
	t.Helper()
	frame, put := MarshalUDPFrame(1, "8.8.8.8:53", []byte("ping"))
	err := conn.SendDatagram(frame)
	put()
	if err != nil {
		t.Fatalf("SendDatagram: %v", err)
	}

	// > authWait (300ms) so a gated-then-rejected session has resolved.
	rctx, rcancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer rcancel()
	data, err := conn.ReceiveDatagram(rctx)
	if err != nil {
		return false // no echo — dropped
	}
	f, err := ReadUDPFrame(bytes.NewReader(data))
	if err != nil {
		return false
	}
	return string(f.Payload) == "ping"
}

// TestIntegration_QUICEnforce_UnauthenticatedDatagramRejected is the datagram
// regression: under enforce, a connection that never authenticated must NOT be
// able to proxy UDP via raw datagrams.
func TestIntegration_QUICEnforce_UnauthenticatedDatagramRejected(t *testing.T) {
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

	if rawDatagramEchoes(t, conn) {
		t.Fatal("SECURITY REGRESSION: unauthenticated UDP datagram proxied through an enforce=on node")
	}
}

// TestIntegration_QUICEnforce_ValidDatagramAdmitted proves the gate doesn't
// break legitimate UDP: a client that sends valid metadata (normal QUICClient)
// can proxy UDP under enforce.
func TestIntegration_QUICEnforce_ValidDatagramAdmitted(t *testing.T) {
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

	client := NewQUICClient(Config{
		Host:  "127.0.0.1",
		Port:  serverAddr.Port,
		UDID:  "good-device",
		Token: "valid-token",
	}, nil)
	client.tlsConfig = buildInsecureTLSConfig()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pconn, err := client.DialUDP(ctx, "8.8.8.8:53")
	if err != nil {
		t.Fatalf("valid client DialUDP under enforce: %v", err)
	}
	defer pconn.Close()

	if _, err := pconn.WriteTo([]byte("ping"), nil); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	buf := make([]byte, 4096)
	pconn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := pconn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("valid client UDP wrongly dropped under enforce: %v", err)
	}
	if string(buf[:n]) != "ping" {
		t.Fatalf("echo mismatch: got %q", buf[:n])
	}
}

// TestIntegration_QUICEnforce_PermissiveServesDatagram proves the rollout
// guarantee for UDP: with enforce=off, an unauthenticated datagram is still
// served, so flipping the flag is the only thing that cuts off legacy clients.
func TestIntegration_QUICEnforce_PermissiveServesDatagram(t *testing.T) {
	serverAddr, cleanup := startDatagramEchoServer(t,
		func(token, udid, mode string) AuthVerdict { return AuthVerdict{Code: 401} }, // reject everyone
		false, // permissive
	)
	defer cleanup()

	conn, connCleanup := dialRawQUIC(t, serverAddr)
	defer connCleanup()

	if !rawDatagramEchoes(t, conn) {
		t.Fatal("permissive mode must serve unauthenticated UDP datagrams (rollout safety)")
	}
}
