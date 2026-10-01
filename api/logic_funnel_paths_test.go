package center

import (
	"os"
	"path/filepath"
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
			{"访问", "web", views}, {"看定价", "", []string{"pricing_view"}},
			{"发起支付", "", []string{"checkout_start"}}, {"付款", "", []string{"purchase", "renewal"}},
		}},
		{"web_checkout_auth", 24 * time.Hour, []step{
			{"发验证码", "web", []string{"auth_code_sent"}}, {"登录成功", "web", []string{"auth_done"}},
			{"发起支付", "web", []string{"checkout_start"}},
		}},
		{"web_install", 7 * day, []step{
			{"访问", "web", views}, {"安装页", "", []string{"install_view"}}, {"点下载", "", []string{"install_click"}},
		}},
		{"app_activation", 7 * day, []step{
			{"首次打开", "", []string{"app_first_open"}}, {"登录", "app", []string{"auth_done"}},
			{"尝试连接", "", []string{"first_connect_attempt"}}, {"连上", "", []string{"first_connect_ok"}},
		}},
		{"app_purchase", 7 * day, []step{
			{"付费墙", "", []string{"paywall_view"}},
			{"发起支付", "", []string{"checkout_start"}}, {"付款", "", []string{"purchase", "renewal"}},
		}},
		{"post_purchase_activation", 7 * day, []step{
			{"付款", "", []string{"purchase"}}, {"连上", "", []string{"first_connect_ok", "connect_ok"}},
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
	assert.Equal(t, []string{"page_view", "pricing_view", "checkout_view", "install_view", "welcome_view", "checkout_start"}, behaviors)
	assert.Equal(t, []string{"purchase", "renewal"}, facts)

	p, _ = funnelPathByKey("post_purchase_activation")
	behaviors, facts = p.eventNames()
	assert.Equal(t, []string{"first_connect_ok", "connect_ok"}, behaviors)
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

// web_checkout_auth 量的是购买页的内联登录，不是全站登录：标题问句必须说清楚。
func TestFunnelPaths_CheckoutAuthQuestionSaysInline(t *testing.T) {
	p, ok := funnelPathByKey("web_checkout_auth")
	require.True(t, ok)
	assert.Contains(t, p.Question, "购买页")
	assert.Contains(t, p.Question, "内联登录")
}

// funnelStepEmitSites：每条路径的每一步 → 客户端在哪里发这一步的事件（相对仓库根），事实步为 nil。
//
// 这张表是一道评审闸门：路径里的每一步都必须是「走到下一步的人一定会产生」的事件——
// 页面一挂载就发的 view、唯一往前走的按钮、或服务端事实。**可选交互不能当步骤**：
// plan_select 曾是购买路径的一步，但默认选中套餐的人从不点它，漏斗在那一步把所有人都算成流失。
// 新增 / 修改步骤时在这里登记发射点，并确认它是无条件发的；这个测试只能校验
// 「登记了、文件在、文件里有这个事件名」，「无条件」要靠评审读发射点。
var funnelStepEmitSites = map[string][][]string{
	"web_purchase": {
		{"web/src/components/FunnelPageView.tsx"}, // 每个页面挂载即发
		{"web/src/app/[locale]/purchase/PurchaseClient.tsx", "web/src/app/[locale]/purchase/OverleapPurchaseClient.tsx", "web/src/app/[locale]/pricing/page.overleap.tsx"}, // 定价 / 购买页挂载即发
		{"web/src/app/[locale]/purchase/PurchaseClient.tsx", "web/src/app/[locale]/purchase/OverleapPurchaseClient.tsx"},                                                   // 下单按钮：唯一的付款入口，带 plan
		nil, // 事实
	},
	"web_checkout_auth": {
		{"web/src/app/[locale]/purchase/PurchaseClient.tsx"}, // 内联登录：发验证码
		{"web/src/app/[locale]/purchase/PurchaseClient.tsx"}, // 内联登录：登录成功
		{"web/src/app/[locale]/purchase/PurchaseClient.tsx"}, // 下单按钮
	},
	"web_install": {
		{"web/src/components/FunnelPageView.tsx"},
		{"web/src/app/[locale]/install/InstallClient.tsx"},   // 安装页挂载即发
		{"web/src/app/[locale]/install/platform-panels.tsx"}, // 下载按钮：这条路径的终点动作
	},
	"app_activation": {
		{"webapp/src/main.tsx"},                   // 启动即发（每设备一次）
		{"webapp/src/components/LoginDialog.tsx"}, // 登录成功
		{"webapp/src/stores/connection.store.ts"}, // 首次点连接
		{"webapp/src/stores/index.ts"},            // 首次连上
	},
	"app_purchase": {
		{"webapp/src/pages/Purchase.tsx"}, // 购买页挂载即发
		{"webapp/src/pages/Purchase.tsx", "webapp/src/components/stripe/StripePurchasePanel.tsx", "webapp/src/components/ios/IosSubscribePanel.tsx"}, // 各渠道的下单按钮，带 plan
		nil, // 事实
	},
	"post_purchase_activation": {
		nil,                            // 事实
		{"webapp/src/stores/index.ts"}, // 每次连上（connect_ok）+ 首次连上
	},
}

func TestFunnelPaths_EveryStepHasUnconditionalEmitSite(t *testing.T) {
	require.Len(t, funnelStepEmitSites, len(funnelPathRegistry), "every path must be listed in funnelStepEmitSites (and no stale ones)")
	for _, p := range funnelPathRegistry {
		sites, ok := funnelStepEmitSites[p.Key]
		require.True(t, ok, "path %q has no emit-site table entry", p.Key)
		require.Len(t, sites, len(p.Steps), "path %q: one emit-site row per step", p.Key)
		for i, s := range p.Steps {
			// 已知的可选交互：永远不能当步骤。
			assert.NotContains(t, s.Events, "plan_select", "%s step %d: plan_select is optional (default-selected plans never emit it)", p.Key, i)

			allFacts := true
			for _, ev := range s.Events {
				if funnelEventKind(ev) != FunnelKindFact {
					allFacts = false
				}
			}
			if allFacts {
				assert.Nil(t, sites[i], "%s step %d is a fact step: no client emit site", p.Key, i)
				continue
			}
			require.NotEmpty(t, sites[i], "%s step %d (%s): register the client emit site", p.Key, i, s.Label)
			for _, rel := range sites[i] {
				src, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(rel)))
				require.NoError(t, err, "%s step %d: emit site %s", p.Key, i, rel)
				found := false
				for _, ev := range s.Events {
					if strings.Contains(string(src), "'"+ev+"'") || strings.Contains(string(src), `"`+ev+`"`) {
						found = true
					}
				}
				assert.True(t, found, "%s step %d: %s emits none of %v", p.Key, i, rel, s.Events)
			}
		}
	}
	// plan_select 仍是注册的、照常记录的事件，只是不当步骤。
	assert.True(t, funnelEventAllowed("plan_select", FunnelSurfaceWeb))
	assert.True(t, funnelEventAllowed("plan_select", FunnelSurfaceApp))
}
