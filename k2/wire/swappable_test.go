package wire

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDialer is a minimal wire.Dialer for SwappableDialer tests.
// It records each Dial* invocation and a unique id so tests can assert
// which underlying Dialer served a given call.
type fakeDialer struct {
	id        string
	tcpCalls  atomic.Int64
	udpCalls  atomic.Int64
	sudpCalls atomic.Int64
	closed    atomic.Bool
	closeErr  error
}

func newFakeDialer(id string) *fakeDialer { return &fakeDialer{id: id} }

func (f *fakeDialer) DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
	f.tcpCalls.Add(1)
	if f.closed.Load() {
		return nil, errors.New("closed")
	}
	// Return a dummy conn that carries the dialer id in its LocalAddr for assertion.
	return &fakeConn{dialerID: f.id}, nil
}

func (f *fakeDialer) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	f.udpCalls.Add(1)
	if f.closed.Load() {
		return nil, errors.New("closed")
	}
	return &fakePacketConn{dialerID: f.id}, nil
}

func (f *fakeDialer) DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	f.sudpCalls.Add(1)
	if f.closed.Load() {
		return nil, errors.New("closed")
	}
	return &fakePacketConn{dialerID: f.id}, nil
}

func (f *fakeDialer) Reset() {}

func (f *fakeDialer) Close() error {
	f.closed.Store(true)
	return f.closeErr
}

// fakeConn / fakePacketConn are throwaway net.Conn / net.PacketConn stubs
// that carry the originating dialer id for test assertions. Only the methods
// touched by tests are meaningful; others satisfy the interfaces with no-ops.

type fakeAddr struct{ id string }

func (a *fakeAddr) Network() string { return "fake" }
func (a *fakeAddr) String() string  { return a.id }

type fakeConn struct {
	dialerID string
}

func (c *fakeConn) Read(b []byte) (int, error)       { return 0, errors.New("unused") }
func (c *fakeConn) Write(b []byte) (int, error)      { return 0, errors.New("unused") }
func (c *fakeConn) Close() error                     { return nil }
func (c *fakeConn) LocalAddr() net.Addr              { return &fakeAddr{id: c.dialerID} }
func (c *fakeConn) RemoteAddr() net.Addr             { return &fakeAddr{id: c.dialerID} }
func (c *fakeConn) SetDeadline(_ time.Time) error     { return nil }
func (c *fakeConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(_ time.Time) error { return nil }

type fakePacketConn struct {
	dialerID string
}

func (p *fakePacketConn) ReadFrom(b []byte) (int, net.Addr, error) { return 0, nil, errors.New("unused") }
func (p *fakePacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	return 0, errors.New("unused")
}
func (p *fakePacketConn) Close() error                         { return nil }
func (p *fakePacketConn) LocalAddr() net.Addr                  { return &fakeAddr{id: p.dialerID} }
func (p *fakePacketConn) SetDeadline(_ time.Time) error        { return nil }
func (p *fakePacketConn) SetReadDeadline(_ time.Time) error    { return nil }
func (p *fakePacketConn) SetWriteDeadline(_ time.Time) error   { return nil }

// --- Tests ---

func TestSwappableDialer_NewNilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on NewSwappableDialer(nil)")
		}
	}()
	_ = NewSwappableDialer(nil)
}

func TestSwappableDialer_SwapNilPanics(t *testing.T) {
	s := NewSwappableDialer(newFakeDialer("a"))
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on Swap(nil)")
		}
	}()
	_ = s.Swap(nil)
}

func TestSwappableDialer_DialForwardsToCurrent(t *testing.T) {
	fake := newFakeDialer("initial")
	s := NewSwappableDialer(fake)

	conn, err := s.DialTCP(context.Background(), "example.com:443", nil)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	if conn.LocalAddr().String() != "initial" {
		t.Errorf("dial routed to %q, want initial", conn.LocalAddr().String())
	}
	if fake.tcpCalls.Load() != 1 {
		t.Errorf("tcpCalls = %d, want 1", fake.tcpCalls.Load())
	}

	pc, err := s.DialUDP(context.Background(), "example.com:443")
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	if pc.LocalAddr().String() != "initial" {
		t.Errorf("udp routed to %q, want initial", pc.LocalAddr().String())
	}

	pc2, err := s.DialStreamUDP(context.Background(), "example.com:443")
	if err != nil {
		t.Fatalf("DialStreamUDP: %v", err)
	}
	if pc2.LocalAddr().String() != "initial" {
		t.Errorf("stream-udp routed to %q, want initial", pc2.LocalAddr().String())
	}
}

