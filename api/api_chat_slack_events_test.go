package center

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/redis"
)

const slackEventsTestSecret = "test-signing-secret-t9"

// slackEventsEnv 一个测试的全部上下文：假 Slack、路由、会话（已挂上专属频道）、客服账号。
type slackEventsEnv struct {
	t       *testing.T
	f       *fakeSlack
	router  http.Handler
	conv    *Conversation
	channel string
	slackID string // 客服的 Slack 用户 id
	email   string
	staff   *User
	seq     int
}

func newSlackEventsEnv(t *testing.T) *slackEventsEnv { return newSlackEventsEnvOpt(t, true) }

// newSlackEventsEnvOpt：scriptStaff=false 时不预置本环境客服的 users.info 响应（给用别的 Slack 用户的用例用，
// 否则队列里残留的响应会被别人的查询消费）。
func newSlackEventsEnvOpt(t *testing.T, scriptStaff bool) *slackEventsEnv {
	t.Helper()
	skipIfNoConfig(t)
	f := newFakeSlack(t)
	old := viper.Get("slack.signing_secret")
	viper.Set("slack.signing_secret", slackEventsTestSecret)
	t.Cleanup(func() { viper.Set("slack.signing_secret", old) })

	conv := slackConv(t, "", "/support")
	channel := "CEV" + generateId("")[10:]
	require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", conv.ID).Update("slack_channel_id", channel).Error)
	conv.SlackChannelID = channel

	e := &slackEventsEnv{t: t, f: f, router: SetupRouter(), conv: conv, channel: channel,
		slackID: "UEV" + generateId("")[10:], email: strings.ToLower(generateId("staff")) + "@example.com"}
	e.staff = createBrandUserWithEmail(t, BrandKaitu, e.email)
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", e.staff.ID).Update("roles", RoleSupport).Error)
	t.Cleanup(func() { redis.Client().Del(context.Background(), chatSlackStaffKeyPrefix+e.slackID) })
	if scriptStaff {
		e.slackUser(e.slackID, e.email, 1)
	}
	return e
}

// slackUser 让 users.info 对后续 n 次调用返回该邮箱。
func (e *slackEventsEnv) slackUser(id, email string, n int) {
	resps := make([]fakeSlackResp, n)
	for i := range resps {
		resps[i] = fakeSlackResp{Body: fmt.Sprintf(`{"ok":true,"user":{"id":%q,"profile":{"email":%q}}}`, id, email)}
	}
	e.f.Script("users.info", resps...)
}

func slackSign(secret, ts string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte("v0:" + ts + ":"))
	h.Write(body)
	return "v0=" + hex.EncodeToString(h.Sum(nil))
}

