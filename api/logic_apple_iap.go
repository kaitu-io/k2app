package center

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/appstore"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// fetchAppleTransaction 是复核交易的信任锚点：向 Apple 认证 API 拉取规范交易信息。
// 以包级变量提供，便于测试替换（不打真 Apple）。
var fetchAppleTransaction = appstore.GetTransaction

// appleBundleID 返回配置的 iOS bundle id（appstore.bundleId）。
func appleBundleID() string { return viper.GetString("appstore.bundleId") }

// appleBundleIDForBrand 返回该品牌 iOS app 的 bundle id。kaitu 沿用 legacy 键
// appstore.bundleId（零破坏）；其它品牌读 appstore.bundleIds.<brand>。
// 空串 = 该品牌尚无 iOS app —— 调用方必须响亮失败，绝不静默回落 kaitu 的 bundle。
func appleBundleIDForBrand(b Brand) string {
	if b == BrandKaitu {
		return appleBundleID()
	}
	return viper.GetString("appstore.bundleIds." + string(b))
}

// appleAccountNS 是派生 appAccountToken 的固定命名空间（任意固定 UUID）。
// 用户的 Center UUID（"user-"+xid）不是合法 RFC 4122 UUID，不能直接当 StoreKit
// appAccountToken。这里用 uuidv5(NS, userUUID) 派生一个确定性的合法 UUID：
// 仅 Go 端计算（无跨语言 parity 风险），暴露给 webapp 原样下发给 StoreKit，
// verify 时用同一算法核对 transaction.appAccountToken，阻断盗用 transactionId 的跨账号冒领。
var appleAccountNS = uuid.MustParse("e8c7b6a5-4d3e-2f1a-9b8c-7d6e5f4a3b2c")

// deriveAppleAccountToken 返回用户的确定性 StoreKit appAccountToken（小写 UUID）。
func deriveAppleAccountToken(userUUID string) string {
	return uuid.NewSHA1(appleAccountNS, []byte(userUUID)).String()
}

// planByAppleProductID 按 Apple 商品ID 查套餐（品牌过滤——同一商品 id 绝不跨品牌入账）；
// 找不到即拒绝入账（未知商品）。
func planByAppleProductID(ctx context.Context, tx *gorm.DB, productID string, brand Brand) (*Plan, error) {
	var plan Plan
	if err := tx.Scopes(ScopeBrand(brand)).Where(&Plan{AppleProductID: productID}).First(&plan).Error; err != nil {
		return nil, fmt.Errorf("no plan for apple product %s (brand %s): %w", productID, brand, err)
	}
	return &plan, nil
}

// deriveVerifiedStatus 返回一次成功 Apple verify 后的订阅状态：由合并后(取最大)的
// 绝对周期到期推导——绝不写出"period 已过去却 status=active"的出生即过期行(线上 bug 根因)。
// 已退款(revoked)的订阅绝不因重放交易复活。grace/billing_retry 由 applyRenewalInfo 单独落地，
// 不经此函数(upsert 仅服务 verify/grant 路径)。
func deriveVerifiedStatus(effectivePeriodEnd int64, existingStatus string, now int64) string {
	if existingStatus == "revoked" {
		return "revoked"
	}
	if effectivePeriodEnd > now {
		return "active"
	}
	return "expired"
}

