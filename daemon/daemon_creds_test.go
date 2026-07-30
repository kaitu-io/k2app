package daemon

import (
	"testing"

	"github.com/kaitu-io/k2/config"
)

func TestRewriteRouteVia(t *testing.T) {
	oldVia := "k2subs://u:oldtok@center.example/api/subs"
	newVia := "k2subs://u:newtok@center.example/api/subs"

	t.Run("RewritesMatchingRoute", func(t *testing.T) {
		cfg := &config.ClientConfig{Routes: []config.RouteConfig{
			{Via: "direct"},
			{Via: oldVia},
		}}
		if !rewriteRouteVia(cfg, oldVia, newVia) {
			t.Fatal("want rewrite to report true")
		}
		if cfg.Routes[1].Via != newVia {
			t.Fatalf("route not rewritten: %q", cfg.Routes[1].Via)
		}
		if cfg.Routes[0].Via != "direct" {
			t.Fatal("unrelated route must not change")
		}
	})

	t.Run("NoMatch_False", func(t *testing.T) {
		cfg := &config.ClientConfig{Routes: []config.RouteConfig{{Via: "k2v5://x@h:443"}}}
		if rewriteRouteVia(cfg, oldVia, newVia) {
			t.Fatal("no matching route — must report false")
		}
	})

	t.Run("NilAndDegenerate_False", func(t *testing.T) {
		if rewriteRouteVia(nil, oldVia, newVia) {
			t.Fatal("nil cfg must be a no-op")
		}
		cfg := &config.ClientConfig{Routes: []config.RouteConfig{{Via: oldVia}}}
		if rewriteRouteVia(cfg, oldVia, oldVia) {
			t.Fatal("identical old/new must be a no-op")
		}
	})
}

// TestPersistRotatedCreds_UpdatesLastConfig：回调把 d.lastConfig 里的 k2subs
// route 换成新 raw（saveLifecycleState 的磁盘写由既有 state 测试覆盖；本测试
// 锚定本 Task 的门——route 重写接线）。
func TestPersistRotatedCreds_UpdatesLastConfig(t *testing.T) {
	// persistRotatedCreds 在 updated=true 时经 saveLifecycleState→saveState 真实
	// 写盘（默认 os.TempDir()/k2/state.json，污染共享持久化状态）。按
	// k2/daemon/state.go:30-31 注释的方式注入 t.TempDir()——与 gateway 侧 Task 8
	// 对 stateDirOverride 的处理对称。
	prev := stateDir
	stateDir = func() string { return t.TempDir() }
	t.Cleanup(func() { stateDir = prev })

	oldVia := "k2subs://u:oldtok@center.example/api/subs"
	newVia := "k2subs://u:newtok@center.example/api/subs"

	d := &Daemon{}
	d.lastConfig = &config.ClientConfig{Routes: []config.RouteConfig{{Via: oldVia}}}

	d.persistRotatedCreds(oldVia, newVia)

	d.mu.RLock()
	got := d.lastConfig.Routes[0].Via
	d.mu.RUnlock()
	if got != newVia {
		t.Fatalf("lastConfig route = %q, want %q", got, newVia)
	}
}
