package center

import (
	"fmt"
	"strings"

	"github.com/wordgate/qtoolkit/appstore"
	"gorm.io/gorm"
)

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

// canRebindTrialChain 是 INV9 永久绑定的唯一例外（spec 2026-10-19 §3.3）：一条**从没付过费**的试用链
// （sub.IsTrial 仍为真），在第一笔**正常付费**交易入账时，若交易的 appAccountToken 是当前账号派生的，
// 允许改绑到当前账号。其余情形（链付过费、试用/其他优惠交易、空 token、别人的 token）一律拒绝。
func canRebindTrialChain(tx *gorm.DB, sub *Subscription, class appleTxnClass, info *appstore.TransactionInfo, userID uint64) (bool, error) {
	if !sub.IsTrial || class != appleTxnPaid || info.AppAccountToken == "" {
		return false, nil
	}
	var u User
	if err := tx.Select("id", "uuid").First(&u, userID).Error; err != nil {
		return false, fmt.Errorf("load user %d for trial-chain rebind: %w", userID, err)
	}
	return strings.EqualFold(info.AppAccountToken, deriveAppleAccountToken(u.UUID)), nil
}

// clearInTrialIfTrialChain 试用链终结（EXPIRED / GRACE_PERIOD_EXPIRED / REFUND / REVOKE）时清掉用户的
// in_trial（试用线路隔离，spec 2026-10-19 §3.5）。sub.IsTrial 在转付费前一直为真，所以它就是
// "这条链从没付过费"的判据；付过费的链不碰标志。只改 users.in_trial 这一列，绝不碰 expired_at。
func clearInTrialIfTrialChain(tx *gorm.DB, sub *Subscription) error {
	if !sub.IsTrial {
		return nil
	}
	if err := tx.Model(&User{}).Where("id = ?", sub.UserID).Update("in_trial", false).Error; err != nil {
		return fmt.Errorf("clear in_trial of user %d (trial sub %d): %w", sub.UserID, sub.ID, err)
	}
	return nil
}
