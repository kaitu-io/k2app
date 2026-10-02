package center

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// setChatViper 设置 viper 键并在测试结束时还原（viper 全局状态跨测试共享）。
// val 必须是带类型的值：viper.Set(key, nil) 等于撤掉覆盖层，读到的是 config.yml 里的值，遮不住本地配置。
func setChatViper(t *testing.T, key string, val any) {
	t.Helper()
	if val == nil {
		t.Fatalf("setChatViper(%q, nil): 用带类型的零值（false / \"\" / []string{}）", key)
	}
	old := viper.Get(key)
	viper.Set(key, val)
	t.Cleanup(func() { viper.Set(key, old) })
}

func TestChatConfig(t *testing.T) {
	t.Run("未配置时的默认值", func(t *testing.T) {
		// 用带类型的零值遮住 center/config.yml 里可能存在的值（nil 遮不住）
		setChatViper(t, "chat.enabled", false)
		setChatViper(t, "chat.preview_enabled", false)
		setChatViper(t, "chat.brands", []string{})
		setChatViper(t, "chat.ws_urls.kaitu", "")
		setChatViper(t, "manager.base_url", "")
		setChatViper(t, "slack.chat_lobby_channel_id", "")
		setChatViper(t, "slack.signing_secret", "")

		if chatEnabled() {
			t.Error("chatEnabled() 未配置应为 false")
		}
		if chatPreviewEnabled() {
			t.Error("chatPreviewEnabled() 未配置应为 false")
		}
		for _, b := range AllBrands() {
			if chatBrandAllowed(b) {
				t.Errorf("chatBrandAllowed(%s) 空名单应为 false", b)
			}
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

// 开放程度 = 品牌白名单 × 两个开关。
func TestChatAccessFor(t *testing.T) {
	in, out := AllBrands()[0], AllBrands()[1]
	cases := []struct {
		name             string
		enabled, preview bool
		brand            Brand
		want             chatAccess
	}{
		{"开", true, false, in, chatAccessOn},
		{"开且预览也开", true, true, in, chatAccessOn},
		{"仅预览", false, true, in, chatAccessPreview},
		{"硬关", false, false, in, chatAccessOff},
		{"名单外：开", true, true, out, chatAccessOff},
		{"名单外：仅预览", false, true, out, chatAccessOff},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setChatViper(t, "chat.brands", []string{" " + strings.ToUpper(string(in)) + " "}) // 容忍大小写与空白
			setChatViper(t, "chat.enabled", tc.enabled)
			setChatViper(t, "chat.preview_enabled", tc.preview)
			if got := chatAccessFor(tc.brand); got != tc.want {
				t.Errorf("chatAccessFor(%s) = %d, want %d", tc.brand, got, tc.want)
			}
		})
	}
}
