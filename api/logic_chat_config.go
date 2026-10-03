package center

import (
	"context"
	"strings"

	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/log"
)

// chatEnabled 访客会话功能总开关（viper "chat.enabled"），未配置为 false。
func chatEnabled() bool {
	return viper.GetBool("chat.enabled")
}

// chatPreviewEnabled 总开关关闭时是否仍放行预览 / 邮件回链（viper "chat.preview_enabled"），未配置为 false。
// 两个开关都为 false 是出事时的硬关：所有访客接口一律拒绝，已有 cookie 的访客也不例外。
func chatPreviewEnabled() bool {
	return viper.GetBool("chat.preview_enabled")
}

// chatBrandAllowed 该品牌是否开放访客会话（viper "chat.brands"，字符串列表）。
// 未配置 / 空列表 = 所有品牌关闭。
func chatBrandAllowed(b Brand) bool {
	for _, v := range viper.GetStringSlice("chat.brands") {
		if strings.EqualFold(strings.TrimSpace(v), string(b)) {
			return true
		}
	}
	return false
}

// chatAccess 是某品牌访客会话当前的开放程度。
type chatAccess int

const (
	chatAccessOff     chatAccess = iota // 关闭：session 返回 enabled:false，其余访客接口拒绝
	chatAccessPreview                   // 仅预览：只有 preview 请求或有效 resume 令牌能进
	chatAccessOn                        // 正常开放
)

// chatAccessFor 综合品牌白名单与两个开关：品牌不在名单恒为关闭；
// enabled=true 正常；enabled=false 且 preview_enabled=true 仅预览；都为 false 硬关。
func chatAccessFor(b Brand) chatAccess {
	switch {
	case !chatBrandAllowed(b):
		return chatAccessOff
	case chatEnabled():
		return chatAccessOn
	case chatPreviewEnabled():
		return chatAccessPreview
	}
	return chatAccessOff
}

// chatWSURL 返回该品牌访客 WebSocket 接入地址（viper "chat.ws_urls.<brand>"），
// 如 "wss://ws.kaitu.io"；未配返回 ""。
func chatWSURL(b Brand) string {
	return viper.GetString("chat.ws_urls." + string(b))
}

// managerBaseURL 管理端基础地址（viper "manager.base_url"），去尾斜杠；未配返回 ""，
// 调用方据此不渲染后台链接（业务代码里不写品牌域名做默认值）。
func managerBaseURL() string {
	return strings.TrimRight(viper.GetString("manager.base_url"), "/")
}

// chatSlackLobby 会话总览频道 ID（viper "slack.chat_lobby_channel_id"），其成员即客服名单。
func chatSlackLobby() string {
	return viper.GetString("slack.chat_lobby_channel_id")
}

// slackSigningSecret Slack 回调验签密钥（viper "slack.signing_secret"）。
func slackSigningSecret() string {
	return viper.GetString("slack.signing_secret")
}

// chatConfigProblems 校验客服聊天的配置组合，返回问题列表（空 = 没问题）。
// 只在功能开着（chat.enabled 或 chat.preview_enabled）时检查：这些缺口都不会让启动失败，
// 只会让功能静默地半残——访客能发消息、客服却看不到或回不了。
func chatConfigProblems() []string {
	if !chatEnabled() && !chatPreviewEnabled() {
		return nil
	}
	var problems []string
	brands := viper.GetStringSlice("chat.brands")
	if len(brands) == 0 {
		problems = append(problems, "chat.brands 为空：所有品牌的访客会话都处于关闭")
	}
	for _, v := range brands {
		if !Brand(strings.ToLower(strings.TrimSpace(v))).Valid() {
			problems = append(problems, "chat.brands 里有未注册的品牌 \""+v+"\"")
		}
	}
	if chatSlackLobby() == "" {
		problems = append(problems, "slack.chat_lobby_channel_id 未配置：Slack 镜像整体关闭，客服看不到任何会话")
	}
	if slackSigningSecret() == "" {
		problems = append(problems, "slack.signing_secret 未配置：Slack 事件回调一律被拒，客服在频道里的回复到不了访客")
	}
	if viper.GetString("slack.bot_token") == "" {
		problems = append(problems, "slack.bot_token 未配置：建频道、发消息等 Slack 调用全部失败")
	}
	return problems
}

// chatLogConfigProblems 启动时把配置问题逐条记成 Error。
func chatLogConfigProblems(ctx context.Context) {
	for _, p := range chatConfigProblems() {
		log.Errorf(ctx, "[CHAT] config: %s", p)
	}
}
