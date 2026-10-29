package wire

import (
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// TestSmuxSessions_RunUDPHandler_RemovesSessionOnExit verifies that the
// TCP-WS smux UDP path removes a session entry from its per-stream map
// after HandleUDP returns. Without this, the map grows monotonically and
// hits maxServerUDPSessions (4096), silently dropping new flows for the
// rest of the stream's lifetime — same failure mode as QUIC server.
func TestSmuxSessions_RunUDPHandler_RemovesSessionOnExit(t *testing.T) {
	sessions := make(map[uint32]*smuxServerUDPConn)
	var mu deadlock.Mutex
	sess := &smuxServerUDPConn{
		sessionID: 42,
		readCh:    make(chan *UDPFrame, 32),
		done:      make(chan struct{}),
		log:       slog.Default(),
	}
	sessions[42] = sess

	handlerEntered := make(chan struct{})
	handler := UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		close(handlerEntered)
	})

	go runSmuxUDPHandler(sessions, &mu, 42, sess, handler, "1.2.3.4:53")

	select {
	case <-handlerEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never entered")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(sessions)
		mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	n := len(sessions)
	mu.Unlock()
	t.Errorf("expected 0 sessions after handler exit, got %d", n)
}

// TestSmuxSessions_RunUDPHandler_RemovesSessionOnPanic ensures the cleanup
// fires even when HandleUDP panics (recovered by the defer).
func TestSmuxSessions_RunUDPHandler_RemovesSessionOnPanic(t *testing.T) {
	sessions := make(map[uint32]*smuxServerUDPConn)
	var mu deadlock.Mutex
	sess := &smuxServerUDPConn{
		sessionID: 99,
		readCh:    make(chan *UDPFrame, 32),
		done:      make(chan struct{}),
		log:       slog.Default(),
	}
	sessions[99] = sess

	handlerEntered := make(chan struct{})
	handler := UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		close(handlerEntered)
		panic("simulated handler panic")
	})

	go runSmuxUDPHandler(sessions, &mu, 99, sess, handler, "1.2.3.4:53")

	<-handlerEntered

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(sessions)
		mu.Unlock()
		if n == 0 {
			select {
			case <-sess.done:
				return
			default:
				t.Fatal("sess.done not closed after panic recovery")
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	n := len(sessions)
	mu.Unlock()
	t.Errorf("expected 0 sessions after handler panic, got %d", n)
}
