package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kaitu-io/k2/config"
)

// newTestSubSession builds a subSession wired to a httptest TLS server
// that returns the supplied tunnel set. The server stays running for the
// life of the test.
func newTestSubSession(t *testing.T, tunnels []config.Tunnel) (*subSession, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Tunnels []config.Tunnel `json:"tunnels"`
			Refresh int             `json:"refresh"`
		}{Tunnels: tunnels, Refresh: 1800}
		_ = json.NewEncoder(w).Encode(resp)
	}))

	raw := "k2subs://u:p@" + srv.Listener.Addr().String() + "/api"
	sub, err := config.NewSubscription(raw, t.TempDir())
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	// Force the subscription to use the test server (NewSubscription parses
	// the Authority from raw, but we want the TLS cert from httptest).
	// Fetch via the test server's HTTP client.
	if err := fetchViaTestServer(sub, srv); err != nil {
		t.Fatalf("Fetch via test server: %v", err)
	}
	return &subSession{sub: sub}, srv
}

// fetchViaTestServer uses reflection-free internals: we already exported
// enough via Fetch. Point s.endpoint at the test server's URL.
func fetchViaTestServer(sub *config.Subscription, srv *httptest.Server) error {
	// The httptest server uses a self-signed cert — use srv.Client() to trust it.
	// Construct a minimal k2subs URL whose endpoint derives to srv.URL would
	// require changing Subscription internals. Instead use Subscription's
	// public Fetch which takes an http.Client — use srv.Client which trusts
	// the test cert. But the endpoint is still the real raw parsed URL,
	// which DOES point at srv.Listener.Addr(), so the TLS connect goes to
	// the right place. srv.Client handles cert verification.
	return sub.Fetch(context.Background(), srv.Client())
}

// TestNextURL_ExcludesCurrentAndPicksAnother verifies the core contract:
// calling NextURL with currentURL adds it to the session's excluded set,
// then returns a DIFFERENT URL from sub.Pick.
func TestNextURL_ExcludesCurrentAndPicksAnother(t *testing.T) {
	tunnels := []config.Tunnel{
		{URL: "k2v5://a.example:443", Weight: 1},
		{URL: "k2v5://b.example:443", Weight: 1},
	}
	sess, srv := newTestSubSession(t, tunnels)
	defer srv.Close()

	d := &Daemon{}
	d.subs = []*subSession{sess}

	// After subscription.Fetch, credentials get injected into each tunnel
	// URL. We need to query sub.Tunnels() to know the actual stored URLs.
	stored := sess.sub.Tunnels()
	if len(stored) != 2 {
		t.Fatalf("stored tunnels = %d, want 2", len(stored))
	}

	// First call: pass the first stored URL as "current".
	first := stored[0].URL
	next, err := d.NextURL(first, errors.New("dead"))
	if err != nil {
		t.Fatalf("NextURL (first): %v", err)
	}
	if next == first {
		t.Errorf("NextURL returned the excluded URL: %q", next)
	}
	// The exclude set should now contain `first`.
	sess.excludeMu.Lock()
	if len(sess.excluded) != 1 || sess.excluded[0] != first {
		t.Errorf("excluded after first call = %+v, want [%q]", sess.excluded, first)
	}
	sess.excludeMu.Unlock()

	// Second call: exclude both tunnels → should exhaust.
	_, err = d.NextURL(next, errors.New("also dead"))
	if err == nil {
		t.Fatal("expected exhaustion after excluding both tunnels")
	}
	sess.excludeMu.Lock()
	if len(sess.excluded) != 2 {
		t.Errorf("excluded after second call = %d, want 2", len(sess.excluded))
	}
	sess.excludeMu.Unlock()
}

// TestNextURL_UnknownURLReturnsErrNoSubscription ensures the defensive
// "unknown URL" path: engine asks about a URL that doesn't belong to any
// active subscription (race with doDown clearing d.subs).
func TestNextURL_UnknownURLReturnsErrNoSubscription(t *testing.T) {
	tunnels := []config.Tunnel{{URL: "k2v5://a.example:443", Weight: 1}}
	sess, srv := newTestSubSession(t, tunnels)
	defer srv.Close()

	d := &Daemon{}
	d.subs = []*subSession{sess}

	_, err := d.NextURL("k2v5://unknown.example:443", errors.New("x"))
	if !errors.Is(err, ErrNoSubscription) {
		t.Errorf("got %v, want ErrNoSubscription", err)
	}
}

// TestNextURL_DuplicateExcludesAreNotDuplicated validates the
// already-excluded dedup logic.
func TestNextURL_DuplicateExcludesAreNotDuplicated(t *testing.T) {
	tunnels := []config.Tunnel{
		{URL: "k2v5://a.example:443", Weight: 1},
		{URL: "k2v5://b.example:443", Weight: 1},
	}
	sess, srv := newTestSubSession(t, tunnels)
	defer srv.Close()

	d := &Daemon{}
	d.subs = []*subSession{sess}
	stored := sess.sub.Tunnels()
	first := stored[0].URL

	_, _ = d.NextURL(first, errors.New("x"))
	_, _ = d.NextURL(first, errors.New("x"))

	sess.excludeMu.Lock()
	count := 0
	for _, u := range sess.excluded {
		if u == first {
			count++
		}
	}
	sess.excludeMu.Unlock()
	if count != 1 {
		t.Errorf("URL %q appears in excluded %d times, want 1", first, count)
	}
}

// TestFindSessionByURL_Match covers the lookup helper's happy path.
func TestFindSessionByURL_Match(t *testing.T) {
	tunnels := []config.Tunnel{{URL: "k2v5://x.example:443", Weight: 1}}
	sess, srv := newTestSubSession(t, tunnels)
	defer srv.Close()

	stored := sess.sub.Tunnels()
	got := findSessionByURL([]*subSession{sess}, stored[0].URL)
	if got != sess {
		t.Errorf("got %p, want %p", got, sess)
	}
}

func TestFindSessionByURL_NoMatch(t *testing.T) {
	tunnels := []config.Tunnel{{URL: "k2v5://x.example:443", Weight: 1}}
	sess, srv := newTestSubSession(t, tunnels)
	defer srv.Close()

	got := findSessionByURL([]*subSession{sess}, "k2v5://nonexistent:443")
	if got != nil {
		t.Errorf("got %p, want nil", got)
	}
}
