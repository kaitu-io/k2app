# License Key Batch Decoupling — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Decouple license key batch operations from campaigns — introduce `LicenseKeyBatch` as standalone module, clean campaign model, add conversion tracking stats.

**Architecture:** New `license_key_batches` table + GORM model. New admin API handlers in `api_admin_license_key_batch.go`. Redefine `LicenseKey` model with `batch_id` (existing 6 test rows will be deleted). Clean campaign of shareable fields. Update MCP tools and web admin pages.

**Tech Stack:** Go (Gin, GORM), TypeScript (Next.js, shadcn/ui), MCP tools (Node.js, zod)

**Spec:** `docs/plans/2026-04-01-license-key-batch-decoupling.md`

**Simplification:** The `license_keys` table has only 6 test rows with no campaign associations. We DELETE them and redefine the model cleanly — no data migration needed.

---

### Task 1: Model + Migration

**Files:**
- Modify: `api/model.go` — add `LicenseKeyBatch`, redefine `LicenseKey`, clean `Campaign`
- Modify: `api/migrate.go` — add `LicenseKeyBatch`, add post-migrate cleanup

- [ ] **Step 1: Add LicenseKeyBatch model to `api/model.go`**

Insert before the existing `LicenseKey` struct (line 1084):

```go
// LicenseKeyBatch 授权码批次（独立于活动码的分发单位）
type LicenseKeyBatch struct {
	ID        uint64         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	Name             string `gorm:"type:varchar(255);not null" json:"name"`
	SourceTag        string `gorm:"type:varchar(100);not null;default:'';index" json:"sourceTag"`
	RecipientMatcher string `gorm:"type:varchar(50);not null;default:'all'" json:"recipientMatcher"`
	PlanDays         int    `gorm:"not null" json:"planDays"`
	Quantity         int    `gorm:"not null" json:"quantity"`
	ExpiresAt        int64  `gorm:"not null" json:"expiresAt"`
	Note             string `gorm:"type:text" json:"note"`
	CreatedByUserID  uint64 `gorm:"not null" json:"createdByUserId"`
}

func (LicenseKeyBatch) TableName() string { return "license_key_batches" }
```

- [ ] **Step 2: Redefine LicenseKey model — add BatchID + Batch, keep old fields for GORM migration compatibility**

Replace the existing `LicenseKey` struct with:

```go
// LicenseKey 一次性授权码
type LicenseKey struct {
	ID        uint64         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	UUID string `gorm:"type:varchar(50);uniqueIndex;not null" json:"uuid"`
	Code string `gorm:"type:varchar(8);uniqueIndex;not null" json:"code"`

	BatchID uint64           `gorm:"not null;default:0;index" json:"batchId"`
	Batch   *LicenseKeyBatch `gorm:"foreignKey:BatchID" json:"-"`

	PlanDays     int        `gorm:"not null;default:30" json:"planDays"`
	ExpiresAt    int64      `gorm:"not null" json:"expiresAt"`
	IsUsed       bool       `gorm:"default:false" json:"isUsed"`
	UsedByUserID *uint64    `gorm:"index" json:"usedByUserId"`
	UsedAt       *time.Time `json:"usedAt"`

	// Legacy fields — GORM AutoMigrate won't drop these. They'll be ignored by new code.
	// DROP COLUMN manually after deploy is verified.
	Source           string  `gorm:"type:varchar(16);not null;default:'batch'" json:"-"`
	Note             string  `gorm:"type:varchar(255)" json:"-"`
	RecipientMatcher string  `gorm:"type:varchar(50);not null;default:'all'" json:"-"`
	CampaignID       *uint64 `gorm:"index" json:"-"`
	CreatedByUserID  *uint64 `gorm:"index" json:"-"`
}
```

Note: Legacy fields have `json:"-"` to hide from API responses. They remain in DB until manual DROP.

- [ ] **Step 3: Remove `IsShareable` and `SharesPerUser` from Campaign model**

In `api/model.go`, delete from Campaign struct (lines 758-759):
```go
	IsShareable   bool   `gorm:"default:false" json:"isShareable"`
	SharesPerUser int64  `gorm:"default:0" json:"sharesPerUser"`
```

- [ ] **Step 4: Add LicenseKeyBatch to AutoMigrate + cleanup old data**

In `api/migrate.go`, add `&LicenseKeyBatch{}` before `&LicenseKey{}`:

```go
		&Campaign{},
		&LicenseKeyBatch{},
		&LicenseKey{},
```

After the `AutoMigrate` call (before the `return`), add cleanup for orphaned test data:

```go
	// Clean up legacy license keys without a batch (test data from pre-batch era)
	db.Get().Where("batch_id = 0").Delete(&LicenseKey{})
```

- [ ] **Step 5: Verify compilation**

Run: `cd api && go build ./...`

- [ ] **Step 6: Commit**

```bash
git add api/model.go api/migrate.go
git commit -m "feat: add LicenseKeyBatch model, clean LicenseKey and Campaign"
```

---

### Task 2: Request/Response Types

**Files:**
- Modify: `api/type.go`

- [ ] **Step 1: Add batch types to `api/type.go`**

Add after the existing LicenseKey types section (around line 1035):

```go
// ========================= LicenseKeyBatch 类型定义 =========================

type CreateLicenseKeyBatchRequest struct {
	Name             string `json:"name" binding:"required"`
	SourceTag        string `json:"sourceTag"`
	RecipientMatcher string `json:"recipientMatcher" binding:"required,oneof=all never_paid"`
	PlanDays         int    `json:"planDays" binding:"required,min=1"`
	Quantity         int    `json:"quantity" binding:"required,min=1,max=10000"`
	ExpiresInDays    int    `json:"expiresInDays" binding:"required,min=1"`
	Note             string `json:"note"`
}

type LicenseKeyBatchResponse struct {
	ID               uint64 `json:"id"`
	Name             string `json:"name"`
	SourceTag        string `json:"sourceTag"`
	RecipientMatcher string `json:"recipientMatcher"`
	PlanDays         int    `json:"planDays"`
	Quantity         int    `json:"quantity"`
	ExpiresAt        int64  `json:"expiresAt"`
	Note             string `json:"note"`
	CreatedByUserID  uint64 `json:"createdByUserId"`
	RedeemedCount    int64  `json:"redeemedCount"`
	ExpiredCount     int64  `json:"expiredCount"`
	CreatedAt        int64  `json:"createdAt"`
}

type LicenseKeyBatchDetailResponse struct {
	LicenseKeyBatchResponse
	ConvertedUsers int64   `json:"convertedUsers"`
	ConversionRate float64 `json:"conversionRate"`
	Revenue        uint64  `json:"revenue"`
}

type BatchStatsResponse struct {
	BatchID        uint64  `json:"batchId"`
	Name           string  `json:"name"`
	SourceTag      string  `json:"sourceTag"`
	TotalKeys      int64   `json:"totalKeys"`
	Redeemed       int64   `json:"redeemed"`
	Expired        int64   `json:"expired"`
	RedeemRate     float64 `json:"redeemRate"`
	ConvertedUsers int64   `json:"convertedUsers"`
	ConversionRate float64 `json:"conversionRate"`
	Revenue        uint64  `json:"revenue"`
}

type BatchStatsBySourceResponse struct {
	SourceTag      string  `json:"sourceTag"`
	TotalKeys      int64   `json:"totalKeys"`
	Redeemed       int64   `json:"redeemed"`
	RedeemRate     float64 `json:"redeemRate"`
	ConvertedUsers int64   `json:"convertedUsers"`
	ConversionRate float64 `json:"conversionRate"`
	Revenue        uint64  `json:"revenue"`
}

type BatchStatsTrendResponse struct {
	Date           string `json:"date"`
	Redeemed       int64  `json:"redeemed"`
	ConvertedUsers int64  `json:"convertedUsers"`
}

type LicenseKeyItemResponse struct {
	ID           uint64  `json:"id"`
	Code         string  `json:"code"`
	PlanDays     int     `json:"planDays"`
	ExpiresAt    int64   `json:"expiresAt"`
	IsUsed       bool    `json:"isUsed"`
	UsedByUserID *uint64 `json:"usedByUserId,omitempty"`
	UsedAt       *int64  `json:"usedAt,omitempty"`
}
```

- [ ] **Step 2: Remove old types that are no longer needed**

Delete from `api/type.go`: `IssueKeysRequest`, `IssueKeysResponse`, `CreateLicenseKeysRequest`, `CreateLicenseKeysResponse`, `LicenseKeyBrief` (lines 997-1033).

Simplify `LicenseKeyResponse` — remove `Source`, `Note`, `RecipientMatcher`, `CampaignID`, `CreatedByUserID`, add `BatchID`:

