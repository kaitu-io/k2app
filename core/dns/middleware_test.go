package dns

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2rule"
	"github.com/miekg/dns"
)

// mockQuerier implements DNSQuerier for testing.
type mockQuerier struct {
	resp   *dns.Msg
	err    error
	called bool
}

func (m *mockQuerier) Query(ctx context.Context, q *dns.Msg) (*dns.Msg, error) {
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
	respBytes, err := mw.HandleQuery(context.Background(), query)
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
	respBytes, err := mw.HandleQuery(context.Background(), query)
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
	respBytes, err := mw.HandleQuery(context.Background(), query)
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

	ctx := context.Background()

	// First query -- should call upstream
	query := buildAQuery("cached.example.com")
	_, err := mw.HandleQuery(ctx, query)
	if err != nil {
		t.Fatalf("First HandleQuery error: %v", err)
	}
	if !directQ.called {
		t.Fatal("Expected directDNS to be called on first query")
	}

	// Reset called flag
	directQ.called = false

	// Second query -- should hit cache, NOT call upstream
	respBytes, err := mw.HandleQuery(ctx, query)
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

	_, err := mw.HandleQuery(context.Background(), []byte{0xff, 0xfe, 0x00})
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
	respBytes, err := mw.HandleQuery(context.Background(), query)
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

// slowQuerier implements DNSQuerier and blocks for a configurable duration.
type slowQuerier struct {
	delay time.Duration
	resp  *dns.Msg
	err   error
	mu    sync.Mutex
	calls int
}

func (q *slowQuerier) Query(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	q.mu.Lock()
	q.calls++
	q.mu.Unlock()

	select {
	case <-time.After(q.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if q.err != nil {
		return nil, q.err
	}
	resp := q.resp.Copy()
	resp.Id = msg.Id
	return resp, nil
}

func TestInflightDedup_WaiterCtxCancelled_ReturnsStaleOrError(t *testing.T) {
	initK2Rule(t, k2rule.TargetDirect)

	// Slow owner blocks for a long time.
	slowQ := &slowQuerier{
		delay: 60 * time.Second,
		resp:  buildAResponse("hang.example.com", net.ParseIP("1.2.3.4")),
	}

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: slowQ,
		ProxyDNS:  &mockQuerier{resp: buildAResponse("hang.example.com", net.ParseIP("5.6.7.8"))},
	})

	query := buildAQuery("hang.example.com")

	// Start the owning query in background (will block in slowQ).
	ownerCtx, ownerCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer ownerCancel()
	go mw.HandleQuery(ownerCtx, query)
	time.Sleep(50 * time.Millisecond) // let owner register inflight entry

	// Waiter query with short context — should return error promptly (sing-box pattern).
	waiterCtx, waiterCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer waiterCancel()

	start := time.Now()
	_, err := mw.HandleQuery(waiterCtx, query)
	elapsed := time.Since(start)

	// Waiter should get context error (no stale data available).
	if err == nil {
		t.Fatal("expected error from waiter context cancellation")
	}
	if elapsed > 2*time.Second {
		t.Errorf("waiter blocked too long: %v (expected ~500ms)", elapsed)
	}
}

func TestInflightDedup_WaiterCtxCancelled_ReturnsStale(t *testing.T) {
	initK2Rule(t, k2rule.TargetDirect)

	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:   1 * time.Second,
		MaxTTL:   1 * time.Second,
		StaleFor: 5 * time.Minute,
	})

	// Populate cache.
	fastQ := &mockQuerier{resp: buildAResponse("stale-waiter.example.com", net.ParseIP("1.2.3.4"))}
	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: fastQ,
		ProxyDNS:  &mockQuerier{resp: buildAResponse("stale-waiter.example.com", net.ParseIP("5.6.7.8"))},
		DNSCache:  cache,
	})
	query := buildAQuery("stale-waiter.example.com")
	_, err := mw.HandleQuery(context.Background(), query)
	if err != nil {
		t.Fatalf("populate cache: %v", err)
	}

	// Wait for cache to expire.
	time.Sleep(1500 * time.Millisecond)

	// Now use a slow querier — owner will block.
	slowQ := &slowQuerier{
		delay: 60 * time.Second,
		resp:  buildAResponse("stale-waiter.example.com", net.ParseIP("9.9.9.9")),
	}
	mw2 := NewMiddleware(MiddlewareConfig{
		DirectDNS: slowQ,
		ProxyDNS:  &mockQuerier{resp: buildAResponse("stale-waiter.example.com", net.ParseIP("5.6.7.8"))},
		DNSCache:  cache,
	})

	// Owner blocks in background.
	ownerCtx, ownerCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer ownerCancel()
	go mw2.HandleQuery(ownerCtx, query)
	time.Sleep(50 * time.Millisecond)

	// Waiter with short context — should get stale data.
	waiterCtx, waiterCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer waiterCancel()

	respBytes, err := mw2.HandleQuery(waiterCtx, query)
	if err != nil {
		t.Fatalf("expected stale fallback, got error: %v", err)
	}

	var resp dns.Msg
	if err := resp.Unpack(respBytes); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if len(resp.Answer) == 0 {
		t.Fatal("expected stale answer")
	}
}

