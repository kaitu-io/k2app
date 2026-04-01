package center

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// GET /app/license-keys
func api_admin_list_license_keys(c *gin.Context) {
	log.Infof(c, "admin request to list license keys")

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
	log.Infof(c, "successfully retrieved %d license keys", len(items))
	ListWithData(c, items, pagination)
}

// DELETE /app/license-keys/:id
func api_admin_delete_license_key(c *gin.Context) {
	log.Infof(c, "admin request to delete license key")

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		log.Warnf(c, "invalid license key id: %s", c.Param("id"))
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}
	if err := db.Get().Delete(&LicenseKey{}, id).Error; err != nil {
		log.Errorf(c, "failed to delete license key %d: %v", id, err)
		Error(c, ErrorSystemError, err.Error())
		return
	}
	log.Infof(c, "successfully deleted license key %d", id)
	SuccessEmpty(c)
	WriteAuditLog(c, "license_key_delete", "license_key", fmt.Sprintf("%d", id), nil)
}

func toLicenseKeyResponse(k *LicenseKey) LicenseKeyResponse {
	resp := LicenseKeyResponse{
		ID:           k.ID,
		UUID:         k.UUID,
		Code:         k.Code,
		BatchID:      k.BatchID,
		PlanDays:     k.PlanDays,
		ExpiresAt:    k.ExpiresAt,
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
