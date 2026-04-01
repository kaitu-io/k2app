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

	batchIDs := make([]uint64, len(batches))
	for i, b := range batches {
		batchIDs[i] = b.ID
	}
	redeemedMap, expiredMap := batchBaseCountsBatch(batchIDs)

	items := make([]LicenseKeyBatchResponse, 0, len(batches))
	for _, b := range batches {
		items = append(items, toLicenseKeyBatchResponse(&b, redeemedMap[b.ID], expiredMap[b.ID]))
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

// DELETE /app/license-key-batches/:id — invalidates unused keys, keeps batch for stats.
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
	summary := fmt.Sprintf("作废授权码批次「%s」未使用的 keys (ID:%d)", batch.Name, id)
	approvalID, executed, err := SubmitApproval(c, "license_key_batch_invalidate", params, summary)
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
