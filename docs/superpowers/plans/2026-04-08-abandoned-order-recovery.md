# Abandoned Order Recovery Email Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Send automated recovery emails to users who created orders but never paid, following a 1h/1d/3d/7d/14d/30d cadence with escalating discounts — converting purchase intent into revenue.

**Architecture:** New `worker_abandoned_order.go` following the exact pattern of `worker_renewal_reminder.go`. Two cron schedules: hourly for the 1h trigger, daily for 1d–30d triggers. Query `orders` table for `is_paid=false` within time windows, exclude users who subsequently paid, deduplicate per-user, send via existing `SendTemplatedEmails`. Campaign codes for 3d+ emails use friendly words (not discount-revealing), all with `first_order` matcher.

**Tech Stack:** Go, GORM, Asynq cron, existing `SendTemplatedEmails` + `EmailSendLog`, existing `Campaign` system for discount codes

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `api/worker_abandoned_order.go` | Create | Abandoned order recovery cron handler + query logic |
| `api/worker_abandoned_order_test.go` | Create | Unit tests for config, date math, batch ID format, filtering logic |
| `api/worker_integration.go` | Modify | Register two new cron entries (hourly + daily) |

No model changes needed. The `Order` model already has `IsPaid`, `CreatedAt`, `UserID`, `Title`, `PayAmount`.

---

### Task 1: Write Unit Tests for Configuration and Date Math

**Files:**
- Create: `api/worker_abandoned_order_test.go`

- [ ] **Step 1: Write the test file with config and date math tests**

```go
package center

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAbandonedTriggerDelays_Config validates the trigger delay configuration
func TestAbandonedTriggerDelays_Config(t *testing.T) {
	// Hourly triggers (in hours)
	assert.Equal(t, []int{1}, abandonedHourlyDelays)
	// Daily triggers (in days)
	assert.Equal(t, []int{1, 3, 7, 14, 30}, abandonedDailyDelays)
}

// TestAbandonedCampaigns_Config validates campaign code configuration
func TestAbandonedCampaigns_Config(t *testing.T) {
	// 1h and 1d have no campaign (pure reminder)
	_, ok := abandonedCampaigns[1]
	assert.False(t, ok, "1-day abandoned should have no campaign")

	// 3d: 95% (5% off)
	c3, ok := abandonedCampaigns[3]
	require.True(t, ok, "3-day abandoned campaign must exist")
	assert.Equal(t, "READY4U", c3.code)
	assert.Equal(t, 95, c3.discountPct)

	// 7d: 90% (10% off)
	c7, ok := abandonedCampaigns[7]
	require.True(t, ok, "7-day abandoned campaign must exist")
	assert.Equal(t, "STAYFREE", c7.code)
	assert.Equal(t, 90, c7.discountPct)

	// 14d: 90%
	c14, ok := abandonedCampaigns[14]
	require.True(t, ok, "14-day abandoned campaign must exist")
	assert.Equal(t, "SMOOTHDAY", c14.code)
	assert.Equal(t, 90, c14.discountPct)

	// 30d: 85% (15% off)
	c30, ok := abandonedCampaigns[30]
	require.True(t, ok, "30-day abandoned campaign must exist")
	assert.Equal(t, "KEEPGOING", c30.code)
	assert.Equal(t, 85, c30.discountPct)
}

// TestAbandonedBatchIDFormat validates batch ID patterns
func TestAbandonedBatchIDFormat(t *testing.T) {
	now := time.Date(2026, 4, 8, 3, 0, 0, 0, time.UTC)
	nowStr := now.Format("2006-01-02")
	hourStr := now.Format("2006-01-02-15")

	// Hourly batch ID includes hour for uniqueness
	batchID := fmt.Sprintf("abandoned:%dh:%s", 1, hourStr)
	assert.Equal(t, "abandoned:1h:2026-04-08-03", batchID)

	// Daily batch ID uses date only
	batchID = fmt.Sprintf("abandoned:%dd:%s", 3, nowStr)
	assert.Equal(t, "abandoned:3d:2026-04-08", batchID)
}

// TestAbandonedHourlyWindow validates the 1h time window calculation
func TestAbandonedHourlyWindow(t *testing.T) {
	// Simulating cron run at 2026-04-08 03:00 UTC
	now := time.Date(2026, 4, 8, 3, 0, 0, 0, time.UTC)

	// For 1h delay: look at orders created between 1.5h and 0.5h ago
	// This gives a 1h window centered roughly on the 1h mark
	windowEnd := now.Add(-30 * time.Minute)   // 02:30
	windowStart := now.Add(-90 * time.Minute)  // 01:30

	assert.Equal(t, time.Date(2026, 4, 8, 1, 30, 0, 0, time.UTC), windowStart)
	assert.Equal(t, time.Date(2026, 4, 8, 2, 30, 0, 0, time.UTC), windowEnd)
	assert.Equal(t, time.Hour, windowEnd.Sub(windowStart))
}

// TestAbandonedDailyWindow validates the daily time window calculation
func TestAbandonedDailyWindow(t *testing.T) {
	// Simulating cron run at 2026-04-08 02:30 UTC
	now := time.Date(2026, 4, 8, 2, 30, 0, 0, time.UTC)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// For 3-day delay: orders created on 2026-04-05
	targetDate := today.AddDate(0, 0, -3)
	targetDateEnd := targetDate.AddDate(0, 0, 1)

	assert.Equal(t, time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC), targetDate)
	assert.Equal(t, time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC), targetDateEnd)
	assert.Equal(t, 24*time.Hour, targetDateEnd.Sub(targetDate))
}

// TestAbandonedSavingsCalculation validates discount amount formatting
func TestAbandonedSavingsCalculation(t *testing.T) {
	tests := []struct {
		name        string
		minPrice    uint64
		maxPrice    uint64
		discountPct int
		expectMin   string
		expectMax   string
	}{
		{
			name:        "READY4U_95pct",
			minPrice:    3900,
			maxPrice:    14900,
			discountPct: 95,
			expectMin:   "$1.95",
			expectMax:   "$7.45",
		},
		{
			name:        "STAYFREE_90pct",
			minPrice:    3900,
			maxPrice:    14900,
			discountPct: 90,
			expectMin:   "$3.9",
			expectMax:   "$14.9",
		},
		{
			name:        "KEEPGOING_85pct",
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
```

