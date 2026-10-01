package center

import (
	"testing"

	"github.com/spf13/viper"
)

// setChatViper 设置 viper 键并在测试结束时还原（viper 全局状态跨测试共享）。
func setChatViper(t *testing.T, key string, val any) {
	t.Helper()
	old := viper.Get(key)
	viper.Set(key, val)
	t.Cleanup(func() { viper.Set(key, old) })
}

func TestChatConfig(t *testing.T) {
	t.Run("未配置时的默认值", func(t *testing.T) {
		// 显式置空，隔离 center/config.yml 里可能存在的值
		setChatViper(t, "chat.enabled", nil)
		setChatViper(t, "chat.ws_urls.kaitu", nil)
		setChatViper(t, "manager.base_url", nil)
		setChatViper(t, "slack.chat_lobby_channel_id", nil)
		setChatViper(t, "slack.signing_secret", nil)

		if chatEnabled() {
			t.Error("chatEnabled() 未配置应为 false")
		}
		if got := chatWSURL(BrandKaitu); got != "" {
			t.Errorf("chatWSURL(kaitu) = %q, want empty", got)
		}
		if got := managerBaseURL(); got != "https://www.kaitu.io" {
			t.Errorf("managerBaseURL() = %q", got)
		}
		if got := chatSlackLobby(); got != "" {
			t.Errorf("chatSlackLobby() = %q", got)
		}
		if got := slackSigningSecret(); got != "" {
			t.Errorf("slackSigningSecret() = %q", got)
		}
	})

	t.Run("managerBaseURL 去尾斜杠", func(t *testing.T) {
		setChatViper(t, "manager.base_url", "https://m.example/")
		if got := managerBaseURL(); got != "https://m.example" {
			t.Errorf("managerBaseURL() = %q", got)
		}
	})

	t.Run("已配置的值", func(t *testing.T) {
		setChatViper(t, "chat.enabled", true)
		setChatViper(t, "chat.ws_urls.kaitu", "wss://ws.kaitu.io")
		setChatViper(t, "chat.ws_urls.overleap", "wss://ws.overleap.io")
		setChatViper(t, "slack.chat_lobby_channel_id", "C123")
		setChatViper(t, "slack.signing_secret", "s3cret")

		if !chatEnabled() {
			t.Error("chatEnabled() want true")
		}
		if got := chatWSURL(BrandKaitu); got != "wss://ws.kaitu.io" {
			t.Errorf("chatWSURL(kaitu) = %q", got)
		}
		if got := chatWSURL(BrandOverleap); got != "wss://ws.overleap.io" {
			t.Errorf("chatWSURL(overleap) = %q", got)
		}
		if got := chatSlackLobby(); got != "C123" {
			t.Errorf("chatSlackLobby() = %q", got)
		}
		if got := slackSigningSecret(); got != "s3cret" {
			t.Errorf("slackSigningSecret() = %q", got)
		}
	})
}
