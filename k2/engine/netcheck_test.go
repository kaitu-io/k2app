package engine

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
)

// activeSpy returns an active() func yielding rs and a pointer counting calls.
func activeSpy(rs []checkResult) (func() []checkResult, *int) {
	n := 0
	return func() []checkResult { n++; return rs }, &n
}

func TestDecide_IfaceDownShortCircuits(t *testing.T) {
	now := time.Now()
	active, calls := activeSpy([]checkResult{checkMatched})
	v, tag := decideNet(netEvidence{IfaceDown: true, LastDirectOK: now, Now: now}, active)
	if v != netIfaceDown || tag != "iface" {
		t.Fatalf("got (%v, %q), want (netIfaceDown, iface)", v, tag)
	}
	if *calls != 0 {
		t.Fatalf("active called %d times, want 0", *calls)
	}
}

func TestDecide_RecentDirectIsOnline(t *testing.T) {
	now := time.Now()
	active, calls := activeSpy([]checkResult{checkNoResponse})
	v, tag := decideNet(netEvidence{LastDirectOK: now.Add(-9 * time.Second), Now: now}, active)
	if v != netOnline || tag != "passive" {
		t.Fatalf("got (%v, %q), want (netOnline, passive)", v, tag)
	}
	if *calls != 0 {
		t.Fatalf("active called %d times, want 0", *calls)
	}

	v, tag = decideNet(netEvidence{LastDirectOK: now.Add(-11 * time.Second), Now: now}, active)
	if *calls != 1 {
		t.Fatalf("stale LastDirectOK: active called %d times, want 1", *calls)
	}
	if v != netNoInternet || tag != "active" {
		t.Fatalf("got (%v, %q), want (netNoInternet, active)", v, tag)
	}

	// Zero LastDirectOK is never passive evidence, even if Now is also zero.
	active2, calls2 := activeSpy([]checkResult{checkMatched})
	if v, _ := decideNet(netEvidence{}, active2); v != netOnline || *calls2 != 1 {
		t.Fatalf("zero evidence: got %v with %d active calls, want online via active", v, *calls2)
	}
}

func TestDecide_ActiveAnyMatchedIsOnline(t *testing.T) {
	active, _ := activeSpy([]checkResult{checkNoResponse, checkMatched, checkOtherResponse})
	if v, tag := decideNet(netEvidence{Now: time.Now()}, active); v != netOnline || tag != "active" {
		t.Fatalf("got (%v, %q), want (netOnline, active)", v, tag)
	}
}

func TestDecide_ActiveOtherIsCaptive(t *testing.T) {
	active, _ := activeSpy([]checkResult{checkNoResponse, checkOtherResponse})
	if v, tag := decideNet(netEvidence{Now: time.Now()}, active); v != netCaptive || tag != "active" {
		t.Fatalf("got (%v, %q), want (netCaptive, active)", v, tag)
	}
}

func TestDecide_ActiveAllNoRespIsNoInternet(t *testing.T) {
	active, _ := activeSpy([]checkResult{checkNoResponse, checkNoResponse})
	if v, tag := decideNet(netEvidence{Now: time.Now()}, active); v != netNoInternet || tag != "active" {
		t.Fatalf("got (%v, %q), want (netNoInternet, active)", v, tag)
	}
}

func TestDecide_NoTargetsIsInconclusiveWait(t *testing.T) {
	active, _ := activeSpy([]checkResult{})
	if v, _ := decideNet(netEvidence{Now: time.Now()}, active); v != netNoInternet {
		t.Fatalf("got %v, want netNoInternet", v)
	}
	activeNil, _ := activeSpy(nil)
	if v, _ := decideNet(netEvidence{Now: time.Now()}, activeNil); v != netNoInternet {
		t.Fatalf("nil results: got %v, want netNoInternet", v)
	}
}

func TestVerdict_HoldErrorCodes(t *testing.T) {
	cases := []struct {
		v    netVerdict
		code int
	}{
		{netIfaceDown, 101},
		{netNoInternet, 106},
		{netCaptive, 107},
	}
	for _, c := range cases {
		e := c.v.holdError()
		if e == nil {
			t.Fatalf("%v: holdError nil, want %d", c.v, c.code)
		}
		if e.Code != c.code || e.Category != "network" {
			t.Fatalf("%v: got {%d, %q}, want {%d, network}", c.v, e.Code, e.Category, c.code)
		}
	}
	if e := netOnline.holdError(); e != nil {
		t.Fatalf("netOnline: holdError = %+v, want nil", e)
	}
	if ErrCodeNoInternet != 106 || ErrCodeCaptivePortal != 107 {
		t.Fatalf("code constants drifted: %d %d", ErrCodeNoInternet, ErrCodeCaptivePortal)
	}
}

