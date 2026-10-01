package center

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/openai/filesearch"
	"github.com/wordgate/qtoolkit/redis"
)

type aiAskFn = func(ctx context.Context, question string, history []filesearch.Message) (string, error)

// withAIAsk 替换 chatAIAsk，返回调用计数；测试结束恢复。
func withAIAsk(t *testing.T, fn aiAskFn) *atomic.Int32 {
	t.Helper()
	orig := chatAIAsk
	var n atomic.Int32
	chatAIAsk = func(ctx context.Context, q string, h []filesearch.Message) (string, error) {
		n.Add(1)
		return fn(ctx, q, h)
	}
	t.Cleanup(func() { chatAIAsk = orig })
	return &n
}

func aiReplyText(s string) aiAskFn {
	return func(context.Context, string, []filesearch.Message) (string, error) { return s, nil }
}

// enableChatAI 开启 AI 追加钩子，测试结束恢复。
func enableChatAI(t *testing.T) {
	t.Helper()
	orig := chatAIHookEnabled
	chatAIHookEnabled = true
	t.Cleanup(func() { chatAIHookEnabled = orig })
}

// newAIConv 建一个 AI 处理中的会话，并开启 AI 钩子（所有 TestChatAI_* 都经由它）。
func newAIConv(t *testing.T) *Conversation {
	t.Helper()
	enableChatAI(t)
	skipIfNoConfig(t)
	conv, _, err := ensureConversation(context.Background(), newChatSubject(t), "/test")
	require.NoError(t, err)
	return conv
}

func visitorSays(t *testing.T, conv *Conversation, kind, content string) {
	t.Helper()
	_, _, err := appendMessage(context.Background(), conv, appendMessageInput{
		SenderType: SenderVisitor, Kind: kind, Content: content,
	})
	require.NoError(t, err)
}

func convMessages(t *testing.T, convID uint64) []ConversationMessage {
	t.Helper()
	var msgs []ConversationMessage
	require.NoError(t, db.Get().Where("conversation_id = ?", convID).Order("id").Find(&msgs).Error)
	return msgs
}

func countBy(msgs []ConversationMessage, sender, kind string) int {
	n := 0
	for _, m := range msgs {
		if m.SenderType == sender && (kind == "" || m.Kind == kind) {
			n++
		}
	}
	return n
}

func handlerOf(t *testing.T, convID uint64) string {
	t.Helper()
	var c Conversation
	require.NoError(t, db.Get().First(&c, convID).Error)
	return c.Handler
}

func TestChatAI_RepliesAsAI(t *testing.T) {
	conv := newAIConv(t)
	withAIAsk(t, aiReplyText("你好，请问怎么了"))
	visitorSays(t, conv, MsgText, "hi")

	msgs := convMessages(t, conv.ID)
	require.Equal(t, 1, countBy(msgs, SenderAI, MsgText))
	last := msgs[len(msgs)-1]
	assert.Equal(t, SenderAI, last.SenderType)
	assert.Equal(t, "AI", last.SenderName)
	assert.Equal(t, "你好，请问怎么了", last.Content)
	assert.Equal(t, HandlerAI, handlerOf(t, conv.ID))
}

func TestChatAI_TransferMarker(t *testing.T) {
	conv := newAIConv(t)
	withAIAsk(t, aiReplyText("稍等[TRANSFER_HUMAN]"))
	visitorSays(t, conv, MsgText, "我要退款")

	msgs := convMessages(t, conv.ID)
	var aiMsgs []ConversationMessage
	for _, m := range msgs {
		if m.SenderType == SenderAI {
			aiMsgs = append(aiMsgs, m)
		}
	}
	require.Len(t, aiMsgs, 1)
	assert.Equal(t, "稍等", aiMsgs[0].Content)
	assert.Equal(t, HandlerHuman, handlerOf(t, conv.ID))
	require.Equal(t, 1, countBy(msgs, SenderSystem, MsgEvent))
	ev := msgs[len(msgs)-1]
	assert.Equal(t, "已转人工", ev.Content)
	assert.Equal(t, chatEventMeta(ChatEventTransferHuman), ev.Meta)
}

