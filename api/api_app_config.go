package center

import (
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/log"
)

// DataAppLinks 应用链接配置
//
type DataAppLinks struct {
	BaseURL           string `json:"baseURL" example:"https://www.kaitu.io"`      // Base URL
	InstallPath       string `json:"installPath" example:"/install"`              // Install page path
	DiscoveryPath     string `json:"discoveryPath" example:"/discovery"`          // Discovery page path
	PrivacyPath       string `json:"privacyPath" example:"/privacy"`              // Privacy policy path
	TermsPath         string `json:"termsPath" example:"/terms"`                  // Terms of service path
	WalletPath        string `json:"walletPath" example:"/wallet"`                // Wallet page path
	RetailerRulesPath string `json:"retailerRulesPath" example:"/retailer/rules"` // Retailer rules page path
	ChangelogPath     string `json:"changelogPath" example:"/changelog"`          // Changelog page path
}

// DataAnnouncement 公告信息
//
type DataAnnouncement struct {
	ID        string `json:"id" example:"announcement-2024-01"`                             // 公告唯一ID，用于客户端跟踪关闭状态
	Message   string `json:"message" example:"系统维护公告：1月1日凌晨进行系统升级"`                         // 公告文字内容
	LinkURL   string `json:"linkUrl,omitempty" example:"https://kaitu.io/news/maintenance"` // 可选：点击跳转链接
	LinkText  string `json:"linkText,omitempty" example:"查看详情"`                             // 可选：链接文字
	ExpiresAt int64  `json:"expiresAt,omitempty" example:"1704067200"`                      // 可选：公告过期时间戳（Unix秒），为0表示不过期
}

// DataAppConfig 应用配置响应数据结构
//
type DataAppConfig struct {
	AppLinks         DataAppLinks      `json:"appLinks"`                   // 应用相关链接
	InviteReward     InviteConfig      `json:"inviteReward"`               // 邀请奖励配置
	MinClientVersion string            `json:"minClientVersion,omitempty"` // 最低客户端版本要求，低于此版本强制升级
	Announcement     *DataAnnouncement `json:"announcement,omitempty"`     // 公告信息，nil表示无公告
}

// api_get_app_config 获取应用配置
//
func api_get_app_config(c *gin.Context) {
	log.Infof(c, "requesting app config")

	// Read app links from config
	appLinks := DataAppLinks{
		BaseURL:           viper.GetString("frontend_config.app_links.base_url"),
		InstallPath:       viper.GetString("frontend_config.app_links.install_path"),
		DiscoveryPath:     viper.GetString("frontend_config.app_links.discovery_path"),
		PrivacyPath:       viper.GetString("frontend_config.app_links.privacy_path"),
		TermsPath:         viper.GetString("frontend_config.app_links.terms_path"),
		WalletPath:        viper.GetString("frontend_config.app_links.wallet_path"),
		RetailerRulesPath: viper.GetString("frontend_config.app_links.retailer_rules_path"),
		ChangelogPath:     viper.GetString("frontend_config.app_links.changelog_path"),
	}

	// 从配置读取邀请奖励规则（使用统一的 configInvite）
	inviteReward := configInvite(c)

	// 设置默认值
	if appLinks.BaseURL == "" {
		appLinks.BaseURL = "https://www.kaitu.io"
	}
	if appLinks.InstallPath == "" {
		appLinks.InstallPath = "/install"
	}
	if appLinks.DiscoveryPath == "" {
		appLinks.DiscoveryPath = "/discovery"
	}
	if appLinks.PrivacyPath == "" {
		appLinks.PrivacyPath = "/privacy"
	}
	if appLinks.TermsPath == "" {
		appLinks.TermsPath = "/terms"
	}
	if appLinks.WalletPath == "" {
		appLinks.WalletPath = "/wallet"
	}
	if appLinks.RetailerRulesPath == "" {
		appLinks.RetailerRulesPath = "/retailer/rules"
	}
	if appLinks.ChangelogPath == "" {
		appLinks.ChangelogPath = "/changelog"
	}

	// Read minimum client version requirement
	minClientVersion := viper.GetString("frontend_config.min_client_version")

	// 读取公告配置
	var announcement *DataAnnouncement
	announcementID := viper.GetString("frontend_config.announcement.id")
	if announcementID != "" {
		announcement = &DataAnnouncement{
			ID:        announcementID,
			Message:   viper.GetString("frontend_config.announcement.message"),
			LinkURL:   viper.GetString("frontend_config.announcement.link_url"),
			LinkText:  viper.GetString("frontend_config.announcement.link_text"),
			ExpiresAt: viper.GetInt64("frontend_config.announcement.expires_at"),
		}
	}

	// 构造响应数据
	data := DataAppConfig{
		AppLinks:         appLinks,
		InviteReward:     inviteReward,
		MinClientVersion: minClientVersion,
		Announcement:     announcement,
	}

	log.Infof(c, "successfully retrieved app config: %+v", data)
	Success(c, &data)
}
