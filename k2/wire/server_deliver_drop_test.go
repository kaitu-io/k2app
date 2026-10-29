package wire

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

// deliverProbe runs deliver() in a goroutine and returns whether it completed
// within `budget`. A blocking deliver returns false; a drop-or-deliver path
// returns true.
func deliverProbe(t *testing.T, fn func(), budget time.Duration) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(budget):
		return false
	}
}

// TestSmuxServerUDPConn_Deliver_DropsWhenReadChFull is the RED test for the
// v0.4.4 client-deadlock root cause: server's smuxServerUDPConn.deliver
// blocks the handleUDPStream main loop when handler is slow, freezing smux
// peer-window flow control and triggering client-side writeMu deadlock FATAL.
//
// Mirror of the protection already in serverUDPConn.deliver (quic.go:1860,
// datagram path): readCh full + done open MUST drop, not block.
func TestSmuxServerUDPConn_Deliver_DropsWhenReadChFull(t *testing.T) {
	c := &smuxServerUDPConn{
		sessionID: 42,
		addr:      "8.8.8.8:53",
		log:       slog.Default(),
		readCh:    make(chan *UDPFrame, 2), // small buffer to force full
		done:      make(chan struct{}),
	}
	// Fill readCh — handler is "slow" / not consuming.
	c.readCh <- &UDPFrame{Payload: []byte("frame-1")}
	c.readCh <- &UDPFrame{Payload: []byte("frame-2")}

	// done is open. With current bug (no default branch) this deliver blocks
	// forever. With fix, it drops and returns immediately.
	if !deliverProbe(t, func() { c.deliver(&UDPFrame{Payload: []byte("frame-3")}) }, 200*time.Millisecond) {
		t.Fatal("smuxServerUDPConn.deliver blocked when readCh full + done open " +
			"— would freeze handleUDPStream main loop and trigger client deadlock FATAL")
	}

	// Drained frames must be exactly the first two (drop discarded the third).
	if got := string((<-c.readCh).Payload); got != "frame-1" {
		t.Errorf("first frame = %q, want frame-1", got)
	}
	if got := string((<-c.readCh).Payload); got != "frame-2" {
		t.Errorf("second frame = %q, want frame-2", got)
	}
	select {
	case extra := <-c.readCh:
		t.Errorf("expected drop, but got extra frame: %q", string(extra.Payload))
	default:
		// ok — frame-3 was dropped
	}
}

// TestQuicStreamServerUDPConn_Deliver_DropsWhenReadChFull — same RED test for
// the QUIC stream UDP fallback path (quic.go:2053). Same root cause, same fix.
func TestQuicStreamServerUDPConn_Deliver_DropsWhenReadChFull(t *testing.T) {
	c := &quicStreamServerUDPConn{
		sessionID: 7,
		addr:      "1.1.1.1:53",
		log:       slog.Default(),
		readCh:    make(chan *UDPFrame, 2),
		done:      make(chan struct{}),
	}
	c.readCh <- &UDPFrame{Payload: []byte("frame-1")}
	c.readCh <- &UDPFrame{Payload: []byte("frame-2")}

	if !deliverProbe(t, func() { c.deliver(&UDPFrame{Payload: []byte("frame-3")}) }, 200*time.Millisecond) {
		t.Fatal("quicStreamServerUDPConn.deliver blocked when readCh full + done open")
	}

	if got := string((<-c.readCh).Payload); got != "frame-1" {
		t.Errorf("first frame = %q, want frame-1", got)
	}
	if got := string((<-c.readCh).Payload); got != "frame-2" {
		t.Errorf("second frame = %q, want frame-2", got)
	}
	select {
	case extra := <-c.readCh:
		t.Errorf("expected drop, but got extra frame: %q", string(extra.Payload))
	default:
	}
}

// TestSmuxServerUDPConn_Deliver_HappyPath confirms the fix preserves the
// happy-path behavior — a non-full readCh accepts the frame as before.
func TestSmuxServerUDPConn_Deliver_HappyPath(t *testing.T) {
	c := &smuxServerUDPConn{
		readCh: make(chan *UDPFrame, 4),
		done:   make(chan struct{}),
		log:    slog.Default(),
	}
	c.deliver(&UDPFrame{Payload: []byte("hello")})
	select {
	case got := <-c.readCh:
		if string(got.Payload) != "hello" {
			t.Errorf("got %q, want hello", string(got.Payload))
		}
	default:
		t.Fatal("happy path: deliver did not enqueue to readCh")
	}
}

// TestSmuxServerUDPConn_Deliver_DoneClosed confirms the done-close path still
// returns (already worked before fix, but the select arm must be preserved).
func TestSmuxServerUDPConn_Deliver_DoneClosed(t *testing.T) {
	c := &smuxServerUDPConn{
		readCh: make(chan *UDPFrame), // unbuffered — full at all times
		done:   make(chan struct{}),
		log:    slog.Default(),
	}
	close(c.done)

	if !deliverProbe(t, func() { c.deliver(&UDPFrame{Payload: []byte("x")}) }, 100*time.Millisecond) {
		t.Fatal("deliver did not return after done was closed")
	}
}

