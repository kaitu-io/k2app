package subscription

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestResolveWarnsStaleCacheAge：fetch 失败但磁盘缓存可用时，除既有的
// 降级 Warn 外必须再打一条 DIAG: subs-stale-cache 且带 cacheAgeS 字段
// （spec §4.4 P5——降级保留，但必须报告"正在用多老的数据"）。
func TestResolveWarnsStaleCacheAge(t *testing.T) {
	cacheDir := t.TempDir()

	// 先用一个正常 server 完成一次成功 Fetch，制造磁盘缓存。
	okSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tunnels":[{"url":"k2v5://u:tok@h.example.com:443/"}],"refresh":600}`)
	}))
	host := strings.TrimPrefix(okSrv.URL, "https://")
	raw := "k2subs://u:tok@" + host + "/api/subs"
	seed, err := config.NewSubscription(raw, cacheDir)
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	if err := seed.Fetch(context.Background(), okSrv.Client()); err != nil {
		t.Fatalf("seed fetch: %v", err)
	}
	okSrv.Close() // 之后同一 endpoint 的 fetch 必然失败 → 走缓存降级

	// 捕获 slog 输出。
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	m := NewManager(cacheDir, nil, nil)
	cfg := &config.ClientConfig{Routes: []config.RouteConfig{{Via: raw}}}
	sessions, resolved, err := m.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve must degrade to cache, got err: %v", err)
	}
	t.Cleanup(m.CloseAll)
	if len(sessions) != 1 || len(resolved) != 1 {
		t.Fatalf("want 1 session + 1 resolved route, got %d/%d", len(sessions), len(resolved))
	}

	out := buf.String()
	if !strings.Contains(out, "DIAG: subs-stale-cache") {
		t.Fatalf("missing DIAG: subs-stale-cache in logs:\n%s", out)
	}
	if !strings.Contains(out, "cacheAgeS=") {
		t.Fatalf("stale-cache DIAG must carry cacheAgeS field:\n%s", out)
	}
}
