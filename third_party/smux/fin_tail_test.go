package smux

import (
	"io"
	"net"
	"testing"
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
	if err := s.waitRead(); err != io.EOF {
		t.Fatalf("drained stream after FIN: waitRead = %v, want io.EOF", err)
	}
}
