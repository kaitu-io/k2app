package center

// 转化漏斗事件注册表：客户端可上报的行为事件白名单 + 事实事件名单。
// 事实事件（fact）只能由服务端从权威表投影产生，任何客户端面上报都被丢弃。

const (
	FunnelSurfaceWeb = "web"
	FunnelSurfaceApp = "app"
)

const (
	FunnelKindView   = "view"
	FunnelKindAction = "action"
	FunnelKindFact   = "fact"
)

type funnelEventDef struct {
	Name     string
	Surfaces []string
	Kind     string
}

var (
	funnelWeb     = []string{FunnelSurfaceWeb}
	funnelApp     = []string{FunnelSurfaceApp}
	funnelWebApp  = []string{FunnelSurfaceWeb, FunnelSurfaceApp}
	funnelNoSurfs = []string{}
)

// funnelEventRegistry 有序，原样导出进 contracts/api-contract.json。
var funnelEventRegistry = []funnelEventDef{
	{"page_view", funnelWeb, FunnelKindView},
	{"pricing_view", funnelWeb, FunnelKindView},
	{"checkout_view", funnelWeb, FunnelKindView},
	{"install_view", funnelWeb, FunnelKindView},
	{"welcome_view", funnelWeb, FunnelKindView},
	{"install_click", funnelWeb, FunnelKindAction},
	{"checkout_cancelled", funnelWeb, FunnelKindAction},
	{"refund_click", funnelWeb, FunnelKindAction},
	{"cancel_click", funnelWeb, FunnelKindAction},
	{"app_first_open", funnelApp, FunnelKindAction},
	{"first_connect_attempt", funnelApp, FunnelKindAction},
	{"first_connect_ok", funnelApp, FunnelKindAction},
	{"connect_ok", funnelApp, FunnelKindAction},
	{"manage_click", funnelApp, FunnelKindAction},
	{"login_view", funnelApp, FunnelKindView},
	{"paywall_view", funnelApp, FunnelKindView},
	{"plan_select", funnelWebApp, FunnelKindAction},
	{"auth_code_sent", funnelWebApp, FunnelKindAction},
	{"auth_done", funnelWebApp, FunnelKindAction},
	{"checkout_start", funnelWebApp, FunnelKindAction},
	{"signup", funnelNoSurfs, FunnelKindFact},
	{"purchase", funnelNoSurfs, FunnelKindFact},
	{"renewal", funnelNoSurfs, FunnelKindFact},
	{"refund", funnelNoSurfs, FunnelKindFact},
	{"trial_start", funnelNoSurfs, FunnelKindFact},
	{"cancel_request", funnelNoSurfs, FunnelKindFact},
	{"resume", funnelNoSurfs, FunnelKindFact},
	{"email_sent", funnelNoSurfs, FunnelKindFact},
}

func funnelEventDefByName(name string) (funnelEventDef, bool) {
	for _, d := range funnelEventRegistry {
		if d.Name == name {
			return d, true
		}
	}
	return funnelEventDef{}, false
}

// funnelEventAllowed: 未注册、面不匹配、fact 一律 false。
func funnelEventAllowed(name, surface string) bool {
	d, ok := funnelEventDefByName(name)
	if !ok || d.Kind == FunnelKindFact {
		return false
	}
	for _, s := range d.Surfaces {
		if s == surface {
			return true
		}
	}
	return false
}

// funnelEventKind: 未注册返回 ""。
func funnelEventKind(name string) string {
	d, _ := funnelEventDefByName(name)
	return d.Kind
}
