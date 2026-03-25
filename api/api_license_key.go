package center

import (
	"errors"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"

	"github.com/gin-gonic/gin"
)

// api_get_license_key handles GET /api/license-keys/code/:code (no auth required).
// Used by the redeem landing page to show gift card details.
func api_get_license_key(c *gin.Context) {
	code := NormalizeCode(c.Param("code"))
	if code == "" {
		Error(c, ErrorLicenseKeyNotFound, "not found")
		return
	}

	var key LicenseKey
	if err := db.Get().Where("code = ?", code).First(&key).Error; err != nil {
		Error(c, ErrorLicenseKeyNotFound, "not found")
		return
	}

	resp := LicenseKeyPublicResponse{
		Code:      key.Code,
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

// api_redeem_license_key handles POST /api/license-keys/code/:code/redeem (requires auth).
// Validates the key, marks it as used, and extends the user's plan.
func api_redeem_license_key(c *gin.Context) {
	code := NormalizeCode(c.Param("code"))
	userID := ReqUserID(c)
	log.Debugf(c, "user %d redeeming license key %s", userID, code)

	key, history, err := RedeemLicenseKey(c.Request.Context(), code, userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrLicenseKeyNotFound):
			Error(c, ErrorLicenseKeyNotFound, "not found")
		case errors.Is(err, ErrLicenseKeyUsed):
			Error(c, ErrorLicenseKeyUsed, "already used")
		case errors.Is(err, ErrLicenseKeyExpired):
			Error(c, ErrorLicenseKeyExpired, "expired")
		case errors.Is(err, ErrLicenseKeyNotMatch):
			Error(c, ErrorLicenseKeyNotMatch, "not eligible")
		case errors.Is(err, ErrLicenseKeyAlreadyRedeemed):
			Error(c, ErrorLicenseKeyAlreadyRedeemed, "already redeemed another key")
		default:
			log.Errorf(c, "redeem license key %s failed: %v", code, err)
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
