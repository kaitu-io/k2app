# k2r Router Features Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the complete k2r router product — Subscription architecture, gateway plan pricing, router device management (MAC allowlist), OTA self-update, nftables Go library rewrite, DNS redirect, and all supporting UI/API/admin changes.

**Architecture:** Subscription table replaces User.ExpiredAt/MaxDevice as source of truth for entitlements. Gateway features (MAC allowlist, OTA, nftables) are implemented in k2/gateway/ on an independent branch. Webapp and website gain platform-aware purchase pages and router device management.

**Tech Stack:** Go (api/ + k2/gateway/), google/nftables (netlink), React+MUI (webapp/), Next.js (web/), GORM (MySQL), vitest (tests)

**Spec:** `docs/superpowers/specs/2026-04-09-k2r-router-release-features-design.md`
**Principles:** `docs/superpowers/specs/2026-04-09-k2r-development-principles.md`

---

## Dependency Graph

```
Phase 1: Subscription Architecture (api/)     ← FOUNDATION, do first
    │
    ├── Phase 2: Plans API + Order Processing (api/)
    │       │
    │       ├── Phase 5: Webapp Purchase + Conditional Rendering (webapp/)
    │       └── Phase 6: Website Purchase + Admin (web/)
    │
Phase 3: nftables Go Library Rewrite (k2/gateway/)  ← INDEPENDENT, can parallel with 1-2
    │
    └── Phase 4: RouterDevice Management + DNS Redirect (k2/gateway/)

Phase 7: Gateway OTA Updater (k2/gateway/)  ← INDEPENDENT, can parallel with anything
```

**Parallelizable:** Phases 1, 3, 7 can run simultaneously. Phase 2 after 1. Phase 4 after 3. Phases 5-6 after 2.

---

## Phase 1: Subscription Architecture

### Task 1.1: Subscription Model + Data Migration

**Files:**
- Modify: `api/model.go` — add Subscription struct, Plan.ProductType, Plan.Quota, Device.IsGateway
- Modify: `api/type.go` — add DataSubscription type, update DataPlan
- Create: `api/logic_subscription.go` — syncUserCache, subscription helpers
- Test: `api/logic_subscription_test.go`

- [ ] **Step 1: Add Subscription model to model.go**

After the existing `Plan` struct (line ~592), add:

```go
type Subscription struct {
	ID          uint64    `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	UserID      uint64    `gorm:"not null;uniqueIndex:idx_user_product" json:"userId"`
	ProductType string    `gorm:"type:varchar(20);not null;uniqueIndex:idx_user_product" json:"productType"`
	PlanPID     string    `gorm:"type:varchar(30);not null" json:"planPid"`
	ExpiredAt   int64     `gorm:"not null" json:"expiredAt"`
	Quota       int       `gorm:"not null" json:"quota"`
}