func slackPost(router http.Handler, secret string, ts time.Time, body []byte) *httptest.ResponseRecorder {
	tsStr := strconv.FormatInt(ts.Unix(), 10)
	req := httptest.NewRequest(http.MethodPost, "/webhook/slack/events", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slack-Request-Timestamp", tsStr)
	req.Header.Set("X-Slack-Signature", slackSign(secret, tsStr, body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func slackEventBody(evt map[string]any) []byte {
	b, _ := json.Marshal(map[string]any{"type": "event_callback", "event": evt})
	return b
}

// msgEvent 造一条频道消息事件；mod 可改字段。ts 默认每次唯一。
func (e *slackEventsEnv) msgEvent(text string, mod ...func(map[string]any)) map[string]any {
	e.seq++
	evt := map[string]any{"type": "message", "channel": e.channel, "user": e.slackID, "text": text,
		"ts": fmt.Sprintf("%d.%06d", time.Now().Unix(), e.seq) + generateId("")[10:14]}
	for _, m := range mod {
		m(evt)
	}
	return evt
}

func (e *slackEventsEnv) send(evt map[string]any) *httptest.ResponseRecorder {
	return slackPost(e.router, slackEventsTestSecret, time.Now(), slackEventBody(evt))
}

func (e *slackEventsEnv) msgs() []ConversationMessage {
	var out []ConversationMessage
	require.NoError(e.t, db.Get().Where("conversation_id = ?", e.conv.ID).Order("id").Find(&out).Error)
	return out
}

func (e *slackEventsEnv) reload() *Conversation { return slackReload(e.t, e.conv.ID) }

func TestSlackEvents_BadSignature401(t *testing.T) {
	e := newSlackEventsEnv(t)
	body := slackEventBody(e.msgEvent("hi"))

	t.Run("wrong secret", func(t *testing.T) {
		w := slackPost(e.router, "wrong-secret", time.Now(), body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
	t.Run("stale timestamp", func(t *testing.T) {
		w := slackPost(e.router, slackEventsTestSecret, time.Now().Add(-6*time.Minute), body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
	assert.Empty(t, e.msgs())
}

func TestSlackEvents_EmptySecretRejects(t *testing.T) {
	e := newSlackEventsEnv(t)
	viper.Set("slack.signing_secret", "")
	body := slackEventBody(e.msgEvent("hi"))
	// 用空密钥签的"合法"签名也必须被拒
	w := slackPost(e.router, "", time.Now(), body)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	urlv, _ := json.Marshal(map[string]any{"type": "url_verification", "challenge": "x"})
	assert.Equal(t, http.StatusUnauthorized, slackPost(e.router, "", time.Now(), urlv).Code)
	assert.Empty(t, e.msgs())
}

func TestSlackEvents_URLVerification(t *testing.T) {
	e := newSlackEventsEnv(t)
	body, _ := json.Marshal(map[string]any{"type": "url_verification", "challenge": "ch-9f3a", "token": "x"})
	w := slackPost(e.router, slackEventsTestSecret, time.Now(), body)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ch-9f3a", w.Body.String())
}

func TestSlackEvents_ChannelMessageReachesVisitor(t *testing.T) {
	e := newSlackEventsEnv(t)
	evt := e.msgEvent("请把日志发我看看")
	w := e.send(evt)
	require.Equal(t, http.StatusOK, w.Code)

	msgs := e.msgs()
	require.Len(t, msgs, 1)
	m := msgs[0]
	assert.Equal(t, SenderStaff, m.SenderType)
	assert.Equal(t, MsgText, m.Kind)
	assert.Equal(t, "请把日志发我看看", m.Content)
	assert.Equal(t, e.staff.ID, m.SenderID)
	assert.Equal(t, strings.Split(e.email, "@")[0], m.SenderName)
	require.NotNil(t, m.SlackTS)
	assert.Equal(t, evt["ts"], *m.SlackTS)
	assert.Equal(t, HandlerHuman, e.reload().Handler)

	visible, err := messagesAfter(context.Background(), e.conv.ID, 0, true)
	require.NoError(t, err)
	require.Len(t, visible, 1)
	assert.Equal(t, "请把日志发我看看", visible[0].Content)

	// 不回镜像：这条内容不会再被 post 回 Slack
	for _, p := range e.f.Posts(e.channel) {
		assert.NotContains(t, p, "请把日志发我看看")
	}
	assert.Empty(t, slackMsgPosts(e.f, e.channel), "reply must not be re-posted to Slack")
}

func TestSlackEvents_RetryDeduped(t *testing.T) {
	e := newSlackEventsEnv(t)
	evt := e.msgEvent("只发一次")
	assert.Equal(t, http.StatusOK, e.send(evt).Code)
	assert.Equal(t, http.StatusOK, e.send(evt).Code)
	assert.Len(t, e.msgs(), 1)
}

func TestSlackEvents_Ignored(t *testing.T) {
	e := newSlackEventsEnv(t)
	cases := map[string]map[string]any{
		"bot_id": e.msgEvent("bot", func(m map[string]any) { m["bot_id"] = "B1" }),
		"message_changed unfurl (text unchanged)": e.changedEvent("1700000000.000100", "same text", "same text"),
		"message_changed by bot (card refresh)":   e.changedEvent("1700000000.000101", "old", "new", func(m map[string]any) { m["bot_id"] = "B1" }),
		"thread_broadcast": e.msgEvent("广播回复", func(m map[string]any) {
			m["subtype"] = "thread_broadcast"
			m["thread_ts"] = "1700000000.000001"
		}),
		"message_deleted of unknown message": e.deletedEvent("1700000000.000999"),
		"channel_join":                       e.msgEvent("joined", func(m map[string]any) { m["subtype"] = "channel_join" }),
		"thread_reply":                       e.msgEvent("内部讨论", func(m map[string]any) { m["thread_ts"] = "1700000000.000001" }),
		"lobby":                              e.msgEvent("总览里的话", func(m map[string]any) { m["channel"] = fakeSlackLobbyID }),
		"unknown channel":                    e.msgEvent("陌生频道", func(m map[string]any) { m["channel"] = "CUNKNOWN" }),
		"not a message":                      e.msgEvent("x", func(m map[string]any) { m["type"] = "reaction_added" }),
	}
	for name, evt := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, http.StatusOK, e.send(evt).Code)
		})
	}
	assert.Empty(t, e.msgs())
	assert.Empty(t, e.f.CallsOf("chat.postMessage"))
	assert.Equal(t, HandlerAI, e.reload().Handler)
}

func TestSlackEvents_NonStaffRejected(t *testing.T) {
	e := newSlackEventsEnvOpt(t, false)
	// 邮箱存在但只是普通用户：不是客服
	plainEmail := strings.ToLower(generateId("plain")) + "@example.com"
	createBrandUserWithEmail(t, BrandKaitu, plainEmail)
	plainID := "UPL" + generateId("")[10:]
	t.Cleanup(func() { redis.Client().Del(context.Background(), chatSlackStaffKeyPrefix+plainID) })
	e.slackUser(plainID, plainEmail, 1)

	w := e.send(e.msgEvent("我不是客服", func(m map[string]any) { m["user"] = plainID }))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, e.msgs())
	posts := slackMsgPosts(e.f, e.channel)
	require.Len(t, posts, 1)
	assert.Contains(t, posts[0], "不是客服账号")
	assert.Contains(t, posts[0], plainID)
}

func TestSlackEvents_BangCommands(t *testing.T) {
	t.Run("!ai hands back", func(t *testing.T) {
		e := newSlackEventsEnv(t)
		require.NoError(t, setHandler(context.Background(), e.conv, HandlerHuman))
		e.f.Reset()
		e.send(e.msgEvent("!ai"))
		assert.Equal(t, HandlerAI, e.reload().Handler)
		var ev *ConversationMessage
		for _, m := range e.msgs() {
			if m.Kind == MsgEvent {
				ev = &m
			}
		}
		require.NotNil(t, ev)
		assert.Equal(t, "已交还 AI", ev.Content)
		assert.Equal(t, chatEventMeta(ChatEventHandedToAI), ev.Meta)
	})
	t.Run("!close closes and archives", func(t *testing.T) {
		e := newSlackEventsEnv(t)
		e.send(e.msgEvent("!close"))
		c := e.reload()
		assert.Equal(t, ConvClosed, c.Status)
		arch := e.f.CallsOf("conversations.archive")
		require.Len(t, arch, 1)
		assert.Equal(t, e.channel, arch[0].Str("channel"))
		var ev *ConversationMessage
		for _, m := range e.msgs() {
			if m.Kind == MsgEvent {
				ev = &m
			}
		}
		require.NotNil(t, ev)
		assert.Equal(t, "会话已关闭", ev.Content)
		assert.Equal(t, chatEventMeta(ChatEventClosed), ev.Meta)
	})
	t.Run("other ! becomes invisible note", func(t *testing.T) {
		e := newSlackEventsEnv(t)
		e.send(e.msgEvent("!随便写 跟进一下"))
		msgs := e.msgs()
		require.Len(t, msgs, 1)
		assert.Equal(t, MsgNote, msgs[0].Kind)
		assert.Equal(t, SenderStaff, msgs[0].SenderType)
		assert.Equal(t, "!随便写 跟进一下", msgs[0].Content)
		require.NotNil(t, msgs[0].SlackTS)
		visible, err := messagesAfter(context.Background(), e.conv.ID, 0, true)
		require.NoError(t, err)
		assert.Empty(t, visible)
		assert.Equal(t, HandlerAI, e.reload().Handler, "a note must not take over the conversation")
	})
}

func TestSlackEvents_CommandRetryIdempotent(t *testing.T) {
	e := newSlackEventsEnv(t)
	evt := e.msgEvent("!close")
	e.send(evt)
	e.send(evt)
	closes := 0
	for _, m := range e.msgs() {
		if m.Kind == MsgEvent && m.Meta == chatEventMeta(ChatEventClosed) {
			closes++
		}
	}
	assert.Equal(t, 1, closes)
	assert.Len(t, e.f.CallsOf("conversations.archive"), 1)
}

func TestSlackEvents_ClosedConversation(t *testing.T) {
	e := newSlackEventsEnv(t)
	require.NoError(t, closeConversation(context.Background(), e.conv))
	e.f.Reset()
	e.send(e.msgEvent("还有人吗"))
	assert.Empty(t, e.msgs())
	posts := slackMsgPosts(e.f, e.channel)
	require.Len(t, posts, 1)
	assert.Equal(t, "⚠️ 会话已关闭，这条没有发给访客。", posts[0])
}

func TestSlackEvents_FileShareWarns(t *testing.T) {
	e := newSlackEventsEnv(t)
	evt := e.msgEvent("", func(m map[string]any) { m["subtype"] = "file_share" })
	e.send(evt)
	e.send(evt) // 重试不重复提示
	assert.Empty(t, e.msgs())
	posts := slackMsgPosts(e.f, e.channel)
	require.Len(t, posts, 1)
	assert.Equal(t, "⚠️ 图片/文件暂不支持，这条没有发给访客。", posts[0])
}

func TestSlackEvents_NormalizesSlackMarkup(t *testing.T) {
	e := newSlackEventsEnv(t)
	e.send(e.msgEvent("见 <https://kaitu.io/x?a=1&amp;b=2|文档> 或 <https://y.io> 1 &lt; 2 &amp;&amp; 3 &gt; 2"))
	msgs := e.msgs()
	require.Len(t, msgs, 1)
	assert.Equal(t, "见 https://kaitu.io/x?a=1&b=2 或 https://y.io 1 < 2 && 3 > 2", msgs[0].Content)
}

func TestChatSlackNormalizeText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"<https://x.io|x.io>", "https://x.io"},
		{"<https://x.io>", "https://x.io"},
		{"<mailto:a@b.io|a@b.io>", "a@b.io"},
		{"<@U123> hi", " hi"},
		{"<#C1|support>", ""},
		{"<!here> now", " now"},
		{"<!subteam^S1|@oncall>", ""},
		{"a &amp; b &lt;c&gt;", "a & b <c>"},
		{"&lt;https://x.io&gt;", "<https://x.io>"}, // 实体还原后不再二次当作标记
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, chatSlackNormalizeText(c.in), c.in)
	}
}

func TestSlackEvents_EmptyAfterTrimIgnored(t *testing.T) {
	e := newSlackEventsEnv(t)
	e.send(e.msgEvent("   \n  "))
	assert.Empty(t, e.msgs())
}

func TestSlackEvents_StaffCacheHitSkipsSlackLookup(t *testing.T) {
	e := newSlackEventsEnv(t)
	e.send(e.msgEvent("第一条"))
	e.send(e.msgEvent("第二条"))
	assert.Len(t, e.msgs(), 2)
	assert.Len(t, e.f.CallsOf("users.info"), 1)
	// 缓存命中后显示名仍是邮箱前缀
	for _, m := range e.msgs() {
		assert.Equal(t, strings.Split(e.email, "@")[0], m.SenderName)
	}
	ttl, err := redis.Client().TTL(context.Background(), chatSlackStaffKeyPrefix+e.slackID).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, 50*time.Minute)
}

