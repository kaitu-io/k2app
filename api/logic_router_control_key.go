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
)

const routerControlKeyPrefix = "rck_"

// newRouterControlKey 生成 rck_ 前缀 + 32 字节 hex 的账号级路由器控制密钥。
func newRouterControlKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate router control key: %w", err)
	}
	return routerControlKeyPrefix + hex.EncodeToString(buf), nil
}

// HashRouterControlKey 返回 key 的 sha256 hex —— /api/subs 下发给 k2r 的形态。
func HashRouterControlKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// EnsureRouterControlKey 幂等取用户的控制密钥：有则返回既有明文，无则生成落库。
func EnsureRouterControlKey(ctx context.Context, userID uint64) (string, error) {
	var user User
	if err := db.Get().WithContext(ctx).First(&user, userID).Error; err != nil {
		return "", fmt.Errorf("load user: %w", err)
	}
	if user.RouterControlKey != nil && *user.RouterControlKey != "" {
		return *user.RouterControlKey, nil
	}
	return ResetRouterControlKey(ctx, userID)
}

// ResetRouterControlKey 轮换：生成新 key 覆盖旧值（同账号多设备需重复下发同一 key，故存明文）。
func ResetRouterControlKey(ctx context.Context, userID uint64) (string, error) {
	key, err := newRouterControlKey()
	if err != nil {
		return "", err
	}
	result := db.Get().WithContext(ctx).Model(&User{}).Where("id = ?", userID).
		Updates(map[string]interface{}{
			"router_control_key":            key,
			"router_control_key_created_at": time.Now().Unix(),
		})
	if result.Error != nil {
		return "", fmt.Errorf("save router control key: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return "", fmt.Errorf("user %d not found", userID)
	}
	log.Infof(ctx, "reset router control key for user %d", userID)
	return key, nil
}
