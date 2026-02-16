package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"crypto/sha256"
	"encoding/hex"

	"github.com/kaitu-io/k2app/api/waymaker"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrCaNotFound = errors.New("ca not found")
	ErrCaPassword = errors.New("ca password error")
)

const (
	// defaultIndexSalt should be overridden in config.yml (security.index_salt)
	defaultIndexSalt = "a-very-secret-and-long-default-salt-for-indexing-plz-change-me"
	hashHexLength    = 64 // sha256.Sum256 in hex
)

// 获取全局密钥（32字节）
func getGlobalSecretKey(ctx context.Context) ([]byte, error) {
	log.Debugf(ctx, "getting global secret key")
	key := os.Getenv("SECRET_KEY_HASH")

	if len(key) != 32 {
		log.Errorf(ctx, "SECRET_KEY_HASH is not 32 bytes long")
		return nil, errors.New("SECRET_KEY_HASH must be 32 bytes (AES-256)")
	}

	log.Debugf(ctx, "successfully got global secret key")
	return []byte(key), nil
}

// AES-256-GCM 加密
func secretEncrypt(ctx context.Context, plain []byte) ([]byte, error) {
	log.Debugf(ctx, "encrypting data of length: %d", len(plain))

	secretKey, err := getGlobalSecretKey(ctx)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(secretKey)
	if err != nil {
		log.Errorf(ctx, "failed to create new cipher: %v", err)
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		log.Errorf(ctx, "failed to create new GCM: %v", err)
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		log.Errorf(ctx, "failed to generate nonce: %v", err)
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, plain, nil)
	log.Debugf(ctx, "encryption successful, ciphertext length: %d", len(ciphertext))
	return ciphertext, nil
}

// AES-256-GCM 解密
func secretDecrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	log.Debugf(ctx, "decrypting data of length: %d", len(ciphertext))

	secretKey, err := getGlobalSecretKey(ctx)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(secretKey)
	if err != nil {
		log.Errorf(ctx, "failed to create new cipher: %v", err)
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		log.Errorf(ctx, "failed to create new GCM: %v", err)
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	log.Debugf(ctx, "GCM nonce size: %d", nonceSize)

	if len(ciphertext) < nonceSize {
		log.Errorf(ctx, "ciphertext is too short (len: %d) for nonce size (%d)", len(ciphertext), nonceSize)
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	log.Debugf(ctx, "extracted nonce, remaining ciphertext length: %d", len(ciphertext))

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		log.Errorf(ctx, "failed to open GCM ciphertext: %v", err)
		return nil, err
	}

	log.Debugf(ctx, "decryption successful, plaintext length: %d", len(plaintext))
	return plaintext, nil
}

// TODO migrate old data from wgcenter, then delete
func secretEncryptString(ctx context.Context, plain string) (string, error) {
	// enc, err := secretEncrypt(ctx, []byte(plain))
	// if err != nil {
	// 	return "", err
	// }
	// return base64.StdEncoding.EncodeToString(enc), nil

	return plain, nil
}

// TODO migrate old data from wgcenter, then delete
func secretDecryptString(ctx context.Context, ciphertext string) (string, error) {

	// enc, err := base64.StdEncoding.DecodeString(ciphertext)
	// if err != nil {
	// 	log.Warnf(ctx, "failed to decode base64 string: %v", err)
	// 	return "", err
	// }
	// dec, err := secretDecrypt(ctx, enc)
	// if err != nil {
	// 	return "", err
	// }
	// return string(dec), nil

	return ciphertext, nil
}

// secretHashIt creates a deterministic hash for a given plaintext value.
func secretHashIt(ctx context.Context, plain []byte) string {
	log.Debugf(ctx, "hashing data of length: %d", len(plain))
	hasher := sha256.New()
	hasher.Write([]byte(defaultIndexSalt))
	hasher.Write(plain)
	hashBytes := hasher.Sum(nil)
	hashString := hex.EncodeToString(hashBytes)
	log.Debugf(ctx, "hashing successful, hash: %s", hashString)
	return hashString
}

// SetSecret 加密存储 Secret
func SetSecret(ctx context.Context, key string, value []byte) error {
	log.Infof(ctx, "setting secret for key: %s", key)

	enc, err := secretEncrypt(ctx, value)
	if err != nil {
		return err
	}

	encStr := base64.StdEncoding.EncodeToString(enc)
	log.Debugf(ctx, "encrypted and base64 encoded secret for key: %s", key)

	s := Secret{TheKey: key, Value: encStr}
	err = db.Get().Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "the_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&s).Error

	if err != nil {
		log.Errorf(ctx, "failed to save secret to DB for key %s: %v", key, err)
		return err
	}

	log.Infof(ctx, "successfully set secret for key: %s", key)
	return nil
}

