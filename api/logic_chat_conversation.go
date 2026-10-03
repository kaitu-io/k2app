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
	"gorm.io/gorm/clause"
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
	handler := HandlerHuman // 品牌不接 AI（BrandConfig.ChatAI）：一开始就等人工
	if s.Brand.Config().ChatAI {
		handler = HandlerAI
	}
	conv := &Conversation{
		UUID: uuid.NewString(), Brand: string(s.Brand), SubjectKind: s.Kind, SubjectID: s.ID,
		Status: ConvOpen, Handler: handler, EntryPath: entryPath, LastMessageAt: now,
	}
	if err := db.Get().WithContext(ctx).Create(conv).Error; err != nil {
		return nil, false, fmt.Errorf("create conversation: %w", err)
	}
	return conv, true, nil
}

// chatAppendMidTx 仅供测试：在 appendMessage 事务内插入之后、更新会话之前调用，用来放大并发窗口。
var chatAppendMidTx func()

// errChatConversationClosed：RequireOpen / CloseConversation 的追加撞上了已关闭的会话，消息没有落库。
var errChatConversationClosed = errors.New("conversation is closed")

type appendMessageInput struct {
	SenderType, SenderName, Kind, Content, Meta string
	SenderID                                    uint64
	ClientID, SlackTS                           *string
	// RequireOpen：会话已关闭则不落库，返回 errChatConversationClosed（访客 / 客服 / AI 的发言都要带）。
	// 状态是在事务里对会话行 FOR UPDATE 之后读的，与关闭串行：不会有消息落在关闭之后。
	RequireOpen bool
	// CloseConversation：追加成功的同一事务里把会话置为 closed（人工关闭的 closed 事件用）。
	// 事件与关闭原子完成，访客消息插不进两者之间；会话已关闭同样返回 errChatConversationClosed。
	CloseConversation bool
}

// chatAppendPreLock 仅供测试：在 appendMessage 开事务之前调用，用来在"读到会话"与"追加"之间插入一次关闭。
var chatAppendPreLock func(convID uint64)

// appendMessage 落库并更新会话 last_message_at/by（DB 与传入的 conv 都更新）。
// ClientID 或 SlackTS 与本会话已有消息冲突时返回已存在的那条且 dup=true，不触发钩子。
// 幂等靠唯一索引冲突后回查，不靠先查后插。默认不检查会话状态（关闭后仍可追加 system 事件）；
// 发言类消息带 RequireOpen，人工关闭的事件带 CloseConversation，见 appendMessageInput。
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
	// 插入与更新会话在同一事务：更新失败则消息一并回滚，重试不会被当成 dup 吞掉而漏掉钩子。
	// 事务内先对会话行 SELECT ... FOR UPDATE，再分配消息 id：同一会话的 append 因此按
	// "拿锁 → 分配 id → 提交" 串行，提交顺序 == id 顺序。否则 T1 先拿到 id 10、T2 拿到 11 并先提交，
	// 钩子（推送/Slack 镜像）先看到 11，游标读取 afterID=11 的访客就永远漏掉 10。
	// 保证：消息 X 的钩子触发时，该会话所有 id < X 的消息已提交（对新读可见）。
	// 不保证：不同 goroutine 里钩子的调用顺序（锁在提交时释放，钩子在提交后跑），所以消费方按 id 排序/去重。
	if chatAppendPreLock != nil {
		chatAppendPreLock(conv.ID)
	}
	var dupKey, closed bool
	err := d.Transaction(func(tx *gorm.DB) error {
		var locked Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").Take(&locked, conv.ID).Error; err != nil {
			return err
		}
		// 行锁之下读到的状态才算数：关闭（条件 UPDATE 或 CloseConversation）与本次追加在这把锁上串行
		if (in.RequireOpen || in.CloseConversation) && locked.Status != ConvOpen {
			closed = true
			return errChatConversationClosed
		}
		// 时间戳在拿锁后取，保证 last_message_at 与 id 同序
		now = time.Now()
		msg.CreatedAt = now
		upd["last_message_at"] = now
		if in.CloseConversation {
			upd["status"], upd["closed_at"] = ConvClosed, now
		}
		// 冲突是预期路径：静默 logger 避免把幂等重试刷成错误日志
		if err := tx.Session(&gorm.Session{Logger: logger.Discard}).Create(msg).Error; err != nil {
			if isDuplicateKeyErr(err) {
				dupKey = true
			}
			return err
		}
		if chatAppendMidTx != nil {
			chatAppendMidTx()
		}
		return tx.Model(&Conversation{}).Where("id = ?", conv.ID).Updates(upd).Error
	})
	if closed {
		// 重试一条早已落在本会话里的消息（会话后来关了）仍是重复，不是"撞上关闭"
		if in.ClientID != nil || in.SlackTS != nil {
			quiet := d.Session(&gorm.Session{Logger: logger.Discard})
			if existing, ferr := findDuplicateMessage(quiet, conv.ID, in); ferr == nil {
				return existing, true, nil
			}
		}
		return nil, false, errChatConversationClosed
	}
	if dupKey {
		existing, err := findDuplicateMessage(d, conv.ID, in)
		if err != nil {
			return nil, false, err
		}
		return existing, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("append message: %w", err)
	}
	conv.LastMessageAt, conv.LastMessageBy = now, by
	if in.CloseConversation {
		conv.Status, conv.ClosedAt = ConvClosed, &now
	}

	for _, h := range chatAfterAppend {
		callChatHook("append", func() { h(conv, msg) })
	}
	return msg, false, nil
}