- [ ] **Step 2: Run tests — they should fail (functions not defined)**

Run: `cd api && go test -run TestAbandoned -v`
Expected: compilation errors — `abandonedHourlyDelays`, `abandonedDailyDelays`, `abandonedCampaigns` undefined.

---

### Task 2: Implement Core Worker Logic

**Files:**
- Create: `api/worker_abandoned_order.go`

- [ ] **Step 3: Create the worker file**

```go
package center

import (
	"context"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
)

// =====================================================================
// 未支付订单召回 Worker
// 基于 Asynq Cron，两个调度：
//   - 每小时运行：处理 1h 延迟（即时提醒）
//   - 每天 03:00 UTC 运行：处理 1d/3d/7d/14d/30d 延迟
// =====================================================================

const (
	TaskTypeAbandonedOrderHourly = "abandoned:hourly"
	TaskTypeAbandonedOrderDaily  = "abandoned:daily"
)

// 触发延迟配置
var (
	abandonedHourlyDelays = []int{1}              // 小时为单位
	abandonedDailyDelays  = []int{1, 3, 7, 14, 30} // 天为单位
)

// abandonedCampaign 未支付召回的活动码配置
type abandonedCampaign struct {
	code         string
	discountPct  int
	validDaysStr string
}

// 活动码配置：daysAfter → campaign
// 1h 和 1d 无优惠码（纯提醒），3d+ 有折扣
var abandonedCampaigns = map[int]abandonedCampaign{
	3:  {code: "READY4U", discountPct: 95, validDaysStr: "7 天内有效"},
	7:  {code: "STAYFREE", discountPct: 90, validDaysStr: "14 天内有效"},
	14: {code: "SMOOTHDAY", discountPct: 90, validDaysStr: "14 天内有效"},
	30: {code: "KEEPGOING", discountPct: 85, validDaysStr: "30 天内有效"},
}

// handleAbandonedOrderHourlyTask 每小时运行，处理 1h 即时提醒
func handleAbandonedOrderHourlyTask(ctx context.Context, _ []byte) error {
	log.Infof(ctx, "[ABANDONED] Starting hourly abandoned order check")

	var totalSent, totalSkipped, totalFailed int
	for _, hours := range abandonedHourlyDelays {
		sent, skipped, failed := processAbandonedHourly(ctx, hours)
		totalSent += sent
		totalSkipped += skipped
		totalFailed += failed
	}

	log.Infof(ctx, "[ABANDONED] Hourly task completed: sent=%d, skipped=%d, failed=%d",
		totalSent, totalSkipped, totalFailed)
	return nil
}

// handleAbandonedOrderDailyTask 每天运行，处理 1d+ 召回
func handleAbandonedOrderDailyTask(ctx context.Context, _ []byte) error {
	log.Infof(ctx, "[ABANDONED] Starting daily abandoned order check")

	var totalSent, totalSkipped, totalFailed int
	for _, days := range abandonedDailyDelays {
		sent, skipped, failed := processAbandonedDaily(ctx, days)
		totalSent += sent
		totalSkipped += skipped
		totalFailed += failed
	}

	log.Infof(ctx, "[ABANDONED] Daily task completed: sent=%d, skipped=%d, failed=%d",
		totalSent, totalSkipped, totalFailed)
	return nil
}

// processAbandonedHourly 处理小时级未支付订单提醒
func processAbandonedHourly(ctx context.Context, hoursAfter int) (sent, skipped, failed int) {
	log.Infof(ctx, "[ABANDONED] Processing %dh reminder", hoursAfter)

	now := time.Now().UTC()
	hourStr := now.Format("2006-01-02-15")

	// 时间窗口：以 hoursAfter 为中心，前后各 30 分钟
	windowEnd := now.Add(-time.Duration(hoursAfter)*time.Hour + 30*time.Minute)
	windowStart := now.Add(-time.Duration(hoursAfter)*time.Hour - 30*time.Minute)

	batchID := fmt.Sprintf("abandoned:%dh:%s", hoursAfter, hourStr)
	slug := fmt.Sprintf("abandoned-%dh", hoursAfter)

	return processAbandonedOrders(ctx, batchID, slug, windowStart, windowEnd, 0)
}

// processAbandonedDaily 处理天级未支付订单召回
func processAbandonedDaily(ctx context.Context, daysAfter int) (sent, skipped, failed int) {
	log.Infof(ctx, "[ABANDONED] Processing %dd reminder", daysAfter)

	now := time.Now().UTC()
	todayStr := now.Format("2006-01-02")
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// 时间窗口：daysAfter 天前的整天
	windowStart := today.AddDate(0, 0, -daysAfter)
	windowEnd := windowStart.AddDate(0, 0, 1)

	batchID := fmt.Sprintf("abandoned:%dd:%s", daysAfter, todayStr)
	slug := fmt.Sprintf("abandoned-%dd", daysAfter)

	return processAbandonedOrders(ctx, batchID, slug, windowStart, windowEnd, daysAfter)
}

// processAbandonedOrders 通用未支付订单处理
// 查询指定时间窗口内创建且未支付的订单，排除后续已支付的用户，每用户只发一次
func processAbandonedOrders(ctx context.Context, batchID, slug string, windowStart, windowEnd time.Time, daysAfter int) (sent, skipped, failed int) {
	if !templateSlugExists(slug) {
		alertMsg := fmt.Sprintf("[ABANDONED] 模板 slug=%q 不存在，跳过。请在管理后台创建该模板。", slug)
		log.Errorf(ctx, "%s", alertMsg)
		slack.Send("alert", alertMsg)
		return 0, 0, 0
	}

	// 查询时间窗口内未支付的订单
	// 关键过滤：排除该用户在该订单之后有已支付订单的情况
	// 同一用户多个未支付订单只取最近一个（GROUP BY user_id）
	type abandonedOrderInfo struct {
		UserID    uint64
		Title     string
		PayAmount uint64
	}

	var orders []abandonedOrderInfo
	err := db.Get().Model(&Order{}).
		Select("user_id, title, pay_amount").
		Where("is_paid = ? AND created_at >= ? AND created_at < ?", false, windowStart, windowEnd).
		Where("user_id NOT IN (?)",
			db.Get().Model(&Order{}).
				Select("DISTINCT user_id").
				Where("is_paid = ? AND created_at >= ?", true, windowStart),
		).
		Group("user_id").
		Find(&orders).Error

	if err != nil {
		log.Errorf(ctx, "[ABANDONED] Failed to query orders: %v", err)
		return 0, 0, 1
	}

	log.Infof(ctx, "[ABANDONED] Found %d users with unpaid orders (window: %s to %s)",
		len(orders), windowStart.Format("2006-01-02 15:04"), windowEnd.Format("2006-01-02 15:04"))

	if len(orders) == 0 {
		return 0, 0, 0
	}

	// 批量加载用户信息（获取邮箱）
	userIDs := make([]uint64, len(orders))
	for i, o := range orders {
		userIDs[i] = o.UserID
	}

	var users []User
	if err := db.Get().Preload("LoginIdentifies").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		log.Errorf(ctx, "[ABANDONED] Failed to query users: %v", err)
		return 0, 0, 1
	}

	userMap := make(map[uint64]*User, len(users))
	for i := range users {
		userMap[users[i].ID] = &users[i]
	}

	// 构造模板变量
	vars := map[string]string{}
	campaign, hasCampaign := abandonedCampaigns[daysAfter]
	if hasCampaign {
		minPrice, maxPrice, ok := getPlanPriceRange(ctx)
		if ok {
			minSave := minPrice * uint64(100-campaign.discountPct) / 100
			maxSave := maxPrice * uint64(100-campaign.discountPct) / 100
			vars["SavingsText"] = fmt.Sprintf("立减 %s 起，最高立减 %s", formatCents(minSave), formatCents(maxSave))
			vars["CampaignCode"] = campaign.code
			vars["ValidDays"] = campaign.validDaysStr
		}
	}

	// 构建发送列表
	items := make([]SendEmailItem, 0, len(orders))
	for _, order := range orders {
		user, ok := userMap[order.UserID]
		if !ok {
			skipped++
			continue
		}

		// 排除已过期用户（他们会收到 winback 邮件，避免重复）
		if user.ExpiredAt > 0 && user.ExpiredAt <= time.Now().Unix() {
			skipped++
			continue
		}

		email := getUserEmailFromIdentifies(user)
		if email == "" {
			skipped++
			continue
		}

		// 合并订单级变量和活动码变量
		itemVars := make(map[string]string, len(vars)+2)
		for k, v := range vars {
			itemVars[k] = v
		}
		itemVars["PlanTitle"] = order.Title
		itemVars["PayAmount"] = formatCents(order.PayAmount)

		items = append(items, SendEmailItem{
			Email:  email,
			UserID: user.ID,
			Slug:   slug,
			Vars:   itemVars,
		})
	}

	if len(items) == 0 {
		return 0, skipped, 0
	}

	result, err := SendTemplatedEmails(ctx, &SendEmailsRequest{
		BatchID: batchID,
		Items:   items,
	})
	if err != nil {
		log.Errorf(ctx, "[ABANDONED] SendTemplatedEmails failed: %v", err)
		return 0, skipped, len(items)
	}

	return result.Sent, skipped + result.Skipped, result.Failed
}
```

