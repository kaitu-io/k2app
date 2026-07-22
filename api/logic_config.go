package center

import (
	"context"
	"fmt"
	"sync"

	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/wordgate-sdk"
)

type ServerConfig struct {
	Port       int    `json:"port"`
	Domain     string `json:"domain"`
	SelfCaSign bool   `json:"selfCaSign"`
}

// JwtConfig JWT 配置结构
type JwtConfig struct {
	Secret             string `json:"secret"`
	AccessTokenExpiry  int64  `json:"accessTokenExpiry"`
	RefreshTokenExpiry int64  `json:"refreshTokenExpiry"`
}

// InviteConfig 邀请奖励配置结构
type InviteConfig struct {
	PurchaseRewardDays        int `json:"purchaseRewardDays"`        // 被邀请人购买奖励天数
	InviterPurchaseRewardDays int `json:"inviterPurchaseRewardDays"` // 普通邀请人购买奖励天数
	MinRewardMonths           int `json:"minRewardMonths"`           // 触发购买奖励的最低套餐月数（首单不足此时长不发奖励）
}

// RetailerDefaultConfig 分销商默认配置结构
type RetailerDefaultConfig struct {
	DefaultCashbackPercent int    `json:"defaultCashbackPercent"` // 默认返现比例（百分比，1-100）
	DefaultCashbackRule    string `json:"defaultCashbackRule"`    // 默认返现规则："first_order" 或 "all_orders"
}

// FileConfig 文件下载配置结构
type FileConfig struct {
	Domain string `json:"domain"`
}

// WordgateConfig Wordgate 配置结构
type WordgateConfig struct {
	AppCode       string
	AppSecret     string
	BaseURL       string
	WebhookSecret string
	WebhookUrl    string
}

// configJwt 获取 JWT 配置
func configJwt(ctx context.Context) JwtConfig {
	cfg := JwtConfig{
		Secret:             viper.GetString("jwt.secret"),
		AccessTokenExpiry:  viper.GetInt64("jwt.access_token_expiry"),
		RefreshTokenExpiry: viper.GetInt64("jwt.refresh_token_expiry"),
	}
	// log.Debugf(ctx, "loading JWT config: %+v", cfg)
	return cfg
}

// configInvite 获取邀请奖励配置
func configInvite(ctx context.Context) InviteConfig {
	purchaseRewardDays := viper.GetInt("invite.purchase_reward_days")
	if purchaseRewardDays == 0 {
		purchaseRewardDays = 7 // 默认7天
	}

	inviterPurchaseRewardDays := viper.GetInt("invite.inviter_purchase_reward_days")
	if inviterPurchaseRewardDays == 0 {
		inviterPurchaseRewardDays = 7 // 默认7天
	}

	// 未配置时默认 12（年付及以上才触发购买奖励）。
	// 显式配置 0 表示关闭门槛（任何首单都发奖励），所以用 IsSet 区分，不能按零值兜底。
	minRewardMonths := 12
	if viper.IsSet("invite.min_reward_months") {
		minRewardMonths = viper.GetInt("invite.min_reward_months")
	}

	cfg := InviteConfig{
		PurchaseRewardDays:        purchaseRewardDays,
		InviterPurchaseRewardDays: inviterPurchaseRewardDays,
		MinRewardMonths:           minRewardMonths,
	}
	log.Debugf(ctx, "loading invite config: %+v", cfg)
	return cfg
}

// configWordgate 获取 Wordgate 配置
func configWordgate(ctx context.Context) WordgateConfig {
	cfg := WordgateConfig{
		AppCode:       viper.GetString("wordgate.app_code"),
		AppSecret:     viper.GetString("wordgate.app_secret"),
		BaseURL:       viper.GetString("wordgate.base_url"),
		WebhookSecret: viper.GetString("wordgate.webhook_secret"),
		WebhookUrl:    viper.GetString("wordgate.webhook_url"),
	}
	log.Debugf(ctx, "loading wordgate config: AppCode=%s, BaseURL=%s", cfg.AppCode, cfg.BaseURL)
	return cfg
}

// createWordgateClient 创建 Wordgate 客户端
func createWordgateClient(ctx context.Context) *wordgate.Client {
	cfg := configWordgate(ctx)
	return wordgate.NewClient(cfg.AppCode, cfg.AppSecret, cfg.BaseURL)
}

// StripeConfig Stripe 配置结构（Overleap LLC 收单主体；overleap 专属渠道，kaitu 永不读取）。
// 开发用测试模式 key（sk_test_/whsec_...），真 key 切换见部署清单。
type StripeConfig struct {
	SecretKey       string
	WebhookSecret   string
	SuccessURL      string // Checkout 成功跳转
	CancelURL       string // Checkout 取消跳转
	PortalReturnURL string // Billing Portal 返回地址
}