func TestInflightDedup_NormalDedup(t *testing.T) {
	initK2Rule(t, k2rule.TargetDirect)

	directQ := &slowQuerier{
		delay: 200 * time.Millisecond,
		resp:  buildAResponse("dedup.example.com", net.ParseIP("1.2.3.4")),
	}

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: directQ,
		ProxyDNS:  &mockQuerier{resp: buildAResponse("dedup.example.com", net.ParseIP("5.6.7.8"))},
	})

	query := buildAQuery("dedup.example.com")
	ctx := context.Background()

	// Fire two concurrent queries for the same domain.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	resps := make([][]byte, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resps[idx], errs[idx] = mw.HandleQuery(ctx, query)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("query %d: unexpected error: %v", i, err)
		}
	}

	// Both should have valid responses with the same IP.
	for i, respBytes := range resps {
		var resp dns.Msg
		if err := resp.Unpack(respBytes); err != nil {
			t.Errorf("query %d: unpack error: %v", i, err)
			continue
		}
		if len(resp.Answer) == 0 {
			t.Errorf("query %d: no answer", i)
			continue
		}
		a := resp.Answer[0].(*dns.A)
		if !a.A.Equal(net.ParseIP("1.2.3.4")) {
			t.Errorf("query %d: got %s, want 1.2.3.4", i, a.A)
		}
	}

	// Verify dedup: only 1 upstream call for 2 queries.
	directQ.mu.Lock()
	calls := directQ.calls
	directQ.mu.Unlock()
	if calls != 1 {
		t.Errorf("expected 1 upstream call (dedup), got %d", calls)
	}
}

func TestHandleQuery_StaleOnUpstreamError(t *testing.T) {
	initK2Rule(t, k2rule.TargetProxy)

	proxyQ := &mockQuerier{
		resp: buildAResponse("stale.example.com", net.ParseIP("10.0.0.1")),
	}

	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:   1 * time.Second,
		MaxTTL:   1 * time.Second, // Cap TTL so entry expires in 1s
		StaleFor: 5 * time.Minute,
	})
	metrics := NewMetrics()

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: &mockQuerier{resp: buildAResponse("stale.example.com", net.ParseIP("1.2.3.4"))},
		ProxyDNS:  proxyQ,
		DNSCache:  cache,
		Metrics:   metrics,
	})

	ctx := context.Background()

	// First query — succeeds and populates cache
	query := buildAQuery("stale.example.com")
	_, err := mw.HandleQuery(ctx, query)
	if err != nil {
		t.Fatalf("first query: %v", err)
	}

	// Wait for cache entry to expire
	time.Sleep(1500 * time.Millisecond)

	// Now make proxy fail
	proxyQ.err = fmt.Errorf("tunnel broken")
	proxyQ.resp = nil

	// Second query — upstream fails, should return stale data
	respBytes, err := mw.HandleQuery(ctx, query)
	if err != nil {
		t.Fatalf("expected stale fallback, got error: %v", err)
	}

	var resp dns.Msg
	if err := resp.Unpack(respBytes); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if len(resp.Answer) == 0 {
		t.Fatal("expected answer from stale cache")
	}

	if metrics.StaleCacheHits != 1 {
		t.Errorf("StaleCacheHits = %d, want 1", metrics.StaleCacheHits)
	}
}

