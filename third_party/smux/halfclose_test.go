package smux

import (
	"bytes"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// Half-close tests pin k2 patch 2 (see K2_PATCHES.md). The payload is larger
// than initialPeerWindow so every write path below has to block on the
// peer's window at least once.
const halfCloseBig = 1 << 20

func halfClosePair(t *testing.T) (cs, ss *Session) {
	t.Helper()
	a, b := net.Pipe()
	cs, err := Client(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	ss, err = Server(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cs.Close()
		ss.Close()
	})
	return cs, ss
}

// acceptAfterFirstByte accepts a stream and reads the opener's first byte
// before returning it, so the tests below know the opener has started.
// (Before patch 5 this ordering was also required for correctness.)
func acceptAfterFirstByte(ss *Session) (*Stream, error) {
	st, err := ss.AcceptStream()
	if err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(st, make([]byte, 1)); err != nil {
		return nil, err
	}
	return st, nil
}

func halfClosePayload() []byte {
	return bytes.Repeat([]byte("0123456789abcdef"), halfCloseBig/16)
}

type readResult struct {
	data []byte
	err  error
}

func readAllAsync(r io.Reader) <-chan readResult {
	ch := make(chan readResult, 1)
	go func() {
		b, err := io.ReadAll(r)
		ch <- readResult{b, err}
	}()
	return ch
}

// The module is go1.13 (no generics), hence one waiter per channel type.
func waitTimeout(t *testing.T, what string) {
	t.Helper()
	t.Fatalf("%s: timed out", what)
}

func waitRead(t *testing.T, ch <-chan readResult, what string) readResult {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		waitTimeout(t, what)
		return readResult{}
	}
}

func waitErr(t *testing.T, ch <-chan error, what string) error {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		waitTimeout(t, what)
		return nil
	}
}

func waitStream(t *testing.T, ch <-chan *Stream, what string) *Stream {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		waitTimeout(t, what)
		return nil
	}
}

// The request/response shape: the opener sends a request, half-closes, and
// must still receive a response larger than one window. Upstream's only
// close was Close(), which deregisters the stream, so every byte of the
// response was dropped.
func TestHalfClose_ReadAfterCloseWrite(t *testing.T) {
	cs, ss := halfClosePair(t)
	cs.SetPeerHalfClose(true) // the client half-closes: the server says it can
	ss.SetPeerHalfClose(true) // the server writes after the client's FIN
	want := halfClosePayload()

	served := make(chan error, 1)
	go func() {
		st, err := ss.AcceptStream()
		if err != nil {
			served <- err
			return
		}
		req, err := io.ReadAll(st) // ends at the client's FIN
		if err != nil || string(req) != "req" {
			served <- io.ErrUnexpectedEOF
			return
		}
		if _, err := st.Write(want); err != nil {
			served <- err
			return
		}
		served <- st.Close()
	}()

	st, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Write([]byte("req")); err != nil {
		t.Fatal(err)
	}
	if err := st.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got := waitRead(t, readAllAsync(st), "read response after CloseWrite")
	if got.err != nil || !bytes.Equal(got.data, want) {
		t.Fatalf("response after CloseWrite: %d bytes, err=%v; want %d bytes", len(got.data), got.err, len(want))
	}
	if err := waitErr(t, served, "server"); err != nil {
		t.Fatalf("server: %v", err)
	}
	st.Close()
}

// The upload shape: the peer half-closes first and keeps reading. With the
// peer's half-close capability known, a FIN no longer ends our writes.
func TestHalfClose_WriteAfterPeerFIN(t *testing.T) {
	cs, ss := halfClosePair(t)
	cs.SetPeerHalfClose(true)
	ss.SetPeerHalfClose(true) // the server half-closes: the client says it can
	want := halfClosePayload()

	uploaded := make(chan readResult, 1)
	go func() {
		st, err := acceptAfterFirstByte(ss)
		if err != nil {
			uploaded <- readResult{nil, err}
			return
		}
		if err := st.CloseWrite(); err != nil {
			uploaded <- readResult{nil, err}
			return
		}
		b, err := io.ReadAll(st)
		uploaded <- readResult{b, err}
		st.Close()
	}()

	st, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	// Opener speaks first (K2_PATCHES.md contract), then waits for the
	// peer's FIN so the large write below starts after it.
	if _, err := st.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if got := waitRead(t, readAllAsync(st), "peer FIN"); got.err != nil || len(got.data) != 0 {
		t.Fatalf("read before upload: %d bytes, err=%v; want clean EOF", len(got.data), got.err)
	}
	n, err := st.Write(want)
	if err != nil || n != len(want) {
		t.Fatalf("write after peer FIN: n=%d err=%v; want %d, nil", n, err, len(want))
	}
	st.Close()
	got := waitRead(t, uploaded, "server upload read")
	if got.err != nil || !bytes.Equal(got.data, want) {
		t.Fatalf("server received %d bytes, err=%v; want %d", len(got.data), got.err, len(want))
	}
}

