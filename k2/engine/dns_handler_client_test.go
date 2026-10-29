package engine

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
)

// resolverSearcher is a Windows-shaped searcher: the DNS flow's socket
// belongs to the resolver service, and a separate lookup says which app the
// service is asking for.
type resolverSearcher struct {
	recordingSearcher
	startErr error
	clients  map[string]string // qname → app

	mu       sync.Mutex
	holds    int
	asked    []string // "resolver|qname"
	released chan struct{}
}

func (r *resolverSearcher) StartDNSClientWatch() (func(), error) {
	if r.startErr != nil {
		return nil, r.startErr
	}
	r.mu.Lock()
	r.holds++
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		r.holds--
		r.mu.Unlock()
		if r.released != nil {
			close(r.released)
		}
	}, nil
}

func (r *resolverSearcher) FindDNSClient(resolver, qname string) (string, string, bool) {
	r.mu.Lock()
	r.asked = append(r.asked, resolver+"|"+qname)
	r.mu.Unlock()
	if resolver != "svchost.exe" {
		return "", "", false
	}
	app, ok := r.clients[qname]
	return app, `c:\apps\` + app, ok
}

// The case this exists for: the query's socket is svchost's, the app behind
// it is force-direct, so the query must resolve through the DIRECT view.
// Without the per-query lookup this is TestServeDNS_UnmatchedProcess_DefaultView
// and the answer is the proxy view.
func TestServeDNS_SystemResolver_AttributedToCallingApp(t *testing.T) {
	directQ := &answerQuerier{ip: net.ParseIP("110.0.0.1")}
	proxyQ := &answerQuerier{ip: net.ParseIP("8.8.8.8")}
	searcher := &resolverSearcher{
		recordingSearcher: recordingSearcher{name: "svchost.exe"},
		clients:           map[string]string{"geo.example.com": "douyin.exe"},
	}
	h := &dnsHandler{dns: buildMetaMW(directQ, proxyQ), searcher: searcher}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.startDNSClientWatch(ctx)

	got := runOneUDPQuery(t, h, &net.UDPAddr{IP: net.ParseIP("172.19.0.2"), Port: 51234})
	if !got.Equal(net.ParseIP("110.0.0.1")) {
		t.Errorf("answer = %s, want direct view 110.0.0.1", got)
	}
	if len(searcher.asked) != 1 || searcher.asked[0] != "svchost.exe|geo.example.com" {
		t.Errorf("FindDNSClient calls = %v, want one (svchost.exe, geo.example.com)", searcher.asked)
	}
}

// Nobody is known to have asked for this name → the flow attribution
// (svchost) stands and the default view applies. Fail-open.
func TestServeDNS_SystemResolver_UnknownCaller_DefaultView(t *testing.T) {
	directQ := &answerQuerier{ip: net.ParseIP("110.0.0.1")}
	proxyQ := &answerQuerier{ip: net.ParseIP("8.8.8.8")}
	searcher := &resolverSearcher{
		recordingSearcher: recordingSearcher{name: "svchost.exe"},
		clients:           map[string]string{"other.example.com": "douyin.exe"},
	}
	h := &dnsHandler{dns: buildMetaMW(directQ, proxyQ), searcher: searcher}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.startDNSClientWatch(ctx)

	got := runOneUDPQuery(t, h, &net.UDPAddr{IP: net.ParseIP("172.19.0.2"), Port: 51234})
	if !got.Equal(net.ParseIP("8.8.8.8")) {
		t.Errorf("answer = %s, want proxy view 8.8.8.8", got)
	}
}

// The watch could not start (daemon not elevated): no per-query lookups at
// all, behavior identical to a searcher without the capability.
func TestServeDNS_SystemResolver_WatchUnavailable(t *testing.T) {
	directQ := &answerQuerier{ip: net.ParseIP("110.0.0.1")}
	proxyQ := &answerQuerier{ip: net.ParseIP("8.8.8.8")}
	searcher := &resolverSearcher{
		recordingSearcher: recordingSearcher{name: "svchost.exe"},
		startErr:          errors.New("access denied"),
		clients:           map[string]string{"geo.example.com": "douyin.exe"},
	}
	h := &dnsHandler{dns: buildMetaMW(directQ, proxyQ), searcher: searcher}
	h.startDNSClientWatch(context.Background())

	got := runOneUDPQuery(t, h, &net.UDPAddr{IP: net.ParseIP("172.19.0.2"), Port: 51234})
	if !got.Equal(net.ParseIP("8.8.8.8")) {
		t.Errorf("answer = %s, want proxy view 8.8.8.8", got)
	}
	if len(searcher.asked) != 0 {
		t.Errorf("FindDNSClient called %d times with the watch off", len(searcher.asked))
	}
}

// The hold is given back when the engine context ends — the one release
// path every teardown (Stop, fail, Start failure) goes through.
func TestStartDNSClientWatch_ReleasedWithContext(t *testing.T) {
	searcher := &resolverSearcher{released: make(chan struct{})}
	h := &dnsHandler{searcher: searcher}
	ctx, cancel := context.WithCancel(context.Background())
	h.startDNSClientWatch(ctx)

	searcher.mu.Lock()
	holds := searcher.holds
	searcher.mu.Unlock()
	if holds != 1 {
		t.Fatalf("holds = %d after start, want 1", holds)
	}
	cancel()
	select {
	case <-searcher.released:
	case <-time.After(2 * time.Second):
		t.Fatal("watch not released after the engine context was cancelled")
	}
	searcher.mu.Lock()
	defer searcher.mu.Unlock()
	if searcher.holds != 0 {
		t.Fatalf("holds = %d after cancel, want 0", searcher.holds)
	}
}

func TestDNSQuestionName(t *testing.T) {
	msg := new(mdns.Msg)
	msg.SetQuestion("Lv-Api.Example.com.", mdns.TypeAAAA)
	raw, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	if got := dnsQuestionName(raw); got != "Lv-Api.Example.com" {
		t.Errorf("got %q", got)
	}
	// Truncated mid-label, header only, and QDCOUNT=0 all yield "".
	if got := dnsQuestionName(raw[:15]); got != "" {
		t.Errorf("truncated query parsed as %q", got)
	}
	if got := dnsQuestionName(raw[:12]); got != "" {
		t.Errorf("header-only query parsed as %q", got)
	}
	noQ := append([]byte(nil), raw...)
	noQ[4], noQ[5] = 0, 0
	if got := dnsQuestionName(noQ); got != "" {
		t.Errorf("QDCOUNT=0 parsed as %q", got)
	}
	// A compression pointer in the question is not something a client sends;
	// refuse to walk it.
	ptr := append(append([]byte(nil), raw[:12]...), 0xC0, 0x0C, 0, 1, 0, 1)
	if got := dnsQuestionName(ptr); got != "" {
		t.Errorf("pointer question parsed as %q", got)
	}
}
