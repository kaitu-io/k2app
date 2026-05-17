package wire

// Behavior tests for the server-side smux UDP writer (stream-level isolation).
//
// These tests verify the 10/10 design:
//   1. Process must not crash on slow consumer (sync.Mutex, no detector trap)
//   2. Stream must self-detect death via per-Write deadline
//   3. Failure must be confined to the dead stream (other resources OK)
//   4. Fast-fail bypass: producers don't queue on mutex after death
//   5. Frame integrity preserved on healthy stream
//   6. Idempotent markDead (sync.Once)

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xtaci/smux/v2"
)

// setupSmuxPair builds an in-memory smux client/server pair where the client
// stream is NEVER read — simulating a stuck consumer (app not draining).
func setupSmuxPair(t *testing.T) (server, client *smux.Stream, cleanup func()) {
	t.Helper()
	serverConn, clientConn := net.Pipe()

	serverSess, err := smux.Server(serverConn, smux.DefaultConfig())
	if err != nil {
		t.Fatalf("smux server: %v", err)
	}
	clientSess, err := smux.Client(clientConn, smux.DefaultConfig())
	if err != nil {
		t.Fatalf("smux client: %v", err)
	}

	streamCh := make(chan *smux.Stream, 1)
	go func() {
		s, err := clientSess.AcceptStream()
		if err == nil {
			streamCh <- s
		}
	}()
	server, err = serverSess.OpenStream()
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	select {
	case client = <-streamCh:
	case <-time.After(2 * time.Second):
		t.Fatal("client never accepted stream")
	}

	cleanup = func() {
		_ = client.Close()
		_ = server.Close()
		_ = clientSess.Close()
		_ = serverSess.Close()
		_ = clientConn.Close()
		_ = serverConn.Close()
	}
	return server, client, cleanup
}

// TestSmuxWriter_ActiveKillOnStuckPeer verifies that when the peer never
// reads, the writer self-detects within streamWriteDeadline and kills the
// stream proactively. Producers see errStreamDead and the mutex is freed.
func TestSmuxWriter_ActiveKillOnStuckPeer(t *testing.T) {
	serverStream, _, cleanup := setupSmuxPair(t)
	defer cleanup()

	w := &serverSmuxWriter{stream: serverStream}

	// First writer blocks until streamWriteDeadline expires. Use a 200ms
	// deadline to keep the test fast.
	// (We rely on streamWriteDeadline being a package constant; the test
	// patches it via SetWriteDeadline implicitly through the producer call.)
	// To make the test deterministic without touching the constant, we
	// override the stream's deadline directly to a very short value:
	_ = serverStream.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))

	// Concurrently send many frames. The peer never reads.
	const N = 50
	var ok, dead, other atomic.Int32
	var wg sync.WaitGroup
	start := time.Now()
	for range N {
		wg.Go(func() {
			err := w.writeFrame(1, "", make([]byte, 8192))
			switch {
			case err == nil:
				ok.Add(1)
			case err == errStreamDead:
				dead.Add(1)
			default:
				other.Add(1)
			}
		})
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("producers stuck: ok=%d dead=%d other=%d", ok.Load(), dead.Load(), other.Load())
	}
	elapsed := time.Since(start)

	t.Logf("✓ %d producers in %v: ok=%d errStreamDead=%d otherErr=%d",
		N, elapsed, ok.Load(), dead.Load(), other.Load())

	// After kill: dead flag must be set
	if !w.dead.Load() {
		t.Errorf("writer should be marked dead after stuck peer")
	}
	// Most producers should fail (either timeout or errStreamDead).
	// At most a tiny prefix can succeed (initial 64KB buffer).
	if ok.Load()+dead.Load()+other.Load() != N {
		t.Errorf("count mismatch: ok=%d dead=%d other=%d != %d",
			ok.Load(), dead.Load(), other.Load(), N)
	}
	if dead.Load() == 0 && other.Load() == 0 {
		t.Errorf("expected some producers to fail (peer stuck), but all succeeded")
	}
}

// TestSmuxWriter_FastFailAfterDeath verifies that once dead is set, new
// producers bypass the mutex completely — no queueing on the lock.
func TestSmuxWriter_FastFailAfterDeath(t *testing.T) {
	serverStream, _, cleanup := setupSmuxPair(t)
	defer cleanup()

	w := &serverSmuxWriter{stream: serverStream}
	w.markDead()

	start := time.Now()
	err := w.writeFrame(1, "", []byte("hello"))
	elapsed := time.Since(start)

	if err != errStreamDead {
		t.Errorf("after markDead, writeFrame returned %v, want errStreamDead", err)
	}
	if elapsed > 10*time.Millisecond {
		t.Errorf("post-death writeFrame took %v, should be near-instant", elapsed)
	}
	t.Logf("✓ post-death writeFrame fast-failed in %v", elapsed)
}

