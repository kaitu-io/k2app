package wire

import (
	"net"
	"testing"
	"time"
)

// ==========================================================================
// Tests for readLoop exit notification (R1-R4)
//
// When a mux's readLoop exits (e.g. underlying connection dies), all blocked
// ReadFrom calls must unblock immediately with net.ErrClosed, not hang forever.
// ==========================================================================

// TestQuicUDPConn_ReadFrom_UnblocksOnMuxDone verifies that a quicUDPConn blocked
// in ReadFrom (no data, no deadline) returns net.ErrClosed when mux.done is closed.
func TestQuicUDPConn_ReadFrom_UnblocksOnMuxDone(t *testing.T) {
	mux := &quicUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		done:        make(chan struct{}),
	}
	conn := &quicUDPConn{
		sessionID:  1,
		targetAddr: "8.8.8.8:53",
		mux:        mux,
		readCh:     make(chan *UDPFrame),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
	}

	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		_, _, err := conn.ReadFrom(buf)
		errCh <- err
	}()

	// Give ReadFrom time to enter select
	time.Sleep(50 * time.Millisecond)

	// Close mux.done (simulates readLoop exit)
	close(mux.done)

	select {
	case err := <-errCh:
		if err != net.ErrClosed {
			t.Fatalf("expected net.ErrClosed, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFrom did not unblock after mux.done closed — BUG: reader hangs forever")
	}
}

// TestQuicStreamUDPConn_ReadFrom_UnblocksOnMuxDone is the same test for
// stream-based UDP mux (used when QUIC datagrams are not supported).
func TestQuicStreamUDPConn_ReadFrom_UnblocksOnMuxDone(t *testing.T) {
	mux := &quicStreamUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		done:        make(chan struct{}),
	}
	conn := &quicStreamUDPConn{
		sessionID:  1,
		targetAddr: "8.8.8.8:53",
		mux:        mux,
		readCh:     make(chan *UDPFrame),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
	}

	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		_, _, err := conn.ReadFrom(buf)
		errCh <- err
	}()

	time.Sleep(50 * time.Millisecond)
	close(mux.done)

	select {
	case err := <-errCh:
		if err != net.ErrClosed {
			t.Fatalf("expected net.ErrClosed, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFrom did not unblock after mux.done closed — BUG: reader hangs forever")
	}
}

// TestSmuxUDPConn_ReadFrom_UnblocksOnMuxDone is the same test for TCP-WS
// smux-based UDP mux.
func TestSmuxUDPConn_ReadFrom_UnblocksOnMuxDone(t *testing.T) {
	mux := &smuxUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		done:        make(chan struct{}),
	}
	conn := &smuxUDPConn{
		sessionID:  1,
		targetAddr: "8.8.8.8:53",
		mux:        mux,
		readCh:     make(chan *UDPFrame),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
	}

	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		_, _, err := conn.ReadFrom(buf)
		errCh <- err
	}()

	time.Sleep(50 * time.Millisecond)
	close(mux.done)

	select {
	case err := <-errCh:
		if err != net.ErrClosed {
			t.Fatalf("expected net.ErrClosed, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFrom did not unblock after mux.done closed — BUG: reader hangs forever")
	}
}