func TestSlackEvents_AdminAnyBrandAccepted(t *testing.T) {
	e := newSlackEventsEnvOpt(t, false)
	adminEmail := strings.ToLower(generateId("adm")) + "@example.com"
	adm := createBrandUserWithEmail(t, BrandOverleap, adminEmail)
	yes := true
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", adm.ID).Update("is_admin", &yes).Error)
	admID := "UAD" + generateId("")[10:]
	t.Cleanup(func() { redis.Client().Del(context.Background(), chatSlackStaffKeyPrefix+admID) })
	e.slackUser(admID, adminEmail, 1)
	e.send(e.msgEvent("管理员回复", func(m map[string]any) { m["user"] = admID }))
	msgs := e.msgs()
	require.Len(t, msgs, 1)
	assert.Equal(t, adm.ID, msgs[0].SenderID)
}

// changedEvent 造真实形状的 message_changed：user/text/ts 在嵌套的 message 里，旧文本在 previous_message。
// mod 作用在嵌套 message 上。
func (e *slackEventsEnv) changedEvent(msgTS, oldText, newText string, mod ...func(map[string]any)) map[string]any {
	e.seq++
	inner := map[string]any{"type": "message", "user": e.slackID, "text": newText, "ts": msgTS}
	for _, m := range mod {
		m(inner)
	}
	return map[string]any{"type": "message", "subtype": "message_changed", "channel": e.channel,
		"hidden": true, "ts": fmt.Sprintf("%d.%06d", time.Now().Unix(), 500+e.seq) + generateId("")[10:14],
		"message":          inner,
		"previous_message": map[string]any{"type": "message", "user": e.slackID, "text": oldText, "ts": msgTS}}
}

