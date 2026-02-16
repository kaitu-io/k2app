package center

import (
	"encoding/base64"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// DataECHConfig ECH 配置响应数据
type DataECHConfig struct {
	Version       string `json:"version"`               // ECH 协议版本（fe0d = draft-18）
	ECHConfigList string `json:"echConfigList"`         // Base64 编码的 ECHConfigList 二进制
	Expiry        int64  `json:"expiry"`                // 配置过期时间（Unix timestamp）
	RefreshHint   int64  `json:"refreshHint,omitempty"` // 建议刷新间隔（秒）
}

// api_fetch_ech_config 获取 ECH 配置（公开接口）
//
// @Summary 获取 ECH 配置
// @Description 返回当前有效的 ECHConfigList，供客户端用于 TLS 握手时的 ECH 加密
// @Tags ECH
// @Produce json
// @Success 200 {object} Response[DataECHConfig]
// @Router /api/ech/config [get]
func api_fetch_ech_config(c *gin.Context) {
	log.Infof(c, "request to get ECH config")

	if err := EnsureActiveECHKeyExists(c); err != nil {
		log.Errorf(c, "failed to ensure active ECH key: %v", err)
		Error(c, ErrorServiceUnavailable, "ech config not available")
		return
	}

	activeKey, err := GetActiveECHKey(c)
	if err != nil {
		log.Errorf(c, "failed to query active ECH key: %v", err)
		Error(c, ErrorSystemError, "failed to get ech config")
		return
	}

	// 解密 ECHConfig
	_, _, echConfig, err := DecryptECHKeyMaterial(c, activeKey)
	if err != nil {
		log.Errorf(c, "failed to decrypt ECH key material: %v", err)
		Error(c, ErrorSystemError, "failed to decrypt ech config")
		return
	}

	// 构建 ECHConfigList（当前只包含一个 config）
	echConfigList := buildECHConfigList([][]byte{echConfig})

	log.Infof(c, "successfully retrieved ECH config, config_id=%d", activeKey.ConfigID)
	Success(c, &DataECHConfig{
		Version:       "fe0d",
		ECHConfigList: base64.StdEncoding.EncodeToString(echConfigList),
		Expiry:        activeKey.ExpiresAt,
		RefreshHint:   24 * 3600, // 建议每天刷新一次
	})
}

// DataECHKeyItem 内部 ECH 密钥数据项
type DataECHKeyItem struct {
	ConfigID   uint8  `json:"configId"`   // ECH Config ID（1-255）
	PrivateKey string `json:"privateKey"` // Base64 编码的 X25519 私钥（32 字节）
	PublicKey  string `json:"publicKey"`  // Base64 编码的 X25519 公钥（32 字节）
	Status     string `json:"status"`     // 密钥状态（active/grace_period）
	KEMId      uint16 `json:"kemId"`      // KEM 算法 ID
	KDFId      uint16 `json:"kdfId"`      // KDF 算法 ID
	AEADId     uint16 `json:"aeadId"`     // AEAD 算法 ID
	ExpiresAt  int64  `json:"expiresAt"`  // 密钥过期时间
}

// api_slave_fetch_ech_keys 获取 ECH 解密密钥（内部接口）
//
// @Summary 获取 ECH 解密密钥
// @Description 返回所有可用于解密的 ECH 私钥（active + grace_period），供反向代理层使用
// @Tags Internal
// @Produce json
// @Security SlaveAuth
// @Success 200 {object} Response[ListResult[DataECHKeyItem]]
// @Router /slave/ech/keys [get]
func api_slave_fetch_ech_keys(c *gin.Context) {
	log.Infof(c, "request to fetch ECH keys for decryption")

	if err := EnsureActiveECHKeyExists(c); err != nil {
		log.Errorf(c, "failed to ensure active ECH key: %v", err)
		Error(c, ErrorSystemError, "failed to ensure ech keys")
		return
	}

	keys, err := GetDecryptableECHKeys(c)
	if err != nil {
		log.Errorf(c, "failed to query ECH keys: %v", err)
		Error(c, ErrorSystemError, "failed to query ech keys")
		return
	}

	result := make([]DataECHKeyItem, 0, len(keys))
	for _, key := range keys {
		// 解密密钥材料
		privateKey, publicKey, _, err := DecryptECHKeyMaterial(c, &key)
		if err != nil {
			log.Errorf(c, "failed to decrypt key material for config_id=%d: %v", key.ConfigID, err)
			continue
		}

		result = append(result, DataECHKeyItem{
			ConfigID:   key.ConfigID,
			PrivateKey: base64.StdEncoding.EncodeToString(privateKey),
			PublicKey:  base64.StdEncoding.EncodeToString(publicKey),
			Status:     string(key.Status),
			KEMId:      key.KEMId,
			KDFId:      key.KDFId,
			AEADId:     key.AEADId,
			ExpiresAt:  key.ExpiresAt,
		})
	}

	log.Infof(c, "successfully retrieved %d ECH keys", len(result))
	ItemsAll(c, result)
}
