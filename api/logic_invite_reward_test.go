package center

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// inviteRewardFixture 搭建 邀请人 + 邀请码 + 被邀请人 + 首单订单 的完整链路。
// 所有记录通过 t.Cleanup 硬删除。
type inviteRewardFixture struct {
	inviter User
	invitee User
	code    InviteCode
	order   Order
}

func setupInviteRewardFixture(t *testing.T, planMonth int) *inviteRewardFixture {
	t.Helper()
	now := time.Now()
	suffix := fmt.Sprintf("%d-%d", now.UnixNano()%1e10, planMonth)

	f := &inviteRewardFixture{}

	f.inviter = User{
		UUID:      "usr-inviter-" + suffix,
		ExpiredAt: now.Unix(),
	}
	require.NoError(t, db.Get().Create(&f.inviter).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&f.inviter) })

	f.code = InviteCode{UserID: f.inviter.ID}
	require.NoError(t, db.Get().Create(&f.code).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&f.code) })

	f.invitee = User{
		UUID:            "usr-invitee-" + suffix,
		ExpiredAt:       now.Unix(),
		InvitedByCodeID: f.code.ID,
	}
	require.NoError(t, db.Get().Create(&f.invitee).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&f.invitee) })

	f.order = Order{
		UUID:      "ord-invite-reward-" + suffix,
		Title:     fmt.Sprintf("Test %dm", planMonth),
		UserID:    f.invitee.ID,
		PayAmount: 1000,
		Meta:      "{}",
	}
	require.NoError(t, f.order.SetPlan(&Plan{Month: planMonth}))
	require.NoError(t, db.Get().Create(&f.order).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&f.order) })

	t.Cleanup(func() {
		db.Get().Unscoped().
			Where("user_id IN ?", []uint64{f.inviter.ID, f.invitee.ID}).
			Delete(&UserProHistory{})
	})

	return f
}

func (f *inviteRewardFixture) runReward(t *testing.T) {
	t.Helper()
	require.NoError(t, db.Get().Transaction(func(tx *gorm.DB) error {
		return handleInvitePurchaseRewardInTx(context.Background(), tx, &f.order)
	}))
}

func (f *inviteRewardFixture) rewardHistories(t *testing.T) (invitee, inviter []UserProHistory) {
	t.Helper()
	require.NoError(t, db.Get().
		Where(&UserProHistory{UserID: f.invitee.ID, Type: VipInvitedReward}).
		Find(&invitee).Error)
	require.NoError(t, db.Get().
		Where(&UserProHistory{UserID: f.inviter.ID, Type: VipInviteReward}).
		Find(&inviter).Error)
	return invitee, inviter
}

// TestInviteReward_QualifyingPlan_GrantsBothSides 首单套餐月数达到门槛 → 双方发奖励。
func TestInviteReward_QualifyingPlan_GrantsBothSides(t *testing.T) {
	skipIfNoConfig(t)
	cfg := configInvite(context.Background())

	f := setupInviteRewardFixture(t, cfg.MinRewardMonths) // 恰好等于门槛，验证边界含等号
	f.runReward(t)

	inviteeHist, inviterHist := f.rewardHistories(t)
	require.Len(t, inviteeHist, 1, "invitee should receive exactly one reward history")
	require.Equal(t, cfg.PurchaseRewardDays, inviteeHist[0].Days)
	require.Len(t, inviterHist, 1, "inviter should receive exactly one reward history")
	require.Equal(t, cfg.InviterPurchaseRewardDays, inviterHist[0].Days)

	// ExpiredAt 双方均已延长
	var invitee2, inviter2 User
	require.NoError(t, db.Get().First(&invitee2, f.invitee.ID).Error)
	require.NoError(t, db.Get().First(&inviter2, f.inviter.ID).Error)
	require.Greater(t, invitee2.ExpiredAt, f.invitee.ExpiredAt)
	require.Greater(t, inviter2.ExpiredAt, f.inviter.ExpiredAt)
}