```go
type LicenseKeyResponse struct {
	ID           uint64  `json:"id"`
	UUID         string  `json:"uuid"`
	Code         string  `json:"code"`
	BatchID      uint64  `json:"batchId"`
	PlanDays     int     `json:"planDays"`
	ExpiresAt    int64   `json:"expiresAt"`
	IsUsed       bool    `json:"isUsed"`
	UsedByUserID *uint64 `json:"usedByUserId"`
	UsedAt       *int64  `json:"usedAt"`
	CreatedAt    int64   `json:"createdAt"`
}
```

Remove `IsShareable` and `SharesPerUser` from `CampaignRequest` and `CampaignResponse`.

- [ ] **Step 3: Verify**

Run: `cd api && go build ./...`
Expected: Compilation errors from handlers referencing deleted types — will be fixed in later tasks.

- [ ] **Step 4: Commit**

```bash
git add api/type.go
git commit -m "feat: add LicenseKeyBatch types, clean old license key and campaign types"
```

---

### Task 3: Batch Business Logic (No Raw SQL)

**Files:**
- Create: `api/logic_license_key_batch.go`

All stats queries use GORM struct queries + Go-side aggregation to comply with the project's "no raw SQL" convention.

- [ ] **Step 1: Create `api/logic_license_key_batch.go`**

```go
package center

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/xid"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// CreateLicenseKeyBatch creates a batch record and generates all keys.
func CreateLicenseKeyBatch(ctx context.Context, req *CreateLicenseKeyBatchRequest, adminUserID uint64) (*LicenseKeyBatch, error) {
	expiresAt := time.Now().AddDate(0, 0, req.ExpiresInDays).Unix()

	batch := LicenseKeyBatch{
		Name:             req.Name,
		SourceTag:        req.SourceTag,
		RecipientMatcher: req.RecipientMatcher,
		PlanDays:         req.PlanDays,
		Quantity:         req.Quantity,
		ExpiresAt:        expiresAt,
		Note:             req.Note,
		CreatedByUserID:  adminUserID,
	}

	if err := db.Get().Create(&batch).Error; err != nil {
		return nil, fmt.Errorf("create batch: %w", err)
	}

	count, err := GenerateLicenseKeysForBatch(ctx, &batch)
	if err != nil {
		return nil, fmt.Errorf("generate keys: %w", err)
	}

	log.Infof(ctx, "[LICENSE_KEY_BATCH] created batch=%d name=%q keys=%d", batch.ID, batch.Name, count)
	return &batch, nil
}

// GenerateLicenseKeysForBatch generates keys for a batch and inserts in chunks.
func GenerateLicenseKeysForBatch(ctx context.Context, batch *LicenseKeyBatch) (int64, error) {
	keys := make([]LicenseKey, 0, batch.Quantity)

	for i := 0; i < batch.Quantity; i++ {
		code, err := GenerateShortCode(ctx)
		if err != nil {
			return 0, fmt.Errorf("generate code for key %d: %w", i+1, err)
		}
		keys = append(keys, LicenseKey{
			UUID:      xid.New().String(),
			Code:      code,
			BatchID:   batch.ID,
			PlanDays:  batch.PlanDays,
			ExpiresAt: batch.ExpiresAt,
		})
	}

	batchSize := 100
	for i := 0; i < len(keys); i += batchSize {
		end := min(i+batchSize, len(keys))
		if err := db.Get().CreateInBatches(keys[i:end], batchSize).Error; err != nil {
			return int64(i), fmt.Errorf("batch insert at offset %d: %w", i, err)
		}
	}

	return int64(len(keys)), nil
}

// batchBaseCounts returns redeemed and expired counts for a batch.
func batchBaseCounts(batchID uint64) (redeemed, expired int64) {
	now := time.Now().Unix()
	db.Get().Model(&LicenseKey{}).Where("batch_id = ? AND is_used = true", batchID).Count(&redeemed)
	db.Get().Model(&LicenseKey{}).Where("batch_id = ? AND is_used = false AND expires_at < ?", batchID, now).Count(&expired)
	return
}

// redeemInfo holds the minimum data needed for conversion calculation.
type redeemInfo struct {
	UsedByUserID uint64    `gorm:"column:used_by_user_id"`
	UsedAt       time.Time `gorm:"column:used_at"`
}

// calcConversion computes converted users and revenue from a set of redeemed keys.
// Uses two GORM struct queries + Go-side matching (no raw SQL).
func calcConversion(redeems []redeemInfo) (convertedUsers int64, revenue uint64) {
	if len(redeems) == 0 {
		return 0, 0
	}

	userIDs := make([]uint64, 0, len(redeems))
	userRedeemTime := make(map[uint64]time.Time, len(redeems))
	for _, r := range redeems {
		userIDs = append(userIDs, r.UsedByUserID)
		userRedeemTime[r.UsedByUserID] = r.UsedAt
	}

	var orders []Order
	db.Get().Where("user_id IN ? AND is_paid = true", userIDs).
		Find(&orders)

	convertedSet := make(map[uint64]bool)
	for _, o := range orders {
		redeemTime, ok := userRedeemTime[o.UserID]
		if ok && o.CreatedAt.After(redeemTime) && !convertedSet[o.UserID] {
			convertedSet[o.UserID] = true
			revenue += o.PayAmount
		}
	}
	return int64(len(convertedSet)), revenue
}

// GetBatchStats computes redemption + conversion stats for a single batch.
func GetBatchStats(ctx context.Context, batchID uint64) (*BatchStatsResponse, error) {
	var batch LicenseKeyBatch
	if err := db.Get().First(&batch, batchID).Error; err != nil {
		return nil, err
	}

	redeemed, expired := batchBaseCounts(batchID)

	// Get redeemed keys for conversion calculation
	var redeems []redeemInfo
	db.Get().Model(&LicenseKey{}).
		Select("used_by_user_id, used_at").
		Where("batch_id = ? AND is_used = true AND used_by_user_id IS NOT NULL", batchID).
		Scan(&redeems)

	convertedUsers, totalRevenue := calcConversion(redeems)

	total := int64(batch.Quantity)
	redeemRate := float64(0)
	if total > 0 {
		redeemRate = float64(redeemed) / float64(total)
	}
	conversionRate := float64(0)
	if redeemed > 0 {
		conversionRate = float64(convertedUsers) / float64(redeemed)
	}

	return &BatchStatsResponse{
		BatchID:        batch.ID,
		Name:           batch.Name,
		SourceTag:      batch.SourceTag,
		TotalKeys:      total,
		Redeemed:       redeemed,
		Expired:        expired,
		RedeemRate:     redeemRate,
		ConvertedUsers: convertedUsers,
		ConversionRate: conversionRate,
		Revenue:        totalRevenue,
	}, nil
}

// GetAllBatchStats computes stats for all non-deleted batches.
func GetAllBatchStats(ctx context.Context) ([]BatchStatsResponse, error) {
	var batches []LicenseKeyBatch
	if err := db.Get().Order("created_at DESC").Find(&batches).Error; err != nil {
		return nil, err
	}

	results := make([]BatchStatsResponse, 0, len(batches))
	for _, b := range batches {
		stats, err := GetBatchStats(ctx, b.ID)
		if err != nil {
			log.Warnf(ctx, "[LICENSE_KEY_BATCH] failed to get stats for batch %d: %v", b.ID, err)
			continue
		}
		results = append(results, *stats)
	}
	return results, nil
}

// GetBatchStatsBySource aggregates stats by source_tag using GORM + Go aggregation.
func GetBatchStatsBySource(ctx context.Context) ([]BatchStatsBySourceResponse, error) {
	var batches []LicenseKeyBatch
	if err := db.Get().Order("source_tag").Find(&batches).Error; err != nil {
		return nil, err
	}

	// Group batches by source_tag
	type sourceGroup struct {
		totalKeys int64
		batchIDs  []uint64
	}
	groups := make(map[string]*sourceGroup)
	for _, b := range batches {
		g, ok := groups[b.SourceTag]
		if !ok {
			g = &sourceGroup{}
			groups[b.SourceTag] = g
		}
		g.totalKeys += int64(b.Quantity)
		g.batchIDs = append(g.batchIDs, b.ID)
	}

	results := make([]BatchStatsBySourceResponse, 0, len(groups))
	for tag, g := range groups {
		var redeemed int64
		db.Get().Model(&LicenseKey{}).Where("batch_id IN ? AND is_used = true", g.batchIDs).Count(&redeemed)

		var redeems []redeemInfo
		db.Get().Model(&LicenseKey{}).
			Select("used_by_user_id, used_at").
			Where("batch_id IN ? AND is_used = true AND used_by_user_id IS NOT NULL", g.batchIDs).
			Scan(&redeems)

		convertedUsers, revenue := calcConversion(redeems)

		redeemRate := float64(0)
		if g.totalKeys > 0 {
			redeemRate = float64(redeemed) / float64(g.totalKeys)
		}
		convRate := float64(0)
		if redeemed > 0 {
			convRate = float64(convertedUsers) / float64(redeemed)
		}

		results = append(results, BatchStatsBySourceResponse{
			SourceTag:      tag,
			TotalKeys:      g.totalKeys,
			Redeemed:       redeemed,
			RedeemRate:     redeemRate,
			ConvertedUsers: convertedUsers,
			ConversionRate: convRate,
			Revenue:        revenue,
		})
	}
	return results, nil
}

// GetBatchStatsTrend returns daily redemption counts for the last N days.
func GetBatchStatsTrend(ctx context.Context, days int) ([]BatchStatsTrendResponse, error) {
	if days <= 0 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days)

	// Get all redeemed keys in the window
	var redeems []redeemInfo
	db.Get().Model(&LicenseKey{}).
		Select("used_by_user_id, used_at").
		Where("is_used = true AND used_at >= ? AND batch_id > 0", since).
		Scan(&redeems)

	// Group by date in Go
	type dayData struct {
		redeemed int64
		userIDs  map[uint64]time.Time // userID → usedAt
	}
	dayMap := make(map[string]*dayData)
	for _, r := range redeems {
		date := r.UsedAt.Format("2006-01-02")
		d, ok := dayMap[date]
		if !ok {
			d = &dayData{userIDs: make(map[uint64]time.Time)}
			dayMap[date] = d
		}
		d.redeemed++
		d.userIDs[r.UsedByUserID] = r.UsedAt
	}

	// For conversion: collect all unique user IDs across all days
	allUserIDs := make(map[uint64]time.Time)
	for _, r := range redeems {
		allUserIDs[r.UsedByUserID] = r.UsedAt
	}

	userIDSlice := make([]uint64, 0, len(allUserIDs))
	for uid := range allUserIDs {
		userIDSlice = append(userIDSlice, uid)
	}

	// Query paid orders for these users
	paidUsers := make(map[uint64]bool)
	if len(userIDSlice) > 0 {
		var orders []Order
		db.Get().Where("user_id IN ? AND is_paid = true", userIDSlice).Find(&orders)
		for _, o := range orders {
			redeemTime, ok := allUserIDs[o.UserID]
			if ok && o.CreatedAt.After(redeemTime) {
				paidUsers[o.UserID] = true
			}
		}
	}

	// Build response sorted by date
	results := make([]BatchStatsTrendResponse, 0, len(dayMap))
	for date, d := range dayMap {
		var converted int64
		for uid := range d.userIDs {
			if paidUsers[uid] {
				converted++
			}
		}
		results = append(results, BatchStatsTrendResponse{
			Date:           date,
			Redeemed:       d.redeemed,
			ConvertedUsers: converted,
		})
	}

	// Sort by date ascending
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[i].Date > results[j].Date {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	return results, nil
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd api && go build ./...`

