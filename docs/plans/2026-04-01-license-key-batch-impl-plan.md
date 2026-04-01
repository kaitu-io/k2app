# License Key Batch Decoupling — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Decouple license key batch operations from campaigns — introduce `LicenseKeyBatch` as standalone module, clean campaign model, add conversion tracking stats.

**Architecture:** New `license_key_batches` table + GORM model. New admin API handlers in `api_admin_license_key_batch.go`. Modify existing `LicenseKey` model to use `batch_id` instead of `campaign_id`. Clean campaign of shareable fields. Update MCP tools and web admin pages.

**Tech Stack:** Go (Gin, GORM), TypeScript (Next.js, shadcn/ui), MCP tools (Node.js, zod)

**Spec:** `docs/plans/2026-04-01-license-key-batch-decoupling.md`

---

### Task 1: Add LicenseKeyBatch Model + Migration

**Files:**
- Modify: `api/model.go` (add `LicenseKeyBatch` struct, modify `LicenseKey` struct, modify `Campaign` struct)
- Modify: `api/migrate.go` (add `&LicenseKeyBatch{}`)

- [ ] **Step 1: Add LicenseKeyBatch model to `api/model.go`**

Insert before the existing `LicenseKey` struct (around line 1084):

```go
// LicenseKeyBatch 授权码批次（独立于活动码的分发单位）
type LicenseKeyBatch struct {
	ID        uint64         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	Name             string `gorm:"type:varchar(255);not null" json:"name"`
	SourceTag        string `gorm:"type:varchar(100);not null;default:'';index" json:"sourceTag"`
	RecipientMatcher string `gorm:"type:varchar(50);not null;default:'all'" json:"recipientMatcher"` // "all" or "never_paid"
	PlanDays         int    `gorm:"not null" json:"planDays"`
	Quantity         int    `gorm:"not null" json:"quantity"`
	ExpiresAt        int64  `gorm:"not null" json:"expiresAt"`
	Note             string `gorm:"type:text" json:"note"`
	CreatedByUserID  uint64 `gorm:"not null" json:"createdByUserId"`
}

func (LicenseKeyBatch) TableName() string { return "license_key_batches" }
```

- [ ] **Step 2: Modify LicenseKey model — add BatchID, Batch relation; keep old fields for migration**

