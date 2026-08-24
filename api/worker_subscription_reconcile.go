package center

import (
	"context"
	"fmt"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
	subscription "github.com/stripe/stripe-go/v82/subscription"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
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
// 幂等:交易重放由 SubscriptionCredit 去重;revoked 绝不触碰(clawback 归退款流)。
func reconcileSubscription(ctx context.Context, sub *Subscription, now int64) (bool, error) {
	if sub.Status == "revoked" {
		return false, nil
	}
	switch sub.Provider {
	case "apple":
		return reconcileAppleSubscription(ctx, sub, now)
	case "stripe":
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

	changed := false
	// 最新交易灌回现有入账路径:cover-through(Task 1)保证 ExpiredAt 收敛,
	// SubscriptionCredit 去重保证幂等——已入账过的交易此调用是无害 no-op。
	if st.txn != nil {
		before := sub.CurrentPeriodEnd
		if err := db.Get().Transaction(func(tx *gorm.DB) error {
			return creditAppleTransaction(ctx, tx, sub.UserID, st.txn)
		}); err != nil {
			return false, err
		}
		if err := db.Get().First(sub, sub.ID).Error; err != nil {
			return false, err
		}
		changed = sub.CurrentPeriodEnd != before
	}
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
	// Apple 报已过期且本地仍标活跃 → 落终态(revoked 已在入口挡掉)。
	if st.status == appstore.SubscriptionStatus_Expired && sub.Status != "expired" {
		if err := db.Get().Model(&Subscription{}).Where("id = ?", sub.ID).
			Update("status", "expired").Error; err != nil {
			return changed, err
		}
		log.Infof(ctx, "[SUB-RECONCILE] sub %s marked expired (was %s)", sub.ProviderSubscriptionID, sub.Status)
		changed = true
	}
	// Apple 报 revoked 而本地不是:退款流没跑到,涉及 clawback,人工介入。
	if st.status == appstore.SubscriptionStatus_Revoked {
		log.Errorf(ctx, "[SUB-RECONCILE] sub %s revoked on Apple but local status=%s — refund flow missed, manual review required",
			sub.ProviderSubscriptionID, sub.Status)
	}
	return changed, nil
}

// handleSubscriptionReconcileTask 每日 cron:扫临期/陈旧的活跃订阅行逐条对账。
// 单条失败记日志跳过,不阻塞整轮。
func handleSubscriptionReconcileTask(ctx context.Context, _ []byte) error {
	now := time.Now().Unix()
	var subs []Subscription
	if err := db.Get().Where("status IN ?", activeSubStatuses).
		Where("current_period_end < ?", now+reconcileLookahead).
		Find(&subs).Error; err != nil {
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

	// 活跃且周期在未来 → cover-through 权益(与 credit 路径同一收敛不变式)。原子条件
	// UPDATE,不先读整行再比较——避免与并发 credit 事务的 lost-update。
	if status == "active" && periodEnd > now {
		res := db.Get().Model(&User{}).
			Where("id = ? AND expired_at < ?", sub.UserID, periodEnd).
			Update("expired_at", periodEnd)
		if res.Error != nil {
			return changed, res.Error
		}
		if res.RowsAffected > 0 {
			changed = true
			log.Warnf(ctx, "[SUB-RECONCILE] stripe cover-through user %d expiry→%d (sub=%s)",
				sub.UserID, periodEnd, sub.ProviderSubscriptionID)
		}
	}
	return changed, nil
}
