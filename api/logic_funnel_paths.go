package center

import (
	"slices"
	"time"
)

// 转化漏斗路径注册表：每条路径是一串有序步骤 + 一个从进入时刻起算的时间窗。
// 路径与品牌无关（品牌只是查询参数），key 里不出现任何品牌词。

// funnelStep 是路径里的一步。记录满足该步 = 事件名命中 Events 之一，且通过非空的过滤条件。
type funnelStep struct {
	Label     string
	Events    []string // 任一命中
	Surface   string   // "" = 不限；非空时事实（Surface==""）永远不匹配
	UtmSource string   // "" = 不限
	// PathPrefix："" = 不限；非空时只匹配 Path 以它开头的记录（web 事件的 Path 已去掉 locale 前缀）。
	// 事实与 app 事件没有 Path，永远不匹配。
	PathPrefix string
}

type funnelPath struct {
	Key, Title, Question string
	// Lane：这条路径在管理端「路线总览」里属于哪条路线——从哪个面进入（funnelLaneWeb / funnelLaneApp）。
	Lane string
	// Hop：路径中途跨面（网站 ↔ App）时，跨过去的那一步靠什么接上、接不上的是谁。不跨面为空。
	// 管理端原样显示在跨面的那一步旁边，所以必须写清「确定性 / 推断」与已知的漏算。
	Hop    string
	Steps  []funnelStep
	Window time.Duration // 从进入（第 1 步）时刻起算，后续每一步都必须落在窗内
}

const (
	funnelLaneWeb = "web"
	funnelLaneApp = "app"
)

// 每一步发生在哪里（管理端据此标出跨面的那一跳）。
const (
	funnelWhereWeb   = "web"   // 只在网站上
	funnelWhereApp   = "app"   // 只在 App 里
	funnelWhereAny   = "any"   // 网站或 App 都算
	funnelWhereFacts = "facts" // 服务端事实（注册 / 付款），与在哪个面发生无关
)

// where 由步骤的面过滤与事件的可上报面推出，不手写：改了事件注册表，标注跟着变。
func (s funnelStep) where() string {
	if s.Surface != "" {
		return s.Surface
	}
	web, app, facts := false, false, false
	for _, e := range s.Events {
		d, _ := funnelEventDefByName(e)
		if d.Kind == FunnelKindFact {
			facts = true
		}
		web = web || slices.Contains(d.Surfaces, FunnelSurfaceWeb)
		app = app || slices.Contains(d.Surfaces, FunnelSurfaceApp)
	}
	switch {
	case web && app:
		return funnelWhereAny
	case web:
		return funnelWhereWeb
	case app:
		return funnelWhereApp
	case facts:
		return funnelWhereFacts
	}
	return funnelWhereAny
}

const funnelDay = 24 * time.Hour

// funnelUtmSourceApp：App 打开自家官网时链接上带的 utm_source（webapp utils/site-link.ts 的 APP_UTM_SOURCE）。
const funnelUtmSourceApp = "app"

// funnelViewsWeb 返回事件注册表里所有 web 面的 view 事件（注册表顺序）。
func funnelViewsWeb() []string {
	var out []string
	for _, d := range funnelEventRegistry {
		if d.Kind == FunnelKindView && slices.Contains(d.Surfaces, FunnelSurfaceWeb) {
			out = append(out, d.Name)
		}
	}
	return out
}

// funnelPaidFacts：购买路径的末步。首购与续费都算「付款」——回头客走完同一条路径时
// 产生的是 renewal 而不是 purchase，只认 purchase 会把他们全算成流失。
var funnelPaidFacts = []string{funnelFactPurchase, funnelFactRenewal}

