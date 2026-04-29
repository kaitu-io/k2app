//go:build linux

package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/subscription"
)

// TestGateway_DoUp_ResolvesK2subs verifies that when cfg.Routes contains
// a k2subs:// URL, doUp fetches the subscription, replaces the route with
// a k2v5:// URL before passing to engine, but the persisted state retains
// the original k2subs:// URL.
//
// This task only wires Manager onto Gateway — we don't call doUp here.
// Instead we drive Resolve directly to prove the field is reachable and
// behaves end-to-end against a real subs HTTP server.
//
// Adaptation from plan: the plan-supplied snippet used http (NewServer) and
// SetCacheDir on the constructor-provided Manager. k2subs always upgrades
// to HTTPS, and the default http.Client rejects httptest's self-signed cert.
// We follow task adaptation (a): build a fresh Manager scoped to the test
// (TLS-trusting httpClient + t.TempDir() cache) and overwrite g.subsMgr.
// The point — gateway.Gateway carries a *subscription.Manager — is preserved.
func TestGateway_DoUp_ResolvesK2subs(t *testing.T) {
	subsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tunnels":[{"url":"k2v5://udid:tok@picked.example/?ech=x","weight":1}]}`))
	}))
	defer subsServer.Close()

	subsURL := "k2subs://u:t@" + strings.TrimPrefix(subsServer.URL, "https://") + "/"

	cfg := &GatewayConfig{
		ClientConfig: config.ClientConfig{
			Routes: []config.RouteConfig{
				{Via: subsURL, Match: rule.MatchConfig{All: true}},
			},
		},
	}

	g := New("test", "test", "linux/amd64")
	if g.subsMgr == nil {
		t.Fatal("New() did not initialize subsMgr")
	}
	// Replace with a test-scoped Manager: TLS-trusting client + temp cache dir.
	g.subsMgr = subscription.NewManager(t.TempDir(), subsServer.Client(), nil)

	// Don't actually call doUp (which spins up engine); test the helper.
	sessions, resolved, err := g.subsMgr.Resolve(&cfg.ClientConfig)
	if err != nil {
		t.Fatalf("Resolve err: %v", err)
	}
	defer g.subsMgr.CloseAll()

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	// Subscription.injectCreds rewrites tunnel URL userinfo with the
	// subs URL's creds — so JSON's "udid:tok" is replaced by "u:t".
	if got, want := resolved[0], "k2v5://u:t@picked.example/?ech=x"; got != want {
		t.Fatalf("resolved[0] = %q, want %q", got, want)
	}
	if cfg.Routes[0].Via != subsURL {
		t.Fatalf("original cfg should retain k2subs:// — got %q", cfg.Routes[0].Via)
	}
}
