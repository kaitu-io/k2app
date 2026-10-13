package center

import (
	"context"
	"errors"
	"fmt"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
	subscription "github.com/stripe/stripe-go/v82/subscription"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// =====================================================================
// 订阅对账 Worker(spec 2026-08-22-subscription-entitlement-convergence)
// cover-through 只保证"已知状态不落后";webhook 全丢时 CurrentPeriodEnd 自身
// 陈旧,谁也覆盖不上——本 cron 是关死"订阅中却过期"工单的兜底,每日一轮。
// 扫描对象:标活跃(active/grace/billing_retry)且周期临期/已过的订阅行。
// =====================================================================

const TaskTypeSubscriptionReconcile = "subscription:reconcile"

// reconcileLookahead:周期末在此窗口内(或已过)的活跃订阅才对账——
// 正常续订由 webhook 实时驱动,对账只管临界与陈旧行,控制 Apple API 调用量。
const reconcileLookahead = 48 * 3600

// appleSubStatus 是 Get All Subscription Statuses 的解码结果(单条订阅)。
type appleSubStatus struct {
	status  int32 // appstore.SubscriptionStatus_* 常量
	txn     *appstore.TransactionInfo
	renewal *appstore.RenewalInfo
}

// fetchAppleSubStatus 测试 seam(镜像 fetchAppleTransaction 的模式)。
// 真实实现:调 Apple 端点,取匹配 originalTxnID 的 lastTransactions 项并解码。
var fetchAppleSubStatus = func(ctx context.Context, bundleID, originalTxnID string) (*appleSubStatus, error) {
	resp, err := appstore.GetAllSubscriptionStatuses(ctx, bundleID, originalTxnID)
	if err != nil {
		return nil, err
	}
	for i := range resp.Data {
		for j := range resp.Data[i].LastTransactions {
			it := &resp.Data[i].LastTransactions[j]
			if it.OriginalTransactionId != originalTxnID {
				continue
			}
			out := &appleSubStatus{status: it.Status}
			if it.SignedTransactionInfo != "" {
				if txn, derr := it.DecodeTransaction(); derr == nil {
					out.txn = txn
				} else {
					log.Warnf(ctx, "[SUB-RECONCILE] decode txn failed for %s: %v", originalTxnID, derr)
				}
			}
			if it.SignedRenewalInfo != "" {
				if ri, derr := it.DecodeRenewal(); derr == nil {
					out.renewal = ri
				} else {
					log.Warnf(ctx, "[SUB-RECONCILE] decode renewal failed for %s: %v", originalTxnID, derr)
				}
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("subscription %s not found in status response", originalTxnID)
}

// reconcileSubscription 对单条订阅向 provider 核对真相并收敛本地状态。
// 幂等:交易重放由 SubscriptionCredit 去重。Stripe 的 revoked 是终态、不触碰;Apple 的 revoked 行
// 仍要问 Apple——漏收的"退款后重订阅"经此复活、撤销退款经此探测(spec 2026-10-08 §3.8)。
func reconcileSubscription(ctx context.Context, sub *Subscription, now int64) (bool, error) {
	switch sub.Provider {
	case "apple":
		return reconcileAppleSubscription(ctx, sub, now)
	case "stripe":
		if sub.Status == "revoked" {
			return false, nil
		}
		return reconcileStripeSubscription(ctx, sub, now) // Task 6
	default:
		return false, nil
	}
}

func reconcileAppleSubscription(ctx context.Context, sub *Subscription, now int64) (bool, error) {
	// 品牌 bundleId:订阅归属用户的品牌决定查询凭据。对齐 verifyAndGrantTransaction
	// 的响亮失败契约——查不到用户或该品牌无 bundleId 一律报错,绝不静默回落 kaitu。
	var u User
	if err := db.Get().Select("brand").First(&u, sub.UserID).Error; err != nil {
		return false, fmt.Errorf("load user %d brand: %w", sub.UserID, err)
	}
	brand := Brand(u.Brand)
	bundleID := appleBundleIDForBrand(brand)
	if bundleID == "" {
		return false, fmt.Errorf("no apple bundle id configured for brand %s", brand)
	}
	st, err := fetchAppleSubStatus(ctx, bundleID, sub.ProviderSubscriptionID)
	if err != nil {
		return false, err
	}

	// Apple 报 Revoked:先走退款收回,跳过续订状态与 Expired 分支——不对被退交易做宽限延长
	// (spec 2026-10-08 §3.8)。证据时刻取 revocationDate,缺失时由 applyAppleRefund 取处理时刻。
	if st.status == appstore.SubscriptionStatus_Revoked {
		if st.txn == nil {
			log.Errorf(ctx, "[SUB-RECONCILE] sub %s revoked on Apple but status response has no transaction — skipped",
				sub.ProviderSubscriptionID)
			return false, nil
		}
		out, err := applyAppleRefund(ctx, sub.ProviderSubscriptionID, st.txn,
			appleRefundEvidence{SignedAt: st.txn.RevocationDate, Source: "reconcile"})
		if err != nil {
			return false, err
		}
		return out != nil && out.Adopted, nil
	}

	changed := false
	// 最新交易灌回现有入账路径:cover-through(Task 1)保证 ExpiredAt 收敛,
	// SubscriptionCredit 去重保证幂等——已入账过的交易此调用是无害 no-op。revoked 行的退款后
	// 新付款在这里复活。被退交易不入账(errAppleTxnRevoked),视为非致命。
	if st.txn != nil {
		before, beforeStatus := sub.CurrentPeriodEnd, sub.Status
		if err := withDeadlockRetry(ctx, 3, func(tx *gorm.DB) error {
			return creditAppleTransaction(ctx, tx, sub.UserID, st.txn)
		}); err != nil && !errors.Is(err, errAppleTxnRevoked) {
			return false, err
		}
		if err := db.Get().First(sub, sub.ID).Error; err != nil {
			return false, err
		}
		changed = sub.CurrentPeriodEnd != before || sub.Status != beforeStatus
	}
	// revoked 行只允许复活入账与撤销退款探测:跳过续订状态与 Expired 分支(Apple 报 Expired 时
	// 会把 revoked 改成 expired,丢失复活判据)。
	if sub.Status != "revoked" {
		// 续期信息(grace/billing_retry/autoRenew)走现有落地路径(Task 3 含 grace cover-through)。
		// applyRenewalInfo 内部是直接 UPDATE DB,不保证同步内存 sub——纯状态迁移(如
		// active→billing_retry,周期不变)不会反映在 CurrentPeriodEnd 上,必须单独快照
		// 前后 status/autoRenew 再重载比对,否则这类漏 webhook 场景会被 changed 漏计。
		if st.renewal != nil {
			prevStatus, prevAutoRenew := sub.Status, sub.AutoRenew
			if err := applyRenewalInfo(ctx, sub, st.renewal, ""); err != nil {
				return changed, err
			}
			if err := db.Get().First(sub, sub.ID).Error; err != nil {
				return changed, err
			}
			if sub.Status != prevStatus || sub.AutoRenew != prevAutoRenew {
				changed = true
			}
		}
		// Apple 报已过期且本地仍标活跃 → 落终态。条件原子 UPDATE
		// (status = 内存快照 AND current_period_end < now),不先读后写——与 Stripe 侧同构
		// (fix round 1)。守卫拦下交错场景:reconcile 拉到 Expired 后、写库前,用户重订阅/
		// 迟到 DID_RENEW 把 status 改回 active 且 period 推到未来,此时该行不再满足守卫,
		// UPDATE 影响 0 行,不误标 expired。
		if st.status == appstore.SubscriptionStatus_Expired && sub.Status != "expired" {
			res := db.Get().Model(&Subscription{}).
				Where("id = ? AND status = ? AND current_period_end < ?", sub.ID, sub.Status, now).
				Update("status", "expired")
			if res.Error != nil {
				return changed, res.Error
			}
			if res.RowsAffected > 0 {
				log.Infof(ctx, "[SUB-RECONCILE] sub %s marked expired (was %s)", sub.ProviderSubscriptionID, sub.Status)
				changed = true
			} else {
				log.Debugf(ctx, "[SUB-RECONCILE] sub %s expired write guarded off (status/period changed under us)", sub.ProviderSubscriptionID)
			}
		}
	}
	reversed, err := probeAppleRefundReversals(ctx, sub, bundleID, now)
	if err != nil {
		return changed, err
	}
	return changed || reversed, nil
}

// appleReversalProbeCooldown 退款证据之后多久才探测撤销:防 Apple 读写不一致造成误恢复。
const appleReversalProbeCooldown = 48 * 3600

// probeAppleRefundReversals 漏收 REFUND_REVERSED 时的兜底:被退交易在 Apple 侧已没有
// revocationDate → Apple 撤销了退款。只认 HTTP 成功、交易号一致、生产环境的应答,任何错误都不恢复;
// 证据时刻 = 最新退款证据 + 1ms(不用 now:用 now 会让之后任何早于扫描时刻的真实再退款被判为迟到)。
func probeAppleRefundReversals(ctx context.Context, sub *Subscription, bundleID string, now int64) (bool, error) {
	var rows []AppleRefund
	if err := db.Get().Where("subscription_id = ? AND active = ? AND refund_signed_at < ?",
		sub.ID, true, (now-appleReversalProbeCooldown)*1000).Find(&rows).Error; err != nil {
		return false, err
	}
	reversed := false
	for i := range rows {
		r := &rows[i]
		info, err := fetchAppleTransaction(ctx, bundleID, r.TransactionID)
		if err != nil || info == nil {
			log.Warnf(ctx, "[SUB-RECONCILE] reversal probe for txn %s skipped: %v", r.TransactionID, err)
			continue
		}
		if info.TransactionId != r.TransactionID || info.Environment != appstore.Environment_Production || info.RevocationDate != 0 {
			continue
		}
		if err := reverseAppleRefund(ctx, sub.ProviderSubscriptionID, info, r.RefundSignedAt+1, "reconcile"); err != nil {
			return reversed, err
		}
		reversed = true
	}
	return reversed, nil
}

// handleSubscriptionReconcileTask 每日 cron:扫临期/陈旧的活跃订阅行逐条对账。
// 单条失败记日志跳过,不阻塞整轮。
func handleSubscriptionReconcileTask(ctx context.Context, _ []byte) error {
	now := time.Now().Unix()
	subs, err := subscriptionsToReconcile(now)
	if err != nil {
		return err
	}
	var changed, failed int
	for i := range subs {
		c, err := reconcileSubscription(ctx, &subs[i], now)
		if err != nil {
			failed++
			log.Errorf(ctx, "[SUB-RECONCILE] sub %s (provider=%s user=%d) reconcile failed: %v",
				subs[i].ProviderSubscriptionID, subs[i].Provider, subs[i].UserID, err)
			continue
		}
		if c {
			changed++
		}
	}
	log.Infof(ctx, "[SUB-RECONCILE] daily sweep done: scanned=%d changed=%d failed=%d", len(subs), changed, failed)
	return nil
}

// subscriptionsToReconcile 本轮要对账的订阅:临期/陈旧的活跃行(全部 provider)∪ Apple 生产订阅的
// 每周巡检桶(spec 2026-10-08 §3.8:有效订阅、近 120 天过期的、带生效退款记录的任意状态),按 id 去重。
func subscriptionsToReconcile(now int64) ([]Subscription, error) {
	var due []Subscription
	if err := db.Get().Where("status IN ?", activeSubStatuses).
		Where("current_period_end < ?", now+reconcileLookahead).
		Find(&due).Error; err != nil {
		return nil, err
	}
	bucket := (now / 86400) % appleReconcileBuckets
	refunded := db.Get().Model(&AppleRefund{}).Select("subscription_id").
		Where("active = ? AND revocation_date >= ?", true, (now-180*86400)*1000)
	var weekly []Subscription
	if err := db.Get().Where("provider = ? AND environment = ? AND MOD(id, ?) = ?",
		SubscriptionProviderApple, appstore.Environment_Production, appleReconcileBuckets, bucket).
		Where(db.Get().Where("status IN ?", activeSubStatuses).
			Or("status = ? AND current_period_end > ?", "expired", now-120*86400).
			Or("id IN (?)", refunded)).
		Find(&weekly).Error; err != nil {
		return nil, err
	}
	seen := make(map[uint64]bool, len(due)+len(weekly))
	out := make([]Subscription, 0, len(due)+len(weekly))
	for _, list := range [][]Subscription{due, weekly} {
		for _, s := range list {
			if !seen[s.ID] {
				seen[s.ID] = true
				out = append(out, s)
			}
		}
	}
	return out, nil
}

// appleReconcileBuckets Apple 订阅按 id 分 7 桶,每天巡检一桶 → 每条每周至少一次。
const appleReconcileBuckets = 7

// stripeSecretKey 取 Stripe secret key,不依赖 *gin.Context——configStripe 只读 viper,
// ctx 参数未被内部使用,传 context.Background() 与传真实请求 ctx 等价。
func stripeSecretKey() string {
	return configStripe(context.Background()).SecretKey
}

// stripeFetchSubscription 测试 seam(镜像 api_stripe.go 的 stripeNewCheckoutSession 模式)。
// key 解析放在 seam 内部而非调用方——调用方(reconcileStripeSubscription)因此不依赖全局
// viper 状态,测试可以整体替换本 seam 而不必配置 stripe.secret_key。
var stripeFetchSubscription = func(subID string) (*stripe.Subscription, error) {
	key := stripeSecretKey()
	if key == "" {
		return nil, fmt.Errorf("stripe secret key unavailable")
	}
	return subscription.Client{B: stripe.GetBackend(stripe.APIBackend), Key: key}.Get(subID, nil)
}

// reconcileStripeSubscription:拉 provider 真相,纠正 status/period 并 cover-through 权益。
// 不伪造 invoice 入账——SubscriptionCredit 审计账本只记真实交易,纠偏留痕于日志。
//
// fix round 1:三处写全部改成条件式原子 UPDATE(单调守卫进 SQL WHERE),不先读后写——
// 与 applyRenewalInfo 的 grace cover-through(logic_apple_iap.go)同一形状,避免与并发
// credit 事务 lost-update(review finding)。period/status 拆两条独立守卫的 UPDATE,
// 避免 status-only 变更被 period 的 WHERE 挡住;RowsAffected 才计入 changed 并打日志。
func reconcileStripeSubscription(ctx context.Context, sub *Subscription, now int64) (bool, error) {
	remote, err := stripeFetchSubscription(sub.ProviderSubscriptionID)
	if err != nil {
		return false, err
	}
	status := stripeSubStatus(remote.Status)
	var periodEnd int64
	if remote.Items != nil && len(remote.Items.Data) > 0 {
		periodEnd = remote.Items.Data[0].CurrentPeriodEnd
	}

	changed := false

	// period 单调守卫:WHERE current_period_end < periodEnd 保证绝不回退;revoked 行永不触碰。
	if periodEnd > 0 && sub.Status != "revoked" {
		res := db.Get().Model(&Subscription{}).
			Where("id = ? AND current_period_end < ?", sub.ID, periodEnd).
			Update("current_period_end", periodEnd)
		if res.Error != nil {
			return false, res.Error
		}
		if res.RowsAffected > 0 {
			changed = true
			log.Infof(ctx, "[SUB-RECONCILE] stripe sub %s period corrected → %d", sub.ProviderSubscriptionID, periodEnd)
		}
	}

	// status 乐观锁:WHERE status = 旧值,revoked 绝不触碰(入口已挡,此处保留对称防御)。
	if status != "" && status != sub.Status && sub.Status != "revoked" {
		res := db.Get().Model(&Subscription{}).
			Where("id = ? AND status = ?", sub.ID, sub.Status).
			Update("status", status)
		if res.Error != nil {
			return changed, res.Error
		}
		if res.RowsAffected > 0 {
			changed = true
			log.Infof(ctx, "[SUB-RECONCILE] stripe sub %s status %s→%s", sub.ProviderSubscriptionID, sub.Status, status)
		}
	}

	// 活跃且周期在未来 → cover-through 权益(与 credit 路径同一收敛不变式)。事务内先锁订阅行
	// 再锁用户行（与 creditStripeInvoice / 收回原语同序）：revoked 在锁内判，并发收回已提交则
	// 不延长；补出来的这段同步累加进 PaidThrough，之后全额退款 / 拒付才扣得掉。
	if status == "active" && periodEnd > now {
		var granted int64
		err := withDeadlockRetry(ctx, 3, func(tx *gorm.DB) error {
			granted = 0
			var cur Subscription
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&cur, sub.ID).Error; err != nil {
				return err
			}
			if cur.Status == "revoked" {
				return nil
			}
			var u User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, sub.UserID).Error; err != nil {
				return err
			}
			if u.ExpiredAt >= periodEnd {
				return nil
			}
			granted = periodEnd - max(u.ExpiredAt, now)
			if err := tx.Model(&User{}).Where("id = ?", u.ID).Update("expired_at", periodEnd).Error; err != nil {
				return err
			}
			return tx.Model(&Subscription{}).Where("id = ?", cur.ID).
				Update("paid_through", max(cur.PaidThrough, now)+granted).Error
		})
		if err != nil {
			return changed, err
		}
		if granted > 0 {
			changed = true
			log.Warnf(ctx, "[SUB-RECONCILE] stripe cover-through user %d expiry→%d (+%ds, sub=%s)",
				sub.UserID, periodEnd, granted, sub.ProviderSubscriptionID)
		}
	}
	return changed, nil
}
