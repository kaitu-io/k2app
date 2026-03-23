package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"

	"github.com/gin-gonic/gin"
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
		UUID:      key.UUID,
		PlanDays:  key.PlanDays,
		ExpiresAt: key.ExpiresAt,
		IsUsed:    key.IsUsed,
		IsExpired: key.IsExpired(),
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

// api_redeem_license_key handles POST /api/license-keys/:uuid/redeem (requires auth).
// Validates the key, marks it as used, and extends the user's plan.
func api_redeem_license_key(c *gin.Context) {
	uuid := c.Param("uuid")
	userID := ReqUserID(c)
	log.Debugf(c, "user %d redeeming license key %s", userID, uuid)

	key, history, err := RedeemLicenseKey(c.Request.Context(), uuid, userID)
	if err != nil {
		switch err {
		case ErrLicenseKeyNotFound:
			Error(c, ErrorLicenseKeyNotFound, "not found")
		case ErrLicenseKeyUsed:
			Error(c, ErrorLicenseKeyUsed, "already used")
		case ErrLicenseKeyExpired:
			Error(c, ErrorLicenseKeyExpired, "expired")
		case ErrLicenseKeyNotMatch:
			Error(c, ErrorLicenseKeyNotMatch, "not eligible")
		default:
			log.Errorf(c, "redeem license key %s failed: %v", uuid, err)
			Error(c, ErrorSystemError, "redeem failed")
		}
		return
	}

	// Reload the user to get the updated ExpiredAt after the transaction.
	var updatedUser User
	if err := db.Get().First(&updatedUser, userID).Error; err != nil {
		log.Errorf(c, "failed to reload user %d after redeem: %v", userID, err)
		Error(c, ErrorSystemError, "redeem succeeded but failed to load updated user")
		return
	}

	type RedeemResponse struct {
		PlanDays    int    `json:"planDays"`
		NewExpireAt int64  `json:"newExpireAt"`
		HistoryID   uint64 `json:"historyId"`
	}
	Success(c, &RedeemResponse{
		PlanDays:    key.PlanDays,
		NewExpireAt: updatedUser.ExpiredAt,
		HistoryID:   history.ID,
	})
}
