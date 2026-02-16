package center

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"golang.org/x/crypto/curve25519"
)

const (
	// ECH key active duration (seconds) — how long a key is the "current" key
	// New clients receive this key's ECHConfig. After expiry, transitions to grace_period.
	echKeyValidityDuration = 30 * 24 * 3600 // 30 days
	// Grace period duration (seconds) — how long an expired key remains usable for decryption.
	// Clients who cached an old ECHConfig can still connect during this window.
	// Total decryptable lifetime per key: active + grace = 30 + 180 = 210 days (~7 months).
	// This ensures users who haven't refreshed their config in 6 months can still connect.
	echGracePeriodDuration = 180 * 24 * 3600 // 180 days (6 months)
	// ECH 协议版本
	echVersion = 0xfe0d // draft-18
	// 算法 ID
	kemX25519HKDFSHA256 = 0x0020
	kdfHKDFSHA256       = 0x0001
	aeadAES128GCM       = 0x0001
	aeadChaCha20Poly    = 0x0003
)

// GenerateECHKey 生成新的 ECH 密钥对
func GenerateECHKey(ctx context.Context) (*ECHKey, error) {
	log.Infof(ctx, "generating new ECH key pair")

	// 1. 生成 X25519 密钥对
	publicKey, privateKey, err := generateX25519KeyPair()
	if err != nil {
		log.Errorf(ctx, "failed to generate X25519 key pair: %v", err)
		return nil, err
	}

	// 2. 分配 Config ID
	configID, err := getNextECHConfigID(ctx)
	if err != nil {
		log.Errorf(ctx, "failed to allocate config_id: %v", err)
		return nil, err
	}

	// 3. 构建 ECHConfig 二进制格式
	echConfig, err := buildECHConfig(configID, publicKey)
	if err != nil {
		log.Errorf(ctx, "failed to build ECHConfig: %v", err)
		return nil, err
	}

	// 4. 加密存储（复用现有的 secretEncrypt 函数）
	encPrivate, err := secretEncrypt(ctx, privateKey)
	if err != nil {
		log.Errorf(ctx, "failed to encrypt private key: %v", err)
		return nil, err
	}
	encPublic, err := secretEncrypt(ctx, publicKey)
	if err != nil {
		log.Errorf(ctx, "failed to encrypt public key: %v", err)
		return nil, err
	}
	encConfig, err := secretEncrypt(ctx, echConfig)
	if err != nil {
		log.Errorf(ctx, "failed to encrypt ECHConfig: %v", err)
		return nil, err
	}

	now := time.Now().Unix()
	key := &ECHKey{
		ConfigID:    configID,
		PrivateKey:  base64.StdEncoding.EncodeToString(encPrivate),
		PublicKey:   base64.StdEncoding.EncodeToString(encPublic),
		ECHConfig:   base64.StdEncoding.EncodeToString(encConfig),
		Status:      ECHKeyStatusActive,
		ActivatedAt: now,
		ExpiresAt:   now + echKeyValidityDuration,
		KEMId:       kemX25519HKDFSHA256,
		KDFId:       kdfHKDFSHA256,
		AEADId:      aeadAES128GCM,
	}

	if err := db.Get().Create(key).Error; err != nil {
		log.Errorf(ctx, "failed to save ECH key to database: %v", err)
		return nil, err
	}

	log.Infof(ctx, "successfully created ECH key with config_id=%d", configID)
	return key, nil
}

// generateX25519KeyPair 生成 X25519 密钥对
func generateX25519KeyPair() (publicKey, privateKey []byte, err error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return nil, nil, fmt.Errorf("failed to read random bytes: %w", err)
	}

	// X25519 密钥格式化（clamp）
	// 参考: https://www.rfc-editor.org/rfc/rfc7748#section-5
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	var pub [32]byte
	curve25519.ScalarBaseMult(&pub, &priv)

	return pub[:], priv[:], nil
}

// getNextECHConfigID 获取下一个可用的 Config ID
// 规则：从 1 开始累加到 255，然后循环回 1
// 确保不与当前活跃或 grace_period 的 ID 冲突
func getNextECHConfigID(ctx context.Context) (uint8, error) {
	var usedIDs []uint8
	err := db.Get().Model(&ECHKey{}).
		Where("status IN ?", []ECHKeyStatus{ECHKeyStatusActive, ECHKeyStatusGracePeriod}).
		Where("deleted_at IS NULL").
		Pluck("config_id", &usedIDs).Error
	if err != nil {
		return 0, err
	}

	usedSet := make(map[uint8]bool)
	for _, id := range usedIDs {
		usedSet[id] = true
	}

	// 获取最后使用的 ID 作为起点
	var lastID uint8
	db.Get().Model(&ECHKey{}).
		Where("deleted_at IS NULL").
		Order("created_at DESC").
		Limit(1).
		Pluck("config_id", &lastID)

	// 从 lastID+1 开始查找可用 ID
	for i := uint8(1); i <= 255; i++ {
		candidate := ((lastID + i - 1) % 255) + 1 // 1-255 循环
		if !usedSet[candidate] {
			log.Debugf(ctx, "allocated config_id=%d", candidate)
			return candidate, nil
		}
	}

	return 0, errors.New("no available config_id (all 255 IDs in use)")
}