// GetSecret 解密读取 Secret
func GetSecret(ctx context.Context, key string) (updatedAt *time.Time, value []byte, err error) {
	log.Infof(ctx, "getting secret for key: %s", key)

	var s Secret
	if err := db.Get().Where("the_key = ?", key).First(&s).Error; err != nil {
		log.Warnf(ctx, "failed to get secret from DB for key %s: %v", key, err)
		return nil, nil, err
	}

	log.Debugf(ctx, "successfully retrieved secret record for key: %s", key)

	enc, err := base64.StdEncoding.DecodeString(s.Value)
	if err != nil {
		log.Errorf(ctx, "failed to decode base64 secret for key %s: %v", key, err)
		return nil, nil, err
	}

	log.Debugf(ctx, "successfully decoded base64 secret for key: %s", key)

	value, err = secretDecrypt(ctx, enc)
	if err != nil {
		log.Errorf(ctx, "failed to decrypt secret for key %s: %v", key, err)
		return nil, nil, err
	}

	log.Infof(ctx, "successfully got secret for key: %s", key)
	return &s.UpdatedAt, value, err
}

// SetCa 加密存储 CA 证书和私钥
func SetCa(ctx context.Context, certPEM, keyPEM []byte) error {
	log.Infof(ctx, "setting CA certificate and key")

	if err := SetSecret(ctx, "ca_cert", certPEM); err != nil {
		log.Errorf(ctx, "failed to set CA cert secret: %v", err)
		return err
	}
	log.Debugf(ctx, "CA cert secret set successfully")

	if err := SetSecret(ctx, "ca_key", keyPEM); err != nil {
		log.Errorf(ctx, "failed to set CA key secret: %v", err)
		return err
	}
	log.Debugf(ctx, "CA key secret set successfully")

	log.Infof(ctx, "CA certificate and key set successfully")
	return nil
}

// GetCa 获取 CA 证书和私钥
func GetCa(ctx context.Context) (certPEM, keyPEM []byte, err error) {
	log.Infof(ctx, "getting CA certificate and key")

	_, certPEM, err = GetSecret(ctx, "ca_cert")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warnf(ctx, "CA certificate not found in DB")
			return nil, nil, ErrCaNotFound
		}
		if err.Error() == "cipher: message authentication failed" || err.Error() == "SECRET_KEY_HASH must be 32 bytes (AES-256)" {
			log.Errorf(ctx, "password error when getting CA cert: %v", err)
			return nil, nil, ErrCaPassword
		}
		log.Errorf(ctx, "failed to get CA cert secret: %v", err)
		return nil, nil, err
	}
	log.Debugf(ctx, "successfully got CA cert secret")

	_, keyPEM, err = GetSecret(ctx, "ca_key")
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warnf(ctx, "CA key not found in DB")
			return nil, nil, ErrCaNotFound
		}
		if err.Error() == "cipher: message authentication failed" {
			log.Errorf(ctx, "password error when getting CA key: %v", err)
			return nil, nil, ErrCaPassword
		}
		log.Errorf(ctx, "failed to get CA key secret: %v", err)
		return nil, nil, err
	}
	log.Debugf(ctx, "successfully got CA key secret")

	log.Infof(ctx, "successfully got CA certificate and key")
	return certPEM, keyPEM, nil
}

// GetDomainCert generates domain certificate and private key on demand
// Default uses k2 protocol's ECDSA CA signature
func GetDomainCert(ctx context.Context, domain string, _ bool) (certPEM, keyPEM []byte, err error) {
	return GetDomainCertForProtocol(ctx, domain, TunnelProtocolK2)
}

// GetDomainCertForProtocol 根据协议即时生成域名证书和私钥
// k2wss 协议：使用 golang ECDSA CA 签名
// k2oc 协议：使用 certtool (GnuTLS) RSA CA 签名（与 wgcenter 一致）
func GetDomainCertForProtocol(ctx context.Context, domain string, protocol TunnelProtocol) (certPEM, keyPEM []byte, err error) {
	// k2oc 协议使用 waymaker certtool 签名
	if protocol == TunnelProtocolK2OC {
		log.Infof(ctx, "generating domain cert for %s using certtool (k2oc)", domain)
		keyStr, certStr, err := waymaker.KeyPairOfDomain(ctx, domain)
		if err != nil {
			log.Errorf(ctx, "failed to generate cert for %s: %v", domain, err)
			return nil, nil, err
		}
		return []byte(certStr), []byte(keyStr), nil
	}

	// k2wss 及其他协议使用 golang ECDSA CA 签名
	log.Infof(ctx, "generating domain cert for %s using ECDSA CA (k2wss)", domain)

	// 生成域名私钥
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Errorf(ctx, "failed to generate private key for %s: %v", domain, err)
		return nil, nil, err
	}

	// 使用 golang ECDSA CA 签名
	certPEM, err = SignDomainCert(ctx, domain, &priv.PublicKey)
	if err != nil {
		log.Errorf(ctx, "failed to sign cert for %s: %v", domain, err)
		return nil, nil, err
	}

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		log.Errorf(ctx, "failed to marshal private key for %s: %v", domain, err)
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	return certPEM, keyPEM, nil
}

func hideEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) < 2 {
		return email
	}
	username := parts[0]
	if len(username) > 3 {
		username = username[0:3] + strings.Repeat("*", len(username)-3)
	}
	domain := parts[1]
	domainParts := strings.Split(domain, ".")
	if len(domainParts) > 1 {
		maskedDomain := domainParts[0][0:1] + strings.Repeat("*", len(domainParts[0])-1)
		return username + "@" + maskedDomain + "." + domainParts[1]
	}
	return username + "@" + strings.Repeat("*", len(domain))
}