func TestChatAI_ErrorTransfers(t *testing.T) {
	conv := newAIConv(t)
	withAIAsk(t, func(context.Context, string, []filesearch.Message) (string, error) {
		return "", errors.New("boom")
	})
	visitorSays(t, conv, MsgText, "help")

	msgs := convMessages(t, conv.ID)
	assert.Equal(t, HandlerHuman, handlerOf(t, conv.ID))
	assert.Equal(t, 1, countBy(msgs, SenderVisitor, MsgText), "访客消息必须仍在库里")
	assert.Equal(t, 0, countBy(msgs, SenderAI, ""))
	assert.Equal(t, 1, countBy(msgs, SenderSystem, MsgEvent))
}

func TestChatAI_EmptyReplyTransfers(t *testing.T) {
	conv := newAIConv(t)
	withAIAsk(t, aiReplyText("   "))
	visitorSays(t, conv, MsgText, "help")

	msgs := convMessages(t, conv.ID)
	assert.Equal(t, HandlerHuman, handlerOf(t, conv.ID))
	assert.Equal(t, 0, countBy(msgs, SenderAI, ""))
	assert.Equal(t, 1, countBy(msgs, SenderSystem, MsgEvent))
}

func TestChatAI_SilentWhenHuman(t *testing.T) {
	conv := newAIConv(t)
	require.NoError(t, setHandler(context.Background(), conv, HandlerHuman))
	calls := withAIAsk(t, aiReplyText("x"))
	visitorSays(t, conv, MsgText, "hello?")

	assert.Zero(t, calls.Load())
	assert.Equal(t, 0, countBy(convMessages(t, conv.ID), SenderAI, ""))
}

func TestChatAI_ClosedConversationIgnored(t *testing.T) {
	conv := newAIConv(t)
	calls := withAIAsk(t, aiReplyText("x"))
	// 会话在访客消息追加前已关闭：钩子不触发；直接调 chatAIHandle 也不应调用 AI
	_, _, err := appendMessage(context.Background(), conv, appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "a"})
	require.NoError(t, err)
	require.NoError(t, closeConversation(context.Background(), conv))
	calls.Store(0)
	chatAIHandle(context.Background(), conv.ID)

	assert.Zero(t, calls.Load())
}

func TestChatAI_CapAt20(t *testing.T) {
	conv := newAIConv(t)
	for i := 0; i < chatAIMaxReplies; i++ {
		_, _, err := appendMessage(context.Background(), conv, appendMessageInput{
			SenderType: SenderAI, SenderName: "AI", Kind: MsgText, Content: fmt.Sprintf("r%d", i),
		})
		require.NoError(t, err)
	}
	calls := withAIAsk(t, aiReplyText("x"))
	visitorSays(t, conv, MsgText, "again")

	assert.Zero(t, calls.Load())
	assert.Equal(t, HandlerHuman, handlerOf(t, conv.ID))
	assert.Equal(t, chatAIMaxReplies, countBy(convMessages(t, conv.ID), SenderAI, ""))
}

func TestChatAI_DropsReplyIfHumanTookOver(t *testing.T) {
	conv := newAIConv(t)
	withAIAsk(t, func(context.Context, string, []filesearch.Message) (string, error) {
		require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", conv.ID).Update("handler", HandlerHuman).Error)
		return "late reply", nil
	})
	visitorSays(t, conv, MsgText, "hi")

	assert.Equal(t, 0, countBy(convMessages(t, conv.ID), SenderAI, ""))
}

func TestChatAI_OptionReplyMapsToQuestion(t *testing.T) {
	cases := map[string]string{
		"install":  "我需要安装帮助",
		"purchase": "我想了解购买和续费",
		"usage":    "我有使用问题",
		"other":    "other",
	}
	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			conv := newAIConv(t)
			var got string
			withAIAsk(t, func(_ context.Context, q string, _ []filesearch.Message) (string, error) {
				got = q
				return "ok", nil
			})
			visitorSays(t, conv, MsgOptionReply, value)
			assert.Equal(t, want, got)
		})
	}
}