func (e *slackEventsEnv) deletedEvent(deletedTS string) map[string]any {
	e.seq++
	return map[string]any{"type": "message", "subtype": "message_deleted", "channel": e.channel, "deleted_ts": deletedTS,
		"ts":               fmt.Sprintf("%d.%06d", time.Now().Unix(), 700+e.seq) + generateId("")[10:14],
		"previous_message": map[string]any{"type": "message", "user": e.slackID, "text": "x", "ts": deletedTS}}
}

func TestSlackEvents_OversizeBody413(t *testing.T) {
	e := newSlackEventsEnv(t)
	// 声明长度超限：不读 body 直接 413（连签名都不验）
	big := []byte(strings.Repeat("x", chatSlackEventsMaxBody+1))
	assert.Equal(t, http.StatusRequestEntityTooLarge, slackPost(e.router, slackEventsTestSecret, time.Now(), big).Code)
}

type endlessReader struct{ n int64 }

func (r *endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.n += int64(len(p))
	return len(p), nil
}

// 未签名、长度未声明的大请求：handler 自己的上限保证读到 1MB 就停（裸 handler，不经日志中间件）。
func TestSlackEvents_UnsignedHugeBodyNotFullyRead(t *testing.T) {
	skipIfNoConfig(t)
	testInitConfig()
	old := viper.Get("slack.signing_secret")
	viper.Set("slack.signing_secret", slackEventsTestSecret)
	t.Cleanup(func() { viper.Set("slack.signing_secret", old) })
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/x", api_slack_events)
	body := &endlessReader{}
	req := httptest.NewRequest(http.MethodPost, "/x", body)
	req.ContentLength = -1
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.LessOrEqual(t, body.n, int64(chatSlackEventsMaxBody+64<<10), "read %d bytes", body.n)
}

