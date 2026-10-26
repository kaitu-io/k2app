package center

import (
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
)

// AppleConsumptionConsentRequest PUT /api/user/apple-consumption-consent 请求体。
// Consented 用指针 + required：缺字段 422，不能被当成 false 落库。
type AppleConsumptionConsentRequest struct {
	Consented *bool `json:"consented" binding:"required"`
}

// api_put_apple_consumption_consent 记录用户是否同意把消费信息提供给 Apple 用于退款审核
// （spec 2026-10-08 §5）。同意 / 撤回都写两列，返回最新 DataUser。
func api_put_apple_consumption_consent(c *gin.Context) {
	userID := ReqUserID(c)

	var req AppleConsumptionConsentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	if err := db.Get().Model(&User{}).Where(&User{ID: userID}).Updates(map[string]any{
		"apple_consumption_consent":    *req.Consented,
		"apple_consumption_consent_at": time.Now().Unix(),
	}).Error; err != nil {
		log.Errorf(c, "failed to update apple consumption consent for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to update consent")
		return
	}

	var user User
	if err := db.Get().Preload("InvitedByCode").Preload("LoginIdentifies").Preload("Devices").
		Where(&User{ID: userID}).First(&user).Error; err != nil {
		if util.DbIsNotFoundErr(err) {
			Error(c, ErrorNotFound, "user not found")
		} else {
			log.Errorf(c, "failed to query user %d: %v", userID, err)
			Error(c, ErrorSystemError, "failed to query user")
		}
		return
	}
	for i := range user.LoginIdentifies {
		value, _ := secretDecryptString(c, user.LoginIdentifies[i].EncryptedValue)
		user.LoginIdentifies[i].IndexID = value
	}

	log.Infof(c, "user %d set apple consumption consent=%v", userID, *req.Consented)
	Success(c, buildDataUserWithDevice(&user, nil))
}
