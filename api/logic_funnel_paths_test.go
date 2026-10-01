package center

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFunnelPaths_Guard(t *testing.T) {
	require.NotEmpty(t, funnelPathRegistry)
	keyRe := regexp.MustCompile(`^[a-z_]+$`)
	seen := map[string]bool{}
	for _, p := range funnelPathRegistry {
		assert.Regexp(t, keyRe, p.Key)
		assert.False(t, seen[p.Key], "duplicate key %q", p.Key)
		seen[p.Key] = true
		for _, word := range []string{"kaitu", "overleap"} {
			assert.NotContains(t, strings.ToLower(p.Key), word, "path key must be brand-agnostic")
		}
		assert.NotEmpty(t, p.Title, p.Key)
		assert.NotEmpty(t, p.Question, p.Key)
		assert.GreaterOrEqual(t, len(p.Steps), 2, p.Key)
		assert.Greater(t, p.Window, time.Duration(0), p.Key)
		for i, s := range p.Steps {
			assert.NotEmpty(t, s.Label, "%s step %d", p.Key, i)
			require.NotEmpty(t, s.Events, "%s step %d", p.Key, i)
			for _, ev := range s.Events {
				def, ok := funnelEventDefByName(ev)
				require.True(t, ok, "%s step %d: event %q is not in funnelEventRegistry", p.Key, i, ev)
				if s.Surface != "" {
					// 带面过滤的步骤里，每个事件都必须真能在该面上出现，否则该步永远命中不了。
					assert.Contains(t, def.Surfaces, s.Surface, "%s step %d: %q never occurs on surface %q", p.Key, i, ev, s.Surface)
				}
			}
		}
	}
}

// 注册表内容逐字锁定（brief 的表）。
func TestFunnelPaths_RegistryContent(t *testing.T) {
	type step struct {
		label, surface string
		events         []string
	}
	views := []string{"page_view", "pricing_view", "checkout_view", "install_view", "welcome_view"}
	day := 24 * time.Hour
	want := []struct {
		key    string
		window time.Duration
		steps  []step
	}{
		{"web_purchase", 14 * day, []step{
			{"访问", "web", views}, {"看定价", "", []string{"pricing_view"}}, {"选套餐", "web", []string{"plan_select"}},
			{"发起支付", "", []string{"checkout_start"}}, {"付款", "", []string{"purchase"}},
		}},
		{"web_checkout_auth", 24 * time.Hour, []step{
			{"发验证码", "web", []string{"auth_code_sent"}}, {"登录成功", "web", []string{"auth_done"}},
			{"发起支付", "", []string{"checkout_start"}},
		}},
		{"web_install", 7 * day, []step{
			{"访问", "web", views}, {"安装页", "", []string{"install_view"}}, {"点下载", "", []string{"install_click"}},
		}},
		{"app_activation", 7 * day, []step{
			{"首次打开", "", []string{"app_first_open"}}, {"登录", "app", []string{"auth_done"}},
			{"尝试连接", "", []string{"first_connect_attempt"}}, {"连上", "", []string{"first_connect_ok"}},
		}},
		{"app_purchase", 7 * day, []step{
			{"付费墙", "", []string{"paywall_view"}}, {"选套餐", "app", []string{"plan_select"}},
			{"发起支付", "", []string{"checkout_start"}}, {"付款", "", []string{"purchase"}},
		}},
		{"post_purchase_activation", 7 * day, []step{
			{"付款", "", []string{"purchase"}}, {"连上", "", []string{"first_connect_ok"}},
		}},
	}
	require.Len(t, funnelPathRegistry, len(want))
	for i, w := range want {
		p := funnelPathRegistry[i]
		assert.Equal(t, w.key, p.Key)
		assert.Equal(t, w.window, p.Window, w.key)
		require.Len(t, p.Steps, len(w.steps), w.key)
		for j, ws := range w.steps {
			assert.Equal(t, ws.label, p.Steps[j].Label, "%s step %d", w.key, j)
			assert.Equal(t, ws.surface, p.Steps[j].Surface, "%s step %d", w.key, j)
			assert.Equal(t, ws.events, p.Steps[j].Events, "%s step %d", w.key, j)
			assert.Empty(t, p.Steps[j].UtmSource, "%s step %d", w.key, j)
		}
	}
}

// viewsWeb 从事件注册表派生：新增一个 web 面 view 事件，访问步自动包含它。
func TestFunnelPaths_ViewsWebDerivedFromRegistry(t *testing.T) {
	var want []string
	for _, d := range funnelEventRegistry {
		if d.Kind == FunnelKindView && slices.Contains(d.Surfaces, FunnelSurfaceWeb) {
			want = append(want, d.Name)
		}
	}
	assert.Equal(t, want, funnelViewsWeb())
	assert.NotContains(t, funnelViewsWeb(), "paywall_view")  // app 面的 view
	assert.NotContains(t, funnelViewsWeb(), "install_click") // web 面的 action
}

func TestFunnelPaths_ByKey(t *testing.T) {
	p, ok := funnelPathByKey("web_purchase")
	require.True(t, ok)
	assert.Equal(t, "web_purchase", p.Key)
	_, ok = funnelPathByKey("nope")
	assert.False(t, ok)
	_, ok = funnelPathByKey("")
	assert.False(t, ok)
}

func TestFunnelPaths_EventNames(t *testing.T) {
	p, _ := funnelPathByKey("web_purchase")
	behaviors, facts := p.eventNames()
	assert.Equal(t, []string{"page_view", "pricing_view", "checkout_view", "install_view", "welcome_view", "plan_select", "checkout_start"}, behaviors)
	assert.Equal(t, []string{"purchase"}, facts)

	p, _ = funnelPathByKey("post_purchase_activation")
	behaviors, facts = p.eventNames()
	assert.Equal(t, []string{"first_connect_ok"}, behaviors)
	assert.Equal(t, []string{"purchase"}, facts)

	// 每条路径：去重，且两类合起来正好是各步事件的并集。
	for _, p := range funnelPathRegistry {
		behaviors, facts := p.eventNames()
		all := append(append([]string{}, behaviors...), facts...)
		seen := map[string]bool{}
		for _, e := range all {
			assert.False(t, seen[e], "%s: %q listed twice", p.Key, e)
			seen[e] = true
		}
		for _, e := range behaviors {
			assert.NotEqual(t, FunnelKindFact, funnelEventKind(e))
		}
		for _, e := range facts {
			assert.Equal(t, FunnelKindFact, funnelEventKind(e))
		}
		for _, s := range p.Steps {
			for _, e := range s.Events {
				assert.True(t, seen[e], "%s: %q missing from eventNames", p.Key, e)
			}
		}
	}
}

func TestFunnelGroupDims(t *testing.T) {
	assert.Equal(t, []string{"source", "utm_campaign", "country", "os", "device", "app_version", "paywall_source", "plan", "channel"}, funnelGroupDims)
}
