package center

import (
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// AdminSetUserPasswordRequest is the body for POST /app/users/:uuid/password.
//
// Admin supplies the new plaintext password (+confirm) and a reason ≥3 chars.
// The plaintext is hashed and immediately discarded; it never enters logs,
// audit metadata, or the error response.
type AdminSetUserPasswordRequest struct {
	Password        string `json:"password"        binding:"required"`
	ConfirmPassword string `json:"confirmPassword" binding:"required"`
	Reason          string `json:"reason"          binding:"required"`
}

// api_admin_set_user_password lets a superadmin set a new password for any
// user. Mirrors api_set_password but does not require the old password (no
// user session is present) and writes an audit log + admin-flavored
// notification email.
func api_admin_set_user_password(c *gin.Context) {
	uuid := c.Param("uuid")

	var req AdminSetUserPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	if req.Password != req.ConfirmPassword {
		Error(c, ErrorInvalidArgument, "passwords do not match")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if len(reason) < 3 {
		Error(c, ErrorInvalidArgument, "reason too short")
		return
	}

	var user User
	if err := db.Get().Preload("LoginIdentifies").Where(&User{UUID: uuid}).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, ErrorNotFound, "user not found")
			return
		}
		log.Errorf(c, "find user %s failed: %v", uuid, err)
		Error(c, ErrorSystemError, "find user failed")
		return
	}

	userInputs := collectUserInputsForPasswordStrength(c, &user)
	if errKey := ValidatePasswordStrength(req.Password, userInputs); errKey != "" {
		Error(c, ErrorInvalidArgument, errKey)
		return
	}

	hash, err := UserPasswordHash(req.Password)
	if err != nil {
		log.Errorf(c, "hash password for user %s failed: %v", uuid, err)
		Error(c, ErrorSystemError, "hash password failed")
		return
	}

	user.PasswordHash = hash
	user.PasswordFailedAttempts = 0
	user.PasswordLockedUntil = 0
	// Scope the save to the three password columns. `LoginIdentifies` was
	// preloaded for strength-check userInputs; without Select(), GORM would
	// attempt to upsert that association on every reset.
	if err := db.Get().Select("PasswordHash", "PasswordFailedAttempts", "PasswordLockedUntil").Save(&user).Error; err != nil {
		log.Errorf(c, "save password for user %s failed: %v", uuid, err)
		Error(c, ErrorSystemError, "save password failed")
		return
	}

	// Audit log — reason recorded; password value never logged.
	WriteAuditLog(c, "user_admin_reset_password", "user", uuid, map[string]string{
		"reason": reason,
	})

	// Notification — admin-flavored template, fire-and-log on failure.
	meta := AdminResetPasswordMeta{
		ChangeTime: time.Now().Format("2006-01-02 15:04:05"),
		AdminEmail: adminDisplayEmail(c),
	}
	if mailErr := emailToUser(c, int64(user.ID), adminResetPasswordTemplate, meta); mailErr != nil {
		log.Errorf(c, "send admin-reset notification to user %s failed: %v", uuid, mailErr)
	}

	log.Infof(c, "superadmin reset password for user %s; reason=%q", uuid, reason)
	SuccessEmpty(c)
}

// adminDisplayEmail returns the calling admin's plaintext email for display
// purposes (notification email body, NOT audit log — audit log records
// actor_id/actor_uuid via WriteAuditLog already). Best-effort: returns "" on
// any failure, and the email template falls back to "（系统管理员）".
func adminDisplayEmail(c *gin.Context) string {
	actor := ReqUser(c)
	if actor == nil {
		return ""
	}
	// LoginIdentifies may not be preloaded on the auth-context user; load fresh.
	var identifies []LoginIdentify
	if err := db.Get().Where("user_id = ? AND type = ?", actor.ID, "email").Find(&identifies).Error; err != nil {
		return ""
	}
	for i := range identifies {
		email, err := secretDecryptString(c, identifies[i].EncryptedValue)
		if err != nil {
			continue
		}
		return email
	}
	return ""
}
