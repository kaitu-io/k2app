package center

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

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
	other := slackConv(t, "", "/other") // 另一个没有频道的会话
	_ = other

	cases := map[string]map[string]any{
		"bot_id":          e.msgEvent("bot", func(m map[string]any) { m["bot_id"] = "B1" }),
		"message_changed": e.msgEvent("edit", func(m map[string]any) { m["subtype"] = "message_changed" }),
		"channel_join":    e.msgEvent("joined", func(m map[string]any) { m["subtype"] = "channel_join" }),
		"thread_reply":    e.msgEvent("内部讨论", func(m map[string]any) { m["thread_ts"] = "1700000000.000001" }),
		"lobby":           e.msgEvent("总览里的话", func(m map[string]any) { m["channel"] = fakeSlackLobbyID }),
		"unknown channel": e.msgEvent("陌生频道", func(m map[string]any) { m["channel"] = "CUNKNOWN" }),
		"not a message":   e.msgEvent("x", func(m map[string]any) { m["type"] = "reaction_added" }),
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
	redis.Client().Del(context.Background(), chatSlackFileWarnKeyPref+evt["ts"].(string))
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
		{"<@U123> hi", "@U123 hi"},
		{"<#C1|support>", "#support"},
		{"<!here> now", "@here now"},
		{"<!subteam^S1|@oncall>", "@oncall"},
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