- [ ] **Step 4: Run tests — they should now pass**

Run: `cd api && go test -run TestAbandoned -v`
Expected: All `TestAbandoned*` tests PASS.

- [ ] **Step 5: Commit**

```bash
git add api/worker_abandoned_order.go api/worker_abandoned_order_test.go
git commit -m "feat(api): add abandoned order recovery email worker

Cron-based worker that sends recovery emails to users who created
orders but never paid. 6-stage cadence: 1h/1d/3d/7d/14d/30d with
escalating discounts (95%→90%→85%). Excludes users who subsequently
paid or are already covered by winback emails."
```

---

### Task 3: Register Cron Schedules

**Files:**
- Modify: `api/worker_integration.go`

- [ ] **Step 6: Add cron registration in `InitWorker()`**

Add after the existing renewal reminder cron (line 48), before the retailer followup cron:

```go
	// 注册未支付订单召回 Cron 任务
	// 每小时运行：1h 即时提醒
	// Cron 格式: 分 时 日 月 周
	asynq.Cron("0 * * * *", TaskTypeAbandonedOrderHourly, nil, hibikenAsynq.Unique(2*time.Hour))

	// 每天北京时间 11:00 执行（UTC 03:00）处理 1d/3d/7d/14d/30d
	// 比续费提醒晚 30 分钟，错开运行
	asynq.Cron("0 3 * * *", TaskTypeAbandonedOrderDaily, nil, hibikenAsynq.Unique(25*time.Hour))
```

