package core

import (
	"net"
	"sync"
	"testing"
	"time"
)

// recordingPacketConn records every WriteTo destination.
type recordingPacketConn struct {
	mu       sync.Mutex
	writes   []string
	readCh   chan *testPacket
	done     chan struct{}
	closeOne sync.Once
}

type testPacket struct {
	payload []byte
	addr    net.Addr
}

type testAddr string

func (a testAddr) Network() string { return "udp" }
func (a testAddr) String() string  { return string(a) }

func newRecordingPacketConn() *recordingPacketConn {
	return &recordingPacketConn{
		readCh: make(chan *testPacket, 8),
		done:   make(chan struct{}),
	}
}

func (c *recordingPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case p := <-c.readCh:
		return copy(b, p.payload), p.addr, nil
	case <-c.done:
		return 0, nil, net.ErrClosed
	}
}

func (c *recordingPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	c.mu.Lock()
	s := "<nil>"
	if addr != nil {
		s = addr.String()
	}
	c.writes = append(c.writes, s)
	c.mu.Unlock()
	return len(b), nil
}

func (c *recordingPacketConn) Close() error {
	c.closeOne.Do(func() { close(c.done) })
	return nil
}
func (c *recordingPacketConn) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (c *recordingPacketConn) SetDeadline(time.Time) error      { return nil }
func (c *recordingPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (c *recordingPacketConn) SetWriteDeadline(time.Time) error { return nil }

func (c *recordingPacketConn) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.writes))
	copy(out, c.writes)
	return out
}

func TestPipePacket_SecondDestinationIsForwarded(t *testing.T) {
	local := newRecordingPacketConn()
	remote := newRecordingPacketConn()

	// App sends to the primary dest, then to a second dest from the same port.
	local.readCh <- &testPacket{payload: []byte("one"), addr: testAddr("1.1.1.1:443")}
	local.readCh <- &testPacket{payload: []byte("two"), addr: testAddr("2.2.2.2:27015")}

	done := make(chan struct{})
	go func() {
		pipePacket(local, remote, nil, nil, nil)
		close(done)
	}()

	deadline := time.After(3 * time.Second)
	for {
		if w := remote.snapshot(); len(w) >= 2 {
			// Primary keeps the session label (nil → wire conn uses targetAddr);
			// the second destination is forwarded verbatim.
			if w[0] != "<nil>" {
				t.Errorf("first write dest = %q, want <nil> (session target)", w[0])
			}
			if w[1] != "2.2.2.2:27015" {
				t.Errorf("second write dest = %q, want 2.2.2.2:27015", w[1])
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timeout, writes = %v", remote.snapshot())
		case <-time.After(10 * time.Millisecond):
		}
	}
	local.Close()
	remote.Close()
	<-done
}
