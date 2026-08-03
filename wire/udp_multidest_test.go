package wire

import (
	"bytes"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
)

// fakeDatagramSender captures frames written by quicUDPConn.
type fakeDatagramSender struct {
	sent [][]byte
}

func (f *fakeDatagramSender) SendDatagram(b []byte) error {
	cp := make([]byte, len(b))
	copy(cp, b)
	f.sent = append(f.sent, cp)
	return nil
}
func (f *fakeDatagramSender) LocalAddr() net.Addr { return &net.UDPAddr{} }

func newTestQUICUDPConn(sender datagramSender) *quicUDPConn {
	mux := &quicUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		done:        make(chan struct{}),
	}
	return &quicUDPConn{
		qconn:      sender,
		sessionID:  7,
		targetAddr: "1.1.1.1:443",
		mux:        mux,
		readCh:     make(chan *UDPFrame, 4),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
	}
}

func TestQUICUDPConn_WriteTo_PerPacketDest(t *testing.T) {
	sender := &fakeDatagramSender{}
	c := newTestQUICUDPConn(sender)

	// nil addr → session target
	if _, err := c.WriteTo([]byte("a"), nil); err != nil {
		t.Fatalf("WriteTo(nil): %v", err)
	}
	// explicit addr → that addr
	if _, err := c.WriteTo([]byte("b"), rawAddr("2.2.2.2:27015")); err != nil {
		t.Fatalf("WriteTo(addr): %v", err)
	}

	if len(sender.sent) != 2 {
		t.Fatalf("sent %d frames, want 2", len(sender.sent))
	}
	f0, err := ReadUDPFrame(newBytesReader(sender.sent[0]))
	if err != nil {
		t.Fatalf("parse frame 0: %v", err)
	}
	if f0.Addr != "1.1.1.1:443" {
		t.Errorf("frame 0 addr = %q, want 1.1.1.1:443", f0.Addr)
	}
	f1, err := ReadUDPFrame(newBytesReader(sender.sent[1]))
	if err != nil {
		t.Fatalf("parse frame 1: %v", err)
	}
	if f1.Addr != "2.2.2.2:27015" {
		t.Errorf("frame 1 addr = %q, want 2.2.2.2:27015", f1.Addr)
	}
}

func TestQUICUDPConn_ReadFrom_ReturnsFrameSource(t *testing.T) {
	c := newTestQUICUDPConn(&fakeDatagramSender{})

	// Frame carrying a real source (new server) → that source is returned.
	c.readCh <- &UDPFrame{SessionID: 7, Addr: "3.3.3.3:27015", Payload: []byte("x")}
	buf := make([]byte, 16)
	n, from, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if n != 1 || from.String() != "3.3.3.3:27015" {
		t.Fatalf("n=%d from=%v, want 1 / 3.3.3.3:27015", n, from)
	}

	// Frame with empty addr (old server) → falls back to session target.
	c.readCh <- &UDPFrame{SessionID: 7, Addr: "", Payload: []byte("y")}
	_, from2, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom 2: %v", err)
	}
	if from2.String() != "1.1.1.1:443" {
		t.Fatalf("from2 = %v, want 1.1.1.1:443", from2)
	}
}

func newBytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

func TestServerUDPConn_PerPacketAddrRoundTrip(t *testing.T) {
	sender := &fakeDatagramSender{}
	var inflight atomic.Int32
	c := &serverUDPConn{
		qconn:        sender,
		sessionID:    9,
		addr:         "1.1.1.1:443",
		log:          slog.Default(),
		readCh:       make(chan *UDPFrame, 4),
		done:         make(chan struct{}),
		sendInflight: &inflight,
	}

	// Uplink: a frame addressed to a second destination surfaces that dest.
	c.deliver(&UDPFrame{SessionID: 9, Addr: "2.2.2.2:27015", Payload: []byte("hi")})
	buf := make([]byte, 16)
	n, to, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if n != 2 || to.String() != "2.2.2.2:27015" {
		t.Fatalf("n=%d to=%v, want 2 / 2.2.2.2:27015", n, to)
	}

	// Uplink with empty addr (subsequent-packet optimization) → session addr.
	c.deliver(&UDPFrame{SessionID: 9, Addr: "", Payload: []byte("yo")})
	_, to2, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom 2: %v", err)
	}
	if to2.String() != "1.1.1.1:443" {
		t.Fatalf("to2 = %v, want 1.1.1.1:443", to2)
	}

	// Downlink: the real source is carried back in the frame addr.
	if _, err := c.WriteTo([]byte("pong"), rawAddr("3.3.3.3:27015")); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent %d frames, want 1", len(sender.sent))
	}
	f, err := ReadUDPFrame(newBytesReader(sender.sent[0]))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.Addr != "3.3.3.3:27015" {
		t.Errorf("downlink frame addr = %q, want 3.3.3.3:27015", f.Addr)
	}
}

func TestQUICUDPMux_CountsTxDrops(t *testing.T) {
	sender := &fakeDatagramSender{}
	c := newTestQUICUDPConn(sender)
	// Saturate the inflight budget so the next write is dropped.
	c.mux.sendInflight.Store(maxDatagramInflight)

	if _, err := c.WriteTo([]byte("x"), nil); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := c.mux.udpTxDrops.Load(); got != 1 {
		t.Fatalf("mux udpTxDrops = %d, want 1", got)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("sent %d frames, want 0 (dropped)", len(sender.sent))
	}
}
