package center

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/redis"
)

// 离线邮件钩子对包内所有追加客服消息的测试都生效：默认关闭，且发信函数换成必然失败的桩——
// 任何测试都不会真的发信。需要的测试用 enableChatOfflineMail(t) / withOfflineMailSend(t)。
func init() {
	chatOfflineMailHookEnabled = false
	chatOfflineMailSend = func(ctx context.Context, b Brand, to, subject, body string) error {
		return errors.New("test: offline mail sender not faked")
	}
}

type offlineMail struct {
	Brand             Brand
	To, Subject, Body string
}

type offlineMailBox struct {
	mu    sync.Mutex
	mails []offlineMail
	err   error // 非空：发信失败
}

func (b *offlineMailBox) All() []offlineMail {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]offlineMail{}, b.mails...)
}

// withOfflineMailSend 把发信函数换成收件箱桩，测试结束恢复。
func withOfflineMailSend(t *testing.T) *offlineMailBox {
	t.Helper()
	box := &offlineMailBox{}
	orig := chatOfflineMailSend
	chatOfflineMailSend = func(ctx context.Context, b Brand, to, subject, body string) error {
		box.mu.Lock()
		defer box.mu.Unlock()
		if box.err != nil {
			return box.err
		}
		box.mails = append(box.mails, offlineMail{b, to, subject, body})
		return nil
	}
	t.Cleanup(func() { chatOfflineMailSend = orig })
	return box
}

// enableChatOfflineMail 开启离线邮件追加钩子，测试结束恢复。
func enableChatOfflineMail(t *testing.T) {
	t.Helper()
	orig := chatOfflineMailHookEnabled
	chatOfflineMailHookEnabled = true
	t.Cleanup(func() { chatOfflineMailHookEnabled = orig })
}

// offlineMailConv 建一个 guest 会话；email 非空则给 guest 挂上。返回会话、主体与收件箱桩。
func offlineMailConv(t *testing.T, brand Brand, email string) (*Conversation, chatSubject, *offlineMailBox) {
	t.Helper()
	skipIfNoConfig(t)
	old := viper.GetString("jwt.secret")
	viper.Set("jwt.secret", "test-secret-for-chat-tokens")
	t.Cleanup(func() { viper.Set("jwt.secret", old) })

	ctx := context.Background()
	s := newChatSubjectBrand(t, brand)
	if email != "" {
		require.NoError(t, addGuestEmail(ctx, s.ID, brand, email))
	}
	conv, _, err := ensureConversation(ctx, s, "/support")
	require.NoError(t, err)
	t.Cleanup(func() {
		redis.Client().Del(context.Background(), chatOfflineMailKey(conv.ID), chatOnlineKey(s))
	})
	return conv, s, withOfflineMailSend(t)
}

func offlineMailAddr() string { return "om-" + strings.ToLower(generateId("")) + "@example.test" }

func staffText(conv *Conversation, content string) *ConversationMessage {
	return &ConversationMessage{ConversationID: conv.ID, SenderType: SenderStaff, SenderName: "小王", Kind: MsgText, Content: content}
}

var offlineMailLinkRe = regexp.MustCompile(`(https://\S+)/support#chat=([A-Za-z0-9_.\-]+)`)

func TestOfflineMail_SentWhenOfflineWithEmail(t *testing.T) {
	addr := offlineMailAddr()
	conv, _, box := offlineMailConv(t, BrandKaitu, addr)

	sent, err := chatMaybeSendOfflineMail(context.Background(), conv, staffText(conv, "您好，问题已处理"))
	require.NoError(t, err)
	assert.True(t, sent)

	mails := box.All()
	require.Len(t, mails, 1)
	m := mails[0]
	assert.Equal(t, BrandKaitu, m.Brand)
	assert.Equal(t, addr, m.To)
	assert.NotEmpty(t, m.Subject)
	assert.Contains(t, m.Body, "您好，问题已处理")
	assert.Contains(t, m.Body, "/support#chat=")
	assert.NotContains(t, m.Body, "?chat=", "令牌必须在 URL 片段里，不能进查询参数")

	match := offlineMailLinkRe.FindStringSubmatch(m.Body)
	require.NotNil(t, match, "body=%s", m.Body)
	assert.Equal(t, BrandKaitu.Config().BaseURL, match[1])
	got, err := parseChatResumeToken(match[2], time.Now())
	require.NoError(t, err)
	assert.Equal(t, conv.UUID, got)
	// 令牌有效期 7 天：6 天后仍有效，8 天后失效
	_, err = parseChatResumeToken(match[2], time.Now().Add(6*24*time.Hour))
	assert.NoError(t, err)
	_, err = parseChatResumeToken(match[2], time.Now().Add(8*24*time.Hour))
	assert.Error(t, err)
}

func TestOfflineMail_BrandDecidesBaseURLAndSender(t *testing.T) {
	conv, _, box := offlineMailConv(t, BrandOverleap, offlineMailAddr())

	sent, err := chatMaybeSendOfflineMail(context.Background(), conv, staffText(conv, "hello"))
	require.NoError(t, err)
	require.True(t, sent)
	m := box.All()[0]
	assert.Equal(t, BrandOverleap, m.Brand)
	assert.Contains(t, m.Body, BrandOverleap.Config().BaseURL+"/support#chat=")
	assert.NotContains(t, m.Subject+m.Body, BrandKaitu.Config().DisplayName)
	assert.NotContains(t, strings.ToLower(m.Subject+m.Body), "kaitu")
}