func TestHandleQuery_StaleOnUpstreamError_Waiter(t *testing.T) {
	initK2Rule(t, k2rule.TargetProxy)

	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:   1 * time.Second,
		MaxTTL:   1 * time.Second,
		StaleFor: 5 * time.Minute,
	})
	metrics := NewMetrics()

	// First: populate cache with a successful response
	goodQ := &mockQuerier{resp: buildAResponse("waiter-stale.example.com", net.ParseIP("10.0.0.1"))}
	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: &mockQuerier{resp: buildAResponse("waiter-stale.example.com", net.ParseIP("1.2.3.4"))},
		ProxyDNS:  goodQ,
		DNSCache:  cache,
		Metrics:   metrics,
	})

	ctx := context.Background()
	query := buildAQuery("waiter-stale.example.com")
	_, err := mw.HandleQuery(ctx, query)
	if err != nil {
		t.Fatalf("populate cache: %v", err)
	}

	// Wait for cache to expire
	time.Sleep(1500 * time.Millisecond)

	// Now use a slow failing querier — owner will block then fail
	failQ := &slowQuerier{
		delay: 200 * time.Millisecond,
		err:   fmt.Errorf("tunnel broken"),
	}
	mw2 := NewMiddleware(MiddlewareConfig{
		DirectDNS: &mockQuerier{resp: buildAResponse("waiter-stale.example.com", net.ParseIP("1.2.3.4"))},
		ProxyDNS:  failQ,
		DNSCache:  cache,
		Metrics:   metrics,
	})

	// Fire two concurrent queries — both should get stale data
	var wg sync.WaitGroup
	errs := make([]error, 2)
	resps := make([][]byte, 2)

	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resps[idx], errs[idx] = mw2.HandleQuery(ctx, query)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("query %d: expected stale fallback, got error: %v", i, err)
		}
	}

	if metrics.StaleCacheHits < 2 {
		t.Errorf("StaleCacheHits = %d, want >= 2", metrics.StaleCacheHits)
	}
}

func TestHandleQuery_NoStale_WhenCacheEmpty(t *testing.T) {
	initK2Rule(t, k2rule.TargetProxy)

	proxyQ := &mockQuerier{err: fmt.Errorf("tunnel broken")}

	cache := NewDNSCache(DNSCacheConfig{StaleFor: 5 * time.Minute})
	metrics := NewMetrics()

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: &mockQuerier{resp: buildAResponse("empty.example.com", net.ParseIP("1.2.3.4"))},
		ProxyDNS:  proxyQ,
		DNSCache:  cache,
		Metrics:   metrics,
	})

	query := buildAQuery("empty.example.com")
	_, err := mw.HandleQuery(context.Background(), query)
	if err == nil {
		t.Fatal("expected error when no stale data available")
	}

	if metrics.StaleCacheHits != 0 {
		t.Errorf("StaleCacheHits = %d, want 0", metrics.StaleCacheHits)
	}
}

