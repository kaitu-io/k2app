package core

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/kaitu-io/k2/rule"
)

// pathSearcher is a desktop-style searcher: it knows each socket's process
// name AND where that process's executable lives, keyed by source port.
type pathSearcher struct {
	byPort     map[uint16][2]string // port → {name, path}
	plainCalls int
}

func (s *pathSearcher) FindProcess(string, netip.Addr, uint16) (string, string, string) {
	s.plainCalls++
	return "", "", ""
}

func (s *pathSearcher) FindProcessPath(_ string, _ netip.Addr, port uint16) (string, string) {
	id := s.byPort[port]
	return id[0], id[1]
}

const (
	doubaoHelperPath = "/Applications/Doubao.app/Contents/Helpers/Doubao Browser.app/Contents/Frameworks/F.framework/Helpers/chrome_crashpad_handler"
	chromeHelperPath = "/Applications/Google Chrome.app/Contents/Frameworks/F.framework/Helpers/chrome_crashpad_handler"
)

// The reason path matching exists: two apps ship a helper with the SAME
// basename. Only the one living inside the routed bundle may take the route.
func TestRouteConn_PathSearcherRoutesByBundleNotByName(t *testing.T) {
	const direct, proxy = rule.Target(0), rule.Target(2)
	eng := rule.NewEngine([]rule.RouteEntry{{
		Target:              direct,
		ProcessPathPrefixes: []string{"/Applications/Doubao.app/"},
	}}, proxy, nil, false)
	s := &pathSearcher{byPort: map[uint16][2]string{
		40000: {"chrome_crashpad_handler", doubaoHelperPath},
		40001: {"chrome_crashpad_handler", chromeHelperPath},
	}}
	ct := &ClientTunnel{router: NewRouter(eng), processSearcher: s, directTarget: direct}

	in, meta := ct.routeConn("203.0.113.9:443", "tcp", &net.UDPAddr{IP: net.ParseIP("10.81.3.7"), Port: 40000})
	if in != direct {
		t.Errorf("helper inside Doubao.app routed to %v, want direct", in)
	}
	if meta.ProcessPath != doubaoHelperPath || meta.ProcessName != "chrome_crashpad_handler" {
		t.Errorf("meta = (%q, %q), want the searcher's name and path", meta.ProcessName, meta.ProcessPath)
	}
	out, _ := ct.routeConn("203.0.113.9:443", "tcp", &net.UDPAddr{IP: net.ParseIP("10.81.3.7"), Port: 40001})
	if out != proxy {
		t.Errorf("same-named helper inside Google Chrome.app routed to %v, want fallback", out)
	}
	if s.plainCalls != 0 {
		t.Errorf("FindProcess called %d times — a path-capable searcher must be asked for the path", s.plainCalls)
	}
}

// srcPacketConn is a PacketConn carrying a flow's source address, the way
// the TUN stack's conns do (provider.PacketSourcer).
type srcPacketConn struct {
	src    *net.UDPAddr
	closed chan struct{}
}

func newSrcPacketConn(port int) *srcPacketConn {
	return &srcPacketConn{src: &net.UDPAddr{IP: net.ParseIP("10.81.3.7"), Port: port}, closed: make(chan struct{})}
}

func (c *srcPacketConn) PacketSourceAddr() *net.UDPAddr { return c.src }
func (c *srcPacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	<-c.closed
	return 0, nil, net.ErrClosed
}
func (c *srcPacketConn) WriteTo(b []byte, _ net.Addr) (int, error) { return len(b), nil }
func (c *srcPacketConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}
func (c *srcPacketConn) LocalAddr() net.Addr              { return c.src }
func (c *srcPacketConn) SetDeadline(time.Time) error      { return nil }
func (c *srcPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (c *srcPacketConn) SetWriteDeadline(time.Time) error { return nil }

// Drives the real HandleUDP path (attribution → rule → outbound → dial) and
// reads the result back through TakeAppRouteStats — the same call the
// heartbeat makes. A stats table nobody writes to would read exactly like
// "no app traffic", so the call sites are what this pins.
func TestAppRouteStats_CountedThroughHandleUDP(t *testing.T) {
	const reject, proxy = rule.Target(1), rule.Target(2)
	eng := rule.NewEngine([]rule.RouteEntry{{
		Target:              reject,
		ProcessPathPrefixes: []string{"/Applications/Doubao.app/"},
	}}, proxy, nil, false)

	dialer := newMockDialer()
	dialer.dialErr = errors.New("node cannot reach destination")
	prov := &mockProvider{}
	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: proxy,
		Router:        NewRouter(eng),
		ProcessSearcher: &pathSearcher{byPort: map[uint16][2]string{
			40000: {"Doubao", "/Applications/Doubao.app/Contents/MacOS/Doubao"},
			40001: {"curl", "/usr/bin/curl"},
		}},
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	prov.handler.HandleUDP(newSrcPacketConn(40000), "203.0.113.9:443") // reject route
	prov.handler.HandleUDP(newSrcPacketConn(40001), "203.0.113.9:443") // fallback proxy, dial fails
	prov.handler.HandleUDP(newSrcPacketConn(40002), "203.0.113.9:443") // unattributed

	rows := tunnel.TakeAppRouteStats()
	got := map[string]AppRouteStat{}
	for _, r := range rows {
		got[r.Process] = r
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want exactly the two attributed processes", rows)
	}
	if r := got["Doubao"]; r.Reject != 1 || r.Proxy != 0 || r.Direct != 0 || r.Fail != 0 {
		t.Errorf("Doubao = %+v, want one rejected connection", r)
	}
	if r := got["curl"]; r.Proxy != 1 || r.Fail != 1 || r.LastFail != "proxy/203.0.113.9:443" {
		t.Errorf("curl = %+v, want one failed proxy dial naming its destination", r)
	}
	if again := tunnel.TakeAppRouteStats(); len(again) != 0 {
		t.Errorf("second take = %+v, want a fresh window", again)
	}
}
