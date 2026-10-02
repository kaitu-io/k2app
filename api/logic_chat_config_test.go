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
		if got := managerBaseURL(); got != "" {
			t.Errorf("managerBaseURL() 未配置应为空（业务代码里不写品牌域名），got %q", got)
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

// 配置组合校验：功能开着却缺关键配置时逐条报出来（启动时记 Error）。
func TestChatConfigProblems(t *testing.T) {
	set := func(t *testing.T, enabled, preview bool, brands []string, lobby, secret, token string) {
		setChatViper(t, "chat.enabled", enabled)
		setChatViper(t, "chat.preview_enabled", preview)
		setChatViper(t, "chat.brands", brands)
		setChatViper(t, "slack.chat_lobby_channel_id", lobby)
		setChatViper(t, "slack.signing_secret", secret)
		setChatViper(t, "slack.bot_token", token)
	}
	all := chatAllBrandNames()
	has := func(problems []string, key string) bool {
		for _, p := range problems {
			if strings.Contains(p, key) {
				return true
			}
		}
		return false
	}

	t.Run("两个开关都关：不检查", func(t *testing.T) {
		set(t, false, false, []string{}, "", "", "")
		if p := chatConfigProblems(); len(p) != 0 {
			t.Errorf("关闭时不该报问题: %v", p)
		}
	})
	t.Run("配置齐全：没有问题", func(t *testing.T) {
		set(t, true, false, all, "C1", "sec", "xoxb-1")
		if p := chatConfigProblems(); len(p) != 0 {
			t.Errorf("配置齐全不该报问题: %v", p)
		}
	})
	for _, mode := range []struct {
		name             string
		enabled, preview bool
	}{{"enabled", true, false}, {"仅预览", false, true}} {
		t.Run(mode.name+"：四项全缺逐条报", func(t *testing.T) {
			set(t, mode.enabled, mode.preview, []string{}, "", "", "")
			p := chatConfigProblems()
			if len(p) != 4 {
				t.Fatalf("want 4 problems, got %d: %v", len(p), p)
			}
			for _, key := range []string{"chat.brands", "slack.chat_lobby_channel_id", "slack.signing_secret", "slack.bot_token"} {
				if !has(p, key) {
					t.Errorf("缺 %s 没报: %v", key, p)
				}
			}
		})
	}
	for key, mod := range map[string]func(t *testing.T){
		"chat.brands":                 func(t *testing.T) { set(t, true, false, []string{}, "C1", "sec", "xoxb-1") },
		"slack.chat_lobby_channel_id": func(t *testing.T) { set(t, true, false, all, "", "sec", "xoxb-1") },
		"slack.signing_secret":        func(t *testing.T) { set(t, true, false, all, "C1", "", "xoxb-1") },
		"slack.bot_token":             func(t *testing.T) { set(t, true, false, all, "C1", "sec", "") },
	} {
		t.Run("只缺 "+key, func(t *testing.T) {
			mod(t)
			p := chatConfigProblems()
			if len(p) != 1 || !has(p, key) {
				t.Errorf("want exactly the %s problem, got %v", key, p)
			}
		})
	}
	t.Run("名单里有未注册的品牌", func(t *testing.T) {
		set(t, true, false, append([]string{"no-such-brand"}, all...), "C1", "sec", "xoxb-1")
		p := chatConfigProblems()
		if len(p) != 1 || !has(p, "no-such-brand") {
			t.Errorf("want the unknown brand problem, got %v", p)
		}
	})
}
