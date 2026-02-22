package dns

import (
	"net"
	"testing"

	"github.com/kaitu-io/k2rule"
	"github.com/miekg/dns"
)

// mockQuerier implements DNSQuerier for testing.
type mockQuerier struct {
	resp   *dns.Msg
	err    error
	called bool
}

func (m *mockQuerier) Query(q *dns.Msg) (*dns.Msg, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	resp := m.resp.Copy()
	resp.Id = q.Id
	return resp, nil
}

// initK2Rule initializes k2rule in global mode with the given target.
// Handles the zero-value issue where TargetDirect (0) is ignored in Config.
//
// Uses non-existent file paths for GeoIP/Porn to avoid network downloads
// (retryForever). Init sets globalConfig before failing on file open, and
// global-mode Match() only checks globalConfig — not the managers.
func initK2Rule(t *testing.T, target k2rule.Target) {
	t.Helper()
	k2rule.Init(&k2rule.Config{
		CacheDir:     t.TempDir(),
		IsGlobal:     true,
		GlobalTarget: k2rule.TargetProxy, // non-zero so Config accepts it
		GeoIPFile:    "/nonexistent",     // skip remote download
		PornFile:     "/nonexistent",     // skip remote download
	})
	k2rule.SetGlobalTarget(target)
	k2rule.ToggleGlobal(true)
}

// buildAQuery creates a DNS A query for domain.
func buildAQuery(domain string) []byte {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), dns.TypeA)
	msg.Id = 1234
	b, _ := msg.Pack()
	return b
}

// buildAResponse creates a DNS response with an A record.
func buildAResponse(domain string, ip net.IP) *dns.Msg {
	resp := new(dns.Msg)
	q := new(dns.Msg)
	q.SetQuestion(dns.Fqdn(domain), dns.TypeA)
	resp.SetReply(q)
	resp.Answer = append(resp.Answer, &dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(domain),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: ip.To4(),
	})
	return resp
}

func TestHandleQuery_DirectDomain(t *testing.T) {
	initK2Rule(t, k2rule.TargetDirect)

	directQ := &mockQuerier{
		resp: buildAResponse("direct.example.com", net.ParseIP("1.2.3.4")),
	}
	proxyQ := &mockQuerier{
		resp: buildAResponse("direct.example.com", net.ParseIP("5.6.7.8")),
	}

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: directQ,
		ProxyDNS:  proxyQ,
	})

	query := buildAQuery("direct.example.com")
	respBytes, err := mw.HandleQuery(query)
	if err != nil {
		t.Fatalf("HandleQuery error: %v", err)
	}

	var resp dns.Msg
	if err := resp.Unpack(respBytes); err != nil {
		t.Fatalf("Unpack response: %v", err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("Expected RcodeSuccess, got %s", dns.RcodeToString[resp.Rcode])
	}

	if len(resp.Answer) == 0 {
		t.Fatal("Expected at least one answer")
	}

	aRecord, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatalf("Expected A record, got %T", resp.Answer[0])
	}
	if !aRecord.A.Equal(net.ParseIP("1.2.3.4")) {
		t.Errorf("Expected 1.2.3.4, got %s", aRecord.A)
	}

	if !directQ.called {
		t.Error("Expected directDNS to be called")
	}
	if proxyQ.called {
		t.Error("Expected proxyDNS NOT to be called")
	}
}

func TestHandleQuery_ProxyDomain(t *testing.T) {
	initK2Rule(t, k2rule.TargetProxy)

	directQ := &mockQuerier{
		resp: buildAResponse("proxy.example.com", net.ParseIP("1.2.3.4")),
	}
	proxyQ := &mockQuerier{
		resp: buildAResponse("proxy.example.com", net.ParseIP("10.0.0.1")),
	}

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: directQ,
		ProxyDNS:  proxyQ,
	})

	query := buildAQuery("proxy.example.com")
	respBytes, err := mw.HandleQuery(query)
	if err != nil {
		t.Fatalf("HandleQuery error: %v", err)
	}

	var resp dns.Msg
	if err := resp.Unpack(respBytes); err != nil {
		t.Fatalf("Unpack response: %v", err)
	}

	if len(resp.Answer) == 0 {
		t.Fatal("Expected at least one answer")
	}

	aRecord := resp.Answer[0].(*dns.A)
	if !aRecord.A.Equal(net.ParseIP("10.0.0.1")) {
		t.Errorf("Expected 10.0.0.1 (from proxy), got %s", aRecord.A)
	}

	if directQ.called {
		t.Error("Expected directDNS NOT to be called")
	}
	if !proxyQ.called {
		t.Error("Expected proxyDNS to be called")
	}
}

