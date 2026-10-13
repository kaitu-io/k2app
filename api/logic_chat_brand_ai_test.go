package center

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/openai/filesearch"
)

// 品牌与会话 AI：每个接 AI 的品牌有自己的知识库与提示词，互不串台；
// 不接 AI 的品牌（BrandConfig.ChatAI=false）会话一开始就是人工。

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// withoutChatAI 临时把品牌 b 设为不接 AI（注册表里的 *BrandConfig 是共享指针，测试结束还原）。
// 现在所有注册品牌都接 AI，"不接 AI"这条路径只能这样构造来测。
func withoutChatAI(t *testing.T, b Brand) {
	t.Helper()
	cfg := b.Config()
	orig := cfg.ChatAI
	cfg.ChatAI = false
	t.Cleanup(func() { cfg.ChatAI = orig })
}

func TestChatBrandAI_RegistryFlag(t *testing.T) {
	assert.True(t, BrandKaitu.Config().ChatAI)
	assert.True(t, BrandOverleap.Config().ChatAI)
}

// 接 AI 的品牌必须三件套齐全：知识库名、提示词；且知识库各品牌独占（共用 = 把一个品牌的内容答给另一个品牌）。
func TestChatBrandAI_EveryAIBrandIsWired(t *testing.T) {
	stores := map[string]Brand{}
	for _, b := range AllBrands() {
		cfg := b.Config()
		if !cfg.ChatAI {
			continue
		}
		assert.NotEmpty(t, cfg.ChatAIStore, "%s: ChatAI 但没有 ChatAIStore", b)
		assert.NotEmpty(t, strings.TrimSpace(chatAIBrandPrompts[b]), "%s: ChatAI 但 chatAIBrandPrompts 没有提示词", b)
		if other, dup := stores[cfg.ChatAIStore]; dup {
			t.Errorf("%s 与 %s 共用知识库 %q", b, other, cfg.ChatAIStore)
		}
		stores[cfg.ChatAIStore] = b
	}
}

// overleap 的 AI 面向海外：提示词、快捷选项、选项问题都不得出现中文或另一品牌名。
func TestChatBrandAI_OverleapNoChineseOrKaitu(t *testing.T) {
	kaituNames := []string{BrandKaitu.Config().DisplayName, "Kaitu", "kaitu"}
	// 查最终喂给模型的完整提示（品牌提示 + 输出约束），不只是品牌提示本身
	texts := map[string]string{"prompt": chatAISystemPrompt(BrandOverleap)}
	text, opts := chatAIWelcome(BrandOverleap)
	texts["welcome"] = text
	require.Len(t, opts, 3)
	for _, o := range opts {
		texts["option:"+o.Value] = o.Label
		texts["question:"+o.Value] = chatOptionQuestion(BrandOverleap, o.Value)
	}
	for name, s := range texts {
		assert.NotEmpty(t, s, name)
		assert.False(t, hasHan(s), "%s 不得有中文: %q", name, s)
		for _, k := range kaituNames {
			assert.NotContains(t, s, k, "%s 不得提及另一品牌", name)
		}
	}
	// 对照：开途的问题仍是中文（防止把 overleap 分支错写成默认分支）
	assert.True(t, hasHan(chatOptionQuestion(BrandKaitu, "install")))
}

// 知识库源文件（docs/customer-service/<brand>/，scripts/sync-kb.sh 原样上传）也不得串品牌：
// overleap 不出现中文与另一品牌名；开途不出现 Overleap，也不出现裸 "Kaitu"（安装包文件名 Kaitu_ 除外）。
func TestChatBrandAI_KnowledgeBaseNoCrossBrand(t *testing.T) {
	kaitu := regexp.MustCompile(`(?i)kaitu`)
	// 开途文档里允许的 "kaitu"：行内代码（命令、文件名、服务名）与域名——都不是给用户看的品牌名
	technical := regexp.MustCompile("`[^`]*`|(?i)[a-z0-9.-]*kaitu\\.(io|me)[^\\s）)]*")
	for _, b := range AllBrands() {
		if !b.Config().ChatAI {
			continue
		}
		files, err := filepath.Glob(filepath.Join("..", "docs", "customer-service", string(b), "[0-9][0-9]-*.md"))
		require.NoError(t, err)
		require.NotEmpty(t, files, "%s: 接 AI 的品牌必须有知识库源文件", b)
		for _, f := range files {
			raw, err := os.ReadFile(f)
			require.NoError(t, err)
			text, name := string(raw), filepath.Base(f)
			switch b {
			case BrandOverleap:
				assert.False(t, hasHan(text), "%s/%s 不得有中文", b, name)
				assert.NotRegexp(t, kaitu, text, "%s/%s 不得提及另一品牌", b, name)
			case BrandKaitu:
				assert.NotContains(t, strings.ToLower(text), "overleap", "%s/%s 不得提及另一品牌", b, name)
				prose := technical.ReplaceAllString(text, "")
				assert.Empty(t, kaitu.FindAllString(prose, -1), "%s/%s 用户可见文字只写「开途」，不写 Kaitu", b, name)
			}
		}
	}
}

func TestChatBrandAI_WelcomePerBrand(t *testing.T) {
	text, opts := chatAIWelcome(BrandOverleap)
	assert.NotEmpty(t, text)
	assert.False(t, hasHan(text), "overleap 欢迎语不得有中文: %q", text)
	assert.Len(t, opts, 3)

	withoutChatAI(t, BrandOverleap)
	_, opts = chatAIWelcome(BrandOverleap)
	assert.Empty(t, opts, "快捷选项是给 AI 的提问，没有 AI 就不给")

	text, opts = chatAIWelcome(BrandKaitu)
	assert.Equal(t, chatWelcomeText, text)
	assert.Len(t, opts, 3)
}

// 会话按自己的品牌去问自己的知识库、用自己的提示词。
func TestChatBrandAI_AskRoutesToBrandStore(t *testing.T) {
	skipIfNoConfig(t)
	enableChatAI(t)
	type call struct{ store, system string }
	var got []call
	orig := chatAIAsk
	t.Cleanup(func() { chatAIAsk = orig })
	chatAIAsk = func(_ context.Context, store, system, _ string, _ []string, _ []filesearch.Message) (string, error) {
		got = append(got, call{store, system})
		return "ok", nil
	}

	for _, b := range []Brand{BrandKaitu, BrandOverleap} {
		conv, created, err := ensureConversation(context.Background(), newChatSubjectBrand(t, b), "/support")
		require.NoError(t, err)
		require.True(t, created)
		assert.Equal(t, HandlerAI, conv.Handler, b)
		visitorSays(t, conv, MsgText, "hello")
	}
	require.Len(t, got, 2)
	assert.Equal(t, call{"crm", chatAISystemPrompt(BrandKaitu)}, got[0])
	assert.Equal(t, call{"overleap", chatAISystemPrompt(BrandOverleap)}, got[1])
	assert.NotEqual(t, got[0].system, got[1].system)
}

func TestChatBrandAI_ConversationStartsHumanAndAINeverAsked(t *testing.T) {
	skipIfNoConfig(t)
	enableChatAI(t)
	withoutChatAI(t, BrandOverleap)
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
	withoutChatAI(t, BrandOverleap)
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
	withoutChatAI(t, BrandOverleap)
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
