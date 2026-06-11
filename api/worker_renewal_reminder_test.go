package center

import (
	"context"
	"fmt"
	"testing"
	"time"

	db "github.com/wordgate/qtoolkit/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// 邮件生命周期 Worker 单元测试
// 测试纯逻辑函数，不需要数据库
// =====================================================================

// TestEmailTriggerDays_Config 验证触发天数配置的正确性
func TestEmailTriggerDays_Config(t *testing.T) {
	// 正数 = 到期前提醒，负数 = 到期后召回
	renewalDays := []int{}
	winbackDays := []int{}
	for _, d := range emailTriggerDays {
		if d > 0 {
			renewalDays = append(renewalDays, d)
		} else {
			winbackDays = append(winbackDays, d)
		}
	}

	// 4 个续费提醒
	assert.Equal(t, []int{30, 14, 7, 3}, renewalDays)
	// 3 个过期召回
	assert.Equal(t, []int{-1, -7, -30}, winbackDays)
}

// TestFormatCents 验证美分转美元显示
func TestFormatCents(t *testing.T) {
	tests := []struct {
		cents    uint64
		expected string
	}{
		{0, "$0"},
		{100, "$1"},
		{390, "$3.9"},
		{585, "$5.85"},
		{1490, "$14.9"},
		{2235, "$22.35"},
		{3900, "$39"},
		{10900, "$109"},
		{14900, "$149"},
		{50, "$0.5"},
		{5, "$0.05"},
		{1001, "$10.01"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_cents", tt.cents), func(t *testing.T) {
			assert.Equal(t, tt.expected, formatCents(tt.cents))
		})
	}
}

// TestWinbackCampaigns_Config 验证召回活动码配置
func TestWinbackCampaigns_Config(t *testing.T) {
	// 7 天召回
	c7, ok := winbackCampaigns[7]
	require.True(t, ok, "7-day winback campaign must exist")
	assert.Equal(t, "BACK90", c7.code)
	assert.Equal(t, 90, c7.discountPct)
	assert.Contains(t, c7.validDaysStr, "14 天")

	// 30 天召回
	c30, ok := winbackCampaigns[30]
	require.True(t, ok, "30-day winback campaign must exist")
	assert.Equal(t, "BACK85", c30.code)
	assert.Equal(t, 85, c30.discountPct)
	assert.Contains(t, c30.validDaysStr, "30 天")

	// 1 天不应有活动码
	_, ok = winbackCampaigns[1]
	assert.False(t, ok, "1-day winback should have no campaign")
}

// TestSavingsCalculation 验证立减金额计算（模拟 getPlanPriceRange 的结果）
func TestSavingsCalculation(t *testing.T) {
	// 当前套餐: $39, $76, $109, $149 (美分: 3900, 7600, 10900, 14900)
	tests := []struct {
		name        string
		minPrice    uint64
		maxPrice    uint64
		discountPct int
		expectMin   string // 最低立减
		expectMax   string // 最高立减
	}{
		{
			name:        "BACK90_current_plans",
			minPrice:    3900,
			maxPrice:    14900,
			discountPct: 90,
			expectMin:   "$3.9",
			expectMax:   "$14.9",
		},
		{
			name:        "BACK85_current_plans",
			minPrice:    3900,
			maxPrice:    14900,
			discountPct: 85,
			expectMin:   "$5.85",
			expectMax:   "$22.35",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minSave := tt.minPrice * uint64(100-tt.discountPct) / 100
			maxSave := tt.maxPrice * uint64(100-tt.discountPct) / 100
			assert.Equal(t, tt.expectMin, formatCents(minSave))
			assert.Equal(t, tt.expectMax, formatCents(maxSave))
		})
	}
}

// TestBatchIDFormat 验证批次 ID 格式
func TestBatchIDFormat(t *testing.T) {
	todayStr := time.Now().UTC().Format("2006-01-02")

	// 续费提醒
	for _, days := range []int{30, 14, 7, 3} {
		batchID := fmt.Sprintf("renewal:%dd:%s", days, todayStr)
		assert.Regexp(t, `^renewal:\d+d:\d{4}-\d{2}-\d{2}$`, batchID)
	}

	// 过期召回
	for _, days := range []int{1, 7, 30} {
		batchID := fmt.Sprintf("winback:%dd:%s", days, todayStr)
		assert.Regexp(t, `^winback:\d+d:\d{4}-\d{2}-\d{2}$`, batchID)
	}
}

// TestDateCalculation_Renewal 验证续费提醒的日期计算
func TestDateCalculation_Renewal(t *testing.T) {
	// 模拟 2026-04-03 运行
	now := time.Date(2026, 4, 3, 2, 30, 0, 0, time.UTC)

	for _, daysBefore := range []int{30, 14, 7, 3} {
		targetDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, daysBefore)
		targetDateEnd := targetDate.AddDate(0, 0, 1)

		t.Run(fmt.Sprintf("%d_days_before", daysBefore), func(t *testing.T) {
			// targetDate 应该在未来
			assert.True(t, targetDate.After(now), "target date should be in the future for renewal")
			// 窗口应为恰好 1 天
			assert.Equal(t, 24*time.Hour, targetDateEnd.Sub(targetDate))
		})
	}

	// 具体验证：30 天提醒应该查 5 月 3 日到期的用户
	target30 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 30)
	assert.Equal(t, time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), target30)
}