func TestSwappableDialer_SwapRedirectsFutureDials(t *testing.T) {
	d1 := newFakeDialer("d1")
	d2 := newFakeDialer("d2")
	s := NewSwappableDialer(d1)

	// Dial hits d1.
	conn, _ := s.DialTCP(context.Background(), "x", nil)
	if conn.LocalAddr().String() != "d1" {
		t.Fatalf("pre-swap routed to %q, want d1", conn.LocalAddr().String())
	}

	// Swap to d2.
	old := s.Swap(d2)
	if old != Dialer(d1) {
		t.Errorf("Swap returned wrong old dialer")
	}

	// Post-swap Dials hit d2.
	conn2, _ := s.DialTCP(context.Background(), "x", nil)
	if conn2.LocalAddr().String() != "d2" {
		t.Errorf("post-swap routed to %q, want d2", conn2.LocalAddr().String())
	}
	if d1.tcpCalls.Load() != 1 || d2.tcpCalls.Load() != 1 {
		t.Errorf("call counts d1=%d d2=%d, want 1 and 1",
			d1.tcpCalls.Load(), d2.tcpCalls.Load())
	}
}

func TestSwappableDialer_CurrentReturnsActive(t *testing.T) {
	d1 := newFakeDialer("d1")
	d2 := newFakeDialer("d2")
	s := NewSwappableDialer(d1)
	if s.Current() != Dialer(d1) {
		t.Error("Current() != d1 before swap")
	}
	_ = s.Swap(d2)
	if s.Current() != Dialer(d2) {
		t.Error("Current() != d2 after swap")
	}
}

// TestSwappableDialer_CloseForwards verifies that Close() reaches the
// currently-installed Dialer. Swap-returned old Dialers must be closed by
// the caller — Close() does not close them.
func TestSwappableDialer_CloseForwards(t *testing.T) {
	d1 := newFakeDialer("d1")
	d2 := newFakeDialer("d2")
	s := NewSwappableDialer(d1)

	old := s.Swap(d2)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !d2.closed.Load() {
		t.Error("Close did not forward to d2 (current)")
	}
	if d1.closed.Load() {
		t.Error("Close incorrectly forwarded to d1 (old, should be caller's responsibility)")
	}
	// Caller then closes the old one.
	if err := old.Close(); err != nil {
		t.Fatalf("old.Close: %v", err)
	}
	if !d1.closed.Load() {
		t.Error("old.Close did not close d1")
	}
}

// TestSwappableDialer_ConcurrentDialAndSwap stresses the atomic-swap path:
// one goroutine rapidly swaps between two Dialers while many goroutines Dial.
// Contract: every Dial reaches exactly one Dialer (old or new), never panics,
// never returns ErrSwappableEmpty. Call counts on the two dialers must sum
// to the total dial count (no lost calls, no duplicates).
func TestSwappableDialer_ConcurrentDialAndSwap(t *testing.T) {
	d1 := newFakeDialer("d1")
	d2 := newFakeDialer("d2")
	s := NewSwappableDialer(d1)

	const dials = 10_000
	const swappers = 1

	var wg sync.WaitGroup
	stopSwap := make(chan struct{})

	// Swapper: alternate d1/d2 as fast as possible.
	for i := 0; i < swappers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			toggle := true
			for {
				select {
				case <-stopSwap:
					return
				default:
				}
				if toggle {
					s.Swap(d2)
				} else {
					s.Swap(d1)
				}
				toggle = !toggle
			}
		}()
	}

	// Dialers: each does one DialTCP and exits.
	var okCount atomic.Int64
	var errCount atomic.Int64
	for i := 0; i < dials; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := s.DialTCP(context.Background(), "x", nil)
			if err != nil {
				errCount.Add(1)
				return
			}
			id := conn.LocalAddr().String()
			if id != "d1" && id != "d2" {
				t.Errorf("unexpected dialer id: %q", id)
			}
			okCount.Add(1)
		}()
	}

	// Wait for all dials, then stop swapper.
	waitCh := make(chan struct{})
	go func() {
		for okCount.Load()+errCount.Load() < dials {
		}
		close(waitCh)
	}()
	<-waitCh
	close(stopSwap)
	wg.Wait()

	if errCount.Load() != 0 {
		t.Errorf("got %d Dial errors; SwappableDialer must never return ErrSwappableEmpty", errCount.Load())
	}
	total := d1.tcpCalls.Load() + d2.tcpCalls.Load()
	if total != dials {
		t.Errorf("call count sum = %d, want %d (lost or double-counted)", total, dials)
	}
	if d1.tcpCalls.Load() == 0 || d2.tcpCalls.Load() == 0 {
		t.Errorf("one-sided distribution d1=%d d2=%d — swap or dial goroutine didn't run concurrently",
			d1.tcpCalls.Load(), d2.tcpCalls.Load())
	}
}