func TestSlackEvents_RecordErrorWarnsOnce(t *testing.T) {
	e := newSlackEventsEnv(t)
	orig := chatSlackAppend
	t.Cleanup(func() { chatSlackAppend = orig })
	chatSlackAppend = func(context.Context, *Conversation, appendMessageInput) (*ConversationMessage, bool, error) {
		return nil, false, errors.New("boom")
	}
	evt := e.msgEvent("会落库失败")

	e.send(evt)
	e.send(evt) // 重投不重复提示
	assert.Empty(t, e.msgs())
	posts := slackMsgPosts(e.f, e.channel)
	require.Len(t, posts, 1)
	assert.Contains(t, posts[0], "这条没有发给访客，请重发")
	// 客服一开口 AI 就停了（先交接再落消息）
	assert.Equal(t, HandlerHuman, e.reload().Handler)
}

// 另一个会话（另一个频道）里有相同 ts 的消息：不影响本会话落库（Slack 的 ts 只在频道内唯一）。
func TestSlackEvents_SameTSInAnotherConversationStillDelivered(t *testing.T) {
	e := newSlackEventsEnv(t)
	other := slackConv(t, "", "/other")
	evt := e.msgEvent("两个频道撞了 ts")
	ts := evt["ts"].(string)
	slackSeed(t, other, SenderStaff, MsgText, "占位", func(m *ConversationMessage) { m.SlackTS = &ts })

	e.send(evt)
	msgs := e.msgs()
	require.Len(t, msgs, 1)
	assert.Equal(t, "两个频道撞了 ts", msgs[0].Content)
	assert.Empty(t, slackMsgPosts(e.f, e.channel), "不该有任何提示")
}

