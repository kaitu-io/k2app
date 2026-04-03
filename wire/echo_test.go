package wire

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/apernet/quic-go"
)

// echoTestServer sets up a raw QUIC listener and returns the listener and cleanup func.
func echoTestServer(t *testing.T) (*quic.Listener, *net.UDPAddr, func()) {
	t.Helper()
	cert := generateTestCert(t)

	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}

	serverTransport := &quic.Transport{Conn: serverUDP}
	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}
	ln, err := serverTransport.Listen(serverTLS, DefaultQUICConfig())
	if err != nil {
		serverUDP.Close()
		t.Fatal(err)
	}

	addr := serverUDP.LocalAddr().(*net.UDPAddr)
	cleanup := func() {
		ln.Close()
		serverTransport.Close()
		serverUDP.Close()
	}
	return ln, addr, cleanup
}

// echoTestClient dials a raw QUIC connection to the given address.
func echoTestClient(t *testing.T, addr *net.UDPAddr) (*quic.Conn, func()) {
	t.Helper()

	clientUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}

	clientTransport := &quic.Transport{Conn: clientUDP}
	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}
	conn, err := clientTransport.Dial(context.Background(), addr, clientTLS, DefaultQUICConfig())
	if err != nil {
		clientTransport.Close()
		clientUDP.Close()
		t.Fatal(err)
	}

	cleanup := func() {
		conn.CloseWithError(0, "")
		clientTransport.Close()
		clientUDP.Close()
	}
	return conn, cleanup
}

func TestEchoProbe_Success(t *testing.T) {
	ln, addr, cleanupServer := echoTestServer(t)
	defer cleanupServer()

	// Server goroutine: accept connection, accept stream, read header, echo nonce.
	serverDone := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			serverDone <- err
			return
		}
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			serverDone <- err
			return
		}

		br := bufio.NewReader(stream)

		// Read H3 frame type marker (same as real server handleStream)
		frameType, _, err := ReadVarint(br)
		if err != nil {
			serverDone <- err
			return
		}
		if frameType != H3FrameTypeK2Proxy {
			serverDone <- net.UnknownNetworkError("unexpected frame type")
			return
		}

		// Read stream header
		streamType, _, err := ReadStreamHeader(br)
		if err != nil {
			serverDone <- err
			return
		}
		if streamType != StreamTypeEcho {
			serverDone <- net.UnknownNetworkError("expected echo stream type")
			return
		}

		// Now call HandleEchoStream — but HandleEchoStream reads from the
		// raw stream, not the buffered reader. We need to handle manually
		// since br has buffered data.
		// Read nonce from buffered reader and write back.
		var buf [echoNonceLen]byte
		if _, err := br.Read(buf[:]); err != nil {
			serverDone <- err
			return
		}
		if _, err := stream.Write(buf[:]); err != nil {
			serverDone <- err
			return
		}
		stream.CancelRead(0)
		stream.Close()
		serverDone <- nil
	}()

	conn, cleanupClient := echoTestClient(t, addr)
	defer cleanupClient()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rtt, err := EchoProbe(ctx, conn)
	if err != nil {
		t.Fatalf("EchoProbe failed: %v", err)
	}
	if rtt <= 0 {
		t.Errorf("expected positive RTT, got %v", rtt)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestEchoProbe_Timeout(t *testing.T) {
	ln, addr, cleanupServer := echoTestServer(t)
	defer cleanupServer()

	// Server goroutine: accept stream but never respond.
	done := make(chan struct{})
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			return
		}
		// Read header to unblock client writes, but never send response.
		br := bufio.NewReader(stream)
		ReadVarint(br)
		ReadStreamHeader(br)
		// Read nonce but don't respond.
		var buf [echoNonceLen]byte
		br.Read(buf[:])
		// Block until test signals completion.
		<-done
		stream.Close()
	}()

	conn, cleanupClient := echoTestClient(t, addr)
	defer cleanupClient()
	defer close(done)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := EchoProbe(ctx, conn)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") &&
		!strings.Contains(err.Error(), "echo: read response") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEchoProbe_NonceMismatch(t *testing.T) {
	ln, addr, cleanupServer := echoTestServer(t)
	defer cleanupServer()

	// Server goroutine: accept stream, read nonce, respond with all zeros.
	serverDone := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			serverDone <- err
			return
		}
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			serverDone <- err
			return
		}

		br := bufio.NewReader(stream)
		ReadVarint(br)
		ReadStreamHeader(br)

		// Read nonce
		var buf [echoNonceLen]byte
		if _, err := br.Read(buf[:]); err != nil {
			serverDone <- err
			return
		}

		// Respond with all zeros (wrong nonce).
		var zeros [echoNonceLen]byte
		if _, err := stream.Write(zeros[:]); err != nil {
			serverDone <- err
			return
		}
		stream.CancelRead(0)
		stream.Close()
		serverDone <- nil
	}()

	conn, cleanupClient := echoTestClient(t, addr)
	defer cleanupClient()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := EchoProbe(ctx, conn)
	if err == nil {
		t.Fatal("expected nonce mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "nonce mismatch") {
		t.Errorf("expected 'nonce mismatch' error, got: %v", err)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}
