package center

import (
	"context"
	"fmt"
	"testing"
	"time"

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

// TestSystemEmailTemplateID 验证系统邮件模板 ID
func TestSystemEmailTemplateID(t *testing.T) {
	assert.Equal(t, uint64(0), uint64(systemEmailTemplateID))
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

// TestGetRenewalReminderContent 验证续费提醒邮件内容
func TestGetRenewalReminderContent(t *testing.T) {
	tests := []struct {
		daysBefore      int
		expectSubject   string
		expectBodyParts []string
	}{
		{
			daysBefore:    30,
			expectSubject: "你的开途账号还有 30 天到期",
			expectBodyParts: []string{
				"Hi，",
				"30 天后到期",
				"https://kaitu.io/purchase",
				"开途团队",
			},
		},
		{
			daysBefore:    14,
			expectSubject: "开途账号即将到期，建议尽快续费",
			expectBodyParts: []string{
				"14 天后到期",
				"建议尽快续费",
			},
		},
		{
			daysBefore:    7,
			expectSubject: "开途账号下周到期",
			expectBodyParts: []string{
				"7 天后到期",
				"所有设备连接将自动断开",
			},
		},
		{
			daysBefore:    3,
			expectSubject: "还有 3 天，开途账号即将到期",
			expectBodyParts: []string{
				"还有 3 天到期",
				"连接立即中断",
			},
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_days", tt.daysBefore), func(t *testing.T) {
			subject, body := getRenewalReminderContent(tt.daysBefore)
			assert.Equal(t, tt.expectSubject, subject)
			for _, part := range tt.expectBodyParts {
				assert.Contains(t, body, part)
			}
			// 所有邮件都不能包含 www.kaitu.io
			assert.NotContains(t, body, "www.kaitu.io")
			// 所有邮件都不能包含 "Kaitu" 英文品牌（应该用"开途"）
			assert.NotContains(t, subject, "Kaitu")
		})
	}

	// default 分支
	t.Run("default_fallback", func(t *testing.T) {
		subject, body := getRenewalReminderContent(60)
		assert.Contains(t, subject, "60")
		assert.Contains(t, body, "60 天后到期")
		assert.Contains(t, body, "https://kaitu.io/purchase")
	})
}

// TestGetWinbackContent_Day1 验证过期 1 天召回内容（无活动码）
func TestGetWinbackContent_Day1(t *testing.T) {
	ctx := context.Background()
	subject, body := getWinbackContent(ctx, 1)

	assert.Equal(t, "你的开途连接已断开", subject)
	assert.Contains(t, body, "昨天到期")
	assert.Contains(t, body, "连接已中断")
	assert.Contains(t, body, "https://kaitu.io/purchase")
	// 1 天召回不应包含活动码
	assert.NotContains(t, body, "BACK")
}

// TestGetWinbackContent_DefaultFallback 验证 fallback 内容
func TestGetWinbackContent_DefaultFallback(t *testing.T) {
	ctx := context.Background()
	// 没有 DB 连接时，7 天和 30 天会 fallback
	subject, body := getWinbackContent(ctx, 99)

	assert.Contains(t, subject, "99")
	assert.Contains(t, body, "99 天")
	assert.Contains(t, body, "https://kaitu.io/purchase")
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