// creditAppleTransaction is the single Apple→ledger entry point. It (1) enforces
// permanent binding (INV9): the binding (first) transaction must carry the caller's
// appAccountToken; an existing subscription row's UserID is then authoritative for all
// later transactions; (2) dedups by (provider, transaction_id) so each transaction
// credits expired_at at most once (INV1); (3) credits the forward period delta
// additively so gifts are never absorbed (INV3); and (4) keeps the subscriptions row's
// plan-state current. Must run inside a tx; locks the subscription + user rows.
func creditAppleTransaction(ctx context.Context, tx *gorm.DB, userID uint64, info *appstore.TransactionInfo) error {
	const provider = "apple"
	newPeriodEnd := info.ExpiresDate / 1000

	// Load-or-create the subscription row (binding key = OriginalTransactionId).
	var sub Subscription
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(&Subscription{Provider: provider, ProviderSubscriptionID: info.OriginalTransactionId}).
		First(&sub).Error
	isFirst := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !isFirst {
		return err
	}
	if !isFirst && sub.UserID != userID {
		// INV9: never re-bind an existing subscription to a different user.
		return fmt.Errorf("subscription %s already bound to user %d", info.OriginalTransactionId, sub.UserID)
	}

	// Lock the crediting user (needed for the additive credit and, on first bind, the
	// appAccountToken check).
	var user User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, userID).Error; err != nil {
		return fmt.Errorf("lock user %d: %w", userID, err)
	}

	// 品牌错配哨兵：Apple IAP 是 kaitu 专属支付渠道。这是唯一的入账动作前置点——
	// 无论调用方是 api_apple_iap_verify（已有独立 handler 守卫）还是
	// api_apple_webhook（无 handler 守卫，靠这里兜底），线上命中即为 bug，记 error
	// 日志告警并拒绝入账，绝不静默记账。品牌错配是持久条件：返回 error 会让 Apple
	// 判为处理失败并按其 server-to-server 重试策略重发通知，本哨兵会对同一笔交易
	// 反复告警——这是 fail-loud 的设计取舍，不是 bug。若此日志持续出现应视为 page
	// 级事件而非重试可容忍瞬态。
	if !Brand(user.Brand).Config().AllowsPayment(PayChannelAppleIAP) {
		alertPaymentBrandMismatch(ctx, "brand-mismatch apple credit: user %d brand %s does not allow apple_iap, txn=%s", userID, user.Brand, info.TransactionId)
		return fmt.Errorf("brand mismatch: user %d brand %s does not allow apple_iap channel", userID, user.Brand)
	}

	now := time.Now().Unix()

	// 账号绑定（INV9，§8.0）：仅在首笔（绑定）交易上强校验 appAccountToken。空 token 硬拒，
	// 杜绝"在他人订阅过的设备上 restore 白嫖"；生产无历史遗留购买，每笔首购都带 token。
	// 续订/对账（isFirst=false）不再校验 token——绑定已永久确立，且 Apple 续订交易常省略
	// appAccountToken；此时归属由上面的 sub.UserID 守卫（webhook 永远传已绑定 user）。
	if isFirst {
		if info.AppAccountToken == "" {
			return fmt.Errorf("missing appAccountToken: refusing to bind subscription")
		}
		if want := deriveAppleAccountToken(user.UUID); !strings.EqualFold(info.AppAccountToken, want) {
			return fmt.Errorf("appAccountToken mismatch: transaction bound to a different account")
		}
	}

	priorPeriodEnd := sub.CurrentPeriodEnd // 0 when first

	// Dedup (INV1): credit each transaction id once.
	var existing SubscriptionCredit
	dErr := tx.Where(&SubscriptionCredit{Provider: provider, TransactionID: info.TransactionId}).First(&existing).Error
	alreadyCredited := dErr == nil
	if dErr != nil && !errors.Is(dErr, gorm.ErrRecordNotFound) {
		return dErr
	}

	// Upsert plan-state on the subscription row (status derived, never hardcoded).
	sub.UserID = userID
	sub.Provider = provider
	sub.ProviderSubscriptionID = info.OriginalTransactionId
	sub.ProductID = info.ProductId
	sub.ProviderLatestRef = info.TransactionId
	if newPeriodEnd > sub.CurrentPeriodEnd {
		sub.CurrentPeriodEnd = newPeriodEnd
	}
	sub.AutoRenew = true
	sub.Environment = info.Environment
	sub.Status = deriveVerifiedStatus(sub.CurrentPeriodEnd, sub.Status, now)
	if err := tx.Save(&sub).Error; err != nil {
		return err
	}

	if alreadyCredited {
		return nil // idempotent: plan-state refreshed, no double credit
	}

	// 邀请购买奖励：Apple IAP 与 wordgate 订单同一规则（首单 + 套餐月数达门槛），
	// 复用 grantInvitePurchaseRewardInTx。必须在下方 IsFirstOrderDone 置位之前执行；
	// SAVEPOINT 保证奖励失败不阻断入账（支付到账优先）。奖励会更新买家 user 行，
	// 之后必须重载本函数持有的 user 快照，否则后续 Save 会用旧值覆盖奖励天数。
	if isFirst {
		if plan, perr := planByAppleProductID(ctx, tx, info.ProductId, Brand(user.Brand)); perr == nil && plan != nil {
			if err := tx.SavePoint("iap_invite_reward").Error; err == nil {
				if rerr := grantInvitePurchaseRewardInTx(ctx, tx, userID, plan); rerr != nil {
					tx.RollbackTo("iap_invite_reward")
					log.Errorf(ctx, "[creditAppleTransaction] invite reward failed (non-fatal, rolled back), user %d txn %s: %v",
						userID, info.TransactionId, rerr)
				}
			}
			if err := tx.First(&user, userID).Error; err != nil {
				return fmt.Errorf("reload user %d after invite reward: %w", userID, err)
			}
		}
	}

	// Compute the additive credit.
	var creditSeconds int64
	var kind string
	if isFirst {
		// First transaction: credit the period this transaction covers, from-now-if-expired.
		// Front-line first purchases have purchaseDate≈now, so this ≈ one period. (Late
		// reconciliation of an OLD missed first transaction could over-credit beyond Apple's
		// actual remaining coverage — capped in Phase 2 reconciliation.)
		creditSeconds = newPeriodEnd - (info.PurchaseDate / 1000)
		if creditSeconds < 0 {
			creditSeconds = 0
		}
		if now-info.PurchaseDate/1000 > 86400 {
			log.Warnf(ctx, "[creditAppleTransaction] txn %s is %dd old at first-bind; credited %ds forward from now — may exceed Apple's remaining coverage, Phase 2 reconciliation will cap",
				info.TransactionId, (now-info.PurchaseDate/1000)/86400, creditSeconds)
		}
		newExpiry := applyGiftCredit(user.ExpiredAt, creditSeconds, now)
		creditSeconds = newExpiry - max(user.ExpiredAt, now) // audited net add (Go 1.21+ builtin max)
		user.ExpiredAt = newExpiry
		kind = "purchase"
	} else {
		newExpiry := applyRenewalCredit(user.ExpiredAt, priorPeriodEnd, newPeriodEnd)
		creditSeconds = newExpiry - user.ExpiredAt
		user.ExpiredAt = newExpiry
		kind = "renewal"
	}

	if user.IsActivated == nil || !*user.IsActivated {
		user.IsActivated = BoolPtr(true)
		user.ActivatedAt = now
	}
	if user.IsFirstOrderDone == nil || !*user.IsFirstOrderDone {
		if plan, _ := planByAppleProductID(ctx, tx, info.ProductId, Brand(user.Brand)); plan != nil && plan.Tier != "" {
			user.Tier = plan.Tier
		}
		user.IsFirstOrderDone = BoolPtr(true)
	}
	if err := tx.Save(&user).Error; err != nil {
		return fmt.Errorf("save user %d: %w", userID, err)
	}

	// Dedup ledger row (INV1). Its auto-increment ID is unique per transaction and
	// becomes the audit reference — using sub.ID would make all renewals of one
	// subscription share the same reference_id, losing per-transaction traceability.
	// The credit row is the canonical per-transaction record.
	creditRow := &SubscriptionCredit{
		UserID:                userID,
		Provider:              provider,
		TransactionID:         info.TransactionId,
		OriginalTransactionID: info.OriginalTransactionId,
		CreditedSeconds:       creditSeconds,
		Kind:                  kind,
	}
	if err := tx.Create(creditRow).Error; err != nil {
		return err
	}
	// Human audit (INV8). ReferenceID = per-transaction credit row id (unique).
	if err := tx.Create(&UserProHistory{
		UserID:      userID,
		Type:        VipAppleSub,
		ReferenceID: creditRow.ID,
		// Days is floored display-only audit; CreditedSeconds (above) is the precise value.
		Days:        int(creditSeconds / 86400),
		Reason:      fmt.Sprintf("apple 订阅入账(%s) - %s", kind, info.TransactionId),
	}).Error; err != nil {
		return err
	}
	log.Infof(ctx, "[creditAppleTransaction] user %d credited +%dd (%s) txn=%s expiry→%s",
		userID, int(creditSeconds/86400), kind, info.TransactionId,
		time.Unix(user.ExpiredAt, 0).Format("2006-01-02"))
	return nil
}