// Ready 判断 Stripe 渠道是否配置可用：secret key 与 webhook secret 缺一不可。
// 缺配置时渠道自动不可用（handler 返 405001 / webhook 返 503），绝不 panic。
func (sc StripeConfig) Ready() bool {
	return sc.SecretKey != "" && sc.WebhookSecret != ""
}

// configStripe 获取 Stripe 配置。跳转 URL 缺省回退 overleap 品牌注册表 BaseURL——
// 沿用「viper 旧键只服务 kaitu，overleap 恒走 BrandConfig」的 Phase 1 规则。
func configStripe(ctx context.Context) StripeConfig {
	base := BrandOverleap.Config().BaseURL
	cfg := StripeConfig{
		SecretKey:       viper.GetString("stripe.secret_key"),
		WebhookSecret:   viper.GetString("stripe.webhook_secret"),
		SuccessURL:      viper.GetString("stripe.success_url"),
		CancelURL:       viper.GetString("stripe.cancel_url"),
		PortalReturnURL: viper.GetString("stripe.portal_return_url"),
	}
	if cfg.SuccessURL == "" {
		cfg.SuccessURL = base + "/account?checkout=success"
	}
	if cfg.CancelURL == "" {
		// /purchase, not /pricing — the latter has never existed on the site,
		// so the old default sent every cancelled checkout to a 404.
		cfg.CancelURL = base + "/purchase?checkout=cancelled"
	}
	if cfg.PortalReturnURL == "" {
		cfg.PortalReturnURL = base + "/account"
	}
	return cfg
}

func ConfigServer(ctx context.Context) ServerConfig {
	cfg := ServerConfig{
		Port:       viper.GetInt("server.port"),
		Domain:     viper.GetString("server.domain"),
		SelfCaSign: viper.GetBool("server.self-ca-sign"),
	}
	if cfg.Port == 0 {
		cfg.Port = 5800
	}
	if cfg.Domain == "" {
		cfg.Domain = "k2.52j.me"
	}
	return cfg
}

// configInviteBaseURL 获取邀请链接基础URL
// 从配置中读取 web_base_url，拼接 /s 路径（Web 前端的邀请落地页路由）
// viper 旧键（frontend_config.web_base_url）只服务 kaitu；overleap 恒走品牌注册表 BrandConfig.BaseURL。
func configInviteBaseURL(b Brand) string {
	webBaseURL := ""
	if b != BrandOverleap { // viper 旧键只服务 kaitu
		webBaseURL = viper.GetString("frontend_config.web_base_url")
	}
	if webBaseURL == "" {
		webBaseURL = b.Config().BaseURL
	}
	return fmt.Sprintf("%s/s", webBaseURL)
}

// AliyunConfig Alibaba Cloud configuration for route diagnosis
type AliyunConfig struct {
	AccessKeyID     string
	AccessKeySecret string
}

// DiagnosisConfig route diagnosis configuration
type DiagnosisConfig struct {
	Enabled   bool   // Whether to enable route diagnosis
	Cron      string // Cron expression for scheduled diagnosis (e.g., "0 3 * * 0" for weekly)
	StaleDays int    // Days after which diagnosis data is considered stale (default: 7)
}

// configAliyunForService gets Alibaba Cloud configuration for a specific service
// Priority: aliyun.{service}.* > aliyun.*
// Example: aliyun.cms.access_key_id > aliyun.access_key_id
func configAliyunForService(service string) AliyunConfig {
	// Try service-specific config first
	accessKeyID := viper.GetString(fmt.Sprintf("aliyun.%s.access_key_id", service))
	accessKeySecret := viper.GetString(fmt.Sprintf("aliyun.%s.access_key_secret", service))

	// Fall back to global aliyun config
	if accessKeyID == "" {
		accessKeyID = viper.GetString("aliyun.access_key_id")
	}
	if accessKeySecret == "" {
		accessKeySecret = viper.GetString("aliyun.access_key_secret")
	}

	return AliyunConfig{
		AccessKeyID:     accessKeyID,
		AccessKeySecret: accessKeySecret,
	}
}

// configAliyunCMS gets Alibaba Cloud CMS (Cloud Monitor Service) configuration
// Used for site monitoring / traceroute diagnosis
func configAliyunCMS() AliyunConfig {
	return configAliyunForService("cms")
}

// configDiagnosis gets route diagnosis configuration
func configDiagnosis() DiagnosisConfig {
	cron := viper.GetString("diagnosis.cron")
	if cron == "" {
		cron = "0 19 * * 0" // Default: Sunday 3:00 AM Beijing time (19:00 UTC Saturday)
	}
	staleDays := viper.GetInt("diagnosis.stale_days")
	if staleDays <= 0 {
		staleDays = 7 // Default: 7 days
	}
	return DiagnosisConfig{
		Enabled:   viper.GetBool("diagnosis.enabled"),
		Cron:      cron,
		StaleDays: staleDays,
	}
}