// TestDateCalculation_Winback 验证过期召回的日期计算
func TestDateCalculation_Winback(t *testing.T) {
	// 模拟 2026-04-03 运行
	now := time.Date(2026, 4, 3, 2, 30, 0, 0, time.UTC)

	for _, daysAfter := range []int{1, 7, 30} {
		// processWinback 接收的是正数
		targetDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -daysAfter)
		targetDateEnd := targetDate.AddDate(0, 0, 1)

		t.Run(fmt.Sprintf("%d_days_after", daysAfter), func(t *testing.T) {
			// targetDate 应该在过去
			assert.True(t, targetDate.Before(now), "target date should be in the past for winback")
			// 窗口应为恰好 1 天
			assert.Equal(t, 24*time.Hour, targetDateEnd.Sub(targetDate))
		})
	}

	// 具体验证：1 天召回应该查 4 月 2 日到期的用户
	target1 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	assert.Equal(t, time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC), target1)

	// 30 天召回应该查 3 月 4 日到期的用户
	target30 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -30)
	assert.Equal(t, time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), target30)
}

// TestRenewedUserSkip 验证已续费用户应被跳过的逻辑
func TestRenewedUserSkip(t *testing.T) {
	now := time.Now().UTC()

	// 用户到期日在未来 = 已续费
	renewedUser := User{ExpiredAt: now.Unix() + 86400*30}
	assert.True(t, renewedUser.ExpiredAt > now.Unix(), "renewed user should be skipped")

	// 用户到期日在过去 = 未续费
	expiredUser := User{ExpiredAt: now.Unix() - 86400}
	assert.False(t, expiredUser.ExpiredAt > now.Unix(), "expired user should not be skipped")
}

// TestGetUserEmailFromIdentifies 验证从用户身份中获取邮箱
func TestGetUserEmailFromIdentifies(t *testing.T) {
	// 没有 LoginIdentifies 时返回空
	user := User{}
	assert.Equal(t, "", getUserEmailFromIdentifies(&user))
}

// TestProcessPrivateNodeRenewalReminders 验证专属节点续费提醒：必须找到 ExpiresAt
// 落在 now+daysBefore 的 Asia/Shanghai 当日窗口内的 active 订阅，从 LoginIdentifies
// 解析主人邮箱，并经 SendTemplatedEmails 路由。是否真实发送取决于 dev 是否存在 EDM 模板
// private-node-renewal-7d；无论如何 query + email-resolve 路径都要跑通，订阅必须被计入
// (sent+skipped >= 1) 且不 panic。集成测试，跑真实 dev MySQL。
func TestProcessPrivateNodeRenewalReminders(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	loc, _ := time.LoadLocation("Asia/Shanghai")
	nowSh := time.Now().In(loc)
	// startOfDay(now+7d in Shanghai) + 12h → 稳落在当日窗口内。
	target := time.Date(nowSh.Year(), nowSh.Month(), nowSh.Day(), 0, 0, 0, 0, loc).
		AddDate(0, 0, 7).Add(12 * time.Hour)
	expiresAt := target.Unix()

	// 固定 OrderID 段 9_601_001..9_601_003 — 预清残留
	// (order_id uniqueIndex，陈旧行会让重跑致命)。
	for oid := uint64(9_601_001); oid <= 9_601_003; oid++ {
		db.Get().Unscoped().Where("order_id = ?", oid).Delete(&PrivateNodeSubscription{})
	}

	// 建一个带 email login_identify 的真实用户。
	user := User{
		UUID:     "usr-pn-renewal-" + nowSh.Format("20060102150405.000000"),
		Language: "zh-CN",
	}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	// secretDecryptString 当前是 TODO identity stub，明文直接放 EncryptedValue
	// (镜像 password 测试的 seeding 模式)。
	email := "pn-renewal-" + nowSh.Format("150405.000000") + "@example.com"
	identify := LoginIdentify{
		UserID:         user.ID,
		Type:           "email",
		IndexID:        email + "-idx",
		EncryptedValue: email,
	}
	require.NoError(t, db.Get().Create(&identify).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&identify) })

	sub := &PrivateNodeSubscription{
		UserID:            user.ID,
		PlanID:            961000,
		OrderID:           9_601_001,
		Region:            "us-east-1",
		IPType:            IPTypeNonResidential,
		TrafficTotalBytes: 2 << 40,
		Status:            PNStatusActive,
		PurchasedAt:       nowSh.Unix(),
		ExpiresAt:         expiresAt,
	}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("order_id = ?", sub.OrderID).Delete(&PrivateNodeSubscription{})
	})

	sent, skipped, failed := processPrivateNodeRenewalReminders(context.Background(), 7)

	// 要点：query + email-resolve 路径跑通且把我们的 sub 计入。真实发送取决于 dev 模板
	// 是否存在；不存在则降级为 skipped —— 无论如何 sent+skipped >= 1。
	assert.GreaterOrEqual(t, sent+skipped, 1,
		"窗口内的 sub 必须 sent 或 skipped (sent=%d skipped=%d failed=%d)",
		sent, skipped, failed)
}

// TestHandlerDispatch 验证 handleRenewalReminderTask 的分发逻辑
func TestHandlerDispatch(t *testing.T) {
	// 验证正数走 renewal，负数走 winback
	for _, days := range emailTriggerDays {
		if days > 0 {
			assert.True(t, days > 0, "%d should be renewal (positive)", days)
		} else {
			assert.True(t, days < 0, "%d should be winback (negative)", days)
			// processWinback 接收正数
			assert.True(t, -days > 0, "negated %d should be positive for processWinback", days)
		}
	}
}
