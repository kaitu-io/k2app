package dns

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/k2/rule"
	"github.com/miekg/dns"
)

// forcedDirectFor returns a MatchDNSMeta that force-directs one process name.
func forcedDirectFor(process string) func(rule.ConnMeta) (rule.Target, bool) {
	return func(m rule.ConnMeta) (rule.Target, bool) {
		if m.ProcessName == process {
			return rule.Target(0), true
		}
		return 0, false
	}
}

func directMeta(process string) *rule.ConnMeta {
	return &rule.ConnMeta{ProcessName: process}
}

// A force-direct app's query must resolve through DIRECT DNS even when the
// domain decision says proxy — while the pin (SetTmpRule) keeps the DOMAIN
// decision, so other processes' routing of the resolved IPs is untouched.
func TestHandleQueryMeta_ViewOverride_DirectUpstream_PinKeepsDomainAction(t *testing.T) {
	directQ := &mockQuerier{resp: buildAResponse("geo.example.com", net.ParseIP("110.0.0.1"))}
	proxyQ := &mockQuerier{resp: buildAResponse("geo.example.com", net.ParseIP("8.8.8.8"))}

	pins := map[string]rule.Target{}
	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS:    directQ,
		ProxyDNS:     proxyQ,
		MatchRule:    alwaysProxy, // domain decision: proxy (Target 2)
		SetTmpRule:   func(ip string, t rule.Target) { pins[ip] = t },
		MatchDNSMeta: forcedDirectFor("douyin.exe"),
	})

	respBytes, err := mw.HandleQueryMeta(context.Background(), buildAQuery("geo.example.com"), directMeta("douyin.exe"))
	if err != nil {
		t.Fatalf("HandleQueryMeta error: %v", err)
	}
	var resp dns.Msg
	if err := resp.Unpack(respBytes); err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if a := resp.Answer[0].(*dns.A); !a.A.Equal(net.ParseIP("110.0.0.1")) {
		t.Errorf("answer = %s, want the DIRECT view's 110.0.0.1", a.A)
	}
	if !directQ.called || proxyQ.called {
		t.Errorf("upstream calls: direct=%v proxy=%v, want direct only", directQ.called, proxyQ.called)
	}
	// Pin must carry the DOMAIN action (proxy), not the overridden view.
	if got, ok := pins["110.0.0.1"]; !ok || got != rule.Target(2) {
		t.Errorf("pin[110.0.0.1] = (%v, %v), want (2, true) — domain action, not view", got, ok)
	}
}

// meta == nil (attribution off/unavailable) keeps the pipeline identical.
func TestHandleQueryMeta_NilMeta_DefaultView(t *testing.T) {
	directQ := &mockQuerier{resp: buildAResponse("geo.example.com", net.ParseIP("110.0.0.1"))}
	proxyQ := &mockQuerier{resp: buildAResponse("geo.example.com", net.ParseIP("8.8.8.8"))}

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS:    directQ,
		ProxyDNS:     proxyQ,
		MatchRule:    alwaysProxy,
		SetTmpRule:   nopSetTmp,
		MatchDNSMeta: forcedDirectFor("douyin.exe"),
	})

	if _, err := mw.HandleQueryMeta(context.Background(), buildAQuery("geo.example.com"), nil); err != nil {
		t.Fatalf("HandleQueryMeta error: %v", err)
	}
	if directQ.called || !proxyQ.called {
		t.Errorf("upstream calls: direct=%v proxy=%v, want proxy only", directQ.called, proxyQ.called)
	}
}

// A process whose route agrees with the domain view must not fork the cache
// identity — and a non-matching process changes nothing.
func TestHandleQueryMeta_SameView_NoOverride(t *testing.T) {
	directQ := &mockQuerier{resp: buildAResponse("cn.example.com", net.ParseIP("110.0.0.1"))}
	proxyQ := &mockQuerier{resp: buildAResponse("cn.example.com", net.ParseIP("8.8.8.8"))}

	cache := NewDNSCache(DNSCacheConfig{MaxEntries: 16, MinTTL: time.Minute, MaxTTL: time.Hour})
	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS:    directQ,
		ProxyDNS:     proxyQ,
		DNSCache:     cache,
		MatchRule:    alwaysDirect, // domain decision already direct
		SetTmpRule:   nopSetTmp,
		MatchDNSMeta: forcedDirectFor("douyin.exe"),
	})

	// First query WITH meta primes the cache (view agrees → default identity).
	if _, err := mw.HandleQueryMeta(context.Background(), buildAQuery("cn.example.com"), directMeta("douyin.exe")); err != nil {
		t.Fatalf("first query: %v", err)
	}
	directQ.called = false
	// Second query WITHOUT meta must be a cache hit — no upstream call.
	if _, err := mw.HandleQueryMeta(context.Background(), buildAQuery("cn.example.com"), nil); err != nil {
		t.Fatalf("second query: %v", err)
	}
	if directQ.called || proxyQ.called {
		t.Errorf("second query hit upstream (direct=%v proxy=%v), want shared cache hit", directQ.called, proxyQ.called)
	}
}

