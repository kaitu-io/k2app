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

// planByAppleProductID 按 Apple 商品ID 查套餐；找不到即拒绝入账（未知商品）。
func planByAppleProductID(ctx context.Context, tx *gorm.DB, productID string) (*Plan, error) {
	// 空串必须显式拒绝：GORM 的结构体条件会丢弃零值字段，`Where(&Plan{AppleProductID: ""})`
	// 会退化成无条件的 `SELECT * FROM plans LIMIT 1`，静默返回**任意一个** plan。
	// 该 plan 的 Price 会成为订单金额与分佣基数——错得毫无痕迹。
	if productID == "" {
		return nil, fmt.Errorf("empty apple product id")
	}
	// plans.apple_product_id 没有唯一约束（历史上大量网页套餐该列为空串，加不了唯一索引）。
	// 若有人用"插新行、留旧行"的方式改价，First 会按主键序永远返回旧的低价行，从此每一笔
	// IAP 订单和返现都按过期价格入账。这里显式查重并硬失败，不猜。
	var plans []Plan
	if err := tx.Where(&Plan{AppleProductID: productID}).Limit(2).Find(&plans).Error; err != nil {
		return nil, fmt.Errorf("query plan for apple product %s: %w", productID, err)
	}
	switch len(plans) {
	case 0:
		return nil, fmt.Errorf("no plan for apple product %s: %w", productID, gorm.ErrRecordNotFound)
	case 1:
		return &plans[0], nil
	default:
		return nil, fmt.Errorf("apple product %s maps to multiple plans (%d, %d) — 定价配置有歧义，拒绝猜测",
			productID, plans[0].ID, plans[1].ID)
	}
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
		if plan, perr := planByAppleProductID(ctx, tx, info.ProductId); perr == nil && plan != nil {
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
		if plan, _ := planByAppleProductID(ctx, tx, info.ProductId); plan != nil && plan.Tier != "" {
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

	// 建订单 + 分销商返现。位置关键：必须在上面的 alreadyCredited 早退之后，
	// 幂等性才由既有的 (provider, transaction_id) 去重天然覆盖——重投的交易根本走不到这里。
	// SAVEPOINT 非致命：Apple 已扣款，权益到账优先级高于内部账务；返现失败可事后补，
	// 入账回滚则是"用户付了钱没权益"的最坏结果。与上面邀请奖励的处理保持一致。
	// 非致命不等于可以静默：以下每条跳过路径都必须留痕，否则「订单没建、返现没发」在生产上
	// 无法被发现——这正是本次一并修掉的 isUserFirstPaidOrderInTx 吞错误的同类问题。
	// planByAppleProductID 永不返回 (nil, nil)，所以只有 err 与 success 两条路——别再加 plan==nil 分支。
	plan, perr := planByAppleProductID(ctx, tx, info.ProductId)
	switch {
	case errors.Is(perr, gorm.ErrRecordNotFound):
		// 配置缺失：该 productId 没挂到任何 plan。运维可修，且修好后需要人工补单。
		log.Errorf(ctx, "[creditAppleTransaction] no plan mapped to apple product %s, order+cashback skipped (non-fatal), user %d txn %s — 检查 plans.apple_product_id 配置，并按此日志补单",
			info.ProductId, userID, info.TransactionId)
	case perr != nil:
		// 真故障（DB 异常 / 定价配置有歧义）。同样非致命，但性质不同，分开记以便告警区分。
		log.Errorf(ctx, "[creditAppleTransaction] lookup plan for product %s failed, order+cashback skipped (non-fatal), user %d txn %s: %v",
			info.ProductId, userID, info.TransactionId, perr)
	default:
		if serr := tx.SavePoint("iap_order_cashback").Error; serr != nil {
			log.Errorf(ctx, "[creditAppleTransaction] savepoint failed, order+cashback skipped (non-fatal), user %d txn %s: %v",
				userID, info.TransactionId, serr)
		} else if oerr := createAppleIAPOrderInTx(ctx, tx, userID, plan, info); oerr != nil {
			if rerr := tx.RollbackTo("iap_order_cashback").Error; rerr != nil {
				// 回滚到保存点都失败，事务状态不可信——此时继续提交会把半截写入落库，
				// 必须把错误升级为致命，让整个入账回滚由 Apple 重投。
				return fmt.Errorf("rollback to savepoint failed after order error (%v): %w", oerr, rerr)
			}
			log.Errorf(ctx, "[creditAppleTransaction] iap order+cashback failed (non-fatal, rolled back), user %d txn %s: %v",
				userID, info.TransactionId, oerr)
		}
	}
	return nil
}

// revokeIAPOrderCashbackInTx 撤销某笔 Apple 交易对应订单的分销商返现，并把订单标记为已退款。
//
// 致命语义（与建单侧的非致命相反）：退款撤返现失败必须整体回滚。少扣一笔返现是真实资损，
// 而回滚只是让 Apple 重投通知。宁可重试，不可漏扣。
//
// 重投安全性靠下面的 IsRefunded 短路门 + 行锁，**不是**靠 refundCashbackInTx 幂等——
// 它并不幂等：它按 (type=income, order_id) 找收入行，而退款既不删也不改那一行，
// 所以第二次调用会再次 `balance - amount` 扣一遍余额，之后才在写 refund 流水时
// 撞上 wallet_changes.idx_type_order 唯一索引**抛错**。是抛错，不是 no-op。
// 结论：那道 IsRefunded 门是承重的，删掉它就等于打开二次扣款。
//
// 订单标记为 IsRefunded 还有第二重作用：isUserFirstPaidOrderInTx 排除已退款订单，
// 退款后用户的下一单会重新算首单，与网页侧口径一致。
func revokeIAPOrderCashbackInTx(ctx context.Context, tx *gorm.DB, txnID string) error {
	if txnID == "" {
		return nil // 调用方无交易号，跳过订单侧处理
	}
	// 行锁：Apple 可能并发投递 REFUND 与 REVOKE（不同 UUID，webhook 的 LastEventID 门拦不住）。
	// 无锁时两个 goroutine 会同时读到 IsRefunded=false 双双穿过短路门，靠唯一索引兜底虽不丢钱，
	// 但输家整个事务回滚 → 返 500 → Apple 重试风暴。与 ProcessOrderRefund 的加锁方式保持一致。
	var order Order
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(&Order{AppleTransactionID: txnID}).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 正常场景：退款的是本功能上线前的交易（当时没建单），无订单可撤。
		log.Infof(ctx, "[revokeIAPOrderCashback] no order for apple txn %s, nothing to revoke", txnID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("lookup order by apple txn %s: %w", txnID, err)
	}

	// 幂等门：Apple 会重投 REFUND 通知。已退款订单直接短路——否则 refundCashbackInTx 会撞上
	// wallet_changes 的 idx_type_order 唯一索引（它靠该索引兜底防二次扣款，但把重复当错误抛）。
	if order.IsRefunded != nil && *order.IsRefunded {
		log.Infof(ctx, "[revokeIAPOrderCashback] order %s already refunded, skipping (apple txn %s)", order.UUID, txnID)
		return nil
	}

	if err := refundCashbackInTx(ctx, tx, order.ID); err != nil {
		return fmt.Errorf("refund cashback for order %d: %w", order.ID, err)
	}

	now := time.Now()
	if err := tx.Model(&Order{}).Where("id = ?", order.ID).Updates(map[string]any{
		"is_refunded":   true,
		"refunded_at":   &now,
		"refund_amount": order.PayAmount,
		"refund_reason": fmt.Sprintf("Apple 退款/撤销 - %s", txnID),
	}).Error; err != nil {
		return fmt.Errorf("mark order %d refunded: %w", order.ID, err)
	}
	// 与网页退款路径（ProcessOrderRefund 第 2 步）保持一致：若这是该用户唯一有效的付费订单，
	// 把 IsFirstOrderDone 翻回 false。否则退款用户仍被当作老客，first_order 活动码会拒绝他、
	// 弃单召回也会把他排除在外（见 api/CLAUDE.md「Campaign Matcher Semantics」：
	// first_order 匹配的是 !IsFirstOrderDone 的新客）。
	var otherPaidCount int64
	if err := tx.Model(&Order{}).
		Where("user_id = ? AND is_paid = ? AND (is_refunded IS NULL OR is_refunded = ?) AND id != ?",
			order.UserID, true, false, order.ID).
		Count(&otherPaidCount).Error; err != nil {
		return fmt.Errorf("count other paid orders for user %d: %w", order.UserID, err)
	}
	if otherPaidCount == 0 {
		if err := tx.Model(&User{}).Where("id = ?", order.UserID).
			Update("is_first_order_done", false).Error; err != nil {
			return fmt.Errorf("reset is_first_order_done for user %d: %w", order.UserID, err)
		}
		log.Infof(ctx, "[revokeIAPOrderCashback] user %d has no other valid paid order, IsFirstOrderDone reset", order.UserID)
	}

	log.Infof(ctx, "[revokeIAPOrderCashback] order %s refunded + cashback revoked (apple txn %s)", order.UUID, txnID)
	return nil
}

// createAppleIAPOrderInTx 为一笔已入账的 Apple 交易补建订单并触发分销商返现。
//
// 口径（重要）：PayAmount/OriginAmount 取 **plan 标价**，不是用户实付、也不是本方实收。
// Apple 是多币种定价（美区 $59.99、国区 ¥328…）且抽成 15%，实付/实收都无法作为统一分佣基数；
// 而 appstore.TransactionInfo 的 Price/Currency 均为 optional，缺字段时无兜底。取 plan 标价
// 让 iOS 与网页两条链路的分佣基数完全一致，也让既有 processRetailerCashbackInTx
// （基数 = order.PayAmount）无需改动。财务侧靠 Order.Channel 区分口径。
//
// 首单/续费比例不在这里判定：processRetailerCashbackInTx 用 isUserFirstPaidOrderInTx
// 查 orders 表，本函数每笔交易建一单，首购天然是首单、续订天然走 RenewalPercent。
func createAppleIAPOrderInTx(ctx context.Context, tx *gorm.DB, userID uint64, plan *Plan, info *appstore.TransactionInfo) error {
	now := time.Now()
	order := &Order{
		UUID:                 generateId("ord"),
		Title:                plan.Label,
		OriginAmount:         plan.Price,
		PayAmount:            plan.Price,
		CampaignReduceAmount: 0,
		UserID:               userID,
		IsPaid:               BoolPtr(true),
		PaidAt:               &now,
		Channel:              OrderChannelAppleIAP,
		AppleTransactionID:   info.TransactionId,
	}
	if err := order.SetPlan(plan); err != nil {
		return fmt.Errorf("set plan meta: %w", err)
	}
	if err := tx.Create(order).Error; err != nil {
		return fmt.Errorf("create iap order: %w", err)
	}

	if err := processOrderCashbackInTx(ctx, tx, order.ID); err != nil {
		return fmt.Errorf("process cashback for iap order %d: %w", order.ID, err)
	}
	log.Infof(ctx, "[createAppleIAPOrder] user %d order %s (%s, %d cents) txn=%s",
		userID, order.UUID, OrderChannelAppleIAP, order.PayAmount, info.TransactionId)
	return nil
}

// verifyAndGrantTransaction 信任锚点：向 Apple 复核 transactionId，校验通过后入账。
// userID 来源——verify 端点：已鉴权用户；webhook：已存在订阅行的 UserID。
func verifyAndGrantTransaction(ctx context.Context, userID uint64, transactionID string) error {
	info, err := fetchAppleTransaction(ctx, appleBundleID(), transactionID)
	if err != nil {
		return fmt.Errorf("apple verify failed: %w", err)
	}
	if info.BundleId != appleBundleID() {
		return fmt.Errorf("bundle mismatch: got %s want %s", info.BundleId, appleBundleID())
	}
	if info.InAppOwnershipType == appstore.OwnershipType_FAMILY_SHARED {
		return fmt.Errorf("family-shared ownership not entitled")
	}

	return withDeadlockRetry(ctx, 3, func(tx *gorm.DB) error {
		if _, err := planByAppleProductID(ctx, tx, info.ProductId); err != nil {
			return err
		}
		return creditAppleTransaction(ctx, tx, userID, info)
	})
}

// revokeSubscription 处理 REFUND/REVOKE：撤销续订授予的权益。保守回收：仅当用户当前到期
// 落在本订阅周期窗口内（由本订阅"撑着"）时才扣到 now，避免误伤叠加的一次性时长。
// 已知简化：双源重叠时不做精确分账（v1 接受，见 spec §9）。
//
// txnID 是被退款的那笔 Apple 交易；用它精确反查对应订单，撤销已发放的分销商返现并把订单
// 标记为已退款。传空串则跳过订单侧处理（调用方拿不到交易号时的降级，不阻断权益回收）。
func revokeSubscription(ctx context.Context, sub *Subscription, txnID string) error {
	return getDB().Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, sub.UserID).Error; err != nil {
			return err
		}
		if err := revokeIAPOrderCashbackInTx(ctx, tx, txnID); err != nil {
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
		tier := ""
		if plan, _ := planByAppleProductID(context.Background(), getDB(), s.ProductID); plan != nil {
			tier = plan.Tier
		}
		manage := ManageSurface{Kind: "url"} // 默认；下方按 provider 覆写
		if s.Provider == "apple" {
			manage = appleManageSurface()
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
