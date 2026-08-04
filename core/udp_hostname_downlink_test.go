package core

import (
	"net"
	"net/netip"
	"os"
	"sync"
	"testing"
	"time"
)

// singLikePacketConn stands in for the TUN-side PacketConn (provider's
// singPacketConn over sing-tun). It enforces the one behaviour that makes
// this bug fatal instead of cosmetic: sing-tun's UDP stack rejects a write
// whose destination is not an IP literal.
//
//	stack_gvisor_udp.go:
//	    if !destination.IsIP() {
//	        return E.Cause(os.ErrInvalid, "invalid destination")
//	    }
//
// M.SocksaddrFromNet turns a "host:port" net.Addr into an Fqdn Socksaddr, so
// any hostname handed to WriteTo lands on that branch.
type singLikePacketConn struct {
	mu       sync.Mutex
	writes   []string
	readCh   chan *testPacket
	done     chan struct{}
	closeOne sync.Once
}

func newSingLikePacketConn() *singLikePacketConn {
	return &singLikePacketConn{
		readCh: make(chan *testPacket, 8),
		done:   make(chan struct{}),
	}
}

func (c *singLikePacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case p := <-c.readCh:
		return copy(b, p.payload), p.addr, nil
	case <-c.done:
		return 0, nil, net.ErrClosed
	}
}

func (c *singLikePacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if addr == nil {
		return 0, os.ErrInvalid
	}
	if _, err := netip.ParseAddrPort(addr.String()); err != nil {
		// Non-IP destination: exactly what sing-tun refuses.
		return 0, os.ErrInvalid
	}
	c.mu.Lock()
	c.writes = append(c.writes, addr.String())
	c.mu.Unlock()
	return len(b), nil
}

func (c *singLikePacketConn) Close() error {
	c.closeOne.Do(func() { close(c.done) })
	return nil
}
func (c *singLikePacketConn) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (c *singLikePacketConn) SetDeadline(time.Time) error      { return nil }
func (c *singLikePacketConn) SetReadDeadline(time.Time) error  { return nil }
func (c *singLikePacketConn) SetWriteDeadline(time.Time) error { return nil }

func (c *singLikePacketConn) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.writes))
	copy(out, c.writes)
	return out
}

// waitFor polls until cond() or the deadline, then reports failure with msg.
func waitFor(t *testing.T, msg string, cond func() bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for %s", msg)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestPipePacket_HostnameSourceIsRepairedToPrimary covers the mid-rollout
// combination that the OldServerDownlink e2e subtest misses because its
// session target is an IP literal:
//
//	new client + not-yet-upgraded node + a session target that handleUDP
//	upgraded to a HOSTNAME (QUIC SNI upgrade / IPv6→hostname rewrite).
//
// The old node sends addr_len == 0 on every downlink frame, the wire layer
// falls back to the session target verbatim, and that target is a hostname.
// Handing it to sing-tun returns os.ErrInvalid, and pipePacket treats a
// WriteTo error as fatal — so the FIRST downlink packet kills the whole
// session, uplink included.
func TestPipePacket_HostnameSourceIsRepairedToPrimary(t *testing.T) {
	const appDest = "93.184.216.34:443" // what the app actually addressed
	// What the wire layer synthesises for an empty downlink addr once the
	// session target was rewritten to the sniffed SNI domain.
	const sessionTargetHostname = "example.com:443"

	local := newSingLikePacketConn()
	remote := newRecordingPacketConn()

	done := make(chan struct{})
	go func() {
		pipePacket(local, remote, testAddr(sessionTargetHostname), nil, nil)
		close(done)
	}()
	defer func() {
		local.Close()
		remote.Close()
		<-done
	}()

	// 1. App's first packet establishes the session's primary destination.
	local.readCh <- &testPacket{payload: []byte("hello"), addr: testAddr(appDest)}
	waitFor(t, "uplink packet 1 to reach remote", func() bool {
		return len(remote.snapshot()) >= 1
	})

	// 2. Old node replies; the wire layer surfaces the hostname session target
	//    as the source.
	remote.readCh <- &testPacket{payload: []byte("reply"), addr: testAddr(sessionTargetHostname)}

	waitFor(t, "downlink packet to reach local", func() bool {
		return len(local.snapshot()) >= 1
	})
	if got := local.snapshot()[0]; got != appDest {
		t.Errorf("downlink delivered to %q, want the app's real destination %q", got, appDest)
	}

	// 3. The session must still be alive. Without the repair, step 2 returned
	//    os.ErrInvalid, the RX loop broke, local.Close() ran and the TX
	//    goroutine died — so this uplink packet never reaches remote.
	local.readCh <- &testPacket{payload: []byte("still here"), addr: testAddr(appDest)}
	waitFor(t, "uplink packet 2 to reach remote (session survived the downlink)", func() bool {
		return len(remote.snapshot()) >= 2
	})
}

// TestPipePacket_IPSourceIsPassedThrough guards the other direction: an
// upgraded node sends a real per-packet source, and that source must reach
// the app untouched even when it is NOT the session's primary destination.
// Rewriting every downlink to primary would break full-cone multi-dest.
func TestPipePacket_IPSourceIsPassedThrough(t *testing.T) {
	const appDest = "93.184.216.34:443"
	const otherPeer = "198.51.100.7:27015"

	local := newSingLikePacketConn()
	remote := newRecordingPacketConn()

	done := make(chan struct{})
	go func() {
		pipePacket(local, remote, testAddr("example.com:443"), nil, nil)
		close(done)
	}()
	defer func() {
		local.Close()
		remote.Close()
		<-done
	}()

	local.readCh <- &testPacket{payload: []byte("hello"), addr: testAddr(appDest)}
	waitFor(t, "uplink packet to reach remote", func() bool {
		return len(remote.snapshot()) >= 1
	})

	remote.readCh <- &testPacket{payload: []byte("from peer"), addr: testAddr(otherPeer)}
	waitFor(t, "downlink packet to reach local", func() bool {
		return len(local.snapshot()) >= 1
	})
	if got := local.snapshot()[0]; got != otherPeer {
		t.Errorf("downlink delivered as %q, want the real source %q", got, otherPeer)
	}
}

func TestRepairSourceAddr(t *testing.T) {
	primaryAddr := net.Addr(testAddr("93.184.216.34:443"))
	udpPrimary := net.Addr(&net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 5000})

	tests := []struct {
		name    string
		from    net.Addr
		primary *net.Addr
		want    string
	}{
		{"hostname repaired to primary", testAddr("example.com:443"), &primaryAddr, "93.184.216.34:443"},
		{"ipv4 literal untouched", testAddr("1.2.3.4:53"), &primaryAddr, "1.2.3.4:53"},
		{"ipv6 literal untouched", testAddr("[2001:db8::1]:443"), &primaryAddr, "[2001:db8::1]:443"},
		{"UDPAddr untouched", udpPrimary, &primaryAddr, "10.0.0.1:5000"},
		// Unreachable in production (no downlink before the first uplink),
		// but must not panic and must not invent a destination.
		{"nil primary passes through", testAddr("example.com:443"), nil, "example.com:443"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := repairSourceAddr(tc.from, tc.primary)
			if got.String() != tc.want {
				t.Errorf("repairSourceAddr = %q, want %q", got, tc.want)
			}
		})
	}

	if got := repairSourceAddr(nil, &primaryAddr); got != nil {
		t.Errorf("repairSourceAddr(nil, primary) = %v, want nil", got)
	}
}
