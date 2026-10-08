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

// errApplePlanAmbiguous：一个 Apple 商品 ID 在同品牌下对应多个 plan（配置问题，不是基础设施故障）。
// planByAppleProductID 用 %w 包它，调用方可用 errors.Is 区分。
var errApplePlanAmbiguous = errors.New("定价配置有歧义，拒绝猜测")

// planByAppleProductID 按 Apple 商品ID 查套餐（品牌过滤——同一商品 id 绝不跨品牌入账）；
// 找不到即拒绝入账（未知商品）。
func planByAppleProductID(ctx context.Context, tx *gorm.DB, productID string, brand Brand) (*Plan, error) {
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
	if err := tx.Scopes(ScopeBrand(brand)).Where(&Plan{AppleProductID: productID}).Limit(2).Find(&plans).Error; err != nil {
		return nil, fmt.Errorf("query plan for apple product %s (brand %s): %w", productID, brand, err)
	}
	switch len(plans) {
	case 0:
		return nil, fmt.Errorf("no plan for apple product %s (brand %s): %w", productID, brand, gorm.ErrRecordNotFound)
	case 1:
		return &plans[0], nil
	default:
		return nil, fmt.Errorf("apple product %s (brand %s) maps to multiple plans (%d, %d) — %w",
			productID, brand, plans[0].ID, plans[1].ID, errApplePlanAmbiguous)
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
	return deriveActiveOrExpired(effectivePeriodEnd, now)
}

// deriveActiveOrExpired 是离开 revoked 的唯一入口（spec 2026-10-08 §3.4）：退款后的新付款
// 复活订阅、Apple 撤销退款恢复订阅，都必须调它——传 sub.Status 给 deriveVerifiedStatus 会
// 静默停在 revoked。
func deriveActiveOrExpired(periodEnd, now int64) string {
	if periodEnd > now {
		return "active"
	}
	return "expired"
}

// errAppleTxnRevoked 被退款的交易永不入账（spec 2026-10-08 §3.4）。verify 端点映射成用户可读
// 错误；webhook 与对账视为已处理。
var errAppleTxnRevoked = errors.New("apple transaction has been refunded")

// lastActiveAppleRefundAt 这条订阅链上仍生效、且曾把订阅置为 revoked 的退款里最晚的
// revocationDate（毫秒）；无则 0。复活判据：新付款的 purchaseDate 必须严格晚于它。
func lastActiveAppleRefundAt(tx *gorm.DB, otx string) (int64, error) {
	var at int64
	err := tx.Model(&AppleRefund{}).
		Where("original_transaction_id = ? AND active = ? AND marked_revoked = ?", otx, true, true).
		Select("COALESCE(MAX(revocation_date), 0)").Scan(&at).Error
	return at, err
}

// creditAppleTransaction is the single Apple→ledger entry point. It (1) enforces
// permanent binding (INV9): the binding (first) transaction must carry the caller's
// appAccountToken; an existing subscription row's UserID is then authoritative for all
// later transactions; (2) dedups by (provider, transaction_id) so each transaction
// credits expired_at at most once (INV1); (3) credits the forward period delta
// additively so gifts are never absorbed (INV3); and (4) keeps the subscriptions row's
// plan-state current. Must run inside a tx; locks the subscription + user rows.
func creditAppleTransaction(ctx context.Context, tx *gorm.DB, userID uint64, info *appstore.TransactionInfo) error {
	const provider = SubscriptionProviderApple
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

	// 入账门：被退款的交易若还没入过账，就永远不入（漏入账后事后经 verify / 对账补入 = 退款后
	// 白拿）。门在 upsert 之前：不建订阅行、不改任何状态。已入账的被退交易走下面的
	// alreadyCredited 分支，只刷新 plan-state，状态停在 revoked。
	if info.RevocationDate > 0 && !alreadyCredited {
		log.Warnf(ctx, "[creditAppleTransaction] txn %s was refunded at %d and never credited — not crediting (user %d)",
			info.TransactionId, info.RevocationDate, userID)
		return errAppleTxnRevoked
	}

	// 复活：订阅因退款被置 revoked 后，又来了一笔退款之后才发生的新付款（重订阅，或 Apple 在
	// 退款后照常续订）。这笔按首购口径入账——时长 = 这一期本身、叠在 max(到期, now) 上——并覆盖
	// current_period_end。不能走续订 delta：退款不改周期末，delta 会按"新期末 − 旧期末"算，
	// 隔一段时间再订会多发、改订短周期会少发（spec 2026-10-08 §3.0/§3.4）。
	revival := false
	if !isFirst && sub.Status == "revoked" && !alreadyCredited && info.RevocationDate == 0 {
		lastRefundAt, err := lastActiveAppleRefundAt(tx, info.OriginalTransactionId)
		if err != nil {
			return fmt.Errorf("load last refund for %s: %w", info.OriginalTransactionId, err)
		}
		revival = info.PurchaseDate > lastRefundAt
	}

	// Upsert plan-state on the subscription row (status derived, never hardcoded).
	sub.UserID = userID
	sub.Provider = provider
	sub.ProviderSubscriptionID = info.OriginalTransactionId
	sub.ProductID = info.ProductId
	sub.ProviderLatestRef = info.TransactionId
	if revival || newPeriodEnd > sub.CurrentPeriodEnd {
		sub.CurrentPeriodEnd = newPeriodEnd
	}
	sub.AutoRenew = true
	sub.Environment = info.Environment
	if revival {
		sub.Status = deriveActiveOrExpired(sub.CurrentPeriodEnd, now)
	} else {
		sub.Status = deriveVerifiedStatus(sub.CurrentPeriodEnd, sub.Status, now)
	}
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
				trigger := inviteTrigger{Kind: InviteTriggerAppleTxn, Ref: info.TransactionId,
					Production: info.Environment != appstore.Environment_Sandbox}
				if rerr := grantInvitePurchaseRewardInTx(ctx, tx, userID, plan, trigger); rerr != nil {
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

	// PaidThrough 的入账基数：邀请奖励重载之后、本笔入账之前的 max(到期, now)（spec §3.2）。
	paidBase := max(user.ExpiredAt, now)

	// Compute the additive credit.
	var creditSeconds int64
	var kind string
	if isFirst || revival {
		// First transaction: credit the period this transaction covers, from-now-if-expired.
		// Front-line first purchases have purchaseDate≈now, so this ≈ one period. (Late
		// reconciliation of an OLD missed first transaction could over-credit beyond Apple's
		// actual remaining coverage — capped in Phase 2 reconciliation.)
		creditSeconds = newPeriodEnd - (info.PurchaseDate / 1000)
		if creditSeconds < 0 {
			creditSeconds = 0
		}
		if isFirst && now-info.PurchaseDate/1000 > 86400 {
			log.Warnf(ctx, "[creditAppleTransaction] txn %s is %dd old at first-bind; credited %ds forward from now — may exceed Apple's remaining coverage, Phase 2 reconciliation will cap",
				info.TransactionId, (now-info.PurchaseDate/1000)/86400, creditSeconds)
		}
		if revival {
			// 复活只发 Apple 还剩下的覆盖期：这笔付款若很久之后才被入账（漏通知、对账补入），按整期
			// 从 now 起叠会超过 Apple 实际覆盖。正常复活购买时刻≈now，等于整期。
			creditSeconds = max(newPeriodEnd-max(info.PurchaseDate/1000, now), 0)
		}
		newExpiry := applyGiftCredit(user.ExpiredAt, creditSeconds, now)
		creditSeconds = max(newExpiry-max(user.ExpiredAt, now), 0) // audited net add (Go 1.21+ builtin max)
		user.ExpiredAt = newExpiry
		kind = "purchase"
	} else {
		newExpiry := applyRenewalCredit(user.ExpiredAt, priorPeriodEnd, newPeriodEnd, now)
		// Audited net add, measured from the base the credit was actually stacked on
		// (mirrors the isFirst branch above). Measuring from a stale user.ExpiredAt
		// would book the dead gap between an expired ledger and now as if this
		// transaction had bought it, inflating both the clawback ledger and the
		// human-readable Days audit.
		creditSeconds = max(newExpiry-max(user.ExpiredAt, priorPeriodEnd, now), 0)
		user.ExpiredAt = newExpiry
		kind = "renewal"
	}

	// Cover-through 收敛不变式(spec 2026-08-22):无论 delta 怎么算,入账后权益至少
	// 覆盖到本周期末。修正 priorPeriodEnd 被状态路径先行推进 / 事件乱序导致 delta≤0
	// 而 ExpiredAt 落后活跃订阅的缺陷。只延长不缩短;审计账本保持 delta 口径,修正量留痕于日志。
	if covered := coverThrough(user.ExpiredAt, newPeriodEnd); covered > user.ExpiredAt {
		log.Warnf(ctx, "[creditAppleTransaction] cover-through corrected user %d expiry %d→%d (txn=%s)",
			userID, user.ExpiredAt, covered, info.TransactionId)
		user.ExpiredAt = covered
	}

	if user.IsActivated == nil || !*user.IsActivated {
		user.IsActivated = BoolPtr(true)
		user.ActivatedAt = now
	}
	// 沙盒交易不是一次真实购买：不消耗首单资格（否则 TestFlight 购买会让真实首购拿不到邀请奖励
	// 与首单活动，spec §3.4）。权益照发，Tier 也照设——Tier 属于权益，沙盒测试要看到它生效。
	isSandbox := info.Environment == appstore.Environment_Sandbox
	if user.IsFirstOrderDone == nil || !*user.IsFirstOrderDone {
		if plan, _ := planByAppleProductID(ctx, tx, info.ProductId, Brand(user.Brand)); plan != nil && plan.Tier != "" {
			user.Tier = plan.Tier
		}
		if !isSandbox {
			user.IsFirstOrderDone = BoolPtr(true)
		}
	}
	if err := tx.Save(&user).Error; err != nil {
		return fmt.Errorf("save user %d: %w", userID, err)
	}

	// PaidThrough（spec §3.2）：已付费权益覆盖到的时刻，按实际发放量累加（含 cover-through 修正量），
	// 与 Stripe 同口径。sub 行已在上面 Save 过，这里单独更新。已接受的偏差：宽限期后续订成功时
	// granted 从 graceEnd 起算，PaidThrough 比真实付费段少一个宽限窗（≤16 天）；赠送在购买之前
	// 叠加时按"先消耗付费段"计数，晚退款时用户保留 min(赠送, 已用) 天——都是向用户倾斜的偏差。
	if granted := max(user.ExpiredAt-paidBase, 0); granted > 0 {
		if err := tx.Model(&Subscription{}).Where("id = ?", sub.ID).
			Update("paid_through", max(sub.PaidThrough, now)+granted).Error; err != nil {
			return fmt.Errorf("update paid_through for sub %d: %w", sub.ID, err)
		}
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
		Days:   int(creditSeconds / 86400),
		Reason: fmt.Sprintf("apple 订阅入账(%s) - %s", kind, info.TransactionId),
	}).Error; err != nil {
		return err
	}
	log.Infof(ctx, "[creditAppleTransaction] user %d credited +%dd (%s) txn=%s expiry→%s",
		userID, int(creditSeconds/86400), kind, info.TransactionId,
		time.Unix(user.ExpiredAt, 0).Format("2006-01-02"))

	// 沙盒交易到此为止：权益照发（iOS 用沙盒账号做内购端到端测试时必须能看到 Pro 生效），
	// 但**绝不建订单**。订单是财务实体——它的 PayAmount 取 plan 标价，直接充当分销返现基数、
	// 营收统计口径、以及后台退款往用户钱包打款的金额，而沙盒交易用户实付为 0。
	// 生产事故（2026-08-10）：沙盒交易建出 ord-d9sjien7k7qc2u9r30ig（4900 分），客服在后台点退款，
	// 差一步就把 $49 可提现余额打进一个从没付过钱的账号；当时全库唯一的 IAP 订单就是这一笔。
	// environment 此前只被写进 subscriptions.environment 存档，全代码库没有任何一处读它做判断。
	if isSandbox {
		log.Warnf(ctx, "[creditAppleTransaction] sandbox txn %s credited to user %d, order+cashback intentionally skipped (sandbox is not a financial event)",
			info.TransactionId, userID)
		return nil
	}

	// 建订单 + 分销商返现。位置关键：必须在上面的 alreadyCredited 早退之后，
	// 幂等性才由既有的 (provider, transaction_id) 去重天然覆盖——重投的交易根本走不到这里。
	// SAVEPOINT 非致命：Apple 已扣款，权益到账优先级高于内部账务；返现失败可事后补，
	// 入账回滚则是"用户付了钱没权益"的最坏结果。与上面邀请奖励的处理保持一致。
	// 非致命不等于可以静默：以下每条跳过路径都必须留痕，否则「订单没建、返现没发」在生产上
	// 无法被发现——这正是本次一并修掉的 isUserFirstPaidOrderInTx 吞错误的同类问题。
	// planByAppleProductID 永不返回 (nil, nil)，所以只有 err 与 success 两条路——别再加 plan==nil 分支。
	plan, perr := planByAppleProductID(ctx, tx, info.ProductId, Brand(user.Brand))
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
// iapOrderRefundResult revokeIAPOrderCashbackInTx 的结果，供 applyAppleRefund 决定是否扣权益。
type iapOrderRefundResult struct {
	Order           *Order   // nil = 没有对应订单（本功能上线前的交易 / 沙盒 / 建单失败）
	AlreadyRefunded bool     // 订单此前已被标记退款（重投、撤销后再退、或后台钱包退款）
	WalletRefunded  bool     // 此前走过后台钱包退款——权益已由那条路径扣过，不能再扣
	OtherPaidCount  int64    // 买家除这笔外的有效付费单数
	Alerts          []string // 提交后再发（事务内发了再回滚或死锁重试会误报 / 重复）
}

func revokeIAPOrderCashbackInTx(ctx context.Context, tx *gorm.DB, txnID string) (*iapOrderRefundResult, error) {
	res := &iapOrderRefundResult{}
	if txnID == "" {
		return res, nil // 调用方无交易号，跳过订单侧处理
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
		return res, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup order by apple txn %s: %w", txnID, err)
	}
	res.Order = &order

	// 其它有效付费单数在短路之前算：撤销退款会把 is_first_order_done 置回 true，之后的再次退款
	// 走到已退款短路时也必须能把它翻回 false（spec 2026-10-08 §3.3 第 5 步）。
	if err := tx.Model(&Order{}).
		Where("user_id = ? AND is_paid = ? AND (is_refunded IS NULL OR is_refunded = ?) AND id != ?",
			order.UserID, true, false, order.ID).
		Count(&res.OtherPaidCount).Error; err != nil {
		return nil, fmt.Errorf("count other paid orders for user %d: %w", order.UserID, err)
	}
	// 与网页退款路径（ProcessOrderRefund 第 2 步）保持一致：若这是该用户唯一有效的付费订单，
	// 把 IsFirstOrderDone 翻回 false。否则退款用户仍被当作老客，first_order 活动码会拒绝他、
	// 弃单召回也会把他排除在外（见 api/CLAUDE.md「Campaign Matcher Semantics」：
	// first_order 匹配的是 !IsFirstOrderDone 的新客）。
	if res.OtherPaidCount == 0 {
		if err := tx.Model(&User{}).Where("id = ?", order.UserID).
			Update("is_first_order_done", false).Error; err != nil {
			return nil, fmt.Errorf("reset is_first_order_done for user %d: %w", order.UserID, err)
		}
		log.Infof(ctx, "[revokeIAPOrderCashback] user %d has no other valid paid order, IsFirstOrderDone reset", order.UserID)
	}

	// 幂等门：Apple 会重投 REFUND 通知。已退款订单直接短路——否则 refundCashbackInTx 会撞上
	// wallet_changes 的 idx_type_order 唯一索引（它靠该索引兜底防二次扣款，但把重复当错误抛）。
	if order.IsRefunded != nil && *order.IsRefunded {
		res.AlreadyRefunded = true
		var msg string
		res.WalletRefunded, msg = alreadyWalletRefunded(ctx, tx, &order, txnID)
		if msg != "" {
			res.Alerts = append(res.Alerts, "[DOUBLE-REFUND] "+msg)
		}
		log.Infof(ctx, "[revokeIAPOrderCashback] order %s already refunded, skipping (apple txn %s)", order.UUID, txnID)
		return res, nil
	}

	cbAlert, err := refundCashbackInTx(ctx, tx, order.ID)
	if err != nil {
		return nil, fmt.Errorf("refund cashback for order %d: %w", order.ID, err)
	}
	if cbAlert != "" {
		res.Alerts = append(res.Alerts, cbAlert)
	}

	now := time.Now()
	if err := tx.Model(&Order{}).Where("id = ?", order.ID).Updates(map[string]any{
		"is_refunded":   true,
		"refunded_at":   &now,
		"refund_amount": order.PayAmount,
		"refund_reason": fmt.Sprintf("Apple 退款/撤销 - %s", txnID),
	}).Error; err != nil {
		return nil, fmt.Errorf("mark order %d refunded: %w", order.ID, err)
	}

	log.Infof(ctx, "[revokeIAPOrderCashback] order %s refunded + cashback revoked (apple txn %s)", order.UUID, txnID)
	return res, nil
}

// alreadyWalletRefunded 是双退哨兵：订单已被标记退款时判断这次短路是否踩到了资损。
//
// 短路的常规原因是 Apple 重投同一条 REFUND/REVOKE 通知，无害。但若这笔订单此前走过**后台钱包
// 退款**（ProcessOrderRefund 会留下 wallet_changes{type=order_refund, order_id}），那么用户已经
// 拿到一份可提现的钱包补偿，现在 Apple 又原路退了一次——同一笔订单退了两次钱，是真实资损。
//
// 返回 (是否走过后台钱包退款, 告警文案)。告警由调用方在事务提交后发（事务内发了再回滚或死锁
// 重试会误报 / 重复）。只告警不阻断：Apple 侧退款已是既成事实，返错只会让 Apple 重试风暴。
// 查询失败时按"走过"处理（宁可这次不扣权益、靠告警人工核对，也不在不确定时双扣）。
func alreadyWalletRefunded(ctx context.Context, tx *gorm.DB, order *Order, txnID string) (bool, string) {
	var walletRefunds int64
	if err := tx.Model(&WalletChange{}).
		Where(&WalletChange{Type: WalletChangeTypeOrderRefund, OrderID: &order.ID}).
		Count(&walletRefunds).Error; err != nil {
		log.Errorf(ctx, "[revokeIAPOrderCashback] double-refund sentinel query failed for order %s (apple txn %s): %v",
			order.UUID, txnID, err)
		return true, fmt.Sprintf("订单 %s 双退哨兵查询失败（txn %s），本次未扣权益，请人工核对", order.UUID, txnID)
	}
	if walletRefunds == 0 {
		return false, ""
	}
	return true, fmt.Sprintf("订单 %s（用户 %d，%d 分）此前已通过后台退款打入用户钱包，现又收到 Apple 退款/撤销通知（txn %s）——同一笔订单退了两次钱，钱包余额可提现，请人工核对并冻结/追回",
		order.UUID, order.UserID, order.PayAmount, txnID)
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
// 把 computeRenewalState 的结论写入订阅行。绝不 re-grant；除 grace cover-through 外
// 不改用户到期（spec 2026-08-22）。
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
	// status<>'revoked' 进 SQL：sub 是事务外读到的快照，与并发的 REFUND 赛跑时不能把 revoked
	// 覆盖成 grace/active（会丢失复活判据并重新延长权益，spec 2026-10-08 §3.4）。
	if err := getDB().Model(&Subscription{}).Where("id = ? AND status <> ?", sub.ID, "revoked").Updates(updates).Error; err != nil {
		return err
	}
	log.Infof(ctx, "[applyRenewalInfo] sub %s autoRenew=%v status=%v (subtype=%s)",
		sub.ProviderSubscriptionID, updates["auto_renew"], updates["status"], subtype)

	// Grace cover-through(spec 2026-08-22, fix round 1):进入宽限期时把权益覆盖到宽限期末——
	// Apple 官方语义是宽限期内继续提供服务、扣费成功后无缝续期。只延长不缩短;同步把
	// sub.current_period_end 也推进到 graceEnd,否则后续 DID_RENEW 的 priorPeriodEnd 仍是旧
	// 周期末,delta 会把宽限补时误当"礼物"叠加,双算整段宽限窗(review finding 1)。两处写
	// 都是条件式原子 UPDATE(单调守卫进 SQL),不先读后写,避免与并发 credit 事务的
	// lost-update（review finding 2）；RowsAffected 判断是否真的改了再打日志,错误一律上抛。
	if status == "grace" && ri != nil && ri.GracePeriodExpiresDate > 0 {
		graceEnd := ri.GracePeriodExpiresDate / 1000

		// 订阅已被退款置 revoked 时不做宽限延长（与上面的状态守卫同一理由）。
		revokedSub := getDB().Model(&Subscription{}).Select("id").Where("id = ? AND status = ?", sub.ID, "revoked")
		userRes := getDB().Model(&User{}).Where("id = ? AND expired_at < ? AND NOT EXISTS (?)", sub.UserID, graceEnd, revokedSub).
			Update("expired_at", graceEnd)
		if userRes.Error != nil {
			return userRes.Error
		}
		if userRes.RowsAffected > 0 {
			log.Warnf(ctx, "[applyRenewalInfo] grace cover-through user %d expiry→%d (sub=%s)",
				sub.UserID, graceEnd, sub.ProviderSubscriptionID)
		}

		subRes := getDB().Model(&Subscription{}).Where("id = ? AND current_period_end < ? AND status <> ?", sub.ID, graceEnd, "revoked").
			Update("current_period_end", graceEnd)
		if subRes.Error != nil {
			return subRes.Error
		}
		if subRes.RowsAffected > 0 {
			sub.CurrentPeriodEnd = graceEnd
			log.Warnf(ctx, "[applyRenewalInfo] grace cover-through sub %s period_end→%d",
				sub.ProviderSubscriptionID, graceEnd)
		}
	}
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
