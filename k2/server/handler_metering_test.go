package server

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// drivePipe runs h.pipe(a, b) where the client side writes `uplink` bytes and
// the target side writes `downlink` bytes, then both half-streams close so pipe
// returns. net.Pipe conns are synchronous and have no half-close, so each side
// must be driven by its own goroutine that both feeds its bytes and drains the
// opposite direction.
//
//	a side:  aClient <-> a    (client)
//	b side:  b       <-> bPeer (target)
//
// pipe copies a->b (uplink => rx) and b->a (downlink => tx).
func drivePipe(t *testing.T, h *ProxyHandler, uplink, downlink int) {
	t.Helper()
	aClient, a := net.Pipe()
	b, bPeer := net.Pipe()

	var wg sync.WaitGroup
	wg.Add(2)

	// Client side: write uplink, then drain whatever target sends back.
	go func() {
		defer wg.Done()
		if uplink > 0 {
			if _, err := aClient.Write(make([]byte, uplink)); err != nil {
				t.Errorf("aClient.Write: %v", err)
			}
		}
		// Done sending uplink: close our write so pipe's a->b copy sees EOF.
		// net.Pipe has no CloseWrite, so we drain downlink first to avoid
		// blocking the b->a copy, then full-close.
		io.Copy(io.Discard, aClient)
		aClient.Close()
	}()

	// Target side: drain uplink, then write downlink and close.
	go func() {
		defer wg.Done()
		if uplink > 0 {
			io.CopyN(io.Discard, bPeer, int64(uplink))
		}
		if downlink > 0 {
			if _, err := bPeer.Write(make([]byte, downlink)); err != nil {
				t.Errorf("bPeer.Write: %v", err)
			}
		}
		bPeer.Close()
	}()

	done := make(chan struct{})
	go func() {
		h.pipe(a, b)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pipe did not return within 5s")
	}
	wg.Wait()
}

// TestProxyHandlerMetering drives h.pipe with a known number of bytes in each
// direction and asserts the node-level rx/tx counters reflect them, then that
// ResetTraffic zeroes them.
func TestProxyHandlerMetering(t *testing.T) {
	h := NewProxyHandler()

	// rx/tx must start at zero on a fresh handler.
	if rx, tx := h.TrafficBytes(); rx != 0 || tx != 0 {
		t.Fatalf("fresh handler counters: got rx=%d tx=%d, want 0/0", rx, tx)
	}

	const uplink = 1234   // bytes client -> target (rx)
	const downlink = 5678 // bytes target -> client (tx)
	drivePipe(t, h, uplink, downlink)

	rx, tx := h.TrafficBytes()
	if rx != uplink {
		t.Errorf("rx counter: got %d, want %d", rx, uplink)
	}
	if tx != downlink {
		t.Errorf("tx counter: got %d, want %d", tx, downlink)
	}

	// A second session accumulates onto the same node-level counters.
	drivePipe(t, h, uplink, downlink)
	rx, tx = h.TrafficBytes()
	if rx != 2*uplink {
		t.Errorf("rx after 2 sessions: got %d, want %d", rx, 2*uplink)
	}
	if tx != 2*downlink {
		t.Errorf("tx after 2 sessions: got %d, want %d", tx, 2*downlink)
	}

	h.ResetTraffic()
	if rx, tx := h.TrafficBytes(); rx != 0 || tx != 0 {
		t.Errorf("after ResetTraffic: got rx=%d tx=%d, want 0/0", rx, tx)
	}
}

// TestAcceptingGate asserts the accepting gate defaults to true (the critical
// "default must SERVE" invariant) and toggles correctly.
func TestAcceptingGate(t *testing.T) {
	h := NewProxyHandler()

	if !h.Accepting() {
		t.Fatal("fresh handler must default to Accepting()==true (default must SERVE)")
	}

	h.SetAccepting(false)
	if h.Accepting() {
		t.Error("after SetAccepting(false): Accepting() should be false")
	}

	h.SetAccepting(true)
	if !h.Accepting() {
		t.Error("after SetAccepting(true): Accepting() should be true")
	}
}