- [ ] **Step 7: Add handler registration in `InitWorker()`**

Add after the existing `asynq.Handle(TaskTypeRenewalReminder, ...)` line:

```go
	asynq.Handle(TaskTypeAbandonedOrderHourly, handleAbandonedOrderHourlyTask)
	asynq.Handle(TaskTypeAbandonedOrderDaily, handleAbandonedOrderDailyTask)
```

- [ ] **Step 8: Update the log message at the end of `InitWorker()`**

Replace the existing log line:
```go
	log.Infof(context.Background(), "[WORKER] Task handlers registered (including renewal reminder cron at 10:30 Beijing time)")
```
with:
```go
	log.Infof(context.Background(), "[WORKER] Task handlers registered (renewal 10:30, abandoned hourly + 11:00 Beijing time)")
```

- [ ] **Step 9: Verify compilation**

Run: `cd api && go build ./...`
Expected: No errors.

- [ ] **Step 10: Run all tests**

Run: `cd api && go test ./... -count=1`
Expected: All tests PASS.

- [ ] **Step 11: Commit**

```bash
git add api/worker_integration.go
git commit -m "feat(api): register abandoned order recovery cron schedules

Hourly cron at :00 for 1h reminders, daily cron at 03:00 UTC
(11:00 Beijing) for 1d-30d recovery emails."
```

