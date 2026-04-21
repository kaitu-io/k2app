package center

import (
	"strings"

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
