package center

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/redis"
)

// 实时通道测试用 miniredis（testutil_test.go 已把 qtoolkit/redis 指向它，它支持 pub/sub），
// 两个"实例"是同进程里的两个 Broadcast。

const chatTestPubSubKey = "broadcast:" + chatBroadcastNamespace

// chatWSServer 起一个只挂 WS 路由的 httptest 服务；刻意不挂 BrandResolver，证明品牌只来自令牌。
func chatWSServer(t *testing.T, h gin.HandlerFunc) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/chat/ws", h)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func chatWaitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", msg)
}

// chatStartSingleton 启动单例广播并等订阅建立（订阅建立前发布会丢）。幂等。
func chatStartSingleton(t *testing.T) {
	t.Helper()
	skipIfNoConfig(t)
	startChatBroadcast(context.Background())
	chatWaitFor(t, func() bool { return testMiniRedis.PubSubNumSub(chatTestPubSubKey)[chatTestPubSubKey] >= 1 }, "broadcast subscribed")
}

func chatDialWS(t *testing.T, srv *httptest.Server, token string, hdr http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/chat/ws?token=" + token
	return websocket.DefaultDialer.Dial(u, hdr)
}

// chatWSClient 用单独的读协程把帧泵进 channel：gorilla 的读超时是粘性错误，
// 不能靠"读超时 = 没有帧"之后再继续读。
type chatWSClient struct {
	conn   *websocket.Conn
	frames chan chatWireFrame
}

func chatMustDial(t *testing.T, srv *httptest.Server, s chatSubject, hdr http.Header) *chatWSClient {
	t.Helper()
	conn, resp, err := chatDialWS(t, srv, signChatWSToken(s, time.Minute), hdr)
	require.NoError(t, err, "resp=%v", resp)
	c := &chatWSClient{conn: conn, frames: make(chan chatWireFrame, 16)}
	go func() {
		defer close(c.frames)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var f chatWireFrame
			if json.Unmarshal(data, &f) == nil {
				f.raw = data
				c.frames <- f
			}
		}
	}()
	t.Cleanup(func() { conn.Close() })
	return c
}

func (c *chatWSClient) Close() { c.conn.Close() }

type chatWireFrame struct {
	raw     []byte // 原始帧，用来断言某些内容根本没下发
	Channel string `json:"channel"`
	Payload struct {
		Type             string       `json:"type"`
		ConversationUUID string       `json:"conversationUuid"`
		Message          ChatMsgDTO   `json:"message"`
		Conversation     *ChatConvDTO `json:"conversation"`
	} `json:"payload"`
}

func chatReadFrame(t *testing.T, c *chatWSClient, within time.Duration) (chatWireFrame, bool) {
	t.Helper()
	select {
	case f, ok := <-c.frames:
		return f, ok
	case <-time.After(within):
		return chatWireFrame{}, false
	}
}

// chatRealGuestSubject 建一个真实 guest 作为主体：发布钩子要把 guest 解析到簇根，guest 行必须存在。
func chatRealGuestSubject(t *testing.T) chatSubject {
	t.Helper()
	v := chatTestValues(t, 1)
	id, err := resolveGuest(context.Background(), BrandKaitu, v[0], "", "zh-CN", "CN")
	require.NoError(t, err)
	t.Cleanup(func() {
		d := db.Get()
		var ids []uint64
		d.Model(&Conversation{}).Where("subject_kind = ? AND subject_id = ?", SubjectGuest, id).Pluck("id", &ids)
		if len(ids) > 0 {
			d.Where("conversation_id IN ?", ids).Delete(&ConversationMessage{})
			d.Where("id IN ?", ids).Delete(&Conversation{})
		}
	})
	return chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: id}
}

func chatWaitSubscribed(t *testing.T, bc *redis.Broadcast, s chatSubject, n int) {
	t.Helper()
	chatWaitFor(t, func() bool { return bc.SubscriberCount(s.Channel()) >= n }, "ws subscribed")
}

