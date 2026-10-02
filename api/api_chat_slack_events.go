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
	"sync"
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
	chatSlackEventsMaxBody  = 1 << 20
	chatSlackEventsTimeout  = 30 * time.Second
	chatSlackWarnTimeout    = 10 * time.Second
	chatSlackStaffKeyPrefix = "chat:slack:user:"
	chatSlackStaffPosTTL    = time.Hour
	chatSlackStaffNegTTL    = 5 * time.Minute
	chatSlackStaffNegValue  = "0"
	chatSlackStaffNoEmailV  = "0:noemail"
	chatSlackWarnKeyPrefix  = "chat:slack:warn:"
)

// 频道提示文案
const (
	chatSlackWarnClosed    = "⚠️ 会话已关闭，这条没有发给访客。"
	chatSlackWarnFile      = "⚠️ 图片/文件暂不支持，这条没有发给访客。"
	chatSlackWarnNoEmail   = "⚠️ 无法识别你的客服身份：Slack 账号没有可见邮箱，这条没有发给访客。"
	chatSlackWarnTransient = "⚠️ 暂时无法确认客服身份，这条没有发给访客，请稍后重发。"
	chatSlackWarnSendFail  = "⚠️ 这条没有发给访客，请重发"
	chatSlackWarnCmdFail   = "⚠️ 命令执行失败，请重发。"
	chatSlackWarnCmdClosed = "⚠️ 会话已关闭，这条命令没有执行。"
	chatSlackWarnAlreadyAI = "ℹ️ 当前已是 AI 接待，无需交还。"
	chatSlackWarnEdited    = "⚠️ 编辑/删除不会同步给访客；如需更正请再发一条。"
)

// errChatSlackNotStaff：Slack 用户不是客服账号（已缓存为负结果也返回它）。
var errChatSlackNotStaff = errors.New("slack user is not a staff account")

// errChatSlackNoEmail：Slack 账号没有可见邮箱（权限范围或 bot 账号），属永久性问题，同样走负缓存。
var errChatSlackNoEmail = errors.New("slack user has no visible email")

// chatSlackAppendSystem 追加 system 事件；包级变量只是测试接缝（注入追加失败）。
var chatSlackAppendSystem = appendMessage

type slackEventEnvelope struct {
	Type      string          `json:"type"`
	Challenge string          `json:"challenge"`
	Event     slackMessageEvt `json:"event"`
}

// slackInnerMsg 是 message_changed / message_deleted 事件里嵌套的 message / previous_message。
type slackInnerMsg struct {
	User     string `json:"user"`
	BotID    string `json:"bot_id"`
	Text     string `json:"text"`
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts"`
}

type slackMessageEvt struct {
	Type            string         `json:"type"`
	Subtype         string         `json:"subtype"`
	BotID           string         `json:"bot_id"`
	ThreadTS        string         `json:"thread_ts"`
	Channel         string         `json:"channel"`
	User            string         `json:"user"`
	Text            string         `json:"text"`
	TS              string         `json:"ts"`
	DeletedTS       string         `json:"deleted_ts"`
	Message         *slackInnerMsg `json:"message"`
	PreviousMessage *slackInnerMsg `json:"previous_message"`
}

// api_slack_events 处理 POST /webhook/slack/events。
// 验签先于一切解析：body 在任何读取之前就套 1MB 上限（超限 413），未签名的大请求不会被整体读入；
// 验签通过后立即 200，处理按频道有序异步。日志只记事件类型/频道/ts/结果，绝不记正文与信封 token。
func api_slack_events(c *gin.Context) {
	secret := slackSigningSecret()
	if secret == "" {
		// 没配签名密钥：一律拒绝（fail closed）
		c.Status(http.StatusUnauthorized)
		return
	}
	if c.Request.ContentLength > chatSlackEventsMaxBody {
		c.Status(http.StatusRequestEntityTooLarge)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, chatSlackEventsMaxBody)
	body, err := io.ReadAll(c.Request.Body)
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
	chatSlackEnqueue(evt.Channel, func() {
		ctx, cancel := context.WithTimeout(context.Background(), chatSlackEventsTimeout)
		defer cancel()
		result := chatSlackHandleMessage(ctx, evt)
		log.Infof(ctx, "slack events: type=%s subtype=%s channel=%s ts=%s result=%s", evt.Type, evt.Subtype, evt.Channel, evt.TS, result)
	})
}

