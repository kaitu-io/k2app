package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 会话核心（客服聊天）：开会话、幂等追加、游标读取、状态流转、空闲关闭，
// 以及后续任务（实时推送 / AI / Slack 镜像 / 离线邮件）挂接的钩子点。
// 设计见 docs/superpowers/specs/2026-10-01-support-console-chatwoot-replacement-design.md

// chatSubject 是会话主体。ID 对 guest 是簇根 id。
type chatSubject struct {
	Brand Brand
	Kind  string // guest|user
	ID    uint64
}

// Channel 返回主体的广播频道名 "s:<brand>:<kind>:<id>"。
func (s chatSubject) Channel() string {
	return fmt.Sprintf("s:%s:%s:%d", s.Brand, s.Kind, s.ID)
}

const (
	ChatEventTransferHuman = "transfer_human"
	ChatEventHandedToAI    = "handed_to_ai"
	ChatEventClosed        = "closed"
	ChatEventAutoClosed    = "auto_closed"
)

// chatEventMeta 返回 system 事件的固定 Meta：{"event":"<name>"}。
func chatEventMeta(name string) string {
	b, _ := json.Marshal(map[string]string{"event": name})
	return string(b)
}

// 钩子：由后续任务在 init() 里追加，按注册顺序同步调用，各自内部自行异步（走 chatAsync）。
var (
	// chatAfterAppend 在追加成功且非 dup 后调用。
	chatAfterAppend []func(conv *Conversation, msg *ConversationMessage)
	// chatAfterStateChange 在会话状态可能变化后调用。
	chatAfterStateChange []func(conv *Conversation)
)

// chatAsync 供钩子内部异步用，默认 go f()；测试（chat_testmain_test.go）换成同步执行。
var chatAsync = chatAsyncDefault

// chatAsyncDefault 在新 goroutine 里执行 f；f 的 panic 只记日志，不拖垮进程。
func chatAsyncDefault(f func()) {
	go callChatHook("async", f)
}

// chatNotifyStateChange 通知所有状态钩子。setHandler/closeConversation 内部调用；
// 留邮箱等外部场景由调用方调用。
func chatNotifyStateChange(conv *Conversation) {
	for _, h := range chatAfterStateChange {
		callChatHook("state change", func() { h(conv) })
	}
}

// callChatHook 隔离单个钩子：panic 只记日志，不影响调用方，也不阻止后面的钩子。
func callChatHook(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf(context.Background(), "chat %s hook panic: %v", name, r)
		}
	}()
	f()
}

const (
	convLockTTL  = 10 * time.Second
	convLockWait = 5 * time.Second

	messagesAfterLimit = 200
)

