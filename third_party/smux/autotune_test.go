package smux

import (
	"bytes"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// Auto-tuning tests pin k2 patch 6 (see K2_PATCHES.md).

func tuneConfig(minBuf, maxBuf, recvBuf int) *Config {
	cfg := DefaultConfig()
	cfg.KeepAliveInterval = 100 * time.Millisecond
	cfg.KeepAliveTimeout = 500 * time.Millisecond
	cfg.MaxReceiveBuffer = recvBuf
	cfg.MaxStreamBuffer = maxBuf
	cfg.MinStreamBuffer = minBuf
	return cfg
}

func tunePair(t *testing.T, client, server *Config) (cs, ss *Session) {
	t.Helper()
	a, b := net.Pipe()
	cs, err := Client(a, client)
	if err != nil {
		t.Fatal(err)
	}
	ss, err = Server(b, server)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close(); ss.Close() })
	return cs, ss
}

func rcvWnd(s *Stream) uint32 {
	s.bufferLock.Lock()
	defer s.bufferLock.Unlock()
	return s.rcvWnd
}

// serveDownloads makes every accepted stream send `size` bytes after the
// opener's first byte, then close.
func serveDownloads(ss *Session, size int) {
	go func() {
		for {
			st, err := ss.AcceptStream()
			if err != nil {
				return
			}
			go func() {
				if _, err := io.ReadFull(st, make([]byte, 1)); err != nil {
					return
				}
				st.Write(make([]byte, size))
				st.Close()
			}()
		}
	}()
}

// The failure this patch exists for: with fixed 64 KB windows and a 256 KB
// session buffer (k2's old TCP-WS config), four streams whose reader has
// stalled filled the buffer, recvLoop stopped, every other stream froze and
// keepalive then killed the session. With auto-tuning a stalled stream
// holds only its starting window, so dozens of them leave the rest working.
func TestAutoTune_StalledStreamsDoNotFreezeSession(t *testing.T) {
	cfg := tuneConfig(16<<10, 512<<10, 1<<20) // k2's iOS budget
	cs, ss := tunePair(t, cfg, cfg)
	serveDownloads(ss, 1<<20)

	const stalled = 40
	for i := 0; i < stalled; i++ {
		st, err := cs.OpenStream()
		if err != nil {
			t.Fatal(err)
		}
		st.Write([]byte("x")) // and never read
	}
	time.Sleep(200 * time.Millisecond)

	// Past several keepalive timeouts, a fresh stream still gets its whole
	// download and the session is alive.
	for round := 0; round < 3; round++ {
		st, err := cs.OpenStream()
		if err != nil {
			t.Fatalf("round %d: OpenStream: %v", round, err)
		}
		st.Write([]byte("x"))
		st.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := io.Copy(io.Discard, st)
		st.Close()
		if err != nil || n != 1<<20 {
			t.Fatalf("round %d: healthy stream got %d bytes, err=%v; want %d", round, n, err, 1<<20)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if cs.IsClosed() || ss.IsClosed() {
		t.Fatal("session died with stalled streams")
	}
	if b := atomic.LoadInt32(&cs.bucket); b <= 0 {
		t.Fatalf("receive bucket exhausted (%d) by stalled streams", b)
	}
}

// A reader that keeps up grows its window to MaxStreamBuffer; one that
// never reads stays at MinStreamBuffer.
func TestAutoTune_FastReaderGrowsStalledDoesNot(t *testing.T) {
	cfg := tuneConfig(16<<10, 512<<10, 4<<20)
	cs, ss := tunePair(t, cfg, cfg)
	serveDownloads(ss, 8<<20)

	stalled, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	stalled.Write([]byte("x"))

	fast, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	fast.Write([]byte("x"))
	fast.SetReadDeadline(time.Now().Add(10 * time.Second))
	if n, err := io.Copy(io.Discard, fast); err != nil || n != 8<<20 {
		t.Fatalf("fast stream: %d bytes, err=%v", n, err)
	}
	if w := rcvWnd(fast); w != 512<<10 {
		t.Fatalf("fast reader's window = %d; want MaxStreamBuffer %d", w, 512<<10)
	}
	if w := rcvWnd(stalled); w != 16<<10 {
		t.Fatalf("stalled reader's window = %d; want MinStreamBuffer %d", w, 16<<10)
	}
}

// Growth across all streams never exceeds half the session buffer, and a
// closed stream gives its growth back.
func TestAutoTune_GrowthPoolCappedAndReleased(t *testing.T) {
	cfg := tuneConfig(16<<10, 512<<10, 1<<20) // pool = 512 KB
	cs, ss := tunePair(t, cfg, cfg)
	serveDownloads(ss, 4<<20)

	const n = 6
	streams := make([]*Stream, n)
	done := make(chan error, n)
	for i := range streams {
		st, err := cs.OpenStream()
		if err != nil {
			t.Fatal(err)
		}
		streams[i] = st
		st.Write([]byte("x"))
		go func() {
			st.SetReadDeadline(time.Now().Add(10 * time.Second))
			_, err := io.Copy(io.Discard, st)
			done <- err
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	var sum uint32
	for _, st := range streams {
		sum += rcvWnd(st) - 16<<10
	}
	pool := int64(cfg.MaxReceiveBuffer / 2) // the documented cap
	if g := atomic.LoadInt64(&cs.grown); g != int64(sum) || g > pool {
		t.Fatalf("grown = %d, sum of stream growth = %d, cap = %d", g, sum, pool)
	}
	if sum == 0 {
		t.Fatal("no stream grew; the test did not exercise the pool")
	}
	for _, st := range streams {
		st.Close()
	}
	if g := atomic.LoadInt64(&cs.grown); g != 0 {
		t.Fatalf("grown = %d after all streams closed; want 0", g)
	}
}

// Window updates are receiver-local: an auto-tuning side works against a
// peer with the fixed-window config, in both directions.
func TestAutoTune_InteropWithFixedWindowPeer(t *testing.T) {
	tuned := tuneConfig(16<<10, 512<<10, 1<<20)
	fixed := DefaultConfig()
	for _, tc := range []struct {
		name           string
		client, server *Config
	}{
		{"TunedClient", tuned, fixed},
		{"TunedServer", fixed, tuned},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, ss := tunePair(t, tc.client, tc.server)
			cs.SetPeerHalfClose(true) // the echo keeps writing after our CloseWrite
			ss.SetPeerHalfClose(true)
			want := bytes.Repeat([]byte("0123456789abcdef"), (2<<20)/16)
			go func() {
				st, err := ss.AcceptStream()
				if err != nil {
					return
				}
				io.Copy(st, st) // echo, both directions under flow control
				st.Close()
			}()
			st, err := cs.OpenStream()
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				st.Write(want)
				st.CloseWrite()
			}()
			st.SetReadDeadline(time.Now().Add(10 * time.Second))
			got, err := io.ReadAll(st)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("echo: %d bytes, err=%v; want %d", len(got), err, len(want))
			}
		})
	}
}

