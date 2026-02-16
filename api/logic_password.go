package center

import (
	"context"
	"regexp"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

const (
	// Password policy
	PasswordMinLength       = 8
	PasswordMaxFailAttempts = 5
	PasswordLockDuration    = time.Hour // 1 hour lockout

	// Rate limiting keys
	PasswordLoginLockPrefix = "auth:password:lock:"
)

var (
	// Password must contain at least one letter and one number
	passwordLetterRegex = regexp.MustCompile(`[a-zA-Z]`)
	passwordNumberRegex = regexp.MustCompile(`[0-9]`)
)

// ValidatePasswordStrength checks if password meets requirements
// Returns error message key if invalid, empty string if valid
func ValidatePasswordStrength(password string) string {
	if len(password) < PasswordMinLength {
		return "password_too_short"
	}
	if !passwordLetterRegex.MatchString(password) {
		return "password_needs_letter"
	}
	if !passwordNumberRegex.MatchString(password) {
		return "password_needs_number"
	}
	return ""
}

// IsAccountLocked checks if user account is locked due to failed attempts
func IsAccountLocked(user *User) bool {
	if user.PasswordLockedUntil == 0 {
		return false
	}
	return time.Now().Unix() < user.PasswordLockedUntil
}

// RecordFailedPasswordAttempt increments failed attempts and locks if threshold reached
func RecordFailedPasswordAttempt(ctx context.Context, user *User) error {
	user.PasswordFailedAttempts++

	if user.PasswordFailedAttempts >= PasswordMaxFailAttempts {
		user.PasswordLockedUntil = time.Now().Add(PasswordLockDuration).Unix()
		log.Warnf(ctx, "user %d locked until %d due to %d failed password attempts",
			user.ID, user.PasswordLockedUntil, user.PasswordFailedAttempts)
	}

	return db.Get().Save(user).Error
}

// ResetFailedPasswordAttempts resets the counter after successful login
func ResetFailedPasswordAttempts(ctx context.Context, user *User) error {
	if user.PasswordFailedAttempts == 0 && user.PasswordLockedUntil == 0 {
		return nil // Nothing to reset
	}
	user.PasswordFailedAttempts = 0
	user.PasswordLockedUntil = 0
	return db.Get().Save(user).Error
}

// HasPasswordSet checks if user has set a password
func HasPasswordSet(user *User) bool {
	return user.PasswordHash != ""
}