// buildECHConfig 构建符合 TLS 1.3 扩展的 ECHConfig 二进制
// Wire Format (draft-ietf-tls-esni-18):
//
//	struct {
//	    uint16 version;              // 0xfe0d
//	    uint16 length;
//	    uint8 config_id;
//	    uint16 kem_id;
//	    opaque public_key<0..2^16-1>;
//	    uint16 cipher_suites_length;
//	    HPKECipherSuite cipher_suites<4..2^16-4>;
//	    uint8 maximum_name_length;
//	    opaque public_name<1..255>;
//	    opaque extensions<0..2^16-1>;
//	} ECHConfig;
func buildECHConfig(configID uint8, publicKey []byte) ([]byte, error) {
	buf := new(bytes.Buffer)

	// ECHConfig 内容（先写入临时 buffer 计算长度）
	content := new(bytes.Buffer)

	// config_id (1 byte)
	content.WriteByte(configID)

	// kem_id (2 bytes) - X25519 = 0x0020
	binary.Write(content, binary.BigEndian, uint16(kemX25519HKDFSHA256))

	// public_key length + data
	binary.Write(content, binary.BigEndian, uint16(len(publicKey)))
	content.Write(publicKey)

	// cipher_suites: 支持 AES-128-GCM 和 ChaCha20Poly1305
	cipherSuites := []struct {
		kdfID  uint16
		aeadID uint16
	}{
		{kdfHKDFSHA256, aeadAES128GCM},     // HKDF-SHA256 + AES-128-GCM
		{kdfHKDFSHA256, aeadChaCha20Poly},  // HKDF-SHA256 + ChaCha20Poly1305
	}

	// cipher_suites length
	binary.Write(content, binary.BigEndian, uint16(len(cipherSuites)*4))
	for _, suite := range cipherSuites {
		binary.Write(content, binary.BigEndian, suite.kdfID)
		binary.Write(content, binary.BigEndian, suite.aeadID)
	}

	// maximum_name_length (1 byte) - 通常 255
	content.WriteByte(255)

	// public_name - 使用通用名称（客户端实际 SNI 与此无关）
	publicName := []byte("cloudflare-ech.com")
	content.WriteByte(uint8(len(publicName)))
	content.Write(publicName)

	// extensions (empty)
	binary.Write(content, binary.BigEndian, uint16(0))

	// 写入 ECHConfig 头部
	// version (2 bytes) - 0xfe0d
	binary.Write(buf, binary.BigEndian, uint16(echVersion))

	// length (2 bytes)
	binary.Write(buf, binary.BigEndian, uint16(content.Len()))

	// content
	buf.Write(content.Bytes())

	return buf.Bytes(), nil
}

// buildECHConfigList 将多个 ECHConfig 打包为 ECHConfigList
func buildECHConfigList(configs [][]byte) []byte {
	buf := new(bytes.Buffer)

	// 计算总长度
	totalLen := 0
	for _, cfg := range configs {
		totalLen += len(cfg)
	}

	// ECHConfigList length (2 bytes)
	binary.Write(buf, binary.BigEndian, uint16(totalLen))

	// 按顺序写入每个 ECHConfig
	for _, cfg := range configs {
		buf.Write(cfg)
	}

	return buf.Bytes()
}

// EnsureActiveECHKeyExists checks for an active ECH key and generates one if missing.
// Idempotent: if a key already exists, this is a no-op.
// If concurrent calls both detect "no key" and generate, both keys are valid
// (GetDecryptableECHKeys returns all active keys, GetActiveECHKey picks latest).
func EnsureActiveECHKeyExists(ctx context.Context) error {
	_, err := GetActiveECHKey(ctx)
	if err == nil {
		return nil
	}
	if !util.DbIsNotFoundErr(err) {
		return err
	}
	log.Infof(ctx, "no active ECH key found, generating new one")
	_, err = GenerateECHKey(ctx)
	return err
}

