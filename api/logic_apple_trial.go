package center

import "github.com/wordgate/qtoolkit/appstore"

// appleTxnClass 是一笔 Apple 交易的入账分类（spec 2026-10-19 §3.2）。
type appleTxnClass int

const (
	appleTxnPaid       appleTxnClass = iota // 正常付费（含 offerType 缺失）
	appleTxnFreeTrial                       // 入门优惠免费试用：offerType=1 且 price=0
	appleTxnOtherOffer                      // 其余优惠（付费入门价 / 促销 / offer code）
)

// Apple JWS TransactionInfo.offerType 取值（qtoolkit 未提供常量）。
const appleOfferTypeIntroductory int32 = 1

// classifyAppleTxn 按 offerType + price 判定交易类型。
// offerType==0（含字段缺失）一律视为付费；只有入门优惠且价格为 0 才是免费试用。
func classifyAppleTxn(info *appstore.TransactionInfo) appleTxnClass {
	if info.OfferType == 0 {
		return appleTxnPaid
	}
	if info.OfferType == appleOfferTypeIntroductory && info.Price == 0 {
		return appleTxnFreeTrial
	}
	return appleTxnOtherOffer
}
