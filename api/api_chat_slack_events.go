package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
	"github.com/wordgate/qtoolkit/slack"
	"gorm.io/gorm"
)

// Slack 事件回调：客服在会话专属频道里发言即回复访客（设计 §8）。

const (
	chatSlackEventsMaxBody   = 1 << 20
	chatSlackEventsTimeout   = 30 * time.Second
	chatSlackWarnTimeout     = 10 * time.Second
	chatSlackStaffKeyPrefix  = "chat:slack:user:"
	chatSlackStaffPosTTL     = time.Hour
	chatSlackStaffNegTTL     = 5 * time.Minute
	chatSlackStaffNegValue   = "0"
	chatSlackFileWarnKeyPref = "chat:slack:filewarn:"
)

// errChatSlackNotStaff：Slack 用户不是客服账号（已缓存为负结果也返回它）。
var errChatSlackNotStaff = errors.New("slack user is not a staff account")

type slackEventEnvelope struct {
	Type      string          `json:"type"`
	Challenge string          `json:"challenge"`
	Event     slackMessageEvt `json:"event"`
}

type slackMessageEvt struct {
	Type     string `json:"type"`
	Subtype  string `json:"subtype"`
	BotID    string `json:"bot_id"`
	ThreadTS string `json:"thread_ts"`
	Channel  string `json:"channel"`
	User     string `json:"user"`
	Text     string `json:"text"`
	TS       string `json:"ts"`
}

// api_slack_events 处理 POST /webhook/slack/events。先验签（原始字节），再立即 200，处理放异步。
func api_slack_events(c *gin.Context) {
	secret := slackSigningSecret()
	if secret == "" {
		// 没配签名密钥：一律拒绝（fail closed）
		c.Status(http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, chatSlackEventsMaxBody))
	if err != nil {
		c.Status(http.StatusRequestEntityTooLarge)
		return
	}
	if err := slack.VerifySignature(secret, c.Request.Header, body, time.Now()); err != nil {
		log.Warnf(c, "slack events: signature rejected: %v", err)
		c.Status(http.StatusUnauthorized)
		return
	}
	var env slackEventEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if env.Type == "url_verification" {
		c.String(http.StatusOK, env.Challenge)
		return
	}
	c.Status(http.StatusOK)
	if env.Type != "event_callback" {
		return
	}
	evt := env.Event
	chatAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), chatSlackEventsTimeout)
		defer cancel()
		chatSlackHandleMessage(ctx, evt)
	})
}

