package server

import (
	"net"
	"testing"
	"time"
)

// fakeUDPSession is a net.PacketConn standing in for a wire UDP session:
// uplink frames are fed via feed(), downlink frames land in out.
type fakeUDPSession struct {
	in   chan *fakePacket
	out  chan *fakePacket
	done chan struct{}
}

type fakePacket struct {
	payload []byte
	addr    net.Addr
}

type strAddr string

func (a strAddr) Network() string { return "udp" }
func (a strAddr) String() string  { return string(a) }

func newFakeUDPSession() *fakeUDPSession {
	return &fakeUDPSession{
		in:   make(chan *fakePacket, 8),
		out:  make(chan *fakePacket, 8),
		done: make(chan struct{}),
	}
}

func (s *fakeUDPSession) feed(payload string, addr string) {
	s.in <- &fakePacket{payload: []byte(payload), addr: strAddr(addr)}
}

func (s *fakeUDPSession) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case p := <-s.in:
		return copy(b, p.payload), p.addr, nil
	case <-s.done:
		return 0, nil, net.ErrClosed
	}
}

func (s *fakeUDPSession) WriteTo(b []byte, addr net.Addr) (int, error) {
	cp := make([]byte, len(b))
	copy(cp, b)
	select {
	case s.out <- &fakePacket{payload: cp, addr: addr}:
	default:
	}
	return len(b), nil
}

func (s *fakeUDPSession) Close() error {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	return nil
}
func (s *fakeUDPSession) LocalAddr() net.Addr                { return &net.UDPAddr{} }
func (s *fakeUDPSession) SetDeadline(time.Time) error        { return nil }
func (s *fakeUDPSession) SetReadDeadline(time.Time) error    { return nil }
func (s *fakeUDPSession) SetWriteDeadline(t time.Time) error { return nil }

// echoUDPServer starts a UDP echo listener and returns its addr + stop func.
func echoUDPServer(t *testing.T, tag string) (string, func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			pc.WriteTo(append([]byte(tag), buf[:n]...), from)
		}
	}()
	return pc.LocalAddr().String(), func() { pc.Close() }
}

// newTestProxyHandler builds a ProxyHandler that HandleUDP can actually run
// against: the zero-value literal `&ProxyHandler{AllowPrivateIP: true}` panics
// inside h.conns.track (nil map write) and would immediately bounce every
// session at the accepting gate (zero-value atomic.Bool is false). Route
// through the real constructor and only flip the one field the test needs.
func newTestProxyHandler() *ProxyHandler {
	h := NewProxyHandler()
	h.AllowPrivateIP = true
	return h
}

func TestHandleUDP_MultipleDestinationsOneSession(t *testing.T) {
	addrA, stopA := echoUDPServer(t, "A")
	defer stopA()
	addrB, stopB := echoUDPServer(t, "B")
	defer stopB()

	h := newTestProxyHandler()
	sess := newFakeUDPSession()

	go h.HandleUDP(sess, addrA)

	sess.feed("one", addrA)
	sess.feed("two", addrB)

	seen := map[string]bool{}
	deadline := time.After(3 * time.Second)
	for len(seen) < 2 {
		select {
		case p := <-sess.out:
			seen[string(p.payload)] = true
		case <-deadline:
			t.Fatalf("timeout, saw %v", seen)
		}
	}
	if !seen["Aone"] {
		t.Errorf("missing echo from A, saw %v", seen)
	}
	if !seen["Btwo"] {
		t.Errorf("missing echo from B, saw %v", seen)
	}
	sess.Close()
}

func TestHandleUDP_DownlinkCarriesRealSource(t *testing.T) {
	addrA, stopA := echoUDPServer(t, "A")
	defer stopA()

	h := newTestProxyHandler()
	sess := newFakeUDPSession()
	go h.HandleUDP(sess, addrA)
	sess.feed("ping", addrA)

	select {
	case p := <-sess.out:
		if p.addr == nil {
			t.Fatal("downlink addr is nil, want the echo server address")
		}
		if p.addr.String() != addrA {
			t.Fatalf("downlink addr = %v, want %v", p.addr, addrA)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for echo")
	}
	sess.Close()
}
