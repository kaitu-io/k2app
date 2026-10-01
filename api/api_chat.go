package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
	"gorm.io/gorm"
)

// 访客聊天 HTTP 接口（官网挂件调用）。
// 会话在第一条访客消息时才创建；session 只下发静态欢迎语 + 已有会话，不落库。
// 设计见 docs/superpowers/specs/2026-10-01-support-console-chatwoot-replacement-design.md §5/§6

const (
	chatContentMaxRunes = 2000
	chatClientIDMaxLen  = 36
	chatEntryTTL        = 24 * time.Hour
)

func registerChatRoutes(api *gin.RouterGroup) {
	g := api.Group("/chat")
	{
		g.POST("/session", api_chat_session)
		g.GET("/messages", api_chat_messages_list)
		g.POST("/messages", api_chat_messages_send)
		g.POST("/email", api_chat_email)
		g.GET("/ws-token", api_chat_ws_token)
	}
}

// ---- DTO ----

type ChatMsgDTO struct {
	ID         uint64          `json:"id"`
	SenderType string          `json:"senderType"`
	SenderName string          `json:"senderName"`
	Kind       string          `json:"kind"`
	Content    string          `json:"content"`
	Meta       json.RawMessage `json:"meta"`
	CreatedAt  time.Time       `json:"createdAt"`
}

type ChatConvDTO struct {
	UUID    string `json:"uuid"`
	Status  string `json:"status"`
	Handler string `json:"handler"`
}

type ChatWelcomeOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type ChatWelcome struct {
	Text    string              `json:"text"`
	Options []ChatWelcomeOption `json:"options"`
}

type ChatWSInfo struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

type ChatSessionResp struct {
	Enabled      bool         `json:"enabled"`
	Conversation *ChatConvDTO `json:"conversation"`
	Messages     []ChatMsgDTO `json:"messages"`
	Welcome      *ChatWelcome `json:"welcome"`
	WS           *ChatWSInfo  `json:"ws"`
}

// chatMessageDTO 访客视图的消息（Meta 是合法 JSON 才原样输出，否则 null）。
func chatMessageDTO(m *ConversationMessage) ChatMsgDTO {
	d := ChatMsgDTO{ID: m.ID, SenderType: m.SenderType, SenderName: m.SenderName, Kind: m.Kind,
		Content: m.Content, CreatedAt: m.CreatedAt}
	if m.Meta != "" && json.Valid([]byte(m.Meta)) {
		d.Meta = json.RawMessage(m.Meta)
	}
	return d
}

func chatMessageDTOs(msgs []ConversationMessage) []ChatMsgDTO {
	out := make([]ChatMsgDTO, 0, len(msgs))
	for i := range msgs {
		out = append(out, chatMessageDTO(&msgs[i]))
	}
	return out
}

func chatConversationDTO(conv *Conversation) *ChatConvDTO {
	if conv == nil {
		return nil
	}
	return &ChatConvDTO{UUID: conv.UUID, Status: conv.Status, Handler: conv.Handler}
}

// chatWelcome 静态欢迎语（不落库，前端渲染）。品牌无关；按 Accept-Language 选中/英。
func chatWelcome(locale string) *ChatWelcome {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return &ChatWelcome{
			Text: "你好，有什么可以帮你？",
			Options: []ChatWelcomeOption{
				{Label: "连接问题", Value: "connection"},
				{Label: "购买与套餐", Value: "billing"},
				{Label: "账号问题", Value: "account"},
				{Label: "其他", Value: "other"},
			},
		}
	}
	return &ChatWelcome{
		Text: "Hi, how can we help?",
		Options: []ChatWelcomeOption{
			{Label: "Connection issue", Value: "connection"},
			{Label: "Plans and billing", Value: "billing"},
			{Label: "Account", Value: "account"},
			{Label: "Something else", Value: "other"},
		},
	}
}

// ---- 会话查找（簇感知） ----

// chatSubjectIDs 返回主体对应的 subject_id 集合：guest 取整簇（含已被并入的 id），user 就是自己。
func chatSubjectIDs(ctx context.Context, s chatSubject) ([]uint64, error) {
	if s.Kind != SubjectGuest {
		return []uint64{s.ID}, nil
	}
	return guestClusterIDs(ctx, s.ID)
}

// findOpenChatConversation 返回主体当前 open 的会话，按簇内任一 id 查找
// （合并前建的会话仍挂在被并入的 guest id 上）。没有返回 nil, nil。
func findOpenChatConversation(ctx context.Context, s chatSubject) (*Conversation, error) {
	return findChatConversation(ctx, s, ConvOpen)
}

// findLatestChatConversation 返回 open 会话；没有则返回最近关闭的一条（访客仍应看到关闭提示）。
func findLatestChatConversation(ctx context.Context, s chatSubject) (*Conversation, error) {
	conv, err := findChatConversation(ctx, s, ConvOpen)
	if err != nil || conv != nil {
		return conv, err
	}
	return findChatConversation(ctx, s, ConvClosed)
}

