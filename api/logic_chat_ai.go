package center

import (
	"context"
	"fmt"
	"strings"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/openai/filesearch"
	"github.com/wordgate/qtoolkit/redis"
)

// AI 应答层（客服聊天）：访客发消息且会话由 AI 处理时，异步调用 filesearch 回复；
// 出错 / 空回复 / 标记 / 达到上限 → 转人工。人工始终可在 Slack 里接管。
// 设计见 docs/superpowers/specs/2026-10-01-support-console-chatwoot-replacement-design.md

const (
	// chatAIMaxReplies 单个会话内 AI 最多回复条数，达到后直接转人工。
	chatAIMaxReplies = 20
	// chatAIHistoryLimit 喂给 AI 的最近消息条数。
	chatAIHistoryLimit = 30
	// chatAIMaxRounds 单次调用内最多连续处理几轮（访客连发消息时）。
	chatAIMaxRounds = 5
	// chatAILockTTL 必须大于单次 AI 调用超时（chatAIAskTimeout 默认 45s）：
	// 否则调用还没结束锁就过期，同一会话会并发出两路回复。
	chatAILockTTL = 90 * time.Second
	// chatAITimeout 是整次 chatAIHandle（最多 5 轮）的宽松总上限。
	chatAITimeout = 5 * time.Minute
	// chatAIFallbackTimeout 转人工兜底路径的独立超时（不继承可能已过期的 ctx）。
	chatAIFallbackTimeout = 10 * time.Second

	chatWelcomeText = "您好！请问需要什么帮助？（遇到安装问题时，您可以直接发截图给我 📷）"
)

// chatAIAskTimeout 单次 AI 调用超时；变量以便测试缩短。必须小于 chatAILockTTL。
var chatAIAskTimeout = 45 * time.Second

// chatAILockFn 取锁函数；变量以便测试注入 Redis 故障。
var chatAILockFn = chatAILock

