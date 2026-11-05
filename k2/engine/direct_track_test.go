package engine

import (
	"context"
	"net"
	"net/netip"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/wire"
)

// trackInnerDialer is a wire.DirectDialer that does NOT implement wire.DialerController.
type trackInnerDialer struct{}

func (trackInnerDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, addr)
}
func (trackInnerDialer) ListenPacket(network, addr string) (net.PacketConn, error) {
	return net.ListenPacket(network, addr)
}

// trackControlDialer is a wire.DirectDialer that also implements wire.DialerController
// (the Android protect / gateway SO_MARK shape).
type trackControlDialer struct {
	trackInnerDialer
	called *bool
}

func (d trackControlDialer) ControlFunc() func(network, address string, c syscall.RawConn) error {
	return func(string, string, syscall.RawConn) error {
		*d.called = true
		return nil
	}
}

func TestTrackingDialer_RecordsSuccessOnly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		inner wire.DirectDialer
	}{
		{"inner", trackInnerDialer{}},
		{"nilInner", nil}, // engine passes cfg.DirectDialer, which may be nil
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newTrackingDialer(tc.inner)
			d.isPublic = func(netip.Addr) bool { return true } // loopback listener stands in for a public host
			if !d.LastOK().IsZero() {
				t.Fatalf("fresh tracker LastOK = %v, want zero", d.LastOK())
			}

			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()

			before := time.Now()
			c, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
			if err != nil {
				t.Fatalf("dial listening port: %v", err)
			}
			c.Close()
			ok := d.LastOK()
			if ok.IsZero() || ok.Before(before) {
				t.Fatalf("after successful dial LastOK = %v, want >= %v", ok, before)
			}

			// A port that was listening and is now closed: dial is refused.
			ln2, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			closedAddr := ln2.Addr().String()
			ln2.Close()
			if c, err := d.DialContext(context.Background(), "tcp", closedAddr); err == nil {
				c.Close()
				t.Fatalf("dial to closed port %s unexpectedly succeeded", closedAddr)
			}
			if got := d.LastOK(); !got.Equal(ok) {
				t.Fatalf("failed dial moved LastOK %v -> %v; only successes are evidence", ok, got)
			}

			// ListenPacket passes through and is not evidence.
			pc, err := d.ListenPacket("udp4", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("ListenPacket: %v", err)
			}
			pc.Close()
			if got := d.LastOK(); !got.Equal(ok) {
				t.Fatalf("ListenPacket moved LastOK %v -> %v; it proves nothing about the network", ok, got)
			}
		})
	}
}

func TestTrackingDialer_PassesDialerController(t *testing.T) {
	called := false
	var d wire.DirectDialer = newTrackingDialer(trackControlDialer{called: &called})
	dc, ok := d.(wire.DialerController)
	if !ok {
		t.Fatal("trackingDialer over a DialerController inner must implement wire.DialerController")
	}
	fn := dc.ControlFunc()
	if fn == nil {
		t.Fatal("ControlFunc() = nil, want the inner dialer's control func")
	}
	_ = fn("tcp", "1.2.3.4:443", nil)
	if !called {
		t.Fatal("ControlFunc() did not pass through to the inner dialer's control func")
	}

	// Non-controller inner: no control func to hand out (net.Dialer treats nil
	// Control as "none", same as the raw dialer).
	if dc, ok := any(newTrackingDialer(trackInnerDialer{})).(wire.DialerController); ok && dc.ControlFunc() != nil {
		t.Fatal("non-controller inner: ControlFunc() must be nil")
	}
}

// capturingProvider records the ConnectionHandler Start hands it, so a test
// can inject a proxied connection into the real tunnel.
type capturingProvider struct {
	h chan provider.ConnectionHandler
}

func (p *capturingProvider) Start(ctx context.Context, h provider.ConnectionHandler) error {
	p.h <- h
	return nil
}
func (p *capturingProvider) Close() error { return nil }
func (p *capturingProvider) Name() string { return "capturing-test" }

// countingDialer is a raw DirectDialer that counts dials, to prove the
// tracker still dials through the platform dialer (Android protect etc.).
type countingDialer struct {
	trackInnerDialer
	n atomic.Int32
}

func (d *countingDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d.n.Add(1)
	return d.trackInnerDialer.DialContext(ctx, network, addr)
}