- [ ] **Step 3: Commit**

```bash
git add api/logic_license_key_batch.go
git commit -m "feat: add LicenseKeyBatch business logic with GORM-only conversion tracking"
```

---

### Task 4: Admin API Handlers + Routes + Approval

**Files:**
- Create: `api/api_admin_license_key_batch.go`
- Modify: `api/route.go`
- Modify: `api/logic_approval_callbacks.go`
- Modify: `api/worker_integration.go`

- [ ] **Step 1: Create `api/api_admin_license_key_batch.go`**

```go
package center

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// POST /app/license-key-batches
func api_admin_create_license_key_batch(c *gin.Context) {
	var req CreateLicenseKeyBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Include admin user ID in approval params so the callback can use it
	type batchCreateParams struct {
		CreateLicenseKeyBatchRequest
		AdminUserID uint64 `json:"adminUserId"`
	}
	actor := ReqUser(c)
	params := batchCreateParams{
		CreateLicenseKeyBatchRequest: req,
		AdminUserID:                  actor.ID,
	}

	summary := fmt.Sprintf("创建授权码批次「%s」(%d 个, %d 天, 渠道:%s)", req.Name, req.Quantity, req.PlanDays, req.SourceTag)
	approvalID, executed, err := SubmitApproval(c, "license_key_batch_create", params, summary)
	if err != nil {
		log.Errorf(c, "failed to submit approval for license key batch: %v", err)
		Error(c, ErrorSystemError, "failed to submit approval")
		return
	}

	if !executed {
		PendingApproval(c, approvalID)
		return
	}
	SuccessEmpty(c)
}

// GET /app/license-key-batches
func api_admin_list_license_key_batches(c *gin.Context) {
	pagination := PaginationFromRequest(c)

	query := db.Get().Model(&LicenseKeyBatch{})
	if tag := c.Query("sourceTag"); tag != "" {
		query = query.Where("source_tag = ?", tag)
	}

	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count license key batches: %v", err)
		Error(c, ErrorSystemError, "failed to count batches")
		return
	}

	var batches []LicenseKeyBatch
	if err := query.Order("created_at DESC").Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&batches).Error; err != nil {
		log.Errorf(c, "failed to query license key batches: %v", err)
		Error(c, ErrorSystemError, "failed to query batches")
		return
	}

	items := make([]LicenseKeyBatchResponse, 0, len(batches))
	for _, b := range batches {
		redeemed, expired := batchBaseCounts(b.ID)
		items = append(items, toLicenseKeyBatchResponse(&b, redeemed, expired))
	}

	ListWithData(c, items, pagination)
}

// GET /app/license-key-batches/:id
func api_admin_get_license_key_batch(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}

	stats, err := GetBatchStats(c.Request.Context(), id)
	if err != nil {
		Error(c, ErrorNotFound, "batch not found")
		return
	}

	var batch LicenseKeyBatch
	db.Get().First(&batch, id)

	redeemed, expired := batchBaseCounts(id)
	resp := LicenseKeyBatchDetailResponse{
		LicenseKeyBatchResponse: toLicenseKeyBatchResponse(&batch, redeemed, expired),
		ConvertedUsers:          stats.ConvertedUsers,
		ConversionRate:          stats.ConversionRate,
		Revenue:                 stats.Revenue,
	}
	Success(c, &resp)
}

// GET /app/license-key-batches/:id/keys
func api_admin_list_license_key_batch_keys(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}

	var batch LicenseKeyBatch
	if err := db.Get().First(&batch, id).Error; err != nil {
		Error(c, ErrorNotFound, "batch not found")
		return
	}

	pagination := PaginationFromRequest(c)
	now := time.Now().Unix()
	query := db.Get().Model(&LicenseKey{}).Where("batch_id = ?", id)

	switch c.Query("status") {
	case "used":
		query = query.Where("is_used = true")
	case "unused":
		query = query.Where("is_used = false AND expires_at >= ?", now)
	case "expired":
		query = query.Where("is_used = false AND expires_at < ?", now)
	}

	if err := query.Count(&pagination.Total).Error; err != nil {
		Error(c, ErrorSystemError, "failed to count keys")
		return
	}

	var keys []LicenseKey
	if err := query.Order("created_at DESC").Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&keys).Error; err != nil {
		Error(c, ErrorSystemError, "failed to query keys")
		return
	}

	items := make([]LicenseKeyItemResponse, len(keys))
	for i, k := range keys {
		items[i] = toLicenseKeyItemResponse(&k)
	}
	ListWithData(c, items, pagination)
}

// DELETE /app/license-key-batches/:id
func api_admin_delete_license_key_batch(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}

	var batch LicenseKeyBatch
	if err := db.Get().First(&batch, id).Error; err != nil {
		Error(c, ErrorNotFound, "batch not found")
		return
	}

	params := struct {
		BatchID uint64 `json:"batchId"`
	}{BatchID: id}
	summary := fmt.Sprintf("删除授权码批次「%s」(ID:%d)", batch.Name, id)
	approvalID, executed, err := SubmitApproval(c, "license_key_batch_delete", params, summary)
	if err != nil {
		Error(c, ErrorSystemError, "failed to submit approval")
		return
	}

	if !executed {
		PendingApproval(c, approvalID)
		return
	}
	SuccessEmpty(c)
}

// GET /app/license-key-batches/stats
func api_admin_license_key_batch_stats(c *gin.Context) {
	stats, err := GetAllBatchStats(c.Request.Context())
	if err != nil {
		log.Errorf(c, "failed to get batch stats: %v", err)
		Error(c, ErrorSystemError, "failed to get stats")
		return
	}
	Success(c, &stats)
}

// GET /app/license-key-batches/stats/by-source
func api_admin_license_key_batch_stats_by_source(c *gin.Context) {
	stats, err := GetBatchStatsBySource(c.Request.Context())
	if err != nil {
		log.Errorf(c, "failed to get stats by source: %v", err)
		Error(c, ErrorSystemError, "failed to get stats")
		return
	}
	Success(c, &stats)
}

// GET /app/license-key-batches/stats/trend
func api_admin_license_key_batch_stats_trend(c *gin.Context) {
	days := 30
	if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 {
		days = d
	}
	stats, err := GetBatchStatsTrend(c.Request.Context(), days)
	if err != nil {
		log.Errorf(c, "failed to get stats trend: %v", err)
		Error(c, ErrorSystemError, "failed to get trend")
		return
	}
	Success(c, &stats)
}

func toLicenseKeyBatchResponse(b *LicenseKeyBatch, redeemed, expired int64) LicenseKeyBatchResponse {
	return LicenseKeyBatchResponse{
		ID:               b.ID,
		Name:             b.Name,
		SourceTag:        b.SourceTag,
		RecipientMatcher: b.RecipientMatcher,
		PlanDays:         b.PlanDays,
		Quantity:         b.Quantity,
		ExpiresAt:        b.ExpiresAt,
		Note:             b.Note,
		CreatedByUserID:  b.CreatedByUserID,
		RedeemedCount:    redeemed,
		ExpiredCount:     expired,
		CreatedAt:        b.CreatedAt.Unix(),
	}
}

func toLicenseKeyItemResponse(k *LicenseKey) LicenseKeyItemResponse {
	resp := LicenseKeyItemResponse{
		ID:        k.ID,
		Code:      k.Code,
		PlanDays:  k.PlanDays,
		ExpiresAt: k.ExpiresAt,
		IsUsed:    k.IsUsed,
	}
	if k.UsedByUserID != nil {
		resp.UsedByUserID = k.UsedByUserID
	}
	if k.UsedAt != nil {
		usedAt := k.UsedAt.Unix()
		resp.UsedAt = &usedAt
	}
	return resp
}
```