// chatAIAsk 调 OpenAI filesearch 取回复；测试里替换，避免打到真实 OpenAI。
var chatAIAsk = func(ctx context.Context, question string, history []filesearch.Message) (string, error) {
	opts := []filesearch.Option{filesearch.WithSystemPrompt(systemPrompt)}
	if len(history) > 0 {
		opts = append(opts, filesearch.WithHistory(history))
	}
	result, err := filesearch.Ask(ctx, "crm", question, opts...)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

type chatOption struct{ Label, Value string }

// chatAIWelcome 返回（简报名 chatWelcome 与 api_chat.go 的 chatWelcome(locale) 冲突）欢迎语与快捷选项。
func chatAIWelcome() (text string, options []chatOption) {
	return chatWelcomeText, []chatOption{
		{Label: "📱 安装问题", Value: "install"},
		{Label: "💳 购买/续费", Value: "purchase"},
		{Label: "❓ 使用问题", Value: "usage"},
	}
}

// chatOptionQuestion 把 option_reply 的 value 映射成给 AI 的问题；未知 value 原样返回。
func chatOptionQuestion(value string) string {
	switch value {
	case "install":
		return "我需要安装帮助"
	case "purchase":
		return "我想了解购买和续费"
	case "usage":
		return "我有使用问题"
	}
	return value
}

func init() {
	chatAfterAppend = append(chatAfterAppend, func(conv *Conversation, msg *ConversationMessage) {
		if msg.SenderType != SenderVisitor || conv.Handler != HandlerAI || conv.Status != ConvOpen {
			return
		}
		convID := conv.ID
		chatAsync(func() {
			ctx, cancel := context.WithTimeout(context.Background(), chatAITimeout)
			defer cancel()
			chatAIHandle(ctx, convID)
		})
	})
}

// chatAILock 取会话级 AI 锁。ok=false 表示别的调用正在处理；err 表示 Redis 不可用。
func chatAILock(ctx context.Context, convID uint64) (release func(), ok bool, err error) {
	rdb := redis.Client()
	key := fmt.Sprintf("chat:ai:%d", convID)
	token := generateId("ail")
	got, err := rdb.SetNX(ctx, key, token, chatAILockTTL).Result()
	if err != nil {
		return nil, false, err
	}
	if !got {
		return nil, false, nil
	}
	return func() {
		const script = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
		_ = rdb.Eval(context.Background(), script, []string{key}, token).Err()
	}, true, nil
}

func chatLoadConversation(ctx context.Context, convID uint64) (*Conversation, error) {
	var conv Conversation
	if err := db.Get().WithContext(ctx).First(&conv, convID).Error; err != nil {
		return nil, err
	}
	return &conv, nil
}

// chatAIActive 会话仍 open 且由 AI 处理。
func chatAIActive(conv *Conversation) bool {
	return conv.Status == ConvOpen && conv.Handler == HandlerAI
}

// chatLatestVisitorMessage 返回会话里最新的访客消息（没有则 nil）。
func chatLatestVisitorMessage(ctx context.Context, convID uint64) (*ConversationMessage, error) {
	var msgs []ConversationMessage
	err := db.Get().WithContext(ctx).
		Where("conversation_id = ? AND sender_type = ? AND kind IN ?", convID, SenderVisitor,
			[]string{MsgText, MsgOptionReply, MsgImage}).
		Order("id DESC").Limit(1).Find(&msgs).Error
	if err != nil || len(msgs) == 0 {
		return nil, err
	}
	return &msgs[0], nil
}

// chatAIHistory 取 beforeID 之前最近 30 条非 note、非 event 的消息：visitor→user，ai/staff→assistant。
func chatAIHistory(ctx context.Context, convID, beforeID uint64) ([]filesearch.Message, error) {
	var msgs []ConversationMessage
	err := db.Get().WithContext(ctx).
		Where("conversation_id = ? AND id < ? AND kind NOT IN ? AND sender_type IN ?", convID, beforeID,
			[]string{MsgNote, MsgEvent}, []string{SenderVisitor, SenderAI, SenderStaff}).
		Order("id DESC").Limit(chatAIHistoryLimit).Find(&msgs).Error
	if err != nil {
		return nil, err
	}
	hist := make([]filesearch.Message, 0, len(msgs))
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		role := "assistant"
		content := m.Content
		if m.SenderType == SenderVisitor {
			role = "user"
			if m.Kind == MsgOptionReply {
				content = chatOptionQuestion(content)
			}
		}
		hist = append(hist, filesearch.Message{Role: role, Content: content})
	}
	return hist, nil
}

// chatAIHandle 同步处理一个会话：取锁 → 回答最新访客消息 → 释放锁；
// 释放后若又有更新的访客消息（其钩子可能因锁被跳过）则再来一轮，最多 5 轮。
func chatAIHandle(ctx context.Context, convID uint64) {
	var answered uint64
	for round := 0; round < chatAIMaxRounds; round++ {
		release, ok, err := chatAILockFn(ctx, convID)
		if err != nil {
			// Redis 不可用：宁可转人工，也不能让访客既没 AI 也没人
			log.Errorf(ctx, "chat ai lock failed: conv=%d err=%v", convID, err)
			chatAIFallback(ctx, convID, "")
			return
		}
		if !ok {
			return
		}
		next, again := chatAIRound(ctx, convID, answered)
		release()
		if !again {
			return
		}
		answered = next
	}
}

// chatAIRound 执行一轮。返回本轮所答访客消息 id，以及释放锁后是否需要再检查（有更新的访客消息）。
func chatAIRound(ctx context.Context, convID, answered uint64) (uint64, bool) {
	conv, err := chatLoadConversation(ctx, convID)
	if err != nil {
		log.Errorf(ctx, "chat ai load conversation: conv=%d err=%v", convID, err)
		return answered, false
	}
	if !chatAIActive(conv) {
		return answered, false
	}
	q, err := chatLatestVisitorMessage(ctx, convID)
	if err != nil {
		log.Errorf(ctx, "chat ai load visitor message: conv=%d err=%v", convID, err)
		return answered, false
	}
	if q == nil || q.ID <= answered {
		return answered, false
	}

	if err := chatAIReply(ctx, conv, q); err != nil {
		log.Errorf(ctx, "chat ai round failed: conv=%d err=%v", convID, err)
		// 回复流程自身出错（如落库失败）：转人工兜底，避免访客无人应答
		chatAIFallback(ctx, convID, "")
		return q.ID, false
	}

	// 本轮已处理完：返回 true 让调用方先释放锁，再进下一轮；
	// 下一轮发现没有比 q 更新的访客消息就会直接退出。这样释放锁之后才判断，
	// 不会漏掉在持锁期间到达、其钩子因拿不到锁而被跳过的消息。
	return q.ID, true
}