// lockConversationCreate 取主体的短锁，串行化同一主体的并发开会话。
// "每个主体每个品牌至多一个 open 会话"没有 DB 唯一约束，靠它兜住。
func lockConversationCreate(ctx context.Context, s chatSubject) (release func(), err error) {
	rdb := redis.Client()
	key := "chat:conv:lock:" + s.Channel()
	token := generateId("clk")
	deadline := time.Now().Add(convLockWait)
	for {
		ok, err := rdb.SetNX(ctx, key, token, convLockTTL).Result()
		if err != nil {
			return nil, fmt.Errorf("lock conversation create: %w", err)
		}
		if ok {
			return func() {
				const script = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
				_ = rdb.Eval(context.Background(), script, []string{key}, token).Err()
			}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("lock conversation create %s: timeout", key)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func openConversationIn(d *gorm.DB, s chatSubject) (*Conversation, error) {
	var conv Conversation
	err := d.Where("brand = ? AND subject_kind = ? AND subject_id = ? AND status = ?",
		string(s.Brand), s.Kind, s.ID, ConvOpen).Order("id DESC").First(&conv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find open conversation: %w", err)
	}
	return &conv, nil
}

// openConversationFor 返回主体当前 open 的会话；没有则返回 nil, nil。
func openConversationFor(ctx context.Context, s chatSubject) (*Conversation, error) {
	return openConversationIn(db.Get().WithContext(ctx), s)
}

// ensureConversation 返回主体当前 open 的会话，没有就新建（created=true）。
func ensureConversation(ctx context.Context, s chatSubject, entryPath string) (*Conversation, bool, error) {
	if conv, err := openConversationFor(ctx, s); err != nil || conv != nil {
		return conv, false, err
	}
	release, err := lockConversationCreate(ctx, s)
	if err != nil {
		return nil, false, err
	}
	defer release()
	// 拿锁后复查：并发的另一方可能已经建好
	if conv, err := openConversationFor(ctx, s); err != nil || conv != nil {
		return conv, false, err
	}
	now := time.Now()
	conv := &Conversation{
		UUID: uuid.NewString(), Brand: string(s.Brand), SubjectKind: s.Kind, SubjectID: s.ID,
		Status: ConvOpen, Handler: HandlerAI, EntryPath: entryPath, LastMessageAt: now,
	}
	if err := db.Get().WithContext(ctx).Create(conv).Error; err != nil {
		return nil, false, fmt.Errorf("create conversation: %w", err)
	}
	return conv, true, nil
}

// errChatSlackTSOtherConversation：slack_ts 已被另一个会话的消息占用。
var errChatSlackTSOtherConversation = errors.New("slack_ts belongs to another conversation")

type appendMessageInput struct {
	SenderType, SenderName, Kind, Content, Meta string
	SenderID                                    uint64
	ClientID, SlackTS                           *string
}

// appendMessage 落库并更新会话 last_message_at/by（DB 与传入的 conv 都更新）。
// ClientID 或 SlackTS 与已有消息冲突时返回已存在的那条且 dup=true，不触发钩子。
// 幂等靠唯一索引冲突后回查，不靠先查后插。不检查会话状态（关闭后仍可追加 system 事件）。
func appendMessage(ctx context.Context, conv *Conversation, in appendMessageInput) (*ConversationMessage, bool, error) {
	now := time.Now()
	msg := &ConversationMessage{
		ConversationID: conv.ID, SenderType: in.SenderType, SenderID: in.SenderID, SenderName: in.SenderName,
		Kind: in.Kind, Content: in.Content, Meta: in.Meta, ClientID: in.ClientID, SlackTS: in.SlackTS, CreatedAt: now,
	}
	d := db.Get().WithContext(ctx)
	by := conv.LastMessageBy
	upd := map[string]any{"last_message_at": now}
	if in.Kind != MsgNote && (in.SenderType == SenderVisitor || in.SenderType == SenderAI || in.SenderType == SenderStaff) {
		by = in.SenderType
		upd["last_message_by"] = by
	}
	// 插入与更新会话在同一事务：更新失败则消息一并回滚，重试不会被当成 dup 吞掉而漏掉钩子
	var dupKey bool
	err := d.Transaction(func(tx *gorm.DB) error {
		// 冲突是预期路径：静默 logger 避免把幂等重试刷成错误日志
		if err := tx.Session(&gorm.Session{Logger: logger.Discard}).Create(msg).Error; err != nil {
			if isDuplicateKeyErr(err) {
				dupKey = true
			}
			return err
		}
		return tx.Model(&Conversation{}).Where("id = ?", conv.ID).Updates(upd).Error
	})
	if dupKey {
		existing, err := findDuplicateMessage(d, conv.ID, in)
		if err != nil {
			return nil, false, err
		}
		// slack_ts 全局唯一：冲突的可能是别的会话的消息，不能当作本会话的 dup
		if existing.ConversationID != conv.ID {
			return nil, false, errChatSlackTSOtherConversation
		}
		return existing, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("append message: %w", err)
	}
	conv.LastMessageAt, conv.LastMessageBy = now, by

	for _, h := range chatAfterAppend {
		callChatHook("append", func() { h(conv, msg) })
	}
	return msg, false, nil
}

// findDuplicateMessage 回查唯一索引冲突的那条已有消息。
func findDuplicateMessage(d *gorm.DB, convID uint64, in appendMessageInput) (*ConversationMessage, error) {
	q := d.Where("1 = 0")
	if in.ClientID != nil {
		q = q.Or("conversation_id = ? AND client_id = ?", convID, *in.ClientID)
	}
	if in.SlackTS != nil {
		q = q.Or("slack_ts = ?", *in.SlackTS)
	}
	var existing ConversationMessage
	if err := d.Where(q).Order("id").First(&existing).Error; err != nil {
		return nil, fmt.Errorf("load duplicate message: %w", err)
	}
	return &existing, nil
}

// messagesAfter 按 id 升序返回 afterID 之后的消息，最多 200 条；visitorView 过滤内部备注（kind=note）。
func messagesAfter(ctx context.Context, convID, afterID uint64, visitorView bool) ([]ConversationMessage, error) {
	q := db.Get().WithContext(ctx).Where("conversation_id = ? AND id > ?", convID, afterID)
	if visitorView {
		q = q.Where("kind <> ?", MsgNote)
	}
	var msgs []ConversationMessage
	if err := q.Order("id ASC").Limit(messagesAfterLimit).Find(&msgs).Error; err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return msgs, nil
}

// setHandler 切换处理方（ai|human）；无变化时不写库、不通知。
func setHandler(ctx context.Context, conv *Conversation, handler string) error {
	if handler != HandlerAI && handler != HandlerHuman {
		return fmt.Errorf("invalid handler %q", handler)
	}
	// 以 DB 为准：条件更新带 handler<>目标，未匹配（RowsAffected=0）即无变化。
	// 不信任内存里的 conv.Handler（可能已陈旧）。
	res := db.Get().WithContext(ctx).Model(&Conversation{}).
		Where("id = ? AND handler <> ?", conv.ID, handler).Update("handler", handler)
	if res.Error != nil {
		return fmt.Errorf("set handler: %w", res.Error)
	}
	conv.Handler = handler
	if res.RowsAffected == 0 {
		return nil
	}
	chatNotifyStateChange(conv)
	return nil
}

// closeConversationIn 把 open 会话置 closed；返回是否真的发生了关闭（已关闭则 false）。
// 条件更新带 status=open，并发下只有一方得到 true。
func closeConversationIn(d *gorm.DB, conv *Conversation) (bool, error) {
	now := time.Now()
	res := d.Model(&Conversation{}).Where("id = ? AND status = ?", conv.ID, ConvOpen).
		Updates(map[string]any{"status": ConvClosed, "closed_at": now})
	if res.Error != nil {
		return false, fmt.Errorf("close conversation: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	conv.Status, conv.ClosedAt = ConvClosed, &now
	return true, nil
}

// closeConversation 关闭会话并通知；已关闭时无操作。
func closeConversation(ctx context.Context, conv *Conversation) error {
	changed, err := closeConversationIn(db.Get().WithContext(ctx), conv)
	if err != nil {
		return err
	}
	if changed {
		chatNotifyStateChange(conv)
	}
	return nil
}

// closeIdleConversations 关闭空闲会话并返回它们：handler=ai 用 aiIdle，handler=human 用 humanIdle（按 last_message_at）。
func closeIdleConversations(ctx context.Context, aiIdle, humanIdle time.Duration) ([]Conversation, error) {
	return closeIdleConversationsIn(ctx, aiIdle, humanIdle, "")
}

// closeIdleConversationsIn 同 closeIdleConversations；brand 非空时只处理该品牌（测试隔离用，生产传空）。
func closeIdleConversationsIn(ctx context.Context, aiIdle, humanIdle time.Duration, brand string) ([]Conversation, error) {
	d := db.Get().WithContext(ctx)
	now := time.Now()
	var candidates []Conversation
	q := d.Where("status = ?", ConvOpen)
	if brand != "" {
		q = q.Where("brand = ?", brand)
	}
	if err := q.Where("(handler = ? AND last_message_at < ?) OR (handler = ? AND last_message_at < ?)",
		HandlerAI, now.Add(-aiIdle), HandlerHuman, now.Add(-humanIdle)).
		Order("id").Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("list idle conversations: %w", err)
	}
	var closed []Conversation
	for i := range candidates {
		c := &candidates[i]
		changed, err := closeConversationIn(d, c)
		if err != nil {
			return closed, err
		}
		if !changed {
			continue
		}
		chatNotifyStateChange(c)
		closed = append(closed, *c)
	}
	return closed, nil
}
