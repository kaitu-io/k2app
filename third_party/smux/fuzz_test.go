package smux

import (
	"encoding/binary"
	"io"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// FuzzSession drives a server session with a script: arbitrary frames from
// the peer (well-formed or not) interleaved with local stream calls. Whatever
// the script, the session must not panic or hang, its receive accounting must
// stay in bounds (bucket <= MaxReceiveBuffer, grown >= 0), and closing the
// conn must end the session and every goroutine it started.
//
//	go test -run '^$' -fuzz FuzzSession -fuzztime 10m ./
func FuzzSession(f *testing.F) {
	f.Add([]byte{0, 0, 1, 2, 1, 40, 6, 0, 1, 1, 1, 1})
	f.Add([]byte{1, 0, 3, 2, 3, 255, 6, 1, 2, 3, 200, 6, 3, 1, 3, 1, 3})
	f.Add([]byte{0, 4, 1, 0xff, 0xff, 0xff, 0x7f, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{1, 5, 2, 9, 0, 0, 0, 0, 0, 0, 0, 6, 4, 6, 5, 7, 7})
	// unread data in a stall-mode stream, then the stream is closed locally
	f.Add([]byte{0, 1, 0, 1, 2, 1, 200, 7, 0, 7, 0, 7, 0, 7, 0, 6, 0, 4, 0})
	f.Fuzz(func(t *testing.T, script []byte) {
		if len(script) == 0 || len(script) > 4096 {
			return
		}
		baseline := runtime.NumGoroutine()
		cfg := DefaultConfig()
		cfg.KeepAliveInterval = 50 * time.Millisecond
		cfg.KeepAliveTimeout = 2 * time.Second
		cfg.MaxFrameSize = 4096
		cfg.MaxReceiveBuffer = 64 << 10
		cfg.MaxStreamBuffer = 16 << 10
		cfg.MinStreamBuffer = 2 << 10

		peer, conn := net.Pipe()
		sess, err := Server(conn, cfg)
		if err != nil {
			t.Fatal(err)
		}
		r := &scriptReader{b: script}
		if r.next()&1 == 1 {
			sess.SetPeerHalfClose(true)
		}

		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); io.Copy(io.Discard, peer) }()

		var mu sync.Mutex
		var local []*Stream
		stall := r.next()&1 == 1 // accepted streams are never read: backpressure path
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				st, err := sess.AcceptStream()
				if err != nil {
					return
				}
				mu.Lock()
				local = append(local, st)
				mu.Unlock()
				if !stall {
					wg.Add(1)
					go func() { defer wg.Done(); io.Copy(io.Discard, st) }()
				}
			}
		}()

		done := make(chan struct{})
		go func() {
			defer close(done)
			for !r.eof() {
				frame, localOp := r.step()
				if localOp >= 0 {
					mu.Lock()
					var st *Stream
					if len(local) > 0 {
						st = local[int(r.next())%len(local)]
					}
					mu.Unlock()
					runLocalOp(sess, st, localOp, &mu, &local)
					continue
				}
				if frame == nil {
					time.Sleep(time.Millisecond)
					continue
				}
				peer.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
				if _, err := peer.Write(frame); err != nil {
					return // backpressure or session gone: both legal
				}
				if !checkAccounting(t, sess) {
					return
				}
			}
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("script hung\n%s", goroutines())
		}
		checkAccounting(t, sess)

		peer.Close()
		deadline := time.Now().Add(3 * time.Second)
		for !sess.IsClosed() && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if !sess.IsClosed() {
			t.Fatalf("session open after its conn closed\n%s", goroutines())
		}
		mu.Lock()
		for _, st := range local {
			st.Close()
		}
		mu.Unlock()
		waited := make(chan struct{})
		go func() { wg.Wait(); close(waited) }()
		select {
		case <-waited:
		case <-time.After(3 * time.Second):
			t.Fatalf("readers did not exit after session close\n%s", goroutines())
		}
		for time.Now().Before(deadline.Add(2 * time.Second)) {
			if runtime.NumGoroutine() <= baseline {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("goroutines %d > baseline %d after close\n%s", runtime.NumGoroutine(), baseline, goroutines())
	})
}