// GetActiveECHKey 获取当前活跃的 ECH 密钥
func GetActiveECHKey(ctx context.Context) (*ECHKey, error) {
	var key ECHKey
	err := db.Get().
		Where("status = ?", ECHKeyStatusActive).
		Where("deleted_at IS NULL").
		Order("activated_at DESC").
		First(&key).Error
	if err != nil {
		return nil, err
	}
	return &key, nil
}

// GetDecryptableECHKeys 获取所有可用于解密的 ECH 密钥（active + grace_period）
func GetDecryptableECHKeys(ctx context.Context) ([]ECHKey, error) {
	var keys []ECHKey
	err := db.Get().
		Where("status IN ?", []ECHKeyStatus{ECHKeyStatusActive, ECHKeyStatusGracePeriod}).
		Where("deleted_at IS NULL").
		Order("status ASC, activated_at DESC"). // active 优先
		Find(&keys).Error
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// DecryptECHKeyMaterial 解密 ECH 密钥材料
func DecryptECHKeyMaterial(ctx context.Context, key *ECHKey) (privateKey, publicKey, echConfig []byte, err error) {
	// 解密私钥
	encPrivate, err := base64.StdEncoding.DecodeString(key.PrivateKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode private key: %w", err)
	}
	privateKey, err = secretDecrypt(ctx, encPrivate)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decrypt private key: %w", err)
	}

	// 解密公钥
	encPublic, err := base64.StdEncoding.DecodeString(key.PublicKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode public key: %w", err)
	}
	publicKey, err = secretDecrypt(ctx, encPublic)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decrypt public key: %w", err)
	}

	// 解密 ECHConfig
	encConfig, err := base64.StdEncoding.DecodeString(key.ECHConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode ECHConfig: %w", err)
	}
	echConfig, err = secretDecrypt(ctx, encConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decrypt ECHConfig: %w", err)
	}

	return privateKey, publicKey, echConfig, nil
}

// RotateECHKeys 执行 ECH 密钥轮换
// 1. 将过期的 active 密钥转为 grace_period
// 2. 将超过 grace period 的密钥标记为 retired
// 3. 如果没有 active 密钥，生成新的
// 4. 清理过期的 retired 密钥
func RotateECHKeys(ctx context.Context) error {
	log.Infof(ctx, "starting ECH key rotation")

	now := time.Now().Unix()

	// 1. 将过期的 active 密钥转为 grace_period
	result := db.Get().Model(&ECHKey{}).
		Where("status = ?", ECHKeyStatusActive).
		Where("expires_at <= ?", now).
		Updates(map[string]interface{}{
			"status":     ECHKeyStatusGracePeriod,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		log.Errorf(ctx, "failed to transition expired keys to grace_period: %v", result.Error)
		return result.Error
	}
	if result.RowsAffected > 0 {
		log.Infof(ctx, "transitioned %d keys to grace_period", result.RowsAffected)
	}

	// 2. 将超过 grace period 的密钥标记为 retired
	retiredAt := now
	result = db.Get().Model(&ECHKey{}).
		Where("status = ?", ECHKeyStatusGracePeriod).
		Where("expires_at + ? <= ?", echGracePeriodDuration, now).
		Updates(map[string]interface{}{
			"status":     ECHKeyStatusRetired,
			"retired_at": retiredAt,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		log.Errorf(ctx, "failed to retire old keys: %v", result.Error)
		return result.Error
	}
	if result.RowsAffected > 0 {
		log.Infof(ctx, "retired %d old keys", result.RowsAffected)
	}

	// 3. 检查是否需要生成新密钥
	var activeCount int64
	db.Get().Model(&ECHKey{}).
		Where("status = ?", ECHKeyStatusActive).
		Where("deleted_at IS NULL").
		Count(&activeCount)

	if activeCount == 0 {
		log.Infof(ctx, "no active ECH key found, generating new one")
		_, err := GenerateECHKey(ctx)
		if err != nil {
			log.Errorf(ctx, "failed to generate new ECH key: %v", err)
			return err
		}
		log.Infof(ctx, "new ECH key generated successfully")
	}

	// 4. Clean up retired keys older than 30 days (soft delete)
	cleanupThreshold := now - 30*24*3600
	result = db.Get().
		Where("status = ?", ECHKeyStatusRetired).
		Where("retired_at <= ?", cleanupThreshold).
		Delete(&ECHKey{})
	if result.Error != nil {
		log.Warnf(ctx, "failed to cleanup old retired keys: %v", result.Error)
		// 不返回错误，清理失败不影响主流程
	} else if result.RowsAffected > 0 {
		log.Infof(ctx, "cleaned up %d old retired keys", result.RowsAffected)
	}

	log.Infof(ctx, "ECH key rotation completed successfully")
	return nil
}
