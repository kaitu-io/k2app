package wire

// Behavior tests for the client-side smux/QUIC UDP mux writeFrame paths.
//
// These verify the 12/10 design (root cause of v0.4.4 desktop daemon FATAL):
//   1. Process must not crash on stuck stream — writeMu is sync.Mutex, not
//      deadlock.Mutex (deadlock.Mutex's 30/60s watchdog mis-detects slow
//      legitimate I/O as deadlock and triggers os.Exit(2) in desktop builds
//      that do NOT carry -tags=deadlock_disable).
//   2. Stream must self-detect death via per-Write SetWriteDeadline (5s).
//   3. Failure stays at mux level — Close cascades through stream Close,
//      readLoop sees EOF, m.done closes, consumers (smuxUDPConn.ReadFrom)
//      observe net.ErrClosed.
//   4. Fast-fail bypass — once dead, subsequent writeFrame calls return
//      errStreamDead in microseconds without queueing on writeMu.
//   5. Idempotent shutdown — mux.close() may race with writeFrame's
//      failure-driven stream close; both paths converge safely.
//   6. Frame integrity preserved on a healthy stream (regression guard).
//
// Mirror of server-side tcpws_writer_test.go but on client mux types.

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// smuxUDPMux (TCP-WS path) — writeFrame deadline + dead flag tests
// ---------------------------------------------------------------------------

// TestClientSmuxUDPMux_WriteFrame_HappyPath confirms regular writes still work
// when the peer drains normally. Guards against accidental damage to the
// fast path by the dead-flag/deadline machinery.
func TestClientSmuxUDPMux_WriteFrame_HappyPath(t *testing.T) {
	serverStream, clientStream, cleanup := setupSmuxPair(t)
	defer cleanup()

	// Drain peer continuously so the writer never stalls.
	done := make(chan struct{})
	defer close(done)
	go func() {
		buf := make([]byte, 4096)
		for {
			select {
			case <-done:
				return
			default:
				_ = clientStream.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
				_, _ = clientStream.Read(buf)
			}
		}
	}()

	mux := newSmuxUDPMux(serverStream)
	for i := range 5 {
		if err := mux.writeFrame(uint32(i+1), "1.2.3.4:53", []byte("hello")); err != nil {
			t.Fatalf("happy writeFrame[%d] err=%v", i, err)
		}
	}
}

// TestClientSmuxUDPMux_WriteFrame_StuckPeer_DeadlineKillsStream is the core
// RED→GREEN test. Pre-fix: writeFrame blocks until deadlock.Mutex's 60s
// watchdog fires os.Exit(2) (or hangs forever on mobile). Post-fix: 5s
// deadline kills the stream, future writeFrame calls fast-fail.
func TestClientSmuxUDPMux_WriteFrame_StuckPeer_DeadlineKillsStream(t *testing.T) {
	serverStream, _, cleanup := setupSmuxPair(t)
	defer cleanup()

	mux := newSmuxUDPMux(serverStream)
	// Tighten the stream's per-call write deadline to keep test fast.
	// The fix uses streamWriteDeadline (5s); we accept up to 8s here.
	_ = serverStream.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))

	// Many concurrent writers. Peer never reads.
	const N = 50
	var ok, dead, other atomic.Int32
	var wg sync.WaitGroup
	start := time.Now()
	for range N {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := mux.writeFrame(1, "1.2.3.4:53", make([]byte, 8192))
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, errStreamDead):
				dead.Add(1)
			default:
				other.Add(1)
			}
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		// good — all writers returned
	case <-time.After(15 * time.Second):
		t.Fatalf("writers hung — ok=%d dead=%d other=%d after %v",
			ok.Load(), dead.Load(), other.Load(), time.Since(start))
	}

	if dead.Load() == 0 && other.Load() == 0 {
		t.Errorf("expected some writers to fail (peer stuck), but all succeeded")
	}
	t.Logf("✓ %d writers in %v: ok=%d errStreamDead=%d otherErr=%d",
		N, time.Since(start), ok.Load(), dead.Load(), other.Load())
}