// TestSmuxServerUDPConn_Deliver_HighConcurrency_NoBlocking exercises the real
// failure mode: many goroutines deliver to one session whose handler is
// stalled. Without the fix, deliveries pile up and block handleUDPStream;
// with the fix all deliveries return promptly (some succeed up to buffer,
// rest are dropped — but none hang).
func TestSmuxServerUDPConn_Deliver_HighConcurrency_NoBlocking(t *testing.T) {
	c := &smuxServerUDPConn{
		readCh: make(chan *UDPFrame, 8),
		done:   make(chan struct{}),
		log:    slog.Default(),
	}
	const N = 100
	var wg sync.WaitGroup
	wg.Add(N)
	for i := range N {
		go func() {
			defer wg.Done()
			c.deliver(&UDPFrame{Payload: []byte{byte(i)}})
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		// pass — all deliveries returned
	case <-time.After(2 * time.Second):
		t.Fatalf("deliver(s) hung — %d concurrent calls did not all return", N)
	}
}

// ---------------------------------------------------------------------------
// Attack-mode boundary tests (added under 12/10 confidence requirement)
// ---------------------------------------------------------------------------

// TestSmuxServerUDPConn_Deliver_CrossSessionIsolation proves the CORE property
// the fix exists for: when session-A's handler is stuck (readCh full + handler
// not draining), session-B on the same stream must still accept delivery
// without head-of-line blocking. Pre-fix this property is violated — the
// stuck session blocks the entire handleUDPStream main loop, freezing every
// other session on the same smux stream.
func TestSmuxServerUDPConn_Deliver_CrossSessionIsolation(t *testing.T) {
	stuck := &smuxServerUDPConn{
		sessionID: 1,
		readCh:    make(chan *UDPFrame, 2), // small to force full
		done:      make(chan struct{}),
		log:       slog.Default(),
	}
	healthy := &smuxServerUDPConn{
		sessionID: 2,
		readCh:    make(chan *UDPFrame, 2),
		done:      make(chan struct{}),
		log:       slog.Default(),
	}
	// Stuck session: fill readCh, handler does NOT drain.
	stuck.readCh <- &UDPFrame{Payload: []byte("a")}
	stuck.readCh <- &UDPFrame{Payload: []byte("b")}

	// Simulate main loop: stuck session deliver MUST return so main loop
	// can immediately deliver to the healthy session.
	mainLoopComplete := make(chan struct{})
	go func() {
		stuck.deliver(&UDPFrame{Payload: []byte("c")})                  // dropped by fix
		healthy.deliver(&UDPFrame{Payload: []byte("for-healthy-sess")}) // must reach healthy
		close(mainLoopComplete)
	}()

	select {
	case <-mainLoopComplete:
		// fix works — main loop progressed past the stuck session.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("main loop blocked on stuck session — cross-session isolation broken")
	}

	// Healthy session must have received the frame.
	select {
	case got := <-healthy.readCh:
		if string(got.Payload) != "for-healthy-sess" {
			t.Errorf("healthy session got %q, want for-healthy-sess", string(got.Payload))
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("healthy session did not receive frame despite main loop progressing")
	}
}

// TestSmuxServerUDPConn_Deliver_CloseDuringDeliverRace stresses the
// concurrent path where sess.close() (from handler exit) closes c.done while
// the main loop is mid-deliver. The combination of select with default + an
// open->closed done channel must NEVER block, panic, or leak — race detector
// catches data races, this asserts liveness.
func TestSmuxServerUDPConn_Deliver_CloseDuringDeliverRace(t *testing.T) {
	for trial := range 200 {
		c := &smuxServerUDPConn{
			sessionID: uint32(trial),
			readCh:    make(chan *UDPFrame, 1),
			done:      make(chan struct{}),
			log:       slog.Default(),
		}
		// Pre-fill so delivery without close would hit default.
		c.readCh <- &UDPFrame{Payload: []byte("preload")}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			// Race window: close fires anywhere relative to deliver's select.
			close(c.done)
		}()
		go func() {
			defer wg.Done()
			c.deliver(&UDPFrame{Payload: []byte("racing-frame")})
		}()

		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("trial %d: deliver/close racing pair hung", trial)
		}
	}
}

