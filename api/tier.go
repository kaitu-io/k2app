package center

import "sort"

// Tier 枚举（pro 已废弃，使用 4 档划分）
const (
	TierLite     = "lite"
	TierBasic    = "basic"
	TierFamily   = "family"
	TierBusiness = "business"
)

// TierQuota 单个 tier 的配额定义
type TierQuota struct {
	MaxDevice       int `json:"maxDevice"`
	MaxRouterDevice int `json:"maxRouterDevice"`
	MaxLanClient    int `json:"maxLanClient"` // -1 表示无限
}

// TierInfo 完整 tier 元信息（用于 /api/tiers 响应）
type TierInfo struct {
	Name string `json:"name"`
	Rank int    `json:"rank"`
	TierQuota
}

// TierQuotas 单一事实源 —— 改配额必须发版
var TierQuotas = map[string]TierInfo{
	TierLite:     {Name: TierLite, Rank: 1, TierQuota: TierQuota{MaxDevice: 1, MaxRouterDevice: 0, MaxLanClient: 0}},
	TierBasic:    {Name: TierBasic, Rank: 2, TierQuota: TierQuota{MaxDevice: 5, MaxRouterDevice: 0, MaxLanClient: 0}},
	TierFamily:   {Name: TierFamily, Rank: 3, TierQuota: TierQuota{MaxDevice: 8, MaxRouterDevice: 1, MaxLanClient: 20}},
	TierBusiness: {Name: TierBusiness, Rank: 4, TierQuota: TierQuota{MaxDevice: 20, MaxRouterDevice: 3, MaxLanClient: -1}},
}

// ZeroQuota 过期/未付费用户的零配额
var ZeroQuota = TierQuota{}

// AllTiers 按 rank 升序返回所有 tier
func AllTiers() []TierInfo {
	out := make([]TierInfo, 0, len(TierQuotas))
	for _, t := range TierQuotas {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
	return out
}

// IsValidTier 校验字符串是否合法 tier
func IsValidTier(t string) bool {
	_, ok := TierQuotas[t]
	return ok
}