// chatSlackHandleMessage 处理一条频道消息事件。
func chatSlackHandleMessage(ctx context.Context, evt slackMessageEvt) {
	// 线程回复留给客服内部讨论；bot 消息与带 subtype 的（编辑、加入频道…）不是客服发言
	if evt.Type != "message" || evt.BotID != "" || evt.ThreadTS != "" || evt.Channel == "" || evt.User == "" {
		return
	}
	if evt.Subtype != "" && evt.Subtype != "file_share" {
		return
	}
	var conv Conversation
	if err := db.Get().WithContext(ctx).Where("slack_channel_id = ?", evt.Channel).First(&conv).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Errorf(ctx, "slack events: load conversation by channel %s: %v", evt.Channel, err)
		}
		return
	}

	if evt.Subtype == "file_share" {
		// 重试会重复送达同一事件：按 ts 去重，免得频道里重复提示
		if ok, err := redis.Client().SetNX(ctx, chatSlackFileWarnKeyPref+evt.TS, "1", time.Hour).Result(); err == nil && !ok {
			return
		}
		chatSlackWarn(ctx, evt.Channel, "⚠️ 图片/文件暂不支持，这条没有发给访客。")
		return
	}

	staff, name, err := chatStaffLookup(ctx, evt.User)
	if err != nil {
		if errors.Is(err, errChatSlackNotStaff) {
			chatSlackWarn(ctx, evt.Channel, fmt.Sprintf("⚠️ <@%s> 不是客服账号，这条没有发给访客。", evt.User))
		} else {
			log.Warnf(ctx, "slack events: staff lookup %s: %v", evt.User, err)
			chatSlackWarn(ctx, evt.Channel, "⚠️ 暂时无法确认客服身份，这条没有发给访客，请稍后重发。")
		}
		return
	}

	text := strings.TrimSpace(chatSlackNormalizeText(evt.Text))
	if text == "" {
		return
	}
	in := appendMessageInput{
		SenderType: SenderStaff, SenderID: staff.ID, SenderName: name,
		Kind: MsgText, Content: text, SlackTS: &evt.TS,
	}

	switch {
	case text == "!ai" || text == "!close":
		// 先把命令本身记成备注（带 SlackTS）：重试时唯一索引冲突 → 不再执行第二遍
		in.Kind = MsgNote
		if !chatSlackRecord(ctx, &conv, in) {
			return
		}
		if text == "!ai" {
			chatSlackCommandAI(ctx, &conv)
		} else {
			chatSlackCommandClose(ctx, &conv)
		}
	case strings.HasPrefix(text, "!"):
		in.Kind = MsgNote
		chatSlackRecord(ctx, &conv, in)
	default:
		// 以库为准判断是否已关闭，不信任刚读出的快照之外的状态
		if conv.Status == ConvClosed {
			chatSlackWarn(ctx, evt.Channel, "⚠️ 会话已关闭，这条没有发给访客。")
			return
		}
		if !chatSlackRecord(ctx, &conv, in) {
			return
		}
		if err := setHandler(ctx, &conv, HandlerHuman); err != nil {
			log.Errorf(ctx, "slack events: set handler human conv=%d: %v", conv.ID, err)
		}
	}
}

// chatSlackRecord 追加一条带 SlackTS 的消息；返回 true 表示这是首次送达（非 dup、无错）。
func chatSlackRecord(ctx context.Context, conv *Conversation, in appendMessageInput) bool {
	_, dup, err := appendMessage(ctx, conv, in)
	if err != nil {
		if errors.Is(err, errChatSlackTSOtherConversation) {
			log.Warnf(ctx, "slack events: slack_ts %v already belongs to another conversation, ignored", *in.SlackTS)
		} else {
			log.Errorf(ctx, "slack events: append message conv=%d: %v", conv.ID, err)
		}
		return false
	}
	return !dup
}

func chatSlackCommandAI(ctx context.Context, conv *Conversation) {
	if err := setHandler(ctx, conv, HandlerAI); err != nil {
		log.Errorf(ctx, "slack events: !ai set handler conv=%d: %v", conv.ID, err)
		return
	}
	chatSlackSystemEvent(ctx, conv, "已交还 AI", ChatEventHandedToAI)
}

func chatSlackCommandClose(ctx context.Context, conv *Conversation) {
	chatSlackSystemEvent(ctx, conv, "会话已关闭", ChatEventClosed)
	if err := closeConversation(ctx, conv); err != nil {
		log.Errorf(ctx, "slack events: !close conv=%d: %v", conv.ID, err)
		return
	}
	if err := chatSlackArchive(ctx, conv); err != nil {
		log.Warnf(ctx, "slack events: archive channel conv=%d: %v", conv.ID, err)
	}
}

func chatSlackSystemEvent(ctx context.Context, conv *Conversation, content, event string) {
	if _, _, err := appendMessage(ctx, conv, appendMessageInput{
		SenderType: SenderSystem, Kind: MsgEvent, Content: content, Meta: chatEventMeta(event),
	}); err != nil {
		log.Errorf(ctx, "slack events: system event %s conv=%d: %v", event, conv.ID, err)
	}
}

// chatSlackWarn 在频道里（非线程）回一条提示；失败只记日志。
func chatSlackWarn(ctx context.Context, channel, text string) {
	ctx, cancel := context.WithTimeout(ctx, chatSlackWarnTimeout)
	defer cancel()
	if _, err := slack.PostMessage(ctx, channel, text, nil); err != nil {
		log.Warnf(ctx, "slack events: post warning to %s: %v", channel, err)
	}
}