func TestChatWS_DeliversAcrossInstances(t *testing.T) {
	skipIfNoConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := testMiniRedis.PubSubNumSub(chatTestPubSubKey)[chatTestPubSubKey]
	a, b := newChatBroadcast(), newChatBroadcast()
	go a.RunContext(ctx)
	chatWaitFor(t, func() bool { return testMiniRedis.PubSubNumSub(chatTestPubSubKey)[chatTestPubSubKey] > base }, "A subscribed")

	srv := chatWSServer(t, chatWSHandler(func() *redis.Broadcast { return a }))
	s := chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: uint64(time.Now().UnixNano()%1e12) + 7}
	conn := chatMustDial(t, srv, s, nil)
	chatWaitSubscribed(t, a, s, 1)

	// 实例 B 没有 RunContext 也没有订阅者，只发布；A 经 Redis 收到
	require.NoError(t, b.Pub(ctx, s.Channel(), chatWirePayload{Type: "message", Message: &ChatMsgDTO{ID: 4242, Content: "hi"}}))
	f, ok := chatReadFrame(t, conn, 2*time.Second)
	require.True(t, ok, "no frame within 2s")
	assert.Equal(t, s.Channel(), f.Channel)
	assert.Equal(t, "message", f.Payload.Type)
	assert.Equal(t, uint64(4242), f.Payload.Message.ID)
}

func TestChatWS_RejectsBadToken(t *testing.T) {
	skipIfNoConfig(t)
	srv := chatWSServer(t, chatWSHandler(chatBroadcast))
	s := chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: 99}
	cases := map[string]string{
		"missing": "",
		"garbage": "abc.def",
		"expired": signChatWSToken(s, -time.Minute),
		"resume":  signChatResumeToken("some-conv-uuid", time.Minute),
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			conn, resp, err := chatDialWS(t, srv, tok, nil)
			if conn != nil {
				conn.Close()
			}
			require.Error(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})
	}
}

func TestChatWS_RejectsForeignOrigin(t *testing.T) {
	skipIfNoConfig(t)
	srv := chatWSServer(t, chatWSHandler(chatBroadcast))
	kaitu := chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: 98}
	cases := []struct {
		name   string
		s      chatSubject
		origin string
		want   int // 0 = 允许
	}{
		{"foreign", kaitu, "https://evil.example", http.StatusForbidden},
		{"other brand host", kaitu, "https://www.overleap.io", http.StatusForbidden},
		{"suffix trick", kaitu, "https://kaitu.io.evil.example", http.StatusForbidden},
		{"own host", kaitu, "https://www.kaitu.io", 0},
		{"own bare host", kaitu, "https://kaitu.io", 0},
		{"overleap own host", chatSubject{Brand: BrandOverleap, Kind: SubjectGuest, ID: 98}, "https://www.overleap.io", 0},
		{"localhost dev", kaitu, "http://localhost:3000", 0},
		{"no origin", kaitu, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hdr := http.Header{}
			if tc.origin != "" {
				hdr.Set("Origin", tc.origin)
			}
			conn, resp, err := chatDialWS(t, srv, signChatWSToken(tc.s, time.Minute), hdr)
			if tc.want == 0 {
				require.NoError(t, err)
				conn.Close()
				return
			}
			if conn != nil {
				conn.Close()
			}
			require.Error(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tc.want, resp.StatusCode)
		})
	}
}

func TestChatWS_BrandFromTokenNotHost(t *testing.T) {
	chatStartSingleton(t)
	srv := chatWSServer(t, chatWSHandler(chatBroadcast))
	id := uint64(time.Now().UnixNano()%1e12) + 11
	overleap := chatSubject{Brand: BrandOverleap, Kind: SubjectGuest, ID: id}
	kaituSame := chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: id}

	hdr := http.Header{}
	hdr.Set("Host", "www.kaitu.io") // 请求 Host 是 kaitu，令牌品牌是 overleap
	conn := chatMustDial(t, srv, overleap, hdr)
	chatWaitSubscribed(t, chatBroadcast(), overleap, 1)

	ctx := context.Background()
	payload := chatWirePayload{Type: "message", Message: &ChatMsgDTO{ID: 1}}
	require.NoError(t, chatBroadcast().Pub(ctx, kaituSame.Channel(), payload))
	_, ok := chatReadFrame(t, conn, 300*time.Millisecond)
	assert.False(t, ok, "kaitu subject with same id must not reach an overleap connection")

	payload.Message.ID = 2
	require.NoError(t, chatBroadcast().Pub(ctx, overleap.Channel(), payload))
	f, ok := chatReadFrame(t, conn, 2*time.Second)
	require.True(t, ok)
	assert.Equal(t, uint64(2), f.Payload.Message.ID)
}

