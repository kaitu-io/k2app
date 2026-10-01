package center

import (
	"strings"

	"github.com/spf13/viper"
)

// chatEnabled 访客会话功能总开关（viper "chat.enabled"），未配置为 false。
func chatEnabled() bool {
	return viper.GetBool("chat.enabled")
}

// chatWSURL 返回该品牌访客 WebSocket 接入地址（viper "chat.ws_urls.<brand>"），
// 如 "wss://ws.kaitu.io"；未配返回 ""。
func chatWSURL(b Brand) string {
	return viper.GetString("chat.ws_urls." + string(b))
}

// managerBaseURL 管理端基础地址（viper "manager.base_url"），去尾斜杠；未配返回默认值。
func managerBaseURL() string {
	u := strings.TrimRight(viper.GetString("manager.base_url"), "/")
	if u == "" {
		return "https://www.kaitu.io"
	}
	return u
}

// chatSlackLobby 会话总览频道 ID（viper "slack.chat_lobby_channel_id"），其成员即客服名单。
func chatSlackLobby() string {
	return viper.GetString("slack.chat_lobby_channel_id")
}

// slackSigningSecret Slack 回调验签密钥（viper "slack.signing_secret"）。
func slackSigningSecret() string {
	return viper.GetString("slack.signing_secret")
}
