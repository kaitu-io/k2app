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
}

type funnelPath struct {
	Key, Title, Question string
	Steps                []funnelStep
	Window               time.Duration // 从进入（第 1 步）时刻起算，后续每一步都必须落在窗内
}

const funnelDay = 24 * time.Hour

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

// funnelPathRegistry 有序。funnelEventRegistry 在同包的变量初始化里先于它完成（Go 按依赖排序）。
var funnelPathRegistry = []funnelPath{
	{
		Key: "web_purchase", Title: "网站购买", Question: "访问网站的人里，有多少最终付款？卡在哪一步？",
		Window: 14 * funnelDay,
		Steps: []funnelStep{
			{Label: "访问", Events: funnelViewsWeb(), Surface: FunnelSurfaceWeb},
			{Label: "看定价", Events: []string{"pricing_view"}},
			{Label: "选套餐", Events: []string{"plan_select"}, Surface: FunnelSurfaceWeb},
			{Label: "发起支付", Events: []string{"checkout_start"}},
			{Label: "付款", Events: []string{funnelFactPurchase}},
		},
	},
	{
		Key: "web_checkout_auth", Title: "网站结账登录", Question: "结账前的验证码登录这一关，流失了多少人？",
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
		Window: 7 * funnelDay,
		Steps: []funnelStep{
			{Label: "访问", Events: funnelViewsWeb(), Surface: FunnelSurfaceWeb},
			{Label: "安装页", Events: []string{"install_view"}},
			{Label: "点下载", Events: []string{"install_click"}},
		},
	},
	{
		Key: "app_activation", Title: "应用激活", Question: "装好应用的人里，有多少真正连上了？",
		Window: 7 * funnelDay,
		Steps: []funnelStep{
			{Label: "首次打开", Events: []string{"app_first_open"}},
			{Label: "登录", Events: []string{"auth_done"}, Surface: FunnelSurfaceApp},
			{Label: "尝试连接", Events: []string{"first_connect_attempt"}},
			{Label: "连上", Events: []string{"first_connect_ok"}},
		},
	},
	{
		Key: "app_purchase", Title: "应用内购买", Question: "看到付费墙的人里，有多少最终付款？",
		Window: 7 * funnelDay,
		Steps: []funnelStep{
			{Label: "付费墙", Events: []string{"paywall_view"}},
			{Label: "选套餐", Events: []string{"plan_select"}, Surface: FunnelSurfaceApp},
			{Label: "发起支付", Events: []string{"checkout_start"}},
			{Label: "付款", Events: []string{funnelFactPurchase}},
		},
	},
	{
		Key: "post_purchase_activation", Title: "付款后激活", Question: "付了款的人里，有多少在一周内连上了？",
		Window: 7 * funnelDay,
		Steps: []funnelStep{
			{Label: "付款", Events: []string{funnelFactPurchase}},
			{Label: "连上", Events: []string{"first_connect_ok"}},
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