// 同一频道的事件在本实例内按到达顺序串行处理：入队发生在 handler 返回 200 之前，
// 每个任务等前一个完成，所以客服连发两条的落库顺序 = 到达顺序。
// 局限：只在单实例内有序；多实例部署下同一频道的两个请求若落在不同实例，仍可能乱序
// （此时靠 Slack ts 排序展示，落库 id 顺序不保证）。
var chatSlackQueue = struct {
	sync.Mutex
	tails map[string]chan struct{}
}{tails: map[string]chan struct{}{}}

func chatSlackEnqueue(channel string, job func()) {
	q := &chatSlackQueue
	q.Lock()
	prev := q.tails[channel]
	done := make(chan struct{})
	q.tails[channel] = done
	q.Unlock()
	chatAsync(func() {
		if prev != nil {
			<-prev
		}
		defer func() {
			q.Lock()
			if q.tails[channel] == done {
				delete(q.tails, channel)
			}
			q.Unlock()
			close(done)
		}()
		job()
	})
}

// chatSlackHandleMessage 处理一条频道消息事件，返回结果标签（只用于日志）。
func chatSlackHandleMessage(ctx context.Context, evt slackMessageEvt) string {
	if evt.Type != "message" || evt.Channel == "" {
		return "ignored"
	}
	isEdit := evt.Subtype == "message_changed" || evt.Subtype == "message_deleted"
	// 线程回复（含 thread_broadcast）留给客服内部讨论；bot 消息与其余 subtype（加入频道…）不是客服发言
	if !isEdit && (evt.ThreadTS != "" || evt.BotID != "" || evt.User == "" ||
		(evt.Subtype != "" && evt.Subtype != "file_share")) {
		return "ignored"
	}
	var conv Conversation
	if err := db.Get().WithContext(ctx).Where("slack_channel_id = ?", evt.Channel).First(&conv).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Errorf(ctx, "slack events: load conversation by channel %s: %v", evt.Channel, err)
			return "error"
		}
		return "unknown_channel"
	}
	if isEdit {
		return chatSlackHandleEdit(ctx, &conv, evt)
	}
	// 自己（bot）发的消息不当作客服发言，杜绝回环
	if id, err := slack.BotUserID(ctx); err == nil && id == evt.User {
		return "self"
	}

	if evt.Subtype == "file_share" {
		chatSlackWarnOnce(ctx, evt.Channel, evt.TS, chatSlackWarnFile)
		return "file_share"
	}
	// Slack 重投：已记录过的 ts 直接结束，免得重复提示/重复执行命令
	if chatSlackSeen(ctx, conv.ID, evt.TS) {
		return "dup"
	}

	staff, name, err := chatStaffLookup(ctx, evt.User)
	if err != nil {
		switch {
		case errors.Is(err, errChatSlackNotStaff):
			chatSlackWarnOnce(ctx, evt.Channel, evt.TS, fmt.Sprintf("⚠️ <@%s> 不是客服账号，这条没有发给访客。", evt.User))
			return "not_staff"
		case errors.Is(err, errChatSlackNoEmail):
			chatSlackWarnOnce(ctx, evt.Channel, evt.TS, chatSlackWarnNoEmail)
			return "no_email"
		}
		log.Warnf(ctx, "slack events: staff lookup %s: %v", evt.User, err)
		chatSlackWarnOnce(ctx, evt.Channel, evt.TS, chatSlackWarnTransient)
		return "lookup_error"
	}

	text := strings.TrimSpace(chatSlackNormalizeText(evt.Text))
	if text == "" {
		return "empty"
	}
	in := appendMessageInput{
		SenderType: SenderStaff, SenderID: staff.ID, SenderName: name,
		Kind: MsgText, Content: text, SlackTS: &evt.TS,
	}

	switch {
	case text == "!ai" || text == "!close":
		return chatSlackCommand(ctx, &conv, in)
	case strings.HasPrefix(text, "!"):
		in.Kind = MsgNote
		_, res := chatSlackRecord(ctx, &conv, in)
		return res.String()
	}

	// 以刚读出的会话状态判断：已关闭不再发给访客
	if conv.Status == ConvClosed {
		chatSlackWarnOnce(ctx, evt.Channel, evt.TS, chatSlackWarnClosed)
		return "closed"
	}
	// 客服一开口 AI 立即停：先交给人工，再落消息；交接失败则不发送
	if err := setHandler(ctx, &conv, HandlerHuman); err != nil {
		log.Errorf(ctx, "slack events: set handler human conv=%d: %v", conv.ID, err)
		chatSlackWarnOnce(ctx, evt.Channel, evt.TS, chatSlackWarnSendFail)
		return "handler_error"
	}
	_, res := chatSlackRecord(ctx, &conv, in)
	return res.String()
}

