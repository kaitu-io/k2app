package server

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	k2dns "github.com/kaitu-io/k2/core/dns"
	"github.com/miekg/dns"
)

func newTestResolver(t *testing.T, upstreamAddr string) *Resolver {
	t.Helper()
	cache := k2dns.NewDNSCache(k2dns.DNSCacheConfig{})
	return NewResolver(cache, []string{upstreamAddr})
}

func TestResolver_LookupHost_IPLiteral(t *testing.T) {
	r := newTestResolver(t, "127.0.0.1:0")
	ips, err := r.LookupHost(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || ips[0] != "8.8.8.8" {
		t.Fatalf("got %v, want [8.8.8.8]", ips)
	}
}

func TestResolver_LookupHost_CacheMiss(t *testing.T) {
	var calls atomic.Int32
	fakeDNS := startFakeDNSServer(t, &calls)
	defer fakeDNS.Close()

	r := newTestResolver(t, fakeDNS.LocalAddr().String())
	ips, err := r.LookupHost(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) == 0 || ips[0] != "93.184.216.34" {
		t.Fatalf("got %v, want [93.184.216.34]", ips)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls.Load())
	}
}

func TestResolver_LookupHost_CacheHit(t *testing.T) {
	var calls atomic.Int32
	fakeDNS := startFakeDNSServer(t, &calls)
	defer fakeDNS.Close()

	r := newTestResolver(t, fakeDNS.LocalAddr().String())

	_, err := r.LookupHost(context.Background(), "cached.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("prime: upstream calls = %d, want 1", calls.Load())
	}

	ips, err := r.LookupHost(context.Background(), "cached.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) == 0 || ips[0] != "93.184.216.34" {
		t.Fatalf("got %v, want [93.184.216.34]", ips)
	}
	if calls.Load() != 1 {
		t.Fatalf("cached: upstream calls = %d, want 1 (no new query)", calls.Load())
	}
}

func TestResolver_LookupHost_SharedCache(t *testing.T) {
	var calls atomic.Int32
	fakeDNS := startFakeDNSServer(t, &calls)
	defer fakeDNS.Close()

	cache := k2dns.NewDNSCache(k2dns.DNSCacheConfig{})
	h := &ProxyHandler{
		DialTimeout:    5 * time.Second,
		AllowPrivateIP: true,
		conns:          connTracker{conns: make(map[io.Closer]struct{})},
		dnsCache:       cache,
	}
	r := NewResolver(cache, []string{fakeDNS.LocalAddr().String()})

	// Prime cache via handleDNSUDP.
	sendDNSViaHandler(t, h, fakeDNS.LocalAddr().String(),
		buildDNSQuery(t, "shared.example.com.", dns.TypeA, 42))
	if calls.Load() != 1 {
		t.Fatalf("handleDNSUDP: upstream calls = %d, want 1", calls.Load())
	}

	// Resolver should find it in shared cache.
	ips, err := r.LookupHost(context.Background(), "shared.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) == 0 || ips[0] != "93.184.216.34" {
		t.Fatalf("got %v, want [93.184.216.34]", ips)
	}
	if calls.Load() != 1 {
		t.Fatalf("resolver: upstream calls = %d, want 1 (shared cache hit)", calls.Load())
	}
}

func TestResolver_LookupHost_NXDOMAIN(t *testing.T) {
	nxServer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer nxServer.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := nxServer.ReadFrom(buf)
			if err != nil {
				return
			}
			var query dns.Msg
			if err := query.Unpack(buf[:n]); err != nil {
				continue
			}
			resp := new(dns.Msg)
			resp.SetRcode(&query, dns.RcodeNameError)
			packed, _ := resp.Pack()
			nxServer.WriteTo(packed, addr)
		}
	}()

	r := newTestResolver(t, nxServer.LocalAddr().String())
	_, err = r.LookupHost(context.Background(), "nx.example.com")
	if err == nil {
		t.Fatal("expected error for NXDOMAIN")
	}
}

func TestResolver_LookupHost_Timeout(t *testing.T) {
	blackhole, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blackhole.Close()

	cache := k2dns.NewDNSCache(k2dns.DNSCacheConfig{})
	r := &Resolver{
		cache:   cache,
		servers: []string{blackhole.LocalAddr().String()},
		timeout: 200 * time.Millisecond,
	}
	_, err = r.LookupHost(context.Background(), "timeout.example.com")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
