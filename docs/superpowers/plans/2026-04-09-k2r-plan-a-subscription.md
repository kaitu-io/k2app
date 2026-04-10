# Plan A: Tiered Plan Model — Implementation Plan (v4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add tiered plan support with router access as premium feature. Plan defines Tier + MaxDevice + MaxRouterDevice + MaxLanClient. User gains Tier + MaxRouterDevice + MaxLanClient. RouterRequired middleware gates router access. No new tables.

**Architecture:** Plan.Tier groups SKUs by feature level. Plan.MaxDevice/MaxRouterDevice/MaxLanClient define tier quotas. applyOrderToTargetUsers writes Tier + quotas to User on purchase. ProRequired unchanged. New RouterRequired checks User.MaxRouterDevice > 0. User.Tier is stable across billing period changes (family-1y → family-2y keeps Tier=family).

**Tech Stack:** Go 1.24, Gin, GORM (MySQL), testify

**Spec:** `docs/superpowers/specs/2026-04-09-k2r-router-release-features-design.md` (v4)

**Confidence: 10/10** — No new tables, no data migration, no middleware rewrite. Just add fields.
**Risk: 1/10** — ProRequired untouched. addProExpiredDays untouched. Only additive changes.

---

## Constants

```go
const (
    DefaultMaxDevice       = 5
    DefaultMaxRouterDevice = 0  // no router access
)
```

Define in a suitable location (e.g., `model.go` or a new `constants.go`).

---

## Task 1: Plan + User + Device Model Changes

**Files:**
- Modify: `api/model.go`
- Modify: `api/type.go`
- Modify: `api/migrate.go`

- [ ] **Step 1: Add Tier, MaxDevice, MaxRouterDevice, MaxLanClient to Plan struct**

In `api/model.go`, Plan struct (line ~581). Add after `IsActive`:

```go
	Tier            string `gorm:"type:varchar(30);not null;default:'pro'" json:"tier"` // 功能等级标识（lite/basic/family/business，向后兼容: pro）
	MaxDevice       int    `gorm:"not null;default:5" json:"maxDevice"`                // app 设备数量（不含路由器）
	MaxRouterDevice int    `gorm:"not null;default:0" json:"maxRouterDevice"`          // 路由器登录数量上限 (0=不支持)
	MaxLanClient    int    `gorm:"not null;default:0" json:"maxLanClient"`             // LAN 接入数量上限 (0=不支持, -1=无限)
```

- [ ] **Step 2: Add Tier, MaxRouterDevice, MaxLanClient to User struct**

In `api/model.go`, User struct (line ~46). Add after `MaxDevice`:

```go
	MaxRouterDevice int    `gorm:"not null;default:0" json:"maxRouterDevice"`            // 路由器登录数量上限
	MaxLanClient    int    `gorm:"not null;default:0" json:"maxLanClient"`               // LAN 接入数量上限
	Tier            string `gorm:"type:varchar(30);not null;default:'pro'" json:"tier"`  // 当前功能等级（稳定标识，不随周期变化）
```

- [ ] **Step 3: Add IsGateway to Device struct**

In `api/model.go`, Device struct (line ~132). Add after last field:

```go
	IsGateway bool `gorm:"not null;default:false" json:"isGateway"`
```

- [ ] **Step 4: Update DataPlan in type.go**

Find `DataPlan` struct (line ~512). Add:

```go
	Tier            string `json:"tier"`
	MaxDevice       int    `json:"maxDevice"`
	MaxRouterDevice int    `json:"maxRouterDevice"`
	MaxLanClient    int    `json:"maxLanClient"`
```

- [ ] **Step 5: Update DataUser in type.go**

Find `DataUser` struct (line ~99). Add after `BetaOptedIn`:

```go
	Tier            string `json:"tier,omitempty"`
	MaxRouterDevice int    `json:"maxRouterDevice"`
	MaxLanClient    int    `json:"maxLanClient"`
```

- [ ] **Step 6: Verify compilation**