func TestChatPublish_DeliversMessageAndSkipsNotes(t *testing.T) {
	chatStartSingleton(t)
	srv := chatWSServer(t, api_chat_ws)
	s := chatRealGuestSubject(t)
	conv, _, err := ensureConversation(context.Background(), s, "")
	require.NoError(t, err)
	conn := chatMustDial(t, srv, s, nil)
	chatWaitSubscribed(t, chatBroadcast(), s, 1)

	staff := appendMessageInput{SenderType: SenderStaff, SenderName: "agent", Kind: MsgText, Content: "hello visitor"}
	msg, _, err := appendMessage(context.Background(), conv, staff)
	require.NoError(t, err)
	f, ok := chatReadFrame(t, conn, 2*time.Second)
	require.True(t, ok)
	assert.Equal(t, msg.ID, f.Payload.Message.ID)
	assert.Equal(t, "hello visitor", f.Payload.Message.Content)
	assert.Equal(t, chatMessageDTO(msg).CreatedAt.UTC(), f.Payload.Message.CreatedAt.UTC())
	assert.Equal(t, "message", f.Payload.Type)
	assert.Equal(t, conv.UUID, f.Payload.ConversationUUID, "message 帧带会话 uuid，前端据此丢弃不属于当前会话的帧")
	assert.Equal(t, SenderStaff, f.Payload.Message.SenderType)
	assert.Empty(t, f.Payload.Message.SenderName, "客服真名不得下发给访客")
	assert.NotContains(t, string(f.raw), "agent")
}

func TestChatPublish_SkipsNotes(t *testing.T) {
	chatStartSingleton(t)
	srv := chatWSServer(t, api_chat_ws)
	s := chatRealGuestSubject(t)
	conv, _, err := ensureConversation(context.Background(), s, "")
	require.NoError(t, err)
	conn := chatMustDial(t, srv, s, nil)
	chatWaitSubscribed(t, chatBroadcast(), s, 1)

	_, _, err = appendMessage(context.Background(), conv, appendMessageInput{
		SenderType: SenderStaff, SenderName: "agent", Kind: MsgNote, Content: "internal only"})
	require.NoError(t, err)
	_, ok := chatReadFrame(t, conn, 500*time.Millisecond)
	assert.False(t, ok, "note must never be published")

	// 对照：同一连接上普通消息能收到，证明上面的静默不是连接坏了
	_, _, err = appendMessage(context.Background(), conv, appendMessageInput{
		SenderType: SenderStaff, SenderName: "agent", Kind: MsgText, Content: "visible"})
	require.NoError(t, err)
	f, ok := chatReadFrame(t, conn, 2*time.Second)
	require.True(t, ok)
	assert.Equal(t, "visible", f.Payload.Message.Content)
}

func TestChatPublish_FollowsGuestRootAfterMerge(t *testing.T) {
	chatStartSingleton(t)
	ctx := context.Background()
	v := chatTestValues(t, 2)
	a := newOrderedGuest(t, BrandKaitu, v[0], time.Hour) // 年轻：会被并入 b
	b := newOrderedGuest(t, BrandKaitu, v[1], 0)
	// 会话建在合并之前的 guest A 上
	conv, _, err := ensureConversation(ctx, chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: a}, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Get().Where("conversation_id = ?", conv.ID).Delete(&ConversationMessage{})
		db.Get().Where("id = ?", conv.ID).Delete(&Conversation{})
	})
	_, err = mergeGuests(ctx, a, b, MergeManual, nil, nil)
	require.NoError(t, err)
	root, err := guestRootID(ctx, a)
	require.NoError(t, err)
	require.Equal(t, b, root)

	got, err := chatSubjectOfConversation(ctx, conv)
	require.NoError(t, err)
	assert.Equal(t, chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: b}, got)

	srv := chatWSServer(t, api_chat_ws)
	conn := chatMustDial(t, srv, got, nil) // 访客令牌携带当前根
	chatWaitSubscribed(t, chatBroadcast(), got, 1)
	msg, _, err := appendMessage(ctx, conv, appendMessageInput{SenderType: SenderStaff, SenderName: "agent", Kind: MsgText, Content: "after merge"})
	require.NoError(t, err)
	f, ok := chatReadFrame(t, conn, 2*time.Second)
	require.True(t, ok, "message on a merged-away guest id must reach the root's socket")
	assert.Equal(t, msg.ID, f.Payload.Message.ID)
}