func findChatConversation(ctx context.Context, s chatSubject, status string) (*Conversation, error) {
	ids, err := chatSubjectIDs(ctx, s)
	if err != nil {
		return nil, err
	}
	var conv Conversation
	err = db.Get().WithContext(ctx).
		Where("brand = ? AND subject_kind = ? AND subject_id IN ? AND status = ?", string(s.Brand), s.Kind, ids, status).
		Order("id DESC").First(&conv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find %s conversation: %w", status, err)
	}
	return &conv, nil
}

// ---- Redis：入口页与预览标记 ----

func chatEntryKey(s chatSubject) string   { return "chat:entry:" + s.Channel() }
func chatPreviewKey(s chatSubject) string { return "chat:preview:" + s.Channel() }

// chatCleanEntryPath 入口页路径：去 query/hash，折叠带码路由，封顶 255。
func chatCleanEntryPath(p string) string {
	p = funnelCapRaw(p, funnelRawURLMax)
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if !strings.HasPrefix(p, "/") {
		return ""
	}
	return funnelTruncate(collapseFunnelCodePath(stripFunnelLocale(p)), funnelPathMaxLen)
}

// ---- handlers ----

type chatSessionReq struct {
	Path    string `json:"path"`
	Preview bool   `json:"preview"`
	Resume  string `json:"resume"`
}

// api_chat_session: POST /api/chat/session
func api_chat_session(c *gin.Context) {
	ctx := c.Request.Context()
	if !chatSessionLimiter.Allow(c.ClientIP()) {
		Error(c, ErrorTooManyRequests, "too many requests")
		return
	}
	var req chatSessionReq
	_ = c.ShouldBindJSON(&req)
	if !chatEnabled() && !req.Preview {
		Success(c, &ChatSessionResp{Enabled: false, Messages: []ChatMsgDTO{}})
		return
	}

	subj, ok := chatSubjectFromRequest(c, true)
	if !ok {
		Error(c, ErrorSystemError, "failed to resolve visitor")
		return
	}
	subj = chatApplyResume(ctx, c, subj, req.Resume)

	rdb := redis.Client()
	if path := chatCleanEntryPath(req.Path); path != "" {
		_ = rdb.Set(ctx, chatEntryKey(subj), path, chatEntryTTL).Err()
	}
	if req.Preview {
		_ = rdb.Set(ctx, chatPreviewKey(subj), "1", chatEntryTTL).Err()
	}

	conv, err := findOpenChatConversation(ctx, subj)
	if err != nil {
		log.Errorf(ctx, "api_chat_session: %v", err)
		Error(c, ErrorSystemError, "failed to load conversation")
		return
	}
	msgs := []ChatMsgDTO{}
	if conv != nil {
		rows, err := messagesAfter(ctx, conv.ID, 0, true)
		if err != nil {
			log.Errorf(ctx, "api_chat_session: %v", err)
			Error(c, ErrorSystemError, "failed to load messages")
			return
		}
		msgs = chatMessageDTOs(rows)
	}
	resp := &ChatSessionResp{
		Enabled:      true,
		Conversation: chatConversationDTO(conv),
		Messages:     msgs,
		Welcome:      chatWelcome(c.GetHeader("Accept-Language")),
	}
	if u := chatWSURL(subj.Brand); u != "" {
		if tok := signChatWSToken(subj, chatWSTokenTTL); tok != "" {
			resp.WS = &ChatWSInfo{URL: u, Token: tok}
		}
	}
	Success(c, resp)
}

// chatApplyResume 处理邮件里的继续对话令牌：无效静默忽略；有效且会话主体是同品牌 guest 时，
// 把当前 guest 与之合并（resume_link），返回合并后的根主体。user 主体或会话主体是 user 一律忽略。
func chatApplyResume(ctx context.Context, c *gin.Context, subj chatSubject, token string) chatSubject {
	if token == "" || subj.Kind != SubjectGuest {
		return subj
	}
	convUUID, err := parseChatResumeToken(token, time.Now())
	if err != nil {
		return subj
	}
	var conv Conversation
	if err := db.Get().WithContext(ctx).Where("uuid = ? AND brand = ? AND subject_kind = ?",
		convUUID, string(subj.Brand), SubjectGuest).First(&conv).Error; err != nil {
		return subj
	}
	if _, err := mergeGuests(ctx, subj.ID, conv.SubjectID, MergeResumeLink, nil, nil); err != nil {
		log.Warnf(ctx, "chat resume merge failed: %v", err)
		return subj
	}
	root, err := guestRootID(ctx, subj.ID)
	if err != nil {
		return subj
	}
	subj.ID = root
	return subj
}