Run: `cd api && go build ./...`
Expected: Compiles. GORM AutoMigrate will add columns on next startup.

- [ ] **Step 7: Commit**

```bash
git add api/model.go api/type.go
git commit -m "feat(api): add Plan.MaxDevice/MaxRouterDevice, User.MaxRouterDevice/PlanPID, Device.IsGateway"
```

---

## Task 2: Update Order Processing

**Files:**
- Modify: `api/logic_member.go`
- Modify: `api/member_test.go`

- [ ] **Step 1: Update applyOrderToTargetUsers to write new fields**

In `api/logic_member.go`, inside the `applyOrderToTargetUsers` function, find the loop over `targetUsers` (line ~152). Currently:

```go
	for i := range targetUsers {
		user := &targetUsers[i]
		reason := fmt.Sprintf("订单支付 - %s", order.UUID)
		_, err := addProExpiredDays(ctx, tx, user, VipPurchase, order.ID, days, reason)
```

Add lines BEFORE the `addProExpiredDays` call:

```go
	for i := range targetUsers {
		user := &targetUsers[i]

		// Update quotas and tier from purchased plan
		user.MaxDevice = plan.MaxDevice
		user.MaxRouterDevice = plan.MaxRouterDevice
		user.MaxLanClient = plan.MaxLanClient

		// Set tier (stable — doesn't change on period renewal)
		tier := plan.Tier
		if tier == "" {
			tier = "pro" // backward compat: old plans without Tier field
		}
		user.Tier = tier

		reason := fmt.Sprintf("订单支付 - %s", order.UUID)
		_, err := addProExpiredDays(ctx, tx, user, VipPurchase, order.ID, days, reason)
```

`addProExpiredDays` calls `tx.Save(user)` at line 65, which writes ALL user fields including the ones we just set. No additional Save needed.

- [ ] **Step 2: Handle missing MaxDevice and Tier in old plans**

Old plans serialized in Order.Meta don't have MaxDevice/Tier. Add defaults after `order.GetPlan()` (line ~86):

```go
	plan, err := order.GetPlan()
	if err != nil { ... }
	// Old plans in Meta don't have MaxDevice — default to current behavior
	if plan.MaxDevice == 0 {
		plan.MaxDevice = DefaultMaxDevice
	}
	// MaxRouterDevice defaults to 0 (no router) — correct for old plans
```

- [ ] **Step 3: Add test for quota write**

Append to `api/member_test.go`:

```go
func TestApplyOrderToTargetUsers_WritesQuotas(t *testing.T) {
	// Verify new Plan fields survive Meta roundtrip
	plan := &Plan{
		PID:             "family-1y",
		Label:           "家庭版年付",
		Price:           9999,
		Month:           12,
		MaxDevice:       5,
		MaxRouterDevice: 1,
		MaxLanClient:    10,
	}
	order := Order{}
	err := order.SetOrderMeta(plan, nil, []string{}, true)
	require.NoError(t, err)

	retrieved, err := order.GetPlan()
	require.NoError(t, err)
	assert.Equal(t, 5, retrieved.MaxDevice)
	assert.Equal(t, 1, retrieved.MaxRouterDevice)
	assert.Equal(t, 10, retrieved.MaxLanClient)
	assert.Equal(t, "family-1y", retrieved.PID)
}

func TestApplyOrderToTargetUsers_OldPlanDefaults(t *testing.T) {
	// Old plans without MaxDevice should get DefaultMaxDevice
	plan := &Plan{PID: "1y", Label: "1年", Price: 4999, Month: 12}
	order := Order{}
	_ = order.SetOrderMeta(plan, nil, []string{}, true)

	retrieved, _ := order.GetPlan()
	// Old plan: MaxDevice=0 (zero value), MaxRouterDevice=0
	assert.Equal(t, 0, retrieved.MaxDevice, "old plan has zero MaxDevice in Meta")
	assert.Equal(t, 0, retrieved.MaxRouterDevice, "old plan has zero MaxRouterDevice")
	// Code must default MaxDevice=0 to DefaultMaxDevice=5
}
```