func bufferedLen(s *Stream) int {
	s.bufferLock.Lock()
	defer s.bufferLock.Unlock()
	return int(s.buffered)
}

func charged(s *Stream) uint32 {
	s.bufferLock.Lock()
	defer s.bufferLock.Unlock()
	return s.charged
}

// downloadAndPause reads `size` bytes from a fresh download stream as fast
// as it can, then leaves the stream open and unread.
func downloadAndPause(t *testing.T, cs *Session, size int) *Stream {
	t.Helper()
	st, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	st.Write([]byte("x"))
	st.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(st, make([]byte, size)); err != nil {
		t.Fatal(err)
	}
	return st
}

// A stream that grew and then went idle (a paused video, a kept-alive
// HTTP/2 conn) used to keep its growth until it closed, so on the iOS
// budget one such stream left every new stream at MinStreamBuffer. Now an
// idle stream shrinks back after tuneInterval and the next stream grows.
func TestAutoTune_IdleStreamGivesGrowthBack(t *testing.T) {
	cfg := tuneConfig(16<<10, 1<<20, 1<<20) // iOS-like: pool 512 KB
	cfg.KeepAliveInterval = time.Second
	cfg.KeepAliveTimeout = 10 * time.Second
	cs, ss := tunePair(t, cfg, cfg)
	serveDownloads(ss, 4<<20)

	a := downloadAndPause(t, cs, 4<<20)
	if w := rcvWnd(a); w <= 16<<10 {
		t.Fatalf("stream A did not grow (window %d)", w)
	}
	deadline := time.Now().Add(3 * tuneInterval)
	for rcvWnd(a) > 16<<10 || atomic.LoadInt64(&cs.grown) != int64(charged(a)) {
		if time.Now().After(deadline) {
			t.Fatalf("idle stream A kept window %d, session grown %d", rcvWnd(a), atomic.LoadInt64(&cs.grown))
		}
		time.Sleep(50 * time.Millisecond)
	}
	// A's peer may still have delivered bytes on the old window; reading
	// them gives the rest of the charge back.
	a.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	io.ReadFull(a, make([]byte, bufferedLen(a)))
	if g := atomic.LoadInt64(&cs.grown); g != int64(charged(a)) {
		t.Fatalf("grown = %d, A charged %d", g, charged(a))
	}

	b := downloadAndPause(t, cs, 4<<20)
	want := uint32(16<<10) + uint32(cfg.MaxReceiveBuffer/4)
	if w := rcvWnd(b); w != want {
		t.Fatalf("stream B window = %d after A went idle; want the per-stream ceiling %d", w, want)
	}
}

