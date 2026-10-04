package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	chatMaxBodyBytes    = 16 << 10 // POST 请求体上限

	// 限流（每分钟）。IP 可伪造，所以新建 guest 与发消息另有全局上限、发消息另有按主体上限。
	chatSessionPerIPPerMin      = 30   // POST /session 每 IP
	chatMessagePerIPPerMin      = 60   // POST /messages、/email 每 IP
	chatReadPerIPPerMin         = 120  // GET /messages、/ws-token 每 IP
	chatGuestCreateGlobalPerMin = 600  // session 里新建 guest 的全局上限
	chatSendGlobalPerMin        = 1200 // 访客发消息的全局上限
	chatSendPerSubjectPerMin    = 20   // 每主体发消息
	chatConvCreatePerMin        = 30   // 每实例新建会话（只限新建，已有 open 会话的访客不受影响）
	chatEmailMaxPerCluster      = 3    // 每个 guest 簇最多留几个不同邮箱
)

func registerChatRoutes(api *gin.RouterGroup) {
	g := api.Group("/chat")
	{
		g.GET("/enabled", api_chat_enabled)
		g.POST("/session", api_chat_session)
		g.GET("/messages", api_chat_messages_list)
		g.POST("/messages", api_chat_messages_send)
		g.POST("/images", api_chat_images_upload)
		g.GET("/images/:token", api_chat_images_view) // 能力链接：令牌即授权，不看品牌与 cookie
		g.POST("/email", api_chat_email)
		g.GET("/ws-token", api_chat_ws_token)
		g.GET("/ws", api_chat_ws) // 品牌取自令牌，不依赖 ReqBrand
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
	Images       bool         `json:"images"` // 能不能发图片（服务端配了存储桶）
}

// chatMessageDTO 消息的完整 DTO（Meta 是合法 JSON 才原样输出，否则 null），含发送者真名——后台接口用。
// 面向访客的出口一律用 chatVisitorMessageDTO。图片消息的 content 是对象 key，对外换成签名的站内查看链接。
func chatMessageDTO(m *ConversationMessage) ChatMsgDTO {
	content := m.Content
	if m.Kind == MsgImage {
		content = chatImagePath(m.ID, chatImageVisitorTTL)
	}
	d := ChatMsgDTO{ID: m.ID, SenderType: m.SenderType, SenderName: m.SenderName, Kind: m.Kind,
		Content: content, CreatedAt: m.CreatedAt}
	if m.Meta != "" && json.Valid([]byte(m.Meta)) {
		d.Meta = json.RawMessage(m.Meta)
	}
	return d
}

// chatVisitorMessageDTO 面向访客的消息：访客不得知道客服是谁，客服消息的 senderName 一律输出空串
// （AI 与系统消息照旧）。访客的 HTTP 接口与 WebSocket 帧都只走这一个出口；库里、后台接口、Slack 保留真名。
func chatVisitorMessageDTO(m *ConversationMessage) ChatMsgDTO {
	d := chatMessageDTO(m)
	if m.SenderType == SenderStaff {
		d.SenderName = ""
	}
	return d
}

// chatVisitorMessageDTOs 访客视图的消息列表。
func chatVisitorMessageDTOs(msgs []ConversationMessage) []ChatMsgDTO {
	out := make([]ChatMsgDTO, 0, len(msgs))
	for i := range msgs {
		out = append(out, chatVisitorMessageDTO(&msgs[i]))
	}
	return out
}

func chatConversationDTO(conv *Conversation) *ChatConvDTO {
	if conv == nil {
		return nil
	}
	return &ChatConvDTO{UUID: conv.UUID, Status: conv.Status, Handler: conv.Handler}
}

// chatWelcomeDTO 欢迎语取该品牌的固定文案 chatAIWelcome(b)（不落库，前端渲染）；与访客语言无关。
func chatWelcomeDTO(b Brand) *ChatWelcome {
	text, opts := chatAIWelcome(b)
	w := &ChatWelcome{Text: text, Options: make([]ChatWelcomeOption, 0, len(opts))}
	for _, o := range opts {
		w.Options = append(w.Options, ChatWelcomeOption{Label: o.Label, Value: o.Value})
	}
	return w
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
// 簇内可能同时有多个 open 会话（same_sid 自动合并两个各有会话的 guest 时）：取最新的（id 最大），
// 较旧的那条不再被访客接触，随空闲自动关闭（closeIdleConversations）。
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

// chatLimitBody 给 POST 请求体加上限。
func chatLimitBody(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, chatMaxBodyBytes)
}

// chatSendCountScript 原子地计数并保证键带过期：INCR 与 EXPIRE 分两步时，后一步失败会留下
// 永不过期的计数键，该主体从此永久限流。键没有过期时间（首次计数，或历史遗留）就补上；
// 已有过期时间的不续期（固定窗口）。
const chatSendCountScript = `local n = redis.call("incr", KEYS[1])
if n == 1 or redis.call("pttl", KEYS[1]) < 0 then redis.call("pexpire", KEYS[1], ARGV[1]) end
return n`

// chatSubjectSendAllow 按主体限速（Redis 计数，60 秒窗口）。Redis 故障时放行并记日志——限流不应拖垮聊天。
func chatSubjectSendAllow(ctx context.Context, s chatSubject) bool {
	n, err := redis.Client().Eval(ctx, chatSendCountScript, []string{"chat:send:" + s.Channel()},
		time.Minute.Milliseconds()).Int64()
	if err != nil {
		log.Warnf(ctx, "chat send limiter: %v", err)
		return true
	}
	return n <= chatSendPerSubjectPerMin
}

// errChatConvCreateLimited 本实例新建会话已达每分钟上限。
var errChatConvCreateLimited = errors.New("chat conversation create limit reached")

// chatNewConversation 给主体开新会话，受每实例新建上限约束（只限新建：调用方已确认主体没有 open 会话）。
// 超限返回 errChatConvCreateLimited 并记 Error——正常流量远到不了这个数，触顶就是有人在刷。
func chatNewConversation(ctx context.Context, subj chatSubject) (*Conversation, error) {
	if !chatConvCreateLimiter.Allow("*") {
		log.Errorf(ctx, "chat: new conversation limit reached (%d/min per instance), rejecting %s", chatConvCreatePerMin, subj.Channel())
		return nil, errChatConvCreateLimited
	}
	entry, _ := redis.Client().Get(ctx, chatEntryKey(subj)).Result()
	conv, _, err := ensureConversation(ctx, subj, entry)
	return conv, err
}

// chatVisitorSubject 解析访客主体并过闸（session 以外的访客接口共用）；ok=false 时已写好响应。
// 硬关 / 品牌未开放：一律拒绝，已有 cookie 的访客也不例外。
// 仅预览：主体必须带预览标记（由 preview 或有效 resume 令牌的 session 请求写入）。
// Redis 读失败按没有标记处理（fail closed）。
func chatVisitorSubject(c *gin.Context) (chatSubject, bool) {
	access := chatAccessFor(ReqBrand(c))
	if access == chatAccessOff {
		Error(c, ErrorInvalidArgument, "chat is not available")
		return chatSubject{}, false
	}
	subj, ok := chatSubjectFromRequest(c, false)
	if !ok {
		Error(c, ErrorInvalidArgument, "no chat session")
		return chatSubject{}, false
	}
	if access == chatAccessPreview && !chatHasPreviewMarker(c.Request.Context(), subj) {
		Error(c, ErrorInvalidArgument, "chat is not available")
		return chatSubject{}, false
	}
	return subj, true
}

// chatHasPreviewMarker 主体是否带预览标记。Redis 读失败按没有处理（fail closed）。
func chatHasPreviewMarker(ctx context.Context, s chatSubject) bool {
	n, err := redis.Client().Exists(ctx, chatPreviewKey(s)).Result()
	return err == nil && n > 0
}

// chatSubjectAdmitted 该主体此刻能否使用访客会话（WebSocket 握手用，规则与 chatVisitorSubject 相同）：
// 关闭 / 品牌未开放一律不行；仅预览要求带预览标记。
func chatSubjectAdmitted(ctx context.Context, s chatSubject) bool {
	switch chatAccessFor(s.Brand) {
	case chatAccessOn:
		return true
	case chatAccessPreview:
		return chatHasPreviewMarker(ctx, s)
	}
	return false
}

// ---- handlers ----

// ChatEnabledResp 挂件的入口探测结果。
type ChatEnabledResp struct {
	Enabled bool `json:"enabled"`
}

// api_chat_enabled: GET /api/chat/enabled
// 挂件据此决定要不要给普通访客画入口（裁定 R26）。只读开关：不解析主体、不建 guest、不种 cookie、
// 不碰数据库，所以不限流。只有正常开放才算 true——仅预览阶段的预览访客 / 邮件回链走 session，不走这里。
func api_chat_enabled(c *gin.Context) {
	Success(c, &ChatEnabledResp{Enabled: chatAccessFor(ReqBrand(c)) == chatAccessOn})
}

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
	chatLimitBody(c)
	var req chatSessionReq
	// 空请求体合法；格式错误在做任何主体解析之前拒绝
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		Error(c, ErrorInvalidArgument, "invalid request")
		return
	}
	// 开放程度（品牌白名单 + 两个开关）。仅预览时，有效的继续对话令牌（签名有效、未过期、
	// 会话存在且属于本请求品牌）等同 preview：收到邮件链接的访客也能接上自己的对话。
	// 无效令牌什么都不改变；硬关 / 品牌未开放时 preview 与令牌都不放行。
	access := chatAccessFor(ReqBrand(c))
	if access == chatAccessPreview && !req.Preview && chatResumeTokenValid(ctx, ReqBrand(c), req.Resume) {
		req.Preview = true
	}
	// 预览 / 回链进来过的访客刷新页面后不再带 preview 或 resume：预览标记还在就视同预览
	// （下面会照常续期标记）。只读解析主体，不建 guest、不种 cookie。
	if access == chatAccessPreview && !req.Preview {
		if s, ok := chatSubjectFromRequest(c, false); ok && chatHasPreviewMarker(ctx, s) {
			req.Preview = true
		}
	}
	if access == chatAccessOff || (access == chatAccessPreview && !req.Preview) {
		Success(c, &ChatSessionResp{Enabled: false, Messages: []ChatMsgDTO{}})
		return
	}

	subj, ok, limited := chatResolveSubject(c, true)
	if limited {
		Error(c, ErrorTooManyRequests, "too many requests")
		return
	}
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
		msgs = chatVisitorMessageDTOs(rows)
	}
	resp := &ChatSessionResp{
		Enabled:      true,
		Conversation: chatConversationDTO(conv),
		Messages:     msgs,
		Welcome:      chatWelcomeDTO(subj.Brand),
		Images:       chatImagesEnabled(),
	}
	if u := chatWSURL(subj.Brand); u != "" {
		if tok := signChatWSToken(subj, chatWSTokenTTL); tok != "" {
			resp.WS = &ChatWSInfo{URL: u, Token: tok}
		}
	}
	Success(c, resp)
}

