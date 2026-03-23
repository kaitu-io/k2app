package center

import (
	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
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

// api_preview_license_key handles POST /api/license-keys/:uuid/preview (requires login).
// Returns discount details if the current user is eligible to redeem this key.
func api_preview_license_key(c *gin.Context) {
	uuid := c.Param("uuid")
	userID := ReqUserID(c)
	log.Debugf(c, "user %d previewing license key %s", userID, uuid)

	key, err := GetLicenseKeyByUUID(c.Request.Context(), uuid)
	if err != nil {
		Error(c, ErrorLicenseKeyNotFound, "not found")
		return
	}
	if key.IsUsed {
		Error(c, ErrorLicenseKeyUsed, "already used")
		return
	}
	if key.IsExpired() {
		Error(c, ErrorLicenseKeyExpired, "expired")
		return
	}

	var currentUser User
	if err := db.Get().First(&currentUser, userID).Error; err != nil {
		log.Errorf(c, "failed to get user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to get user")
		return
	}

	if !MatchLicenseKey(key, &currentUser) {
		Error(c, ErrorLicenseKeyNotMatch, "not eligible")
		return
	}

	type PreviewResponse struct {
		DiscountType  string `json:"discountType"`
		DiscountValue uint64 `json:"discountValue"`
		ExpiresAt     int64  `json:"expiresAt"`
		IsValid       bool   `json:"isValid"`
	}
	resp := PreviewResponse{
		DiscountType:  key.DiscountType,
		DiscountValue: key.DiscountValue,
		ExpiresAt:     key.ExpiresAt,
		IsValid:       true,
	}
	Success(c, &resp)
}