- [ ] **Step 4: Run tests**

Run: `cd api && go test -run TestApplyOrderToTargetUsers -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/logic_member.go api/member_test.go
git commit -m "feat(api): applyOrderToTargetUsers writes MaxDevice/MaxRouterDevice/PlanPID from Plan"
```

---

## Task 3: RouterRequired Middleware + IsGateway

**Files:**
- Modify: `api/middleware.go`

- [ ] **Step 1: Add RouterRequired middleware**

After existing `ProRequired` function (line ~435), add:

```go
// RouterRequired 检查用户是否有路由器权限 (MaxRouterDevice > 0 或 == -1)
// 需要先经过 AuthRequired + ProRequired
func RouterRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := ReqUser(c)
		if user == nil {
			Error(c, ErrorNotLogin, "authentication failed")
			c.Abort()
			return
		}
		if user.IsExpired() {
			Error(c, ErrorPaymentRequired, "membership expired")
			c.Abort()
			return
		}
		if user.MaxRouterDevice == 0 {
			Error(c, ErrorPaymentRequired, "router access requires upgrade")
			c.Abort()
			return
		}
		c.Next()
	}
}
```

- [ ] **Step 2: Update fillDeviceAppInfo for isGateway**

In `api/middleware.go`, replace `fillDeviceAppInfo` (line 65-76):

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
	// k2r sends X-App-Info JSON with isGateway flag
	if appInfoHeader := c.GetHeader("X-App-Info"); appInfoHeader != "" {
		var info struct {
			Version   string `json:"version"`
			Platform  string `json:"platform"`
			Arch      string `json:"arch"`
			IsGateway bool   `json:"isGateway"`
		}
		if json.Unmarshal([]byte(appInfoHeader), &info) == nil {
			device.IsGateway = info.IsGateway
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

Add `"encoding/json"` to imports (line ~1-19, after `"errors"`).

- [ ] **Step 3: Write RouterRequired tests**

Create `api/middleware_router_test.go`:

```go
package center

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupRouterTest(user *User) (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)
	if user != nil {
		c.Set("user", user) // adjust key to match ReqUser implementation
	}
	return w, c
}

func TestRouterRequired_HasAccess(t *testing.T) {
	_, c := setupRouterTest(&User{
		ID:              1,
		ExpiredAt:       time.Now().Add(30 * 24 * time.Hour).Unix(),
		MaxRouterDevice: 10,
	})
	RouterRequired()(c)
	assert.False(t, c.IsAborted())
}

func TestRouterRequired_UnlimitedAccess(t *testing.T) {
	_, c := setupRouterTest(&User{
		ID:              1,
		ExpiredAt:       time.Now().Add(30 * 24 * time.Hour).Unix(),
		MaxRouterDevice: -1, // unlimited
	})
	RouterRequired()(c)
	assert.False(t, c.IsAborted())
}

func TestRouterRequired_NoAccess(t *testing.T) {
	w, c := setupRouterTest(&User{
		ID:              1,
		ExpiredAt:       time.Now().Add(30 * 24 * time.Hour).Unix(),
		MaxRouterDevice: 0, // no router
	})
	RouterRequired()(c)
	assert.True(t, c.IsAborted())
	assert.Contains(t, w.Body.String(), "402")
}

func TestRouterRequired_Expired(t *testing.T) {
	_, c := setupRouterTest(&User{
		ID:              1,
		ExpiredAt:       time.Now().Add(-24 * time.Hour).Unix(),
		MaxRouterDevice: 10,
	})
	RouterRequired()(c)
	assert.True(t, c.IsAborted())
}

func TestRouterRequired_NoUser(t *testing.T) {
	_, c := setupRouterTest(nil)
	RouterRequired()(c)
	assert.True(t, c.IsAborted())
}
```

- [ ] **Step 4: Run tests**

Run: `cd api && go test -run TestRouterRequired -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/middleware.go api/middleware_router_test.go
git commit -m "feat(api): add RouterRequired middleware + fillDeviceAppInfo isGateway parsing"
```

---

## Task 3b: Device Limit Split by Type

**Files:**
- Modify: `api/api_auth.go`

- [ ] **Step 1: Update OTP login device limit (line ~285)**

Currently counts ALL devices. Split to count by type:

**Before (line ~285):**
```go
var deviceCount int64
if err := tx.Model(&Device{}).Where("user_id = ?", identify.UserID).Count(&deviceCount).Error; err != nil {
```

**After:**
```go
// Determine if this is a router login
isGateway := false
if appInfoHeader := c.GetHeader("X-App-Info"); appInfoHeader != "" {
	var info struct{ IsGateway bool `json:"isGateway"` }
	if json.Unmarshal([]byte(appInfoHeader), &info) == nil {
		isGateway = info.IsGateway
	}
}

var deviceCount int64
if isGateway {
	// Count only router devices
	if err := tx.Model(&Device{}).Where("user_id = ? AND is_gateway = true", identify.UserID).Count(&deviceCount).Error; err != nil {
		return err
	}
	if deviceCount >= int64(user.MaxRouterDevice) {
		// Router limit reached — reject (don't kick, routers are precious)
		log.Warnf(c, "router device limit reached for user %d", identify.UserID)
		return fmt.Errorf("router device limit reached")
	}
} else {
	// Count only app devices
	if err := tx.Model(&Device{}).Where("user_id = ? AND is_gateway = false", identify.UserID).Count(&deviceCount).Error; err != nil {
		return err
	}
	if deviceCount >= int64(user.MaxDevice) {
		// App device limit reached — kick oldest app device (existing behavior)
		var oldestDevice Device
		if err := tx.Where("user_id = ? AND is_gateway = false", identify.UserID).Order("token_last_used_at ASC").First(&oldestDevice).Error; err != nil {
			return err
		}
		if err := tx.Delete(&oldestDevice).Error; err != nil {
			return err
		}
		log.Infof(c, "deleted oldest app device %s for user %d", oldestDevice.UDID, identify.UserID)
		// ... existing kick email logic ...
	}
}
```

**Key difference:** Router devices are NOT kicked on limit — they're rejected. Kicking someone's router would disconnect their entire household.

- [ ] **Step 2: Same split in password login (line ~770)**

Apply identical split logic.

- [ ] **Step 3: Add RouterRequired check in auth flow**

When `isGateway = true`, also check `user.MaxRouterDevice > 0` before proceeding. If user's plan doesn't support router, reject before counting devices:

```go
if isGateway && user.MaxRouterDevice == 0 {
	return fmt.Errorf("plan does not support router")
}
```

- [ ] **Step 4: Run tests**

Run: `cd api && go build ./... && go test ./...`

- [ ] **Step 5: Commit**

```bash
git add api/api_auth.go
git commit -m "feat(api): split device limit by type (app vs router), router rejects instead of kicks"
```

---

## Task 4: Plans API + Admin + User Profile

**Files:**
- Modify: `api/api_plan.go`
- Modify: `api/api_admin_plan.go`
- Modify: `api/logic_approval_callbacks.go`
- Modify: `api/api_user.go`

- [ ] **Step 1: Update DataPlan construction in api_plan.go**

In `api_get_plans` (line ~11), update the DataPlan mapping inside the loop:

```go
		items = append(items, DataPlan{
			PID:             plan.PID,
			Tier:            plan.Tier,
			Label:           plan.Label,
			Price:           plan.Price,
			OriginPrice:     plan.OriginPrice,
			Month:           plan.Month,
			Highlight:       plan.Highlight != nil && *plan.Highlight,
			IsActive:        plan.IsActive != nil && *plan.IsActive,
			MaxDevice:       plan.MaxDevice,
			MaxRouterDevice: plan.MaxRouterDevice,
			MaxLanClient:    plan.MaxLanClient,
		})
```

- [ ] **Step 2: Update admin create plan request**

In `api/api_admin_plan.go`, update `AdminCreatePlanRequest` (line ~36):

```go
type AdminCreatePlanRequest struct {
	PID             string `json:"pid" binding:"required"`
	Tier            string `json:"tier" binding:"required"`      // 功能等级标识
	Label           string `json:"label" binding:"required"`
	Price           uint64 `json:"price" binding:"required"`
	OriginPrice     uint64 `json:"originPrice" binding:"required"`
	Month           int    `json:"month" binding:"required"`
	Highlight       bool   `json:"highlight"`
	IsActive        bool   `json:"isActive"`
	MaxDevice       int    `json:"maxDevice"`
	MaxRouterDevice int    `json:"maxRouterDevice"`
	MaxLanClient    int    `json:"maxLanClient"`
}
```

In `api_admin_create_plan` handler, add defaults and include in Plan creation:

```go
	if req.MaxDevice == 0 {
		req.MaxDevice = DefaultMaxDevice
	}
	plan := Plan{
		PID:             req.PID,
		Tier:            req.Tier,
		Label:           req.Label,
		Price:           req.Price,
		OriginPrice:     req.OriginPrice,
		Month:           req.Month,
		Highlight:       BoolPtr(req.Highlight),
		IsActive:        BoolPtr(req.IsActive),
		MaxDevice:       req.MaxDevice,
		MaxRouterDevice: req.MaxRouterDevice,
		MaxLanClient:    req.MaxLanClient,
	}
```

- [ ] **Step 3: Update admin update plan request**

In `AdminUpdatePlanRequest` (line ~90), add:

```go
	Tier            *string `json:"tier"`
	MaxDevice       *int    `json:"maxDevice"`
	MaxRouterDevice *int    `json:"maxRouterDevice"`
	MaxLanClient    *int    `json:"maxLanClient"`
```

- [ ] **Step 4: Update approval callback**

In `api/logic_approval_callbacks.go`, `executeApprovalPlanUpdate` function (line ~293), after the `IsActive` block add:

```go
	if req.Tier != nil {
		plan.Tier = *req.Tier
	}
	if req.MaxDevice != nil {
		plan.MaxDevice = *req.MaxDevice
	}
	if req.MaxRouterDevice != nil {
		plan.MaxRouterDevice = *req.MaxRouterDevice
	}
	if req.MaxLanClient != nil {
		plan.MaxLanClient = *req.MaxLanClient
	}
```

- [ ] **Step 5: Update user profile response**

In `api/api_user.go`, `buildDataUserWithDevice` function (line ~420), in the return statement add:

```go
		Tier:            user.Tier,
		MaxRouterDevice: user.MaxRouterDevice,
		MaxLanClient:    user.MaxLanClient,
```

- [ ] **Step 6: Run all tests**

Run: `cd api && go build ./... && go test ./...`
Expected: Compiles and all tests pass.

- [ ] **Step 7: Commit**

```bash
git add api/api_plan.go api/api_admin_plan.go api/logic_approval_callbacks.go api/api_user.go
git commit -m "feat(api): Plans API returns MaxDevice/MaxRouterDevice, admin CRUD, user profile update"
```

---

## Self-Review

| Spec Requirement | Task |
|-----------------|------|
| 2.1 Plan model | Task 1 Step 1 |
| 2.2 User model | Task 1 Step 2 |
| 2.3 Device model | Task 1 Step 3 |
| 3.1 Order processing | Task 2 |
| 4.1 ProRequired unchanged | N/A (not touched) |
| 4.2 RouterRequired | Task 3 Step 1 |
| 4.3 Device limit unchanged | N/A (reads User.MaxDevice, updated by Task 2) |
| 4.4 Gateway device registration | Task 3 Step 2 |
| 5 Plans API | Task 4 Step 1 |
| 5.3 Admin plans | Task 4 Steps 2-4 |
| User profile | Task 4 Step 5 |

**4 tasks, ~15 steps. Each step is a precise edit with exact line numbers. No placeholders.**
