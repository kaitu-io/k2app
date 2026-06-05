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

// computeRecurringEntitlement 纯函数：给定用户当前到期(unix 秒)、续订周期绝对到期(unix 秒)、
// now(unix 秒)，返回新到期、审计净增天数、是否推进。续订型以绝对周期到期为真相源：
// 只抬升、绝不缩短（幂等）。provider 无关。
func computeRecurringEntitlement(currentExpiredAt, periodEndSec, nowSec int64) (newExpiredAt int64, auditDays int, advanced bool) {
	if periodEndSec <= currentExpiredAt {
		return currentExpiredAt, 0, false
	}
	base := currentExpiredAt
	if base < nowSec {
		base = nowSec
	}
	days := int((periodEndSec - base) / 86400)
	return periodEndSec, days, true
}

// planByAppleProductID 按 Apple 商品ID 查套餐；找不到即拒绝入账（未知商品）。
func planByAppleProductID(ctx context.Context, tx *gorm.DB, productID string) (*Plan, error) {
	var plan Plan
	if err := tx.Where(&Plan{AppleProductID: productID}).First(&plan).Error; err != nil {
		return nil, fmt.Errorf("no plan for apple product %s: %w", productID, err)
	}
	return &plan, nil
}

// upsertSubscription 按 (provider, provider_subscription_id) upsert 订阅行。
// 关键安全约束：已存在时绝不修改 UserID（first-write-wins），防伪造/重放改归属。
// 事务内调用，对订阅行加 FOR UPDATE。
func upsertSubscription(ctx context.Context, tx *gorm.DB, in *Subscription) (*Subscription, error) {
	var existing Subscription
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(&Subscription{Provider: in.Provider, ProviderSubscriptionID: in.ProviderSubscriptionID}).
		First(&existing).Error
	if err == nil {
		existing.ProductID = in.ProductID
		existing.ProviderLatestRef = in.ProviderLatestRef
		if in.CurrentPeriodEnd > existing.CurrentPeriodEnd {
			existing.CurrentPeriodEnd = in.CurrentPeriodEnd
		}
		existing.AutoRenew = in.AutoRenew
		existing.Environment = in.Environment
		if in.Status != "" {
			existing.Status = in.Status
		}
		if err := tx.Save(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if err := tx.Create(in).Error; err != nil {
		return nil, err
	}
	return in, nil
}

// applyRecurringSubscription 用续订周期绝对到期抬升用户权益（max 语义，幂等）。
// 事务内调用，对用户行加 FOR UPDATE。
func applyRecurringSubscription(ctx context.Context, tx *gorm.DB, sub *Subscription) error {
	var user User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, sub.UserID).Error; err != nil {
		return fmt.Errorf("lock user %d: %w", sub.UserID, err)
	}

	newExpiry, auditDays, advanced := computeRecurringEntitlement(user.ExpiredAt, sub.CurrentPeriodEnd, time.Now().Unix())
	if !advanced {
		log.Debugf(ctx, "[applyRecurringSubscription] user %d no advance (periodEnd=%d <= current=%d)",
			user.ID, sub.CurrentPeriodEnd, user.ExpiredAt)
		return nil
	}

	user.ExpiredAt = newExpiry
	if user.IsFirstOrderDone == nil || !*user.IsFirstOrderDone {
		if plan, _ := planByAppleProductID(ctx, tx, sub.ProductID); plan != nil && plan.Tier != "" {
			user.Tier = plan.Tier
		}
		user.IsFirstOrderDone = BoolPtr(true)
	}
	if user.IsActivated == nil || !*user.IsActivated {
		user.IsActivated = BoolPtr(true)
		user.ActivatedAt = time.Now().Unix()
	}
	if err := tx.Save(&user).Error; err != nil {
		return fmt.Errorf("save user %d: %w", user.ID, err)
	}

	history := &UserProHistory{
		UserID:      user.ID,
		Type:        VipAppleSub,
		ReferenceID: sub.ID,
		Days:        auditDays,
		Reason:      fmt.Sprintf("%s 订阅入账 - %s", sub.Provider, sub.ProviderSubscriptionID),
	}
	if err := tx.Create(history).Error; err != nil {
		return fmt.Errorf("write pro history: %w", err)
	}

	log.Infof(ctx, "[applyRecurringSubscription] user %d expiry → %s (+%dd) via %s sub %s",
		user.ID, time.Unix(newExpiry, 0).Format("2006-01-02"), auditDays, sub.Provider, sub.ProviderSubscriptionID)
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

	// 防盗用（defense-in-depth）：交易若携带 appAccountToken，必须等于本用户派生值。
	// 阻断"偷到他人 transactionId 用自己会话冒领"——购买时 token 绑的是受害者账号。
	// token 为空（旧客户端/边缘）时退回 first-write-wins 单一保护，不强拒。
	if info.AppAccountToken != "" {
		var u User
		if err := getDB().Select("uuid").First(&u, userID).Error; err != nil {
			return fmt.Errorf("load user for appAccountToken check: %w", err)
		}
		if want := deriveAppleAccountToken(u.UUID); !strings.EqualFold(info.AppAccountToken, want) {
			return fmt.Errorf("appAccountToken mismatch: transaction bound to a different account")
		}
	}

	return withDeadlockRetry(ctx, 3, func(tx *gorm.DB) error {
		if _, err := planByAppleProductID(ctx, tx, info.ProductId); err != nil {
			return err
		}
		sub, err := upsertSubscription(ctx, tx, &Subscription{
			UserID:                 userID,
			Provider:               "apple",
			ProviderSubscriptionID: info.OriginalTransactionId,
			ProductID:              info.ProductId,
			ProviderLatestRef:      info.TransactionId,
			CurrentPeriodEnd:       info.ExpiresDate / 1000, // Apple ms → s 归一
			AutoRenew:              true,                     // 入账即自动续订（best-effort；DID_CHANGE_RENEWAL_STATUS 后续修正）
			Environment:            info.Environment,
			Status:                 "active",
		})
		if err != nil {
			return err
		}
		return applyRecurringSubscription(ctx, tx, sub)
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
			log.Infof(ctx, "[revokeSubscription] user %d entitlement clawed back (-%dd) for %s sub %s",
				user.ID, cutDays, sub.Provider, sub.ProviderSubscriptionID)
		}
		return tx.Model(&Subscription{}).Where("id = ?", sub.ID).Update("status", "revoked").Error
	})
}

// activeSubStatuses 视为"活跃"的状态：grace/billing_retry 也算活跃，避免在 Apple 仍在
// 重试扣费时向用户兜售第二份订阅（防双扣）。
var activeSubStatuses = []string{"active", "grace", "billing_retry"}

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
	out := make([]DataSubscription, 0, len(subs))
	for i := range subs {
		s := &subs[i]
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