func TestChatSubjectOfConversation_User(t *testing.T) {
	skipIfNoConfig(t)
	conv := &Conversation{Brand: string(BrandOverleap), SubjectKind: SubjectUser, SubjectID: 77}
	got, err := chatSubjectOfConversation(context.Background(), conv)
	require.NoError(t, err)
	assert.Equal(t, chatSubject{Brand: BrandOverleap, Kind: SubjectUser, ID: 77}, got)
	_, err = chatSubjectOfConversation(context.Background(), &Conversation{Brand: "kaitu", SubjectKind: "x", SubjectID: 1})
	assert.Error(t, err)
}

func TestChatVisitorOnline(t *testing.T) {
	skipIfNoConfig(t)
	srv := chatWSServer(t, chatWSHandler(chatBroadcast))
	ctx := context.Background()
	s := chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: uint64(time.Now().UnixNano()%1e12) + 13}
	assert.False(t, chatVisitorOnline(ctx, s))

	c1 := chatMustDial(t, srv, s, nil)
	chatWaitFor(t, func() bool { return chatVisitorOnline(ctx, s) }, "online after connect")
	ttl := testMiniRedis.TTL(chatOnlineKey(s))
	assert.True(t, ttl > 0 && ttl <= chatOnlineTTL, "ttl=%v", ttl)

	// 第二个连接断开不应清掉标记（本实例仍有订阅者）
	c2 := chatMustDial(t, srv, s, nil)
	chatWaitSubscribed(t, chatBroadcast(), s, 2)
	c2.Close()
	chatWaitFor(t, func() bool { return chatBroadcast().SubscriberCount(s.Channel()) == 1 }, "c2 unsubscribed")
	time.Sleep(100 * time.Millisecond)
	assert.True(t, chatVisitorOnline(ctx, s), "marker must survive while another local connection remains")

	c1.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && chatVisitorOnline(ctx, s) {
		time.Sleep(10 * time.Millisecond)
	}
	assert.False(t, chatVisitorOnline(ctx, s), "marker must be gone within 1s of the last disconnect")
}

func TestChatWS_LimitsConnectionsPerChannel(t *testing.T) {
	skipIfNoConfig(t)
	srv := chatWSServer(t, chatWSHandler(chatBroadcast))
	s := chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: uint64(time.Now().UnixNano()%1e12) + 17}
	for i := 0; i < chatWSMaxPerChannel; i++ {
		chatMustDial(t, srv, s, nil)
		chatWaitSubscribed(t, chatBroadcast(), s, i+1)
	}
	conn, resp, err := chatDialWS(t, srv, signChatWSToken(s, time.Minute), nil)
	if conn != nil {
		conn.Close()
	}
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
}

func TestChatSession_WelcomeIsFixedCopy(t *testing.T) {
	r := chatSetup(t, true)
	w, data := chatOpenSession(t, r, "/pricing", nil)
	if ck := chatCookie(w, CookieChatCid); ck != nil {
		chatCleanupCID(t, ck.Value)
	}
	welcome, ok := data["welcome"].(map[string]any)
	require.True(t, ok)
	text, _ := chatAIWelcome(BrandKaitu)
	assert.Equal(t, text, welcome["text"])
	var values []string
	for _, o := range welcome["options"].([]any) {
		values = append(values, o.(map[string]any)["value"].(string))
	}
	assert.Equal(t, []string{"install", "purchase", "usage"}, values)
}

