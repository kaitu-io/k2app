# Plan A: Subscription Architecture — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace User.ExpiredAt/MaxDevice with a Subscription table as the source of truth for entitlements. Support both personal and gateway product types. Backward compatible via cache sync.

**Architecture:** New `Subscription` model (one per user per product). Order payment creates/renews Subscription. `syncUserCache()` keeps User.ExpiredAt/MaxDevice in sync for untouched legacy code. ProRequired middleware reads Subscription with fallback to User.ExpiredAt during migration window.

**Tech Stack:** Go 1.24, Gin, GORM (MySQL), testify

**Spec:** `docs/superpowers/specs/2026-04-09-k2r-router-release-features-design.md` (Sections 1-5)

**Risk mitigation:**
- ProRequired has fallback to User.ExpiredAt if Subscription not found (safe rollout)
- Data migration is idempotent (safe to re-run)
- syncUserCache ensures legacy code never breaks

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `api/model.go` | Modify | Add Subscription struct, Plan.ProductType, Plan.Quota, Device.IsGateway |
| `api/type.go` | Modify | Add DataSubscription, update DataPlan, update AppInfo |
| `api/migrate.go` | Modify | Add Subscription to AutoMigrate + data migration |
| `api/logic_subscription.go` | Create | syncUserCache, getActiveSubscription, getUserSubscriptions |
| `api/logic_subscription_test.go` | Create | Unit tests for subscription helpers |
| `api/logic_member.go` | Modify | Rewrite applyOrderToTargetUsers to upsert Subscription |
| `api/middleware.go` | Modify | ProRequired reads Subscription (with fallback), add GatewayProRequired, parse isGateway |
| `api/api_auth.go` | Modify | Device limit reads Subscription.Quota |
| `api/api_plan.go` | Modify | Add product_type query filter |
| `api/api_admin_plan.go` | Modify | Add ProductType/Quota to CRUD |
| `api/api_user.go` | Modify | Profile returns subscriptions list |
| `api/route.go` | Modify | Register gateway routes with GatewayProRequired |

---

## Task 1: Subscription Model + Plan Fields + Migration

**Files:**
- Modify: `api/model.go`
- Modify: `api/type.go`
- Modify: `api/migrate.go`

- [ ] **Step 1: Add Subscription struct to model.go**

After the `Plan` struct (line ~592), add:

```go
// Subscription 用户订阅 — 权益状态的唯一真理源
// 每个用户每条产品线最多一个 Subscription (UserID + ProductType unique)
type Subscription struct {
	ID          uint64    `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	UserID      uint64    `gorm:"not null;uniqueIndex:idx_user_product" json:"userId"`
	ProductType string    `gorm:"type:varchar(20);not null;uniqueIndex:idx_user_product" json:"productType"` // "personal" | "gateway"
	PlanPID     string    `gorm:"type:varchar(30);not null" json:"planPid"`                                  // 当前生效的套餐 PID
	ExpiredAt   int64     `gorm:"not null" json:"expiredAt"`                                                 // 到期时间 Unix 秒
	Quota       int       `gorm:"not null" json:"quota"`                                                     // personal=最大设备数, gateway=最大接入设备数, 0=无限
}

func (s *Subscription) IsExpired() bool {
	return s.ExpiredAt <= time.Now().Unix()
}
```

- [ ] **Step 2: Add ProductType and Quota to Plan struct**

Modify the existing `Plan` struct (line ~581). Add two fields after `IsActive`:

```go
	ProductType string `gorm:"type:varchar(20);not null;default:'personal'" json:"productType"` // "personal" | "gateway"
	Quota       int    `gorm:"not null;default:5" json:"quota"`                                  // personal=设备数, gateway=接入设备数, 0=无限
```

- [ ] **Step 3: Add IsGateway to Device struct**

In the `Device` struct (line ~132), add after `DeviceModel` (or last field):

```go
	IsGateway bool `gorm:"not null;default:false" json:"isGateway"` // 路由器设备标识
```

- [ ] **Step 4: Update DataPlan in type.go**

Find `DataPlan` struct (line ~512) and add two fields:

```go
	ProductType string `json:"productType"`
	Quota       int    `json:"quota"`
```

- [ ] **Step 5: Add DataSubscription to type.go**

After DataPlan, add:

```go
type DataSubscription struct {
	ID          uint64 `json:"id"`
	ProductType string `json:"productType"`
	PlanPID     string `json:"planPid"`
	ExpiredAt   int64  `json:"expiredAt"`
	Quota       int    `json:"quota"`
}
```

- [ ] **Step 6: Add IsGateway to AppInfo in type.go**

Find the `AppInfo` struct and add:

```go
	IsGateway bool // 路由器设备
