package smux

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// TestWaitReadFinDoesNotDropBufferedData pins the k2 patch to waitRead (see
// K2_PATCHES.md). When a read event and FIN are both pending, select picks
// either case at random; upstream v2.1.0 then returned io.EOF with the peer's
// last bytes still buffered, so the caller stopped reading and lost them.
// Each iteration makes both cases ready at once, so the unpatched code fails
// this test with probability 1-2^-100.
func TestWaitReadFinDoesNotDropBufferedData(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	cs, err := Client(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	ss, err := Server(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	go ss.AcceptStream()
	s, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}

	s.fin() // FIN stays ready for every iteration below
	buf := make([]byte, 8)
	for i := 0; i < 100; i++ {
		s.pushBytes([]byte("tail"))
		s.notifyReadEvent()
		if err := s.waitRead(); err != nil {
			t.Fatalf("iteration %d: waitRead = %v with data still buffered", i, err)
		}
		if n, err := s.TryRead(buf); n != 4 || err != nil {
			t.Fatalf("iteration %d: TryRead = %d, %v; want the 4 buffered bytes", i, n, err)
		}
	}
	// An iteration whose select took the FIN case leaves its read event
	// pending, so the first wait after draining may be that stale wakeup
	// (Read just retries); the one after it must be EOF.
	err = s.waitRead()
	if err == nil {
		err = s.waitRead()
	}
	if err != io.EOF {
		t.Fatalf("drained stream after FIN: waitRead = %v, want io.EOF", err)
	}
}

// TestReadAfterFinDeliversBufferedData guards the public Read around the
// patch: with FIN signalled and stale read events left behind, Read still
// hands back buffered bytes and, once drained, returns io.EOF instead of
// spinning or blocking. It does not discriminate the dropped-tail bug itself —
// that window sits between TryRead and waitRead, which only the waitRead-level
// test above can hit deterministically (this one passes on upstream v2.1.0).
func TestReadAfterFinDeliversBufferedData(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	cs, err := Client(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	ss, err := Server(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	go ss.AcceptStream()
	s, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}

	s.fin()
	buf := make([]byte, 8)
	for i := 0; i < 1000; i++ {
		s.pushBytes([]byte("tail"))
		s.notifyReadEvent()
		if n, err := s.Read(buf); n != 4 || err != nil {
			t.Fatalf("iteration %d: Read = %d, %v; want the 4 buffered bytes", i, n, err)
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.Read(buf)
		done <- err
	}()
	select {
	case err := <-done:
		if err != io.EOF {
			t.Fatalf("drained stream after FIN: Read = %v, want io.EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("drained stream after FIN: Read blocked instead of returning io.EOF")
	}
}

// TestWriteThenCloseDeliversEverything is an end-to-end smoke test of the shape
// that lost data in production: the peer writes and closes immediately, the
// reader drains with io.ReadAll over a real session and must see every byte.
// The race window is narrow, so this is not a discriminator for the patch
// (upstream v2.1.0 usually passes it); the waitRead-level test is.
//
// The acceptor reads the opener's first byte before replying, as k2's server
// does (ReadStreamHeader comes first). smux registers an opened stream only
// after its SYN is on the wire, so a peer that writes before the opener speaks
// can race that registration and have its frames dropped — see K2_PATCHES.md.
func TestWriteThenCloseDeliversEverything(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	cs, err := Client(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	ss, err := Server(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()

	payload := bytes.Repeat([]byte("k2-tail-"), 512)
	for i := 0; i < 2000; i++ {
		go func() {
			st, err := ss.AcceptStream()
			if err != nil {
				return
			}
			var first [1]byte
			if _, err := io.ReadFull(st, first[:]); err != nil {
				st.Close()
				return
			}
			st.Write(payload)
			st.Close()
		}()
		st, err := cs.OpenStream()
		if err != nil {
			t.Fatal(err)
		}
		st.Write([]byte{0}) // make the stream visible to the acceptor
		got, err := io.ReadAll(st)
		st.Close()
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("iteration %d: ReadAll = %d bytes, %v; want %d bytes", i, len(got), err, len(payload))
		}
	}
}
