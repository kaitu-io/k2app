package center

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// Slack 镜像的文案（客服聊天）：状态、状态卡、总览频道那一行、频道主题、频道内消息文本与转义。
// 发送、建频道、归档、sweep 在 logic_chat_slack.go。

const chatSlackNoEmail = "未留邮箱"

// chatSlackLoc 频道名与卡片里的日期按客服所在时区（Asia/Shanghai，无夏令时，固定 +8）。
var chatSlackLoc = time.FixedZone("Asia/Shanghai", 8*3600)

// chatSlackEscaper 按 Slack 规则转义访客可控文本：否则访客能 <!channel> 全员提醒、伪造链接。
var chatSlackEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// chatSlackChannelURL 返回在 Slack 客户端里打开该频道的链接。
func chatSlackChannelURL(channelID string) string {
	return "https://slack.com/app_redirect?channel=" + channelID
}

// chatSlackChannelName 返回 "chat-MMDD-<uuid 前 6 位>"（Slack 只允许小写字母、数字、连字符、下划线，≤80 字符）。
// MMDD 取会话创建时间在客服时区（Asia/Shanghai）的日期。
func chatSlackChannelName(conv *Conversation) string {
	id := strings.ToLower(strings.ReplaceAll(conv.UUID, "-", ""))
	if len(id) > 6 {
		id = id[:6]
	}
	return "chat-" + conv.CreatedAt.In(chatSlackLoc).Format("0102") + "-" + id
}

// chatSlackStatus 返回会话状态的 emoji 与文案。
func chatSlackStatus(conv *Conversation) (emoji, label string) {
	switch {
	case conv.Status == ConvClosed:
		return "⚪", "已关闭"
	case conv.Handler == HandlerAI:
		return "🤖", "AI 接待中"
	case conv.LastMessageBy == SenderVisitor:
		return "🔴", "等待人工"
	}
	return "🟡", "已回复待访客"
}

// chatSlackView 一个会话在 Slack 里的三段文案。
type chatSlackView struct{ card, lobby, topic string }

// chatSlackRender 生成状态卡、总览频道那一行、频道主题。
// 登录用户的邮箱是加密存储的登录标识，取它要解密，这里不取：user 主体一律显示"未留邮箱"。
func chatSlackRender(ctx context.Context, conv *Conversation) chatSlackView {
	brand := Brand(conv.Brand).Config().DisplayName
	entry := chatSlackEscaper.Replace(conv.EntryPath)
	emoji, label := chatSlackStatus(conv)

	who, email, ids := fmt.Sprintf("用户 #%d", conv.SubjectID), "", []uint64{conv.SubjectID}
	if conv.SubjectKind == SubjectGuest {
		root := conv.SubjectID
		if s, err := chatSubjectOfConversation(ctx, conv); err == nil {
			root = s.ID
			if cluster, err := chatSubjectIDs(ctx, s); err == nil && len(cluster) > 0 {
				ids = append(cluster, conv.SubjectID)
			}
		}
		who, email = fmt.Sprintf("游客 #%d", root), chatSlackEscaper.Replace(guestEmail(ctx, root))
	}
	emailOrNone, emailOrWho := email, email
	if email == "" {
		emailOrNone, emailOrWho = chatSlackNoEmail, who
	}

	head := fmt.Sprintf("%s *%s* · %s", emoji, label, brand)
	if strings.HasPrefix(conv.EntryPath, "/pay-result") {
		head += " · 🔥 支付结果页"
	}
	card := strings.Join([]string{
		head,
		"入口: " + entry,
		"访客: " + emailOrNone + " · " + who,
		"此前会话: " + chatSlackHistory(ctx, conv, ids),
		fmt.Sprintf("<%s/manager/conversations?c=%s|在后台查看> · 直接在此频道发言即回复访客；`!ai` 交还 AI，`!close` 关闭，其他 `!` 开头为内部备注",
			managerBaseURL(), conv.UUID),
	}, "\n")
	// 频道名不落库：重名加过 -2/-3 后缀的会话（同日 + uuid 前 6 位相同，极罕见）这里显示的是不带后缀的名字，链接仍正确
	lobby := fmt.Sprintf("%s <%s|%s> · %s · %s · 入口 %s", emoji, chatSlackChannelURL(conv.SlackChannelID),
		chatSlackChannelName(conv), brand, emailOrWho, entry)
	topic := fmt.Sprintf("%s · %s · 入口 %s", brand, emailOrNone, entry)
	return chatSlackView{card: card, lobby: lobby, topic: topic}
}