// verifyAndGrantTransaction 信任锚点：向 Apple 复核 transactionId，校验通过后入账。
// userID 来源——verify 端点：已鉴权用户；webhook：已存在订阅行的 UserID。
func verifyAndGrantTransaction(ctx context.Context, userID uint64, transactionID string) error {
	var vu User
	if err := getDB().Select("brand").First(&vu, userID).Error; err != nil {
		return fmt.Errorf("load user %d brand: %w", userID, err)
	}
	userBrand := Brand(vu.Brand)
	bundleID := appleBundleIDForBrand(userBrand)
	if bundleID == "" {
		return fmt.Errorf("no apple bundle id configured for brand %s", userBrand)
	}
	info, err := fetchAppleTransaction(ctx, bundleID, transactionID)
	if err != nil {
		return fmt.Errorf("apple verify failed: %w", err)
	}
	if info.BundleId != bundleID {
		return fmt.Errorf("bundle mismatch: got %s want %s", info.BundleId, bundleID)
	}
	if info.InAppOwnershipType == appstore.OwnershipType_FAMILY_SHARED {
		return fmt.Errorf("family-shared ownership not entitled")
	}

	return withDeadlockRetry(ctx, 3, func(tx *gorm.DB) error {
		if _, err := planByAppleProductID(ctx, tx, info.ProductId, userBrand); err != nil {
			return err
		}
		return creditAppleTransaction(ctx, tx, userID, info)
	})
}

