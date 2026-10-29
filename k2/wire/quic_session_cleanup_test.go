package wire

import (
	"log/slog"
	"net"
	"testing"
	"time"
)

// TestConnUDPState_RunUDPHandler_RemovesSessionOnExit verifies that when a
// UDP handler returns (e.g. server-side ProxyHandler.HandleUDP exits after
// 60s idle timeout), the corresponding session entry is removed from the
// connection's session map. Without this, state.sessions grows monotonically
// for the lifetime of the QUIC connection, eventually hitting maxServerUDPSessions
// (4096) and silently dropping new flows.
func TestConnUDPState_RunUDPHandler_RemovesSessionOnExit(t *testing.T) {
	state := &connUDPState{
		sessions: make(map[uint32]*serverUDPConn),
		done:     make(chan struct{}),
		log:      slog.Default(),
	}
	sess := &serverUDPConn{
		sessionID: 42,
		readCh:    make(chan *UDPFrame, 32),
		done:      make(chan struct{}),
		log:       slog.Default(),
	}
	state.sessions[42] = sess

	handlerEntered := make(chan struct{})
	handler := UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		close(handlerEntered)
		// return immediately — simulates ProxyHandler.HandleUDP after target idle
	})

	go state.runUDPHandler(handler, sess, "1.2.3.4:53")

	select {
	case <-handlerEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never entered")
	}

	// Wait for the deferred cleanup to fire. The defer runs after handler
	// returns; we poll because there's no synchronization signal we can
	// observe externally.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state.mu.Lock()
		n := len(state.sessions)
		state.mu.Unlock()
		if n == 0 {
			return // PASS
		}
		time.Sleep(5 * time.Millisecond)
	}

	state.mu.Lock()
	n := len(state.sessions)
	state.mu.Unlock()
	t.Errorf("expected 0 sessions after handler exit, got %d", n)
}

// TestConnUDPState_RunUDPHandler_RemovesSessionOnPanic verifies that even
// when the handler panics (recovered by the defer), the session is still
// removed from the map. Without this, a single panic can leak a session
// slot forever.
func TestConnUDPState_RunUDPHandler_RemovesSessionOnPanic(t *testing.T) {
	state := &connUDPState{
		sessions: make(map[uint32]*serverUDPConn),
		done:     make(chan struct{}),
		log:      slog.Default(),
	}
	sess := &serverUDPConn{
		sessionID: 99,
		readCh:    make(chan *UDPFrame, 32),
		done:      make(chan struct{}),
		log:       slog.Default(),
	}
	state.sessions[99] = sess

	handlerEntered := make(chan struct{})
	handler := UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		close(handlerEntered)
		panic("simulated handler panic")
	})

	go state.runUDPHandler(handler, sess, "1.2.3.4:53")

	<-handlerEntered

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state.mu.Lock()
		n := len(state.sessions)
		state.mu.Unlock()
		if n == 0 {
			// Also verify done was closed (sess.close() ran).
			select {
			case <-sess.done:
				return // PASS
			default:
				t.Fatal("sess.done was not closed after panic recovery")
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	state.mu.Lock()
	n := len(state.sessions)
	state.mu.Unlock()
	t.Errorf("expected 0 sessions after handler panic, got %d", n)
}