// 经生产路由（SetupRouter：请求日志 + 恢复 + BrandResolver + CORS 全链路）拨 /api/chat/ws。
// 其余测试把处理器挂在裸 gin 引擎上，删掉路由注册也不会红。
func TestChatWS_ThroughRealRouter(t *testing.T) {
	chatStartSingleton(t)
	srv := httptest.NewServer(SetupRouter())
	t.Cleanup(srv.Close)

	t.Run("bad token 401", func(t *testing.T) {
		conn, resp, err := chatDialWS(t, srv, "garbage", nil)
		if conn != nil {
			conn.Close()
		}
		require.Error(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("token brand wins over Host", func(t *testing.T) {
		s := chatSubject{Brand: BrandOverleap, Kind: SubjectGuest, ID: uint64(time.Now().UnixNano()%1e12) + 23}
		hdr := http.Header{}
		hdr.Set("Host", "www.kaitu.io")
		hdr.Set("Origin", "https://www.overleap.io")
		conn, resp, err := chatDialWS(t, srv, signChatWSToken(s, time.Minute), hdr)
		require.NoError(t, err, "resp=%v", resp)
		require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
		defer conn.Close()
		chatWaitSubscribed(t, chatBroadcast(), s, 1)

		require.NoError(t, chatBroadcast().Pub(context.Background(), s.Channel(),
			chatWirePayload{Type: "message", Message: &ChatMsgDTO{ID: 31337}}))
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := conn.ReadMessage()
		require.NoError(t, err)
		var f chatWireFrame
		require.NoError(t, json.Unmarshal(data, &f))
		assert.Equal(t, uint64(31337), f.Payload.Message.ID)
	})
}

// 续期：缩短 TTL/间隔，socket 保持连接时标记应活过原 TTL；若续期从不触发则在 FastForward 后消失。
func TestChatVisitorOnline_RefreshedWhileConnected(t *testing.T) {
	skipIfNoConfig(t)
	origTTL, origRefresh := chatOnlineTTL, chatOnlineRefresh
	chatOnlineTTL, chatOnlineRefresh = 2*time.Second, 50*time.Millisecond
	t.Cleanup(func() { chatOnlineTTL, chatOnlineRefresh = origTTL, origRefresh })

	srv := chatWSServer(t, chatWSHandler(chatBroadcast))
	ctx := context.Background()
	s := chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: uint64(time.Now().UnixNano()%1e12) + 29}
	chatMustDial(t, srv, s, nil)
	chatWaitFor(t, func() bool { return chatVisitorOnline(ctx, s) }, "online")

	for i := 0; i < 4; i++ {
		time.Sleep(150 * time.Millisecond) // 让续期协程至少跑过
		testMiniRedis.FastForward(1500 * time.Millisecond)
		// 累计已快进 >2s，没有续期的话早已过期
	}
	assert.True(t, chatVisitorOnline(ctx, s), "marker must be refreshed while the socket stays connected")
}

// ---- R29：会话状态抵达访客 ----

func chatMsgsGet(t *testing.T, r *gin.Engine, cid string) map[string]any {
	t.Helper()
	w := NewTestRequest("GET", "/api/chat/messages").WithCookie(CookieChatCid, cid).Execute(r)
	_, data := chatDecode(t, w)
	return data
}

func TestChatMessages_ReturnsConversationState(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/pricing", nil)
	ck := chatCookie(w, CookieChatCid)
	require.NotNil(t, ck)
	chatCleanupCID(t, ck.Value)

	// 没有会话 -> null
	data := chatMsgsGet(t, r, ck.Value)
	assert.Nil(t, data["conversation"])
	assert.Contains(t, data, "conversation")

	resp, _ := chatPostMessage(t, r, ck.Value, "c-1", "hello")
	require.Equal(t, 0, resp.Code, resp.Message)
	conv := chatMsgsGet(t, r, ck.Value)["conversation"].(map[string]any)
	assert.Equal(t, "open", conv["status"])
	assert.Equal(t, HandlerAI, conv["handler"])
	uuid := conv["uuid"].(string)

	var c Conversation
	require.NoError(t, db.Get().Where("uuid = ?", uuid).First(&c).Error)
	require.NoError(t, setHandler(context.Background(), &c, HandlerHuman))
	conv = chatMsgsGet(t, r, ck.Value)["conversation"].(map[string]any)
	assert.Equal(t, HandlerHuman, conv["handler"])

	require.NoError(t, closeConversation(context.Background(), &c))
	conv = chatMsgsGet(t, r, ck.Value)["conversation"].(map[string]any)
	assert.Equal(t, "closed", conv["status"])
	assert.Equal(t, uuid, conv["uuid"])
}

func chatStateTestConv(t *testing.T) (*Conversation, *chatWSClient) {
	t.Helper()
	chatStartSingleton(t)
	srv := chatWSServer(t, api_chat_ws)
	s := chatRealGuestSubject(t)
	conv, _, err := ensureConversation(context.Background(), s, "")
	require.NoError(t, err)
	conn := chatMustDial(t, srv, s, nil)
	chatWaitSubscribed(t, chatBroadcast(), s, 1)
	return conv, conn
}

func TestChatWS_PushesStateOnHandlerChange(t *testing.T) {
	conv, conn := chatStateTestConv(t)
	require.NoError(t, setHandler(context.Background(), conv, HandlerHuman))
	f, ok := chatReadFrame(t, conn, 2*time.Second)
	require.True(t, ok, "no state frame")
	assert.Equal(t, "state", f.Payload.Type)
	require.NotNil(t, f.Payload.Conversation)
	assert.Equal(t, conv.UUID, f.Payload.Conversation.UUID)
	assert.Equal(t, conv.UUID, f.Payload.ConversationUUID, "state 帧同样在顶层带会话 uuid")
	assert.Equal(t, HandlerHuman, f.Payload.Conversation.Handler)
	assert.Equal(t, "open", f.Payload.Conversation.Status)
}

func TestChatWS_PushesStateOnClose(t *testing.T) {
	conv, conn := chatStateTestConv(t)
	require.NoError(t, closeConversation(context.Background(), conv))
	f, ok := chatReadFrame(t, conn, 2*time.Second)
	require.True(t, ok, "no state frame")
	assert.Equal(t, "state", f.Payload.Type)
	require.NotNil(t, f.Payload.Conversation)
	assert.Equal(t, "closed", f.Payload.Conversation.Status)
}

// ---- R30：有效的 resume 令牌等同 preview ----

// chatConvOfBrand 建一个指定品牌的 guest 会话并清理，返回其 uuid。
func chatConvOfBrand(t *testing.T, brand Brand) string {
	t.Helper()
	v := chatTestValues(t, 1)
	id, err := resolveGuest(context.Background(), brand, v[0], "", "zh-CN", "CN")
	require.NoError(t, err)
	conv, _, err := ensureConversation(context.Background(), chatSubject{Brand: brand, Kind: SubjectGuest, ID: id}, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Get().Where("conversation_id = ?", conv.ID).Delete(&ConversationMessage{})
		db.Get().Where("id = ?", conv.ID).Delete(&Conversation{})
	})
	return conv.UUID
}

func TestChatSession_ValidResumeEnablesWhenDisabled(t *testing.T) {
	r := chatSetup(t, false)
	convUUID := chatConvOfBrand(t, BrandKaitu)
	tok := signChatResumeToken(convUUID, time.Hour)
	w := NewTestRequest("POST", "/api/chat/session").WithBody(map[string]any{"path": "/x", "resume": tok}).Execute(r)
	_, data := chatDecode(t, w)
	assert.Equal(t, true, data["enabled"])
	ck := chatCookie(w, CookieChatCid)
	require.NotNil(t, ck, "cookie must be planted")
	chatCleanupCID(t, ck.Value)
	owner, err := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, ck.Value)
	require.NoError(t, err)
	require.NotNil(t, owner)
	root, err := guestRootID(context.Background(), owner.GuestID)
	require.NoError(t, err)
	subj := chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: root}
	n, err := redis.Client().Exists(context.Background(), chatPreviewKey(subj)).Result()
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "preview marker must be stored")
	assert.NotNil(t, data["conversation"], "resume merge applied for the fresh guest")
}

