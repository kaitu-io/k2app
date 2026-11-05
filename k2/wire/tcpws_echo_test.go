package wire

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

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

// newTCPWSBlackholePair simulates a TCP-WS session whose data path has died:
// the server accepts every smux stream and reads its header but never writes
// the echo reply (node egress gone, or the path dropping server→client data
// while the session still exists). The stream stays open, so the client's
// read can only end at its deadline.
func newTCPWSBlackholePair(t *testing.T) (*TCPWSClient, func()) {
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
				st, _, err := ReadStreamHeader(br)
				if err != nil || st != StreamTypeEcho {
					stream.Close() // metadata etc.: answer like a legacy server, so connect is fast
					return
				}
				_, _ = io.Copy(io.Discard, br) // echo: swallow the nonce, never reply
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

// TestTCPWSEcho_RealSmuxReadTimeoutIsFailure pins the field bug of 2026-11-05:
// smux answers an expired read deadline with smux.ErrTimeout, a bare
// errors.New("timeout") that is neither context.DeadlineExceeded nor a
// net.Error, so IsEchoUnsupported's fail-open default read a dead tcpws data
// path as "server has no echo". Trust grew, strikes stayed 0, and a dead
// tcpws primary went undetected for ~70s (macOS 0.4.15, iOS 0.4.14 logs).
// Drives a real smux session to a real read deadline, like the QUIC-side
// TestIsEchoUnsupported_RealStreamDeadlineIsNotUnsupported.
func TestTCPWSEcho_RealSmuxReadTimeoutIsFailure(t *testing.T) {
	client, cleanup := newTCPWSBlackholePair(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := client.RunEchoProbe(ctx)
	if err == nil {
		t.Fatal("echo against a black-holed tcpws session must fail")
	}
	if el := time.Since(start); el < 250*time.Millisecond || el > 2*time.Second {
		t.Fatalf("probe should end at its own 300ms read deadline, took %v (err=%v)", el, err)
	}
	if !errors.Is(err, smux.ErrTimeout) {
		t.Fatalf("expected the real smux read-deadline error, got %v", err)
	}
	if IsEchoUnsupported(err) {
		t.Fatalf("a tcpws read timeout must not read as 'unsupported': %v", err)
	}
	if IsEchoNotProbed(err) {
		t.Fatalf("a tcpws read timeout on a live session is a failure, not 'not probed': %v", err)
	}
}