func TestHandleQuery_NoStale_ForNXDOMAIN(t *testing.T) {
	initK2Rule(t, k2rule.TargetProxy)

	// First response is NXDOMAIN
	nxResp := new(dns.Msg)
	q := new(dns.Msg)
	q.SetQuestion(dns.Fqdn("gone.example.com"), dns.TypeA)
	nxResp.SetReply(q)
	nxResp.Rcode = dns.RcodeNameError
	nxResp.Ns = []dns.RR{&dns.SOA{
		Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 1},
		Ns:     "ns.example.com.", Mbox: "admin.example.com.",
		Serial: 1, Refresh: 3600, Retry: 600, Expire: 86400, Minttl: 1,
	}}

	proxyQ := &mockQuerier{resp: nxResp}

	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:   1 * time.Second,
		MaxTTL:   1 * time.Second,
		StaleFor: 5 * time.Minute,
	})
	metrics := NewMetrics()

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: &mockQuerier{resp: buildAResponse("gone.example.com", net.ParseIP("1.2.3.4"))},
		ProxyDNS:  proxyQ,
		DNSCache:  cache,
		Metrics:   metrics,
	})

	ctx := context.Background()

	// First query — NXDOMAIN gets cached in negative cache
	query := buildAQuery("gone.example.com")
	_, err := mw.HandleQuery(ctx, query)
	if err != nil {
		t.Fatalf("first query: %v", err)
	}

	// Wait for negative cache to expire
	time.Sleep(1500 * time.Millisecond)

	// Now make proxy fail
	proxyQ.err = fmt.Errorf("tunnel broken")
	proxyQ.resp = nil

	// Should get error, not stale NXDOMAIN
	_, err = mw.HandleQuery(ctx, query)
	if err == nil {
		t.Fatal("expected error, not stale NXDOMAIN")
	}

	if metrics.StaleCacheHits != 0 {
		t.Errorf("StaleCacheHits = %d, want 0 (NXDOMAIN should never be served stale)", metrics.StaleCacheHits)
	}
}

func TestHandleQuery_FreshCacheHit_Unchanged(t *testing.T) {
	initK2Rule(t, k2rule.TargetProxy)

	proxyQ := &mockQuerier{
		resp: buildAResponse("fresh.example.com", net.ParseIP("10.0.0.1")),
	}

	cache := NewDNSCache(DNSCacheConfig{StaleFor: 5 * time.Minute})
	metrics := NewMetrics()

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: &mockQuerier{resp: buildAResponse("fresh.example.com", net.ParseIP("1.2.3.4"))},
		ProxyDNS:  proxyQ,
		DNSCache:  cache,
		Metrics:   metrics,
	})

	ctx := context.Background()
	query := buildAQuery("fresh.example.com")

	// First query — populates cache
	_, err := mw.HandleQuery(ctx, query)
	if err != nil {
		t.Fatalf("first query: %v", err)
	}

	// Second query — fresh cache hit (no stale involved)
	respBytes, err := mw.HandleQuery(ctx, query)
	if err != nil {
		t.Fatalf("second query: %v", err)
	}

	var resp dns.Msg
	if err := resp.Unpack(respBytes); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if len(resp.Answer) == 0 {
		t.Fatal("expected answer from fresh cache")
	}

	if metrics.StaleCacheHits != 0 {
		t.Errorf("StaleCacheHits = %d, want 0 (fresh cache hit should not increment stale)", metrics.StaleCacheHits)
	}
	if metrics.MessageCacheHits != 1 {
		t.Errorf("MessageCacheHits = %d, want 1", metrics.MessageCacheHits)
	}
}

func TestQueryUpstream_ProxyFailNoFallback(t *testing.T) {
	initK2Rule(t, k2rule.TargetProxy)

	directQ := &mockQuerier{
		resp: buildAResponse("fallback.example.com", net.ParseIP("1.2.3.4")),
	}
	proxyQ := &mockQuerier{
		err: fmt.Errorf("tunnel broken"),
	}

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: directQ,
		ProxyDNS:  proxyQ,
	})

	query := buildAQuery("fallback.example.com")
	_, err := mw.HandleQuery(context.Background(), query)

	// Proxy should have been called and failed.
	if !proxyQ.called {
		t.Error("expected proxyDNS to be called")
	}

	// Direct must NOT be called — no fallback for proxy-routed domains.
	if directQ.called {
		t.Error("directDNS must not be called as fallback for proxy-routed domains")
	}

	// Must return error, not poisoned direct DNS results.
	if err == nil {
		t.Fatal("expected error when proxy DNS fails, got nil (no fallback allowed)")
	}
}

