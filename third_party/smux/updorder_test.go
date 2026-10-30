package smux

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
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
