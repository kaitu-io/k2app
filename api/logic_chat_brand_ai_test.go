package center

import (
	"context"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/openai/filesearch"
)

// 不接 AI 的品牌（BrandConfig.ChatAI=false）：AI 的系统提示与知识库只写了开途，接上就会把开途的内容答给另一品牌的访客。

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func TestChatBrandAI_RegistryFlag(t *testing.T) {
	assert.True(t, BrandKaitu.Config().ChatAI)
	assert.False(t, BrandOverleap.Config().ChatAI)
}

func TestChatBrandAI_WelcomePerBrand(t *testing.T) {
	text, opts := chatAIWelcome(BrandOverleap)
	assert.NotEmpty(t, text)
	assert.False(t, hasHan(text), "overleap 欢迎语不得有中文: %q", text)
	assert.Empty(t, opts, "快捷选项是给 AI 的提问，没有 AI 就不给")

	text, opts = chatAIWelcome(BrandKaitu)
	assert.Equal(t, chatWelcomeText, text)
	assert.Len(t, opts, 3)
}

func TestChatBrandAI_ConversationStartsHumanAndAINeverAsked(t *testing.T) {
	skipIfNoConfig(t)
	enableChatAI(t)
	asked := withAIAsk(t, func(context.Context, string, []filesearch.Message) (string, error) {
		return "should not happen", nil
	})

	conv, created, err := ensureConversation(context.Background(), newChatSubjectBrand(t, BrandOverleap), "/support")
	require.NoError(t, err)
	require.True(t, created)
	assert.Equal(t, HandlerHuman, conv.Handler)

	visitorSays(t, conv, MsgText, "hello")
	assert.Zero(t, asked.Load(), "不接 AI 的品牌不得调用 AI")
	for _, m := range convMessages(t, conv.ID) {
		assert.NotEqual(t, SenderAI, m.SenderType)
		assert.NotEqual(t, MsgEvent, m.Kind, "一开始就是人工，不应出现转人工事件")
	}

	// 对照：开途照旧由 AI 接待并真的被问到
	k := newAIConv(t)
	assert.Equal(t, HandlerAI, k.Handler)
	visitorSays(t, k, MsgText, "hello")
	assert.Equal(t, int32(1), asked.Load())
}

func TestChatBrandAI_SessionWelcomeForBrand(t *testing.T) {
	r := chatSetup(t, true)
	w, data := chatOpenSession(t, r, "/support", func(q *TestRequest) *TestRequest {
		return q.WithHeader("X-K2-Brand", string(BrandOverleap))
	})
	if ck := chatCookie(w, CookieChatCid); ck != nil {
		chatCleanupCID(t, ck.Value)
	}
	welcome, ok := data["welcome"].(map[string]any)
	require.True(t, ok, "%v", data)
	assert.Equal(t, chatWelcomeTextFor(BrandOverleap), welcome["text"])
	opts, ok := welcome["options"].([]any)
	require.True(t, ok, "options 必须是数组（空也是 []，不是 null）: %v", welcome["options"])
	assert.Empty(t, opts)
}

func TestChatBrandAI_SlackAICommandRefused(t *testing.T) {
	e := newSlackEventsEnv(t)
	require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", e.conv.ID).
		Updates(map[string]any{"brand": string(BrandOverleap), "handler": HandlerHuman}).Error)
	e.conv.Brand, e.conv.Handler = string(BrandOverleap), HandlerHuman
	t.Cleanup(func() { // 改回原品牌，主体的清理按品牌查会话
		db.Get().Model(&Conversation{}).Where("id = ?", e.conv.ID).Update("brand", string(BrandKaitu))
	})
	e.f.Reset()

	e.send(e.msgEvent("!ai"))
	assert.Equal(t, HandlerHuman, e.reload().Handler)
	assert.Empty(t, e.msgs(), "被拒的命令不落标记、不追加事件")
	posts := slackMsgPosts(e.f, e.channel)
	require.Len(t, posts, 1)
	assert.Equal(t, chatSlackWarnNoAI, posts[0])
}