// revokeSubscription 处理 REFUND/REVOKE：撤销续订授予的权益。保守回收：仅当用户当前到期
// 落在本订阅周期窗口内（由本订阅"撑着"）时才扣到 now，避免误伤叠加的一次性时长。
// 已知简化：双源重叠时不做精确分账（v1 接受，见 spec §9）。
func revokeSubscription(ctx context.Context, sub *Subscription) error {
	return getDB().Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, sub.UserID).Error; err != nil {
			return err
		}
		now := time.Now().Unix()
		if user.ExpiredAt > now && user.ExpiredAt <= sub.CurrentPeriodEnd {
			cutDays := int((user.ExpiredAt - now) / 86400)
			user.ExpiredAt = now
			if err := tx.Model(&user).Select("ExpiredAt").Updates(&user).Error; err != nil {
				return err
			}
			if err := tx.Create(&UserProHistory{
				UserID:      user.ID,
				Type:        VipRefund,
				ReferenceID: sub.ID,
				Days:        -cutDays,
				Reason:      fmt.Sprintf("%s 退款/撤销 - %s", sub.Provider, sub.ProviderSubscriptionID),
			}).Error; err != nil {
				return err
			}
			log.Infof(ctx, "[revokeSubscription] user %d clawed back -%dd for %s sub %s",
				user.ID, cutDays, sub.Provider, sub.ProviderSubscriptionID)
		} else {
			// Entitlement not clawed back: either already expired (expiredAt≤now) or
			// user has additional time beyond this subscription's period (e.g. from gifts
			// or other sources). Sub is revoked but paid time is preserved.
			log.Infof(ctx, "[revokeSubscription] user %d no clawback (expiredAt=%d periodEnd=%d now=%d); sub marked revoked",
				user.ID, user.ExpiredAt, sub.CurrentPeriodEnd, now)
		}
		return tx.Model(&Subscription{}).Where("id = ?", sub.ID).Update("status", "revoked").Error
	})
}

// computeRenewalState 纯函数：依据 Apple 已签名的 RenewalInfo（缺失时退回 subtype）
// 推导订阅的自动续订开关与计费状态。provider 无关、无副作用。
//   - autoRenew：RenewalInfo.AutoRenewStatus 为权威；RenewalInfo 缺失时退回 subtype；
//     两者都无信息则返回 nil（表示"不改"）。
//   - status：terminal（expired/revoked）一律返回 ""（绝不复活已终结订阅）；否则按
//     计费重试 / 宽限期 / 正常 推导为 billing_retry|grace|active。
//
// 关键：此函数绝不触碰权益到期——取消自动续订 / 扣费失败都不缩短用户已购周期，
// 到期由 EXPIRED 事件落地。
func computeRenewalState(currentStatus string, ri *appstore.RenewalInfo, subtype string, nowSec int64) (autoRenew *bool, status string) {
	if ri != nil {
		v := ri.AutoRenewStatus == appstore.AutoRenewStatus_On
		autoRenew = &v
	} else {
		switch subtype {
		case appstore.Subtype_AUTO_RENEW_ENABLED:
			v := true
			autoRenew = &v
		case appstore.Subtype_AUTO_RENEW_DISABLED:
			v := false
			autoRenew = &v
		}
	}

	if currentStatus == "expired" || currentStatus == "revoked" {
		return autoRenew, "" // terminal：绝不复活
	}

	status = "active"
	if ri != nil {
		if ri.IsInBillingRetryPeriod {
			status = "billing_retry"
		} else if ri.GracePeriodExpiresDate/1000 > nowSec {
			status = "grace"
		}
	}
	return autoRenew, status
}