---

### Task 4: Create EDM Templates via Center API

This task creates the 6 email templates in the database. Templates are created via the Center admin API or MCP tools.

**Note:** These templates use `{{.PlanTitle}}` and `{{.PayAmount}}` as order-specific variables. Templates with campaigns additionally use `{{.SavingsText}}`, `{{.CampaignCode}}`, `{{.ValidDays}}`.

- [ ] **Step 12: Create template `abandoned-1h` (纯提醒)**

```
Name:    未支付提醒-1小时
Slug:    abandoned-1h
Subject: 你的开途订单等你完成支付
Content:
Hi，

你刚刚选择了「{{.PlanTitle}}」（{{.PayAmount}}），还差最后一步。

点击完成支付：
https://kaitu.io/purchase

支付遇到问题？直接回复这封邮件，我们帮你解决。

开途团队
```

- [ ] **Step 13: Create template `abandoned-1d` (痛点提醒)**

```
Name:    未支付提醒-1天
Slug:    abandoned-1d
Subject: 网络不等人，你的开途订单还在
Content:
Hi，

昨天你准备购买「{{.PlanTitle}}」，但还没完成支付。

不管是 AI 工具、流媒体还是日常浏览，稳定的连接让一切更顺畅。

继续完成购买：
https://kaitu.io/purchase

开途团队
```