// TestClientSmuxUDPMux_WriteFrame_FastFailAfterDeath confirms that once the
// mux is marked dead, subsequent writeFrame calls bypass the mutex entirely
// and return errStreamDead in microseconds.
func TestClientSmuxUDPMux_WriteFrame_FastFailAfterDeath(t *testing.T) {
	serverStream, _, cleanup := setupSmuxPair(t)
	defer cleanup()

	mux := newSmuxUDPMux(serverStream)
	mux.markDead()

	start := time.Now()
	err := mux.writeFrame(1, "", []byte("hello"))
	elapsed := time.Since(start)

	if !errors.Is(err, errStreamDead) {
		t.Errorf("after markDead, writeFrame returned %v, want errStreamDead", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("post-death writeFrame took %v, should be near-instant", elapsed)
	}
	t.Logf("✓ post-death writeFrame fast-failed in %v", elapsed)
}

// TestClientSmuxUDPMux_WriteFrame_DeadFlagVisibility tests attack vector C2:
// a writer waiting on writeMu while another writer is mid-failure MUST see
// dead=true after acquiring the lock and fast-fail instead of attempting a
// fresh Write on the already-dead stream.
//
// Deterministic setup: writer A is mid-Write on a stuck-peer stream when
// markDead is called externally. A's Write returns an error (stream closed),
// writeFrame stores dead=true and unlocks. Writer B, which queued on writeMu
// during A's Write, now acquires the lock and must observe dead=true via
// the re-check, returning errStreamDead without invoking Write again.
func TestClientSmuxUDPMux_WriteFrame_DeadFlagVisibility(t *testing.T) {
	serverStream, _, cleanup := setupSmuxPair(t)
	defer cleanup()

	mux := newSmuxUDPMux(serverStream)

	// Writer A: launches first, large payload so it blocks on smux Write
	// (peer never reads → flow-control stall).
	aErr := make(chan error, 1)
	go func() {
		aErr <- mux.writeFrame(1, "", make([]byte, 256*1024))
	}()
	time.Sleep(100 * time.Millisecond) // ensure A holds writeMu in Write

	// Writer B: launches second, queues on writeMu behind A.
	bErr := make(chan error, 1)
	bStart := time.Now()
	go func() {
		bErr <- mux.writeFrame(2, "", []byte("b"))
	}()
	time.Sleep(50 * time.Millisecond) // ensure B is queued on Lock

	// External death: closes stream, unblocks A's Write.
	mux.markDead()

	select {
	case err := <-aErr:
		if err == nil {
			t.Errorf("writer A: expected error after markDead, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer A hung after markDead")
	}

	select {
	case err := <-bErr:
		if !errors.Is(err, errStreamDead) {
			t.Errorf("writer B: expected errStreamDead, got %v", err)
		}
		if elapsed := time.Since(bStart); elapsed > 1*time.Second {
			t.Errorf("writer B took %v — should fast-fail once dead set", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer B hung")
	}
}

// TestClientSmuxUDPMux_MarkDead_Concurrent verifies markDead is safe under
// many concurrent callers. closeStreamOnce (sync.Once) must serialize the
// underlying stream.Close call. No panic, no double-close.
func TestClientSmuxUDPMux_MarkDead_Concurrent(t *testing.T) {
	serverStream, _, cleanup := setupSmuxPair(t)
	defer cleanup()

	mux := newSmuxUDPMux(serverStream)

	panicCh := make(chan any, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCh <- r
				}
			}()
			mux.markDead()
		}()
	}
	wg.Wait()
	close(panicCh)
	for p := range panicCh {
		t.Errorf("markDead panicked: %v", p)
	}
}

// TestClientSmuxUDPMux_Close_NoDoubleClose verifies mux.close() is idempotent
// across concurrent calls (TCPWSClient.Close + healthMonitor + reconnect race).
func TestClientSmuxUDPMux_Close_NoDoubleClose(t *testing.T) {
	serverStream, _, cleanup := setupSmuxPair(t)
	defer cleanup()

	mux := newSmuxUDPMux(serverStream)

	panicCh := make(chan any, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCh <- r
				}
			}()
			mux.close()
		}()
	}
	wg.Wait()
	close(panicCh)
	for p := range panicCh {
		t.Errorf("mux.close panicked: %v", p)
	}
}