// A half-close-capable peer that then fully closes must still unblock our
// writer: its Close() sends the second FIN.
func TestHalfClose_PeerCloseUnblocksWriter(t *testing.T) {
	cs, ss := halfClosePair(t)
	cs.SetPeerHalfClose(true)

	go func() {
		st, err := acceptAfterFirstByte(ss)
		if err != nil {
			return
		}
		st.Close() // reads nothing more
	}()

	st, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	waitRead(t, readAllAsync(st), "peer FIN")
	wrote := make(chan error, 1)
	go func() {
		_, err := st.Write(halfClosePayload())
		wrote <- err
	}()
	if err := waitErr(t, wrote, "write to fully closed peer"); err != io.EOF {
		t.Fatalf("write to fully closed peer: err=%v; want io.EOF", err)
	}
	st.Close()
}

// Without the peer's capability (an old peer, whose single FIN means full
// close) a FIN still ends writes, exactly as upstream: waiting for window
// updates that an old peer will never send would hang the writer.
func TestHalfClose_UnknownPeerFINEndsWrites(t *testing.T) {
	cs, ss := halfClosePair(t)

	go func() {
		st, err := acceptAfterFirstByte(ss)
		if err != nil {
			return
		}
		st.CloseWrite() // FIN, and reads nothing more
	}()

	st, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	waitRead(t, readAllAsync(st), "peer FIN")
	wrote := make(chan error, 1)
	go func() {
		_, err := st.Write(halfClosePayload())
		wrote <- err
	}()
	if err := waitErr(t, wrote, "write after unknown peer's FIN"); err != io.EOF {
		t.Fatalf("write after unknown peer's FIN: err=%v; want io.EOF", err)
	}
	st.Close()
}

func TestHalfClose_WriteAfterCloseWriteFails(t *testing.T) {
	cs, ss := halfClosePair(t)
	cs.SetPeerHalfClose(true)
	go ss.AcceptStream()
	st, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if err := st.CloseWrite(); err != nil {
		t.Fatalf("second CloseWrite = %v; want nil (idempotent)", err)
	}
	if _, err := st.Write([]byte("late")); err != io.ErrClosedPipe {
		t.Fatalf("write after CloseWrite: err=%v; want io.ErrClosedPipe", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close after CloseWrite = %v", err)
	}
}

// Every stream ends with exactly two FINs on the wire: one for the write
// side, one for the read side, whichever order the closes come in.
func TestHalfClose_FINCount(t *testing.T) {
	for _, tc := range []struct {
		name  string
		close func(*Stream)
	}{
		{"CloseOnly", func(s *Stream) { s.Close() }},
		{"CloseWriteThenClose", func(s *Stream) { s.CloseWrite(); s.Close() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, ss := halfClosePair(t)
			accepted := make(chan *Stream, 1)
			go func() {
				st, _ := ss.AcceptStream()
				accepted <- st
			}()
			st, err := cs.OpenStream()
			if err != nil {
				t.Fatal(err)
			}
			peer := waitStream(t, accepted, "accept")
			tc.close(st)
			deadline := time.Now().Add(5 * time.Second)
			for atomic.LoadUint32(&peer.finsRecv) < 2 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			// Give a stray third FIN time to arrive before asserting.
			time.Sleep(20 * time.Millisecond)
			if got := atomic.LoadUint32(&peer.finsRecv); got != 2 {
				t.Fatalf("peer saw %d FINs; want 2", got)
			}
		})
	}
}

// A write that fits in the window must fail too once the peer has fully
// closed; otherwise it "succeeds" and the peer drops the bytes.
func TestHalfClose_SmallWriteToClosedPeerFails(t *testing.T) {
	cs, ss := halfClosePair(t)
	cs.SetPeerHalfClose(true)
	go func() {
		st, err := acceptAfterFirstByte(ss)
		if err != nil {
			return
		}
		st.Close()
	}()

	st, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadUint32(&st.finsRecv) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := st.Write([]byte("y")); err != io.EOF {
		t.Fatalf("small write to fully closed peer: err=%v; want io.EOF", err)
	}
	st.Close()
}

// A peer may send any number of FINs for a stream it keeps open. The count
// must saturate: wrapping back to 1 closed chFinEvent a second time, a
// panic inside recvLoop that killed the whole process.
func TestHalfClose_FINFloodDoesNotPanic(t *testing.T) {
	cs, ss := halfClosePair(t)
	accepted := make(chan *Stream, 1)
	go func() {
		st, _ := ss.AcceptStream()
		accepted <- st
	}()
	st, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	peer := waitStream(t, accepted, "accept")
	for i := 0; i < 100; i++ {
		if _, err := cs.writeFrame(newFrame(cmdFIN, st.id)); err != nil {
			t.Fatal(err)
		}
	}
	// A NOP round-trip after the flood proves recvLoop processed every FIN.
	if _, err := cs.writeFrame(newFrame(cmdNOP, 0)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadUint32(&peer.finsRecv) < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := atomic.LoadUint32(&peer.finsRecv); got != 3 {
		t.Fatalf("finsRecv = %d after 100 FINs; want saturated at 3", got)
	}
	if ss.IsClosed() {
		t.Fatal("server session died on a FIN flood")
	}
}

// The wrap itself, without sending 2^32 frames: a counter at the top of its
// range must not come round to "first FIN" again.
func TestHalfClose_FINCounterNeverWraps(t *testing.T) {
	cs, _ := halfClosePair(t)
	st := newStream(1, cs.config.MaxFrameSize, cs)
	st.fin()
	st.fin()
	atomic.StoreUint32(&st.finsRecv, ^uint32(0))
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("fin() panicked near counter wrap: %v", r)
		}
	}()
	st.fin()
	st.fin()
}