func TestSlackEvents_IgnoresOwnBotUser(t *testing.T) {
	e := newSlackEventsEnv(t)
	e.send(e.msgEvent("我是 bot 自己", func(m map[string]any) { m["user"] = fakeSlackBotID }))
	assert.Empty(t, e.msgs())
	assert.Empty(t, e.f.CallsOf("users.info"))
	assert.Empty(t, slackMsgPosts(e.f, e.channel))
}

func TestSlackEvents_NoVisibleEmailIsPermanent(t *testing.T) {
	e := newSlackEventsEnvOpt(t, false)
	uid := "UNE" + generateId("")[10:]
	t.Cleanup(func() { redis.Client().Del(context.Background(), chatSlackStaffKeyPrefix+uid) })
	e.f.Script("users.info", fakeSlackResp{Body: `{"ok":true,"user":{"id":"x","profile":{}}}`})
	e.send(e.msgEvent("没邮箱", func(m map[string]any) { m["user"] = uid }))
	e.send(e.msgEvent("还是没邮箱", func(m map[string]any) { m["user"] = uid }))
	assert.Empty(t, e.msgs())
	posts := slackMsgPosts(e.f, e.channel)
	require.Len(t, posts, 2)
	for _, p := range posts {
		assert.Contains(t, p, "没有可见邮箱")
		assert.NotContains(t, p, "稍后重发")
	}
	assert.Len(t, e.f.CallsOf("users.info"), 1, "negative result must be cached")
}

func TestSlackEvents_TransientLookupErrorNotCached(t *testing.T) {
	e := newSlackEventsEnvOpt(t, false)
	e.f.Script("users.info", fakeSlackHTTP(500))
	e.send(e.msgEvent("第一条"))
	assert.Empty(t, e.msgs())
	posts := slackMsgPosts(e.f, e.channel)
	require.Len(t, posts, 1)
	assert.Contains(t, posts[0], "暂时无法确认")
	n, err := redis.Client().Exists(context.Background(), chatSlackStaffKeyPrefix+e.slackID).Result()
	require.NoError(t, err)
	assert.Zero(t, n, "transient errors must not be cached")
	// 恢复后同一个人可以正常发
	e.slackUser(e.slackID, e.email, 1)
	e.send(e.msgEvent("第二条"))
	assert.Len(t, e.msgs(), 1)
}

func TestSlackEvents_WarningsDedupedByEventTS(t *testing.T) {
	e := newSlackEventsEnvOpt(t, false)
	plainEmail := strings.ToLower(generateId("plain")) + "@example.com"
	createBrandUserWithEmail(t, BrandKaitu, plainEmail)
	plainID := "UPL" + generateId("")[10:]
	t.Cleanup(func() { redis.Client().Del(context.Background(), chatSlackStaffKeyPrefix+plainID) })
	e.slackUser(plainID, plainEmail, 1)
	evt := e.msgEvent("非客服重投", func(m map[string]any) { m["user"] = plainID })
	e.send(evt)
	e.send(evt)
	assert.Len(t, slackMsgPosts(e.f, e.channel), 1)
}

