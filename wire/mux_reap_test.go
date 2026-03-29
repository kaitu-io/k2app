package wire

import (
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// ==========================================================================
// Tests for idle session reap notification
//
// When a mux's reapIdleSessions removes an idle session, any smuxUDPConn /
// quicUDPConn / quicStreamUDPConn blocked in ReadFrom must unblock immediately
// with net.ErrClosed — not hang until the 5-second DNS proxy timeout.
//
// Bug: reapIdleSessions deletes the session's readCh from the map but does NOT
// signal the smuxUDPConn. ReadFrom blocks forever on the orphaned readCh.
// WriteTo still succeeds (writes to the smux stream) but responses are dropped
// by readLoop (session not found in map) — a zombie connection.
// ==========================================================================

// --- smuxUDPMux (TCP-WS transport) ---

func TestSmuxUDPConn_ReadFrom_UnblocksOnReap(t *testing.T) {
	mux := &smuxUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
		started:     true, // prevent readLoop (no stream in test)
	}

	// Register a session, get both readCh and reaped channel.
	readCh, reaped := mux.register(1)
	conn := &smuxUDPConn{
		sessionID:  1,
		targetAddr: "8.8.8.8:53",
		mux:        mux,
		readCh:     readCh,
		reaped:     reaped,
		done:       make(chan struct{}),
	}

	// Force session to look idle (created 61s ago).
	mux.mu.Lock()
	mux.lastActive[1].Store(time.Now().Unix() - 61)
	mux.mu.Unlock()

	// Block ReadFrom in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		_, _, err := conn.ReadFrom(buf)
		errCh <- err
	}()
	time.Sleep(50 * time.Millisecond)

	// Simulate reaper tick.
	mux.reapOnce()

	select {
	case err := <-errCh:
		if err != net.ErrClosed {
			t.Fatalf("expected net.ErrClosed, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFrom did not unblock after session reaped — BUG: zombie connection")
	}

	// Verify session removed from mux.
	mux.mu.Lock()
	_, inSessions := mux.sessions[1]
	_, inDone := mux.sessionDone[1]
	mux.mu.Unlock()
	if inSessions {
		t.Fatal("session still in mux.sessions after reap")
	}
	if inDone {
		t.Fatal("session still in mux.sessionDone after reap")
	}
}

func TestSmuxUDPMux_ReapDoesNotPanicConcurrentReadLoop(t *testing.T) {
	// Verify that reaping a session while readLoop holds a readCh reference
	// does NOT cause send-on-closed-channel panic. The readCh is never closed;
	// only the separate reaped channel is closed.
	mux := &smuxUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
		started:     true, // prevent readLoop/reapIdleSessions (no stream in test)
	}

	readCh, _ := mux.register(1)

	// Simulate readLoop holding readCh and trying to send after reap.
	// Force idle.
	mux.mu.Lock()
	mux.lastActive[1].Store(time.Now().Unix() - 61)
	mux.mu.Unlock()

	mux.reapOnce()

	// After reap, readLoop would have looked up sessions[1] → not found.
	// But if it held ch reference from before reap, send must not panic.
	// readCh is NOT closed, so this must not panic:
	select {
	case readCh <- &UDPFrame{Payload: []byte("test")}:
		// Sent successfully (buffered channel) — fine, just orphaned.
	default:
		// Channel full — also fine.
	}
	// If we reach here without panic, the test passes.
}

func TestSmuxUDPMux_ReapThenClose(t *testing.T) {
	mux := &smuxUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
		started:     true, // prevent readLoop/reapIdleSessions (no stream in test)
	}

	readCh, reaped := mux.register(1)
	conn := &smuxUDPConn{
		sessionID:  1,
		targetAddr: "8.8.8.8:53",
		mux:        mux,
		readCh:     readCh,
		reaped:     reaped,
		done:       make(chan struct{}),
	}

	// Reap first.
	mux.mu.Lock()
	mux.lastActive[1].Store(time.Now().Unix() - 61)
	mux.mu.Unlock()
	mux.reapOnce()

	// Then Close — must not double-close panic.
	conn.Close()
}

func TestSmuxUDPMux_CloseThenReap(t *testing.T) {
	mux := &smuxUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
		started:     true, // prevent readLoop/reapIdleSessions (no stream in test)
	}

	readCh, reaped := mux.register(1)
	conn := &smuxUDPConn{
		sessionID:  1,
		targetAddr: "8.8.8.8:53",
		mux:        mux,
		readCh:     readCh,
		reaped:     reaped,
		done:       make(chan struct{}),
	}

	// Close first (unregister removes from maps).
	conn.Close()

	// Force idle timestamp (already deleted by unregister, so reap is no-op).
	mux.reapOnce()
	// Must not panic.
}

func TestSmuxUDPConn_NormalReadUnchanged(t *testing.T) {
	mux := &smuxUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
		started:     true, // prevent readLoop/reapIdleSessions (no stream in test)
	}

	readCh, reaped := mux.register(1)
	conn := &smuxUDPConn{
		sessionID:  1,
		targetAddr: "8.8.8.8:53",
		mux:        mux,
		readCh:     readCh,
		reaped:     reaped,
		done:       make(chan struct{}),
	}

	// Send a frame to readCh (simulating mux readLoop).
	readCh <- &UDPFrame{Payload: []byte("hello")}

	buf := make([]byte, 64)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("expected 'hello', got %q", buf[:n])
	}
}

// --- quicUDPMux (QUIC datagram transport) ---

func TestQuicUDPConn_ReadFrom_UnblocksOnReap(t *testing.T) {
	mux := &quicUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
		started:     true,
	}

	readCh, reaped := mux.register(1)
	conn := &quicUDPConn{
		sessionID:  1,
		targetAddr: "8.8.8.8:53",
		mux:        mux,
		readCh:     readCh,
		reaped:     reaped,
		done:       make(chan struct{}),
	}

	mux.mu.Lock()
	mux.lastActive[1].Store(time.Now().Unix() - 61)
	mux.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		_, _, err := conn.ReadFrom(buf)
		errCh <- err
	}()
	time.Sleep(50 * time.Millisecond)

	mux.reapOnce()

	select {
	case err := <-errCh:
		if err != net.ErrClosed {
			t.Fatalf("expected net.ErrClosed, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFrom did not unblock after session reaped")
	}
}

// --- quicStreamUDPMux (QUIC stream fallback transport) ---

func TestQuicStreamUDPConn_ReadFrom_UnblocksOnReap(t *testing.T) {
	mux := &quicStreamUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
		started:     true,
	}

	readCh, reaped := mux.register(1)
	conn := &quicStreamUDPConn{
		sessionID:  1,
		targetAddr: "8.8.8.8:53",
		mux:        mux,
		readCh:     readCh,
		reaped:     reaped,
		done:       make(chan struct{}),
	}

	mux.mu.Lock()
	mux.lastActive[1].Store(time.Now().Unix() - 61)
	mux.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		_, _, err := conn.ReadFrom(buf)
		errCh <- err
	}()
	time.Sleep(50 * time.Millisecond)

	mux.reapOnce()

	select {
	case err := <-errCh:
		if err != net.ErrClosed {
			t.Fatalf("expected net.ErrClosed, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFrom did not unblock after session reaped")
	}
}