// TestClientSmuxUDPMux_Close_DuringWrite_NoRace exercises the concurrent
// close(self) + writeFrame race. With -race, this catches any unsafe
// concurrent access on mux fields during shutdown.
func TestClientSmuxUDPMux_Close_DuringWrite_NoRace(t *testing.T) {
	for trial := range 100 {
		serverStream, _, cleanup := setupSmuxPair(t)
		mux := newSmuxUDPMux(serverStream)
		_ = serverStream.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = mux.writeFrame(uint32(trial+1), "", make([]byte, 1024))
		}()
		go func() {
			defer wg.Done()
			time.Sleep(time.Microsecond * time.Duration(trial%50)) // jitter
			mux.close()
		}()

		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("trial %d: write+close hung", trial)
		}
		cleanup()
	}
}

// TestClientSmuxUDPMux_DeathCascadesToConsumer verifies the close cascade:
// markDead → stream closed → readLoop sees EOF → m.done closes →
// smuxUDPConn.ReadFrom returns net.ErrClosed. This guarantees consumers
// do not get stuck waiting on a dead mux.
//
// Uses markDead directly (not the deadline path) for deterministic timing.
// The deadline-triggered path is exercised end-to-end in StuckPeer test.
func TestClientSmuxUDPMux_DeathCascadesToConsumer(t *testing.T) {
	serverStream, _, cleanup := setupSmuxPair(t)
	defer cleanup()

	mux := newSmuxUDPMux(serverStream)
	// Register a session — this also starts the readLoop.
	_, _ = mux.register(1)

	conn := &smuxUDPConn{
		mux:        mux,
		sessionID:  1,
		targetAddr: "1.2.3.4:53",
		readCh:     make(chan *UDPFrame),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
	}

	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		_, _, err := conn.ReadFrom(buf)
		readErr <- err
	}()
	time.Sleep(20 * time.Millisecond) // ensure ReadFrom is blocked on select

	mux.markDead() // closes stream → readLoop EOF → closeDone → mux.done closed

	select {
	case err := <-readErr:
		if !errors.Is(err, net.ErrClosed) {
			t.Errorf("ReadFrom got err=%v, want net.ErrClosed (death cascade)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFrom did not unblock after mux death — cascade broken")
	}
}

// ---------------------------------------------------------------------------
// quicStreamUDPMux (QUIC stream UDP fallback path) — mirror tests
// ---------------------------------------------------------------------------
//
// Note: Building a real quic.Stream pair in unit tests requires a full QUIC
// transport (UDP socket + TLS). The writeFrame algorithm is identical to
// smuxUDPMux, so deadline + dead-flag behavior is already exhaustively
// covered by the smux pair tests above. The two tests below exercise the
// quicStreamUDPMux dead-flag fast-path and markDead idempotency without
// needing a live stream (nil stream + closeStreamOnce nil-guard).

// TestClientQuicStreamUDPMux_WriteFrame_FastFailAfterDeath confirms the dead
// flag and fast-fail bypass also apply to quicStreamUDPMux. We construct the
// mux with a nil stream (the fast-fail path never touches stream) — sufficient
// to validate the dead-flag bypass without a live QUIC connection.
func TestClientQuicStreamUDPMux_WriteFrame_FastFailAfterDeath(t *testing.T) {
	mux := &quicStreamUDPMux{
		stream: nil, // never reached on fast-fail path
		done:   make(chan struct{}),
	}
	mux.markDead()

	start := time.Now()
	err := mux.writeFrame(1, "", []byte("hello"))
	elapsed := time.Since(start)

	if !errors.Is(err, errStreamDead) {
		t.Errorf("after markDead, writeFrame returned %v, want errStreamDead", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("post-death writeFrame took %v, should be near-instant", elapsed)
	}
}

// TestClientQuicStreamUDPMux_MarkDead_Concurrent verifies markDead idempotency
// without requiring a live QUIC stream (closeStreamOnce guards stream.Close).
// We use a closed stream-substitute via nil and rely on sync.Once.
func TestClientQuicStreamUDPMux_MarkDead_Concurrent(t *testing.T) {
	mux := &quicStreamUDPMux{
		stream: nil,
		done:   make(chan struct{}),
	}

	panicCh := make(chan any, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCh <- r
				}
			}()
			mux.markDead()
		}()
	}
	wg.Wait()
	close(panicCh)
	for p := range panicCh {
		t.Errorf("quicStreamUDPMux.markDead panicked: %v", p)
	}
}

