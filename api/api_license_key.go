package center

import (
	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
)

// api_get_license_key handles GET /api/license-keys/:uuid (no auth required).
// Used by the redeem landing page to show gift card details.
func api_get_license_key(c *gin.Context) {
	uuid := c.Param("uuid")
	key, err := GetLicenseKeyByUUID(c.Request.Context(), uuid)
	if err != nil {
		Error(c, ErrorLicenseKeyNotFound, "not found")
		return
	}

	resp := LicenseKeyPublicResponse{
		UUID:          key.UUID,
		DiscountType:  key.DiscountType,
		DiscountValue: key.DiscountValue,
		ExpiresAt:     key.ExpiresAt,
		IsUsed:        key.IsUsed,
		IsExpired:     key.IsExpired(),
	}

	// Fetch sender display name (masked email)
	if key.CreatedByUserID != nil {
		var sender User
		if err := db.Get().Preload("LoginIdentifies").
			First(&sender, *key.CreatedByUserID).Error; err == nil {
			email := getUserEmailFromIdentifies(&sender)
			if email != "" {
				resp.SenderName = hideEmail(email)
			}
		}
	}

	Success(c, &resp)
}
