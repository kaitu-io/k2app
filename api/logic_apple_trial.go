package center

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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
// 允许改绑到当前账号。其余情形（链付过费、试用交易、空 token、别人的 token）一律拒绝。
func canRebindTrialChain(tx *gorm.DB, sub *Subscription, class appleTxnClass, info *appstore.TransactionInfo, userID uint64) (bool, error) {
	// OtherOffer 也按付费入账（只多一条告警），所以同样允许改绑——否则带 B token 的 OtherOffer
	// 交易会被 errAppleTxnAwaitingRebind 永远挂起、B 的 verify 又被拒。
	if !sub.IsTrial || class == appleTxnFreeTrial || info.AppAccountToken == "" {
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
	return clearUserInTrialUnlessOtherTrialLive(tx, sub.UserID, sub.ID)
}

// clearUserInTrialUnlessOtherTrialLive 是清 users.in_trial 的唯一入口（试用链终结、试用链改绑走原账号）。
// 同一用户可能有多条 Apple 试用链（换 Apple ID 各开一次试用）：只要还有别的链 is_trial 且试用期未结束，
// 标志就还属于那条链，不清。exceptSubID 是正在终结 / 已改绑走的那条链。
func clearUserInTrialUnlessOtherTrialLive(tx *gorm.DB, userID, exceptSubID uint64) error {
	var live int64
	if err := tx.Model(&Subscription{}).
		Where("user_id = ? AND provider = ? AND id <> ? AND is_trial = ? AND trial_ends_at > ?",
			userID, SubscriptionProviderApple, exceptSubID, true, time.Now().Unix()).
		Count(&live).Error; err != nil {
		return fmt.Errorf("check other live trial chains of user %d: %w", userID, err)
	}
	if live > 0 {
		return nil
	}
	if err := tx.Model(&User{}).Where("id = ?", userID).Update("in_trial", false).Error; err != nil {
		return fmt.Errorf("clear in_trial of user %d (trial sub %d): %w", userID, exceptSubID, err)
	}
	return nil
}

// ---------- 对账遇到等改绑的交易：告警 ----------

// appleAwaitingRebindAlertWindow 同一 otx「等改绑」告警的最短间隔。对账每天 / 每周都会重扫到它，
// 不去重就是每天一条。
const appleAwaitingRebindAlertWindow = 24 * time.Hour

// appleAwaitingRebindAlerts 进程内按 otx 去重（多实例各自至多每天一条，可接受）。
var appleAwaitingRebindAlerts = struct {
	mu   sync.Mutex
	last map[string]time.Time
}{last: map[string]time.Time{}}

// maybeAlertAppleAwaitingRebind 对账 worker 跳过一笔等改绑的付费交易时调用：Apple 已经收了钱，
// 而 token 主人一直没来 verify，权益就一直挂着——需要人看。返回是否发出，供测试断言。
func maybeAlertAppleAwaitingRebind(ctx context.Context, otx, txnID string, boundUserID uint64, now time.Time) bool {
	appleAwaitingRebindAlerts.mu.Lock()
	if last, ok := appleAwaitingRebindAlerts.last[otx]; ok && now.Sub(last) < appleAwaitingRebindAlertWindow {
		appleAwaitingRebindAlerts.mu.Unlock()
		return false
	}
	appleAwaitingRebindAlerts.last[otx] = now
	appleAwaitingRebindAlerts.mu.Unlock()
	alertPaymentAnomaly(ctx, "APPLE-AWAITING-REBIND",
		"试用链 otx=%s 上的付费交易 %s 带着另一个账号的 appAccountToken，等那个账号的客户端 verify 改绑；链当前绑定用户 %d。"+
			"Apple 已扣款但尚未入账给任何人——若持续出现，查 token 主人是否还在用 app",
		otx, txnID, boundUserID)
	return true
}
