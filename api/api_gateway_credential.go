package center

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"

	db "github.com/wordgate/qtoolkit/db"
)

// gatewayCredentialBase returns the bare k2subs:// subscription URL (no creds)
// pointing at this Center's own /api/subs endpoint. injectSubsCreds then splices
// in udid:token before the host.
func gatewayCredentialBase() string {
	return "k2subs://" + viper.GetString("server.domain") + "/api/subs"
}

// newRouterUDID mints an opaque, collision-resistant UDID for a router/gateway
// device. The "router-" prefix keeps it visually distinguishable from app UDIDs.
func newRouterUDID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is a system-level fault; never degrade to a
		// predictable UDID (mirrors GenerateCSRFToken's hard-fail convention).
		panic("crypto/rand failed: " + err.Error())
	}
	return "router-" + hex.EncodeToString(b[:])
}

// api_gateway_credential mints a k2subs:// gateway credential for the
// authenticated user, provided they hold an active 专属线路 (PrivateNodeSubscription).
// The credential lets a self-hosted OpenWrt router (k2r) connect via the same
// /api/subs Basic-Auth path the daemon uses.
//
// Device lifecycle reuses checkDeviceLimitOrKick(isGateway=true): router devices
// are counted/limited independently from app devices, and minting a router
// credential never kicks an app device. Re-minting (rotation) deletes the user's
// existing router devices first, then re-checks the limit.
//
// Route: POST /api/user/gateway-credential (AuthRequired)
func api_gateway_credential(c *gin.Context) {
	ctx := c.Request.Context()
	user := ReqUser(c)
	if user == nil {
		Error(c, ErrorNotLogin, "authentication failed")
		return
	}

	udid := newRouterUDID()
	var accessToken string
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		// Rotation: drop any existing router devices so the limit check counts
		// only the device we are about to mint.
		if err := tx.Where("user_id = ? AND is_gateway = ?", user.ID, true).
			Delete(&Device{}).Error; err != nil {
			return err
		}
		if err := checkDeviceLimitOrKick(ctx, tx, user, true); err != nil {
			return err
		}
		tokens, now, err := generateTokens(ctx, user.ID, udid, user.Roles)
		if err != nil {
			return err
		}
		accessToken = tokens.AccessToken
		return tx.Create(&Device{
			UDID:            udid,
			UserID:          user.ID,
			IsGateway:       true,
			AppPlatform:     "router",
			TokenIssueAt:    now.Unix(),
			TokenLastUsedAt: now.Unix(),
		}).Error
	})
	if err != nil {
		// e(...) rerr carries a business error code (e.g. ErrorRouterDeviceLimit,
		// ErrorPlanNoRouter) — surface it; otherwise it's a system fault.
		if _, ok := err.(rerr); ok {
			ErrorE(c, err)
			return
		}
		log.Errorf(c, "gateway-credential: failed to mint router credential for user %d: %v", user.ID, err)
		Error(c, ErrorSystemError, "mint router credential failed")
		return
	}

	url := injectSubsCreds(gatewayCredentialBase(), udid, accessToken)
	Success(c, &gin.H{"url": url})
}