func (s *Subscription) IsExpired() bool {
	return s.ExpiredAt <= time.Now().Unix()
}
```

- [ ] **Step 2: Add ProductType and Quota to Plan model**

Modify the existing Plan struct in `model.go` (line ~581):

```go
type Plan struct {
	ID          uint64    `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	PID         string    `gorm:"column:pid;type:varchar(30);not null;uniqueIndex" json:"pid"`
	Label       string    `gorm:"type:varchar(255);not null" json:"label"`
	Price       uint64    `gorm:"not null" json:"price"`
	OriginPrice uint64    `gorm:"not null" json:"originPrice"`
	Month       int       `gorm:"not null" json:"month"`
	Highlight   *bool     `gorm:"default:false" json:"highlight"`
	IsActive    *bool     `gorm:"default:true" json:"isActive"`
	// new fields
	ProductType string `gorm:"type:varchar(20);not null;default:'personal'" json:"productType"`
	Quota       int    `gorm:"not null;default:5" json:"quota"`
}
```

- [ ] **Step 3: Add IsGateway to Device model**

In the Device struct in `model.go` (line ~132), add after the last field:

```go
	IsGateway bool `gorm:"not null;default:false" json:"isGateway"`
```

- [ ] **Step 4: Add Subscription to AutoMigrate**

Find the `AutoMigrate` call (likely in `cmd/` or startup code) and add `&Subscription{}` to the model list. Also ensure GORM auto-migrates the new Plan fields.

Run: `cd api && go build ./...`
Expected: Compiles successfully.

- [ ] **Step 5: Update DataPlan and add DataSubscription in type.go**

Update `DataPlan` (line ~512):

```go
type DataPlan struct {
	PID         string `json:"pid"`
	Label       string `json:"label"`
	Price       uint64 `json:"price"`
	OriginPrice uint64 `json:"originPrice"`
	Month       int    `json:"month"`
	Highlight   bool   `json:"highlight"`
	IsActive    bool   `json:"isActive"`
	ProductType string `json:"productType"`
	Quota       int    `json:"quota"`
}
```

Add `DataSubscription`:

```go
type DataSubscription struct {
	ID          uint64 `json:"id"`
	ProductType string `json:"productType"`
	PlanPID     string `json:"planPid"`
	ExpiredAt   int64  `json:"expiredAt"`
	Quota       int    `json:"quota"`
}
```

- [ ] **Step 6: Create logic_subscription.go with syncUserCache**

Create `api/logic_subscription.go`:

```go
package center

import (
	"context"

	"gorm.io/gorm"
)

// syncUserCache updates User.ExpiredAt and User.MaxDevice from the personal
// Subscription. This keeps legacy code paths (workers, EDM) working without
// modification. New code reads Subscription directly.
func syncUserCache(ctx context.Context, tx *gorm.DB, userID uint64) error {
	var sub Subscription
	err := tx.Where("user_id = ? AND product_type = 'personal'", userID).First(&sub).Error
	if err == gorm.ErrRecordNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	return tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"expired_at": sub.ExpiredAt,
		"max_device": sub.Quota,
	}).Error
}

// getActiveSubscription returns the active subscription for a user and product type.
// Returns nil if no active subscription exists.
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

// getUserSubscriptions returns all subscriptions for a user.
func getUserSubscriptions(tx *gorm.DB, userID uint64) ([]Subscription, error) {
	var subs []Subscription
	err := tx.Where("user_id = ?", userID).Find(&subs).Error
	return subs, err
}
```

- [ ] **Step 7: Write tests for syncUserCache**

Create `api/logic_subscription_test.go`:

```go
package center

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncUserCache_PersonalSubscription(t *testing.T) {
	mockDB := SetupMockDB(t)
	ctx := context.Background()

	futureExpiry := time.Now().Add(30 * 24 * time.Hour).Unix()

	// Expect: SELECT subscription WHERE user_id=1 AND product_type='personal'
	mockDB.ExpectQuery("SELECT.*FROM.*subscriptions.*WHERE.*user_id.*product_type").
		WithArgs(uint64(1), "personal").
		WillReturnRows(mockDB.NewRows([]string{"id", "user_id", "product_type", "plan_pid", "expired_at", "quota"}).
			AddRow(1, 1, "personal", "1y", futureExpiry, 5))

	// Expect: UPDATE users SET expired_at=?, max_device=? WHERE id=?
	mockDB.ExpectExec("UPDATE.*users.*SET.*expired_at.*max_device").
		WithArgs(futureExpiry, 5, uint64(1)).
		WillReturnResult(mockDB.NewResult(1, 1))

	err := syncUserCache(ctx, mockDB.DB(), 1)
	require.NoError(t, err)
}

func TestSyncUserCache_NoSubscription(t *testing.T) {
	mockDB := SetupMockDB(t)
	ctx := context.Background()

	// Expect: SELECT returns no rows
	mockDB.ExpectQuery("SELECT.*FROM.*subscriptions.*WHERE.*user_id.*product_type").
		WithArgs(uint64(1), "personal").
		WillReturnRows(mockDB.NewRows([]string{}))

	// No UPDATE expected — user untouched
	err := syncUserCache(ctx, mockDB.DB(), 1)
	require.NoError(t, err)
}

func TestSubscription_IsExpired(t *testing.T) {
	past := &Subscription{ExpiredAt: time.Now().Add(-1 * time.Hour).Unix()}
	assert.True(t, past.IsExpired())

	future := &Subscription{ExpiredAt: time.Now().Add(1 * time.Hour).Unix()}
	assert.False(t, future.IsExpired())
}
```

- [ ] **Step 8: Run tests**

Run: `cd api && go test -run TestSyncUserCache -v ./...`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add api/model.go api/type.go api/logic_subscription.go api/logic_subscription_test.go
git commit -m "feat(api): add Subscription model, Plan.ProductType/Quota, syncUserCache"
```

---

### Task 1.2: Rewrite Order Processing to Use Subscription

**Files:**
- Modify: `api/logic_member.go` — rewrite `applyOrderToTargetUsers` and `addProExpiredDays`
- Test: `api/member_test.go` — update existing tests

- [ ] **Step 1: Write test for Subscription-based order processing**

Add to `api/member_test.go`:

```go
func TestApplyOrderToTargetUsers_CreatesSubscription(t *testing.T) {
	mockDB := SetupMockDB(t)
	ctx := context.Background()

	plan := &Plan{PID: "1y", ProductType: "personal", Month: 12, Quota: 5}
	order := &Order{UserID: 1, IsPaid: boolPtr(true)}
	order.SetPlan(plan)
	order.SetForMyself(true)
	order.SetForUsers([]string{})

	// Expect: lookup user by ID
	mockDB.ExpectQuery("SELECT.*FROM.*users.*WHERE.*id").
		WillReturnRows(mockDB.NewRows(userColumns).AddRow(userRow(1)))

	// Expect: lookup existing subscription (not found → create)
	mockDB.ExpectQuery("SELECT.*FROM.*subscriptions.*WHERE.*user_id.*product_type").
		WillReturnRows(mockDB.NewRows([]string{}))

	// Expect: INSERT subscription
	mockDB.ExpectExec("INSERT INTO.*subscriptions").
		WillReturnResult(mockDB.NewResult(1, 1))

	// Expect: syncUserCache SELECT + UPDATE
	// ... (sync cache expectations)

	err := applyOrderToTargetUsers(ctx, mockDB.DB(), order)
	require.NoError(t, err)
}

func TestApplyOrderToTargetUsers_GatewayPlan_NoSyncCache(t *testing.T) {
	// Gateway subscription should NOT sync to User cache fields
	// (gateway state lives exclusively in Subscription)
	mockDB := SetupMockDB(t)
	ctx := context.Background()

	plan := &Plan{PID: "router-monthly-5", ProductType: "gateway", Month: 1, Quota: 5}
	order := &Order{UserID: 1, IsPaid: boolPtr(true)}
	order.SetPlan(plan)
	order.SetForMyself(true)

	// ... setup expectations ...
	// Expect: INSERT subscription for gateway
	// Expect: NO UPDATE to users table (no syncUserCache for gateway)

	err := applyOrderToTargetUsers(ctx, mockDB.DB(), order)
	require.NoError(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd api && go test -run TestApplyOrderToTargetUsers_CreatesSubscription -v ./...`
Expected: FAIL (old implementation doesn't create Subscription)

- [ ] **Step 3: Rewrite applyOrderToTargetUsers in logic_member.go**

Replace the existing `applyOrderToTargetUsers` function (lines ~77-168) with:

```go
func applyOrderToTargetUsers(ctx context.Context, tx *gorm.DB, order *Order) error {
	if order.IsPaid == nil || !*order.IsPaid {
		return fmt.Errorf("order not paid")
	}

	plan := order.GetPlan()
	if plan == nil {
		return fmt.Errorf("plan not found in order meta")
	}

	// Calculate days from months
	now := time.Now()
	endDate := now.AddDate(0, plan.Month, 0)
	days := int(endDate.Sub(now).Hours() / 24)

	// Collect target user IDs
	var targetUserIDs []uint64
	if order.GetForMyself() {
		targetUserIDs = append(targetUserIDs, order.UserID)
	}
	forUserUUIDs := order.GetForUsers()
	if len(forUserUUIDs) > 0 {
		var users []User
		if err := tx.Where("uuid IN ?", forUserUUIDs).Find(&users).Error; err != nil {
			return err
		}
		if len(users) != len(forUserUUIDs) {
			return fmt.Errorf("not all target users found")
		}
		for _, u := range users {
			targetUserIDs = append(targetUserIDs, u.ID)
		}
	}

	for _, uid := range targetUserIDs {
		var user User
		if err := tx.First(&user, uid).Error; err != nil {
			return fmt.Errorf("user %d not found: %w", uid, err)
		}

		// Upsert Subscription
		var sub Subscription
		err := tx.Where("user_id = ? AND product_type = ?", uid, plan.ProductType).First(&sub).Error
		if err == gorm.ErrRecordNotFound {
			// New subscription — start from now
			sub = Subscription{
				UserID:      uid,
				ProductType: plan.ProductType,
				PlanPID:     plan.PID,
				ExpiredAt:   now.AddDate(0, plan.Month, 0).Unix(),
				Quota:       plan.Quota,
			}
			if err := tx.Create(&sub).Error; err != nil {
				return fmt.Errorf("create subscription: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("query subscription: %w", err)
		} else {
			// Renew — extend from current expiry (or now if expired)
			base := time.Unix(sub.ExpiredAt, 0)
			if sub.IsExpired() {
				base = now
			}
			sub.ExpiredAt = base.AddDate(0, plan.Month, 0).Unix()
			sub.PlanPID = plan.PID
			sub.Quota = plan.Quota
			if err := tx.Save(&sub).Error; err != nil {
				return fmt.Errorf("update subscription: %w", err)
			}
		}

		// Record pro history (keep existing behavior)
		addProExpiredDaysFromSubscription(ctx, tx, &user, VipPurchase, order.ID, days, "order")

		// Sync User cache (personal only — gateway state is Subscription-only)
		if plan.ProductType == "personal" {
			if err := syncUserCache(ctx, tx, uid); err != nil {
				log.Warnf(ctx, "syncUserCache failed for user %d: %v", uid, err)
			}
		}
	}
	return nil
}
```

Note: `addProExpiredDaysFromSubscription` is a slimmed version of `addProExpiredDays` that only handles UserProHistory + IsFirstOrderDone + IsActivated, WITHOUT modifying ExpiredAt (that's now in Subscription). Implement this as a helper in the same file.

- [ ] **Step 4: Run tests**

Run: `cd api && go test -run TestApplyOrderToTargetUsers -v ./...`
Expected: PASS

- [ ] **Step 5: Run all existing tests to check for regressions**

Run: `cd api && go test ./...`
Expected: PASS (or document which tests need updating)

- [ ] **Step 6: Commit**

```bash
git add api/logic_member.go api/member_test.go
git commit -m "feat(api): rewrite order processing to create Subscription + syncUserCache"
```

---

### Task 1.3: Migrate ProRequired Middleware to Read Subscription

**Files:**
- Modify: `api/middleware.go` — rewrite ProRequired, add GatewayProRequired, parse isGateway from AppInfo
- Test: `api/middleware_mock_test.go`

- [ ] **Step 1: Write test for Subscription-based ProRequired**

Add to `api/middleware_mock_test.go`:

```go
func TestProRequired_ReadsSubscription(t *testing.T) {
	// Test that ProRequired checks Subscription table, not User.ExpiredAt
	// Setup: User.ExpiredAt = 0 (expired cache), but personal Subscription active
	// Expected: request passes (Subscription is source of truth)
}

func TestGatewayProRequired_ChecksGatewaySubscription(t *testing.T) {
	// Test that GatewayProRequired checks gateway Subscription
	// Setup: personal Subscription active, gateway Subscription expired
	// Expected: 402 PaymentRequired
}
```

- [ ] **Step 2: Rewrite ProRequired in middleware.go**

Replace the existing `ProRequired()` (line ~418):

```go
func ProRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := ReqUser(c)
		if user == nil {
			Error(c, ErrorNotLogin, "authentication failed")
			c.Abort()
			return
		}
		sub, err := getActiveSubscription(db.Get(), user.ID, "personal")
		if err != nil {
			Error(c, ErrorSystemError, "failed to check subscription")
			c.Abort()
			return
		}
		if sub == nil || sub.IsExpired() {
			Error(c, ErrorPaymentRequired, "membership expired")
			c.Abort()
			return
		}
		c.Next()
	}
}

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
			Error(c, ErrorSystemError, "failed to check subscription")
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

- [ ] **Step 3: Add isGateway to AppInfo parsing**

In `middleware.go` or `type.go`, add `IsGateway bool` to the `AppInfo` struct. Update the parsing logic to extract `isGateway` from the `X-App-Info` JSON header. In the device registration logic (both OTP and password auth paths), set `device.IsGateway = appInfo.IsGateway`.

- [ ] **Step 4: Run tests**

Run: `cd api && go test -run TestProRequired -v ./...`
Run: `cd api && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/middleware.go api/type.go api/middleware_mock_test.go
git commit -m "feat(api): ProRequired reads Subscription, add GatewayProRequired + Device.IsGateway"
```

---

### Task 1.4: Migrate Device Limit to Read Subscription

**Files:**
- Modify: `api/api_auth.go` — device count reads Subscription.Quota instead of User.MaxDevice

- [ ] **Step 1: Update device limit logic in OTP login (line ~291)**

Replace `user.MaxDevice` with Subscription query:

```go
// Get device quota from personal subscription
maxDevice := 5 // default fallback
sub, _ := getActiveSubscription(tx, user.ID, "personal")
if sub != nil {
	maxDevice = sub.Quota
}

if deviceCount >= int64(maxDevice) {
	// ... existing oldest-device removal logic unchanged ...
}
```

- [ ] **Step 2: Same change in password login (line ~776)**

Apply identical Subscription-based quota lookup.

- [ ] **Step 3: Run existing auth tests**

Run: `cd api && go test -run TestAuth -v ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add api/api_auth.go
git commit -m "feat(api): device limit reads Subscription.Quota instead of User.MaxDevice"
```

---

### Task 1.5: Data Migration Script

**Files:**
- Create: `api/cmd/migrate_subscriptions.go` or add to existing migrate command

- [ ] **Step 1: Add migration logic**

After AutoMigrate creates the `subscriptions` table, run a one-time data migration:

```go
// Populate subscriptions from existing users
result := tx.Exec(`
	INSERT INTO subscriptions (user_id, product_type, plan_pid, expired_at, quota, created_at, updated_at)
	SELECT id, 'personal', '', expired_at, max_device, NOW(), NOW()
	FROM users
	WHERE expired_at > 0
	AND id NOT IN (SELECT user_id FROM subscriptions WHERE product_type = 'personal')
`)
```

The `NOT IN` clause makes it idempotent.

- [ ] **Step 2: Test migration locally**

Run: `cd api/cmd && ./kaitu-center migrate -c ../config.yml`
Expected: subscriptions table created and populated.

- [ ] **Step 3: Commit**

```bash
git add api/cmd/
git commit -m "feat(api): add Subscription data migration from existing User.ExpiredAt"
```

---

## Phase 2: Plans API + Purchase Flow

### Task 2.1: Plans API product_type Filter

**Files:**
- Modify: `api/api_plan.go` — add product_type query filter
- Modify: `api/api_admin_plan.go` — add ProductType/Quota to CRUD

- [ ] **Step 1: Update GET /api/plans with product_type filter**

```go
func api_get_plans(c *gin.Context) {
	productType := c.DefaultQuery("product_type", "personal")

	var plans []Plan
	err := db.Get().Where("is_active = ? AND product_type = ?", true, productType).Find(&plans).Error
	if err != nil {
		Error(c, ErrorSystemError, "failed to load plans")
		return
	}

	var items []DataPlan
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
	ItemsAll(c, items)
}
```

- [ ] **Step 2: Update admin plan CRUD to include new fields**

In `api_admin_plan.go`, update `AdminCreatePlanRequest`:

```go
type AdminCreatePlanRequest struct {
	PID         string `json:"pid" binding:"required"`
	Label       string `json:"label" binding:"required"`
	Price       uint64 `json:"price" binding:"required"`
	OriginPrice uint64 `json:"originPrice" binding:"required"`
	Month       int    `json:"month" binding:"required"`
	Highlight   bool   `json:"highlight"`
	IsActive    bool   `json:"isActive"`
	ProductType string `json:"productType"`
	Quota       int    `json:"quota"`
}
```

Set defaults in the handler: if `ProductType` empty, default to `"personal"`. If `Quota` zero and ProductType is `"personal"`, default to 5.

Update the admin list endpoint to accept `product_type` query filter.

- [ ] **Step 3: Update user profile to return subscriptions**

In `api_user.go`, add subscriptions to the user info response:

```go
// After building dataUser
subs, _ := getUserSubscriptions(db.Get(), user.ID)
// Add subs to response
```

Add a `Subscriptions []DataSubscription` field to the user info response type.

- [ ] **Step 4: Run tests**

Run: `cd api && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/api_plan.go api/api_admin_plan.go api/api_user.go
git commit -m "feat(api): Plans API product_type filter, admin CRUD, user profile subscriptions"
```

---

## Phase 3: nftables Go Library Rewrite (k2 submodule)

> **Branch:** Create independent branch in k2 submodule (e.g., `feat/nftables-go-library`)

### Task 3.1: Add google/nftables Dependency

**Files:**
- Modify: `k2/go.mod` — add `github.com/google/nftables`
- Modify: `k2/gateway/intercept.go` — update NewInterceptor auto-detection

- [ ] **Step 1: Add dependency**

```bash
cd k2 && go get github.com/google/nftables@latest
```

- [ ] **Step 2: Update NewInterceptor to try netlink first**

In `k2/gateway/intercept.go`, update `NewInterceptor` to:
1. Try creating a netlink nftables connection
2. If succeeds → use Go nftables backend
3. If fails → try `iptables` command → use iptables backend
4. If both fail → error

- [ ] **Step 3: Commit**

```bash
cd k2 && git add go.mod go.sum gateway/intercept.go
git commit -m "feat(gateway): add google/nftables dependency, update interceptor detection"
```

---

### Task 3.2: Rewrite intercept_nft.go with Go Library

**Files:**
- Rewrite: `k2/gateway/intercept_nft.go` — replace shell exec with google/nftables
- Test: `k2/gateway/intercept_test.go` — update existing tests

- [ ] **Step 1: Rewrite nftInterceptor.Install()**

Replace the shell exec `buildScript` approach with Go nftables API:

```go
package gateway

import (
	"fmt"
	"log/slog"
	"net"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

type nftInterceptor struct {
	conn      *nftables.Conn
	table     *nftables.Table
	chain     *nftables.Chain
	macSet    *nftables.Set
	installed bool
}

func newNftInterceptor() (*nftInterceptor, error) {
	conn, err := nftables.New()
	if err != nil {
		return nil, fmt.Errorf("nftables netlink: %w", err)
	}
	return &nftInterceptor{conn: conn}, nil
}

func (n *nftInterceptor) Name() string { return "nftables" }

func (n *nftInterceptor) Install(cfg InterceptConfig) error {
	// Create table inet k2r
	n.table = n.conn.AddTable(&nftables.Table{
		Family: nftables.TableFamilyINet,
		Name:   "k2r",
	})

	// Create prerouting chain
	n.chain = n.conn.AddChain(&nftables.Chain{
		Name:     "prerouting",
		Table:    n.table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityMangle,
	})

	// Skip loopback
	n.addLoopbackBypass()

	// Exclude IPs (server, DNS, self)
	n.addExcludeRules(cfg.ExcludeIPs)

	// DNS redirect rules (before TPROXY)
	if cfg.DNSRedirect {
		n.addDNSRedirectRules(cfg)
	}

	// TPROXY rules per LAN subnet
	n.addTPROXYRules(cfg)

	// Flush (atomic commit)
	if err := n.conn.Flush(); err != nil {
		return fmt.Errorf("nftables flush: %w", err)
	}

	// ip rule + route for TPROXY fwmark
	n.setupIPRules()

	n.installed = true
	slog.Info("DIAG: gw-intercept-install", "backend", "nftables-netlink",
		"subnets", cfg.LANSubnets, "excludeCount", len(cfg.ExcludeIPs))
	return nil
}
```

Note: The actual `addTPROXYRules`, `addExcludeRules`, `addDNSRedirectRules` methods need to construct nftables expressions. This requires familiarity with the `google/nftables` API for building match + verdict expressions. Reference the existing shell script rules in `buildScript()` to ensure identical nftables semantics.

- [ ] **Step 2: Rewrite nftInterceptor.Remove()**

```go
func (n *nftInterceptor) Remove() {
	if n.table != nil {
		n.conn.DelTable(n.table)
		_ = n.conn.Flush()
	}
	// Clean up ip rules/routes
	_ = runCmd("ip", "rule", "del", "fwmark", "1", "table", "100")
	_ = runCmd("ip", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", "100")
	_ = runCmd("ip", "-6", "rule", "del", "fwmark", "1", "table", "100")
	_ = runCmd("ip", "-6", "route", "del", "local", "::/0", "dev", "lo", "table", "100")
	n.installed = false
	slog.Info("gateway: nftables rules cleaned up")
}
```

- [ ] **Step 3: Add MAC set management methods**

```go
// AddMACToSet adds a MAC address to the allowed_router_devices nftables set
func (n *nftInterceptor) AddMACToSet(mac net.HardwareAddr) error {
	if n.macSet == nil {
		return fmt.Errorf("MAC set not initialized (allowlist mode not active)")
	}
	err := n.conn.SetAddElements(n.macSet, []nftables.SetElement{
		{Key: mac},
	})
	if err != nil {
		return err
	}
	return n.conn.Flush()
}

// RemoveMACFromSet removes a MAC address from the nftables set
func (n *nftInterceptor) RemoveMACFromSet(mac net.HardwareAddr) error {
	if n.macSet == nil {
		return fmt.Errorf("MAC set not initialized")
	}
	err := n.conn.SetDeleteElements(n.macSet, []nftables.SetElement{
		{Key: mac},
	})
	if err != nil {
		return err
	}
	return n.conn.Flush()
}
```

- [ ] **Step 4: Add DNS redirect rules**

```go
func (n *nftInterceptor) addDNSRedirectRules(cfg InterceptConfig) {
	// For each LAN subnet, redirect port 53 to k2r DNS port
	// This MUST come before TPROXY rules in the chain
	for _, subnet := range cfg.LANSubnets {
		_, ipNet, _ := net.ParseCIDR(subnet)
		if ipNet.IP.To4() != nil {
			// IPv4 DNS redirect
			n.conn.AddRule(&nftables.Rule{
				Table: n.table,
				Chain: n.chain,
				// Match: ip saddr $subnet, l4proto {tcp, udp}, dport 53
				// Action: redirect to :$dns_port
				Exprs: buildDNSRedirectExprs(ipNet, cfg.DNSPort, false),
			})
		}
		// IPv6 equivalent
	}
}
```

- [ ] **Step 5: Update intercept_test.go**

Update existing tests to verify the new implementation produces equivalent behavior. Test Install + Remove + MAC set operations.

Run: `cd k2 && go test ./gateway/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd k2 && git add gateway/intercept_nft.go gateway/intercept_test.go
git commit -m "feat(gateway): rewrite nftables interceptor with google/nftables Go library

Direct netlink communication, no nft binary dependency.
Adds MAC set management and DNS redirect rules."
```

---

## Phase 4: RouterDevice Management (k2 submodule)

### Task 4.1: LAN Device Discovery

**Files:**
- Create: `k2/gateway/discovery.go` — platform-aware LAN device discovery
- Create: `k2/gateway/discovery_test.go`

- [ ] **Step 1: Define interface and implement backends**

```go
package gateway

// LanDevice represents a device discovered on the LAN
type LanDevice struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	Online   bool   `json:"online"`
}

// discoverLANDevices returns devices on the LAN using platform-specific methods.
// Priority: ubus (OpenWrt) → dnsmasq leases → ip neigh
func discoverLANDevices(lanSubnets []string) []LanDevice {
	if hasUbus() {
		return discoverViaUbus()
	}
	if path := findDnsmasqLeases(); path != "" {
		return discoverViaDnsmasqLeases(path)
	}
	return discoverViaIPNeigh(lanSubnets)
}
```

Implement each backend: `discoverViaUbus()`, `discoverViaDnsmasqLeases()`, `discoverViaIPNeigh()`.

- [ ] **Step 2: Write tests**

Test parsing of each backend's output format with sample data.

- [ ] **Step 3: Commit**

```bash
cd k2 && git add gateway/discovery.go gateway/discovery_test.go
git commit -m "feat(gateway): platform-aware LAN device discovery (ubus/dnsmasq/ip-neigh)"
```

---

### Task 4.2: RouterDevice Allowlist + API

**Files:**
- Create: `k2/gateway/router_device.go` — allowlist CRUD, API handlers
- Modify: `k2/gateway/api.go` — register new endpoints

- [ ] **Step 1: Implement allowlist storage and API handlers**

```go
package gateway

// RouterDeviceManager manages the MAC allowlist and nftables enforcement
type RouterDeviceManager struct {
	storage     *Storage
	interceptor Interceptor
	quota       int // from gateway Subscription, 0=unlimited
}

// GET /api/router-devices
func (m *RouterDeviceManager) handleList(w http.ResponseWriter, r *http.Request) { ... }

// POST /api/router-devices/allow
func (m *RouterDeviceManager) handleAllow(w http.ResponseWriter, r *http.Request) { ... }

// POST /api/router-devices/remove
func (m *RouterDeviceManager) handleRemove(w http.ResponseWriter, r *http.Request) { ... }

// POST /api/router-devices/mode
func (m *RouterDeviceManager) handleMode(w http.ResponseWriter, r *http.Request) { ... }
```

- [ ] **Step 2: Register endpoints in api.go**

Add to the HTTP mux in `gateway.go` or `api.go`:

```go
mux.HandleFunc("GET /api/router-devices", gw.routerDeviceMgr.handleList)
mux.HandleFunc("POST /api/router-devices/allow", gw.routerDeviceMgr.handleAllow)
mux.HandleFunc("POST /api/router-devices/remove", gw.routerDeviceMgr.handleRemove)
mux.HandleFunc("POST /api/router-devices/mode", gw.routerDeviceMgr.handleMode)
```

- [ ] **Step 3: Write tests**

Test allowlist CRUD operations, quota enforcement, mode switching.

- [ ] **Step 4: Commit**

```bash
cd k2 && git add gateway/router_device.go gateway/api.go
git commit -m "feat(gateway): RouterDevice management API (MAC allowlist CRUD + quota)"
```

---

## Phase 5: Webapp UI Changes

### Task 5.1: Purchase Page — Platform-Based Plans

**Files:**
- Modify: `webapp/src/pages/Purchase.tsx` — filter plans by platformType
- Modify: `webapp/src/services/api-types.ts` — add Plan.productType, Plan.quota, Subscription type
- Modify: `webapp/src/services/cloud-api.ts` or relevant API module — pass product_type param

- [ ] **Step 1: Update API types**

In `webapp/src/services/api-types.ts`, update Plan type and add Subscription:

```typescript
export interface Plan {
  pid: string;
  label: string;
  price: number;
  originPrice: number;
  month: number;
  highlight: boolean;
  productType: 'personal' | 'gateway';
  quota: number;
}

export interface Subscription {
  id: number;
  productType: 'personal' | 'gateway';
  planPid: string;
  expiredAt: number;
  quota: number;
}
```

- [ ] **Step 2: Update Purchase.tsx to filter by platform**

In the plans fetch logic, pass `product_type` based on `window._platform.platformType`:

```typescript
const productType = window._platform.platformType === 'gateway' ? 'gateway' : 'personal';
const response = await cloudApi.request('GET', `/plans?product_type=${productType}`);
```

Hide member selection when `productType === 'gateway'`.

- [ ] **Step 3: Add gateway plan card grouping**

For gateway plans, group by `quota` tier and render tier headers.

- [ ] **Step 4: Update membership benefits for gateway**

In `MembershipBenefits.tsx`, show gateway-specific benefits when `productType === 'gateway'`.

- [ ] **Step 5: Add i18n keys**

Add gateway purchase text to all 7 locale files in `webapp/src/i18n/locales/*/purchase.json`.

- [ ] **Step 6: Commit**

```bash
git add webapp/src/
git commit -m "feat(webapp): purchase page platform-based plan display + gateway benefits"
```

---

### Task 5.2: Router Device Management Page

**Files:**
- Create: `webapp/src/pages/RouterDevices.tsx`
- Modify: `webapp/src/App.tsx` or router config — add route
- Create: `webapp/src/i18n/locales/*/routerDevice.json` — all 7 locales

- [ ] **Step 1: Create RouterDevices page**

Implement the page as shown in the spec wireframe — mode toggle, quota display, online/offline device lists, allow/remove/remark actions.

- [ ] **Step 2: Add route (gateway only)**

Add `/router-devices` route, conditional on `platformType === 'gateway'`.

- [ ] **Step 3: Add i18n namespace**

Create `routerDevice.json` in all 7 locale directories with the keys from the spec.

- [ ] **Step 4: Commit**

```bash
git add webapp/src/pages/RouterDevices.tsx webapp/src/i18n/ webapp/src/App.tsx
git commit -m "feat(webapp): router device management page (MAC allowlist UI)"
```

---

### Task 5.3: Webapp Conditional Rendering

**Files:**
- Modify: Various components per spec Section 9

- [ ] **Step 1: Hide desktop-only features on gateway**

Add `platformType === 'gateway'` guards to hide: service reinstall, ADB helper, proxy mode toggle, TUN mode toggle.

- [ ] **Step 2: Show gateway-only features**

Show router device management nav item, LAN/DNS settings, interceptor status when `platformType === 'gateway'`.

- [ ] **Step 3: Gateway updater integration**

In `gateway-k2.ts`, implement `gatewayUpdater` (IUpdater) that calls `/api/updater/check` and `/api/updater/apply`. Set `window._platform.updater = gatewayUpdater`.

- [ ] **Step 4: Commit**

```bash
git add webapp/src/
git commit -m "feat(webapp): gateway conditional rendering + updater integration"
```

---

## Phase 6: Website + Admin

### Task 6.1: Website Purchase Page — Product Tabs

**Files:**
- Modify: `web/src/app/[locale]/purchase/PurchaseClient.tsx`
- Modify: `web/src/lib/api.ts` — add product_type param to getPlans()
- Modify: `web/messages/*/purchase.json` — add gateway labels (7 locales)

- [ ] **Step 1: Add product_type param to getPlans()**

```typescript
async getPlans(productType?: string): Promise<Plan[]> {
  const params = productType ? `?product_type=${productType}` : '';
  const response = await this.get(`/api/plans${params}`);
  return response.data;
}
```

- [ ] **Step 2: Add tab switcher to PurchaseClient**

Add `[个人版] [路由器版]` tabs at the top of the purchase page. Tab switch re-fetches plans with the corresponding product_type. URL param `?product=gateway` selects router tab on load.

- [ ] **Step 3: Add i18n keys**

Add `productPersonal`, `productGateway`, `routerDeviceAccess`, `routerDeviceUnlimited` to all 7 locale `purchase.json` files.

- [ ] **Step 4: Commit**

```bash
git add web/src/ web/messages/
git commit -m "feat(web): purchase page product tabs (personal/gateway)"
```

---

### Task 6.2: Admin Plans Management

**Files:**
- Modify: `web/src/app/(manager)/manager/plans/page.tsx`

- [ ] **Step 1: Add ProductType and Quota columns**

Add `ProductType` and `Quota` columns to the plans table. Add these fields to the create/edit dialog forms. Add a product_type dropdown filter.

- [ ] **Step 2: Commit**

```bash
git add web/src/app/
git commit -m "feat(web): admin plans management with ProductType/Quota fields"
```

---

### Task 6.3: Website Install Page — Router Tab

**Files:**
- Modify: `web/src/app/[locale]/install/` — add router tab

- [ ] **Step 1: Add router tab content**

Add a 「路由器」tab with one-line install command, architecture info, and post-install instructions.

- [ ] **Step 2: Commit**

```bash
git add web/src/app/
git commit -m "feat(web): install page router tab with one-line install command"
```

---

## Phase 7: Gateway OTA Updater (k2 submodule)

### Task 7.1: Updater Implementation

**Files:**
- Create: `k2/gateway/updater.go` — CDN check, download, verify, backup, replace, restart
- Create: `k2/gateway/updater_test.go`
- Modify: `k2/gateway/api.go` — register updater endpoints

- [ ] **Step 1: Implement updater core**

```go
package gateway

type Updater struct {
	currentVersion string
	arch           string
	cdnBaseURL     string
	binaryPath     string // /usr/bin/k2r
}

func (u *Updater) Check() (*UpdateInfo, error) {
	// GET $cdnBaseURL/LATEST → compare version
}

func (u *Updater) Apply(onProgress func(stage string, progress float64)) error {
	// 1. Download binary to /tmp/k2r-update-{version}
	// 2. Verify SHA256 from checksums.txt
	// 3. Backup: cp /usr/bin/k2r /usr/bin/k2r.bak
	// 4. Atomic replace: rename temp → /usr/bin/k2r + chmod +x
	// 5. Restart service (detect init system)
}
```

- [ ] **Step 2: Add HTTP API endpoints**

```go
// POST /api/updater/check
// POST /api/updater/apply
// GET  /api/updater/status (SSE)
```

- [ ] **Step 3: Write tests**

Test version comparison, checksum verification, backup logic.

- [ ] **Step 4: Commit**

```bash
cd k2 && git add gateway/updater.go gateway/updater_test.go gateway/api.go
git commit -m "feat(gateway): OTA self-update (CDN check, download, verify, backup, replace)"
```

---

## Phase 8: CI + Final Integration

### Task 8.1: Enable CI

**Files:**
- Modify: `.github/workflows/release-openwrt.yml`

- [ ] **Step 1: Uncomment v* tag trigger**

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/release-openwrt.yml
git commit -m "ci: enable v* tag auto-trigger for k2r release"
```

---

## Self-Review Checklist

| Spec Section | Plan Task |
|-------------|-----------|
| 1. Entity Relationships | Task 1.1 (model) |
| 2. Data Model | Tasks 1.1-1.5 |
| 3. Plans API | Task 2.1 |
| 4. Purchase Page | Tasks 5.1, 6.1 |
| 5. Subscription Expiry | Task 1.3 (ProRequired/GatewayProRequired) |
| 6. RouterDevice Management | Tasks 3.2, 4.1, 4.2, 5.2 |
| 6.6a DNS Redirect | Task 3.2 (nftables rewrite includes DNS redirect) |
| 7. OTA Self-Update | Task 7.1, Task 5.3 (webapp updater) |
| 8. Gateway Device Registration | Task 1.3 (IsGateway in middleware) |
| 9. Webapp Conditional Rendering | Task 5.3 |
| 10. Website Changes | Tasks 6.1, 6.3 |
| 11. Admin Dashboard | Task 6.2 |
| 12. CI | Task 8.1 |