// chatAIReply 对访客消息 q 产出一次回复（或转人工）。
func chatAIReply(ctx context.Context, conv *Conversation, q *ConversationMessage) error {
	var aiCount int64
	if err := db.Get().WithContext(ctx).Model(&ConversationMessage{}).
		Where("conversation_id = ? AND sender_type = ?", conv.ID, SenderAI).Count(&aiCount).Error; err != nil {
		return fmt.Errorf("count ai replies: %w", err)
	}
	if aiCount >= chatAIMaxReplies {
		return chatAIFallback(ctx, conv.ID, "")
	}

	history, err := chatAIHistory(ctx, conv.ID, q.ID)
	if err != nil {
		return fmt.Errorf("load history: %w", err)
	}
	question := q.Content
	if q.Kind == MsgOptionReply {
		question = chatOptionQuestion(question)
	}
	askCtx, cancel := context.WithTimeout(ctx, chatAIAskTimeout)
	reply, err := chatAIAsk(askCtx, question, history)
	cancel()
	if err != nil || strings.TrimSpace(reply) == "" {
		log.Errorf(ctx, "chat ai ask failed: conv=%d err=%v", conv.ID, err)
		return chatAIFallback(ctx, conv.ID, "")
	}
	if strings.HasSuffix(strings.TrimSpace(reply), transferHumanMarker) {
		return chatAIFallback(ctx, conv.ID, reply)
	}

	// AI 调用期间人工可能已接管或会话已关闭：以 DB 为准，丢弃这条回复
	fresh, err := chatLoadConversation(ctx, conv.ID)
	if err != nil {
		return fmt.Errorf("reload conversation: %w", err)
	}
	if !chatAIActive(fresh) {
		return nil
	}
	return chatAppendAI(ctx, fresh, reply)
}

// chatAIFallback 在全新的 context 上转人工：调用方的 ctx 可能已超时/取消，
// 沿用它会让转人工立刻失败、访客既没 AI 也没人。失败必须记 error 日志，不得吞掉。
func chatAIFallback(ctx context.Context, convID uint64, aiReply string) error {
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), chatAIFallbackTimeout)
	defer cancel()
	conv, err := chatLoadConversation(fctx, convID)
	if err == nil {
		err = chatTransferToHuman(fctx, conv, aiReply)
	}
	if err != nil {
		log.Errorf(fctx, "chat ai transfer to human failed: conv=%d err=%v", convID, err)
	}
	return err
}

func chatAppendAI(ctx context.Context, conv *Conversation, content string) error {
	_, _, err := appendMessage(ctx, conv, appendMessageInput{
		SenderType: SenderAI, SenderName: "AI", Kind: MsgText, Content: content,
	})
	return err
}

// chatTransferToHuman 去掉转人工标记后（非空时）作为 AI 消息追加，切到人工并追加"已转人工"事件。
// 以 DB 为准：会话已关闭或已是人工时什么都不做（不重复事件）。
func chatTransferToHuman(ctx context.Context, conv *Conversation, aiReply string) error {
	fresh, err := chatLoadConversation(ctx, conv.ID)
	if err != nil {
		return fmt.Errorf("reload conversation: %w", err)
	}
	if !chatAIActive(fresh) {
		return nil
	}
	if reply := strings.TrimSpace(strings.ReplaceAll(aiReply, transferHumanMarker, "")); reply != "" {
		if err := chatAppendAI(ctx, fresh, reply); err != nil {
			return err
		}
	}
	if err := setHandler(ctx, fresh, HandlerHuman); err != nil {
		return err
	}
	_, _, err = appendMessage(ctx, fresh, appendMessageInput{
		SenderType: SenderSystem, Kind: MsgEvent, Content: "已转人工", Meta: chatEventMeta(ChatEventTransferHuman),
	})
	return err
}