func TestChatSession_InvalidResumeStaysDisabled(t *testing.T) {
	r := chatSetup(t, false)
	overleapConv := chatConvOfBrand(t, BrandOverleap)
	cases := map[string]string{
		"garbage":       "not-a-token",
		"expired":       signChatResumeToken(chatConvOfBrand(t, BrandKaitu), -time.Minute),
		"foreign brand": signChatResumeToken(overleapConv, time.Hour), // 请求品牌是 kaitu
		"unknown conv":  signChatResumeToken("no-such-conversation", time.Hour),
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			w := NewTestRequest("POST", "/api/chat/session").WithBody(map[string]any{"path": "/x", "resume": tok}).Execute(r)
			_, data := chatDecode(t, w)
			assert.Equal(t, false, data["enabled"])
			assert.Nil(t, chatCookie(w, CookieChatCid), "no cookie")
		})
	}
}

// ---- 收尾：WebSocket 握手过闸 / 预览标记跨刷新 ----

// 握手与其它访客接口同一道闸：开放放行；硬关、品牌不在白名单 403；仅预览要主体带预览标记。
func TestChatWS_HandshakeGate(t *testing.T) {
	skipIfNoConfig(t)
	chatStartSingleton(t)
	srv := chatWSServer(t, api_chat_ws)
	s := chatRealGuestSubject(t)
	tok := signChatWSToken(s, time.Minute)
	dial := func() int {
		conn, resp, err := chatDialWS(t, srv, tok, nil)
		if err == nil {
			conn.Close()
			return http.StatusSwitchingProtocols
		}
		require.NotNil(t, resp, "握手失败应有 HTTP 响应: %v", err)
		return resp.StatusCode
	}
	set := func(t *testing.T, enabled, preview bool, brands []string) {
		setChatViper(t, "chat.enabled", enabled)
		setChatViper(t, "chat.preview_enabled", preview)
		setChatViper(t, "chat.brands", brands)
	}
	all := chatAllBrandNames()

	t.Run("开放", func(t *testing.T) {
		set(t, true, false, all)
		assert.Equal(t, http.StatusSwitchingProtocols, dial())
	})
	t.Run("硬关", func(t *testing.T) {
		set(t, false, false, all)
		assert.Equal(t, http.StatusForbidden, dial())
	})
	t.Run("品牌不在白名单", func(t *testing.T) {
		set(t, true, true, []string{string(BrandOverleap)})
		assert.Equal(t, http.StatusForbidden, dial())
	})
	t.Run("仅预览", func(t *testing.T) {
		set(t, false, true, all)
		assert.Equal(t, http.StatusForbidden, dial(), "没有预览标记")
		ctx := context.Background()
		require.NoError(t, redis.Client().Set(ctx, chatPreviewKey(s), "1", time.Minute).Err())
		t.Cleanup(func() { redis.Client().Del(ctx, chatPreviewKey(s)) })
		assert.Equal(t, http.StatusSwitchingProtocols, dial(), "有预览标记")
	})
}

