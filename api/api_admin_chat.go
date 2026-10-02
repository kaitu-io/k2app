package center

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// 后台会话接口：Slack 是客服主界面，这里只是只读索引（列表 + 单会话消息），外加一个关闭动作。
// 响应字段与工单接口同一风格：camelCase、时间戳为 unix 秒、分页信封 {items, pagination}（page 从 1 开始）。

// adminChatMessageLimit 单会话详情最多返回的消息条数（取最新的，旧→新排列）。
var adminChatMessageLimit = 500

// adminChatEmailMatchLimit 邮箱子串过滤时，最多采纳多少个命中的身份行；防止短关键词把 IN 列表撑爆。
const adminChatEmailMatchLimit = 500

// registerAdminChatRoutes 注册后台会话路由；挂在 opsAdmin 组下，权限为 RoleSupport。
func registerAdminChatRoutes(g *gin.RouterGroup) {
	g.GET("/chat/conversations", RoleRequired(RoleSupport), api_admin_chat_list)
	g.GET("/chat/conversations/:uuid", RoleRequired(RoleSupport), api_admin_chat_detail)
	g.PUT("/chat/conversations/:uuid/close", RoleRequired(RoleSupport), api_admin_chat_close)
}

type AdminChatConversation struct {
	UUID           string `json:"uuid"`
	Brand          string `json:"brand"`
	SubjectKind    string `json:"subjectKind"`
	SubjectID      uint64 `json:"subjectId"`
	Email          string `json:"email"`
	Status         string `json:"status"`
	Handler        string `json:"handler"`
	EntryPath      string `json:"entryPath"`
	LastMessageAt  int64  `json:"lastMessageAt"`
	LastMessageBy  string `json:"lastMessageBy"`
	SlackPermalink string `json:"slackPermalink"` // 没有专属频道时为空串
	CreatedAt      int64  `json:"createdAt"`
}

type AdminChatMessage struct {
	ID         uint64 `json:"id"`
	SenderType string `json:"senderType"`
	SenderName string `json:"senderName"`
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	Meta       any    `json:"meta"`
	CreatedAt  int64  `json:"createdAt"`
}

type AdminChatDetail struct {
	Conversation AdminChatConversation `json:"conversation"`
	Messages     []AdminChatMessage    `json:"messages"`
	Truncated    bool                  `json:"truncated"` // 消息超过上限，只返回了最新的一部分
}

// adminChatEmails 解析一页会话的访客邮箱，key 为会话 id。
// guest：簇内最近的 email 标识；user：登录邮箱（同工单接口，后台明文展示）。取不到的留空。
func adminChatEmails(ctx context.Context, convs []Conversation) map[uint64]string {
	out := make(map[uint64]string, len(convs))
	var userIDs []uint64
	for _, c := range convs {
		switch c.SubjectKind {
		case SubjectGuest:
			if root, err := guestRootID(ctx, c.SubjectID); err == nil {
				out[c.ID] = guestEmail(ctx, root)
			}
		case SubjectUser:
			userIDs = append(userIDs, c.SubjectID)
		}
	}
	if len(userIDs) == 0 {
		return out
	}
	var idents []LoginIdentify
	if err := db.Get().WithContext(ctx).Where("user_id IN ? AND type = ?", userIDs, "email").Find(&idents).Error; err != nil {
		log.Errorf(ctx, "admin chat: load user emails: %v", err)
		return out
	}
	byUser := make(map[uint64]string, len(idents))
	for _, li := range idents {
		if v, err := secretDecryptString(ctx, li.EncryptedValue); err == nil {
			byUser[li.UserID] = v
		}
	}
	for _, c := range convs {
		if c.SubjectKind == SubjectUser {
			out[c.ID] = byUser[c.SubjectID]
		}
	}
	return out
}

func adminChatConversationDTO(c *Conversation, email string) AdminChatConversation {
	link := ""
	if c.SlackChannelID != "" {
		link = chatSlackChannelURL(c.SlackChannelID)
	}
	return AdminChatConversation{
		UUID: c.UUID, Brand: c.Brand, SubjectKind: c.SubjectKind, SubjectID: c.SubjectID, Email: email,
		Status: c.Status, Handler: c.Handler, EntryPath: c.EntryPath,
		LastMessageAt: c.LastMessageAt.Unix(), LastMessageBy: c.LastMessageBy,
		SlackPermalink: link, CreatedAt: c.CreatedAt.Unix(),
	}
}

// adminChatEmailScope 邮箱过滤：guest 的 email 标识（子串匹配，覆盖整个合并簇）或 user 的登录邮箱（精确匹配，登录标识按哈希索引）。
// 没有任何命中返回恒假条件，而不是放行全表。
func adminChatEmailScope(ctx context.Context, email string) func(*gorm.DB) *gorm.DB {
	d := db.Get().WithContext(ctx)
	email = strings.ToLower(strings.TrimSpace(email))

	var guestIDs []uint64
	d.Model(&GuestIdentity{}).Where("kind = ? AND value LIKE ?", IdentityEmail, "%"+email+"%").
		Limit(adminChatEmailMatchLimit).Pluck("guest_id", &guestIDs)
	var subjectGuests []uint64
	if len(guestIDs) > 0 {
		// 命中的 guest 可能是簇根也可能是被并入方：先取根，再取根下全部成员，会话挂在哪个成员上都能找到
		var roots []uint64
		d.Model(&Guest{}).Where("id IN ?", guestIDs).Pluck("COALESCE(merged_into_id, id)", &roots)
		d.Model(&Guest{}).Where("id IN ? OR merged_into_id IN ?", roots, roots).Limit(adminChatEmailMatchLimit*4).Pluck("id", &subjectGuests)
	}

	var userIDs []uint64
	d.Model(&LoginIdentify{}).Where("type = ? AND index_id = ?", "email", secretHashIt(ctx, []byte(email))).
		Pluck("user_id", &userIDs)

	return func(tx *gorm.DB) *gorm.DB {
		switch {
		case len(subjectGuests) > 0 && len(userIDs) > 0:
			return tx.Where("(subject_kind = ? AND subject_id IN ?) OR (subject_kind = ? AND subject_id IN ?)",
				SubjectGuest, subjectGuests, SubjectUser, userIDs)
		case len(subjectGuests) > 0:
			return tx.Where("subject_kind = ? AND subject_id IN ?", SubjectGuest, subjectGuests)
		case len(userIDs) > 0:
			return tx.Where("subject_kind = ? AND subject_id IN ?", SubjectUser, userIDs)
		}
		return tx.Where("1 = 0")
	}
}