// io.WriterTo returns nil at EOF; io.Copy passes WriteTo's error through
// unchanged, so returning io.EOF made every clean FIN look like a failure.
func TestHalfClose_WriteToEndsWithNilAtFIN(t *testing.T) {
	cs, ss := halfClosePair(t)
	want := halfClosePayload()
	go func() {
		st, err := acceptAfterFirstByte(ss)
		if err != nil {
			return
		}
		st.Write(want)
		st.Close()
	}()
	st, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Write([]byte("x"))
	st.SetReadDeadline(time.Now().Add(5 * time.Second))
	var got bytes.Buffer
	if _, err := io.Copy(&got, st); err != nil || !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("io.Copy: %d bytes, err=%v; want %d, nil", got.Len(), err, len(want))
	}
}

// PeerClosed fires when the peer will read nothing more: its second FIN on
// a half-close session, its first otherwise.
func TestHalfClose_PeerClosed(t *testing.T) {
	for _, hc := range []bool{true, false} {
		cs, ss := halfClosePair(t)
		cs.SetPeerHalfClose(hc)
		ss.SetPeerHalfClose(hc)
		accepted := make(chan *Stream, 1)
		go func() {
			st, err := acceptAfterFirstByte(ss)
			if err == nil {
				accepted <- st
			}
		}()
		cst, err := cs.OpenStream()
		if err != nil {
			t.Fatal(err)
		}
		cst.Write([]byte("x"))
		sst := waitStream(t, accepted, "accept")

		cst.CloseWrite()
		select {
		case <-sst.PeerClosed():
			if hc {
				t.Fatal("half-close session: PeerClosed after CloseWrite alone")
			}
		case <-time.After(200 * time.Millisecond):
			if !hc {
				t.Fatal("legacy session: PeerClosed not fired by the peer's FIN")
			}
		}
		cst.Close()
		select {
		case <-sst.PeerClosed():
		case <-time.After(2 * time.Second):
			t.Fatalf("hc=%v: PeerClosed not fired after the peer's Close", hc)
		}
	}
}

// Half-closing toward a peer not known to support it (an old server, which
// reads our FIN as a full close and stops sending after its window) would
// leave a response cut at an arbitrary point that still ends in a clean EOF.
// CloseWrite is a full Close there, as before half-close existed: the
// stream is gone, and a read fails instead of returning part of the reply.
func TestHalfClose_CloseWriteToUnknownPeerClosesFully(t *testing.T) {
	cs, ss := halfClosePair(t) // neither side has heard the other can half-close
	go acceptAfterFirstByte(ss)
	st, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := st.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if n := cs.NumStreams(); n != 0 {
		t.Fatalf("stream still registered after CloseWrite to an unknown peer (%d streams)", n)
	}
	if _, err := st.Read(make([]byte, 1)); err == nil {
		t.Fatal("read after CloseWrite to an unknown peer succeeded; want the stream closed")
	}
}