// applyRenewalInfo 落地续订状态变更（DID_CHANGE_RENEWAL_STATUS / DID_FAIL_TO_RENEW）：
// 把 computeRenewalState 的结论写入订阅行。绝不 re-grant、绝不改用户到期。
// 用 map 更新而非 struct，以免 GORM 跳过 auto_renew=false 这一零值。
func applyRenewalInfo(ctx context.Context, sub *Subscription, ri *appstore.RenewalInfo, subtype string) error {
	autoRenew, status := computeRenewalState(sub.Status, ri, subtype, time.Now().Unix())
	updates := map[string]any{}
	if autoRenew != nil {
		updates["auto_renew"] = *autoRenew
	}
	if status != "" {
		updates["status"] = status
	}
	if len(updates) == 0 {
		log.Infof(ctx, "[applyRenewalInfo] sub %s no state change (subtype=%s currentStatus=%s)",
			sub.ProviderSubscriptionID, subtype, sub.Status)
		return nil
	}
	if err := getDB().Model(&Subscription{}).Where("id = ?", sub.ID).Updates(updates).Error; err != nil {
		return err
	}
	log.Infof(ctx, "[applyRenewalInfo] sub %s autoRenew=%v status=%v (subtype=%s)",
		sub.ProviderSubscriptionID, updates["auto_renew"], updates["status"], subtype)
	return nil
}

// activeSubStatuses 视为"活跃"的状态：grace/billing_retry 也算活跃，避免在 Apple 仍在
// 重试扣费时向用户兜售第二份订阅（防双扣）。粗筛用，精筛见 isSubscriptionLive。
var activeSubStatuses = []string{"active", "grace", "billing_retry"}

// isSubscriptionLive 读模型的唯一判据：订阅当前是否真的覆盖用户(→ 显示"管理"/防双卖)。
// active 必须 current_period_end 仍在未来；grace/billing_retry 无视周期都算活跃(Apple 仍在
// 宽限/重试扣费)；terminal(expired/revoked/未知)一律不算。这样一行 status=active 但 period 已过
// 的陈旧行(线上 bug)永远不会被读成活跃——与 user.expired_at 这个真相源保持一致。
func isSubscriptionLive(s *Subscription, now int64) bool {
	switch s.Status {
	case "active":
		return s.CurrentPeriodEnd > now
	case "grace", "billing_retry":
		return true
	default:
		return false
	}
}

// appleManageSurface 是 Apple 订阅的系统管理面（iOS 设置内订阅页）。
func appleManageSurface() ManageSurface {
	return ManageSurface{Kind: "apple_settings"}
}

// GetActiveSubscriptions 返回用户当前活跃的续订订阅读模型（provider 中立）。
// 容错：任何查询错误返回 nil（不让 user-info 因附带读模型失败而 500；mock-DB 测试
// 未 mock 此查询时也优雅降级为空列表）。
func GetActiveSubscriptions(userID uint64) []DataSubscription {
	brand := BrandKaitu
	var su User
	if err := getDB().Select("brand").First(&su, userID).Error; err == nil {
		brand = Brand(su.Brand)
	}

	var subs []Subscription
	if err := getDB().Where("user_id = ? AND status IN ?", userID, activeSubStatuses).
		Find(&subs).Error; err != nil {
		return nil
	}
	now := time.Now().Unix()
	out := make([]DataSubscription, 0, len(subs))
	for i := range subs {
		s := &subs[i]
		if !isSubscriptionLive(s, now) {
			continue // 防陈旧 active 行(period 已过)被读成订阅中
		}
		// provider 分派：ProductID 语义随 provider 变（apple=商品ID / stripe=price ID），
		// tier 反查与管理面各走各的。
		tier := ""
		manage := ManageSurface{Kind: "url"} // 未知 provider 的兜底
		switch s.Provider {
		case "apple":
			if plan, _ := planByAppleProductID(context.Background(), getDB(), s.ProductID, brand); plan != nil {
				tier = plan.Tier
			}
			manage = appleManageSurface()
		case "stripe":
			if plan, _ := planByStripePriceID(context.Background(), getDB(), s.ProductID); plan != nil {
				tier = plan.Tier
			}
			manage = ManageSurface{Kind: "stripe_portal"} // 客户端调 POST /api/user/stripe/portal 换 URL
		}
		out = append(out, DataSubscription{
			Provider:         s.Provider,
			Tier:             tier,
			CurrentPeriodEnd: s.CurrentPeriodEnd,
			AutoRenew:        s.AutoRenew,
			Manage:           manage,
		})
	}
	return out
}