func TestInflightDedup_OwnerPanic_WaitersRecover(t *testing.T) {
	initK2Rule(t, k2rule.TargetDirect)

	panicQ := &panicQuerier{
		resp: buildAResponse("panic.example.com", net.ParseIP("1.2.3.4")),
	}

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: panicQ,
		ProxyDNS:  &mockQuerier{resp: buildAResponse("panic.example.com", net.ParseIP("5.6.7.8"))},
	})

	query := buildAQuery("panic.example.com")
	ctx := context.Background()

	// Start owner query — will panic in queryUpstream.
	ownerDone := make(chan struct{})
	go func() {
		defer close(ownerDone)
		defer func() { recover() }() // catch the panic
		mw.HandleQuery(ctx, query)
	}()
	time.Sleep(50 * time.Millisecond)

	// Waiter query — should not hang forever.
	waiterCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_, err := mw.HandleQuery(waiterCtx, query)
	// Waiter should either get an error or a valid response, but not hang.
	<-ownerDone
	_ = err // either outcome is acceptable — the important thing is no deadlock
}

// panicQuerier panics on first call.
type panicQuerier struct {
	resp *dns.Msg
	once sync.Once
}

func (q *panicQuerier) Query(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	q.once.Do(func() {
		panic("simulated panic in DNS query")
	})
	resp := q.resp.Copy()
	resp.Id = msg.Id
	return resp, nil
}

// countingQuerier implements DNSQuerier and counts calls.
type countingQuerier struct {
	resp  *dns.Msg
	err   error
	count *atomic.Int32
}

func (q *countingQuerier) Query(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	q.count.Add(1)
	if q.err != nil {
		return nil, q.err
	}
	resp := q.resp.Copy()
	resp.Id = msg.Id
	return resp, nil
}

// slowCountingQuerier implements DNSQuerier with a configurable delay.
type slowCountingQuerier struct {
	delay time.Duration
	resp  *dns.Msg
	count *atomic.Int32
}

func (q *slowCountingQuerier) Query(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	q.count.Add(1)
	select {
	case <-time.After(q.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	resp := q.resp.Copy()
	resp.Id = msg.Id
	return resp, nil
}

func TestHandleQuery_PrefreshTriggered(t *testing.T) {
	initK2Rule(t, k2rule.TargetDirect)

	callCount := atomic.Int32{}
	countQ := &countingQuerier{
		resp:  buildAResponse("prefresh.example.com", net.ParseIP("1.2.3.4")),
		count: &callCount,
	}

	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:   4 * time.Second,
		MaxTTL:   4 * time.Second,
		StaleFor: 5 * time.Minute,
	})
	metrics := NewMetrics()

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: countQ,
		ProxyDNS:  &mockQuerier{resp: buildAResponse("prefresh.example.com", net.ParseIP("5.6.7.8"))},
		DNSCache:  cache,
		Metrics:   metrics,
		EngineCtx: context.Background(),
	})

	ctx := context.Background()
	query := buildAQuery("prefresh.example.com")

	// First query — populates cache
	_, err := mw.HandleQuery(ctx, query)
	if err != nil {
		t.Fatalf("first query: %v", err)
	}
	if callCount.Load() != 1 {
		t.Fatalf("expected 1 upstream call, got %d", callCount.Load())
	}

	// Query while fresh — pure cache hit, no prefresh
	_, err = mw.HandleQuery(ctx, query)
	if err != nil {
		t.Fatalf("second query: %v", err)
	}
	if callCount.Load() != 1 {
		t.Error("expected no additional upstream call while cache is fresh")
	}

	// Wait until near expiry (> 75% of 4s TTL elapsed)
	time.Sleep(3200 * time.Millisecond)

	// Query near expiry — should return cached AND trigger background refresh
	respBytes, err := mw.HandleQuery(ctx, query)
	if err != nil {
		t.Fatalf("near-expiry query: %v", err)
	}
	var resp dns.Msg
	if err := resp.Unpack(respBytes); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if len(resp.Answer) == 0 {
		t.Fatal("expected cached answer")
	}

	// Wait for background refresh
	time.Sleep(500 * time.Millisecond)

	if callCount.Load() != 2 {
		t.Errorf("expected background refresh, total calls = %d, want 2", callCount.Load())
	}
	if metrics.PrefreshTriggered != 1 {
		t.Errorf("PrefreshTriggered = %d, want 1", metrics.PrefreshTriggered)
	}
}