// checkAccounting reports with Errorf: it runs on the script goroutine too.
// A violation must persist to count: recvLoop pushes a frame's bytes before
// it takes their tokens from the bucket, so a reader that consumes them in
// between briefly lifts the bucket past MaxReceiveBuffer — upstream's order,
// harmless, and gone a moment later. Tokens returned twice stay returned.
func checkAccounting(t *testing.T, s *Session) bool {
	t.Helper()
	var b int32
	var g int64
	for i := 0; i < 50; i++ {
		b, g = atomic.LoadInt32(&s.bucket), s.grown.Load()
		if b <= int32(s.config.MaxReceiveBuffer) && g >= 0 {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	if b > int32(s.config.MaxReceiveBuffer) {
		t.Errorf("bucket %d > MaxReceiveBuffer %d for 100 ms: tokens returned twice", b, s.config.MaxReceiveBuffer)
	} else {
		t.Errorf("grown %d < 0 for 100 ms: growth released twice", g)
	}
	return false
}

// runLocalOp performs one application call on the session or a stream, all
// bounded by deadlines so a call that never returns is a hang, not a slow test.
func runLocalOp(sess *Session, st *Stream, op int, mu *sync.Mutex, local *[]*Stream) {
	short := time.Now().Add(50 * time.Millisecond)
	switch op {
	case 0:
		if s, err := sess.OpenStream(); err == nil {
			mu.Lock()
			*local = append(*local, s)
			mu.Unlock()
		}
	case 1:
		if st != nil {
			st.SetWriteDeadline(short)
			st.Write(make([]byte, 3000))
		}
	case 2:
		if st != nil {
			st.SetReadDeadline(short)
			st.Read(make([]byte, 5000))
		}
	case 3:
		if st != nil {
			st.CloseWrite()
		}
	case 4:
		if st != nil {
			st.Close()
		}
	case 5:
		if st != nil {
			st.SetWriteDeadline(short)
			st.WriteTo(deadlineWriter{short})
		}
	}
}

// deadlineWriter discards writes until its deadline, then fails them, so a
// WriteTo into it ends.
type deadlineWriter struct{ until time.Time }

func (w deadlineWriter) Write(b []byte) (int, error) {
	if time.Now().After(w.until) {
		return 0, io.ErrClosedPipe
	}
	return len(b), nil
}

type scriptReader struct {
	b []byte
	i int
}

func (r *scriptReader) eof() bool { return r.i >= len(r.b) }

func (r *scriptReader) next() byte {
	if r.eof() {
		return 0
	}
	c := r.b[r.i]
	r.i++
	return c
}

func (r *scriptReader) u32() uint32 {
	return uint32(r.next()) | uint32(r.next())<<8 | uint32(r.next())<<16 | uint32(r.next())<<24
}

// step decodes the next action: a peer frame (localOp == -1), a local call
// (localOp >= 0), or a 1 ms pause (both zero values).
func (r *scriptReader) step() (frame []byte, localOp int) {
	op := r.next() % 8
	sid := uint32(r.next() % 8)
	switch op {
	case 0:
		return rawFrame(cmdSYN, sid, 0, nil), -1
	case 1:
		return rawFrame(cmdFIN, sid, 0, nil), -1
	case 2:
		n := int(r.next()) * 16
		return rawFrame(cmdPSH, sid, uint16(n), make([]byte, n)), -1
	case 3:
		return rawFrame(cmdNOP, 0, 0, nil), -1
	case 4:
		p := make([]byte, szCmdUPD)
		binary.LittleEndian.PutUint32(p, r.u32())
		binary.LittleEndian.PutUint32(p[4:], r.u32())
		return rawFrame(cmdUPD, sid, szCmdUPD, p), -1
	case 5: // arbitrary header: version, cmd and length straight from the script
		var h [headerSize]byte
		for i := range h {
			h[i] = r.next()
		}
		n := int(binary.LittleEndian.Uint16(h[2:]))
		if n > 512 {
			n = 512
		}
		return append(h[:], make([]byte, n)...), -1
	case 6:
		return nil, int(r.next() % 6)
	}
	return nil, -1
}

func goroutines() string {
	buf := make([]byte, 1<<20)
	return string(buf[:runtime.Stack(buf, true)])
}