// chatResumeTokenValid 报告令牌是否有效：签名/有效期通过，且它指向的会话存在并属于该品牌。
func chatResumeTokenValid(ctx context.Context, brand Brand, token string) bool {
	if token == "" {
		return false
	}
	convUUID, err := parseChatResumeToken(token, time.Now())
	if err != nil {
		return false
	}
	var n int64
	if err := db.Get().WithContext(ctx).Model(&Conversation{}).
		Where("uuid = ? AND brand = ?", convUUID, string(brand)).Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

// chatApplyResume 处理邮件里的继续对话令牌：无效静默忽略。
// 令牌可在 7 天内重放，所以合并有多重边界：同品牌、会话主体必须是 guest，
// 且当前 guest 簇必须"全新"——名下没有任何状态的会话（否则别人把自己的链接发给受害者，
// 就能把受害者已有的对话并进自己的簇）；客服撤销过的合并不被令牌重做。
// 满足才合并（resume_link），返回合并后的根主体；否则原样返回，访客保留自己的历史。
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
	ids, err := guestClusterIDs(ctx, subj.ID)
	if err != nil {
		return subj
	}
	var own int64
	if err := db.Get().WithContext(ctx).Model(&Conversation{}).
		Where("brand = ? AND subject_kind = ? AND subject_id IN ?", string(subj.Brand), SubjectGuest, ids).
		Count(&own).Error; err != nil || own > 0 {
		return subj
	}
	if undone, err := guestMergeUndoneBetween(ctx, subj.ID, conv.SubjectID); err != nil || undone {
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
	if !chatReadLimiter.Allow(c.ClientIP()) {
		Error(c, ErrorTooManyRequests, "too many requests")
		return
	}
	subj, ok := chatVisitorSubject(c)
	if !ok {
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
		msgs = chatVisitorMessageDTOs(rows)
	}
	Success(c, &struct {
		Messages     []ChatMsgDTO `json:"messages"`
		Conversation *ChatConvDTO `json:"conversation"`
	}{msgs, chatConversationDTO(conv)})
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
	chatLimitBody(c)
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
	subj, ok := chatVisitorSubject(c)
	if !ok {
		return
	}
	// 全局额度放在最后扣：校验失败、没有主体、被按主体限速的请求都不该消耗所有访客共用的额度
	if !chatSubjectSendAllow(ctx, subj) || !chatSendGlobalLimiter.Allow("*") {
		Error(c, ErrorTooManyRequests, "too many requests")
		return
	}

	conv, err := findOpenChatConversation(ctx, subj)
	if err != nil {
		log.Errorf(ctx, "api_chat_messages_send: %v", err)
		Error(c, ErrorSystemError, "failed to load conversation")
		return
	}
	clientID := req.ClientID
	conv, msg, err := chatVisitorAppend(ctx, subj, conv, appendMessageInput{
		SenderType: SenderVisitor, Kind: req.Kind, Content: req.Content, ClientID: &clientID,
	})
	if err != nil {
		chatSendNewConvError(c, err)
		return
	}
	dto := chatVisitorMessageDTO(msg)
	Success(c, &struct {
		Message      ChatMsgDTO   `json:"message"`
		Conversation *ChatConvDTO `json:"conversation"`
	}{dto, chatConversationDTO(conv)})
}

// api_chat_images_upload: POST /api/chat/images（multipart：file = 图片，clientId 同发文字消息）
// 校验顺序与发文字一致：先校验请求、再解析主体、最后扣按主体与全局的额度；都过了才写 S3。
// 类型按内容嗅探（png / jpeg / gif / webp），上限 5 MB。
func api_chat_images_upload(c *gin.Context) {
	ctx := c.Request.Context()
	if !chatMessageLimiter.Allow(c.ClientIP()) {
		Error(c, ErrorTooManyRequests, "too many requests")
		return
	}
	store := chatImages()
	if store == nil {
		Error(c, ErrorInvalidArgument, "images not available")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, chatImageMaxBody)
	if err := c.Request.ParseMultipartForm(chatImageMaxBody); err != nil {
		Error(c, ErrorInvalidArgument, "invalid image")
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	clientID := c.Request.FormValue("clientId")
	if clientID == "" || len(clientID) > chatClientIDMaxLen {
		Error(c, ErrorInvalidArgument, "invalid clientId")
		return
	}
	fh, err := c.FormFile("file")
	if err != nil || fh.Size <= 0 || fh.Size > chatImageMaxBytes {
		Error(c, ErrorInvalidArgument, "invalid image")
		return
	}
	f, err := fh.Open()
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid image")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, chatImageMaxBytes+1))
	_ = f.Close()
	if err != nil || len(data) == 0 || len(data) > chatImageMaxBytes {
		Error(c, ErrorInvalidArgument, "invalid image")
		return
	}
	contentType := chatSniffImage(data)
	if contentType == "" {
		Error(c, ErrorInvalidArgument, "unsupported image type")
		return
	}
	subj, ok := chatVisitorSubject(c)
	if !ok {
		return
	}
	if !chatSubjectSendAllow(ctx, subj) || !chatSendGlobalLimiter.Allow("*") {
		Error(c, ErrorTooManyRequests, "too many requests")
		return
	}

	conv, err := findOpenChatConversation(ctx, subj)
	if err != nil {
		log.Errorf(ctx, "api_chat_images_upload: %v", err)
		Error(c, ErrorSystemError, "failed to load conversation")
		return
	}
	key := chatImageKey(subj.Brand, contentType, time.Now())
	if err := store.Put(ctx, key, contentType, data); err != nil {
		log.Errorf(ctx, "api_chat_images_upload: put %s: %v", key, err)
		Error(c, ErrorSystemError, "failed to store image")
		return
	}
	meta, _ := json.Marshal(map[string]any{"type": contentType, "size": len(data)})
	conv, msg, err := chatVisitorAppend(ctx, subj, conv, appendMessageInput{
		SenderType: SenderVisitor, Kind: MsgImage, Content: key, Meta: string(meta), ClientID: &clientID,
	})
	if err != nil {
		chatSendNewConvError(c, err)
		return
	}
	Success(c, &struct {
		Message      ChatMsgDTO   `json:"message"`
		Conversation *ChatConvDTO `json:"conversation"`
	}{chatVisitorMessageDTO(msg), chatConversationDTO(conv)})
}

// api_chat_images_view: GET /api/chat/images/:token —— 校验令牌，302 到短期 S3 下载地址。
// 令牌无效、过期、消息不存在或不是图片一律 404（不区分原因）。
func api_chat_images_view(c *gin.Context) {
	ctx := c.Request.Context()
	if !chatReadLimiter.Allow(c.ClientIP()) {
		c.Status(http.StatusTooManyRequests)
		return
	}
	id, err := parseChatImageToken(c.Param("token"), time.Now())
	store := chatImages()
	if err != nil || store == nil {
		c.Status(http.StatusNotFound)
		return
	}
	var m ConversationMessage
	if err := db.Get().WithContext(ctx).Where("id = ? AND kind = ?", id, MsgImage).First(&m).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Errorf(ctx, "api_chat_images_view: load %d: %v", id, err)
		}
		c.Status(http.StatusNotFound)
		return
	}
	u, err := store.PresignGet(ctx, m.Content, chatImageS3TTL)
	if err != nil {
		log.Errorf(ctx, "api_chat_images_view: presign %d: %v", id, err)
		c.Status(http.StatusBadGateway)
		return
	}
	c.Header("Cache-Control", "private, max-age=60")
	c.Header("Referrer-Policy", "no-referrer")
	c.Redirect(http.StatusFound, u)
}