// TestStart_DirectExitTracked drives the real Start (proxy mode, direct-only
// route) and routes one connection through the core direct exit: the engine's
// directTrack must record it, and the dial must still go through
// cfg.DirectDialer. Guards the Start wiring, which the unit tests above can't see.
func TestStart_DirectExitTracked(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan struct{}, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- struct{}{}
			c.Close()
		}
	}()

	prov := &capturingProvider{h: make(chan provider.ConnectionHandler, 1)}
	raw := &countingDialer{}
	e := New()
	if err := e.Start(Config{
		Client: &config.ClientConfig{
			Mode:   "proxy",
			Routes: []config.RouteConfig{{Via: "direct", Match: rule.MatchConfig{All: true}}},
		},
		FileDescriptor: -1,
		Provider:       prov,
		DirectDialer:   raw,
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop()

	e.mu.Lock()
	track := e.directTrack
	e.mu.Unlock()
	if track == nil {
		t.Fatal("Start did not install directTrack")
	}
	if !track.LastOK().IsZero() {
		t.Fatal("LastOK non-zero before any direct traffic")
	}
	// The listener is loopback; let it stand in for a public host. Written
	// before HandleTCP's goroutine starts (go statement = happens-before).
	track.isPublic = func(netip.Addr) bool { return true }

	h := <-prov.h
	client, server := net.Pipe()
	defer client.Close()
	go h.HandleTCP(server, ln.Addr().String())

	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("direct connection never reached the listener")
	}
	deadline := time.Now().Add(5 * time.Second)
	for track.LastOK().IsZero() {
		if time.Now().After(deadline) {
			t.Fatal("direct exit dial succeeded but directTrack.LastOK stayed zero — core direct outbound is not wired to the tracker")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if raw.n.Load() == 0 {
		t.Fatal("direct dial bypassed cfg.DirectDialer — the tracker must wrap, not replace, the platform dialer")
	}
}

// nilAddrConn is a conn whose RemoteAddr is nil.
type nilAddrConn struct{ net.Conn }

func (nilAddrConn) RemoteAddr() net.Addr { return nil }

type nilAddrDialer struct{ trackInnerDialer }

func (nilAddrDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	a, b := net.Pipe()
	b.Close()
	return nilAddrConn{a}, nil
}

// pipeDialer returns a net.Pipe conn, whose RemoteAddr ("pipe") is not an IP.
type pipeDialer struct{ trackInnerDialer }

func (pipeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	a, b := net.Pipe()
	b.Close()
	return a, nil
}

// TestTrackingDialer_IgnoresPrivateDestinations: LAN traffic keeps working
// when the internet is down, so only public destinations are evidence.
func TestTrackingDialer_IgnoresPrivateDestinations(t *testing.T) {
	// Real predicate: a successful loopback dial must not count.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	d := newTrackingDialer(trackInnerDialer{})
	c, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial loopback: %v", err)
	}
	c.Close()
	if !d.LastOK().IsZero() {
		t.Fatal("loopback dial counted as direct-internet evidence")
	}

	// Unparseable RemoteAddr: not counted even if the predicate says yes.
	pd := newTrackingDialer(pipeDialer{})
	pd.isPublic = func(netip.Addr) bool { return true }
	pc, err := pd.DialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	pc.Close()
	if !pd.LastOK().IsZero() {
		t.Fatal("conn with non-IP RemoteAddr counted as evidence")
	}

	nd := newTrackingDialer(nilAddrDialer{})
	nd.isPublic = func(netip.Addr) bool { return true }
	nc, err := nd.DialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	nc.Close()
	if !nd.LastOK().IsZero() {
		t.Fatal("conn with nil RemoteAddr counted as evidence")
	}

	for _, tc := range []struct {
		addr   string
		public bool
	}{
		{"127.0.0.1", false},
		{"::1", false},
		{"10.1.2.3", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"169.254.10.1", false},
		{"100.64.0.1", false},
		{"100.127.255.254", false},
		{"fe80::1", false},
		{"fd00::1", false},
		{"::ffff:192.168.1.1", false},
		{"::ffff:100.64.0.1", false}, // mapped CGNAT: Prefix.Contains needs the Unmap
		{"0.0.0.0", false},
		{"224.0.0.1", false},
		{"8.8.8.8", true},
		{"100.128.0.1", true},
		{"172.32.0.1", true},
		{"2001:4860:4860::8888", true},
		{"::ffff:1.1.1.1", true},
	} {
		if got := isPublicDest(netip.MustParseAddr(tc.addr)); got != tc.public {
			t.Errorf("isPublicDest(%s) = %v, want %v", tc.addr, got, tc.public)
		}
	}
}