// api_chat_messages_list: GET /api/chat/messages?after=
func api_chat_messages_list(c *gin.Context) {
	ctx := c.Request.Context()
	subj, ok := chatSubjectFromRequest(c, false)
	if !ok {
		Error(c, ErrorInvalidArgument, "no chat session")
		return
	}
	after, _ := strconv.ParseUint(c.Query("after"), 10, 64)
	conv, err := findLatestChatConversation(ctx, subj)
	if err != nil {
		log.Errorf(ctx, "api_chat_messages_list: %v", err)
		Error(c, ErrorSystemError, "failed to load conversation")
		return
	}
	msgs := []ChatMsgDTO{}
	if conv != nil {
		rows, err := messagesAfter(ctx, conv.ID, after, true)
		if err != nil {
			log.Errorf(ctx, "api_chat_messages_list: %v", err)
			Error(c, ErrorSystemError, "failed to load messages")
			return
		}
		msgs = chatMessageDTOs(rows)
	}
	Success(c, &struct {
		Messages []ChatMsgDTO `json:"messages"`
	}{msgs})
}

type chatSendReq struct {
	Kind     string `json:"kind"`
	Content  string `json:"content"`
	ClientID string `json:"clientId"`
}

// api_chat_messages_send: POST /api/chat/messages
func api_chat_messages_send(c *gin.Context) {
	ctx := c.Request.Context()
	if !chatMessageLimiter.Allow(c.ClientIP()) {
		Error(c, ErrorTooManyRequests, "too many requests")
		return
	}
	var req chatSendReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request")
		return
	}
	if req.Kind != MsgText && req.Kind != MsgOptionReply {
		Error(c, ErrorInvalidArgument, "invalid kind")
		return
	}
	if strings.TrimSpace(req.Content) == "" || utf8.RuneCountInString(req.Content) > chatContentMaxRunes {
		Error(c, ErrorInvalidArgument, "invalid content")
		return
	}
	if req.ClientID == "" || len(req.ClientID) > chatClientIDMaxLen {
		Error(c, ErrorInvalidArgument, "invalid clientId")
		return
	}
	subj, ok := chatSubjectFromRequest(c, false)
	if !ok {
		Error(c, ErrorInvalidArgument, "no chat session")
		return
	}

	conv, err := findOpenChatConversation(ctx, subj)
	if err != nil {
		log.Errorf(ctx, "api_chat_messages_send: %v", err)
		Error(c, ErrorSystemError, "failed to load conversation")
		return
	}
	if conv == nil {
		rdb := redis.Client()
		// 开关关闭时，只有预览会话能开新会话
		if !chatEnabled() {
			if n, err := rdb.Exists(ctx, chatPreviewKey(subj)).Result(); err != nil || n == 0 {
				Error(c, ErrorInvalidArgument, "chat is not available")
				return
			}
		}
		entry, _ := rdb.Get(ctx, chatEntryKey(subj)).Result()
		conv, _, err = ensureConversation(ctx, subj, entry)
		if err != nil {
			log.Errorf(ctx, "api_chat_messages_send: %v", err)
			Error(c, ErrorSystemError, "failed to create conversation")
			return
		}
	}
	clientID := req.ClientID
	msg, _, err := appendMessage(ctx, conv, appendMessageInput{
		SenderType: SenderVisitor, Kind: req.Kind, Content: req.Content, ClientID: &clientID,
	})
	if err != nil {
		log.Errorf(ctx, "api_chat_messages_send: %v", err)
		Error(c, ErrorSystemError, "failed to save message")
		return
	}
	dto := chatMessageDTO(msg)
	Success(c, &struct {
		Message      ChatMsgDTO   `json:"message"`
		Conversation *ChatConvDTO `json:"conversation"`
	}{dto, chatConversationDTO(conv)})
}

type chatEmailReq struct {
	Email string `json:"email"`
}

// api_chat_email: POST /api/chat/email —— 访客留邮箱（自报，弱证据，不触发合并）。
func api_chat_email(c *gin.Context) {
	ctx := c.Request.Context()
	if !chatMessageLimiter.Allow(c.ClientIP()) {
		Error(c, ErrorTooManyRequests, "too many requests")
		return
	}
	var req chatEmailReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if len(email) > 254 || !isValidEmail(email) {
		Error(c, ErrorInvalidArgument, "invalid email address")
		return
	}
	subj, ok := chatSubjectFromRequest(c, false)
	if !ok {
		Error(c, ErrorInvalidArgument, "no chat session")
		return
	}
	if subj.Kind == SubjectGuest {
		if err := addGuestEmail(ctx, subj.ID, subj.Brand, email); err != nil {
			log.Errorf(ctx, "api_chat_email: %v", err)
			Error(c, ErrorSystemError, "failed to save email")
			return
		}
	}
	if conv, err := findOpenChatConversation(ctx, subj); err != nil {
		log.Warnf(ctx, "api_chat_email: load conversation: %v", err)
	} else if conv != nil {
		chatNotifyStateChange(conv)
	}
	SuccessEmpty(c)
}

// api_chat_ws_token: GET /api/chat/ws-token —— 给 WebSocket 重连换新令牌。
func api_chat_ws_token(c *gin.Context) {
	subj, ok := chatSubjectFromRequest(c, false)
	if !ok {
		Error(c, ErrorInvalidArgument, "no chat session")
		return
	}
	tok := signChatWSToken(subj, chatWSTokenTTL)
	if tok == "" {
		Error(c, ErrorSystemError, "token unavailable")
		return
	}
	Success(c, &struct {
		Token string `json:"token"`
	}{tok})
}