// chatVisitorAppend 把访客消息追加到 conv（nil = 主体还没有 open 会话，先开一个），返回消息最终所在的会话。
// 追加带 RequireOpen：读到 conv 之后它被关闭（人工 !close、后台关闭、闲置关闭）的话，消息不落进已关闭的会话
// ——那个会话的 Slack 频道即将归档，落进去就再也到不了客服——而是对访客透明地开一个新会话再追加。
// 新开会话受每实例新建上限约束（errChatConvCreateLimited）。
func chatVisitorAppend(ctx context.Context, subj chatSubject, conv *Conversation, in appendMessageInput) (*Conversation, *ConversationMessage, error) {
	in.RequireOpen = true
	if conv != nil {
		msg, _, err := appendMessage(ctx, conv, in)
		if !errors.Is(err, errChatConversationClosed) {
			return conv, msg, err
		}
	}
	conv, err := chatNewConversation(ctx, subj)
	if err != nil {
		return nil, nil, err
	}
	// 新会话刚建好又被关掉的概率可以忽略；真撞上就报错，由访客端重试
	msg, _, err := appendMessage(ctx, conv, in)
	return conv, msg, err
}

// chatSendNewConvError 把访客发送的失败写成响应：新建会话超限 429，其余 500。
func chatSendNewConvError(c *gin.Context, err error) {
	if errors.Is(err, errChatConvCreateLimited) {
		Error(c, ErrorTooManyRequests, "too many requests")
		return
	}
	log.Errorf(c.Request.Context(), "api_chat_messages_send: %v", err)
	Error(c, ErrorSystemError, "failed to save message")
}