// 仅预览模式下，回链 / preview 进来过的访客刷新页面（session 不再带任何标记）仍然可用，标记被续期；
// 硬关时不行；从未有过标记的访客不行。
func TestChatSession_PreviewMarkerSurvivesReload(t *testing.T) {
	r := chatSetup(t, false) // enabled=false, preview_enabled=true
	ctx := context.Background()
	tok := signChatResumeToken(chatConvOfBrand(t, BrandKaitu), time.Hour)
	w := NewTestRequest("POST", "/api/chat/session").WithBody(map[string]any{"path": "/x", "resume": tok}).Execute(r)
	_, data := chatDecode(t, w)
	require.Equal(t, true, data["enabled"])
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	owner, err := findIdentityOwner(ctx, BrandKaitu, IdentityCID, cid)
	require.NoError(t, err)
	root, err := guestRootID(ctx, owner.GuestID)
	require.NoError(t, err)
	key := chatPreviewKey(chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: root})
	t.Cleanup(func() { redis.Client().Del(ctx, key) })
	plain := func(c string) map[string]any {
		q := NewTestRequest("POST", "/api/chat/session").WithBody(map[string]any{"path": "/x"})
		if c != "" {
			q = q.WithCookie(CookieChatCid, c)
		}
		_, d := chatDecode(t, q.Execute(r))
		return d
	}

	// 刷新：不带 preview / resume，标记快过期了 → 仍可用且被续期
	require.NoError(t, redis.Client().Expire(ctx, key, time.Minute).Err())
	assert.Equal(t, true, plain(cid)["enabled"])
	ttl, err := redis.Client().TTL(ctx, key).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Hour, "标记应被续期")

	// 从未有过标记的访客（有 cid、没标记；以及完全陌生的）仍关闭
	setChatViper(t, "chat.enabled", true)
	w2, _ := chatOpenSession(t, r, "/", nil)
	stranger := chatCookie(w2, CookieChatCid).Value
	chatCleanupCID(t, stranger)
	setChatViper(t, "chat.enabled", false)
	assert.Equal(t, false, plain(stranger)["enabled"])
	assert.Equal(t, false, plain("")["enabled"])

	// 硬关：有标记也不行
	setChatViper(t, "chat.preview_enabled", false)
	assert.Equal(t, false, plain(cid)["enabled"])

	// 标记没了：回到关闭
	setChatViper(t, "chat.preview_enabled", true)
	require.NoError(t, redis.Client().Del(ctx, key).Err())
	assert.Equal(t, false, plain(cid)["enabled"])
}
