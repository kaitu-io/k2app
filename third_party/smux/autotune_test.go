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
