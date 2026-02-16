package cloudprovider

import (
	"fmt"
)

// ProviderConfig holds configuration for creating a provider
type ProviderConfig struct {
	Provider        string
	AccessKeyID     string
	AccessKeySecret string
	SecretAccessKey string
	Region          string
	// Bandwagon: supports multiple instances
	Instances []BandwagonInstanceConfig
	// Legacy Bandwagon config (deprecated)
	VEID   string
	APIKey string
}

// NewProvider creates a provider based on the provider type
func NewProvider(cfg ProviderConfig) (Provider, error) {
	switch cfg.Provider {
	case ProviderBandwagon:
		return newBandwagonProvider(cfg)

	case ProviderAWSLightsail:
		if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
			return nil, fmt.Errorf("aws_lightsail requires access_key_id and secret_access_key")
		}
		// If region is empty, create multi-region provider to manage all regions
		if cfg.Region == "" {
			return NewMultiRegionAWSLightsailProvider(cfg.AccessKeyID, cfg.SecretAccessKey)
		}
		return NewAWSLightsailProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.Region)

	case ProviderAliyunSWAS:
		if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
			return nil, fmt.Errorf("aliyun_swas requires access_key_id and access_key_secret")
		}
		// If region is empty, create multi-region provider to manage all domestic regions
		if cfg.Region == "" {
			return NewMultiRegionAliyunSWASProvider(cfg.AccessKeyID, cfg.AccessKeySecret)
		}
		return NewAliyunSWASProvider(cfg.AccessKeyID, cfg.AccessKeySecret, cfg.Region), nil

	case ProviderAlibabaSWAS:
		if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
			return nil, fmt.Errorf("alibaba_swas requires access_key_id and access_key_secret")
		}
		// If region is empty, create multi-region provider to manage all international regions
		if cfg.Region == "" {
			return NewMultiRegionAlibabaSWASProvider(cfg.AccessKeyID, cfg.AccessKeySecret)
		}
		return NewAlibabaSWASProvider(cfg.AccessKeyID, cfg.AccessKeySecret, cfg.Region), nil

	case ProviderTencentLighthouse:
		// Tencent uses AccessKeyID for SecretId and AccessKeySecret for SecretKey
		secretId := cfg.AccessKeyID
		secretKey := cfg.AccessKeySecret
		if secretId == "" || secretKey == "" {
			return nil, fmt.Errorf("tencent_lighthouse requires access_key_id (SecretId) and access_key_secret (SecretKey)")
		}
		if cfg.Region == "" {
			return NewMultiRegionTencentLighthouseProvider(secretId, secretKey)
		}
		return NewTencentLighthouseProvider(secretId, secretKey, cfg.Region)

	case ProviderQCloudLighthouse:
		// QCloud uses AccessKeyID for SecretId and AccessKeySecret for SecretKey
		secretId := cfg.AccessKeyID
		secretKey := cfg.AccessKeySecret
		if secretId == "" || secretKey == "" {
			return nil, fmt.Errorf("qcloud_lighthouse requires access_key_id (SecretId) and access_key_secret (SecretKey)")
		}
		if cfg.Region == "" {
			return NewMultiRegionQCloudLighthouseProvider(secretId, secretKey)
		}
		return NewQCloudLighthouseProvider(secretId, secretKey, cfg.Region)

	case ProviderSSHStandalone:
		// SSH Standalone requires DB access, which is passed separately
		// Use NewSSHStandaloneProvider() directly instead of this factory
		return nil, fmt.Errorf("ssh_standalone provider requires NewSSHStandaloneProvider(accountName, db)")

	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider)
	}
}

// newBandwagonProvider creates a Bandwagon provider with support for multi-instance config
func newBandwagonProvider(cfg ProviderConfig) (Provider, error) {
	// Prefer new Instances config
	if len(cfg.Instances) > 0 {
		return NewMultiBandwagonProvider(cfg.Instances), nil
	}

	// Fall back to legacy single veid/api_key config
	if cfg.VEID != "" && cfg.APIKey != "" {
		return NewBandwagonProvider(cfg.VEID, cfg.APIKey), nil
	}

	return nil, fmt.Errorf("bandwagon requires either instances[] or veid+api_key")
}