// plainDialer is a thin wire.DirectDialer over net.Dialer for tests.
type plainDialer struct{ d net.Dialer }

func (p *plainDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return p.d.DialContext(ctx, network, addr)
}
func (p *plainDialer) ListenPacket(network, addr string) (net.PacketConn, error) {
	return net.ListenPacket(network, addr)
}

func loopbackResolve(ctx context.Context, host string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

// asHostURL rewrites an httptest URL (127.0.0.1:port) to use a hostname, so
// the test proves the checker resolves via resolve() rather than dialing the
// literal host.
func asHostURL(raw, path string) string {
	return strings.Replace(raw, "127.0.0.1", "netcheck.test", 1) + path
}

func TestNetChecker_Classification(t *testing.T) {
	var gotUA, gotCookie string
	mux := http.NewServeMux()
	mux.HandleFunc("/generate_204", func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotCookie = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/generate_204", http.StatusFound)
	})
	mux.HandleFunc("/success", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Success"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// A port that was listening and is now closed.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedURL := "http://netcheck.test:" + itoaPort(ln.Addr()) + "/generate_204"
	_ = ln.Close()

	targets := []config.NetcheckTarget{
		{URL: asHostURL(srv.URL, "/generate_204"), Status: 204},
		{URL: asHostURL(srv.URL, "/redirect"), Status: 204},
		{URL: asHostURL(srv.URL, "/success"), Status: 200, Body: "Success"},
		{URL: closedURL, Status: 204},
	}
	c := newNetChecker(targets, &plainDialer{}, loopbackResolve)
	rs, ds := c.run(context.Background())

	want := []checkResult{checkMatched, checkOtherResponse, checkMatched, checkNoResponse}
	if len(ds) != len(rs) {
		t.Fatalf("got %d durations for %d results", len(ds), len(rs))
	}
	if len(rs) != len(want) {
		t.Fatalf("got %d results, want %d", len(rs), len(want))
	}
	for i := range want {
		if rs[i] != want[i] {
			t.Errorf("target %d (%s): got %v, want %v", i, targets[i].URL, rs[i], want[i])
		}
	}
	if gotUA != "k2-netcheck" {
		t.Errorf("User-Agent = %q, want k2-netcheck", gotUA)
	}
	if gotCookie != "" {
		t.Errorf("unexpected Cookie header %q", gotCookie)
	}

	// Body mismatch with right status is "other", not matched.
	c2 := newNetChecker([]config.NetcheckTarget{
		{URL: asHostURL(srv.URL, "/success"), Status: 200, Body: "Nope"},
	}, &plainDialer{}, loopbackResolve)
	if rs, _ := c2.run(context.Background()); rs[0] != checkOtherResponse {
		t.Errorf("body mismatch: got %v, want other", rs[0])
	}

	s := formatCheckResults(targets, rs, ds)
	if !strings.Contains(s, "netcheck.test=matched/") || !strings.Contains(s, "netcheck.test=noResp/") ||
		!strings.Contains(s, "netcheck.test=other/") {
		t.Errorf("formatCheckResults = %q", s)
	}
	if got := formatCheckResults(targets[:2], []checkResult{checkMatched, checkNoResponse},
		[]time.Duration{12 * time.Millisecond, 3000 * time.Millisecond}); got != "netcheck.test=matched/12ms netcheck.test=noResp/3000ms" {
		t.Errorf("formatCheckResults with durations = %q", got)
	}
}

func TestNetChecker_RespectsTimeoutAndCtx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newNetChecker([]config.NetcheckTarget{
		{URL: asHostURL(srv.URL, "/generate_204"), Status: 204},
	}, &plainDialer{}, loopbackResolve)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	rs, ds := c.run(ctx)
	if el := time.Since(start); el > 500*time.Millisecond {
		t.Fatalf("run took %v, want < 500ms", el)
	}
	if len(ds) != 1 || ds[0] < 90*time.Millisecond || ds[0] > 500*time.Millisecond {
		t.Fatalf("per-target elapsed = %v, want ~100ms (the ctx deadline)", ds)
	}
	if len(rs) != 1 || rs[0] != checkNoResponse {
		t.Fatalf("got %v, want [noResp]", rs)
	}
}

func itoaPort(a net.Addr) string {
	_, p, _ := net.SplitHostPort(a.String())
	return p
}