- [ ] **Step 14: Create template `abandoned-3d` (首次折扣 95折)**

```
Name:    未支付召回-3天
Slug:    abandoned-3d
Subject: 限时优惠，完成你的开途订单
Content:
Hi，

你 3 天前选择了「{{.PlanTitle}}」，我们为你准备了一个专属优惠：

优惠码：{{.CampaignCode}}
{{.SavingsText}}，{{.ValidDays}}

前往购买：https://kaitu.io/purchase
结算时输入优惠码即可。

开途团队
```

- [ ] **Step 15: Create template `abandoned-7d` (场景提醒 + 90折)**

```
Name:    未支付召回-7天
Slug:    abandoned-7d
Subject: 还在犹豫？来看看开途能帮你什么
Content:
Hi，

一周前你选择了「{{.PlanTitle}}」，是支付遇到了问题，还是在考虑其他方案？

开途用户最常用的场景：
• AI 工具（ChatGPT、Claude、Cursor）稳定访问
• 流媒体（YouTube、Netflix）无缓冲播放
• 跨境办公和开发工具不受限

专属优惠码：{{.CampaignCode}}
{{.SavingsText}}，{{.ValidDays}}

继续购买：https://kaitu.io/purchase

有任何问题直接回复这封邮件。

开途团队
```

- [ ] **Step 16: Create template `abandoned-14d` (再次提醒 + 90折)**

```
Name:    未支付召回-14天
Slug:    abandoned-14d
Subject: 你的专属优惠还在，别错过
Content:
Hi，

两周前你曾准备购买开途。

我们最近的更新：全平台支持（macOS/Windows/iOS/Android），连接稳定性持续提升。

你的专属优惠码仍然有效：{{.CampaignCode}}
{{.SavingsText}}，{{.ValidDays}}

前往购买：https://kaitu.io/purchase

开途团队
```

- [ ] **Step 17: Create template `abandoned-30d` (最终优惠 85折)**

```
Name:    未支付召回-30天
Slug:    abandoned-30d
Subject: 最后机会：{{.SavingsText}}
Content:
Hi，

一个月前你选择了开途，但还没完成购买。

这是我们能给出的最大优惠了：

优惠码：{{.CampaignCode}}
{{.SavingsText}}，{{.ValidDays}}

前往购买：https://kaitu.io/purchase

过期后优惠码将失效。如果你已经有了其他方案，也欢迎回复告诉我们原因。

开途团队
```

- [ ] **Step 18: Verify all 6 templates exist and are active**

Use `list_edm_templates` to confirm all 6 slugs (`abandoned-1h`, `abandoned-1d`, `abandoned-3d`, `abandoned-7d`, `abandoned-14d`, `abandoned-30d`) appear with `isActive: true`.

---

### Task 5: Create Campaign Codes in Database

The discount codes referenced in templates must exist in the `campaigns` table for users to actually redeem them.

- [ ] **Step 19: Create campaign codes**

Create 4 campaign codes via Center admin API, all with `matcher_type: "first_order"`:
- `READY4U` — 95% (5% off), no expiry, unlimited uses
- `STAYFREE` — 90% (10% off), no expiry, unlimited uses
- `SMOOTHDAY` — 90% (10% off), no expiry, unlimited uses
- `KEEPGOING` — 85% (15% off), no expiry, unlimited uses

All campaigns must use `matcher_type: "first_order"` so only users who have completed a first order can redeem.

---

### Task 6: End-to-End Verification

- [ ] **Step 20: Verify compilation and tests**

Run: `cd api && go build ./... && go test ./... -count=1`
Expected: Build succeeds, all tests pass.

- [ ] **Step 21: Verify worker starts cleanly**

Check logs for:
```
[WORKER] Task handlers registered (renewal 10:30, abandoned hourly + 11:00 Beijing time)
```

- [ ] **Step 22: Commit any remaining changes**

```bash
git add -A
git commit -m "docs: add abandoned order recovery implementation plan"
```
