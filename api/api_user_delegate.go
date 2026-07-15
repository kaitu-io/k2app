package center

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// DataDelegateInfo is the response shape for GET /api/user/delegate.
// Returned as null when the user has no delegate set.
type DataDelegateInfo struct {
	Email string `json:"email"`
	SetAt int64  `json:"setAt"`
}

// api_get_delegate returns the current user's delegate payer info, or null if unset.
// Response shape: { email: string, setAt: int64 } | null
func api_get_delegate(c *gin.Context) {
	user := ReqUser(c)
	if user == nil {
		Error(c, ErrorNotLogin, "not logged in")
		return
	}

	if user.DelegateID == nil || *user.DelegateID == 0 {
		Success[DataDelegateInfo](c, nil)
		return
	}

	var li LoginIdentify
	err := db.Get().Where("user_id = ? AND type = ?", *user.DelegateID, "email").First(&li).Error
	if err == gorm.ErrRecordNotFound {
		// Dangling delegate_id — stub user has no email. Treat as unset.
		log.Warnf(c, "delegate_id %d has no email login_identify for user %d", *user.DelegateID, user.ID)
		Success[DataDelegateInfo](c, nil)
		return
	}
	if err != nil {
		log.Errorf(c, "failed to load delegate login_identify for user %d: %v", user.ID, err)
		Error(c, ErrorSystemError, "failed to load delegate")
		return
	}

	email, err := secretDecryptString(c, li.EncryptedValue)
	if err != nil {
		log.Errorf(c, "failed to decrypt delegate email: %v", err)
		Error(c, ErrorSystemError, "failed to decrypt delegate email")
		return
	}

	Success(c, &DataDelegateInfo{
		Email: strings.ToLower(email),
		SetAt: user.UpdatedAt.Unix(),
	})
}

// PutDelegateRequest is the body for PUT /api/user/delegate
type PutDelegateRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// api_put_delegate sets (or overwrites) the current user's delegate payer.
// If the given email doesn't already exist, a stub user is created.
// Overwrite is unconditional — the caller's previous delegate_id (if any) is replaced.
func api_put_delegate(c *gin.Context) {
	user := ReqUser(c)
	if user == nil {
		Error(c, ErrorNotLogin, "not logged in")
		return
	}

	var req PutDelegateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request: "+err.Error())
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))

	// Reject self-invite
	selfEmail, _ := getUserEmail(c, user.ID)
	if strings.EqualFold(selfEmail, email) {
		Error(c, ErrorInvalidArgument, "cannot set yourself as delegate")
		return
	}

	indexID := secretHashIt(c, []byte(email))

	// 限定当前请求品牌：委托代付人必须与付费人同品牌注册（跨品牌账号体系独立，
	// 不应互相成为代付关系）。
	brand := ReqBrand(c)
	var delegateUserID uint64
	var existingLI LoginIdentify
	err := db.Get().Where("type = ? AND index_id = ? AND brand = ?", "email", indexID, string(brand)).First(&existingLI).Error
	switch {
	case err == gorm.ErrRecordNotFound:
		// Create stub user via LoginIdentify cascade
		encEmail, encErr := secretEncryptString(c, email)
		if encErr != nil {
			log.Errorf(c, "failed to encrypt delegate email: %v", encErr)
			Error(c, ErrorSystemError, "failed to encrypt email")
			return
		}
		newLI := LoginIdentify{
			Type:           "email",
			IndexID:        indexID,
			EncryptedValue: encEmail,
			Brand:          string(brand),
			User: &User{
				UUID:      generateId("user"),
				ExpiredAt: 0,
				Brand:     string(brand),
			},
		}
		if err := db.Get().Create(&newLI).Error; err != nil {
			log.Errorf(c, "failed to create delegate stub for %s: %v", email, err)
			Error(c, ErrorSystemError, "failed to create delegate stub")
			return
		}
		delegateUserID = newLI.User.ID
	case err != nil:
		log.Errorf(c, "failed to lookup delegate email %s: %v", email, err)
		Error(c, ErrorSystemError, "failed to lookup delegate")
		return
	default:
		delegateUserID = existingLI.UserID
	}

	// Overwrite user.delegate_id unconditionally
	if err := db.Get().Model(&User{}).Where("id = ?", user.ID).
		Update("delegate_id", delegateUserID).Error; err != nil {
		log.Errorf(c, "failed to update delegate_id for user %d: %v", user.ID, err)
		Error(c, ErrorSystemError, "failed to set delegate")
		return
	}

	log.Infof(c, "user %d set delegate to user %d (email: %s)", user.ID, delegateUserID, email)

	Success(c, &DataDelegateInfo{
		Email: email,
		SetAt: time.Now().Unix(),
	})
}

// api_delete_delegate clears the current user's delegate payer.
//
// Stub users created by api_put_delegate are NOT deleted here — they may be
// shared across users (multiple callers can point to the same stub) and the
// cost of an orphan stub is negligible. Only the current user's delegate_id
// link is cleared.
func api_delete_delegate(c *gin.Context) {
	user := ReqUser(c)
	if user == nil {
		Error(c, ErrorNotLogin, "not logged in")
		return
	}

	if err := db.Get().Model(&User{}).Where("id = ?", user.ID).
		Update("delegate_id", nil).Error; err != nil {
		log.Errorf(c, "failed to clear delegate_id for user %d: %v", user.ID, err)
		Error(c, ErrorSystemError, "failed to clear delegate")
		return
	}

	log.Infof(c, "user %d cleared delegate", user.ID)
	SuccessEmpty(c)
}
