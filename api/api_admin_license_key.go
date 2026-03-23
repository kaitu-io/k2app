package center

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// GET /app/license-keys
func api_admin_list_license_keys(c *gin.Context) {
	log.Infof(c, "admin request to list license keys")

	pagination := PaginationFromRequest(c)
	campaignIDStr := c.Query("campaignId")
	isUsedStr := c.Query("isUsed")

	query := db.Get().Model(&LicenseKey{})
	if campaignIDStr != "" {
		id, err := strconv.ParseUint(campaignIDStr, 10, 64)
		if err == nil {
			query = query.Where("campaign_id = ?", id)
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
	for _, k := range keys {
		items = append(items, toLicenseKeyResponse(k))
	}
	log.Infof(c, "successfully retrieved %d license keys", len(items))
	ListWithData(c, items, pagination)
}

// GET /app/license-keys/stats
func api_admin_license_key_stats(c *gin.Context) {
	log.Infof(c, "admin request for license key stats")

	type StatsRow struct {
		CampaignID *uint64 `json:"campaignId"`
		Total      int64   `json:"total"`
		Used       int64   `json:"used"`
		Expired    int64   `json:"expired"`
	}

	now := time.Now().Unix()
	var rows []StatsRow
	if err := db.Get().Model(&LicenseKey{}).
		Select("campaign_id, COUNT(*) as total, SUM(CASE WHEN is_used THEN 1 ELSE 0 END) as used, SUM(CASE WHEN is_used = false AND expires_at < ? THEN 1 ELSE 0 END) as expired", now).
		Group("campaign_id").
		Scan(&rows).Error; err != nil {
		log.Errorf(c, "failed to query license key stats: %v", err)
		Error(c, ErrorSystemError, "failed to query license key stats")
		return
	}

	log.Infof(c, "successfully retrieved license key stats for %d campaigns", len(rows))
	Success(c, &rows)
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
}

func toLicenseKeyResponse(k LicenseKey) LicenseKeyResponse {
	r := LicenseKeyResponse{
		ID:               k.ID,
		UUID:             k.UUID,
		DiscountType:     k.DiscountType,
		DiscountValue:    k.DiscountValue,
		RecipientMatcher: k.RecipientMatcher,
		ExpiresAt:        k.ExpiresAt,
		CampaignID:       k.CampaignID,
		CreatedByUserID:  k.CreatedByUserID,
		IsUsed:           k.IsUsed,
		UsedByUserID:     k.UsedByUserID,
		CreatedAt:        k.CreatedAt.Unix(),
	}
	if k.UsedAt != nil {
		ts := k.UsedAt.Unix()
		r.UsedAt = &ts
	}
	return r
}