// The two views of one domain are different answers: the overridden view's
// cached entry must never serve the default view, and vice versa.
func TestHandleQueryMeta_CacheIsolationBetweenViews(t *testing.T) {
	directQ := &mockQuerier{resp: buildAResponse("geo.example.com", net.ParseIP("110.0.0.1"))}
	proxyQ := &mockQuerier{resp: buildAResponse("geo.example.com", net.ParseIP("8.8.8.8"))}

	cache := NewDNSCache(DNSCacheConfig{MaxEntries: 16, MinTTL: time.Minute, MaxTTL: time.Hour})
	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS:    directQ,
		ProxyDNS:     proxyQ,
		DNSCache:     cache,
		MatchRule:    alwaysProxy,
		SetTmpRule:   nopSetTmp,
		MatchDNSMeta: forcedDirectFor("douyin.exe"),
	})

	// 1. Force-direct app resolves → direct view, cached under view identity.
	if _, err := mw.HandleQueryMeta(context.Background(), buildAQuery("geo.example.com"), directMeta("douyin.exe")); err != nil {
		t.Fatalf("attributed query: %v", err)
	}
	if !directQ.called {
		t.Fatal("attributed query did not use direct upstream")
	}

	// 2. Unattributed query for the same domain must NOT be served the
	// direct-view entry — it goes to proxy upstream.
	respBytes, err := mw.HandleQueryMeta(context.Background(), buildAQuery("geo.example.com"), nil)
	if err != nil {
		t.Fatalf("unattributed query: %v", err)
	}
	if !proxyQ.called {
		t.Fatal("unattributed query was served the direct view's cache entry")
	}
	var resp dns.Msg
	if err := resp.Unpack(respBytes); err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if a := resp.Answer[0].(*dns.A); !a.A.Equal(net.ParseIP("8.8.8.8")) {
		t.Errorf("unattributed answer = %s, want the PROXY view's 8.8.8.8", a.A)
	}

	// 3. Second attributed query is a cache hit on the view entry.
	directQ.called = false
	proxyQ.called = false
	if _, err := mw.HandleQueryMeta(context.Background(), buildAQuery("geo.example.com"), directMeta("douyin.exe")); err != nil {
		t.Fatalf("second attributed query: %v", err)
	}
	if directQ.called || proxyQ.called {
		t.Errorf("second attributed query hit upstream (direct=%v proxy=%v), want view-cache hit", directQ.called, proxyQ.called)
	}
}

// Reject stays a DOMAIN decision: a force-direct process cannot resurrect a
// rejected domain.
func TestHandleQueryMeta_RejectStillWins(t *testing.T) {
	directQ := &mockQuerier{resp: buildAResponse("ads.example.com", net.ParseIP("1.2.3.4"))}
	proxyQ := &mockQuerier{resp: buildAResponse("ads.example.com", net.ParseIP("5.6.7.8"))}

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS:    directQ,
		ProxyDNS:     proxyQ,
		MatchRule:    alwaysReject,
		SetTmpRule:   nopSetTmp,
		MatchDNSMeta: forcedDirectFor("douyin.exe"),
	})

	respBytes, err := mw.HandleQueryMeta(context.Background(), buildAQuery("ads.example.com"), directMeta("douyin.exe"))
	if err != nil {
		t.Fatalf("HandleQueryMeta error: %v", err)
	}
	var resp dns.Msg
	if err := resp.Unpack(respBytes); err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("Rcode = %s, want NXDOMAIN", dns.RcodeToString[resp.Rcode])
	}
	if directQ.called || proxyQ.called {
		t.Error("rejected domain reached an upstream")
	}
}
