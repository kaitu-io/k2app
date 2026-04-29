package subscription

import (
	"testing"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
)

func TestResolve_NoK2subsRoutes_NoOp(t *testing.T) {
	cfg := &config.ClientConfig{
		Routes: []config.RouteConfig{
			{Via: "k2v5://udid:tok@a.example/?ech=x", Match: rule.MatchConfig{All: true}},
			{Via: "direct"},
		},
	}
	mgr := NewManager(t.TempDir(), nil, nil)
	sessions, resolved, err := mgr.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve err: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions, got %d", len(sessions))
	}
	if len(resolved) != 0 {
		t.Fatalf("expected empty resolved map, got %v", resolved)
	}
}

func TestApplyResolved_NoMutationOnEmpty(t *testing.T) {
	cfg := &config.ClientConfig{
		Routes: []config.RouteConfig{{Via: "k2v5://x"}},
	}
	got := ApplyResolved(cfg, map[int]string{})
	if got != cfg {
		t.Fatalf("ApplyResolved should return cfg unchanged when resolved is empty")
	}
}

func TestApplyResolved_ReplacesK2subsWithK2v5(t *testing.T) {
	cfg := &config.ClientConfig{
		Routes: []config.RouteConfig{
			{Via: "k2subs://example.com/api/subs"},
			{Via: "direct"},
		},
	}
	got := ApplyResolved(cfg, map[int]string{0: "k2v5://udid:tok@picked.example/?ech=x"})

	if got == cfg {
		t.Fatalf("ApplyResolved must return a new ClientConfig (no mutation)")
	}
	if cfg.Routes[0].Via != "k2subs://example.com/api/subs" {
		t.Fatalf("original cfg mutated: routes[0].via = %q", cfg.Routes[0].Via)
	}
	if got.Routes[0].Via != "k2v5://udid:tok@picked.example/?ech=x" {
		t.Fatalf("resolved cfg routes[0].via = %q", got.Routes[0].Via)
	}
	if got.Routes[1].Via != "direct" {
		t.Fatalf("non-k2subs route should pass through, got %q", got.Routes[1].Via)
	}
}