func TestOfflineMail_SkippedWhenOnline(t *testing.T) {
	conv, s, box := offlineMailConv(t, BrandKaitu, offlineMailAddr())
	ctx := context.Background()
	require.NoError(t, redis.Client().Set(ctx, chatOnlineKey(s), "1", time.Minute).Err())

	sent, err := chatMaybeSendOfflineMail(ctx, conv, staffText(conv, "在吗"))
	require.NoError(t, err)
	assert.False(t, sent)
	assert.Empty(t, box.All())

	// 在线时不占用频率窗口：访客随后离线，下一条回复照常发信
	require.NoError(t, redis.Client().Del(ctx, chatOnlineKey(s)).Err())
	sent, err = chatMaybeSendOfflineMail(ctx, conv, staffText(conv, "留言给您"))
	require.NoError(t, err)
	assert.True(t, sent)
}

func TestOfflineMail_NoEmail(t *testing.T) {
	conv, _, box := offlineMailConv(t, BrandKaitu, "")

	sent, err := chatMaybeSendOfflineMail(context.Background(), conv, staffText(conv, "您好"))
	require.NoError(t, err)
	assert.False(t, sent)
	assert.Empty(t, box.All())
}

func TestOfflineMail_AIMessage(t *testing.T) {
	conv, _, box := offlineMailConv(t, BrandKaitu, offlineMailAddr())
	ctx := context.Background()

	for name, msg := range map[string]*ConversationMessage{
		"ai text":      {ConversationID: conv.ID, SenderType: SenderAI, Kind: MsgText, Content: "AI 回复"},
		"visitor text": {ConversationID: conv.ID, SenderType: SenderVisitor, Kind: MsgText, Content: "访客"},
		"system event": {ConversationID: conv.ID, SenderType: SenderSystem, Kind: MsgEvent, Content: "已转人工"},
		"staff note":   {ConversationID: conv.ID, SenderType: SenderStaff, Kind: MsgNote, Content: "内部备注"},
	} {
		sent, err := chatMaybeSendOfflineMail(ctx, conv, msg)
		require.NoError(t, err, name)
		assert.False(t, sent, name)
	}
	assert.Empty(t, box.All())
}

func TestOfflineMail_WithinTenMinutes(t *testing.T) {
	conv, _, box := offlineMailConv(t, BrandKaitu, offlineMailAddr())
	ctx := context.Background()

	sent, err := chatMaybeSendOfflineMail(ctx, conv, staffText(conv, "第一条"))
	require.NoError(t, err)
	require.True(t, sent)
	ttl := testMiniRedis.TTL(chatOfflineMailKey(conv.ID))
	assert.True(t, ttl > 9*time.Minute && ttl <= 10*time.Minute, "ttl=%v", ttl)

	sent, err = chatMaybeSendOfflineMail(ctx, conv, staffText(conv, "第二条"))
	require.NoError(t, err)
	assert.False(t, sent)
	assert.Len(t, box.All(), 1)

	// 窗口过后恢复
	testMiniRedis.FastForward(10*time.Minute + time.Second)
	sent, err = chatMaybeSendOfflineMail(ctx, conv, staffText(conv, "第三条"))
	require.NoError(t, err)
	assert.True(t, sent)
	assert.Len(t, box.All(), 2)
}

func TestOfflineMail_UserSubjectNotSent(t *testing.T) {
	conv, _, box := offlineMailConv(t, BrandKaitu, offlineMailAddr())
	asUser := *conv
	asUser.SubjectKind = SubjectUser

	sent, err := chatMaybeSendOfflineMail(context.Background(), &asUser, staffText(conv, "您好"))
	require.NoError(t, err)
	assert.False(t, sent)
	assert.Empty(t, box.All())
}

func TestOfflineMail_SendFailureReleasesWindow(t *testing.T) {
	conv, _, box := offlineMailConv(t, BrandKaitu, offlineMailAddr())
	ctx := context.Background()
	box.err = errors.New("smtp down")

	sent, err := chatMaybeSendOfflineMail(ctx, conv, staffText(conv, "第一条"))
	require.Error(t, err)
	assert.False(t, sent)

	// 发信失败不应白占 10 分钟窗口
	box.mu.Lock()
	box.err = nil
	box.mu.Unlock()
	sent, err = chatMaybeSendOfflineMail(ctx, conv, staffText(conv, "第二条"))
	require.NoError(t, err)
	assert.True(t, sent)
}

func TestOfflineMail_NoSecretNoMail(t *testing.T) {
	conv, _, box := offlineMailConv(t, BrandKaitu, offlineMailAddr())
	viper.Set("jwt.secret", "") // offlineMailConv 的 Cleanup 会还原

	sent, err := chatMaybeSendOfflineMail(context.Background(), conv, staffText(conv, "您好"))
	require.Error(t, err, "签不出回链令牌时不能发一封没有链接的邮件")
	assert.False(t, sent)
	assert.Empty(t, box.All())
}

// 钩子：追加客服文字消息即触发；默认（未开启）不发。
func TestOfflineMail_HookOnAppend(t *testing.T) {
	addr := offlineMailAddr()
	conv, _, box := offlineMailConv(t, BrandKaitu, addr)
	ctx := context.Background()

	_, _, err := appendMessage(ctx, conv, appendMessageInput{SenderType: SenderStaff, Kind: MsgText, Content: "钩子关闭"})
	require.NoError(t, err)
	assert.Empty(t, box.All(), "hook must be off by default in tests")

	enableChatOfflineMail(t)
	_, _, err = appendMessage(ctx, conv, appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "访客消息"})
	require.NoError(t, err)
	assert.Empty(t, box.All())

	_, _, err = appendMessage(ctx, conv, appendMessageInput{SenderType: SenderStaff, Kind: MsgText, Content: "钩子开启"})
	require.NoError(t, err)
	mails := box.All()
	require.Len(t, mails, 1)
	assert.Equal(t, addr, mails[0].To)
	assert.Contains(t, mails[0].Body, "钩子开启")
}