func TestChatAI_HistoryRoles(t *testing.T) {
	conv := newAIConv(t)
	ctx := context.Background()
	// 预置：35 条 visitor/ai 交替，之间夹 note 与 event、staff 消息；先让 AI 静默避免触发
	require.NoError(t, setHandler(ctx, conv, HandlerHuman))
	add := func(sender, kind, content string) {
		_, _, err := appendMessage(ctx, conv, appendMessageInput{SenderType: sender, Kind: kind, Content: content})
		require.NoError(t, err)
	}
	add(SenderVisitor, MsgText, "v-first")
	add(SenderStaff, MsgNote, "internal note")
	add(SenderSystem, MsgEvent, "已转人工")
	add(SenderStaff, MsgText, "staff-says")
	add(SenderAI, MsgText, "ai-says")
	require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", conv.ID).Update("handler", HandlerAI).Error)
	conv.Handler = HandlerAI

	var hist []filesearch.Message
	withAIAsk(t, func(_ context.Context, _ string, h []filesearch.Message) (string, error) {
		hist = h
		return "ok", nil
	})
	visitorSays(t, conv, MsgText, "v-last")

	require.Len(t, hist, 3)
	assert.Equal(t, filesearch.Message{Role: "user", Content: "v-first"}, hist[0])
	assert.Equal(t, filesearch.Message{Role: "assistant", Content: "staff-says"}, hist[1])
	assert.Equal(t, filesearch.Message{Role: "assistant", Content: "ai-says"}, hist[2])

	// 上限 30：再灌 40 条，历史只取最近 30 条
	conv.Handler = HandlerHuman // 内存里标 human，灌数据时不触发钩子
	for i := 0; i < 40; i++ {
		add(SenderVisitor, MsgText, fmt.Sprintf("bulk%d", i))
	}
	require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", conv.ID).Update("handler", HandlerAI).Error)
	conv.Handler = HandlerAI
	hist = nil
	visitorSays(t, conv, MsgText, "final")
	require.Len(t, hist, chatAIHistoryLimit)
	assert.Equal(t, "bulk39", hist[len(hist)-1].Content)
}

func TestChatAI_TransferOnlyOnce(t *testing.T) {
	conv := newAIConv(t)
	withAIAsk(t, func(context.Context, string, []filesearch.Message) (string, error) {
		return "", errors.New("boom")
	})
	visitorSays(t, conv, MsgText, "one")
	// 已是人工；强制再触发一次 AI 处理（模拟重复失败）不得再追加事件
	chatAIHandle(context.Background(), conv.ID)
	require.NoError(t, chatTransferToHuman(context.Background(), conv, ""))

	assert.Equal(t, 1, countBy(convMessages(t, conv.ID), SenderSystem, MsgEvent))
}

func TestChatAIWelcome(t *testing.T) {
	text, opts := chatAIWelcome()
	assert.Equal(t, "您好！请问需要什么帮助？（遇到安装问题时，您可以直接发截图给我 📷）", text)
	assert.Equal(t, []chatOption{
		{"📱 安装问题", "install"}, {"💳 购买/续费", "purchase"}, {"❓ 使用问题", "usage"},
	}, opts)
}