// chatSlackCardText 返回状态卡文案。
func chatSlackCardText(ctx context.Context, conv *Conversation) string {
	return chatSlackRender(ctx, conv).card
}

// chatSlackHistory 同一主体（guest 取整簇）此前已关闭的会话：`无` 或 `N 次 · 上次 MM月DD日 <url|打开>`。
func chatSlackHistory(ctx context.Context, conv *Conversation, ids []uint64) string {
	base := func() *gorm.DB {
		return db.Get().WithContext(ctx).Model(&Conversation{}).
			Where("brand = ? AND subject_kind = ? AND subject_id IN ? AND status = ? AND id < ?",
				conv.Brand, conv.SubjectKind, ids, ConvClosed, conv.ID)
	}
	var n int64
	if err := base().Count(&n).Error; err != nil || n == 0 {
		return "无"
	}
	var prev []Conversation
	if err := base().Order("id DESC").Limit(1).Find(&prev).Error; err != nil || len(prev) == 0 {
		return "无"
	}
	out := fmt.Sprintf("%d 次 · 上次 %s", n, prev[0].LastMessageAt.In(chatSlackLoc).Format("01月02日"))
	if prev[0].SlackChannelID != "" {
		out += fmt.Sprintf(" <%s|打开>", chatSlackChannelURL(prev[0].SlackChannelID))
	}
	return out
}

// chatSlackMessageText 返回消息在频道里的文本；post=false 表示不发（内部备注、本就来自 Slack 的消息）。
func chatSlackMessageText(m *ConversationMessage) (text string, post bool) {
	if m.Kind == MsgNote || (m.SlackTS != nil && *m.SlackTS != "") {
		return "", false
	}
	body := chatSlackEscaper.Replace(m.Content)
	if m.Kind == MsgOptions {
		if labels := chatSlackOptionLabels(m.Meta); len(labels) > 0 {
			body = strings.TrimPrefix(body+"\n选项: "+chatSlackEscaper.Replace(strings.Join(labels, " / ")), "\n")
		}
	}
	switch m.SenderType {
	case SenderVisitor:
		return "👤 " + body, true
	case SenderAI:
		return "🤖 " + body, true
	case SenderStaff:
		return "🧑‍💼 " + chatSlackEscaper.Replace(m.SenderName) + ": " + body, true
	}
	var meta struct {
		Event string `json:"event"`
	}
	_ = json.Unmarshal([]byte(m.Meta), &meta)
	if meta.Event == ChatEventTransferHuman {
		return "<!channel> ℹ️ " + body, true // 转人工是唯一的强提醒
	}
	return "ℹ️ " + body, true
}

// chatSlackOptionLabels 从 options 消息的 Meta（{"options":[{"label":..}|"文本"]}）取各选项的显示文字。
func chatSlackOptionLabels(meta string) []string {
	var parsed struct {
		Options []json.RawMessage `json:"options"`
	}
	if json.Unmarshal([]byte(meta), &parsed) != nil {
		return nil
	}
	var labels []string
	for _, raw := range parsed.Options {
		var s string
		var o struct {
			Label string `json:"label"`
		}
		if json.Unmarshal(raw, &s) == nil && s != "" {
			labels = append(labels, s)
		} else if json.Unmarshal(raw, &o) == nil && o.Label != "" {
			labels = append(labels, o.Label)
		}
	}
	return labels
}
