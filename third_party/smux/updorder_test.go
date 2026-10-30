package smux

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// An idle shrink (tuneLoop) and the reader both send window updates for the
// same stream. The peer keeps whichever update arrives last, so if the
// shrink's update — built before the reader consumed more — lands after the
// reader's, the peer's view of "consumed" goes backwards and a sender
// blocked on the window may never be released. Updates for a stream must
// therefore reach the wire with a non-decreasing consumed count.
func TestWindowUpdates_NeverGoBackwards(t *testing.T) {
	decided := make(chan struct{})
	readerSent := make(chan struct{})
	var once sync.Once
	testHookDecaySend = func() {
		once.Do(func() {
			close(decided)
			<-readerSent
		})
	}
	defer func() { testHookDecaySend = nil }()

	cfg := DefaultConfig()
	cfg.MaxReceiveBuffer = 1 << 20
	cfg.MaxStreamBuffer = 64 << 10
	cfg.MinStreamBuffer = 4 << 10
	peer, conn := net.Pipe()
	sess, err := Server(conn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	defer peer.Close()

	type upd struct{ consumed, window uint32 }
	var mu sync.Mutex
	var upds []upd
	go func() { // the peer: record every UPD for stream 1
		hdr := make([]byte, headerSize)
		for {
			if _, err := io.ReadFull(peer, hdr); err != nil {
				return
			}
			body := make([]byte, binary.LittleEndian.Uint16(hdr[2:]))
			if _, err := io.ReadFull(peer, body); err != nil {
				return
			}
			if hdr[1] == cmdUPD && binary.LittleEndian.Uint32(hdr[4:]) == 1 {
				mu.Lock()
				upds = append(upds, upd{binary.LittleEndian.Uint32(body), binary.LittleEndian.Uint32(body[4:])})
				mu.Unlock()
			}
		}
	}()
	send := func(b []byte) {
		if _, err := peer.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	send(rawFrame(cmdSYN, 1, 0, nil))
	st, err := sess.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	// Grow the window: the reader takes a whole window at once.
	send(rawFrame(cmdPSH, 1, 4<<10, make([]byte, 4<<10)))
	if _, err := io.ReadFull(st, make([]byte, 4<<10)); err != nil {
		t.Fatal(err)
	}
	// Then leave data unread so the stream goes idle and tuneLoop shrinks it.
	send(rawFrame(cmdPSH, 1, 6<<10, make([]byte, 6<<10)))
	select {
	case <-decided:
	case <-time.After(5 * time.Second):
		t.Fatal("idle shrink never decided")
	}
	// The reader wakes up between the shrink's decision and its send.
	if _, err := io.ReadFull(st, make([]byte, 6<<10)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond) // reader's update reaches the wire
	close(readerSent)
	time.Sleep(100 * time.Millisecond) // shrink's update reaches the wire

	mu.Lock()
	defer mu.Unlock()
	if len(upds) < 2 {
		t.Fatalf("want ≥2 window updates, got %v", upds)
	}
	for i := 1; i < len(upds); i++ {
		if upds[i].consumed < upds[i-1].consumed {
			t.Fatalf("window update %d went backwards: %+v after %+v (all: %+v)", i, upds[i], upds[i-1], upds)
		}
	}
}

// The consequence, end to end with a real sender: the stale update leaves
// the sender counting bytes the receiver has already read as still in
// flight. It is permanent when nothing else repairs it: the reader's catch-up
// read was between half a window and a window (enough for an update, not
// enough to grow the window again, so tuneLoop never fires again), and the
// sender pauses past tuneInterval (request/response, a websocket between
// messages). The sender's next write fills the shrunk window with what it
// thinks is in flight, the receiver acknowledges less than half a window,
// and no update ever comes while keepalive keeps the session up. A pool
// exhausted by other streams (iOS: 512 KB) removes the growth that could
// rescue it.
func TestWindowUpdates_StaleShrinkDoesNotStallSender(t *testing.T) {
	var armed atomic.Bool
	decided := make(chan struct{})
	readerSent := make(chan struct{})
	var once sync.Once
	testHookDecaySend = func() {
		if !armed.Load() {
			return
		}
		once.Do(func() {
			close(decided)
			<-readerSent
		})
	}
	defer func() { testHookDecaySend = nil }()

	cfg := DefaultConfig()
	cfg.KeepAliveInterval = 100 * time.Millisecond
	cfg.KeepAliveTimeout = 10 * time.Second
	cfg.MaxReceiveBuffer = 1 << 20
	cfg.MaxStreamBuffer = 64 << 10
	cfg.MinStreamBuffer = 4 << 10
	cs, ss := tunePair(t, cfg, cfg)

	sender, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sender.Write(make([]byte, 4<<10)); err != nil {
		t.Fatal(err)
	}
	recv, err := ss.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	// The receiver keeps up with a whole window: its window grows past min.
	if _, err := io.ReadFull(recv, make([]byte, 4<<10)); err != nil {
		t.Fatal(err)
	}
	if rcvWnd(recv) <= 4<<10 {
		t.Fatalf("window did not grow: %d", rcvWnd(recv))
	}

	// A burst the receiver leaves unread for a while; the sender then pauses.
	armed.Store(true)
	if _, err := sender.Write(make([]byte, 3<<10)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-decided: // tuneLoop shrank the idle window and is about to say so
	case <-time.After(5 * time.Second):
		t.Fatal("idle shrink never decided")
	}
	if _, err := io.ReadFull(recv, make([]byte, 3<<10)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond) // the reader's update reaches the sender
	close(readerSent)
	time.Sleep(1200 * time.Millisecond) // then the shrink's; the sender pauses past tuneInterval

	// The sender writes again; the receiver is reading.
	got := make(chan error, 1)
	go func() {
		recv.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, err := io.ReadFull(recv, make([]byte, 16<<10))
		got <- err
	}()
	sender.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := sender.Write(make([]byte, 16<<10)); err != nil {
		t.Fatalf("sender stuck after the receiver drained its buffer: %v", err)
	}
	if err := <-got; err != nil {
		t.Fatalf("receiver: %v", err)
	}
}

// updMu is what keeps a snapshot and its send together: the shrink has read
// consumed/window but not yet enqueued, and the reader consumes more and
// sends. Without updMu the reader's newer update goes out first and the
// shrink's older one lands last.
func TestWindowUpdates_SnapshotAndSendAreAtomic(t *testing.T) {
	var shrinking, armed atomic.Bool
	snapped := make(chan struct{})
	var once sync.Once
	testHookDecaySend = func() { shrinking.Store(armed.Load()) }
	testHookUpdSnapshot = func() {
		if !shrinking.Load() {
			return
		}
		once.Do(func() {
			shrinking.Store(false)
			close(snapped)
			time.Sleep(300 * time.Millisecond) // the reader gets every chance to overtake
		})
	}
	defer func() { testHookDecaySend, testHookUpdSnapshot = nil, nil }()

	cfg := DefaultConfig()
	cfg.MaxReceiveBuffer = 1 << 20
	cfg.MaxStreamBuffer = 64 << 10
	cfg.MinStreamBuffer = 4 << 10
	peer, conn := net.Pipe()
	sess, err := Server(conn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	defer peer.Close()
	var mu sync.Mutex
	var consumed []uint32
	go func() {
		hdr := make([]byte, headerSize)
		for {
			if _, err := io.ReadFull(peer, hdr); err != nil {
				return
			}
			body := make([]byte, binary.LittleEndian.Uint16(hdr[2:]))
			if _, err := io.ReadFull(peer, body); err != nil {
				return
			}
			if hdr[1] == cmdUPD && binary.LittleEndian.Uint32(hdr[4:]) == 1 {
				mu.Lock()
				consumed = append(consumed, binary.LittleEndian.Uint32(body))
				mu.Unlock()
			}
		}
	}()
	send := func(b []byte) {
		if _, err := peer.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	send(rawFrame(cmdSYN, 1, 0, nil))
	st, err := sess.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	send(rawFrame(cmdPSH, 1, 4<<10, make([]byte, 4<<10)))
	if _, err := io.ReadFull(st, make([]byte, 4<<10)); err != nil {
		t.Fatal(err)
	}
	armed.Store(true)
	send(rawFrame(cmdPSH, 1, 6<<10, make([]byte, 6<<10)))
	select {
	case <-snapped: // the shrink has read its values
	case <-time.After(5 * time.Second):
		t.Fatal("idle shrink never sent")
	}
	if _, err := io.ReadFull(st, make([]byte, 6<<10)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(consumed); i++ {
		if consumed[i] < consumed[i-1] {
			t.Fatalf("window update %d went backwards: consumed %v", i, consumed)
		}
	}
	if len(consumed) == 0 || consumed[len(consumed)-1] != 10<<10 {
		t.Fatalf("last update consumed=%v; want the reader's 10240 last", consumed)
	}
}
