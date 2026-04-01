package center

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"

	gormdb "gorm.io/gorm"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// Crockford Base32 alphabet — excludes I, L, O, U to avoid visual confusion.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// GenerateShortCode generates a unique 8-char Crockford Base32 code.
// Retries up to 10 times on collision.
func GenerateShortCode(ctx context.Context) (string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		code := make([]byte, 8)
		for i := range code {
			n, err := rand.Int(rand.Reader, big.NewInt(32))
			if err != nil {
				return "", fmt.Errorf("failed to generate random byte: %w", err)
			}
			code[i] = crockfordAlphabet[n.Int64()]
		}
		codeStr := string(code)

		// Check uniqueness
		var count int64
		if err := db.Get().Model(&LicenseKey{}).Where("code = ?", codeStr).Count(&count).Error; err != nil {
			return "", fmt.Errorf("failed to check code uniqueness: %w", err)
		}
		if count == 0 {
			return codeStr, nil
		}
		log.Warnf(ctx, "[LICENSE_KEY] code collision on attempt %d: %s", attempt+1, codeStr)
	}
	return "", fmt.Errorf("failed to generate unique code after 10 attempts")
}

// NormalizeCode normalizes user input to uppercase for lookup.
func NormalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// IsExpired reports whether the key has passed its expiry time.
func (k *LicenseKey) IsExpired() bool {
	return k.ExpiresAt < time.Now().Unix()
}

// MatchLicenseKey checks whether user is eligible to redeem this key.
func MatchLicenseKey(key *LicenseKey, user *User) bool {
	matcher := key.RecipientMatcher
	if key.Batch != nil {
		matcher = key.Batch.RecipientMatcher
	}
	switch matcher {
	case "all":
		return true
	case "never_paid":
		return user.IsFirstOrderDone == nil || !*user.IsFirstOrderDone
	default:
		return false
	}
}

// RedeemLicenseKey validates, consumes, and grants plan access to the user.
// Runs inside a DB transaction.
func RedeemLicenseKey(ctx context.Context, code string, userID uint64) (*LicenseKey, *UserProHistory, error) {
	code = NormalizeCode(code)
	var history *UserProHistory
	var key *LicenseKey

	err := db.Get().Transaction(func(tx *gormdb.DB) error {
		// 1. Load key
		var k LicenseKey
		if err := tx.Preload("Batch").Where("code = ?", code).First(&k).Error; err != nil {
			return ErrLicenseKeyNotFound
		}
		if k.IsUsed {
			return ErrLicenseKeyUsed
		}
		if k.IsExpired() {
			return ErrLicenseKeyExpired
		}

		// 2. Anti-abuse: one key per user ever
		var existingCount int64
		if err := tx.Model(&LicenseKey{}).Where("used_by_user_id = ?", userID).Count(&existingCount).Error; err != nil {
			return err
		}
		if existingCount > 0 {
			return ErrLicenseKeyAlreadyRedeemed
		}

		// 3. Load user
		var user User
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}

		// 4. Check eligibility
		if !MatchLicenseKey(&k, &user) {
			return ErrLicenseKeyNotMatch
		}

		// 5. Atomic consume
		result := tx.Model(&LicenseKey{}).
			Where("code = ? AND is_used = false", code).
			Updates(map[string]any{
				"is_used":         true,
				"used_by_user_id": userID,
				"used_at":         time.Now(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrLicenseKeyUsed
		}

		// 6. Grant plan days
		reason := fmt.Sprintf("礼物码兑换 - %s", k.Code)
		h, err := addProExpiredDays(ctx, tx, &user, VipSystemGrant, k.ID, k.PlanDays, reason)
		if err != nil {
			return err
		}

		k.IsUsed = true
		k.UsedByUserID = &userID
		key = &k
		history = h
		return nil
	})

	return key, history, err
}

// GetLicenseKeyByCode fetches a key by its short code.
func GetLicenseKeyByCode(ctx context.Context, code string) (*LicenseKey, error) {
	code = NormalizeCode(code)
	var key LicenseKey
	if err := db.Get().Where("code = ?", code).First(&key).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

// ConsumeLicenseKey atomically marks a key as used.
// Uses conditional UPDATE to prevent concurrent double-redemption.
func ConsumeLicenseKey(ctx context.Context, tx *gormdb.DB, code string, userID uint64) (*LicenseKey, error) {
	code = NormalizeCode(code)
	var key LicenseKey
	if err := tx.Where("code = ?", code).First(&key).Error; err != nil {
		return nil, ErrLicenseKeyNotFound
	}
	if key.IsUsed {
		return nil, ErrLicenseKeyUsed
	}
	if key.IsExpired() {
		return nil, ErrLicenseKeyExpired
	}

	// 同一用户只能使用一个 LicenseKey
	var existingUseCount int64
	if err := tx.Model(&LicenseKey{}).Where("used_by_user_id = ?", userID).Count(&existingUseCount).Error; err != nil {
		return nil, err
	}
	if existingUseCount > 0 {
		return nil, ErrLicenseKeyAlreadyRedeemed
	}

	now := time.Now()
	result := tx.Model(&LicenseKey{}).
		Where("code = ? AND is_used = false", code).
		Updates(map[string]any{
			"is_used":         true,
			"used_by_user_id": userID,
			"used_at":         now,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrLicenseKeyUsed
	}

	key.IsUsed = true
	key.UsedByUserID = &userID
	key.UsedAt = &now
	return &key, nil
}

