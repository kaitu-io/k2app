package wire

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/xtaci/smux/v2"
)

// Streams can be opened before the peer authenticates. A stream that never
// sends its header must be dropped after streamHeaderTimeout instead of
// pinning a server goroutine for the life of the connection.

func shortHeaderTimeout(t *testing.T) {
	old := streamHeaderTimeout
	streamHeaderTimeout = 100 * time.Millisecond
	t.Cleanup(func() { streamHeaderTimeout = old })
}

// rawTCPWSSession connects a bare smux client to a TCPWSServer, so the test
// controls exactly what each stream sends.
func rawTCPWSSession(t *testing.T) *smux.Session {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	server := NewTCPWSServer()
	server.HandleConn(serverConn)
	sess, err := smux.Client(clientConn, DefaultSmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close(); server.Close(); clientConn.Close(); serverConn.Close() })
	return sess
}

// serverEnded reports whether the server ended a stream (EOF on our read)
// within d.
func serverEnded(st io.Reader, d time.Duration) bool {
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(st)
		done <- err
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func TestTCPWSServer_SilentStreamDroppedAfterHeaderTimeout(t *testing.T) {
	shortHeaderTimeout(t)
	sess := rawTCPWSSession(t)
	st, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if !serverEnded(st, 3*time.Second) {
		t.Fatal("server still holds a stream that never sent a header")
	}
}

func TestTCPWSServer_StreamCap(t *testing.T) {
	old := tcpwsMaxStreams
	tcpwsMaxStreams = 2
	t.Cleanup(func() { tcpwsMaxStreams = old })
	sess := rawTCPWSSession(t)

	var held []*smux.Stream
	for i := 0; i < 2; i++ {
		st, err := sess.OpenStream()
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		held = append(held, st)
	}
	time.Sleep(50 * time.Millisecond) // both accepted and waiting on headers
	over, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	defer over.Close()
	if !serverEnded(over, 2*time.Second) {
		t.Fatal("stream past the cap was not refused")
	}
	for i, st := range held {
		if serverEnded(st, 50*time.Millisecond) {
			t.Fatalf("stream %d under the cap was closed", i)
		}
	}
	// A freed slot is usable again.
	held[0].Close()
	time.Sleep(50 * time.Millisecond)
	shortHeaderTimeout(t)
	again, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if serverEnded(again, 50*time.Millisecond) {
		t.Fatal("stream refused after a slot was freed")
	}
}

func TestQUICServer_SilentStreamDroppedAfterHeaderTimeout(t *testing.T) {
	shortHeaderTimeout(t)
	client, _, cleanup := setupQUICPair(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := client.connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	st, err := conn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// QUIC announces a stream with its first data: send half of a 2-byte
	// varint, so the server is left waiting inside the frame-type read.
	if _, err := st.Write([]byte{0x40}); err != nil {
		t.Fatal(err)
	}
	if !serverEnded(st, 3*time.Second) {
		t.Fatal("QUIC server still holds a stream that never sent a header")
	}
}