// TestSmuxServerUDPConn_Deliver_ReadFromAfterDrop verifies that the fix does
// not break the read path: frames enqueued BEFORE the drop must still be
// readable via ReadFrom, and frames AFTER the drop (once readCh drains) must
// also be deliverable. This catches any accidental damage to the read-side
// semantics.
func TestSmuxServerUDPConn_Deliver_ReadFromAfterDrop(t *testing.T) {
	c := &smuxServerUDPConn{
		sessionID: 1,
		addr:      "1.2.3.4:53",
		readCh:    make(chan *UDPFrame, 2),
		done:      make(chan struct{}),
		log:       slog.Default(),
	}

	// Fill, force drop, drain, refill — confirms read-path integrity.
	c.deliver(&UDPFrame{Payload: []byte("a")})
	c.deliver(&UDPFrame{Payload: []byte("b")})
	c.deliver(&UDPFrame{Payload: []byte("c")}) // dropped
	c.deliver(&UDPFrame{Payload: []byte("d")}) // dropped

	buf := make([]byte, 64)
	n, _, err := c.ReadFrom(buf)
	if err != nil || string(buf[:n]) != "a" {
		t.Fatalf("ReadFrom #1: got %q err=%v, want 'a'", string(buf[:n]), err)
	}
	n, _, err = c.ReadFrom(buf)
	if err != nil || string(buf[:n]) != "b" {
		t.Fatalf("ReadFrom #2: got %q err=%v, want 'b'", string(buf[:n]), err)
	}

	// readCh drained. New deliver should now succeed (not drop).
	c.deliver(&UDPFrame{Payload: []byte("e")})
	n, _, err = c.ReadFrom(buf)
	if err != nil || string(buf[:n]) != "e" {
		t.Fatalf("ReadFrom post-drain: got %q err=%v, want 'e'", string(buf[:n]), err)
	}
}

// TestSmuxServerUDPConn_Deliver_NilReadCh_DoesNotCrash asserts that a
// zero-value sess (readCh == nil, done == nil) routes to default-drop without
// panic. Production code always initializes both, but defensive: a future
// refactor that nils these out (e.g., post-close) must not crash.
//
// Pre-fix: deliver on nil readCh blocks forever (nil chan send blocks).
// Post-fix: default branch fires, drop is logged, deliver returns.
func TestSmuxServerUDPConn_Deliver_NilReadCh_DoesNotCrash(t *testing.T) {
	c := &smuxServerUDPConn{
		sessionID: 1,
		log:       slog.Default(),
		// readCh: nil — send to nil chan blocks
		// done:   nil — recv from nil chan blocks
	}
	if !deliverProbe(t, func() { c.deliver(&UDPFrame{Payload: []byte("x")}) }, 100*time.Millisecond) {
		t.Fatal("deliver on nil readCh/done blocked — default branch did not fire")
	}
}

// Mirror tests for quicStreamServerUDPConn: same boundary properties must hold
// on the QUIC-stream UDP fallback path.

func TestQuicStreamServerUDPConn_Deliver_CrossSessionIsolation(t *testing.T) {
	stuck := &quicStreamServerUDPConn{
		sessionID: 1,
		readCh:    make(chan *UDPFrame, 2),
		done:      make(chan struct{}),
		log:       slog.Default(),
	}
	healthy := &quicStreamServerUDPConn{
		sessionID: 2,
		readCh:    make(chan *UDPFrame, 2),
		done:      make(chan struct{}),
		log:       slog.Default(),
	}
	stuck.readCh <- &UDPFrame{Payload: []byte("a")}
	stuck.readCh <- &UDPFrame{Payload: []byte("b")}

	mainLoopComplete := make(chan struct{})
	go func() {
		stuck.deliver(&UDPFrame{Payload: []byte("c")})
		healthy.deliver(&UDPFrame{Payload: []byte("for-healthy-sess")})
		close(mainLoopComplete)
	}()

	select {
	case <-mainLoopComplete:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("quic-stream: cross-session isolation broken")
	}

	select {
	case got := <-healthy.readCh:
		if string(got.Payload) != "for-healthy-sess" {
			t.Errorf("quic-stream: healthy got %q", string(got.Payload))
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("quic-stream: healthy did not receive frame")
	}
}

func TestQuicStreamServerUDPConn_Deliver_CloseDuringDeliverRace(t *testing.T) {
	for trial := range 200 {
		c := &quicStreamServerUDPConn{
			sessionID: uint32(trial),
			readCh:    make(chan *UDPFrame, 1),
			done:      make(chan struct{}),
			log:       slog.Default(),
		}
		c.readCh <- &UDPFrame{Payload: []byte("preload")}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); close(c.done) }()
		go func() { defer wg.Done(); c.deliver(&UDPFrame{Payload: []byte("racing")}) }()

		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("quic-stream trial %d: hung", trial)
		}
	}
}

func TestQuicStreamServerUDPConn_Deliver_NilReadCh_DoesNotCrash(t *testing.T) {
	c := &quicStreamServerUDPConn{
		sessionID: 1,
		log:       slog.Default(),
	}
	if !deliverProbe(t, func() { c.deliver(&UDPFrame{Payload: []byte("x")}) }, 100*time.Millisecond) {
		t.Fatal("quic-stream: deliver on nil chans blocked")
	}
}
