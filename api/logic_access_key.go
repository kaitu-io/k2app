package center

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

const accessKeyPrefix = "ktu_"

// GenerateAccessKey creates a new access key for a user.
// Returns the plaintext key (shown once) and updates the user's DB record with the hash.
func GenerateAccessKey(ctx context.Context, userID uint64) (plaintext string, err error) {
	// Generate 32 random bytes → 64 hex chars
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	plaintext = accessKeyPrefix + hex.EncodeToString(randomBytes) // "ktu_" + 64 hex = 68 chars total
	hash := HashAccessKey(plaintext)

	now := time.Now().Unix()
	result := db.Get().Model(&User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"access_key":            hash,
		"access_key_created_at": now,
	})
	if result.Error != nil {
		return "", fmt.Errorf("failed to save access key hash: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return "", fmt.Errorf("user %d not found", userID)
	}

	log.Infof(ctx, "generated new access key for user %d", userID)
	return plaintext, nil
}

// RevokeAccessKey removes a user's access key.
func RevokeAccessKey(ctx context.Context, userID uint64) error {
	result := db.Get().Model(&User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"access_key":            gorm.Expr("NULL"),
		"access_key_created_at": 0,
	})
	if result.Error != nil {
		return fmt.Errorf("failed to revoke access key: %w", result.Error)
	}
	log.Infof(ctx, "revoked access key for user %d", userID)
	return nil
}

// HashAccessKey returns the SHA-256 hex digest of a plaintext access key.
func HashAccessKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}
