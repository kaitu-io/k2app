package center

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/wordgate/qtoolkit/redis"
)

// 浏览器 handoff（spec §6.3）：桌面 / Linux / 安卓 Apple 在系统浏览器里登录，App 轮询领取结果。
// 只做登录，不做绑定。会话存 Redis，TTL 5 分钟；URL 里只有 sessionId，领取须带只在 App 内存里的 pollSecret。

// errSocialHandoffExpired handoff 会话不存在 / 已过期 / 状态不对 / 品牌不符 / pollSecret 错。
var errSocialHandoffExpired = errors.New("social handoff session expired")

const (
	socialHandoffTTL       = 5 * time.Minute
	socialHandoffKeyPrefix = "social:handoff:"

	socialHandoffPending = "pending"
	socialHandoffDone    = "done"
	socialHandoffFailed  = "failed"
)

type socialHandoffSession struct {
	Brand, Provider string
	RawNonce        string // 服务端生成
	PollHash        string // sha256(pollSecret)
	Status          string // "pending" | "done" | "failed"
	UserID          uint64
	ErrCode         ErrorCode
}

func socialHandoffKey(sessionID string) string { return socialHandoffKeyPrefix + sessionID }

// randomHex32 32 字节 crypto/rand，小写 hex。
func randomHex32() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func socialHandoffPollHash(pollSecret string) string {
	sum := sha256.Sum256([]byte(pollSecret))
	return hex.EncodeToString(sum[:])
}

// createSocialHandoff 建 pending 会话，返回 sessionId、pollSecret 与浏览器打开的 url。
func createSocialHandoff(ctx context.Context, brand Brand, provider string) (sessionID, pollSecret, openURL string, err error) {
	if sessionID, err = randomHex32(); err != nil {
		return "", "", "", err
	}
	if pollSecret, err = randomHex32(); err != nil {
		return "", "", "", err
	}
	rawNonce, err := randomHex32()
	if err != nil {
		return "", "", "", err
	}
	s := socialHandoffSession{
		Brand:    string(brand),
		Provider: provider,
		RawNonce: rawNonce,
		PollHash: socialHandoffPollHash(pollSecret),
		Status:   socialHandoffPending,
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "", "", "", err
	}
	if err := redis.Client().Set(ctx, socialHandoffKey(sessionID), b, socialHandoffTTL).Err(); err != nil {
		return "", "", "", fmt.Errorf("store handoff session: %w", err)
	}
	openURL = brand.Config().BaseURL + "/auth/social?session=" + url.QueryEscape(sessionID) +
		"&provider=" + url.QueryEscape(provider) + "&nonce=" + socialNonceHash(rawNonce)
	return sessionID, pollSecret, openURL, nil
}

// loadSocialHandoff 读会话；不存在 → errSocialHandoffExpired。
func loadSocialHandoff(ctx context.Context, sessionID string) (*socialHandoffSession, error) {
	raw, err := redis.Client().Get(ctx, socialHandoffKey(sessionID)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, errSocialHandoffExpired
	}
	if err != nil {
		return nil, fmt.Errorf("load handoff session: %w", err)
	}
	var s socialHandoffSession
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("decode handoff session: %w", err)
	}
	return &s, nil
}

// updateSocialHandoff 写回会话并保留剩余 TTL；XX 保证已过期的会话不会被无 TTL 地重建。
func updateSocialHandoff(ctx context.Context, sessionID string, s *socialHandoffSession) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	ok, err := redis.Client().SetXX(ctx, socialHandoffKey(sessionID), b, goredis.KeepTTL).Result()
	if err != nil {
		return fmt.Errorf("update handoff session: %w", err)
	}
	if !ok {
		return errSocialHandoffExpired
	}
	return nil
}

// claimSocialHandoff 删除会话；只有删除计数为 1 的调用方算领取成功。
func claimSocialHandoff(ctx context.Context, sessionID string) (bool, error) {
	n, err := redis.Client().Del(ctx, socialHandoffKey(sessionID)).Result()
	if err != nil {
		return false, fmt.Errorf("claim handoff session: %w", err)
	}
	return n == 1, nil
}

// pollSecretMatches 常量时间比对 sha256(pollSecret) 与会话里的 PollHash。
func (s *socialHandoffSession) pollSecretMatches(pollSecret string) bool {
	return subtle.ConstantTimeCompare([]byte(socialHandoffPollHash(pollSecret)), []byte(s.PollHash)) == 1
}