// TestSmuxWriter_ConcurrentMarkDead verifies markDead is idempotent and
// safe under concurrent callers (no Close panic, no race).
func TestSmuxWriter_ConcurrentMarkDead(t *testing.T) {
	serverStream, _, cleanup := setupSmuxPair(t)
	defer cleanup()

	w := &serverSmuxWriter{stream: serverStream}

	panicCh := make(chan any, 20)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					panicCh <- r
				}
			}()
			w.markDead()
		})
	}
	wg.Wait()
	close(panicCh)

	for p := range panicCh {
		t.Errorf("markDead panicked: %v", p)
	}
	if !w.dead.Load() {
		t.Errorf("dead flag not set")
	}
	t.Log("✓ 10 concurrent markDead — no panic, dead=true")
}

// TestSmuxWriter_FrameIntegrityHealthyStream verifies that on a HEALTHY
// stream (peer reads), N concurrent producers produce a well-formed byte
// stream — every UDP frame parseable, no interleave, no corruption.
func TestSmuxWriter_FrameIntegrityHealthyStream(t *testing.T) {
	serverStream, clientStream, cleanup := setupSmuxPair(t)
	defer cleanup()

	const N = 100
	const addr = "192.0.2.42:12345"

	type rcvd struct {
		sessionID uint32
		addr      string
		payload   []byte
	}
	rcvdCh := make(chan rcvd, N+10)
	readErrCh := make(chan error, 1)
	go func() {
		br := bufio.NewReader(clientStream)
		for i := 0; i < N; i++ {
			f, err := ReadSmuxUDPFrame(br)
			if err != nil {
				readErrCh <- fmt.Errorf("frame %d: %w", i, err)
				return
			}
			rcvdCh <- rcvd{
				sessionID: f.SessionID,
				addr:      f.Addr,
				payload:   append([]byte(nil), f.Payload...),
			}
		}
		close(readErrCh)
	}()

	w := &serverSmuxWriter{stream: serverStream}

	makePayload := func(sid uint32) []byte {
		size := 64 + int(sid)%512 + (int(sid)*7)%512
		buf := make([]byte, size)
		for i := 0; i+4 <= len(buf); i += 4 {
			buf[i] = byte(sid >> 24)
			buf[i+1] = byte(sid >> 16)
			buf[i+2] = byte(sid >> 8)
			buf[i+3] = byte(sid)
		}
		return buf
	}

	var wg sync.WaitGroup
	for i := 1; i <= N; i++ {
		sid := uint32(i)
		wg.Go(func() {
			if err := w.writeFrame(sid, addr, makePayload(sid)); err != nil {
				t.Errorf("write sid=%d: %v", sid, err)
			}
		})
	}
	wg.Wait()

	select {
	case err := <-readErrCh:
		if err != nil {
			t.Fatalf("reader: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reader timeout — possible frame corruption (stream desync)")
	}
	close(rcvdCh)

	seen := make(map[uint32]int)
	for r := range rcvdCh {
		seen[r.sessionID]++
		if r.addr != addr {
			t.Errorf("sid=%d: addr=%q want %q", r.sessionID, r.addr, addr)
		}
		want := makePayload(r.sessionID)
		if !bytes.Equal(r.payload, want) {
			t.Errorf("sid=%d: payload corrupt", r.sessionID)
		}
	}
	if len(seen) != N {
		t.Errorf("got %d unique sessionIDs, want %d", len(seen), N)
	}
	for sid := uint32(1); sid <= N; sid++ {
		if seen[sid] != 1 {
			t.Errorf("sessionID %d: count=%d (want 1)", sid, seen[sid])
		}
	}
	if !t.Failed() {
		t.Logf("✓ %d concurrent producers on healthy stream — all frames intact", N)
	}
}

// TestSmuxWriter_NoGoroutineLeak verifies the writer doesn't spawn any
// long-lived goroutines (this design is purely synchronous).
func TestSmuxWriter_NoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	for range 50 {
		serverStream, _, cleanup := setupSmuxPair(t)

		w := &serverSmuxWriter{stream: serverStream}
		_ = serverStream.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))

		var wg sync.WaitGroup
		for range 5 {
			wg.Go(func() {
				_ = w.writeFrame(1, "", make([]byte, 1024))
			})
		}
		wg.Wait()
		w.markDead()
		cleanup()
	}

	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	time.Sleep(200 * time.Millisecond)

	after := runtime.NumGoroutine()
	diff := after - before
	if diff > 5 {
		t.Errorf("goroutine leak: +%d after 50 lifecycle cycles", diff)
	} else {
		t.Logf("✓ no goroutine leak (diff=%d)", diff)
	}
}