// findDuplicateMessage 回查唯一索引冲突的那条已有消息。两个幂等键都只在会话内唯一，所以只在本会话里找。
func findDuplicateMessage(d *gorm.DB, convID uint64, in appendMessageInput) (*ConversationMessage, error) {
	q := d.Where("1 = 0")
	if in.ClientID != nil {
		q = q.Or("conversation_id = ? AND client_id = ?", convID, *in.ClientID)
	}
	if in.SlackTS != nil {
		q = q.Or("conversation_id = ? AND slack_ts = ?", convID, *in.SlackTS)
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

// updateConversationLocked 是按条件改会话行（status / handler …）的唯一入口：事务里先按主键对会话行
// FOR UPDATE，decide 看锁下读到的当前行决定改什么（返回 nil = 不改），UPDATE 只按主键。
//
// 条件不能写进 UPDATE 的 WHERE：加锁顺序必须与 appendMessage 相同（先锁主键行）。WHERE 里带上有二级索引的列
// （status、last_message_at…），MariaDB 有时会改走二级索引——先锁索引记录再等主键行；而追加事务先锁主键行、
// 提交前再改同一条索引记录，互等即死锁（实测 1213），被回滚的可能是访客那条消息。
// TestConversationWrites_ByPrimaryKeyOnly 守着这条：对 conversations 的 UPDATE 一律 WHERE id = ?。
func updateConversationLocked(d *gorm.DB, convID uint64, decide func(cur *Conversation) map[string]any) (changed bool, err error) {
	err = d.Transaction(func(tx *gorm.DB) error {
		var cur Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "status", "handler", "last_message_at").Take(&cur, convID).Error; err != nil {
			return err
		}
		upd := decide(&cur)
		if len(upd) == 0 {
			return nil
		}
		if err := tx.Model(&Conversation{}).Where("id = ?", convID).Updates(upd).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}

// setHandler 切换处理方（ai|human）；无变化时不写库、不通知。
func setHandler(ctx context.Context, conv *Conversation, handler string) error {
	if handler != HandlerAI && handler != HandlerHuman {
		return fmt.Errorf("invalid handler %q", handler)
	}
	// 以 DB 为准：锁下读到的 handler 已是目标值即无变化。不信任内存里的 conv.Handler（可能已陈旧）。
	changed, err := updateConversationLocked(db.Get().WithContext(ctx), conv.ID, func(cur *Conversation) map[string]any {
		if cur.Handler == handler {
			return nil
		}
		return map[string]any{"handler": handler}
	})
	if err != nil {
		return fmt.Errorf("set handler: %w", err)
	}
	conv.Handler = handler
	if changed {
		chatNotifyStateChange(conv)
	}
	return nil
}

// closeConversationIf 把 open 会话置 closed；返回是否真的发生了关闭（已关闭、或不满足 eligible 则 false）。
// 条件在行锁之下判断（见 updateConversationLocked），并发下只有一方得到 true（后到的在锁下看到已关闭）。
func closeConversationIf(d *gorm.DB, conv *Conversation, eligible func(cur *Conversation) bool) (bool, error) {
	var closedAt time.Time
	changed, err := updateConversationLocked(d, conv.ID, func(cur *Conversation) map[string]any {
		if cur.Status != ConvOpen || (eligible != nil && !eligible(cur)) {
			return nil
		}
		closedAt = time.Now()
		return map[string]any{"status": ConvClosed, "closed_at": closedAt}
	})
	if err != nil {
		return false, fmt.Errorf("close conversation %d: %w", conv.ID, err)
	}
	if !changed {
		return false, nil
	}
	conv.Status, conv.ClosedAt = ConvClosed, &closedAt
	return true, nil
}

// closeConversationIn 把 open 会话置 closed；返回是否真的发生了关闭（已关闭则 false）。
func closeConversationIn(d *gorm.DB, conv *Conversation) (bool, error) {
	return closeConversationIf(d, conv, nil)
}

// chatCloseIdleOne 关闭一个闲置会话；包级变量只是测试接缝（在候选查询与关闭之间插入动作 / 注入失败）。
var chatCloseIdleOne = closeIdleConversationIn

// closeIdleConversationIn 闲置关闭一个候选会话：除了仍是 open，判定它闲置的依据也必须还成立
// （handler 没变、last_message_at 仍早于 cutoff）。候选查询之后被唤醒（来了新消息）或被人工接手的会话
// 不关，返回 false。
func closeIdleConversationIn(d *gorm.DB, conv *Conversation, cutoff time.Time) (bool, error) {
	handler := conv.Handler
	return closeConversationIf(d, conv, func(cur *Conversation) bool {
		return cur.Handler == handler && cur.LastMessageAt.Before(cutoff)
	})
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
	// 单个会话关闭失败不中断整批：其余照常关闭，错误汇总返回（下一轮再来）
	var closed []Conversation
	var errs []error
	for i := range candidates {
		c := &candidates[i]
		cutoff := now.Add(-aiIdle)
		if c.Handler == HandlerHuman {
			cutoff = now.Add(-humanIdle)
		}
		changed, err := chatCloseIdleOne(d, c, cutoff)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !changed {
			continue
		}
		chatNotifyStateChange(c)
		closed = append(closed, *c)
	}
	return closed, errors.Join(errs...)
}