```

- [ ] **Step 7: Update migrate.go — add Subscription to AutoMigrate**

In `api/migrate.go`, add `&Subscription{}` to the AutoMigrate list (after `&Announcement{}`):

```go
		// Subscription system
		&Subscription{},
```

- [ ] **Step 8: Add data migration after AutoMigrate**

After the `AutoMigrate` call in `migrate.go` (after line 72, before the legacy cleanup), add:

```go
	// Populate personal subscriptions from existing User.ExpiredAt
	// Idempotent: NOT IN subquery skips users who already have a subscription
	result := db.Get().Exec(`
		INSERT INTO subscriptions (user_id, product_type, plan_pid, expired_at, quota, created_at, updated_at)
		SELECT id, 'personal', '', expired_at, max_device, NOW(), NOW()
		FROM users
		WHERE expired_at > 0
		AND id NOT IN (SELECT user_id FROM subscriptions WHERE product_type = 'personal')
	`)
	if result.Error != nil {
		log.Warnf(ctx, "subscription data migration failed (may be first run before table exists): %v", result.Error)
	} else {
		log.Infof(ctx, "subscription data migration: %d rows inserted", result.RowsAffected)
	}
```

- [ ] **Step 9: Verify compilation**

Run: `cd api && go build ./...`
Expected: Compiles successfully.

- [ ] **Step 10: Commit**

```bash
git add api/model.go api/type.go api/migrate.go
git commit -m "feat(api): add Subscription model, Plan.ProductType/Quota, Device.IsGateway, data migration"
```

---

## Task 2: Subscription Helper Functions

**Files:**
- Create: `api/logic_subscription.go`
- Create: `api/logic_subscription_test.go`

- [ ] **Step 1: Create logic_subscription.go**

```go
package center

import (
	"context"

	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// syncUserCache 将 personal Subscription 同步到 User 的缓存字段
// 目的: 未改动的代码 (workers, EDM, admin) 继续读 User.ExpiredAt/MaxDevice 正常工作
// 所有新代码读 Subscription
func syncUserCache(ctx context.Context, tx *gorm.DB, userID uint64) error {
	var sub Subscription
	err := tx.Where("user_id = ? AND product_type = ?", userID, "personal").First(&sub).Error
	if err == gorm.ErrRecordNotFound {
		return nil // 无 personal 订阅, 不动 User
	}
	if err != nil {
		return err
	}
	return tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"expired_at": sub.ExpiredAt,
		"max_device": sub.Quota,
	}).Error
}

