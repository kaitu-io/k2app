package center

import (
	"context"
	"time"

	"github.com/trustelem/zxcvbn"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

const (
	// PasswordMinLength is the hard length floor.
	PasswordMinLength = 10
	// PasswordMinZxcvbnScore: 0=very weak, 4=very strong. 3 corresponds to
	// "safely unguessable: moderate protection from offline slow-hash attack"
	// per zxcvbn's documentation — the right floor for a consumer login form.
	PasswordMinZxcvbnScore = 3

	PasswordMaxFailAttempts = 5
	PasswordLockDuration    = time.Hour

	PasswordLoginLockPrefix = "auth:password:lock:"
)

// ValidatePasswordStrength returns the empty string when the password is
// acceptable. Otherwise it returns a stable enum string identifying the
// failure mode. userInputs (typically [email]) are extra tokens zxcvbn
// penalizes when found inside the password — pass nil if unavailable.
func ValidatePasswordStrength(password string, userInputs []string) string {
	if len(password) < PasswordMinLength {
		return "password_too_short"
	}
	result := zxcvbn.PasswordStrength(password, userInputs)
	if result.Score < PasswordMinZxcvbnScore {
		return "password_too_weak"
	}
	return ""
}

// IsAccountLocked reports whether the user's account is currently locked due to too many failed password attempts.
func IsAccountLocked(user *User) bool {
	if user.PasswordLockedUntil == 0 {
		return false
	}
	return time.Now().Unix() < user.PasswordLockedUntil
}

// RecordFailedPasswordAttempt increments the user's failed-attempt counter and locks the account when the threshold is hit.
func RecordFailedPasswordAttempt(ctx context.Context, user *User) error {
	user.PasswordFailedAttempts++

	if user.PasswordFailedAttempts >= PasswordMaxFailAttempts {
		user.PasswordLockedUntil = time.Now().Add(PasswordLockDuration).Unix()
		log.Warnf(ctx, "user %d locked until %d due to %d failed password attempts",
			user.ID, user.PasswordLockedUntil, user.PasswordFailedAttempts)
	}

	return db.Get().Save(user).Error
}

// ResetFailedPasswordAttempts clears the counter and any active lock — call on successful authentication.
func ResetFailedPasswordAttempts(ctx context.Context, user *User) error {
	if user.PasswordFailedAttempts == 0 && user.PasswordLockedUntil == 0 {
		return nil
	}
	user.PasswordFailedAttempts = 0
	user.PasswordLockedUntil = 0
	return db.Get().Save(user).Error
}

// HasPasswordSet reports whether the user has a password hash on record.
func HasPasswordSet(user *User) bool {
	return user.PasswordHash != ""
}
