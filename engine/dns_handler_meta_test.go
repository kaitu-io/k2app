package engine

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/kaitu-io/k2/core/dns"
	"github.com/kaitu-io/k2/rule"
	mdns "github.com/miekg/dns"
)

// sourcedPacketConn is a pipePacketConn that also carries a flow source
// address — the shape provider.SourcePacketAddr extracts from sing-tun's
// singPacketConn (via the provider.PacketSourcer interface).
type sourcedPacketConn struct {
	pipePacketConn
	source *net.UDPAddr
}

func (s *sourcedPacketConn) PacketSourceAddr() *net.UDPAddr { return s.source }

// recordingSearcher returns a fixed process name and records the lookup.
type recordingSearcher struct {
	name    string
	calls   int
	network string
	srcIP   netip.Addr
	srcPort uint16
}

func (r *recordingSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string, string) {
	r.calls++
	r.network, r.srcIP, r.srcPort = network, srcIP, srcPort
	return r.name, "", ""
}

// answerQuerier answers every query with a fixed A record.
type answerQuerier struct {
	ip     net.IP
	called bool
}

func (a *answerQuerier) Query(_ context.Context, q *mdns.Msg) (*mdns.Msg, error) {
	a.called = true
	resp := new(mdns.Msg)
	resp.SetReply(q)
	resp.Answer = append(resp.Answer, &mdns.A{
		Hdr: mdns.RR_Header{Name: q.Question[0].Name, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: 300},
		A:   a.ip.To4(),
	})
	return resp, nil
}

// buildMetaMW wires a Middleware whose domain decision is PROXY and whose
// MatchDNSMeta force-directs "douyin.exe".
func buildMetaMW(directQ, proxyQ dns.DNSQuerier) *dns.Middleware {
	return dns.NewMiddleware(dns.MiddlewareConfig{
		DirectDNS:  directQ,
		ProxyDNS:   proxyQ,
		MatchRule:  func(string) rule.Target { return rule.Target(2) },
		SetTmpRule: func(string, rule.Target) {},
		MatchDNSMeta: func(m rule.ConnMeta) (rule.Target, bool) {
			if m.ProcessName == "douyin.exe" {
				return rule.Target(0), true
			}
			return 0, false
		},
	})
}

func runOneUDPQuery(t *testing.T, h *dnsHandler, src *net.UDPAddr) net.IP {
	t.Helper()
	msg := new(mdns.Msg)
	msg.SetQuestion(mdns.Fqdn("geo.example.com"), mdns.TypeA)
	msg.Id = 0x1234
	queryBytes, err := msg.Pack()
	if err != nil {
		t.Fatalf("pack query: %v", err)
	}

	client, server := net.Pipe()
	defer client.Close()
	clientPC := &pipePacketConn{Conn: client}
	serverPC := &sourcedPacketConn{pipePacketConn: pipePacketConn{Conn: server}, source: src}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.serveDNS(serverPC)
	}()

	clientPC.WriteTo(queryBytes, nil)
	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := clientPC.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var resp mdns.Msg
	if err := resp.Unpack(buf[:n]); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	client.Close()
	<-done
	if len(resp.Answer) == 0 {
		t.Fatal("no answer in response")
	}
	return resp.Answer[0].(*mdns.A).A
}

// End-to-end plumbing: the DNS query's source is attributed via the searcher,
// the resulting meta reaches HandleQueryMeta, and a force-direct process's
// query resolves through the DIRECT upstream while the domain decision says
// proxy.
func TestServeDNS_ProcessAttribution_SteersResolverView(t *testing.T) {
	directQ := &answerQuerier{ip: net.ParseIP("110.0.0.1")}
	proxyQ := &answerQuerier{ip: net.ParseIP("8.8.8.8")}
	searcher := &recordingSearcher{name: "douyin.exe"}

	h := &dnsHandler{
		dns:      buildMetaMW(directQ, proxyQ),
		searcher: searcher,
	}
	src := &net.UDPAddr{IP: net.ParseIP("172.19.0.2"), Port: 51234}
	got := runOneUDPQuery(t, h, src)

	if !got.Equal(net.ParseIP("110.0.0.1")) {
		t.Errorf("answer = %s, want direct view 110.0.0.1", got)
	}
	if !directQ.called || proxyQ.called {
		t.Errorf("upstreams: direct=%v proxy=%v, want direct only", directQ.called, proxyQ.called)
	}
	if searcher.calls != 1 {
		t.Errorf("searcher calls = %d, want exactly 1 (once per flow, not per query)", searcher.calls)
	}
	if searcher.network != "udp" || searcher.srcPort != 51234 || searcher.srcIP != netip.MustParseAddr("172.19.0.2") {
		t.Errorf("searcher saw (%s, %s, %d), want (udp, 172.19.0.2, 51234)",
			searcher.network, searcher.srcIP, searcher.srcPort)
	}
}

// No searcher (gate closed: no process rules loaded) → default view, zero
// attribution cost.
func TestServeDNS_NoSearcher_DefaultView(t *testing.T) {
	directQ := &answerQuerier{ip: net.ParseIP("110.0.0.1")}
	proxyQ := &answerQuerier{ip: net.ParseIP("8.8.8.8")}

	h := &dnsHandler{dns: buildMetaMW(directQ, proxyQ)}
	got := runOneUDPQuery(t, h, &net.UDPAddr{IP: net.ParseIP("172.19.0.2"), Port: 51234})

	if !got.Equal(net.ParseIP("8.8.8.8")) {
		t.Errorf("answer = %s, want proxy view 8.8.8.8", got)
	}
}

// A process not covered by any rule (svchost, browsers…) falls through to the
// default view — attribution is fail-open.
func TestServeDNS_UnmatchedProcess_DefaultView(t *testing.T) {
	directQ := &answerQuerier{ip: net.ParseIP("110.0.0.1")}
	proxyQ := &answerQuerier{ip: net.ParseIP("8.8.8.8")}
	searcher := &recordingSearcher{name: "svchost.exe"}

	h := &dnsHandler{dns: buildMetaMW(directQ, proxyQ), searcher: searcher}
	got := runOneUDPQuery(t, h, &net.UDPAddr{IP: net.ParseIP("172.19.0.2"), Port: 51234})

	if !got.Equal(net.ParseIP("8.8.8.8")) {
		t.Errorf("answer = %s, want proxy view 8.8.8.8", got)
	}
}

func TestQueryMeta_NilPaths(t *testing.T) {
	h := &dnsHandler{}
	if m := h.queryMeta("udp", &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 1}); m != nil {
		t.Error("nil searcher must yield nil meta")
	}
	h.searcher = &recordingSearcher{name: ""}
	if m := h.queryMeta("udp", nil); m != nil {
		t.Error("nil source must yield nil meta")
	}
	if m := h.queryMeta("udp", &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 1}); m != nil {
		t.Error("empty attribution must yield nil meta")
	}
}