// TestInviteReward_OneMonthPlan_NoReward 首单只买 1 个月 → 双方均不发奖励。
func TestInviteReward_OneMonthPlan_NoReward(t *testing.T) {
	skipIfNoConfig(t)
	cfg := configInvite(context.Background())
	require.Greater(t, cfg.MinRewardMonths, 1, "precondition: threshold must exclude 1-month plans")

	f := setupInviteRewardFixture(t, 1)
	f.runReward(t)

	inviteeHist, inviterHist := f.rewardHistories(t)
	require.Empty(t, inviteeHist, "1-month first order must not grant invitee reward")
	require.Empty(t, inviterHist, "1-month first order must not grant inviter reward")

	// ExpiredAt 双方都不变
	var invitee2, inviter2 User
	require.NoError(t, db.Get().First(&invitee2, f.invitee.ID).Error)
	require.NoError(t, db.Get().First(&inviter2, f.inviter.ID).Error)
	require.Equal(t, f.invitee.ExpiredAt, invitee2.ExpiredAt)
	require.Equal(t, f.inviter.ExpiredAt, inviter2.ExpiredAt)
}

// TestInviteReward_BelowThresholdPlan_NoReward 门槛减一个月（如 11 个月）也不发。
func TestInviteReward_BelowThresholdPlan_NoReward(t *testing.T) {
	skipIfNoConfig(t)
	cfg := configInvite(context.Background())

	f := setupInviteRewardFixture(t, cfg.MinRewardMonths-1)
	f.runReward(t)

	inviteeHist, inviterHist := f.rewardHistories(t)
	require.Empty(t, inviteeHist)
	require.Empty(t, inviterHist)
}

// TestInviteReward_NotFirstOrder_NoReward 非首单（IsFirstOrderDone=true）即使买年付也不发。
func TestInviteReward_NotFirstOrder_NoReward(t *testing.T) {
	skipIfNoConfig(t)
	cfg := configInvite(context.Background())

	f := setupInviteRewardFixture(t, cfg.MinRewardMonths)
	require.NoError(t, db.Get().Model(&User{}).
		Where("id = ?", f.invitee.ID).
		Update("is_first_order_done", true).Error)

	f.runReward(t)

	inviteeHist, inviterHist := f.rewardHistories(t)
	require.Empty(t, inviteeHist)
	require.Empty(t, inviterHist)
}

// TestInviteReward_MissingPlan_SkipsWithoutError 订单 Meta 无 plan → 跳过且不报错（不阻断支付）。
func TestInviteReward_MissingPlan_SkipsWithoutError(t *testing.T) {
	skipIfNoConfig(t)

	f := setupInviteRewardFixture(t, 12)
	f.order.Meta = "{}" // 清掉 plan
	require.NoError(t, db.Get().Model(&f.order).Update("meta", "{}").Error)

	f.runReward(t)

	inviteeHist, inviterHist := f.rewardHistories(t)
	require.Empty(t, inviteeHist)
	require.Empty(t, inviterHist)
}

// appleIAPCredit 在事务中执行一笔 Apple IAP 入账（复用被邀请人 fixture）。
func (f *inviteRewardFixture) appleIAPCredit(t *testing.T, plan *Plan, txnID, origTxnID string) {
	t.Helper()
	now := time.Now().Unix()
	require.NoError(t, db.Get().Transaction(func(tx *gorm.DB) error {
		return creditAppleTransaction(context.Background(), tx, f.invitee.ID, &appstore.TransactionInfo{
			BundleId:              appleBundleID(),
			OriginalTransactionId: origTxnID,
			TransactionId:         txnID,
			ProductId:             plan.AppleProductID,
			AppAccountToken:       deriveAppleAccountToken(f.invitee.UUID),
			InAppOwnershipType:    appstore.OwnershipType_PURCHASED,
			Environment:           "Sandbox",
			PurchaseDate:          now * 1000,
			ExpiresDate:           (now + int64(plan.Month)*30*86400) * 1000,
		})
	}))
	t.Cleanup(func() {
		db.Get().Where("user_id = ?", f.invitee.ID).Delete(&SubscriptionCredit{})
		db.Get().Where("user_id = ?", f.invitee.ID).Delete(&Subscription{})
	})
}

func createApplePlan(t *testing.T, month int) *Plan {
	t.Helper()
	uniq := time.Now().UnixNano() % 1e10
	plan := &Plan{
		PID:            fmt.Sprintf("tir%d-%d", month, uniq),
		Label:          "invite reward iap test",
		Price:          1000,
		OriginPrice:    1000,
		Month:          month,
		Tier:           "basic",
		AppleProductID: fmt.Sprintf("io.kaitu.test.inv%dm.%d", month, uniq),
	}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(plan) })
	return plan
}

