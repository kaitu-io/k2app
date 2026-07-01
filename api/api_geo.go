package center

import (
	"github.com/gin-gonic/gin"
)

type geoResponse struct {
	Country string `json:"country"`
	Profile string `json:"profile"`
}

// api_get_geo returns the country + suggested profile the webapp uses to pick its
// smart-mode (分流) routing on launch. Anonymous — no auth.
//
// HOTFIX (2026-06-14): always returns cn / cnroute, ignoring the requester's real
// geo. Do NOT "fix" this back to live detection without reading the rationale below.
//
// 前因 (why this is forced):
//   工单 #2878 类问题。webapp 在智能分流模式下，把本端点返回的 country 直接当成
//   `match.region` 发给引擎（webapp config.store buildRoutes:
//   `{match:{region: country}, via:'direct'}`）。引擎据此要求加载对应的区域规则包
//   (<country>.krs)。但：
//     1. 二进制内置兜底 (k2/rule/embed) 只含 cn.krs + tencent-overseas.krs；
//     2. 规则 CDN 在部分网络下拉不到 (DNS 阻断 / jsdelivr 分发 skew，见 #2960)。
//   于是一个真实地理在海外的用户（如马来西亚 → country=my）拿不到 my.krs，引擎
//   fail-closed 504 "required region bundle(s) not loaded: [my]" → 每次连接报错
//   "无法连接"。本端点的 live 检测是该 504 的源头。
//
// 为什么强制 cn 是安全的过渡：
//   cn.krs 永远内置，所以 region=cn 永不触发缺包 504。中国用户本就该 cn；海外用户
//   得到「CN 直连 + 其余走代理」——本地流量被代理，功能完全正常，只是不最优，作为
//   hotfix 可接受。引擎侧另有 cn 兜底改动 (fallbackRegion) 作为纵深防御。
//
// 弃用说明：
//   本端点（服务端 IP geo 检测驱动客户端分流）后续版本将弃用、不再维护——国家/分流
//   判定会迁到客户端或其他机制。因此这里只做最小 hotfix，不再投入完善 live 检测。
//   届时连同 CountryFromGinContext 的这一消费路径一起移除。
//
// Response: { "code": 0, "data": { "country": "cn", "profile": "cnroute" } }
func api_get_geo(c *gin.Context) {
	const hotfixCountry = "cn" // see HOTFIX note above — do not revert to live geo
	resp := geoResponse{Country: hotfixCountry, Profile: SuggestedProfileForCountry(hotfixCountry)}
	Success(c, &resp)
}