// 客服连发两条，第一条的身份查询很慢（缓存未命中），第二条身份已缓存、瞬间可处理：
// 落库顺序仍须等于到达顺序。
func TestSlackEvents_SameChannelProcessedInOrder(t *testing.T) {
	e := newSlackEventsEnvOpt(t, false)
	old := chatAsync
	chatAsync = chatAsyncDefault // 真异步：同步桩下根本测不出顺序
	t.Cleanup(func() { chatAsync = old })
	e.slackUser(e.slackID, e.email, 1)
	fastID := "UFA" + generateId("")[10:]
	key := chatSlackStaffKeyPrefix + fastID
	require.NoError(t, redis.Client().Set(context.Background(), key, fmt.Sprintf("%d:fast", e.staff.ID), time.Minute).Err())
	t.Cleanup(func() { redis.Client().Del(context.Background(), key) })
	e.f.OnCall(func(c fakeSlackCall) {
		if c.Method == "users.info" {
			time.Sleep(700 * time.Millisecond)
		}
	})
	e.send(e.msgEvent("先发的"))
	e.send(e.msgEvent("后发的", func(m map[string]any) { m["user"] = fastID }))
	require.Eventually(t, func() bool { return len(e.msgs()) == 2 }, 20*time.Second, 50*time.Millisecond)
	msgs := e.msgs()
	assert.Equal(t, "先发的", msgs[0].Content)
	assert.Equal(t, "后发的", msgs[1].Content)
}

func TestSlackEvents_ConcurrentRedeliveryStoresOnce(t *testing.T) {
	e := newSlackEventsEnv(t)
	evt := e.msgEvent("并发重投")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); e.send(evt) }()
	}
	wg.Wait()
	assert.Len(t, e.msgs(), 1)
}

func TestSlackEvents_CommandEdgeCases(t *testing.T) {
	t.Run("!ai when already ai: hint only", func(t *testing.T) {
		e := newSlackEventsEnv(t)
		e.send(e.msgEvent("!ai"))
		assert.Empty(t, e.msgs())
		posts := slackMsgPosts(e.f, e.channel)
		require.Len(t, posts, 1)
		assert.Contains(t, posts[0], "已是 AI")
	})
	t.Run("!ai redelivery appends one event, no hint", func(t *testing.T) {
		e := newSlackEventsEnv(t)
		require.NoError(t, setHandler(context.Background(), e.conv, HandlerHuman))
		e.f.Reset()
		evt := e.msgEvent("!ai")
		e.send(evt)
		e.send(evt)
		events := 0
		for _, m := range e.msgs() {
			if m.Kind == MsgEvent {
				events++
			}
		}
		assert.Equal(t, 1, events)
		for _, p := range slackMsgPosts(e.f, e.channel) {
			assert.NotContains(t, p, "⚠️")
			assert.NotContains(t, p, "已是 AI")
		}
	})
	t.Run("commands on closed conversation: hint, no events", func(t *testing.T) {
		e := newSlackEventsEnv(t)
		require.NoError(t, closeConversation(context.Background(), e.conv))
		e.slackUser(e.slackID, e.email, 1)
		e.send(e.msgEvent("!close"))
		e.send(e.msgEvent("!ai"))
		assert.Empty(t, e.msgs())
		assert.Len(t, slackMsgPosts(e.f, e.channel), 2)
	})
	t.Run("close fails when event cannot be recorded, retry works", func(t *testing.T) {
		e := newSlackEventsEnv(t)
		oldFn := chatSlackAppendSystem
		chatSlackAppendSystem = func(ctx context.Context, c *Conversation, in appendMessageInput) (*ConversationMessage, bool, error) {
			return nil, false, errors.New("boom")
		}
		evt := e.msgEvent("!close")
		e.send(evt)
		chatSlackAppendSystem = oldFn
		assert.Equal(t, ConvOpen, e.reload().Status, "must not close without the closed event")
		assert.Empty(t, e.msgs(), "the idempotency marker must not block a retry")
		posts := slackMsgPosts(e.f, e.channel)
		require.Len(t, posts, 1)
		assert.Contains(t, posts[0], "命令执行失败")
		assert.Empty(t, e.f.CallsOf("conversations.archive"))
		// 同一事件重投（用缓存的客服身份）现在能成功
		e.send(evt)
		assert.Equal(t, ConvClosed, e.reload().Status)
	})
}