func TestHandleQuery_PrefreshDedup(t *testing.T) {
	initK2Rule(t, k2rule.TargetDirect)

	callCount := atomic.Int32{}
	// Slow querier so prefresh goroutine stays alive during second query
	slowCountQ := &slowCountingQuerier{
		delay: 500 * time.Millisecond,
		resp:  buildAResponse("dedup-prefresh.example.com", net.ParseIP("1.2.3.4")),
		count: &callCount,
	}

	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:   4 * time.Second,
		MaxTTL:   4 * time.Second,
		StaleFor: 5 * time.Minute,
	})

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: slowCountQ,
		ProxyDNS:  &mockQuerier{resp: buildAResponse("dedup-prefresh.example.com", net.ParseIP("5.6.7.8"))},
		DNSCache:  cache,
		Metrics:   NewMetrics(),
		EngineCtx: context.Background(),
	})

	ctx := context.Background()
	query := buildAQuery("dedup-prefresh.example.com")

	// Populate cache (slow querier takes 500ms)
	_, _ = mw.HandleQuery(ctx, query)
	// Wait until near expiry
	time.Sleep(3200 * time.Millisecond)

	// Fire two near-expiry queries rapidly — only one prefresh should trigger
	_, _ = mw.HandleQuery(ctx, query)
	_, _ = mw.HandleQuery(ctx, query)

	// Wait for prefresh to complete
	time.Sleep(1 * time.Second)

	// 1 initial + 1 prefresh = 2 (not 3 — dedup prevented second prefresh)
	if callCount.Load() != 2 {
		t.Errorf("expected dedup to prevent duplicate prefresh, calls = %d, want 2", callCount.Load())
	}
}

func TestHandleQuery_PrefreshDisabledWithoutEngineCtx(t *testing.T) {
	initK2Rule(t, k2rule.TargetDirect)

	callCount := atomic.Int32{}
	countQ := &countingQuerier{
		resp:  buildAResponse("no-prefresh.example.com", net.ParseIP("1.2.3.4")),
		count: &callCount,
	}

	cache := NewDNSCache(DNSCacheConfig{
		MinTTL: 4 * time.Second,
		MaxTTL: 4 * time.Second,
	})

	// No EngineCtx → prefresh disabled
	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: countQ,
		ProxyDNS:  &mockQuerier{resp: buildAResponse("no-prefresh.example.com", net.ParseIP("5.6.7.8"))},
		DNSCache:  cache,
	})

	ctx := context.Background()
	query := buildAQuery("no-prefresh.example.com")

	_, _ = mw.HandleQuery(ctx, query)
	time.Sleep(3200 * time.Millisecond)
	_, _ = mw.HandleQuery(ctx, query)
	time.Sleep(500 * time.Millisecond)

	if callCount.Load() != 1 {
		t.Errorf("expected no prefresh without EngineCtx, calls = %d", callCount.Load())
	}
}

func TestHandleQuery_ContextCancellation(t *testing.T) {
	initK2Rule(t, k2rule.TargetProxy)

	// Slow querier that respects context cancellation.
	slowQ := &slowQuerier{
		delay: 10 * time.Second,
		resp:  buildAResponse("cancel.example.com", net.ParseIP("10.0.0.1")),
	}

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: &mockQuerier{resp: buildAResponse("cancel.example.com", net.ParseIP("1.2.3.4"))},
		ProxyDNS:  slowQ,
	})

	query := buildAQuery("cancel.example.com")

	// Cancel context quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := mw.HandleQuery(ctx, query)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
	if elapsed > 2*time.Second {
		t.Errorf("query took too long despite cancellation: %v", elapsed)
	}
}
