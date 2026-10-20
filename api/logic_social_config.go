package center

import "github.com/spf13/viper"

// 联合登录配置读取。client ID 是公开值，在 BrandConfig.Social；
// 这里只读取"是否上线 + 秘密"：
//
//	social_auth.<brand>.google.enabled      bool   Google 无秘密，靠显式开关上线
//	social_auth.<brand>.apple.key_id        string Apple 私钥 ID
//	social_auth.<brand>.apple.private_key   string Apple .p8 PEM
//	social_auth.apple_team_id               string 两品牌同一开发者账号，缺省 NJT954Q3RH
const defaultAppleTeamID = "NJT954Q3RH"

type socialAppleSecret struct {
	TeamID        string
	KeyID         string
	PrivateKeyPEM string
}

type socialAuthConfig struct {
	Brand         Brand
	IDs           SocialClientIDs
	GoogleEnabled bool
	Apple         socialAppleSecret
}

func configSocialAuth(b Brand) socialAuthConfig {
	prefix := "social_auth." + string(b) + "."
	teamID := viper.GetString("social_auth.apple_team_id")
	if teamID == "" {
		teamID = defaultAppleTeamID
	}
	return socialAuthConfig{
		Brand:         b,
		IDs:           b.Config().Social,
		GoogleEnabled: viper.GetBool(prefix + "google.enabled"),
		Apple: socialAppleSecret{
			TeamID:        teamID,
			KeyID:         viper.GetString(prefix + "apple.key_id"),
			PrivateKeyPEM: viper.GetString(prefix + "apple.private_key"),
		},
	}
}

// Ready 该提供方在本品牌是否可用（未就绪的提供方对客户端隐藏，接口返回 ErrorSocialProviderDisabled）。
func (c socialAuthConfig) Ready(provider string) bool {
	if len(c.IDs.Audiences(provider)) == 0 {
		return false
	}
	switch provider {
	case SocialProviderGoogle:
		return c.GoogleEnabled
	case SocialProviderApple:
		return c.Apple.KeyID != "" && c.Apple.PrivateKeyPEM != ""
	}
	return false
}