// TestAppleIAP_InviteReward_QualifyingFirstPurchase Apple IAP 首购年付：双方发奖励，
// 入账与奖励叠加互不覆盖（lost update 防护），续订不重复发。
func TestAppleIAP_InviteReward_QualifyingFirstPurchase(t *testing.T) {
	skipIfNoConfig(t)
	cfg := configInvite(context.Background())

	f := setupInviteRewardFixture(t, cfg.MinRewardMonths)
	plan := createApplePlan(t, cfg.MinRewardMonths)
	uniq := time.Now().UnixNano() % 1e10
	orig := fmt.Sprintf("OTX-INV-%d", uniq)

	f.appleIAPCredit(t, plan, fmt.Sprintf("T1-%d", uniq), orig)

	inviteeHist, inviterHist := f.rewardHistories(t)
	require.Len(t, inviteeHist, 1, "IAP qualifying first purchase must grant invitee reward")
	require.Equal(t, cfg.PurchaseRewardDays, inviteeHist[0].Days)
	require.Len(t, inviterHist, 1, "IAP qualifying first purchase must grant inviter reward")
	require.Equal(t, cfg.InviterPurchaseRewardDays, inviterHist[0].Days)

	// 奖励与订阅入账叠加：ExpiredAt >= now + 订阅周期 + 奖励天数（防止旧快照覆盖奖励）
	var invitee2 User
	require.NoError(t, db.Get().First(&invitee2, f.invitee.ID).Error)
	now := time.Now().Unix()
	minExpiry := now + int64(plan.Month)*30*86400 + int64(cfg.PurchaseRewardDays)*86400 - 3600
	require.GreaterOrEqual(t, invitee2.ExpiredAt, minExpiry,
		"subscription credit must stack on top of invite reward, not overwrite it")
	require.NotNil(t, invitee2.IsFirstOrderDone)
	require.True(t, *invitee2.IsFirstOrderDone)

	// 续订交易不重复发奖励
	f.appleIAPCredit(t, plan, fmt.Sprintf("T2-%d", uniq), orig)
	inviteeHist, inviterHist = f.rewardHistories(t)
	require.Len(t, inviteeHist, 1, "renewal must not re-grant invitee reward")
	require.Len(t, inviterHist, 1, "renewal must not re-grant inviter reward")
}

// TestAppleIAP_InviteReward_MonthlyNoReward Apple IAP 首购月付：不发奖励，入账正常。
func TestAppleIAP_InviteReward_MonthlyNoReward(t *testing.T) {
	skipIfNoConfig(t)
	cfg := configInvite(context.Background())
	require.Greater(t, cfg.MinRewardMonths, 1, "precondition: threshold must exclude 1-month plans")

	f := setupInviteRewardFixture(t, 1)
	plan := createApplePlan(t, 1)
	uniq := time.Now().UnixNano() % 1e10

	f.appleIAPCredit(t, plan, fmt.Sprintf("T1M-%d", uniq), fmt.Sprintf("OTX-INVM-%d", uniq))

	inviteeHist, inviterHist := f.rewardHistories(t)
	require.Empty(t, inviteeHist, "1-month IAP first purchase must not grant invitee reward")
	require.Empty(t, inviterHist, "1-month IAP first purchase must not grant inviter reward")

	// 订阅入账本身照常生效
	var invitee2 User
	require.NoError(t, db.Get().First(&invitee2, f.invitee.ID).Error)
	require.Greater(t, invitee2.ExpiredAt, f.invitee.ExpiredAt, "subscription credit must still apply")
	require.NotNil(t, invitee2.IsFirstOrderDone)
	require.True(t, *invitee2.IsFirstOrderDone)
}

// TestConfigInvite_MinRewardMonths 验证门槛解析：
// 未配置时默认 12；显式配置（含 0 = 关闭门槛）按配置值生效，不被零值兜底改写。
func TestConfigInvite_MinRewardMonths(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()

	if !viper.IsSet("invite.min_reward_months") {
		require.Equal(t, 12, configInvite(ctx).MinRewardMonths,
			"invite.min_reward_months unset, default must be 12")
	}

	// 显式 0 = 关闭门槛，必须原样生效（viper.Set 进程内覆盖，测试结束后还原）
	orig := viper.Get("invite.min_reward_months")
	viper.Set("invite.min_reward_months", 0)
	t.Cleanup(func() { viper.Set("invite.min_reward_months", orig) })
	require.Equal(t, 0, configInvite(ctx).MinRewardMonths,
		"explicit 0 must disable the threshold, not fall back to 12")

	viper.Set("invite.min_reward_months", 6)
	require.Equal(t, 6, configInvite(ctx).MinRewardMonths)
}