// api_admin_chat_list GET /app/chat/conversations?status=&handler=&brand=&email=&page=&pageSize=
// 按 last_message_at 降序；分页同工单接口（page 从 1 开始，pageSize 上限 100）。
func api_admin_chat_list(c *gin.Context) {
	pagination := PaginationFromRequest(c)
	ctx := c.Request.Context()

	query := db.Get().WithContext(ctx).Model(&Conversation{}).Scopes(adminBrandScope(c, "brand"))
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	if handler := c.Query("handler"); handler != "" {
		query = query.Where("handler = ?", handler)
	}
	if email := strings.TrimSpace(c.Query("email")); email != "" {
		query = query.Scopes(adminChatEmailScope(ctx, email))
	}

	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count chat conversations: %v", err)
		Error(c, ErrorSystemError, "failed to count conversations")
		return
	}
	var convs []Conversation
	if err := query.Order("last_message_at DESC, id DESC").Offset(pagination.Offset()).Limit(pagination.PageSize).
		Find(&convs).Error; err != nil {
		log.Errorf(c, "failed to query chat conversations: %v", err)
		Error(c, ErrorSystemError, "failed to query conversations")
		return
	}

	emails := adminChatEmails(ctx, convs)
	items := make([]AdminChatConversation, len(convs))
	for i := range convs {
		items[i] = adminChatConversationDTO(&convs[i], emails[convs[i].ID])
	}
	ListWithData(c, items, pagination)
}

// adminChatLoad 按 uuid 取会话；带了 ?brand= 时同样受品牌过滤（跨品牌当作不存在）。
func adminChatLoad(c *gin.Context) (*Conversation, bool) {
	var conv Conversation
	err := db.Get().WithContext(c.Request.Context()).Scopes(adminBrandScope(c, "brand")).
		Where("uuid = ?", c.Param("uuid")).First(&conv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		Error(c, ErrorNotFound, "conversation not found")
		return nil, false
	}
	if err != nil {
		log.Errorf(c, "failed to load chat conversation: %v", err)
		Error(c, ErrorSystemError, "failed to load conversation")
		return nil, false
	}
	return &conv, true
}

// api_admin_chat_detail GET /app/chat/conversations/:uuid —— 会话 + 消息（含内部备注），旧→新，最多 adminChatMessageLimit 条（取最新）。
func api_admin_chat_detail(c *gin.Context) {
	conv, ok := adminChatLoad(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	var msgs []ConversationMessage
	if err := db.Get().WithContext(ctx).Where("conversation_id = ?", conv.ID).
		Order("created_at DESC, id DESC").Limit(adminChatMessageLimit + 1).Find(&msgs).Error; err != nil {
		log.Errorf(c, "failed to query chat messages: %v", err)
		Error(c, ErrorSystemError, "failed to query messages")
		return
	}
	truncated := len(msgs) > adminChatMessageLimit
	if truncated {
		msgs = msgs[:adminChatMessageLimit]
	}
	out := make([]AdminChatMessage, 0, len(msgs))
	for i := len(msgs) - 1; i >= 0; i-- { // 翻回旧→新
		m := chatMessageDTO(&msgs[i])
		var meta any
		if len(m.Meta) > 0 {
			meta = m.Meta
		}
		out = append(out, AdminChatMessage{ID: m.ID, SenderType: m.SenderType, SenderName: m.SenderName,
			Kind: m.Kind, Content: m.Content, Meta: meta, CreatedAt: msgs[i].CreatedAt.Unix()})
	}

	email := adminChatEmails(ctx, []Conversation{*conv})[conv.ID]
	Success(c, &AdminChatDetail{Conversation: adminChatConversationDTO(conv, email), Messages: out, Truncated: truncated})
}

// api_admin_chat_close PUT /app/chat/conversations/:uuid/close —— 与 Slack `!close` 同一套：
// 记 system 事件、关闭、归档频道（归档失败只记日志，chatSlackSweep 会补），并写审计日志。幂等。
func api_admin_chat_close(c *gin.Context) {
	conv, ok := adminChatLoad(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	wasOpen := conv.Status == ConvOpen
	if wasOpen {
		chatSlackSystemEvent(ctx, conv, "会话已关闭", ChatEventClosed)
	}
	if err := closeConversation(ctx, conv); err != nil {
		log.Errorf(c, "admin chat close conv=%d: %v", conv.ID, err)
		Error(c, ErrorSystemError, "failed to close conversation")
		return
	}
	if err := chatSlackArchive(ctx, conv); err != nil {
		log.Warnf(c, "admin chat close: archive channel conv=%d: %v", conv.ID, err)
	}
	WriteAuditLog(c, "chat_conversation_close", "conversation", conv.UUID, map[string]any{
		"conversationId": conv.ID, "brand": conv.Brand, "wasOpen": wasOpen,
	})
	SuccessEmpty(c)
}