var errChatEmailCap = errors.New("chat email cap reached")

// chatCheckEmailCap 每个 guest 簇最多 chatEmailMaxPerCluster 个不同邮箱；重复提交已有的不算新增。
func chatCheckEmailCap(ctx context.Context, s chatSubject, email string) error {
	ids, err := chatSubjectIDs(ctx, s)
	if err != nil {
		return err
	}
	var have []string
	if err := db.Get().WithContext(ctx).Model(&GuestIdentity{}).
		Where("guest_id IN ? AND kind = ?", ids, IdentityEmail).Distinct().Pluck("value", &have).Error; err != nil {
		return fmt.Errorf("list emails: %w", err)
	}
	for _, v := range have {
		if v == email {
			return nil
		}
	}
	if len(have) >= chatEmailMaxPerCluster {
		return errChatEmailCap
	}
	return nil
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
	chatLimitBody(c)
	var req chatEmailReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request")
		return
	}
	// 与发信前同一个校验（chatMailAddrOK）：入口放进来、发信时才拒的地址只会让访客白等一封收不到的邮件
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if len(email) > 254 || !isValidEmail(email) || !chatMailAddrOK(email) {
		Error(c, ErrorInvalidArgument, "invalid email address")
		return
	}
	subj, ok := chatVisitorSubject(c)
	if !ok {
		return
	}
	if subj.Kind == SubjectGuest {
		if err := chatCheckEmailCap(ctx, subj, email); err != nil {
			if errors.Is(err, errChatEmailCap) {
				Error(c, ErrorInvalidArgument, "too many emails")
			} else {
				log.Errorf(ctx, "api_chat_email: %v", err)
				Error(c, ErrorSystemError, "failed to save email")
			}
			return
		}
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
	if !chatReadLimiter.Allow(c.ClientIP()) {
		Error(c, ErrorTooManyRequests, "too many requests")
		return
	}
	subj, ok := chatVisitorSubject(c)
	if !ok {
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