- [ ] **Step 2: Add approval callbacks in `api/logic_approval_callbacks.go`**

Add after `executeApprovalCampaignIssueKeys` (around line 155):

```go
// ===================== License Key Batch =====================

func executeApprovalLicenseKeyBatchCreate(ctx context.Context, params json.RawMessage) error {
	var p struct {
		CreateLicenseKeyBatchRequest
		AdminUserID uint64 `json:"adminUserId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	_, err := CreateLicenseKeyBatch(ctx, &p.CreateLicenseKeyBatchRequest, p.AdminUserID)
	return err
}

func executeApprovalLicenseKeyBatchDelete(ctx context.Context, params json.RawMessage) error {
	var p struct {
		BatchID uint64 `json:"batchId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	// Delete unused keys in the batch
	if err := db.Get().Where("batch_id = ? AND is_used = false", p.BatchID).Delete(&LicenseKey{}).Error; err != nil {
		return fmt.Errorf("delete unused keys: %w", err)
	}

	// Soft-delete the batch
	if err := db.Get().Delete(&LicenseKeyBatch{}, p.BatchID).Error; err != nil {
		return fmt.Errorf("delete batch: %w", err)
	}

	return nil
}
```

- [ ] **Step 3: Register callbacks in `api/worker_integration.go`**

Add after line 83 (`campaign_issue_keys` registration):

```go
	RegisterApprovalCallback("license_key_batch_create", executeApprovalLicenseKeyBatchCreate)
	RegisterApprovalCallback("license_key_batch_delete", executeApprovalLicenseKeyBatchDelete)
```

- [ ] **Step 4: Add routes in `api/route.go`**

Add in the `opsAdmin` block, BEFORE the existing `/license-keys` routes. The `/stats` routes must come before `/:id` to avoid Gin treating "stats" as an `:id` parameter:

```go
		// 授权码批次管理
		opsAdmin.GET("/license-key-batches/stats",             RoleRequired(RoleMarketing), api_admin_license_key_batch_stats)
		opsAdmin.GET("/license-key-batches/stats/by-source",   RoleRequired(RoleMarketing), api_admin_license_key_batch_stats_by_source)
		opsAdmin.GET("/license-key-batches/stats/trend",       RoleRequired(RoleMarketing), api_admin_license_key_batch_stats_trend)
		opsAdmin.POST("/license-key-batches",                  RoleRequired(RoleMarketing), api_admin_create_license_key_batch)
		opsAdmin.GET("/license-key-batches",                   RoleRequired(RoleMarketing), api_admin_list_license_key_batches)
		opsAdmin.GET("/license-key-batches/:id",               RoleRequired(RoleMarketing), api_admin_get_license_key_batch)
		opsAdmin.GET("/license-key-batches/:id/keys",          RoleRequired(RoleMarketing), api_admin_list_license_key_batch_keys)
		opsAdmin.DELETE("/license-key-batches/:id",            RoleRequired(RoleMarketing), api_admin_delete_license_key_batch)
```

- [ ] **Step 5: Verify compilation**

Run: `cd api && go build ./...`

- [ ] **Step 6: Commit**

```bash
git add api/api_admin_license_key_batch.go api/logic_approval_callbacks.go api/worker_integration.go api/route.go
git commit -m "feat: add license key batch admin API handlers and routes"
```

---

### Task 5: Update Redemption Logic

**Files:**
- Modify: `api/logic_license_key.go`

- [ ] **Step 1: Update `MatchLicenseKey` to read from Batch**

Replace `MatchLicenseKey` (lines 92-101):

```go
// MatchLicenseKey checks whether user is eligible to redeem this key.
func MatchLicenseKey(key *LicenseKey, user *User) bool {
	matcher := key.RecipientMatcher
	if key.Batch != nil {
		matcher = key.Batch.RecipientMatcher
	}
	switch matcher {
	case "all":
		return true
	case "never_paid":
		return user.IsFirstOrderDone == nil || !*user.IsFirstOrderDone
	default:
		return false
	}
}
```

- [ ] **Step 2: Preload Batch in RedeemLicenseKey**

Change line 113 from:
```go
		if err := tx.Where("code = ?", code).First(&k).Error; err != nil {
```
to:
```go
		if err := tx.Preload("Batch").Where("code = ?", code).First(&k).Error; err != nil {
```

- [ ] **Step 3: Verify + Commit**

Run: `cd api && go build ./...`

```bash
git add api/logic_license_key.go
git commit -m "feat: redemption reads RecipientMatcher from batch"
```

---

### Task 6: Campaign + Old License Key Cleanup

**Files:**
- Modify: `api/api_admin_campaigns.go` — delete `api_admin_issue_license_keys`, update `convertCampaignToResponse`
- Modify: `api/api_admin_license_key.go` — simplify list handler, delete create/stats handlers
- Delete: `api/worker_license_key.go`
- Modify: `api/logic_license_key.go` — delete `GenerateLicenseKeysForCampaign`, `CountEligibleUsers`, `queryEligibleUsers`, `CreateManualLicenseKeys`, `licenseKeyTTLDays`
- Modify: `api/logic_approval_callbacks.go` — delete `executeApprovalCampaignIssueKeys`
- Modify: `api/worker_integration.go` — remove `campaign_issue_keys` registration
- Modify: `api/route.go` — remove old routes

- [ ] **Step 1: Delete `api_admin_issue_license_keys`** from `api/api_admin_campaigns.go` (lines 13-82 including section comment)

- [ ] **Step 2: Remove `IsShareable`/`SharesPerUser` from `convertCampaignToResponse`** (lines 497-498)

- [ ] **Step 3: Delete `api/worker_license_key.go`** entirely

- [ ] **Step 4: Delete campaign-coupled functions from `api/logic_license_key.go`**: `licenseKeyTTLDays` (line 18), `CreateManualLicenseKeys` (lines 56-84), `GenerateLicenseKeysForCampaign` (lines 230-279), `CountEligibleUsers` (lines 281-288), `queryEligibleUsers` (lines 290-319)

- [ ] **Step 5: Simplify `api_admin_list_license_keys`** — change `campaignId` filter to `batchId`, remove `source` filter. Delete `api_admin_license_key_stats` and `api_admin_create_license_keys`. Update `toLicenseKeyResponse`:

```go
func toLicenseKeyResponse(k *LicenseKey) LicenseKeyResponse {
	resp := LicenseKeyResponse{
		ID:        k.ID,
		UUID:      k.UUID,
		Code:      k.Code,
		BatchID:   k.BatchID,
		PlanDays:  k.PlanDays,
		ExpiresAt: k.ExpiresAt,
		IsUsed:    k.IsUsed,
		UsedByUserID: k.UsedByUserID,
		CreatedAt: k.CreatedAt.Unix(),
	}
	if k.UsedAt != nil {
		usedAt := k.UsedAt.Unix()
		resp.UsedAt = &usedAt
	}
	return resp
}
```

- [ ] **Step 6: Delete `executeApprovalCampaignIssueKeys`** from `api/logic_approval_callbacks.go` (lines 130-155)

- [ ] **Step 7: Remove `campaign_issue_keys` registration** from `api/worker_integration.go` (line 83)

- [ ] **Step 8: Update routes** in `api/route.go` — remove:
  - `opsAdmin.POST("/campaigns/:id/issue-keys", ...)`
  - `opsAdmin.GET("/license-keys/stats", ...)`
  - `opsAdmin.POST("/license-keys", ...)`

- [ ] **Step 9: Verify + Commit**

Run: `cd api && go build ./...`

```bash
git add -A api/
git commit -m "refactor: remove campaign-license key coupling, clean shareable fields"
```

---

### Task 7: MCP Tools

**Files:**
- Modify: `tools/kaitu-center/src/tools/admin-license-keys.ts` — rewrite with batch tools
- Modify: `tools/kaitu-center/src/tools/admin-campaigns.ts` — remove `issue_campaign_keys`

- [ ] **Step 1: Rewrite `tools/kaitu-center/src/tools/admin-license-keys.ts`**

```typescript
/**
 * Admin license key batch management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const licenseKeyTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_license_key_batches',
    description: 'List license key batches with pagination.',
    group: 'license_keys',
    params: {
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
      source_tag: z.string().optional().describe('Filter by source tag'),
    },
    path: '/app/license-key-batches',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      if (p.source_tag !== undefined) q.sourceTag = String(p.source_tag)
      return q
    },
  }),

  defineApiTool({
    name: 'get_license_key_batch',
    description: 'Get license key batch detail with conversion stats.',
    group: 'license_keys',
    params: {
      batch_id: z.number().describe('Batch ID'),
    },
    path: (p) => `/app/license-key-batches/${p.batch_id}`,
  }),

  defineApiTool({
    name: 'create_license_key_batch',
    description: 'Create a license key batch (requires approval). Generates keys upon approval.',
    group: 'license_keys.write',
    method: 'POST',
    params: {
      name: z.string().describe('Batch name'),
      source_tag: z.string().optional().describe('Channel tag (twitter, kol-xxx, winback)'),
      recipient_matcher: z.enum(['all', 'never_paid']).describe('Who can redeem'),
      plan_days: z.number().describe('Membership days per key'),
      quantity: z.number().describe('Number of keys (1-10000)'),
      expires_in_days: z.number().describe('Key expiration in days'),
      note: z.string().optional().describe('Note'),
    },
    path: '/app/license-key-batches',
    mapBody: (p) => ({
      name: p.name,
      sourceTag: p.source_tag || '',
      recipientMatcher: p.recipient_matcher,
      planDays: p.plan_days,
      quantity: p.quantity,
      expiresInDays: p.expires_in_days,
      note: p.note || '',
    }),
  }),

  defineApiTool({
    name: 'list_license_key_batch_keys',
    description: 'List keys in a batch with status filter and pagination.',
    group: 'license_keys',
    params: {
      batch_id: z.number().describe('Batch ID'),
      status: z.enum(['all', 'used', 'unused', 'expired']).optional().describe('Filter by status'),
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
    },
    path: (p) => `/app/license-key-batches/${p.batch_id}/keys`,
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.status !== undefined) q.status = String(p.status)
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      return q
    },
  }),

  defineApiTool({
    name: 'license_key_batch_stats',
    description: 'Get license key batch stats with conversion rate. Omit batch_id for all batches.',
    group: 'license_keys',
    params: {
      batch_id: z.number().optional().describe('Batch ID (omit for all)'),
    },
    path: (p) => p.batch_id ? `/app/license-key-batches/${p.batch_id}` : '/app/license-key-batches/stats',
  }),

  defineApiTool({
    name: 'license_key_batch_stats_by_source',
    description: 'Get license key stats aggregated by source tag (channel).',
    group: 'license_keys',
    path: '/app/license-key-batches/stats/by-source',
  }),

  defineApiTool({
    name: 'delete_license_key',
    description: 'Delete a single license key by ID.',
    group: 'license_keys.write',
    method: 'DELETE',
    params: {
      id: z.number().describe('License key ID'),
    },
    path: (p) => `/app/license-keys/${p.id}`,
  }),

  defineApiTool({
    name: 'list_license_keys',
    description: 'List all license keys (filter by batchId).',
    group: 'license_keys',
    params: {
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
      batch_id: z.number().optional().describe('Filter by batch ID'),
      is_used: z.boolean().optional().describe('Filter by used status'),
    },
    path: '/app/license-keys',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      if (p.batch_id !== undefined) q.batchId = String(p.batch_id)
      if (p.is_used !== undefined) q.isUsed = String(p.is_used)
      return q
    },
  }),
]
```

- [ ] **Step 2: Remove `issue_campaign_keys`** from `tools/kaitu-center/src/tools/admin-campaigns.ts` (lines 113-123)

- [ ] **Step 3: Build + Commit**

Run: `cd tools/kaitu-center && npm run build`

```bash
git add tools/kaitu-center/src/tools/admin-license-keys.ts tools/kaitu-center/src/tools/admin-campaigns.ts
git commit -m "feat: update MCP tools — batch-based license key management"
```

---

### Task 8: Web API Types + Methods

**Files:**
- Modify: `web/src/lib/api.ts`

- [ ] **Step 1: Replace LicenseKey types section** (lines 2323-2380) with:

```typescript
// ============================================================
// LicenseKey Batch types
// ============================================================

export interface LicenseKeyBatch {
  id: number;
  name: string;
  sourceTag: string;
  recipientMatcher: string;
  planDays: number;
  quantity: number;
  expiresAt: number;
  note: string;
  createdByUserId: number;
  redeemedCount: number;
  expiredCount: number;
  createdAt: number;
}

export interface LicenseKeyBatchDetail extends LicenseKeyBatch {
  convertedUsers: number;
  conversionRate: number;
  revenue: number;
}

export interface CreateLicenseKeyBatchRequest {
  name: string;
  sourceTag: string;
  recipientMatcher: string;
  planDays: number;
  quantity: number;
  expiresInDays: number;
  note?: string;
}

export interface BatchStats {
  batchId: number;
  name: string;
  sourceTag: string;
  totalKeys: number;
  redeemed: number;
  expired: number;
  redeemRate: number;
  convertedUsers: number;
  conversionRate: number;
  revenue: number;
}

export interface BatchStatsBySource {
  sourceTag: string;
  totalKeys: number;
  redeemed: number;
  redeemRate: number;
  convertedUsers: number;
  conversionRate: number;
  revenue: number;
}

export interface LicenseKeyItem {
  id: number;
  code: string;
  planDays: number;
  expiresAt: number;
  isUsed: boolean;
  usedByUserId?: number;
  usedAt?: number;
}

export interface LicenseKeyPublic {
  code: string;
  planDays: number;
  expiresAt: number;
  isUsed: boolean;
  isExpired: boolean;
  senderName: string;
}

export interface LicenseKeyAdmin {
  id: number;
  uuid: string;
  code: string;
  batchId: number;
  planDays: number;
  expiresAt: number;
  isUsed: boolean;
  usedByUserId?: number;
  usedAt?: number;
  createdAt: number;
}
```

- [ ] **Step 2: Replace license key API methods** (around lines 2028-2078) with:

```typescript
  // License Key Batch APIs
  async listLicenseKeyBatches(params: { page?: number; pageSize?: number; sourceTag?: string } = {}): Promise<{ items: LicenseKeyBatch[]; total: number }> {
    const q = new URLSearchParams();
    if (params.page) q.set('page', String(params.page));
    if (params.pageSize) q.set('pageSize', String(params.pageSize));
    if (params.sourceTag) q.set('sourceTag', params.sourceTag);
    const qs = q.toString();
    return this.request<{ items: LicenseKeyBatch[]; total: number }>(`/app/license-key-batches${qs ? '?' + qs : ''}`);
  },

  async getLicenseKeyBatch(id: number): Promise<LicenseKeyBatchDetail> {
    return this.request<LicenseKeyBatchDetail>(`/app/license-key-batches/${id}`);
  },

  async createLicenseKeyBatch(req: CreateLicenseKeyBatchRequest): Promise<void> {
    return this.request<void>('/app/license-key-batches', { method: 'POST', body: JSON.stringify(req) });
  },

  async deleteLicenseKeyBatch(id: number): Promise<void> {
    return this.request<void>(`/app/license-key-batches/${id}`, { method: 'DELETE' });
  },

  async listLicenseKeyBatchKeys(batchId: number, params: { status?: string; page?: number; pageSize?: number } = {}): Promise<{ items: LicenseKeyItem[]; total: number }> {
    const q = new URLSearchParams();
    if (params.status) q.set('status', params.status);
    if (params.page) q.set('page', String(params.page));
    if (params.pageSize) q.set('pageSize', String(params.pageSize));
    const qs = q.toString();
    return this.request<{ items: LicenseKeyItem[]; total: number }>(`/app/license-key-batches/${batchId}/keys${qs ? '?' + qs : ''}`);
  },

  async getLicenseKeyBatchStats(): Promise<BatchStats[]> {
    return this.request<BatchStats[]>('/app/license-key-batches/stats');
  },

  async getLicenseKeyBatchStatsBySource(): Promise<BatchStatsBySource[]> {
    return this.request<BatchStatsBySource[]>('/app/license-key-batches/stats/by-source');
  },

  async getLicenseKey(code: string): Promise<LicenseKeyPublic> {
    return this.request<LicenseKeyPublic>(`/api/license-keys/code/${code}`);
  },

  async listAdminLicenseKeys(params: { batchId?: number; isUsed?: boolean; page?: number; pageSize?: number } = {}): Promise<{ items: LicenseKeyAdmin[]; total: number }> {
    const q = new URLSearchParams();
    if (params.batchId) q.set('batchId', String(params.batchId));
    if (params.isUsed !== undefined) q.set('isUsed', String(params.isUsed));
    if (params.page) q.set('page', String(params.page));
    if (params.pageSize) q.set('pageSize', String(params.pageSize));
    const qs = q.toString();
    return this.request<{ items: LicenseKeyAdmin[]; total: number }>(`/app/license-keys${qs ? '?' + qs : ''}`);
  },

  async deleteAdminLicenseKey(id: number): Promise<void> {
    return this.request<void>(`/app/license-keys/${id}`, { method: 'DELETE' });
  },

  async redeemLicenseKey(code: string): Promise<{ planDays: number; newExpireAt: number; historyId: number }> {
    return this.request<{ planDays: number; newExpireAt: number; historyId: number }>(`/api/license-keys/code/${code}/redeem`, { method: 'POST' });
  },
```

- [ ] **Step 3: Delete old types**: `IssueKeysRequest`, `IssueKeysResponse`, `CreateLicenseKeysRequest`, `CreateLicenseKeysResponse`, `LicenseKeyStatsRow`

- [ ] **Step 4: Commit**

```bash
git add web/src/lib/api.ts
git commit -m "feat: add license key batch API types and methods"
```

---

### Task 9: Campaign Page Cleanup

**Files:**
- Modify: `web/src/app/(manager)/manager/campaigns/page.tsx`

- [ ] **Step 1:** Remove `isShareable` and `sharesPerUser` from all form state defaults, reset, and edit populate
- [ ] **Step 2:** Remove the isShareable Switch + sharesPerUser Input UI block
- [ ] **Step 3:** Remove the "Issue keys section" in edit dialog
- [ ] **Step 4:** Remove the isShareable table column
- [ ] **Step 5:** Remove unused `Key` import from lucide-react if no longer used
- [ ] **Step 6: Verify + Commit**

Run: `cd web && npx tsc --noEmit`

```bash
git add web/src/app/(manager)/manager/campaigns/page.tsx
git commit -m "refactor: remove license key issuance UI from campaigns page"
```

---

### Task 10: License Key Batches Admin Page + Simplify License Keys Page

**Files:**
- Create: `web/src/app/(manager)/manager/license-key-batches/page.tsx`
- Modify: `web/src/app/(manager)/manager/license-keys/page.tsx`
- Modify: `web/src/components/manager-sidebar.tsx`

- [ ] **Step 1: Create `web/src/app/(manager)/manager/license-key-batches/page.tsx`**

Following the exact pattern of the existing license-keys page (tanstack table, shadcn dialogs, sonner toasts):

```tsx
"use client";

import { useState, useEffect, useCallback } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ColumnDef,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import {
  api,
  LicenseKeyBatch,
  LicenseKeyBatchDetail,
  LicenseKeyItem,
  CreateLicenseKeyBatchRequest,
  BatchStats,
} from "@/lib/api";
import { toast } from "sonner";
import { Plus, Trash2, Copy, Eye } from "lucide-react";

function formatDate(ts: number) {
  if (!ts) return "-";
  return new Date(ts * 1000).toLocaleString("zh-CN");
}

function pct(n: number) {
  return (n * 100).toFixed(1) + "%";
}

export default function LicenseKeyBatchesPage() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const [batches, setBatches] = useState<LicenseKeyBatch[]>([]);
  const [total, setTotal] = useState(0);
  const [pageCount, setPageCount] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [stats, setStats] = useState<BatchStats[]>([]);

  // Create dialog
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState<CreateLicenseKeyBatchRequest>({
    name: "", sourceTag: "", recipientMatcher: "all", planDays: 30, quantity: 100, expiresInDays: 30,
  });
  const [isCreating, setIsCreating] = useState(false);

  // Detail dialog
  const [detailOpen, setDetailOpen] = useState(false);
  const [detail, setDetail] = useState<LicenseKeyBatchDetail | null>(null);
  const [detailKeys, setDetailKeys] = useState<LicenseKeyItem[]>([]);
  const [detailKeysTotal, setDetailKeysTotal] = useState(0);
  const [detailKeyStatus, setDetailKeyStatus] = useState("all");
  const [detailKeyPage, setDetailKeyPage] = useState(1);

  // Delete dialog
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deletingId, setDeletingId] = useState<number | null>(null);

  const page = parseInt(searchParams.get("page") || "1", 10);
  const pageSize = parseInt(searchParams.get("pageSize") || "20", 10);

  const columns: ColumnDef<LicenseKeyBatch>[] = [
    { accessorKey: "name", header: "批次名称" },
    { accessorKey: "sourceTag", header: "渠道", cell: ({ row }) => row.original.sourceTag || "-" },
    {
      accessorKey: "quantity", header: "数量",
      cell: ({ row }) => {
        const b = row.original;
        return <span>{b.redeemedCount}/{b.quantity}</span>;
      },
    },
    { accessorKey: "planDays", header: "天数", cell: ({ row }) => `${row.original.planDays} 天` },
    {
      accessorKey: "recipientMatcher", header: "限制",
      cell: ({ row }) => row.original.recipientMatcher === "never_paid"
        ? <Badge variant="secondary">未付费用户</Badge>
        : <Badge variant="default">所有用户</Badge>,
    },
    { accessorKey: "expiresAt", header: "过期时间", cell: ({ row }) => formatDate(row.original.expiresAt) },
    { accessorKey: "createdAt", header: "创建时间", cell: ({ row }) => formatDate(row.original.createdAt) },
    {
      id: "actions", header: "操作",
      cell: ({ row }) => (
        <div className="flex gap-1">
          <Button variant="ghost" size="sm" onClick={() => openDetail(row.original.id)}>
            <Eye className="h-4 w-4" />
          </Button>
          <Button variant="ghost" size="sm" onClick={() => { setDeletingId(row.original.id); setDeleteOpen(true); }}>
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      ),
    },
  ];

  const table = useReactTable({ data: batches, columns, pageCount, getCoreRowModel: getCoreRowModel(), manualPagination: true });

  const fetchBatches = useCallback(async () => {
    setIsLoading(true);
    try {
      const res = await api.listLicenseKeyBatches({ page, pageSize });
      setBatches(res.items || []);
      setTotal(res.total);
      setPageCount(Math.ceil(res.total / pageSize));
    } catch { toast.error("获取批次列表失败"); }
    finally { setIsLoading(false); }
  }, [page, pageSize]);

  const fetchStats = useCallback(async () => {
    try { setStats(await api.getLicenseKeyBatchStats()); } catch {}
  }, []);

  useEffect(() => { fetchBatches(); }, [fetchBatches]);
  useEffect(() => { fetchStats(); }, [fetchStats]);

  const handleCreate = async () => {
    setIsCreating(true);
    try {
      await api.createLicenseKeyBatch(createForm);
      setCreateOpen(false);
      toast.success("批次创建已提交（等待审批）");
      fetchBatches();
      fetchStats();
    } catch { toast.error("创建批次失败"); }
    finally { setIsCreating(false); }
  };

  const handleDelete = async () => {
    if (!deletingId) return;
    try {
      await api.deleteLicenseKeyBatch(deletingId);
      toast.success("删除已提交");
      setDeleteOpen(false);
      fetchBatches();
      fetchStats();
    } catch { toast.error("删除失败"); }
  };

  const openDetail = async (id: number) => {
    try {
      const d = await api.getLicenseKeyBatch(id);
      setDetail(d);
      setDetailKeyPage(1);
      setDetailKeyStatus("all");
      const keys = await api.listLicenseKeyBatchKeys(id, { page: 1, pageSize: 50 });
      setDetailKeys(keys.items || []);
      setDetailKeysTotal(keys.total);
      setDetailOpen(true);
    } catch { toast.error("获取详情失败"); }
  };

  const fetchDetailKeys = async () => {
    if (!detail) return;
    try {
      const keys = await api.listLicenseKeyBatchKeys(detail.id, { status: detailKeyStatus === "all" ? undefined : detailKeyStatus, page: detailKeyPage, pageSize: 50 });
      setDetailKeys(keys.items || []);
      setDetailKeysTotal(keys.total);
    } catch {}
  };

  useEffect(() => { if (detailOpen) fetchDetailKeys(); }, [detailKeyStatus, detailKeyPage]);

  // Aggregate stats
  const totalKeys = stats.reduce((s, r) => s + r.totalKeys, 0);
  const totalRedeemed = stats.reduce((s, r) => s + r.redeemed, 0);
  const totalConverted = stats.reduce((s, r) => s + r.convertedUsers, 0);
  const overallRedeemRate = totalKeys > 0 ? totalRedeemed / totalKeys : 0;
  const overallConvRate = totalRedeemed > 0 ? totalConverted / totalRedeemed : 0;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">授权码批次</h1>
          <p className="text-muted-foreground">管理授权码批次、查看转化统计</p>
        </div>
        <Button onClick={() => setCreateOpen(true)}><Plus className="h-4 w-4 mr-2" />创建批次</Button>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <Card><CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-muted-foreground">总 Keys</CardTitle></CardHeader><CardContent><div className="text-2xl font-bold">{totalKeys}</div></CardContent></Card>
        <Card><CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-muted-foreground">已兑换</CardTitle></CardHeader><CardContent><div className="text-2xl font-bold">{totalRedeemed}</div></CardContent></Card>
        <Card><CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-muted-foreground">兑换率</CardTitle></CardHeader><CardContent><div className="text-2xl font-bold">{pct(overallRedeemRate)}</div></CardContent></Card>
        <Card><CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-muted-foreground">兑换→付费转化</CardTitle></CardHeader><CardContent><div className="text-2xl font-bold">{pct(overallConvRate)}</div><p className="text-xs text-muted-foreground">{totalConverted} 人</p></CardContent></Card>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>{table.getHeaderGroups().map(hg => (<TableRow key={hg.id}>{hg.headers.map(h => (<TableHead key={h.id}>{h.isPlaceholder ? null : flexRender(h.column.columnDef.header, h.getContext())}</TableHead>))}</TableRow>))}</TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow><TableCell colSpan={columns.length} className="h-24 text-center"><div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary mx-auto" /></TableCell></TableRow>
            ) : table.getRowModel().rows?.length ? (
              table.getRowModel().rows.map(row => (<TableRow key={row.id}>{row.getVisibleCells().map(cell => (<TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>))}</TableRow>))
            ) : (
              <TableRow><TableCell colSpan={columns.length} className="h-24 text-center">无数据</TableCell></TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <div className="flex items-center justify-end space-x-2 py-4">
        <span className="text-sm text-muted-foreground">总计: {total}</span>
        <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => router.push(`/manager/license-key-batches?page=${page - 1}&pageSize=${pageSize}`)}>上一页</Button>
        <Button variant="outline" size="sm" disabled={page >= pageCount} onClick={() => router.push(`/manager/license-key-batches?page=${page + 1}&pageSize=${pageSize}`)}>下一页</Button>
      </div>

      {/* Create dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader><DialogTitle>创建授权码批次</DialogTitle><DialogDescription>创建后需审批，审批通过自动生成授权码</DialogDescription></DialogHeader>
          <div className="space-y-4 py-2">
            <div><label className="text-sm font-medium block mb-1">批次名称</label><Input value={createForm.name} onChange={e => setCreateForm(f => ({ ...f, name: e.target.value }))} placeholder="Apr Twitter 投放" /></div>
            <div><label className="text-sm font-medium block mb-1">渠道标签</label><Input value={createForm.sourceTag} onChange={e => setCreateForm(f => ({ ...f, sourceTag: e.target.value }))} placeholder="twitter / kol-xxx / winback" /></div>
            <div className="grid grid-cols-2 gap-4">
              <div><label className="text-sm font-medium block mb-1">数量 (1-10000)</label><Input type="number" min={1} max={10000} value={createForm.quantity} onChange={e => setCreateForm(f => ({ ...f, quantity: parseInt(e.target.value) || 100 }))} /></div>
              <div><label className="text-sm font-medium block mb-1">天数</label><Input type="number" min={1} value={createForm.planDays} onChange={e => setCreateForm(f => ({ ...f, planDays: parseInt(e.target.value) || 30 }))} /></div>
            </div>
            <div><label className="text-sm font-medium block mb-1">有效期（天）</label><Input type="number" min={1} value={createForm.expiresInDays} onChange={e => setCreateForm(f => ({ ...f, expiresInDays: parseInt(e.target.value) || 30 }))} /></div>
            <div><label className="text-sm font-medium block mb-1">使用条件</label><select className="w-full p-2 border border-border bg-background text-foreground rounded-md" value={createForm.recipientMatcher} onChange={e => setCreateForm(f => ({ ...f, recipientMatcher: e.target.value }))}><option value="all">所有用户</option><option value="never_paid">未付费用户</option></select></div>
            <div><label className="text-sm font-medium block mb-1">备注</label><Input value={createForm.note || ""} onChange={e => setCreateForm(f => ({ ...f, note: e.target.value }))} placeholder="可选" /></div>
          </div>
          <DialogFooter><Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button><Button onClick={handleCreate} disabled={isCreating || !createForm.name}>{isCreating ? "提交中..." : "提交审批"}</Button></DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Detail dialog */}
      <Dialog open={detailOpen} onOpenChange={setDetailOpen}>
        <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
          {detail && (<>
            <DialogHeader><DialogTitle>{detail.name}</DialogTitle><DialogDescription>渠道: {detail.sourceTag || "-"} · {detail.quantity} 个 · {detail.planDays} 天</DialogDescription></DialogHeader>
            <div className="grid grid-cols-3 gap-3 py-2">
              <Card><CardContent className="pt-4"><div className="text-sm text-muted-foreground">兑换率</div><div className="text-xl font-bold">{pct(detail.redeemedCount / (detail.quantity || 1))}</div><div className="text-xs text-muted-foreground">{detail.redeemedCount}/{detail.quantity}</div></CardContent></Card>
              <Card><CardContent className="pt-4"><div className="text-sm text-muted-foreground">转化率</div><div className="text-xl font-bold">{pct(detail.conversionRate)}</div><div className="text-xs text-muted-foreground">{detail.convertedUsers} 人付费</div></CardContent></Card>
              <Card><CardContent className="pt-4"><div className="text-sm text-muted-foreground">收入</div><div className="text-xl font-bold">¥{(detail.revenue / 100).toFixed(2)}</div></CardContent></Card>
            </div>
            <div className="flex items-center gap-2 py-2">
              <select className="p-1 border border-border bg-background text-foreground rounded text-sm" value={detailKeyStatus} onChange={e => { setDetailKeyStatus(e.target.value); setDetailKeyPage(1); }}>
                <option value="all">全部</option><option value="used">已使用</option><option value="unused">未使用</option><option value="expired">已过期</option>
              </select>
              <span className="text-sm text-muted-foreground">共 {detailKeysTotal} 条</span>
            </div>
            <div className="rounded-md border max-h-64 overflow-y-auto">
              <Table>
                <TableHeader><TableRow><TableHead>授权码</TableHead><TableHead>状态</TableHead><TableHead>过期</TableHead><TableHead>使用者</TableHead></TableRow></TableHeader>
                <TableBody>
                  {detailKeys.map(k => (
                    <TableRow key={k.id}>
                      <TableCell><div className="flex items-center gap-1"><code className="text-xs bg-muted px-1 py-0.5 rounded font-mono">{k.code}</code><Button variant="ghost" size="sm" className="h-5 w-5 p-0" onClick={() => { navigator.clipboard.writeText(k.code); toast.success("已复制"); }}><Copy className="h-3 w-3" /></Button></div></TableCell>
                      <TableCell>{k.isUsed ? <Badge variant="secondary">已使用</Badge> : k.expiresAt < Date.now()/1000 ? <Badge variant="destructive">已过期</Badge> : <Badge>未使用</Badge>}</TableCell>
                      <TableCell className="text-xs">{formatDate(k.expiresAt)}</TableCell>
                      <TableCell className="text-xs">{k.usedByUserId || "-"}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            {detailKeysTotal > 50 && (
              <div className="flex justify-end gap-2 pt-2">
                <Button variant="outline" size="sm" disabled={detailKeyPage <= 1} onClick={() => setDetailKeyPage(p => p - 1)}>上一页</Button>
                <Button variant="outline" size="sm" disabled={detailKeyPage * 50 >= detailKeysTotal} onClick={() => setDetailKeyPage(p => p + 1)}>下一页</Button>
              </div>
            )}
          </>)}
        </DialogContent>
      </Dialog>

      {/* Delete dialog */}
      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader><DialogTitle>确认删除</DialogTitle><DialogDescription>将删除批次及其未使用的授权码。已使用的授权码保留。</DialogDescription></DialogHeader>
          <DialogFooter><Button variant="outline" onClick={() => setDeleteOpen(false)}>取消</Button><Button variant="destructive" onClick={handleDelete}>删除</Button></DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
```

- [ ] **Step 2: Simplify `web/src/app/(manager)/manager/license-keys/page.tsx`**

Replace the entire file. The simplified page only has: list with batchId filter + isUsed filter, delete, no create, no stats:

```tsx
"use client";

import { useState, useEffect, useCallback } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ColumnDef, flexRender, getCoreRowModel, useReactTable,
} from "@tanstack/react-table";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { api, LicenseKeyAdmin } from "@/lib/api";
import { toast } from "sonner";
import { Trash2, Copy } from "lucide-react";

function formatDate(ts: number) {
  if (!ts) return "-";
  return new Date(ts * 1000).toLocaleString("zh-CN");
}

function getStatus(key: LicenseKeyAdmin): { label: string; variant: "default" | "secondary" | "destructive" } {
  if (key.isUsed) return { label: "已使用", variant: "secondary" };
  if (key.expiresAt < Date.now() / 1000) return { label: "已过期", variant: "destructive" };
  return { label: "未使用", variant: "default" };
}

export default function LicenseKeysPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [data, setData] = useState<LicenseKeyAdmin[]>([]);
  const [total, setTotal] = useState(0);
  const [pageCount, setPageCount] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deletingId, setDeletingId] = useState<number | null>(null);

  const page = parseInt(searchParams.get("page") || "1", 10);
  const pageSize = parseInt(searchParams.get("pageSize") || "50", 10);
  const batchIdParam = searchParams.get("batchId") || "";
  const isUsedParam = searchParams.get("isUsed") || "";

  const [localBatchId, setLocalBatchId] = useState(batchIdParam);
  const [localIsUsed, setLocalIsUsed] = useState(isUsedParam);

  const columns: ColumnDef<LicenseKeyAdmin>[] = [
    {
      accessorKey: "code", header: "授权码",
      cell: ({ row }) => (
        <div className="flex items-center gap-1">
          <code className="text-xs bg-muted px-1 py-0.5 rounded font-mono">{row.original.code}</code>
          <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={() => { navigator.clipboard.writeText(row.original.code); toast.success("已复制"); }}><Copy className="h-3 w-3" /></Button>
        </div>
      ),
    },
    { accessorKey: "batchId", header: "批次ID", cell: ({ row }) => <code className="text-xs bg-muted px-1 py-0.5 rounded">{row.original.batchId}</code> },
    { accessorKey: "planDays", header: "天数", cell: ({ row }) => `${row.original.planDays} 天` },
    { header: "状态", cell: ({ row }) => { const s = getStatus(row.original); return <Badge variant={s.variant}>{s.label}</Badge>; } },
    { accessorKey: "expiresAt", header: "过期时间", cell: ({ row }) => formatDate(row.original.expiresAt) },
    { accessorKey: "createdAt", header: "创建时间", cell: ({ row }) => formatDate(row.original.createdAt) },
    {
      id: "actions", header: "操作",
      cell: ({ row }) => (<Button variant="ghost" size="sm" onClick={() => { setDeletingId(row.original.id); setDeleteOpen(true); }}><Trash2 className="h-4 w-4" /></Button>),
    },
  ];

  const table = useReactTable({ data, columns, pageCount, getCoreRowModel: getCoreRowModel(), manualPagination: true });

  const fetchData = useCallback(async () => {
    setIsLoading(true);
    try {
      const params: { page: number; pageSize: number; batchId?: number; isUsed?: boolean } = { page, pageSize };
      if (batchIdParam) params.batchId = parseInt(batchIdParam, 10);
      if (isUsedParam !== "") params.isUsed = isUsedParam === "true";
      const res = await api.listAdminLicenseKeys(params);
      setData(res.items || []);
      setTotal(res.total);
      setPageCount(Math.ceil(res.total / pageSize));
    } catch { toast.error("获取授权码列表失败"); }
    finally { setIsLoading(false); }
  }, [page, pageSize, batchIdParam, isUsedParam]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const handleFilter = () => {
    const p = new URLSearchParams();
    p.set("page", "1"); p.set("pageSize", pageSize.toString());
    if (localBatchId.trim()) p.set("batchId", localBatchId.trim());
    if (localIsUsed && localIsUsed !== "all") p.set("isUsed", localIsUsed);
    router.push(`/manager/license-keys?${p.toString()}`);
  };

  const handleDelete = async () => {
    if (!deletingId) return;
    try { await api.deleteAdminLicenseKey(deletingId); toast.success("已删除"); setDeleteOpen(false); fetchData(); }
    catch { toast.error("删除失败"); }
  };

  return (
    <div className="space-y-6">
      <div><h1 className="text-3xl font-bold">授权码列表</h1><p className="text-muted-foreground">查看所有授权码。批次管理请到「授权码批次」页面。</p></div>

      <div className="flex items-end gap-4 p-4 bg-muted/50 rounded-lg">
        <div className="flex-1"><label className="text-sm font-medium">批次ID</label><Input placeholder="输入批次ID" value={localBatchId} onChange={e => setLocalBatchId(e.target.value)} /></div>
        <div className="flex-1"><label className="text-sm font-medium">状态</label><select className="w-full p-2 border border-border bg-muted text-foreground rounded-md" value={localIsUsed} onChange={e => setLocalIsUsed(e.target.value)}><option value="">全部</option><option value="true">已使用</option><option value="false">未使用</option></select></div>
        <div className="flex gap-2"><Button onClick={handleFilter}>筛选</Button><Button variant="outline" onClick={() => { setLocalBatchId(""); setLocalIsUsed(""); router.push("/manager/license-keys"); }}>重置</Button></div>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>{table.getHeaderGroups().map(hg => (<TableRow key={hg.id}>{hg.headers.map(h => (<TableHead key={h.id}>{h.isPlaceholder ? null : flexRender(h.column.columnDef.header, h.getContext())}</TableHead>))}</TableRow>))}</TableHeader>
          <TableBody>
            {isLoading ? (<TableRow><TableCell colSpan={columns.length} className="h-24 text-center"><div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary mx-auto" /></TableCell></TableRow>)
            : table.getRowModel().rows?.length ? table.getRowModel().rows.map(row => (<TableRow key={row.id}>{row.getVisibleCells().map(cell => (<TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>))}</TableRow>))
            : (<TableRow><TableCell colSpan={columns.length} className="h-24 text-center">无数据</TableCell></TableRow>)}
          </TableBody>
        </Table>
      </div>

      <div className="flex items-center justify-end space-x-2 py-4">
        <span className="text-sm text-muted-foreground">总计: {total}</span>
        <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => { const p = new URLSearchParams(searchParams.toString()); p.set("page", String(page - 1)); router.push(`/manager/license-keys?${p}`); }}>上一页</Button>
        <Button variant="outline" size="sm" disabled={page >= pageCount} onClick={() => { const p = new URLSearchParams(searchParams.toString()); p.set("page", String(page + 1)); router.push(`/manager/license-keys?${p}`); }}>下一页</Button>
      </div>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent><DialogHeader><DialogTitle>确认删除</DialogTitle><DialogDescription>确定要删除这个授权码吗？</DialogDescription></DialogHeader><DialogFooter><Button variant="outline" onClick={() => setDeleteOpen(false)}>取消</Button><Button variant="destructive" onClick={handleDelete}>删除</Button></DialogFooter></DialogContent>
      </Dialog>
    </div>
  );
}
```

- [ ] **Step 3: Add nav entry in `web/src/components/manager-sidebar.tsx`**

Find the "运营配置" section (around line 42) and add the batch page:

```tsx
      { href: "/manager/license-key-batches", icon: Package, label: "授权码批次" },
```

Add after the "授权码" entry. Import `Package` from lucide-react if not already imported.

- [ ] **Step 4: Verify + Commit**

Run: `cd web && npx tsc --noEmit`

```bash
git add web/src/app/(manager)/manager/license-key-batches/ web/src/app/(manager)/manager/license-keys/page.tsx web/src/components/manager-sidebar.tsx
git commit -m "feat: add license key batches page, simplify license keys page"
```

---

### Task 11: Verification

- [ ] **Step 1: Run Go build**

Run: `cd api && go build ./...`

- [ ] **Step 2: Run Go tests**

Run: `cd api && go test ./...`

- [ ] **Step 3: Run MCP build**

Run: `cd tools/kaitu-center && npm run build`

- [ ] **Step 4: Run web build**

Run: `cd web && yarn build`

- [ ] **Step 5: Fix any failures and commit**

```bash
git add -A
git commit -m "fix: verification fixes for license key batch decoupling"
```