func TestSlackEvents_EditAndDeleteWarnOnce(t *testing.T) {
	e := newSlackEventsEnv(t)
	sent := e.msgEvent("原文")
	ts := sent["ts"].(string)
	e.send(sent)
	require.Len(t, e.msgs(), 1)

	e.send(e.changedEvent(ts, "原文", "原文")) // 链接展开：文本没变，静默
	assert.Empty(t, slackMsgPosts(e.f, e.channel))

	edit := e.changedEvent(ts, "原文", "改过的文")
	e.send(edit)
	e.send(edit) // 重投不重复
	posts := slackMsgPosts(e.f, e.channel)
	require.Len(t, posts, 1)
	assert.Contains(t, posts[0], "不会同步给访客")

	e.send(e.deletedEvent(ts))
	assert.Len(t, slackMsgPosts(e.f, e.channel), 2)
	// 访客侧内容不变
	assert.Equal(t, "原文", e.msgs()[0].Content)
	assert.Len(t, e.msgs(), 1)
}

func TestSlackEvents_ThreadBroadcastAndNestedEditsIgnored(t *testing.T) {
	e := newSlackEventsEnv(t)
	e.send(e.msgEvent("广播", func(m map[string]any) {
		m["subtype"] = "thread_broadcast"
		m["thread_ts"] = "1700000000.000001"
	}))
	assert.Empty(t, e.msgs())
}

func TestChatSlackNormalizeNoInternalIDs(t *testing.T) {
	for _, in := range []string{
		"<@U0123ABC> 你好", "你好 <@U0123ABC>", "<#C0123|secret-room> 看下", "<!channel> 注意", "<!here>", "<!everyone>",
		"<!subteam^S0123|@oncall-team> 来看", "<!subteam^S0123>",
	} {
		got := chatSlackNormalizeText(in)
		for _, leak := range []string{"U0123", "C0123", "secret-room", "oncall", "S0123", "channel", "here", "everyone", "@", "#"} {
			assert.NotContains(t, got, leak, in)
		}
	}
	assert.Equal(t, "你好", strings.TrimSpace(chatSlackNormalizeText("<@U0123ABC> 你好")))
	assert.Equal(t, "5月1日", chatSlackNormalizeText("<!date^1700000000^{date_short}|5月1日>"))
	assert.Equal(t, "https://x.io", chatSlackNormalizeText("<https://x.io|点这里>"))
}

// 经生产路由：路由不再挂请求日志中间件（它会在验签前整体读入 body），未签名的无限 body 读到上限即停。
func TestSlackEvents_UnsignedHugeBodyNotFullyReadThroughRouter(t *testing.T) {
	e := newSlackEventsEnv(t)
	body := &endlessReader{}
	req := httptest.NewRequest(http.MethodPost, "/webhook/slack/events", body)
	req.ContentLength = -1
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.LessOrEqual(t, body.n, int64(chatSlackEventsMaxBody+64<<10), "read %d bytes", body.n)
}

// ---- 终审修复：关闭竞态 ----

// 读到会话时还是 open、落库前已关闭：以库为准——不落库，频道里提示"会话已关闭"。
func TestSlackEvents_ClosedAfterLoadIsNotDelivered(t *testing.T) {
	e := newSlackEventsEnv(t)
	var fired atomic.Bool
	chatAppendPreLock = func(id uint64) {
		if id == e.conv.ID && fired.CompareAndSwap(false, true) {
			require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", id).
				Updates(map[string]any{"status": ConvClosed, "closed_at": time.Now()}).Error)
		}
	}
	t.Cleanup(func() { chatAppendPreLock = nil })

	evt := e.msgEvent("刚好撞上关闭")
	e.send(evt)
	e.send(evt) // 重投不重复提示
	require.True(t, fired.Load(), "对照：确实走到了追加")
	assert.Empty(t, e.msgs(), "已关闭的会话不得再落客服发言")
	posts := slackMsgPosts(e.f, e.channel)
	require.Len(t, posts, 1)
	assert.Equal(t, chatSlackWarnClosed, posts[0])
}