// BandwagonInstance represents a single Bandwagon VPS instance
type BandwagonInstance struct {
	VEID   string `json:"veid"`
	APIKey string `json:"api_key"`
}

// CloudInstanceAccount represents a cloud provider account from config file
type CloudInstanceAccount struct {
	Name            string // Account identifier
	Provider        string // aliyun_swas, aws_lightsail, bandwagon
	Region          string // Provider region (AWS/Aliyun: optional for multi-region)
	AccessKeyID     string // Aliyun/AWS
	AccessKeySecret string // Aliyun only
	SecretAccessKey string // AWS only
	// Bandwagon: supports multiple instances per account
	Instances []BandwagonInstance // provider=bandwagon: list of instances
	// Legacy Bandwagon config (deprecated, use Instances)
	VEID   string // BandwagonHost only (deprecated)
	APIKey string // BandwagonHost only (deprecated)
}

// CloudInstanceSyncConfig holds cloud instance sync worker configuration
type CloudInstanceSyncConfig struct {
	Enabled bool
	Cron    string
}

// CloudInstanceConfig holds all cloud instance related configuration
// Config structure:
//
//	cloud_instance:
//	  sync:
//	    enabled: false
//	    cron: "*/30 * * * *"
//	  accounts:
//	    - name: "aliyun-hk"
//	      provider: "aliyun_swas"
//	      region: "cn-hongkong"
//	      access_key_id: "xxx"
//	      access_key_secret: "xxx"
type CloudInstanceConfig struct {
	Sync     CloudInstanceSyncConfig
	Accounts []CloudInstanceAccount
}

var (
	cloudInstanceConfig     *CloudInstanceConfig
	cloudInstanceConfigOnce sync.Once
)

// ConfigCloudInstance returns cloud instance configuration (sync + accounts)
func ConfigCloudInstance() CloudInstanceConfig {
	cloudInstanceConfigOnce.Do(func() {
		cloudInstanceConfig = &CloudInstanceConfig{
			Sync: CloudInstanceSyncConfig{
				Enabled: viper.GetBool("cloud_instance.sync.enabled"),
				Cron:    viper.GetString("cloud_instance.sync.cron"),
			},
		}
		if cloudInstanceConfig.Sync.Cron == "" {
			cloudInstanceConfig.Sync.Cron = "*/30 * * * *" // Default: every 30 minutes
		}

		// Parse accounts
		var accounts []interface{}
		if err := viper.UnmarshalKey("cloud_instance.accounts", &accounts); err != nil {
			log.Errorf(context.Background(), "[CONFIG] Failed to parse cloud_instance.accounts: %v", err)
			cloudInstanceConfig.Accounts = []CloudInstanceAccount{}
			return
		}

		cloudInstanceConfig.Accounts = make([]CloudInstanceAccount, 0, len(accounts))
		for _, acc := range accounts {
			m, ok := acc.(map[string]interface{})
			if !ok {
				continue
			}
			account := CloudInstanceAccount{
				Name:            getString(m, "name"),
				Provider:        getString(m, "provider"),
				Region:          getString(m, "region"),
				AccessKeyID:     getString(m, "access_key_id"),
				AccessKeySecret: getString(m, "access_key_secret"),
				SecretAccessKey: getString(m, "secret_access_key"),
				VEID:            getString(m, "veid"),
				APIKey:          getString(m, "api_key"),
			}

			// Parse Bandwagon instances array
			if instances, ok := m["instances"].([]interface{}); ok {
				for _, inst := range instances {
					if instMap, ok := inst.(map[string]interface{}); ok {
						account.Instances = append(account.Instances, BandwagonInstance{
							VEID:   getString(instMap, "veid"),
							APIKey: getString(instMap, "api_key"),
						})
					}
				}
			}

			if account.Name != "" && account.Provider != "" {
				cloudInstanceConfig.Accounts = append(cloudInstanceConfig.Accounts, account)
			}
		}
		log.Infof(context.Background(), "[CONFIG] Loaded %d cloud instance accounts", len(cloudInstanceConfig.Accounts))
	})
	return *cloudInstanceConfig
}

// ConfigCloudInstanceAccountByName returns a specific cloud account by name
func ConfigCloudInstanceAccountByName(name string) *CloudInstanceAccount {
	cfg := ConfigCloudInstance()
	for i := range cfg.Accounts {
		if cfg.Accounts[i].Name == name {
			return &cfg.Accounts[i]
		}
	}
	return nil
}

// getString is a helper to get string from map
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