// 步骤只能用「走到下一步的人一定会产生」的事件（挂载即发的 view、唯一往前走的按钮、事实）。
// 可选交互不能当步骤：plan_select 照常注册和记录，但默认选中套餐的人从不发它，所以不在任何路径里。
// 发射点登记在 logic_funnel_paths_test.go 的 funnelStepEmitSites。
//
// funnelPathRegistry 有序。funnelEventRegistry 在同包的变量初始化里先于它完成（Go 按依赖排序）。
var funnelPathRegistry = []funnelPath{
	{
		Key: "web_purchase", Title: "网站购买", Question: "访问网站的人里，有多少最终付款？卡在哪一步？",
		Lane:   funnelLaneWeb,
		Window: 14 * funnelDay,
		Steps: []funnelStep{
			{Label: "访问", Events: funnelViewsWeb(), Surface: FunnelSurfaceWeb},
			{Label: "看定价", Events: []string{"pricing_view"}},
			{Label: "发起支付", Events: []string{"checkout_start"}},
			{Label: "付款", Events: funnelPaidFacts},
		},
	},
	{
		Key: "web_checkout_auth", Title: "网站结账登录", Question: "在购买页内联登录（发验证码 → 登录）的人里，有多少走到了发起支付？只量购买页上的内联登录，不含站内其它登录入口。",
		Lane:   funnelLaneWeb,
		Window: 24 * time.Hour,
		Steps: []funnelStep{
			{Label: "发验证码", Events: []string{"auth_code_sent"}, Surface: FunnelSurfaceWeb},
			{Label: "登录成功", Events: []string{"auth_done"}, Surface: FunnelSurfaceWeb},
			// 这条路径诊断的是网站内联登录，所以末步也限 web 面（购买路径的 checkout_start 不限面）。
			{Label: "发起支付", Events: []string{"checkout_start"}, Surface: FunnelSurfaceWeb},
		},
	},
	{
		Key: "web_install", Title: "网站下载", Question: "访问网站的人里，有多少走到了点击下载？",
		Lane:   funnelLaneWeb,
		Window: 7 * funnelDay,
		Steps: []funnelStep{
			{Label: "访问", Events: funnelViewsWeb(), Surface: FunnelSurfaceWeb},
			{Label: "安装页", Events: []string{"install_view"}},
			{Label: "点下载", Events: []string{"install_click"}},
		},
	},
	{
		// 网页 → App：第 2 步起是 App 侧身份（did），与网页 sid 之间没有确定性关联，
		// 所以不带 ?inferred=1 时第 2 步基本为 0；带上后由推断归因（ip_os_v1）接上，结果需标注「推断」。
		Key: "web_to_app", Title: "网站下载到应用", Question: "在网站点了下载的人里，有多少打开了应用、注册 / 登录并最终付款？（依赖推断归因）",
		Lane: funnelLaneWeb, Hop: "网站 → App：网页访客（sid）与 App 设备（did）之间没有确定性关联。不开「含推断归因」时，只有在网站和 App 都登录过同一账号的人才接得上；开了之后再按「同出口 + 同平台、24 小时内」推断（仅开途；Overleap 官网不记 IP，不做推断）。",
		Window: 14 * funnelDay,
		Steps: []funnelStep{
			{Label: "点下载", Events: []string{"install_click"}, Surface: FunnelSurfaceWeb},
			{Label: "首次打开", Events: []string{"app_first_open"}},
			{Label: "注册 / 登录", Events: []string{funnelFactSignup, "auth_done"}},
			{Label: "付款", Events: funnelPaidFacts},
		},
	},
	{
		// 长尾指南的归因：进入记录就是指南页本身（搜索进来的人第一条 page_view 没有 utm），
		// 所以按 landing_path 分组即「每篇指南带来了多少注册 / 付款」。
		// 已登录的老用户读完直接买不会产生第 2 步——看他们用 web_purchase 按 landing_path 分组。
		// 局限：读完去下载 App、在 App 里注册 / 付款的人，App 侧身份（did）与网页 sid 没有确定性关联，
		// 默认算作流失；?inferred=1 时由推断归因（ip_os_v1）接上一部分。
		Key: "guide_conversion", Title: "指南转化", Question: "读过指南的人里，有多少注册 / 登录并最终付款？按落地页分组看每篇指南。",
		Lane: funnelLaneWeb, Hop: "读完指南去 App 里注册 / 登录的人，只有网页上也登录过才接得上，否则算作流失；开「含推断归因」可再接上在网站点过下载的一部分（仅开途）。",
		Window: 14 * funnelDay,
		Steps: []funnelStep{
			{Label: "读指南", Events: []string{"page_view"}, Surface: FunnelSurfaceWeb, PathPrefix: "/guides/"},
			{Label: "注册 / 登录", Events: []string{funnelFactSignup, "auth_done"}},
			{Label: "付款", Events: funnelPaidFacts},
		},
	},
	{
		// X 广告着陆页 /go/<码>：分组用 landing_path（= 归因码，A/B = /go/xa1 vs /go/xb1，扫码 = /go/xa1q；Mac 组 xa1m / xb1m，扫码 xa1mq / xb1mq）。
		// 不要把 /go/ 加进 funnelCodePathPrefixes，否则码被折叠成 *、A/B 分不开。
		// 首次打开 / 开始试用 / 付款只能靠推断归因接上，只是下限；A/B 胜负看确定性的上半段。
		Key: "ad_landing", Title: "广告着陆", Question: "广告点进来的人里，有多少开始试用、最终付款？按 landing_path（= 归因码）分组；A / B 只在同一设备组内比较（iPhone 组 xa1 vs xb1，Mac 组 xa1m(+xa1mq) vs xb1m(+xb1mq)）。",
		Lane: funnelLaneWeb, Hop: "网页 → App 依赖推断归因（ip_os_v1，仅开途）；Mac 扫码者在手机上是新的 sid，以 <码>q 路径单独成组。",
		Window: 14 * funnelDay,
		Steps: []funnelStep{
			{Label: "访问", Events: []string{"page_view"}, Surface: FunnelSurfaceWeb, PathPrefix: "/go/"},
			{Label: "点获取 / 看到二维码", Events: []string{"install_click", "qr_view"}},
			{Label: "首次打开", Events: []string{"app_first_open"}},
			{Label: "开始试用", Events: []string{"trial_start"}},
			{Label: "付款", Events: funnelPaidFacts},
		},
	},
	{
		Key: "app_activation", Title: "应用激活", Question: "装好应用的人里，有多少真正连上了？",
		Lane:   funnelLaneApp,
		Window: 7 * funnelDay,
		Steps: []funnelStep{
			{Label: "首次打开", Events: []string{"app_first_open"}},
			// 开途 iOS 新壳免登录建号（device_account_created，服务端写）与真实身份登录并列（spec §4.7）。
			{Label: "建号或登录", Events: []string{"auth_done", funnelEventDeviceAccountCreated}, Surface: FunnelSurfaceApp},
			{Label: "尝试连接", Events: []string{"first_connect_attempt"}},
			{Label: "连上", Events: []string{"first_connect_ok"}},
			// identity_bound（设备账号补上首个身份）只注册、照常记录，不当步骤：安卓 / 桌面 / 邮箱用户永远不发，
			// 放进来会改写「走完」的含义。
		},
	},
	{
		// 试用到期由 Apple 自动扣费，产生的是 Apple 入账的 purchase 事实；窗口 = 7 天试用 + Apple 扣费重试的头几天。
		// 关闭自动续费、扣费重试中等中间状态漏斗看不到，看 /app/stats/trials。
		Key: "app_trial", Title: "试用转付费", Question: "开了 Apple 免费试用的人里，有多少试用到期后付了款？",
		Lane:   funnelLaneApp,
		Window: 14 * funnelDay,
		Steps: []funnelStep{
			{Label: "开始试用", Events: []string{funnelFactTrial}},
			{Label: "付款", Events: funnelPaidFacts},
		},
	},
	{
		Key: "app_purchase", Title: "应用内购买", Question: "看到付费墙的人里，有多少最终付款？",
		Lane: funnelLaneApp, Hop: "非 iOS 平台在 App 内点「支付」后跳到外部浏览器的支付页付款（开途 NextPay，Overleap Stripe），iOS 走 Apple 内购不离开 App；付款事实按登录账号归属，与在哪个面付的无关，是确定性的。",
		Window: 7 * funnelDay,
		Steps: []funnelStep{
			{Label: "付费墙", Events: []string{"paywall_view"}},
			{Label: "发起支付", Events: []string{"checkout_start"}},
			{Label: "付款", Events: funnelPaidFacts},
		},
	},
	{
		// App 里打开自家官网的链接都带 utm_source=app（webapp utils/site-link.ts），进入记录就是那次带标记的访问。
		// 只量从 App 跳到网站之后在网站上走完的部分；在 App 里点了多少次链接不在这里（App 侧不发事件）。
		Key: "app_to_web", Title: "应用跳到网站", Question: "从 App 里点链接打开官网的人里，有多少看了定价、发起支付并付款？",
		Lane: funnelLaneApp, Hop: "App → 网站：靠链接上的 utm_source=app 标记识别，是确定性的；只覆盖带标记的新版 App（旧版打开的官网算作直接访问）。网页上登录后，付款按账号归属。",
		Window: 14 * funnelDay,
		Steps: []funnelStep{
			{Label: "从 App 打开官网", Events: funnelViewsWeb(), Surface: FunnelSurfaceWeb, UtmSource: funnelUtmSourceApp},
			{Label: "看定价", Events: []string{"pricing_view"}},
			{Label: "发起支付", Events: []string{"checkout_start"}},
			{Label: "付款", Events: funnelPaidFacts},
		},
	},
	{
		Key: "post_purchase_activation", Title: "付款后激活", Question: "付了款的人里，有多少在一周内连上了？",
		Lane:   funnelLaneApp,
		Window: 7 * funnelDay,
		Steps: []funnelStep{
			{Label: "付款", Events: []string{funnelFactPurchase}},
			// 老用户续费 / 换设备后不会再有 first_connect_ok：任何一次连上都算激活。
			{Label: "连上", Events: []string{"first_connect_ok", "connect_ok"}},
		},
	},
}

func funnelPathByKey(key string) (funnelPath, bool) {
	for _, p := range funnelPathRegistry {
		if p.Key == key {
			return p, true
		}
	}
	return funnelPath{}, false
}

// eventNames 返回路径用到的全部事件名：去重、保持步骤里首次出现的顺序，按 Kind 分成
// 行为事件（从 funnel_events 加载）和事实（从权威表投影）。
func (p funnelPath) eventNames() (behaviors, facts []string) {
	seen := make(map[string]bool)
	for _, s := range p.Steps {
		for _, e := range s.Events {
			if seen[e] {
				continue
			}
			seen[e] = true
			if funnelEventKind(e) == FunnelKindFact {
				facts = append(facts, e)
			} else {
				behaviors = append(behaviors, e)
			}
		}
	}
	return behaviors, facts
}
