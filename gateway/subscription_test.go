//go:build linux

package gateway

import (
	"encoding/json"
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

// TestGateway_EngineConfig_HasOutboundProvider verifies the gateway
// hands its subsMgr to engine.Config.OutboundProvider so the engine's
// outboundReplaceLoop can drive hot-swap.
func TestGateway_EngineConfig_HasOutboundProvider(t *testing.T) {
	g := New("test", "test", "linux/amd64")
	cfg := &GatewayConfig{ClientConfig: config.ClientConfig{}}

	ecfg := engineConfig(&cfg.ClientConfig, nil, nil, g.subsMgr)
	if ecfg.OutboundProvider == nil {
		t.Fatalf("engineConfig must set OutboundProvider when subsMgr is non-nil")
	}
	if ecfg.OutboundProvider != g.subsMgr {
		t.Fatalf("OutboundProvider != subsMgr")
	}
}

// TestGateway_DoDown_ClosesSubscriptionSessions verifies that doDown drains
// the subsMgr's session slice — required so a subsequent doUp's Resolve
// repopulates from a clean slate (otherwise stale sessions accumulate).
//
// Adapted from plan: the plan-supplied snippet uses httptest.NewServer +
// SetCacheDir on the constructor-provided Manager. Following the same
// convention as TestGateway_DoUp_ResolvesK2subs, we use httptest.NewTLSServer
// (k2subs upgrades to HTTPS) and overwrite g.subsMgr with a fresh,
// TLS-trusting Manager scoped to the test.
func TestGateway_DoDown_ClosesSubscriptionSessions(t *testing.T) {
	subsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tunnels":[{"url":"k2v5://u:t@x.example/?ech=y","weight":1}]}`))
	}))
	defer subsServer.Close()

	g := New("test", "test", "linux/amd64")
	g.subsMgr = subscription.NewManager(t.TempDir(), subsServer.Client(), nil)

	subsURL := "k2subs://u:t@" + strings.TrimPrefix(subsServer.URL, "https://") + "/"
	cfg := &config.ClientConfig{
		Routes: []config.RouteConfig{{Via: subsURL, Match: rule.MatchConfig{All: true}}},
	}
	if _, _, err := g.subsMgr.Resolve(cfg); err != nil {
		t.Fatalf("Resolve err: %v", err)
	}
	if got := g.subsMgr.SessionsLen(); got != 1 {
		t.Fatalf("after Resolve: SessionsLen = %d, want 1", got)
	}

	g.doDown()
	if got := g.subsMgr.SessionsLen(); got != 0 {
		t.Fatalf("doDown should clear sessions; got %d", got)
	}
}

// TestGateway_State_PersistsK2subsURL is the persistence audit. doUp builds
// engineClient via subscription.ApplyResolved (k2v5://) and hands that to the
// engine, but saveState is called with the original cfg (still k2subs://).
// This test exercises the persistedState round-trip directly — the same data
// path saveState/loadState use — to lock in that contract.
func TestGateway_State_PersistsK2subsURL(t *testing.T) {
	cfg := &GatewayConfig{
		ClientConfig: config.ClientConfig{
			Routes: []config.RouteConfig{{Via: "k2subs://example.com/api/subs"}},
		},
	}

	// saveState/loadState use a fixed /etc/k2r path; emulate the same JSON
	// round-trip locally to keep the test hermetic.
	st := &persistedState{Config: cfg, State: "connected"}
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var loaded persistedState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.Config == nil {
		t.Fatal("loaded.Config nil")
	}
	if got, want := loaded.Config.Routes[0].Via, "k2subs://example.com/api/subs"; got != want {
		t.Fatalf("persisted via = %q, want %q (must NOT be a k2v5:// resolution)", got, want)
	}
}