func TestChatAI_TimeoutTransfers(t *testing.T) {
	conv := newAIConv(t)
	orig := chatAIAskTimeout
	chatAIAskTimeout = 100 * time.Millisecond
	t.Cleanup(func() { chatAIAskTimeout = orig })
	withAIAsk(t, func(ctx context.Context, _ string, _ []filesearch.Message) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	visitorSays(t, conv, MsgText, "slow")

	assert.Equal(t, HandlerHuman, handlerOf(t, conv.ID))
	msgs := convMessages(t, conv.ID)
	assert.Equal(t, 1, countBy(msgs, SenderSystem, MsgEvent))
	assert.Equal(t, 0, countBy(msgs, SenderAI, ""))
}

func TestChatAI_MessageDuringLockIsAnsweredAfter(t *testing.T) {
	conv := newAIConv(t)
	var questions []string
	withAIAsk(t, func(_ context.Context, q string, _ []filesearch.Message) (string, error) {
		questions = append(questions, q)
		if len(questions) == 1 {
			visitorSays(t, conv, MsgText, "second") // 持锁期间到达：钩子拿不到锁
		}
		return "ok", nil
	})
	visitorSays(t, conv, MsgText, "first")

	assert.Equal(t, []string{"first", "second"}, questions)
	assert.Equal(t, 2, countBy(convMessages(t, conv.ID), SenderAI, MsgText))
}

func TestChatAI_LockHeldByOtherSkips(t *testing.T) {
	conv := newAIConv(t)
	key := fmt.Sprintf("chat:ai:%d", conv.ID)
	require.NoError(t, redis.Client().Set(context.Background(), key, "other", time.Minute).Err())
	t.Cleanup(func() { redis.Client().Del(context.Background(), key) })
	calls := withAIAsk(t, aiReplyText("x"))
	visitorSays(t, conv, MsgText, "hi")

	assert.Zero(t, calls.Load())
	assert.Equal(t, 0, countBy(convMessages(t, conv.ID), SenderAI, ""))
	assert.Equal(t, 0, countBy(convMessages(t, conv.ID), SenderSystem, ""))
	assert.Equal(t, HandlerAI, handlerOf(t, conv.ID))
}

func TestChatAI_RedisDownTransfers(t *testing.T) {
	conv := newAIConv(t)
	orig := chatAILockFn
	chatAILockFn = func(context.Context, uint64) (func(), bool, error) { return nil, false, errors.New("redis down") }
	t.Cleanup(func() { chatAILockFn = orig })
	calls := withAIAsk(t, aiReplyText("x"))
	visitorSays(t, conv, MsgText, "hi")

	assert.Zero(t, calls.Load())
	assert.Equal(t, HandlerHuman, handlerOf(t, conv.ID))
	assert.Equal(t, 1, countBy(convMessages(t, conv.ID), SenderSystem, MsgEvent))
}

func TestChatAI_RoundBound(t *testing.T) {
	conv := newAIConv(t)
	calls := withAIAsk(t, func(context.Context, string, []filesearch.Message) (string, error) {
		visitorSays(t, conv, MsgText, "more")
		return "ok", nil
	})
	visitorSays(t, conv, MsgText, "start")

	assert.EqualValues(t, chatAIMaxRounds, calls.Load())
}

func TestChatAI_DropsReplyIfClosedMeanwhile(t *testing.T) {
	conv := newAIConv(t)
	withAIAsk(t, func(context.Context, string, []filesearch.Message) (string, error) {
		require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", conv.ID).Update("status", ConvClosed).Error)
		return "late", nil
	})
	visitorSays(t, conv, MsgText, "hi")

	assert.Equal(t, 0, countBy(convMessages(t, conv.ID), SenderAI, ""))
}

// 外层 ctx 本身过期（而非只有单次调用超时）时，转人工仍必须成功：兜底走全新 ctx。
func TestChatAI_OuterContextExpiredStillTransfers(t *testing.T) {
	conv := newAIConv(t)
	conv.Handler = HandlerHuman // 内存里标 human，追加访客消息时不触发钩子
	visitorSays(t, conv, MsgText, "slow")
	withAIAsk(t, func(ctx context.Context, _ string, _ []filesearch.Message) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	chatAIHandle(ctx, conv.ID)

	assert.Equal(t, HandlerHuman, handlerOf(t, conv.ID))
	assert.Equal(t, 1, countBy(convMessages(t, conv.ID), SenderSystem, MsgEvent))
}