In `api/model.go`, add `BatchID` and `Batch` to the existing `LicenseKey` struct. Keep old fields (they'll be removed after data migration in Task 7):

```go
// LicenseKey 一次性授权码
type LicenseKey struct {
	ID        uint64         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	UUID string `gorm:"type:varchar(50);uniqueIndex;not null" json:"uuid"`
	Code string `gorm:"type:varchar(8);uniqueIndex;not null" json:"code"`

	// New: batch association
	BatchID uint64           `gorm:"not null;default:0;index" json:"batchId"`
	Batch   *LicenseKeyBatch `gorm:"foreignKey:BatchID" json:"-"`

	// Legacy fields — kept until migration completes (Task 7)
	Source           string  `gorm:"type:varchar(16);not null;default:'campaign'" json:"source"`
	Note             string  `gorm:"type:varchar(255)" json:"note"`
	RecipientMatcher string  `gorm:"type:varchar(50);not null" json:"recipientMatcher"`
	CampaignID       *uint64 `gorm:"index" json:"campaignId"`
	CreatedByUserID  *uint64 `gorm:"index" json:"createdByUserId"`

	PlanDays     int        `gorm:"not null;default:30" json:"planDays"`
	ExpiresAt    int64      `gorm:"not null" json:"expiresAt"`
	IsUsed       bool       `gorm:"default:false" json:"isUsed"`
	UsedByUserID *uint64    `gorm:"index" json:"usedByUserId"`
	UsedAt       *time.Time `json:"usedAt"`
}
```

- [ ] **Step 3: Add LicenseKeyBatch to AutoMigrate**

In `api/migrate.go`, add `&LicenseKeyBatch{}` before `&LicenseKey{}` in the `AutoMigrate` call (around line 30):

```go
		&Campaign{},
		&LicenseKeyBatch{},
		&LicenseKey{},
```

- [ ] **Step 4: Run migrate to verify**

Run: `cd api && go build ./...`
Expected: Compiles without error.

- [ ] **Step 5: Commit**

```bash
git add api/model.go api/migrate.go
git commit -m "feat: add LicenseKeyBatch model and BatchID on LicenseKey"
```

---

### Task 2: Add Request/Response Types

**Files:**
- Modify: `api/type.go` (add batch types, update license key types)

- [ ] **Step 1: Add batch request/response types to `api/type.go`**

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
	Date           string  `json:"date"`
	Redeemed       int64   `json:"redeemed"`
	ConvertedUsers int64   `json:"convertedUsers"`
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

- [ ] **Step 2: Verify compilation**

Run: `cd api && go build ./...`
Expected: Compiles without error.

- [ ] **Step 3: Commit**

```bash
git add api/type.go
git commit -m "feat: add LicenseKeyBatch request/response types"
```

---

### Task 3: Batch Business Logic + Key Generation

**Files:**
- Create: `api/logic_license_key_batch.go`

- [ ] **Step 1: Create `api/logic_license_key_batch.go` with batch generation logic**

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
			UUID:             xid.New().String(),
			Code:             code,
			BatchID:          batch.ID,
			PlanDays:         batch.PlanDays,
			RecipientMatcher: batch.RecipientMatcher,
			ExpiresAt:        batch.ExpiresAt,
			Source:           "batch",
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

// GetBatchStats computes redemption + conversion stats for a single batch.
func GetBatchStats(ctx context.Context, batchID uint64) (*BatchStatsResponse, error) {
	var batch LicenseKeyBatch
	if err := db.Get().First(&batch, batchID).Error; err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	var redeemed, expired int64
	if err := db.Get().Model(&LicenseKey{}).Where("batch_id = ? AND is_used = true", batchID).Count(&redeemed).Error; err != nil {
		return nil, err
	}
	if err := db.Get().Model(&LicenseKey{}).Where("batch_id = ? AND is_used = false AND expires_at < ?", batchID, now).Count(&expired).Error; err != nil {
		return nil, err
	}

	// Conversion: users who redeemed a key from this batch and later made a paid order
	type convResult struct {
		ConvertedUsers int64  `gorm:"column:converted_users"`
		Revenue        uint64 `gorm:"column:revenue"`
	}
	var conv convResult
	db.Get().Raw(`
		SELECT
			COUNT(DISTINCT o.user_id) AS converted_users,
			COALESCE(SUM(o.pay_amount), 0) AS revenue
		FROM license_keys k
		JOIN orders o ON o.user_id = k.used_by_user_id AND o.is_paid = true AND o.created_at > k.used_at
		WHERE k.batch_id = ? AND k.is_used = true AND k.deleted_at IS NULL
	`, batchID).Scan(&conv)

	total := int64(batch.Quantity)
	redeemRate := float64(0)
	if total > 0 {
		redeemRate = float64(redeemed) / float64(total)
	}
	conversionRate := float64(0)
	if redeemed > 0 {
		conversionRate = float64(conv.ConvertedUsers) / float64(redeemed)
	}

	return &BatchStatsResponse{
		BatchID:        batch.ID,
		Name:           batch.Name,
		SourceTag:      batch.SourceTag,
		TotalKeys:      total,
		Redeemed:       redeemed,
		Expired:        expired,
		RedeemRate:     redeemRate,
		ConvertedUsers: conv.ConvertedUsers,
		ConversionRate: conversionRate,
		Revenue:        conv.Revenue,
	}, nil
}

// GetAllBatchStats computes stats for all batches.
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

// GetBatchStatsBySource aggregates stats by source_tag.
func GetBatchStatsBySource(ctx context.Context) ([]BatchStatsBySourceResponse, error) {
	now := time.Now().Unix()
	type row struct {
		SourceTag      string `gorm:"column:source_tag"`
		TotalKeys      int64  `gorm:"column:total_keys"`
		Redeemed       int64  `gorm:"column:redeemed"`
		ConvertedUsers int64  `gorm:"column:converted_users"`
		Revenue        uint64 `gorm:"column:revenue"`
	}
	var rows []row
	if err := db.Get().Raw(`
		SELECT
			b.source_tag,
			SUM(b.quantity) AS total_keys,
			SUM(CASE WHEN k.is_used = 1 THEN 1 ELSE 0 END) AS redeemed,
			COUNT(DISTINCT CASE WHEN o.id IS NOT NULL THEN o.user_id END) AS converted_users,
			COALESCE(SUM(CASE WHEN o.id IS NOT NULL THEN o.pay_amount ELSE 0 END), 0) AS revenue
		FROM license_key_batches b
		LEFT JOIN license_keys k ON k.batch_id = b.id AND k.deleted_at IS NULL
		LEFT JOIN orders o ON o.user_id = k.used_by_user_id AND o.is_paid = true AND o.created_at > k.used_at
		WHERE b.deleted_at IS NULL
		GROUP BY b.source_tag
		ORDER BY total_keys DESC
	`).Scan(&rows).Error; err != nil {
		return nil, err
	}

	results := make([]BatchStatsBySourceResponse, 0, len(rows))
	for _, r := range rows {
		redeemRate := float64(0)
		if r.TotalKeys > 0 {
			redeemRate = float64(r.Redeemed) / float64(r.TotalKeys)
		}
		convRate := float64(0)
		if r.Redeemed > 0 {
			convRate = float64(r.ConvertedUsers) / float64(r.Redeemed)
		}
		results = append(results, BatchStatsBySourceResponse{
			SourceTag:      r.SourceTag,
			TotalKeys:      r.TotalKeys,
			Redeemed:       r.Redeemed,
			RedeemRate:     redeemRate,
			ConvertedUsers: r.ConvertedUsers,
			ConversionRate: convRate,
			Revenue:        r.Revenue,
		})
	}
	return results, nil
}

// GetBatchStatsTrend returns daily redemption trend for the last N days.
func GetBatchStatsTrend(ctx context.Context, days int) ([]BatchStatsTrendResponse, error) {
	if days <= 0 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days)

	type row struct {
		Date           string `gorm:"column:date"`
		Redeemed       int64  `gorm:"column:redeemed"`
		ConvertedUsers int64  `gorm:"column:converted_users"`
	}
	var rows []row
	if err := db.Get().Raw(`
		SELECT
			DATE(k.used_at) AS date,
			COUNT(*) AS redeemed,
			COUNT(DISTINCT CASE WHEN o.id IS NOT NULL THEN o.user_id END) AS converted_users
		FROM license_keys k
		LEFT JOIN orders o ON o.user_id = k.used_by_user_id AND o.is_paid = true AND o.created_at > k.used_at
		WHERE k.is_used = true AND k.used_at >= ? AND k.deleted_at IS NULL AND k.batch_id > 0
		GROUP BY DATE(k.used_at)
		ORDER BY date
	`, since).Scan(&rows).Error; err != nil {
		return nil, err
	}

	results := make([]BatchStatsTrendResponse, len(rows))
	for i, r := range rows {
		results[i] = BatchStatsTrendResponse{
			Date:           r.Date,
			Redeemed:       r.Redeemed,
			ConvertedUsers: r.ConvertedUsers,
		}
	}
	return results, nil
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd api && go build ./...`
Expected: Compiles without error.

- [ ] **Step 3: Commit**

```bash
git add api/logic_license_key_batch.go
git commit -m "feat: add LicenseKeyBatch business logic with conversion tracking"
```

---

### Task 4: Batch Admin API Handlers + Routes

**Files:**
- Create: `api/api_admin_license_key_batch.go`
- Modify: `api/route.go` (add batch routes, remove campaign issue-keys route)
- Modify: `api/worker_integration.go` (register new approval callback)
- Modify: `api/logic_approval_callbacks.go` (add batch create callback)

- [ ] **Step 1: Create `api/api_admin_license_key_batch.go`**

```go
package center

import (
	"fmt"
	"strconv"

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

	summary := fmt.Sprintf("创建授权码批次「%s」(%d 个, %d 天, 渠道:%s)", req.Name, req.Quantity, req.PlanDays, req.SourceTag)
	approvalID, executed, err := SubmitApproval(c, "license_key_batch_create", req, summary)
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

	now := time.Now().Unix()

	items := make([]LicenseKeyBatchResponse, 0, len(batches))
	for _, b := range batches {
		var redeemed, expired int64
		db.Get().Model(&LicenseKey{}).Where("batch_id = ? AND is_used = true", b.ID).Count(&redeemed)
		db.Get().Model(&LicenseKey{}).Where("batch_id = ? AND is_used = false AND expires_at < ?", b.ID, now).Count(&expired)
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

	var batch LicenseKeyBatch
	if err := db.Get().First(&batch, id).Error; err != nil {
		Error(c, ErrorNotFound, "batch not found")
		return
	}

	stats, err := GetBatchStats(c.Request.Context(), id)
	if err != nil {
		log.Errorf(c, "failed to get batch stats: %v", err)
		Error(c, ErrorSystemError, "failed to get stats")
		return
	}

	resp := LicenseKeyBatchDetailResponse{
		LicenseKeyBatchResponse: toLicenseKeyBatchResponse(&batch, stats.Redeemed, stats.Expired),
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

	// Verify batch exists
	var batch LicenseKeyBatch
	if err := db.Get().First(&batch, id).Error; err != nil {
		Error(c, ErrorNotFound, "batch not found")
		return
	}

	pagination := PaginationFromRequest(c)
	query := db.Get().Model(&LicenseKey{}).Where("batch_id = ?", id)

	switch c.Query("status") {
	case "used":
		query = query.Where("is_used = true")
	case "unused":
		query = query.Where("is_used = false AND expires_at >= ?", time.Now().Unix())
	case "expired":
		query = query.Where("is_used = false AND expires_at < ?", time.Now().Unix())
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

Add after the existing `executeApprovalCampaignIssueKeys` function (around line 155):

```go
// ===================== License Key Batch =====================

func executeApprovalLicenseKeyBatchCreate(ctx context.Context, params json.RawMessage) error {
	var req CreateLicenseKeyBatchRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}

	// Admin user ID from approval record is not passed here — use 0 as system-created.
	// The approval record itself tracks who initiated.
	_, err := CreateLicenseKeyBatch(ctx, &req, 0)
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

In `InitWorker()`, add after the `campaign_issue_keys` registration (around line 83):

```go
	RegisterApprovalCallback("license_key_batch_create", executeApprovalLicenseKeyBatchCreate)
	RegisterApprovalCallback("license_key_batch_delete", executeApprovalLicenseKeyBatchDelete)
```

- [ ] **Step 4: Add routes in `api/route.go`**

In the `opsAdmin` block (around line 438), add the batch routes before the existing license-keys routes:

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

**Important**: The `/stats` routes must come BEFORE `/:id` to avoid Gin treating "stats" as an `:id` parameter.

- [ ] **Step 5: Verify compilation**

Run: `cd api && go build ./...`
Expected: Compiles without error.

- [ ] **Step 6: Commit**

```bash
git add api/api_admin_license_key_batch.go api/logic_approval_callbacks.go api/worker_integration.go api/route.go
git commit -m "feat: add license key batch admin API handlers and routes"
```

---

### Task 5: Update Redemption Logic — Read RecipientMatcher from Batch

**Files:**
- Modify: `api/logic_license_key.go` (update `MatchLicenseKey`, `RedeemLicenseKey`)

- [ ] **Step 1: Update `MatchLicenseKey` to support batch-based matching**

In `api/logic_license_key.go`, replace `MatchLicenseKey` (lines 92-101):

```go
// MatchLicenseKey checks whether user is eligible to redeem this key.
// Reads RecipientMatcher from the associated batch if available, otherwise falls back to key's own field.
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

- [ ] **Step 2: Update `RedeemLicenseKey` to preload Batch**

In `api/logic_license_key.go`, change line 113 from:

```go
		if err := tx.Where("code = ?", code).First(&k).Error; err != nil {
```

to:

```go
		if err := tx.Preload("Batch").Where("code = ?", code).First(&k).Error; err != nil {
```

- [ ] **Step 3: Verify compilation**

Run: `cd api && go build ./...`
Expected: Compiles without error.

- [ ] **Step 4: Commit**

```bash
git add api/logic_license_key.go
git commit -m "feat: update redemption to read RecipientMatcher from batch"
```

---

### Task 6: Update Existing License Key Admin Handlers

**Files:**
- Modify: `api/api_admin_license_key.go` (update list filter from campaignId to batchId, remove create/stats handlers)
- Modify: `api/route.go` (remove old routes)

- [ ] **Step 1: Update `api_admin_list_license_keys` in `api/api_admin_license_key.go`**

Replace lines 14-59 with:

```go
// GET /app/license-keys
func api_admin_list_license_keys(c *gin.Context) {
	pagination := PaginationFromRequest(c)
	batchIDStr := c.Query("batchId")
	isUsedStr := c.Query("isUsed")

	query := db.Get().Model(&LicenseKey{})
	if batchIDStr != "" {
		id, err := strconv.ParseUint(batchIDStr, 10, 64)
		if err == nil {
			query = query.Where("batch_id = ?", id)
		}
	}
	if isUsedStr == "true" {
		query = query.Where("is_used = true")
	} else if isUsedStr == "false" {
		query = query.Where("is_used = false")
	}

	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count license keys: %v", err)
		Error(c, ErrorSystemError, "failed to count license keys")
		return
	}

	var keys []LicenseKey
	if err := query.Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&keys).Error; err != nil {
		log.Errorf(c, "failed to query license keys: %v", err)
		Error(c, ErrorSystemError, "failed to query license keys")
		return
	}

	items := make([]LicenseKeyResponse, 0, len(keys))
	for i := range keys {
		items = append(items, toLicenseKeyResponse(&keys[i]))
	}
	ListWithData(c, items, pagination)
}
```

- [ ] **Step 2: Remove `api_admin_license_key_stats` and `api_admin_create_license_keys` functions**

Delete `api_admin_license_key_stats` (lines 62-85) and `api_admin_create_license_keys` (lines 107-131) from `api/api_admin_license_key.go`.

- [ ] **Step 3: Update `toLicenseKeyResponse` to include batchId**

Replace `toLicenseKeyResponse` (lines 133-154):

```go
func toLicenseKeyResponse(k *LicenseKey) LicenseKeyResponse {
	resp := LicenseKeyResponse{
		ID:           k.ID,
		UUID:         k.UUID,
		Code:         k.Code,
		Source:       k.Source,
		Note:         k.Note,
		PlanDays:     k.PlanDays,
		RecipientMatcher: k.RecipientMatcher,
		ExpiresAt:    k.ExpiresAt,
		CampaignID:   k.CampaignID,
		BatchID:      k.BatchID,
		IsUsed:       k.IsUsed,
		UsedByUserID: k.UsedByUserID,
		CreatedAt:    k.CreatedAt.Unix(),
	}
	if k.UsedAt != nil {
		usedAt := k.UsedAt.Unix()
		resp.UsedAt = &usedAt
	}
	return resp
}
```

- [ ] **Step 4: Add `BatchID` to `LicenseKeyResponse` in `api/type.go`**

In the `LicenseKeyResponse` struct (around line 980), add:

```go
	BatchID          uint64  `json:"batchId"`
```

- [ ] **Step 5: Update routes in `api/route.go`**

Remove the old routes (lines 439-441):
```go
		// Remove these 3 lines:
		opsAdmin.GET("/license-keys/stats", ...)
		opsAdmin.POST("/license-keys", ...)
```

Keep only:
```go
		// LicenseKey 列表 + 删除（批次管理通过 batch 路由）
		opsAdmin.GET("/license-keys",                       RoleRequired(RoleMarketing), api_admin_list_license_keys)
		opsAdmin.DELETE("/license-keys/:id",                RoleRequired(RoleMarketing), api_admin_delete_license_key)
```

- [ ] **Step 6: Verify compilation**

Run: `cd api && go build ./...`
Expected: Compiles without error.

- [ ] **Step 7: Commit**

```bash
git add api/api_admin_license_key.go api/type.go api/route.go
git commit -m "refactor: simplify license key admin — remove create/stats, filter by batchId"
```

---

### Task 7: Campaign Cleanup — Remove Shareable Fields

**Files:**
- Modify: `api/model.go` (remove `IsShareable`, `SharesPerUser` from Campaign)
- Modify: `api/type.go` (remove from CampaignRequest, CampaignResponse)
- Modify: `api/api_admin_campaigns.go` (remove `api_admin_issue_license_keys`, update `convertCampaignToResponse`)
- Modify: `api/logic_approval_callbacks.go` (remove `executeApprovalCampaignIssueKeys`)
- Modify: `api/worker_integration.go` (remove callback registration)
- Delete: `api/worker_license_key.go` (SendLicenseKeyEmails)
- Modify: `api/logic_license_key.go` (remove `GenerateLicenseKeysForCampaign`, `CountEligibleUsers`, `queryEligibleUsers`)
- Modify: `api/route.go` (remove issue-keys route)

- [ ] **Step 1: Remove `IsShareable` and `SharesPerUser` from Campaign model**

In `api/model.go`, delete lines 758-759:
```go
	IsShareable   bool   `gorm:"default:false" json:"isShareable"`
	SharesPerUser int64  `gorm:"default:0" json:"sharesPerUser"`
```

- [ ] **Step 2: Remove from CampaignRequest and CampaignResponse in `api/type.go`**

In `CampaignRequest` (around line 585-586), delete:
```go
	IsShareable   bool   `json:"isShareable"`
	SharesPerUser int64  `json:"sharesPerUser"`
```

In `CampaignResponse` (around line 606-607), delete:
```go
	IsShareable   bool   `json:"isShareable"`
	SharesPerUser int64  `json:"sharesPerUser"`
```

- [ ] **Step 3: Remove `IsShareable`/`SharesPerUser` from `convertCampaignToResponse`**

In `api/api_admin_campaigns.go` function `convertCampaignToResponse` (around line 497-498), delete:
```go
		IsShareable:   campaign.IsShareable,
		SharesPerUser: campaign.SharesPerUser,
```

- [ ] **Step 4: Delete `api_admin_issue_license_keys` handler**

In `api/api_admin_campaigns.go`, delete the entire function (lines 13-82) including its section comment.

- [ ] **Step 5: Remove the issue-keys route**

In `api/route.go`, delete line 436:
```go
		opsAdmin.POST("/campaigns/:id/issue-keys",          RoleRequired(RoleMarketing), api_admin_issue_license_keys)
```

- [ ] **Step 6: Remove `executeApprovalCampaignIssueKeys` callback**

In `api/logic_approval_callbacks.go`, delete the entire `executeApprovalCampaignIssueKeys` function (lines 130-155).

- [ ] **Step 7: Remove callback registration**

In `api/worker_integration.go`, delete line 83:
```go
	RegisterApprovalCallback("campaign_issue_keys", executeApprovalCampaignIssueKeys)
```

- [ ] **Step 8: Delete `api/worker_license_key.go`**

Remove the entire file (contains `SendLicenseKeyEmails` and `sendGiftEmail`).

- [ ] **Step 9: Remove campaign-coupled functions from `api/logic_license_key.go`**

Delete `GenerateLicenseKeysForCampaign` (lines 230-279), `CountEligibleUsers` (lines 281-288), and `queryEligibleUsers` (lines 290-319).

Also delete the now-unused constant `licenseKeyTTLDays` (line 18) and the `CreateManualLicenseKeys` function (lines 56-84) which is replaced by batch creation.

- [ ] **Step 10: Remove unused types from `api/type.go`**

Delete `IssueKeysRequest`, `IssueKeysResponse`, `CreateLicenseKeysRequest`, `CreateLicenseKeysResponse`, `LicenseKeyBrief` (lines 997-1033).

Also remove `LicenseKeyResponse` fields that no longer exist: `CreatedByUserID` (line 990) and `CampaignID` (line 989). Keep `BatchID`.

- [ ] **Step 11: Verify compilation**

Run: `cd api && go build ./...`
Expected: Compiles without error. Fix any remaining references.

- [ ] **Step 12: Commit**

```bash
git add -A api/
git commit -m "refactor: remove campaign-license key coupling, clean shareable fields"
```

---

### Task 8: MCP Tools Update

**Files:**
- Modify: `tools/kaitu-center/src/tools/admin-license-keys.ts` (replace with batch tools)
- Modify: `tools/kaitu-center/src/tools/admin-campaigns.ts` (remove `issue_campaign_keys`)

- [ ] **Step 1: Replace `tools/kaitu-center/src/tools/admin-license-keys.ts`**

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
    description: 'Create a license key batch (requires approval). Generates keys immediately upon approval.',
    group: 'license_keys.write',
    method: 'POST',
    params: {
      name: z.string().describe('Batch name'),
      source_tag: z.string().optional().describe('Channel tag (e.g. twitter, kol-xxx, winback)'),
      recipient_matcher: z.enum(['all', 'never_paid']).describe('Who can redeem: all or never_paid'),
      plan_days: z.number().describe('Membership days per key'),
      quantity: z.number().describe('Number of keys to generate (1-10000)'),
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
    description: 'Get license key batch stats (all batches or single batch). Includes conversion rate.',
    group: 'license_keys',
    params: {
      batch_id: z.number().optional().describe('Batch ID (omit for all batches)'),
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
    description: 'List all license keys with pagination (filter by batchId).',
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

- [ ] **Step 2: Remove `issue_campaign_keys` from `tools/kaitu-center/src/tools/admin-campaigns.ts`**

Delete the `issue_campaign_keys` tool definition (lines 113-123).

- [ ] **Step 3: Build MCP tools**

Run: `cd tools/kaitu-center && npm run build`
Expected: Builds without error.

- [ ] **Step 4: Commit**

```bash
git add tools/kaitu-center/src/tools/admin-license-keys.ts tools/kaitu-center/src/tools/admin-campaigns.ts
git commit -m "feat: update MCP tools — batch-based license key management"
```

---

### Task 9: Web Admin — API Types + Batch API Methods

**Files:**
- Modify: `web/src/lib/api.ts` (add batch types/methods, update license key types, remove old methods)

- [ ] **Step 1: Update LicenseKey types in `web/src/lib/api.ts`**

Replace the entire `// LicenseKey types` section (lines 2323-2380) with:

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

export interface BatchStatsTrend {
  date: string;
  redeemed: number;
  convertedUsers: number;
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

// Keep for public redemption page
export interface LicenseKeyPublic {
  code: string;
  planDays: number;
  expiresAt: number;
  isUsed: boolean;
  isExpired: boolean;
  senderName: string;
}

// Keep simplified for admin list
export interface LicenseKeyAdmin {
  id: number;
  uuid: string;
  code: string;
  source: string;
  note: string;
  planDays: number;
  recipientMatcher: string;
  expiresAt: number;
  batchId: number;
  isUsed: boolean;
  usedByUserId?: number;
  usedAt?: number;
  createdAt: number;
}
```

- [ ] **Step 2: Update API methods**

Replace the license key API methods (around lines 2028-2078) with:

```typescript
  // License Key Batch APIs
  async listLicenseKeyBatches(params: { page?: number; pageSize?: number; sourceTag?: string } = {}): Promise<{ items: LicenseKeyBatch[]; total: number }> {
    const queryParams = new URLSearchParams();
    if (params.page) queryParams.set('page', String(params.page));
    if (params.pageSize) queryParams.set('pageSize', String(params.pageSize));
    if (params.sourceTag) queryParams.set('sourceTag', params.sourceTag);
    const query = queryParams.toString();
    return this.request<{ items: LicenseKeyBatch[]; total: number }>(`/app/license-key-batches${query ? '?' + query : ''}`);
  },

  async getLicenseKeyBatch(id: number): Promise<LicenseKeyBatchDetail> {
    return this.request<LicenseKeyBatchDetail>(`/app/license-key-batches/${id}`);
  },

  async createLicenseKeyBatch(req: CreateLicenseKeyBatchRequest): Promise<void> {
    return this.request<void>('/app/license-key-batches', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  },

  async deleteLicenseKeyBatch(id: number): Promise<void> {
    return this.request<void>(`/app/license-key-batches/${id}`, { method: 'DELETE' });
  },

  async listLicenseKeyBatchKeys(batchId: number, params: { status?: string; page?: number; pageSize?: number } = {}): Promise<{ items: LicenseKeyItem[]; total: number }> {
    const queryParams = new URLSearchParams();
    if (params.status) queryParams.set('status', params.status);
    if (params.page) queryParams.set('page', String(params.page));
    if (params.pageSize) queryParams.set('pageSize', String(params.pageSize));
    const query = queryParams.toString();
    return this.request<{ items: LicenseKeyItem[]; total: number }>(`/app/license-key-batches/${batchId}/keys${query ? '?' + query : ''}`);
  },

  async getLicenseKeyBatchStats(): Promise<BatchStats[]> {
    return this.request<BatchStats[]>('/app/license-key-batches/stats');
  },

  async getLicenseKeyBatchStatsBySource(): Promise<BatchStatsBySource[]> {
    return this.request<BatchStatsBySource[]>('/app/license-key-batches/stats/by-source');
  },

  async getLicenseKeyBatchStatsTrend(days?: number): Promise<BatchStatsTrend[]> {
    const query = days ? `?days=${days}` : '';
    return this.request<BatchStatsTrend[]>(`/app/license-key-batches/stats/trend${query}`);
  },

  // Legacy license key APIs (simplified)
  async getLicenseKey(code: string): Promise<LicenseKeyPublic> {
    return this.request<LicenseKeyPublic>(`/api/license-keys/code/${code}`);
  },

  async listAdminLicenseKeys(params: { batchId?: number; isUsed?: boolean; page?: number; pageSize?: number } = {}): Promise<{ items: LicenseKeyAdmin[]; total: number }> {
    const queryParams = new URLSearchParams();
    if (params.batchId) queryParams.set('batchId', String(params.batchId));
    if (params.isUsed !== undefined) queryParams.set('isUsed', String(params.isUsed));
    if (params.page) queryParams.set('page', String(params.page));
    if (params.pageSize) queryParams.set('pageSize', String(params.pageSize));
    const query = queryParams.toString();
    return this.request<{ items: LicenseKeyAdmin[]; total: number }>(`/app/license-keys${query ? '?' + query : ''}`);
  },

  async deleteAdminLicenseKey(id: number): Promise<void> {
    return this.request<void>(`/app/license-keys/${id}`, { method: 'DELETE' });
  },

  async redeemLicenseKey(code: string): Promise<{ planDays: number; newExpireAt: number; historyId: number }> {
    return this.request<{ planDays: number; newExpireAt: number; historyId: number }>(`/api/license-keys/code/${code}/redeem`, {
      method: 'POST',
    });
  },
```

- [ ] **Step 3: Remove old types**

Delete `IssueKeysRequest`, `IssueKeysResponse`, `CreateLicenseKeysRequest`, `CreateLicenseKeysResponse`, `LicenseKeyStatsRow` interfaces.

- [ ] **Step 4: Verify build**

Run: `cd web && yarn build`
Expected: May show errors in pages that reference removed types — those will be fixed in next tasks.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/api.ts
git commit -m "feat: add license key batch API types and methods"
```

---

### Task 10: Web Admin — Campaign Page Cleanup

**Files:**
- Modify: `web/src/app/(manager)/manager/campaigns/page.tsx` (remove isShareable/sharesPerUser UI, remove issue-keys section)

- [ ] **Step 1: Remove `isShareable` and `sharesPerUser` from form state defaults**

In the campaigns page, find all `isShareable` and `sharesPerUser` references and remove them from:
- Default form state (lines ~105-106)
- Reset form state (lines ~363-364)
- Edit form populate (lines ~401-402)

- [ ] **Step 2: Remove the isShareable switch + sharesPerUser input UI**

Delete the entire block from the Switch for isShareable through the conditional sharesPerUser input (lines ~635-660).

- [ ] **Step 3: Remove the issue-keys section in edit dialog**

Delete the entire "Issue keys section" block (lines ~848-900+).

- [ ] **Step 4: Remove the isShareable column from the table**

Remove the column that shows "— / N 个/人" (lines ~189-197).

- [ ] **Step 5: Remove the Key import if unused**

Check if `Key` from lucide-react is still used. If only used for isShareable UI, remove the import.

- [ ] **Step 6: Verify build**

Run: `cd web && yarn build`
Expected: Compiles without error.

- [ ] **Step 7: Commit**

```bash
git add web/src/app/(manager)/manager/campaigns/page.tsx
git commit -m "refactor: remove license key issuance UI from campaigns page"
```

---

### Task 11: Web Admin — License Key Batches Page

**Files:**
- Create: `web/src/app/(manager)/manager/license-key-batches/page.tsx`
- Modify: `web/src/app/(manager)/manager/license-keys/page.tsx` (simplify)

This is a larger UI task. The implementation should follow the existing page patterns in the manager section (shadcn table, dialog for create, stats cards at top).

- [ ] **Step 1: Create the batch management page**

Create `web/src/app/(manager)/manager/license-key-batches/page.tsx` following the pattern of the existing campaigns page. Key sections:
- Stats summary cards at top (total batches, total keys, overall redemption rate, overall conversion rate)
- Batch list table with columns: Name, Source Tag, Quantity, Redeemed/Total, Conversion Rate, Expires, Created
- Create dialog with form fields matching `CreateLicenseKeyBatchRequest`
- Click row → expand/dialog showing batch detail + keys list with status filter
- Delete button (with confirmation)

The page should use `api.listLicenseKeyBatches()`, `api.createLicenseKeyBatch()`, `api.getLicenseKeyBatch()`, `api.listLicenseKeyBatchKeys()`, `api.deleteLicenseKeyBatch()`, `api.getLicenseKeyBatchStats()`.

- [ ] **Step 2: Simplify the existing license-keys page**

In `web/src/app/(manager)/manager/license-keys/page.tsx`:
- Remove the stats section (now in batches page)
- Remove the create dialog (now via batches)
- Change `campaignId` filter to `batchId` filter
- Remove `source` filter (no longer relevant)
- Update imports to use new types from `api.ts`

- [ ] **Step 3: Add navigation link**

Add "授权码批次" to the manager sidebar/nav (find the nav config file and add the route).

- [ ] **Step 4: Verify build**

Run: `cd web && yarn build`
Expected: Compiles without error.

- [ ] **Step 5: Commit**

```bash
git add web/src/app/(manager)/manager/license-key-batches/ web/src/app/(manager)/manager/license-keys/page.tsx
git commit -m "feat: add license key batches admin page, simplify license keys page"
```

---

### Task 12: Data Migration Script

**Files:**
- Create: `scripts/migrate-license-keys-to-batches.sql`

- [ ] **Step 1: Write migration SQL**

```sql
-- License Key Batch Decoupling Migration
-- Run AFTER the new code is deployed (GORM AutoMigrate creates the new table/columns)

-- 1. Create batch records for existing campaign-sourced keys
INSERT INTO license_key_batches (name, source_tag, recipient_matcher, plan_days, quantity, expires_at, note, created_by_user_id, created_at, updated_at)
SELECT
    CONCAT('Legacy Campaign #', k.campaign_id),
    'campaign-legacy',
    'never_paid',
    30,
    COUNT(*),
    MAX(k.expires_at),
    CONCAT('Auto-migrated from campaign_id=', k.campaign_id),
    0,
    MIN(k.created_at),
    NOW()
FROM license_keys k
WHERE k.campaign_id IS NOT NULL AND k.deleted_at IS NULL
GROUP BY k.campaign_id;

-- 2. Create a batch for manual keys
INSERT INTO license_key_batches (name, source_tag, recipient_matcher, plan_days, quantity, expires_at, note, created_by_user_id, created_at, updated_at)
SELECT
    'Legacy Manual Keys',
    'manual-legacy',
    'all',
    30,
    COUNT(*),
    COALESCE(MAX(expires_at), UNIX_TIMESTAMP() + 86400*30),
    'Auto-migrated manual keys',
    0,
    MIN(created_at),
    NOW()
FROM license_keys
WHERE (campaign_id IS NULL OR source = 'manual') AND deleted_at IS NULL
HAVING COUNT(*) > 0;

-- 3. Backfill batch_id for campaign-sourced keys
UPDATE license_keys k
JOIN license_key_batches b ON b.name = CONCAT('Legacy Campaign #', k.campaign_id) AND b.source_tag = 'campaign-legacy'
SET k.batch_id = b.id
WHERE k.campaign_id IS NOT NULL AND k.batch_id = 0;

-- 4. Backfill batch_id for manual keys
UPDATE license_keys k
JOIN license_key_batches b ON b.name = 'Legacy Manual Keys' AND b.source_tag = 'manual-legacy'
SET k.batch_id = b.id
WHERE k.batch_id = 0 AND k.deleted_at IS NULL;

-- 5. Verify: no keys with batch_id = 0
SELECT COUNT(*) AS orphaned_keys FROM license_keys WHERE batch_id = 0 AND deleted_at IS NULL;
-- Expected: 0

-- 6. After verification, drop old columns (run separately after confirming step 5)
-- ALTER TABLE license_keys DROP COLUMN campaign_id;
-- ALTER TABLE license_keys DROP COLUMN source;
-- ALTER TABLE license_keys DROP COLUMN recipient_matcher;
-- ALTER TABLE license_keys DROP COLUMN created_by_user_id;
-- ALTER TABLE campaigns DROP COLUMN is_shareable;
-- ALTER TABLE campaigns DROP COLUMN shares_per_user;
```

- [ ] **Step 2: Commit**

```bash
git add scripts/migrate-license-keys-to-batches.sql
git commit -m "feat: add data migration script for license key batch decoupling"
```

---

### Task 13: Verification

- [ ] **Step 1: Run Go tests**

Run: `cd api && go test ./...`
Expected: All existing tests pass. Fix any failures caused by removed types/functions.

- [ ] **Step 2: Run MCP tools build**

Run: `cd tools/kaitu-center && npm run build`
Expected: Builds without error.

- [ ] **Step 3: Run web build**

Run: `cd web && yarn build`
Expected: Builds without error.

- [ ] **Step 4: Run full build verification**

Run: `scripts/test_build.sh`
Expected: All 14 checks pass.

- [ ] **Step 5: Manual smoke test checklist**

Verify these endpoints work against a local dev instance:
- `POST /app/license-key-batches` — creates batch + keys
- `GET /app/license-key-batches` — lists batches with counts
- `GET /app/license-key-batches/:id` — detail with conversion stats
- `GET /app/license-key-batches/:id/keys` — lists keys with status filter
- `GET /app/license-key-batches/stats` — all batch stats
- `GET /app/license-key-batches/stats/by-source` — source aggregation
- `POST /api/license-keys/code/:code/redeem` — redemption still works
- `GET /app/campaigns` — no IsShareable/SharesPerUser in response
- Campaign create/update — no shareable fields accepted

- [ ] **Step 6: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: verification fixes for license key batch decoupling"
```