type chatRecordResult int

const (
	recordOK chatRecordResult = iota
	recordDup
	recordErr
)

func (r chatRecordResult) String() string {
	return [...]string{"recorded", "dup", "record_error"}[r]
}

// chatSlackRecord 追加一条带 SlackTS 的消息，区分首次送达 / 重复 / 出错。
// 出错（含 slack_ts 属于别的会话）时在频道提示"没有发给访客"，Slack 不会重投，只能让客服重发。
func chatSlackRecord(ctx context.Context, conv *Conversation, in appendMessageInput) (*ConversationMessage, chatRecordResult) {
	msg, dup, err := appendMessage(ctx, conv, in)
	if err != nil {
		log.Errorf(ctx, "slack events: append message conv=%d: %v", conv.ID, err)
		chatSlackWarnOnce(ctx, conv.SlackChannelID, *in.SlackTS, chatSlackWarnSendFail)
		return nil, recordErr
	}
	if dup {
		return msg, recordDup
	}
	return msg, recordOK
}

// chatSlackSeen：该 Slack ts 是否已在本会话落库。别的会话占用同一 ts 不算"已见"，
// 交给 appendMessage 报 errChatSlackTSOtherConversation 并提示客服。
func chatSlackSeen(ctx context.Context, convID uint64, ts string) bool {
	if ts == "" {
		return false
	}
	var n int64
	if err := db.Get().WithContext(ctx).Model(&ConversationMessage{}).Where("conversation_id = ? AND slack_ts = ?", convID, ts).Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

// chatSlackCommand 处理 !ai / !close。先把命令本身记成带 SlackTS 的备注作为幂等标记；
// 动作失败则删掉标记并提示，客服重发（新 ts）或 Slack 重投都不会被它挡住。
func chatSlackCommand(ctx context.Context, conv *Conversation, in appendMessageInput) string {
	ch, ts := conv.SlackChannelID, *in.SlackTS
	isAI := in.Content == "!ai"
	if conv.Status == ConvClosed {
		chatSlackWarnOnce(ctx, ch, ts, chatSlackWarnCmdClosed)
		return "closed"
	}
	if isAI && conv.Handler == HandlerAI {
		chatSlackWarnOnce(ctx, ch, ts, chatSlackWarnAlreadyAI)
		return "already_ai"
	}
	in.Kind = MsgNote
	marker, res := chatSlackRecord(ctx, conv, in)
	if res != recordOK {
		return res.String()
	}
	fail := func() string {
		if err := db.Get().WithContext(ctx).Delete(&ConversationMessage{}, marker.ID).Error; err != nil {
			log.Errorf(ctx, "slack events: remove command marker conv=%d: %v", conv.ID, err)
		}
		chatSlackWarnOnce(ctx, ch, ts, chatSlackWarnCmdFail)
		return "command_error"
	}

	if isAI {
		if err := setHandler(ctx, conv, HandlerAI); err != nil {
			log.Errorf(ctx, "slack events: !ai set handler conv=%d: %v", conv.ID, err)
			return fail()
		}
		chatSlackSystemEvent(ctx, conv, "已交还 AI", ChatEventHandedToAI)
		return "handed_to_ai"
	}

	if _, err := chatCloseByStaff(ctx, conv); err != nil {
		log.Errorf(ctx, "slack events: !close conv=%d: %v", conv.ID, err)
		return fail()
	}
	return "closed_by_staff"
}

const (
	chatCloseLockTTL  = 30 * time.Second
	chatCloseLockWait = 10 * time.Second
)

// chatCloseByStaff 是人工关闭会话的唯一实现（Slack `!close` 与后台 PUT .../close 共用）：
// closed 事件 → 关闭 → 归档。事件落不下就不关闭并返回错误（"人工关闭必有 closed 事件"，T10 的补偿判据依赖它）；
// 关闭失败则把刚落的事件删掉，保持"有事件 ⇔ 已关闭"。
// 按会话加 Redis 锁并在锁内以库为准复查：并发调用只有一方落事件、关一次；已关闭则幂等返回（wasOpen=false），
// 不追加事件。归档失败只记日志（chatSlackSweep 会补）。
func chatCloseByStaff(ctx context.Context, conv *Conversation) (wasOpen bool, err error) {
	release, err := chatCloseLock(ctx, conv.ID)
	if err != nil {
		return false, err
	}
	wasOpen, err = func() (bool, error) {
		defer release()
		var cur Conversation
		if err := db.Get().WithContext(ctx).Select("id", "status").First(&cur, conv.ID).Error; err != nil {
			return false, fmt.Errorf("reload conversation: %w", err)
		}
		if cur.Status != ConvOpen {
			conv.Status = cur.Status
			return false, nil
		}
		ev, err := chatSlackSystemEvent(ctx, conv, "会话已关闭", ChatEventClosed)
		if err != nil {
			return false, err
		}
		if err := closeConversation(ctx, conv); err != nil {
			if derr := db.Get().WithContext(ctx).Delete(&ConversationMessage{}, ev.ID).Error; derr != nil {
				log.Errorf(ctx, "chat close: remove orphan closed event conv=%d: %v", conv.ID, derr)
			}
			return false, err
		}
		return true, nil
	}()
	if err != nil {
		return false, err
	}
	if err := chatSlackArchive(ctx, conv); err != nil {
		log.Warnf(ctx, "chat close: archive channel conv=%d: %v", conv.ID, err)
	}
	return wasOpen, nil
}

// chatCloseLock 取会话关闭锁；被占用时轮询等待（持锁方很快结束），超时报错。
func chatCloseLock(ctx context.Context, convID uint64) (release func(), err error) {
	key := "chat:close:" + strconv.FormatUint(convID, 10)
	token := generateId("cl")
	deadline := time.Now().Add(chatCloseLockWait)
	for {
		ok, err := redis.Client().SetNX(ctx, key, token, chatCloseLockTTL).Result()
		if err != nil {
			return nil, fmt.Errorf("close lock: %w", err)
		}
		if ok {
			return func() {
				const script = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
				_ = redis.Client().Eval(context.Background(), script, []string{key}, token).Err()
			}, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("close lock: timeout")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func chatSlackSystemEvent(ctx context.Context, conv *Conversation, content, event string) (*ConversationMessage, error) {
	msg, _, err := chatSlackAppendSystem(ctx, conv, appendMessageInput{
		SenderType: SenderSystem, Kind: MsgEvent, Content: content, Meta: chatEventMeta(event),
	})
	if err != nil {
		log.Errorf(ctx, "slack events: system event %s conv=%d: %v", event, conv.ID, err)
		return nil, err
	}
	return msg, nil
}

// chatSlackHandleEdit：编辑/删除不会同步给访客。只有改的是已发给访客的客服消息、且文本真的变了
// （链接展开 unfurl 也会触发 message_changed，文本不变）才在频道里提示一次。
func chatSlackHandleEdit(ctx context.Context, conv *Conversation, evt slackMessageEvt) string {
	var target string
	switch evt.Subtype {
	case "message_changed":
		m, prev := evt.Message, evt.PreviousMessage
		if m == nil || prev == nil || m.BotID != "" || prev.Text == m.Text {
			return "ignored"
		}
		if m.ThreadTS != "" && m.ThreadTS != m.TS {
			return "ignored" // 线程里的编辑
		}
		target = m.TS
	case "message_deleted":
		if evt.PreviousMessage != nil && (evt.PreviousMessage.BotID != "" ||
			(evt.PreviousMessage.ThreadTS != "" && evt.PreviousMessage.ThreadTS != evt.PreviousMessage.TS)) {
			return "ignored"
		}
		target = evt.DeletedTS
	}
	if target == "" {
		return "ignored"
	}
	var n int64
	if err := db.Get().WithContext(ctx).Model(&ConversationMessage{}).
		Where("conversation_id = ? AND slack_ts = ? AND kind = ?", conv.ID, target, MsgText).Count(&n).Error; err != nil {
		log.Errorf(ctx, "slack events: lookup edited message conv=%d: %v", conv.ID, err)
		return "error"
	}
	if n == 0 {
		return "ignored"
	}
	chatSlackWarnOnce(ctx, conv.SlackChannelID, evt.TS, chatSlackWarnEdited)
	return "edit_warned"
}

// chatSlackWarn 在频道里（非线程）回一条提示；失败只记日志。
func chatSlackWarn(ctx context.Context, channel, text string) {
	ctx, cancel := context.WithTimeout(ctx, chatSlackWarnTimeout)
	defer cancel()
	if _, err := slack.PostMessage(ctx, channel, text, nil); err != nil {
		log.Warnf(ctx, "slack events: post warning to %s: %v", channel, err)
	}
}

// chatSlackWarnOnce 按（频道, 事件 ts）去重后发提示：Slack 重投同一事件不会重复提示。
// Redis 不可用时放行（宁可重复提示也不吞提示）。
func chatSlackWarnOnce(ctx context.Context, channel, ts, text string) {
	if ts != "" {
		if ok, err := redis.Client().SetNX(ctx, chatSlackWarnKeyPrefix+channel+":"+ts, "1", time.Hour).Result(); err == nil && !ok {
			return
		}
	}
	chatSlackWarn(ctx, channel, text)
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
		switch v {
		case chatSlackStaffNegValue:
			return nil, "", errChatSlackNotStaff
		case chatSlackStaffNoEmailV:
			return nil, "", errChatSlackNoEmail
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
		// 没有可见邮箱是永久性的（权限范围/bot 账号），负缓存；网络、限流、5xx 才是临时错误。
		// 库对"无邮箱"没有导出类型化错误，按其固定文案识别（qtoolkit/slack 已锁版本）。
		if strings.Contains(err.Error(), "has no profile email") {
			redis.Client().Set(ctx, key, chatSlackStaffNoEmailV, chatSlackStaffNegTTL)
			return nil, "", errChatSlackNoEmail
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

var slackSpaceRunRe = regexp.MustCompile(`[ \t]{2,}`)

var slackEntityReplacer = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&")

// chatSlackNormalizeText 把 Slack 的标记还原成访客能读的文本：
// <url|label>、<url> → url；<mailto:x|x> → x；再还原 & < > 实体。
// 内部标识一律不外泄：<@U1>、<#C1|name>、<!here>/<!channel>/<!subteam^S|@grp> 整个去掉
// （只有 <!date^…|fallback> 保留 fallback 文本）。
// 先拆标记再还原实体：真打出的 "&lt;" 不会被当成标记二次解析。
func chatSlackNormalizeText(s string) string {
	s = slackMarkupRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := m[1 : len(m)-1]
		target, label, hasLabel := strings.Cut(inner, "|")
		switch {
		case strings.HasPrefix(target, "@"), strings.HasPrefix(target, "#"):
			return ""
		case strings.HasPrefix(target, "!date"):
			if hasLabel {
				return label
			}
			return ""
		case strings.HasPrefix(target, "!"):
			return ""
		}
		return strings.TrimPrefix(target, "mailto:")
	})
	s = slackSpaceRunRe.ReplaceAllString(s, " ")
	return slackEntityReplacer.Replace(s)
}