// One stream grows to at most MinStreamBuffer + half the pool, so a single
// fast stream leaves room for another to grow.
func TestAutoTune_PerStreamCeiling(t *testing.T) {
	cfg := tuneConfig(16<<10, 1<<20, 1<<20) // ceiling 272 KB < MaxStreamBuffer
	cs, ss := tunePair(t, cfg, cfg)
	serveDownloads(ss, 8<<20)

	ceiling := uint32(16<<10) + uint32(cfg.MaxReceiveBuffer/4)
	a := downloadAndPause(t, cs, 8<<20)
	b := downloadAndPause(t, cs, 8<<20)
	if wa, wb := rcvWnd(a), rcvWnd(b); wa != ceiling || wb != ceiling {
		t.Fatalf("windows %d and %d; want both at the ceiling %d", wa, wb, ceiling)
	}
}

// After a window shrinks, the sender may still deliver what the old
// window allowed. Those bytes are charged while buffered, even past the
// pool, and the charge goes when they are read.
func TestAutoTune_BytesPastShrunkWindowAreCharged(t *testing.T) {
	cfg := tuneConfig(16<<10, 512<<10, 1<<20)
	cs, ss := tunePair(t, cfg, cfg)
	serveDownloads(ss, 4<<20) // the peer is done: no bytes in flight

	a := downloadAndPause(t, cs, 4<<20)
	if _, _, ok := a.decayIfIdle(time.Now().Add(time.Hour)); !ok {
		t.Fatal("decayIfIdle did not shrink a grown window")
	}
	atomic.StoreInt64(&cs.grown, cs.growPool) // pool exhausted by others
	base := charged(a)
	a.pushBytes(make([]byte, 64<<10))
	held := bufferedLen(a)
	wantCharge := uint32(held) - 16<<10
	if uint32(held) <= 16<<10 {
		wantCharge = 0
	}
	if c := charged(a); c != wantCharge {
		t.Fatalf("charged %d with %d bytes buffered; want %d", c, held, wantCharge)
	}
	if g := atomic.LoadInt64(&cs.grown); g != cs.growPool+int64(wantCharge)-int64(base) {
		t.Fatalf("grown = %d; want pool %d + %d - %d", g, cs.growPool, wantCharge, base)
	}
	a.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	io.ReadFull(a, make([]byte, held))
	if c := charged(a); c != 0 {
		t.Fatalf("charged %d after the buffer was read; want 0", c)
	}
}

// The acceptor's initial window update goes out on the stream's first
// Read/Write, not inside AcceptStream: an accept loop must not wait on a
// slow conn, or one stuck write stalls every new stream on the session.
func TestAutoTune_AcceptDoesNotWaitOnConn(t *testing.T) {
	cfg := tuneConfig(16<<10, 512<<10, 1<<20)
	cfg.KeepAliveInterval = 10 * time.Second
	cfg.KeepAliveTimeout = 30 * time.Second
	a, b := net.Pipe() // nobody reads a: every write by ss blocks
	ss, err := Server(b, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close(); a.Close() })

	const n = 5
	go func() {
		for i := uint32(0); i < n; i++ {
			a.Write(rawFrame(cmdSYN, 2*i+1, 0, nil))
		}
	}()
	for i := 0; i < n; i++ {
		ss.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := ss.AcceptStream(); err != nil {
			t.Fatalf("accept %d with the conn's writes blocked: %v", i, err)
		}
	}
}