func TestHandleQuery_RejectDomain(t *testing.T) {
	initK2Rule(t, k2rule.TargetReject)

	directQ := &mockQuerier{
		resp: buildAResponse("reject.example.com", net.ParseIP("1.2.3.4")),
	}
	proxyQ := &mockQuerier{
		resp: buildAResponse("reject.example.com", net.ParseIP("5.6.7.8")),
	}

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: directQ,
		ProxyDNS:  proxyQ,
	})

	query := buildAQuery("reject.example.com")
	respBytes, err := mw.HandleQuery(query)
	if err != nil {
		t.Fatalf("HandleQuery error: %v", err)
	}

	var resp dns.Msg
	if err := resp.Unpack(respBytes); err != nil {
		t.Fatalf("Unpack response: %v", err)
	}

	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("Expected NXDOMAIN, got %s", dns.RcodeToString[resp.Rcode])
	}

	if directQ.called {
		t.Error("Expected directDNS NOT to be called")
	}
	if proxyQ.called {
		t.Error("Expected proxyDNS NOT to be called")
	}
}

func TestHandleQuery_CacheHit(t *testing.T) {
	initK2Rule(t, k2rule.TargetDirect)

	directQ := &mockQuerier{
		resp: buildAResponse("cached.example.com", net.ParseIP("1.2.3.4")),
	}
	proxyQ := &mockQuerier{
		resp: buildAResponse("cached.example.com", net.ParseIP("5.6.7.8")),
	}

	cache := NewDNSCache(DNSCacheConfig{})

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: directQ,
		ProxyDNS:  proxyQ,
		DNSCache:  cache,
	})

	// First query -- should call upstream
	query := buildAQuery("cached.example.com")
	_, err := mw.HandleQuery(query)
	if err != nil {
		t.Fatalf("First HandleQuery error: %v", err)
	}
	if !directQ.called {
		t.Fatal("Expected directDNS to be called on first query")
	}

	// Reset called flag
	directQ.called = false

	// Second query -- should hit cache, NOT call upstream
	respBytes, err := mw.HandleQuery(query)
	if err != nil {
		t.Fatalf("Second HandleQuery error: %v", err)
	}
	if directQ.called {
		t.Error("Expected directDNS NOT to be called on cache hit")
	}

	var resp dns.Msg
	if err := resp.Unpack(respBytes); err != nil {
		t.Fatalf("Unpack response: %v", err)
	}
	if len(resp.Answer) == 0 {
		t.Fatal("Expected cached answer")
	}
}

func TestHandleQuery_InvalidBytes(t *testing.T) {
	mw := NewMiddleware(MiddlewareConfig{})

	_, err := mw.HandleQuery([]byte{0xff, 0xfe, 0x00})
	if err == nil {
		t.Fatal("Expected error for invalid bytes")
	}
}

func TestHandleQuery_NilWriter(t *testing.T) {
	// Middleware with nil Writer (L4 mode) -- HandleQuery works fine
	initK2Rule(t, k2rule.TargetDirect)

	directQ := &mockQuerier{
		resp: buildAResponse("l4.example.com", net.ParseIP("1.2.3.4")),
	}

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: directQ,
		ProxyDNS:  &mockQuerier{resp: buildAResponse("l4.example.com", net.ParseIP("5.6.7.8"))},
		Writer:    nil, // L4 mode -- no writer
	})

	query := buildAQuery("l4.example.com")
	respBytes, err := mw.HandleQuery(query)
	if err != nil {
		t.Fatalf("HandleQuery with nil writer should work: %v", err)
	}

	var resp dns.Msg
	if err := resp.Unpack(respBytes); err != nil {
		t.Fatalf("Unpack response: %v", err)
	}
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("Expected success, got %s", dns.RcodeToString[resp.Rcode])
	}
}