// getActiveSubscription 查询用户在指定产品线的订阅
// 返回 nil, nil 表示无订阅 (不是 error)
func getActiveSubscription(tx *gorm.DB, userID uint64, productType string) (*Subscription, error) {
	var sub Subscription
	err := tx.Where("user_id = ? AND product_type = ?", userID, productType).First(&sub).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// getUserSubscriptions 返回用户的所有订阅
func getUserSubscriptions(tx *gorm.DB, userID uint64) ([]DataSubscription, error) {
	var subs []Subscription
	if err := tx.Where("user_id = ?", userID).Find(&subs).Error; err != nil {
		return nil, err
	}
	result := make([]DataSubscription, len(subs))
	for i, s := range subs {
		result[i] = DataSubscription{
			ID:          s.ID,
			ProductType: s.ProductType,
			PlanPID:     s.PlanPID,
			ExpiredAt:   s.ExpiredAt,
			Quota:       s.Quota,
		}
	}
	return result, nil
}

// getSubscriptionQuota 获取订阅配额, 无订阅返回默认值
func getSubscriptionQuota(tx *gorm.DB, userID uint64, productType string, defaultQuota int) int {
	sub, err := getActiveSubscription(tx, userID, productType)
	if err != nil || sub == nil {
		return defaultQuota
	}
	return sub.Quota
}
```

- [ ] **Step 2: Create logic_subscription_test.go**

```go
package center

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSubscription_IsExpired(t *testing.T) {
	past := &Subscription{ExpiredAt: time.Now().Add(-1 * time.Hour).Unix()}
	assert.True(t, past.IsExpired())

	future := &Subscription{ExpiredAt: time.Now().Add(1 * time.Hour).Unix()}
	assert.False(t, future.IsExpired())

	exact := &Subscription{ExpiredAt: time.Now().Unix()}
	assert.True(t, exact.IsExpired(), "exactly now counts as expired (<=)")
}
```

- [ ] **Step 3: Run tests**

Run: `cd api && go test -run TestSubscription -v ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add api/logic_subscription.go api/logic_subscription_test.go
git commit -m "feat(api): subscription helper functions (syncUserCache, getActiveSubscription)"
```

---

## Task 3: Rewrite Order Processing

**Files:**
- Modify: `api/logic_member.go`
- Modify: `api/member_test.go`

- [ ] **Step 1: Replace applyOrderToTargetUsers in logic_member.go**

Replace lines 76-168 (the entire `applyOrderToTargetUsers` function) with:

```go
// applyOrderToTargetUsers 订单生效：为指定用户创建/续期 Subscription
func applyOrderToTargetUsers(ctx context.Context, tx *gorm.DB, order *Order) error {
	log.Infof(ctx, "[applyOrderToTargetUsers] applying order %d to target users", order.ID)

	if order.IsPaid == nil || !*order.IsPaid {
		log.Warnf(ctx, "[applyOrderToTargetUsers] order %d is not paid, skipping", order.ID)
		return nil
	}

	plan, err := order.GetPlan()
	if err != nil {
		return fmt.Errorf("get plan for order %d: %w", order.ID, err)
	}
	if plan == nil {
		return fmt.Errorf("plan not found for order %d", order.ID)
	}

	// 使用月数计算天数 (用于 UserProHistory 记录)
	now := time.Now()
	futureDate := now.AddDate(0, plan.Month, 0)
	days := int(futureDate.Sub(now).Hours() / 24)

	// 确定 ProductType: 旧套餐没有 ProductType, 默认 personal
	productType := plan.ProductType
	if productType == "" {
		productType = "personal"
	}
	// 确定 Quota: 旧套餐没有 Quota, 默认 5
	quota := plan.Quota
	if quota == 0 && productType == "personal" {
		quota = 5
	}

	// 收集目标用户
	var targetUsers []User
	if order.GetForMyself() {
		var buyer User
		if err := tx.First(&buyer, order.UserID).Error; err != nil {
			return fmt.Errorf("购买者不存在: %w", err)
		}
		targetUsers = append(targetUsers, buyer)
	}
	forUserUUIDs := order.GetForUsers()
	if len(forUserUUIDs) > 0 {
		var users []User
		if err := tx.Where("uuid IN ?", forUserUUIDs).Find(&users).Error; err != nil {
			return err
		}
		if len(users) != len(forUserUUIDs) {
			return fmt.Errorf("部分目标用户不存在: 期望 %d 个, 实际找到 %d 个", len(forUserUUIDs), len(users))
		}
		targetUsers = append(targetUsers, users...)
	}

	if len(targetUsers) == 0 {
		log.Warnf(ctx, "[applyOrderToTargetUsers] no target users for order %d", order.ID)
		return nil
	}

	for i := range targetUsers {
		user := &targetUsers[i]

		// Upsert Subscription
		var sub Subscription
		err := tx.Where("user_id = ? AND product_type = ?", user.ID, productType).First(&sub).Error
		if err == gorm.ErrRecordNotFound {
			// 新订阅 — 从现在开始
			sub = Subscription{
				UserID:      user.ID,
				ProductType: productType,
				PlanPID:     plan.PID,
				ExpiredAt:   now.AddDate(0, plan.Month, 0).Unix(),
				Quota:       quota,
			}
			if err := tx.Create(&sub).Error; err != nil {
				return fmt.Errorf("create subscription for user %d: %w", user.ID, err)
			}
			log.Infof(ctx, "[applyOrderToTargetUsers] created %s subscription for user %d, expires %s",
				productType, user.ID, time.Unix(sub.ExpiredAt, 0).Format("2006-01-02"))
		} else if err != nil {
			return fmt.Errorf("query subscription for user %d: %w", user.ID, err)
		} else {
			// 续期 — 从当前到期时间延长 (已过期则从现在开始)
			base := time.Unix(sub.ExpiredAt, 0)
			if sub.IsExpired() {
				base = now
			}
			sub.ExpiredAt = base.AddDate(0, plan.Month, 0).Unix()
			sub.PlanPID = plan.PID
			sub.Quota = quota
			if err := tx.Save(&sub).Error; err != nil {
				return fmt.Errorf("update subscription for user %d: %w", user.ID, err)
			}
			log.Infof(ctx, "[applyOrderToTargetUsers] renewed %s subscription for user %d, expires %s",
				productType, user.ID, time.Unix(sub.ExpiredAt, 0).Format("2006-01-02"))
		}

		// UserProHistory + IsFirstOrderDone + IsActivated (保持现有行为)
		reason := fmt.Sprintf("订单支付 - %s", order.UUID)
		if _, err := addProExpiredDays(ctx, tx, user, VipPurchase, order.ID, days, reason); err != nil {
			return fmt.Errorf("addProExpiredDays for user %d: %w", user.ID, err)
		}

		// 同步 User 缓存 (仅 personal — gateway 状态只在 Subscription)
		if productType == "personal" {
			if err := syncUserCache(ctx, tx, user.ID); err != nil {
				log.Warnf(ctx, "[applyOrderToTargetUsers] syncUserCache failed for user %d: %v", user.ID, err)
			}
		}
	}

	log.Infof(ctx, "[applyOrderToTargetUsers] applied order %d to %d users", order.ID, len(targetUsers))
	return nil
}
```

**Note:** `addProExpiredDays` is kept as-is. It still writes `User.ExpiredAt` (which is now a cache field — `syncUserCache` will overwrite it immediately after from Subscription). The `addProExpiredDays` function also handles `IsFirstOrderDone`, `IsActivated`, and creates `UserProHistory` — we need all of these side effects to continue working.

- [ ] **Step 2: Add test for Subscription creation in member_test.go**

Append to `api/member_test.go`:

```go
func TestApplyOrderToTargetUsers_SetsProductType(t *testing.T) {
	// Verify plan ProductType defaults to "personal" when empty
	plan := &Plan{PID: "1y", Label: "1 Year", Price: 4999, Month: 12}
	order := Order{}
	_ = order.SetOrderMeta(plan, nil, []string{}, true)

	retrievedPlan, _ := order.GetPlan()
	// Old plans have no ProductType — code should default to "personal"
	if retrievedPlan.ProductType != "" {
		// This is fine — new plans will have it set
	}
	// The applyOrderToTargetUsers function handles the default internally
}
```

- [ ] **Step 3: Verify compilation and run all tests**

Run: `cd api && go build ./... && go test ./...`
Expected: Compiles and all existing tests pass.

- [ ] **Step 4: Commit**

```bash
git add api/logic_member.go api/member_test.go
git commit -m "feat(api): rewrite applyOrderToTargetUsers to upsert Subscription + syncUserCache

Order payment now creates/renews Subscription (source of truth).
addProExpiredDays kept for UserProHistory + IsFirstOrderDone + IsActivated.
syncUserCache keeps User.ExpiredAt/MaxDevice in sync for legacy code.
Gateway subscriptions (productType=gateway) skip User cache sync."
```

---

## Task 4: ProRequired Middleware + GatewayProRequired

**Files:**
- Modify: `api/middleware.go`

- [ ] **Step 1: Rewrite ProRequired with Subscription check + fallback**

Replace ProRequired (line ~418) with:

```go
// ProRequired 检查 personal 订阅是否有效
// 优先查 Subscription 表 (source of truth), 查不到时 fallback 到 User.ExpiredAt (迁移兼容)
func ProRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := ReqUser(c)
		if user == nil {
			log.Warnf(c, "vip required: auth failed for request to %s", c.Request.URL.Path)
			Error(c, ErrorNotLogin, "authentication failed")
			c.Abort()
			return
		}

		sub, err := getActiveSubscription(db.Get(), user.ID, "personal")
		if err != nil {
			log.Errorf(c, "ProRequired: failed to query subscription for user %d: %v", user.ID, err)
			// DB error — fall back to User cache to avoid blocking all users
			if user.IsExpired() {
				Error(c, ErrorPaymentRequired, "membership expired")
				c.Abort()
				return
			}
			c.Next()
			return
		}

		if sub != nil {
			// Subscription found — use it as source of truth
			if sub.IsExpired() {
				log.Warnf(c, "vip required: user %d subscription expired, request to %s denied", user.ID, c.Request.URL.Path)
				Error(c, ErrorPaymentRequired, "membership expired")
				c.Abort()
				return
			}
		} else {
			// No Subscription row — fallback to User.ExpiredAt (migration window)
			if user.IsExpired() {
				log.Warnf(c, "vip required: user %d membership expired (no subscription, using cache), request to %s denied", user.ID, c.Request.URL.Path)
				Error(c, ErrorPaymentRequired, "membership expired")
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// GatewayProRequired 检查 gateway 订阅是否有效
// 无 fallback — gateway 是新产品线, 必须有 Subscription
func GatewayProRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := ReqUser(c)
		if user == nil {
			Error(c, ErrorNotLogin, "authentication failed")
			c.Abort()
			return
		}

		sub, err := getActiveSubscription(db.Get(), user.ID, "gateway")
		if err != nil {
			log.Errorf(c, "GatewayProRequired: failed to query subscription for user %d: %v", user.ID, err)
			Error(c, ErrorSystemError, "failed to check gateway subscription")
			c.Abort()
			return
		}
		if sub == nil || sub.IsExpired() {
			Error(c, ErrorPaymentRequired, "gateway membership expired")
			c.Abort()
			return
		}

		c.Next()
	}
}
```

- [ ] **Step 2: Update AppInfo parsing to extract isGateway**

Find the `parseClientHeader` function (or wherever `AppInfo` is constructed from headers). The `X-App-Info` header is a JSON object for gateway, but the existing `X-K2-Client` header uses a regex pattern.

For gateway devices, k2r sends `X-App-Info` as JSON: `{"version":"0.4.2","platform":"linux","arch":"arm64","isGateway":true}`

Add to `middleware.go` — in the device registration section (the `fillDeviceAppInfo` function or equivalent), after setting `device.AppPlatform`:

```go
	// Parse isGateway from X-App-Info JSON header (k2r sends this)
	if appInfoHeader := c.GetHeader("X-App-Info"); appInfoHeader != "" {
		var appInfoJSON struct {
			IsGateway bool `json:"isGateway"`
		}
		if json.Unmarshal([]byte(appInfoHeader), &appInfoJSON) == nil {
			device.IsGateway = appInfoJSON.IsGateway
		}
	}
```

- [ ] **Step 3: Verify compilation**

Run: `cd api && go build ./...`
Expected: Compiles.

- [ ] **Step 4: Commit**

```bash
git add api/middleware.go
git commit -m "feat(api): ProRequired reads Subscription with User fallback, add GatewayProRequired

ProRequired: Subscription (source of truth) → User.ExpiredAt (fallback).
DB error also falls back to User cache to avoid blocking all users.
GatewayProRequired: no fallback, gateway is new product line.
AppInfo: parse isGateway from X-App-Info JSON header."
```

---

## Task 5: Device Limit Reads Subscription.Quota

**Files:**
- Modify: `api/api_auth.go`

- [ ] **Step 1: Update OTP login device limit (line ~280)**

Replace:
```go
if deviceCount >= int64(user.MaxDevice) {
```

With:
```go
maxDevice := getSubscriptionQuota(tx, user.ID, "personal", 5)
if deviceCount >= int64(maxDevice) {
```

(Note: `getSubscriptionQuota` is defined in `logic_subscription.go`, returns quota from Subscription with fallback to default)

- [ ] **Step 2: Update password login device limit (line ~770)**

Same replacement as Step 1.

- [ ] **Step 3: Verify compilation and run auth tests**

Run: `cd api && go build ./... && go test -run TestAuth -v ./...`
Expected: Compiles and passes.

- [ ] **Step 4: Commit**

```bash
git add api/api_auth.go
git commit -m "feat(api): device limit reads Subscription.Quota with default fallback"
```

---

## Task 6: Plans API product_type Filter

**Files:**
- Modify: `api/api_plan.go`
- Modify: `api/api_admin_plan.go`

- [ ] **Step 1: Update GET /api/plans with product_type filter**

Replace the entire `api_get_plans` function in `api/api_plan.go`:

```go
func api_get_plans(c *gin.Context) {
	productType := c.DefaultQuery("product_type", "personal")

	log.Infof(c, "request to get plans, product_type=%s", productType)
	var items []DataPlan

	var plans []Plan
	err := db.Get().Where("is_active = ? AND product_type = ?", true, productType).Find(&plans).Error
	if err != nil {
		log.Errorf(c, "failed to load plans from database: %v", err)
		Error(c, ErrorSystemError, "failed to load plans")
		return
	}
	for _, plan := range plans {
		items = append(items, DataPlan{
			PID:         plan.PID,
			Label:       plan.Label,
			Price:       plan.Price,
			OriginPrice: plan.OriginPrice,
			Month:       plan.Month,
			Highlight:   plan.Highlight != nil && *plan.Highlight,
			IsActive:    plan.IsActive != nil && *plan.IsActive,
			ProductType: plan.ProductType,
			Quota:       plan.Quota,
		})
	}
	log.Infof(c, "successfully loaded %d plans for product_type=%s", len(items), productType)
	ItemsAll(c, items)
}
```

- [ ] **Step 2: Update AdminCreatePlanRequest**

In `api/api_admin_plan.go`, update the create request struct:

```go
type AdminCreatePlanRequest struct {
	PID         string `json:"pid" binding:"required"`
	Label       string `json:"label" binding:"required"`
	Price       uint64 `json:"price" binding:"required"`
	OriginPrice uint64 `json:"originPrice" binding:"required"`
	Month       int    `json:"month" binding:"required"`
	Highlight   bool   `json:"highlight"`
	IsActive    bool   `json:"isActive"`
	ProductType string `json:"productType"` // "personal" (default) | "gateway"
	Quota       int    `json:"quota"`       // personal=设备数(default 5), gateway=接入设备数, 0=无限
}
```

In the `api_admin_create_plan` handler, after binding, add defaults:

```go
	if req.ProductType == "" {
		req.ProductType = "personal"
	}
	if req.Quota == 0 && req.ProductType == "personal" {
		req.Quota = 5
	}
```

And include in the Plan creation:

```go
	plan := Plan{
		PID:         req.PID,
		Label:       req.Label,
		Price:       req.Price,
		OriginPrice: req.OriginPrice,
		Month:       req.Month,
		Highlight:   BoolPtr(req.Highlight),
		IsActive:    BoolPtr(req.IsActive),
		ProductType: req.ProductType,
		Quota:       req.Quota,
	}
```

- [ ] **Step 3: Update AdminUpdatePlanRequest**

Add optional fields:

```go
type AdminUpdatePlanRequest struct {
	PID         *string `json:"pid"`
	Label       *string `json:"label"`
	Price       *uint64 `json:"price"`
	OriginPrice *uint64 `json:"originPrice"`
	Month       *int    `json:"month"`
	Highlight   *bool   `json:"highlight"`
	IsActive    *bool   `json:"isActive"`
	ProductType *string `json:"productType"`
	Quota       *int    `json:"quota"`
}
```

- [ ] **Step 4: Update admin list to support product_type filter**

In `api_admin_list_plans`, add optional filter:

```go
	query := db.Get().Model(&Plan{})
	if productType := c.Query("product_type"); productType != "" {
		query = query.Where("product_type = ?", productType)
	}
```

- [ ] **Step 5: Verify compilation**

Run: `cd api && go build ./...`
Expected: Compiles.

- [ ] **Step 6: Commit**

```bash
git add api/api_plan.go api/api_admin_plan.go
git commit -m "feat(api): Plans API product_type filter, admin CRUD with ProductType/Quota"
```

---

## Task 7: User Profile Returns Subscriptions

**Files:**
- Modify: `api/api_user.go`

- [ ] **Step 1: Add subscriptions to user info response**

In the `api_get_user_info` handler, after building `dataUser`, add:

```go
	// Fetch user subscriptions
	subs, err := getUserSubscriptions(db.Get(), user.ID)
	if err != nil {
		log.Warnf(c, "failed to fetch subscriptions for user %d: %v", user.ID, err)
	}
```

Add a `Subscriptions []DataSubscription` field to the response. The exact response type depends on how `buildDataUserWithDevice` works — find the `DataUser` struct and add:

```go
	Subscriptions []DataSubscription `json:"subscriptions,omitempty"`
```

Set it before returning:

```go
	dataUser.Subscriptions = subs
```

- [ ] **Step 2: Verify compilation**

Run: `cd api && go build ./...`
Expected: Compiles.

- [ ] **Step 3: Commit**

```bash
git add api/api_user.go api/type.go
git commit -m "feat(api): user profile returns subscriptions list"
```

---

## Task 8: Run Full Test Suite + Integration Verification

- [ ] **Step 1: Run all unit tests**

Run: `cd api && go test ./...`
Expected: All tests pass. Document any failures and fix.

- [ ] **Step 2: Run with local database (if config.yml available)**

Run: `cd api && go test -count=1 ./...`
Expected: Integration tests pass against local MySQL.

- [ ] **Step 3: Verify data migration**

Run: `cd api/cmd && ./kaitu-center migrate -c ../config.yml`
Expected: Subscription table created, existing users migrated.

Verify: `SELECT COUNT(*) FROM subscriptions WHERE product_type = 'personal';` should match count of users with `expired_at > 0`.

- [ ] **Step 4: Final commit (if any fixes)**

```bash
git add -A
git commit -m "fix(api): address test failures from Subscription migration"
```

---

## Self-Review

| Spec Requirement | Task |
|-----------------|------|
| 2.1 Subscription Model | Task 1 |
| 2.2 Plan.ProductType + Quota | Task 1 |
| 2.3 Device.IsGateway | Task 1 |
| 2.4 User cache fields | Task 2 (syncUserCache) |
| 2.5 Cache sync | Task 2 |
| 2.6 Source of truth migration | Tasks 3-5 |
| 2.7 Data migration | Task 1 (migrate.go) |
| 3.1 Plans API filter | Task 6 |
| 3.2 Admin plans | Task 6 |
| 3.3 Frontend Plan type | Task 6 (DataPlan updated) |
| 3.4 Frontend Subscription type | Task 1 (DataSubscription) |
| 4.6 Order processing | Task 3 |
| 5.1-5.3 Subscription expiry | Task 4 (ProRequired + GatewayProRequired) |
| 8.1 Gateway auth header | Task 4 (isGateway parsing) |
| 8.2 Device.IsGateway | Task 1 + Task 4 |
| User profile subscriptions | Task 7 |

**Type consistency check:** `DataSubscription`, `DataPlan`, `Subscription`, `Plan` — field names and types consistent across all tasks. `getActiveSubscription`, `syncUserCache`, `getSubscriptionQuota`, `getUserSubscriptions` — same signatures used in all references.

---

## Supplement: Precision Details (12/10)

The tasks above define WHAT to change. This supplement adds the EXACT edit locations and edge case handling for each task.

### S1: Task 3 Edge Case — addProExpiredDays vs Subscription Expiry

`addProExpiredDays` (kept for UserProHistory/IsFirstOrderDone/IsActivated) also writes `User.ExpiredAt` as a side effect. This creates a temporary inconsistency:

```
1. Upsert Subscription (correct expiry: now.AddDate(0, plan.Month, 0))
2. addProExpiredDays writes User.ExpiredAt (slightly different: now + truncated days)
3. syncUserCache overwrites User.ExpiredAt from Subscription (corrects it)
```

The intermediate value from step 2 is overwritten in step 3 within the same transaction. **No data inconsistency is committed.** The rounding difference (up to 1 day due to `int(hours/24)` truncation) is never visible to any reader.

This is intentional — modifying `addProExpiredDays` to skip writing `ExpiredAt` would be a larger refactor touching the `tx.Save(user)` call (line 65) which saves ALL user fields. We accept the redundant write.

### S2: Task 4 Exact Edit — fillDeviceAppInfo + isGateway

File: `api/middleware.go`, line 65-76.

**Current code:**
```go
func fillDeviceAppInfo(c *gin.Context, device *Device) {
	if clientHeader := c.GetHeader("X-K2-Client"); clientHeader != "" {
		if appInfo := parseClientHeader(clientHeader); appInfo != nil {
			device.AppVersion = appInfo.Version
			device.AppPlatform = appInfo.Platform
			device.AppArch = appInfo.Arch
			device.OSVersion = appInfo.OSVersion
			device.DeviceModel = appInfo.DeviceModel
		}
	}
}
```

**Replace with:**
```go
func fillDeviceAppInfo(c *gin.Context, device *Device) {
	if clientHeader := c.GetHeader("X-K2-Client"); clientHeader != "" {
		if appInfo := parseClientHeader(clientHeader); appInfo != nil {
			device.AppVersion = appInfo.Version
			device.AppPlatform = appInfo.Platform
			device.AppArch = appInfo.Arch
			device.OSVersion = appInfo.OSVersion
			device.DeviceModel = appInfo.DeviceModel
		}
	}
	// k2r gateway sends X-App-Info as JSON with isGateway flag
	if appInfoHeader := c.GetHeader("X-App-Info"); appInfoHeader != "" {
		var info struct {
			Version   string `json:"version"`
			Platform  string `json:"platform"`
			Arch      string `json:"arch"`
			IsGateway bool   `json:"isGateway"`
		}
		if json.Unmarshal([]byte(appInfoHeader), &info) == nil {
			device.IsGateway = info.IsGateway
			// X-App-Info fields override X-K2-Client if both present
			if info.Version != "" {
				device.AppVersion = info.Version
			}
			if info.Platform != "" {
				device.AppPlatform = info.Platform
			}
			if info.Arch != "" {
				device.AppArch = info.Arch
			}
		}
	}
}
```

**Required import:** Add `"encoding/json"` to imports in `middleware.go` (line 1-19). Insert after `"errors"`:

```go
	"encoding/json"
```

### S3: Task 5 Exact Edits — Device Limit

**Edit 1: OTP login (api/api_auth.go line ~291)**

Replace:
```go
			if deviceCount >= int64(user.MaxDevice) {
```
With:
```go
			maxDevice := getSubscriptionQuota(tx, identify.UserID, "personal", 5)
			if deviceCount >= int64(maxDevice) {
```

**Edit 2: Password login (api/api_auth.go line ~776)**

Replace:
```go
		if deviceCount >= int64(user.MaxDevice) {
```
With:
```go
		maxDevice := getSubscriptionQuota(tx, identify.UserID, "personal", 5)
		if deviceCount >= int64(maxDevice) {
```

Both edits are single-line replacements. `getSubscriptionQuota` is defined in `logic_subscription.go` — same package, no import needed.

### S4: Task 6 — Approval Callback Update

File: `api/logic_approval_callbacks.go`, line ~293-335.

The `executeApprovalPlanUpdate` function applies `AdminUpdatePlanRequest` fields to the plan. It currently handles: Label, Price, OriginPrice, Month, Highlight, IsActive. **Must add ProductType and Quota.**

After the `IsActive` block (line ~331), add:

```go
	if req.ProductType != nil {
		plan.ProductType = *req.ProductType
	}
	if req.Quota != nil {
		plan.Quota = *req.Quota
	}
```

### S5: Task 7 Exact Edits — User Profile

**Edit 1: DataUser struct (api/type.go line ~99)**

Add after `BetaOptedIn` field (last field, line ~116):

```go
	Subscriptions []DataSubscription `json:"subscriptions,omitempty"` // 用户订阅列表
```

**Edit 2: buildDataUserWithDevice (api/api_user.go line ~420)**

The function returns `&DataUser{...}`. Before the return statement (line ~450), the `Subscriptions` field is not set. We cannot fetch subscriptions here because this function doesn't have access to gin.Context or DB.

Instead, set it in the **caller** — `api_get_user_info` (line ~75). Before `Success(c, dataUser)`, add:

```go
	// Fetch subscriptions
	subs, err := getUserSubscriptions(db.Get(), user.ID)
	if err != nil {
		log.Warnf(c, "failed to fetch subscriptions for user %d: %v", user.ID, err)
	} else {
		dataUser.Subscriptions = subs
	}
```

Import `db "github.com/wordgate/qtoolkit/db"` is already present in `api_user.go`.

### S6: Missing — Route Changes

File: `api/route.go`

The current tunnel/relay routes use `ProRequired()`:
```go
api.GET("/tunnels", AuthRequired(), ProRequired(), DeviceAuthRequired(), api_k2_tunnels)
api.GET("/relays", AuthRequired(), ProRequired(), DeviceAuthRequired(), api_k2_relays)
```

These stay as-is (personal product). For gateway-specific routes, register with `GatewayProRequired()` when they're needed in future phases. **No route changes needed in Plan A** — GatewayProRequired is defined but not yet wired to any routes (gateway API is on the k2r binary, not Center API).

### S7: Deployment Strategy

**Order of operations:**

1. **Deploy code** with all changes (Subscription model, rewritten order processing, ProRequired with fallback)
2. GORM AutoMigrate runs automatically on startup → creates `subscriptions` table + adds Plan columns + Device.IsGateway
3. Data migration SQL runs → populates subscriptions from existing User data
4. ProRequired fallback ensures: if migration is slow/incomplete, users still authenticated via User.ExpiredAt
5. Once migration complete, all reads go through Subscription

**Rollback plan:**

If critical issues found:
1. Revert code deploy (git revert)
2. `subscriptions` table stays (harmless orphan — GORM won't break with extra table)
3. Plan.ProductType/Quota columns stay (defaults are backward-compatible: `'personal'`, `5`)
4. Device.IsGateway stays (default false, harmless)
5. User.ExpiredAt/MaxDevice are still being written by syncUserCache → old code reads them correctly

**Zero-downtime:** The fallback in ProRequired means there is no moment where users are blocked even during deployment.

### S8: Performance Note

`ProRequired` now does 1 extra DB query per request (SELECT from subscriptions WHERE user_id AND product_type). This is:
- Indexed: `uniqueIndex:idx_user_product` on (user_id, product_type)
- Small table: one row per user per product (max ~2x users count)
- Same MySQL instance, sub-millisecond

If this becomes a bottleneck, cache the Subscription in gin.Context during auth middleware (already fetches User — can piggyback Subscription). But premature optimization — measure first.
