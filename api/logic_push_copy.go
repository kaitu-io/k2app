package center

import "strings"

// =====================================================================
// 交易类推送文案（spec 2026-11-03-ios-push-design §6.4）
// =====================================================================
//
// 规则：不写品牌名（iOS 通知自己显示 App 名，且开途中文面禁 "Kaitu" 裸词）；
// 不写金额、邮箱、订单号。两个品牌共用同一套文案。

type pushCopyKind string

const (
	pushCopyBillingRetry pushCopyKind = "billing_retry" // 续费扣款失败
	pushCopyRenewal3d    pushCopyKind = "renewal_3d"    // 会员 3 天后到期
	pushCopyExpired1d    pushCopyKind = "expired_1d"    // 会员已过期 1 天
)

type pushCopyText struct{ title, body string }

var pushCopyTable = map[pushCopyKind]map[string]pushCopyText{
	pushCopyBillingRetry: {
		"zh-CN": {"续费扣款未成功", "请更新 Apple ID 的付款方式，以免服务中断。"},
		"zh-TW": {"續費扣款未成功", "請更新 Apple ID 的付款方式，以免服務中斷。"},
		"en-US": {"Renewal payment failed", "Update your Apple ID payment method to avoid interruption."},
		"ja":    {"更新のお支払いに失敗しました", "サービスが中断しないよう、Apple ID のお支払い方法を更新してください。"},
	},
	pushCopyRenewal3d: {
		"zh-CN": {"会员 3 天后到期", "现在续费，连接不中断。"},
		"zh-TW": {"會員 3 天後到期", "現在續費，連線不中斷。"},
		"en-US": {"Your membership expires in 3 days", "Renew now to stay connected without interruption."},
		"ja":    {"メンバーシップの有効期限まであと3日です", "今すぐ更新して、途切れずにご利用ください。"},
	},
	pushCopyExpired1d: {
		"zh-CN": {"会员已到期", "续费后即可恢复连接。"},
		"zh-TW": {"會員已到期", "續費後即可恢復連線。"},
		"en-US": {"Your membership has expired", "Renew to restore your connection."},
		"ja":    {"メンバーシップの有効期限が切れました", "更新すると接続を再開できます。"},
	},
}

// pushCopyLang 把用户语言归到文案表的四种语言之一；空值按品牌默认（开途 zh-CN、其余 en-US）。
func pushCopyLang(lang string, brand Brand) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch {
	case l == "":
		if brand == BrandKaitu {
			return "zh-CN"
		}
		return "en-US"
	case l == "zh-tw" || l == "zh-hk" || l == "zh-mo" || strings.HasPrefix(l, "zh-hant"):
		return "zh-TW"
	case strings.HasPrefix(l, "zh"):
		return "zh-CN"
	case strings.HasPrefix(l, "ja"):
		return "ja"
	default:
		return "en-US"
	}
}

// pushCopy 返回某类提醒在某语言（已经过 pushCopyLang 归一）下的标题和正文。
func pushCopy(kind pushCopyKind, lang string) (title, body string) {
	byLang := pushCopyTable[kind]
	c, ok := byLang[lang]
	if !ok {
		c = byLang["en-US"]
	}
	return c.title, c.body
}