// chatStaffFromSlackUser：Slack 用户 id → 邮箱 → users 里 IsAdmin 或带 RoleSupport 的任一品牌账号。
// 只读，绝不创建用户。
func chatStaffFromSlackUser(ctx context.Context, slackUserID string) (*User, error) {
	u, _, err := chatStaffLookup(ctx, slackUserID)
	return u, err
}

// chatStaffLookup 同上，另返回显示名（邮箱 @ 前部分）。
// 正结果缓存 1 小时（值 "<userID>:<显示名>"），负结果缓存 5 分钟；Slack 临时性错误不缓存。
func chatStaffLookup(ctx context.Context, slackUserID string) (*User, string, error) {
	key := chatSlackStaffKeyPrefix + slackUserID
	if v, err := redis.Client().Get(ctx, key).Result(); err == nil {
		if v == chatSlackStaffNegValue {
			return nil, "", errChatSlackNotStaff
		}
		idStr, name, _ := strings.Cut(v, ":")
		if id, perr := strconv.ParseUint(idStr, 10, 64); perr == nil {
			var u User
			if err := db.Get().WithContext(ctx).First(&u, id).Error; err == nil && chatIsStaffUser(&u) {
				return &u, name, nil
			}
		}
		// 缓存的账号已不存在或失去客服身份：走完整查询
	}

	email, err := slack.UserEmail(ctx, slackUserID)
	if err != nil {
		if errors.Is(err, slack.ErrUserNotFound) {
			chatSlackCacheNegative(ctx, key)
			return nil, "", errChatSlackNotStaff
		}
		return nil, "", fmt.Errorf("slack user email: %w", err)
	}
	email = strings.ToLower(strings.TrimSpace(email))
	name := localPart(email)

	var identifies []LoginIdentify
	if err := db.Get().WithContext(ctx).
		Where("type = ? AND index_id = ?", "email", secretHashIt(ctx, []byte(email))).
		Preload("User").Find(&identifies).Error; err != nil {
		return nil, "", fmt.Errorf("lookup staff by email: %w", err)
	}
	for _, li := range identifies {
		if li.User != nil && chatIsStaffUser(li.User) {
			redis.Client().Set(ctx, key, strconv.FormatUint(li.User.ID, 10)+":"+name, chatSlackStaffPosTTL)
			return li.User, name, nil
		}
	}
	chatSlackCacheNegative(ctx, key)
	return nil, "", errChatSlackNotStaff
}

func chatSlackCacheNegative(ctx context.Context, key string) {
	redis.Client().Set(ctx, key, chatSlackStaffNegValue, chatSlackStaffNegTTL)
}

func chatIsStaffUser(u *User) bool {
	return (u.IsAdmin != nil && *u.IsAdmin) || HasRole(u.Roles, RoleSupport)
}

func localPart(email string) string {
	if i := strings.IndexByte(email, '@'); i > 0 {
		return email[:i]
	}
	return email
}

// ---- 文本归一化 ----

var slackMarkupRe = regexp.MustCompile(`<([^<>]+)>`)

var slackEntityReplacer = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&")

// chatSlackNormalizeText 把 Slack 的标记还原成访客能读的文本：
// <url|label>、<url> → url；<@U1> → @U1；<#C1|name> → #name；<!here> → @here；再还原 & < > 实体。
// 先拆标记再还原实体：访客/客服真打出的 "&lt;" 不会被当成标记二次解析。
func chatSlackNormalizeText(s string) string {
	s = slackMarkupRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := m[1 : len(m)-1]
		target, label, hasLabel := strings.Cut(inner, "|")
		switch {
		case strings.HasPrefix(target, "@"):
			return target
		case strings.HasPrefix(target, "#"):
			if hasLabel {
				return "#" + label
			}
			return target
		case strings.HasPrefix(target, "!"):
			if hasLabel {
				return label
			}
			return "@" + strings.TrimPrefix(target, "!")
		}
		return strings.TrimPrefix(target, "mailto:")
	})
	return slackEntityReplacer.Replace(s)
}
