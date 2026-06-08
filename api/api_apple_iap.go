package center

import (
	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_apple_iap_verify 客户端 StoreKit 购买完成后上报 transactionId。
// 服务端向 Apple 认证 API 复核（verifyAndGrantTransaction 内），通过后入账并返回刷新后的用户信息。
// 这是 Apple 入账的"已鉴权"入口：在这里把 originalTransactionId ↔ userID 绑定。
func api_apple_iap_verify(c *gin.Context) {
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "not logged in")
		return
	}

	var req struct {
		TransactionID string `json:"transactionId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TransactionID == "" {
		Error(c, ErrorInvalidArgument, "transactionId required")
		return
	}

	if err := verifyAndGrantTransaction(c, userID, req.TransactionID); err != nil {
		log.Errorf(c, "[AppleIAP] verify failed user=%d txn=%s: %v", userID, req.TransactionID, err)
		Error(c, ErrorInvalidOperation, "verification failed")
		return
	}
	log.Infof(c, "[AppleIAP] verify ok user=%d txn=%s", userID, req.TransactionID)

	// 返回刷新后的用户信息（与 /api/user/info 一致的序列化）
	var user User
	if err := db.Get().Preload("InvitedByCode").Preload("LoginIdentifies").Preload("Devices").
		Where(&User{ID: userID}).First(&user).Error; err != nil {
		log.Warnf(c, "apple iap verify: granted but reload user %d failed: %v", userID, err)
		SuccessEmpty(c)
		return
	}
	for i := range user.LoginIdentifies {
		value, _ := secretDecryptString(c, user.LoginIdentifies[i].EncryptedValue)
		user.LoginIdentifies[i].IndexID = value
	}
	Success(c, buildDataUserWithDevice(&user, nil))
}
