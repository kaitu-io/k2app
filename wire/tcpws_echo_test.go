package wire

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"testing"

	smux "github.com/xtaci/smux/v2"
)

func TestHandleEcho_ReflectsNonce(t *testing.T) {
	nonce := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	var out bytes.Buffer
	br := bufio.NewReader(bytes.NewReader(nonce))
	handleEcho(&out, br)
	if !bytes.Equal(out.Bytes(), nonce) {
		t.Fatalf("echo reflected %v, want %v", out.Bytes(), nonce)
	}
}

// newTCPWSEchoPair wires a TCPWSClient to a real TCPWSServer via net.Pipe.
// The server side runs the actual HandleConn -> handleSession -> handleStream
// dispatch (same as setupTCPWSPair in tcpws_test.go), so a probe here drives
// the real StreamTypeEcho case added in Task 5, not a synthetic echo loop.
func newTCPWSEchoPair(t *testing.T) (*TCPWSClient, func()) {
	t.Helper()
	clientConn, serverConn := net.Pipe()

	server := NewTCPWSServer()
	server.HandleConn(serverConn)

	client := NewTCPWSClient(Config{Host: "127.0.0.1", Port: 443}, nil)
	client.testConn = clientConn

	cleanup := func() {
		client.Close()
		server.Close()
		clientConn.Close()
		serverConn.Close()
	}
	return client, cleanup
}

// newTCPWSOldServerPair simulates a pre-Task-5 server: it accepts smux
// streams directly (bypassing TCPWSServer.handleStream) and closes each one
// without reflecting, mirroring the `default:` case that StreamTypeEcho fell
// into before Task 5 added a dedicated case.
func newTCPWSOldServerPair(t *testing.T) (*TCPWSClient, func()) {
	t.Helper()
	clientConn, serverConn := net.Pipe()

	client := NewTCPWSClient(Config{Host: "127.0.0.1", Port: 443}, nil)
	client.testConn = clientConn

	sess, err := smux.Server(serverConn, DefaultSmuxConfig())
	if err != nil {
		t.Fatalf("smux.Server: %v", err)
	}
	go func() {
		for {
			stream, err := sess.AcceptStream()
			if err != nil {
				return
			}
			go func() {
				br := bufio.NewReader(stream)
				_, _, _ = ReadStreamHeader(br) // drain header, old server ignores StreamTypeEcho
				stream.Close()
			}()
		}
	}()

	cleanup := func() {
		client.Close()
		sess.Close()
		clientConn.Close()
		serverConn.Close()
	}
	return client, cleanup
}

func TestTCPWSEcho_EndToEnd(t *testing.T) {
	client, cleanup := newTCPWSEchoPair(t)
	defer cleanup()
	if err := client.RunEchoProbe(context.Background()); err != nil {
		t.Fatalf("echo probe failed: %v", err)
	}
}

func TestTCPWSEcho_OldServerUnsupported(t *testing.T) {
	client, cleanup := newTCPWSOldServerPair(t)
	defer cleanup()
	err := client.RunEchoProbe(context.Background())
	if err == nil || !IsEchoUnsupported(err) {
		t.Fatalf("old server should yield IsEchoUnsupported, got %v", err)
	}
}
