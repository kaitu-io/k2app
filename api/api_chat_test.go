package center

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/redis"
)

func chatRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.Use(BrandResolver())
	registerChatRoutes(api)
	return r
}

// chatSetup 重置限流并设置总开关，结束时还原。预览开关默认打开（enabled=false 的用例测的是"仅预览"），
// 要测硬关的用例自己用 setChatViper 把 chat.preview_enabled 置 false。
func chatSetup(t *testing.T, enabled bool) *gin.Engine {
	t.Helper()
	skipIfNoConfig(t)
	chatSessionLimiter.reset()
	chatMessageLimiter.reset()
	chatReadLimiter.reset()
	chatGuestCreateLimiter.reset()
	chatSendGlobalLimiter.reset()
	chatConvCreateLimiter.reset()
	t.Cleanup(chatConvCreateLimiter.reset)
	t.Cleanup(chatSessionLimiter.reset)
	t.Cleanup(chatMessageLimiter.reset)
	t.Cleanup(chatReadLimiter.reset)
	t.Cleanup(chatGuestCreateLimiter.reset)
	t.Cleanup(chatSendGlobalLimiter.reset)
	setChatViper(t, "chat.enabled", enabled)
	setChatViper(t, "chat.preview_enabled", true)
	return chatRouter()
}

type chatResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func chatDecode(t *testing.T, w *httptest.ResponseRecorder) (chatResp, map[string]any) {
	t.Helper()
	require.Equal(t, 200, w.Code, w.Body.String())
	var resp chatResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	var data map[string]any
	if len(resp.Data) > 0 && string(resp.Data) != "null" {
		_ = json.Unmarshal(resp.Data, &data)
	}
	return resp, data
}

func chatCookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// chatCleanupCID 清掉某个 cid 簇留下的全部数据。
func chatCleanupCID(t *testing.T, cid string) {
	t.Helper()
	t.Cleanup(func() {
		d := db.Get()
		var guestIDs []uint64
		d.Model(&GuestIdentity{}).Where("kind = ? AND value = ?", IdentityCID, cid).Pluck("guest_id", &guestIDs)
		if len(guestIDs) == 0 {
			return
		}
		var all []uint64
		d.Model(&Guest{}).Where("id IN ? OR merged_into_id IN ?", guestIDs, guestIDs).Pluck("id", &all)
		var convIDs []uint64
		d.Model(&Conversation{}).Where("subject_kind = ? AND subject_id IN ?", SubjectGuest, all).Pluck("id", &convIDs)
		if len(convIDs) > 0 {
			d.Where("conversation_id IN ?", convIDs).Delete(&ConversationMessage{})
			d.Where("id IN ?", convIDs).Delete(&Conversation{})
		}
		d.Where("from_guest_id IN ? OR into_guest_id IN ?", all, all).Delete(&GuestMerge{})
		d.Where("guest_id IN ?", all).Delete(&GuestIdentity{})
		d.Where("id IN ?", all).Delete(&Guest{})
	})
}

// chatLocaleMarker 返回本测试独有的 Accept-Language 值（≤16 字符，落到 guests.locale），
// 用来精确数"本测试触发建了几个 guest"，不受并行 agent 干扰。
func chatLocaleMarker() string {
	return fmt.Sprintf("x%015d", time.Now().UnixNano()%1_000_000_000_000_000)
}

func chatGuestsWithLocale(t *testing.T, marker string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Get().Model(&Guest{}).Where("locale = ?", marker).Count(&n).Error)
	return n
}

func chatOpenSession(t *testing.T, r *gin.Engine, path string, extra func(*TestRequest) *TestRequest) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := NewTestRequest("POST", "/api/chat/session").WithBody(map[string]any{"path": path})
	if extra != nil {
		req = extra(req)
	}
	w := req.Execute(r)
	_, data := chatDecode(t, w)
	return w, data
}

func chatPostMessage(t *testing.T, r *gin.Engine, cid, clientID, content string) (chatResp, map[string]any) {
	t.Helper()
	req := NewTestRequest("POST", "/api/chat/messages").
		WithBody(map[string]any{"kind": "text", "content": content, "clientId": clientID})
	if cid != "" {
		req = req.WithCookie(CookieChatCid, cid)
	}
	return chatDecode(t, req.Execute(r))
}

func chatConvCount(t *testing.T, cid string) int64 {
	t.Helper()
	owner, err := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, cid)
	require.NoError(t, err)
	if owner == nil {
		return 0
	}
	root, err := guestRootID(context.Background(), owner.GuestID)
	require.NoError(t, err)
	var n int64
	require.NoError(t, db.Get().Model(&Conversation{}).
		Where("subject_kind = ? AND subject_id = ?", SubjectGuest, root).Count(&n).Error)
	return n
}

func TestChatSession_DisabledByDefault(t *testing.T) {
	r := chatSetup(t, false)
	marker := chatLocaleMarker()
	withMarker := func(q *TestRequest) *TestRequest { return q.WithHeader("Accept-Language", marker) }
	w, data := chatOpenSession(t, r, "/pricing", withMarker)
	assert.Equal(t, false, data["enabled"])
	assert.Equal(t, int64(0), chatGuestsWithLocale(t, marker), "关闭状态不得建 guest")
	assert.Nil(t, data["conversation"])
	assert.Nil(t, data["ws"])
	assert.Nil(t, chatCookie(w, CookieChatCid), "关闭状态不得种 cid")

	w, data = chatOpenSession(t, r, "/pricing", func(q *TestRequest) *TestRequest {
		return withMarker(q.WithBody(map[string]any{"path": "/pricing", "preview": true}))
	})
	assert.Equal(t, true, data["enabled"])
	assert.Equal(t, int64(1), chatGuestsWithLocale(t, marker), "对照：预览会建 guest（marker 有效）")
	ck := chatCookie(w, CookieChatCid)
	require.NotNil(t, ck)
	chatCleanupCID(t, ck.Value)
	assert.True(t, ck.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, ck.SameSite)
	assert.Equal(t, "/", ck.Path)
	assert.Len(t, ck.Value, 22)
}

func TestChatSession_NoConversationUntilFirstMessage(t *testing.T) {
	r := chatSetup(t, true)
	w, data := chatOpenSession(t, r, "/pricing", nil)
	assert.Equal(t, true, data["enabled"])
	assert.Nil(t, data["conversation"])
	assert.Empty(t, data["messages"])
	assert.NotNil(t, data["welcome"])
	ck := chatCookie(w, CookieChatCid)
	require.NotNil(t, ck)
	chatCleanupCID(t, ck.Value)
	assert.Equal(t, int64(0), chatConvCount(t, ck.Value))

	resp, md := chatPostMessage(t, r, ck.Value, "c-1", "hello")
	require.Equal(t, 0, resp.Code, resp.Message)
	assert.NotNil(t, md["conversation"])
	owner, err := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, ck.Value)
	require.NoError(t, err)
	require.NotNil(t, owner)
	var convs []Conversation
	require.NoError(t, db.Get().Where("subject_kind = ? AND subject_id = ?", SubjectGuest, owner.GuestID).Find(&convs).Error)
	require.Len(t, convs, 1)
	assert.Equal(t, "/pricing", convs[0].EntryPath)
	assert.Equal(t, ConvOpen, convs[0].Status)

	// 再开一次 session：返回 open 会话与消息
	_, data = chatOpenSession(t, r, "/other", func(q *TestRequest) *TestRequest { return q.WithCookie(CookieChatCid, ck.Value) })
	conv, _ := data["conversation"].(map[string]any)
	require.NotNil(t, conv)
	assert.Equal(t, convs[0].UUID, conv["uuid"])
	msgs, _ := data["messages"].([]any)
	assert.NotEmpty(t, msgs)
}

func TestChatMessages_IdempotentClientID(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)

	r1, d1 := chatPostMessage(t, r, cid, "same-id", "hi")
	r2, d2 := chatPostMessage(t, r, cid, "same-id", "hi")
	require.Equal(t, 0, r1.Code, r1.Message)
	require.Equal(t, 0, r2.Code, r2.Message)
	m1, _ := d1["message"].(map[string]any)
	m2, _ := d2["message"].(map[string]any)
	require.NotNil(t, m1)
	assert.Equal(t, m1["id"], m2["id"])
	assert.Equal(t, "visitor", m1["senderType"])
	assert.Equal(t, "hi", m1["content"])
}

func TestChatMessages_RequiresSubject(t *testing.T) {
	r := chatSetup(t, true)
	for _, req := range []*TestRequest{
		NewTestRequest("GET", "/api/chat/messages"),
		NewTestRequest("GET", "/api/chat/ws-token"),
		NewTestRequest("POST", "/api/chat/email").WithBody(map[string]any{"email": "a@b.co"}),
		NewTestRequest("POST", "/api/chat/messages").WithBody(map[string]any{"kind": "text", "content": "x", "clientId": "c"}),
	} {
		w := req.Execute(r)
		resp, _ := chatDecode(t, w)
		assert.Equal(t, int(ErrorInvalidArgument), resp.Code, req.path)
		assert.Empty(t, w.Result().Cookies(), "无主体不得种 cookie: %s", req.path)
	}
	// 带着库里不存在的 cid 也不创建
	w := NewTestRequest("GET", "/api/chat/messages").WithCookie(CookieChatCid, "AAAAAAAAAAAAAAAAAAAAAA").Execute(r)
	resp, _ := chatDecode(t, w)
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	owner, err := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, "AAAAAAAAAAAAAAAAAAAAAA")
	require.NoError(t, err)
	assert.Nil(t, owner)
}

func TestChatSession_GPCIgnoresSid(t *testing.T) {
	r := chatSetup(t, true)
	sid := newFunnelSid()
	w, _ := chatOpenSession(t, r, "/", func(q *TestRequest) *TestRequest {
		return q.WithCookie("sid", sid).WithHeader("Sec-GPC", "1")
	})
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	owner, err := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, cid)
	require.NoError(t, err)
	require.NotNil(t, owner)
	var n int64
	require.NoError(t, db.Get().Model(&GuestIdentity{}).Where("guest_id = ? AND kind = ?", owner.GuestID, IdentitySID).Count(&n).Error)
	assert.Equal(t, int64(0), n)

	// 对照：无 GPC 时 sid 会挂上
	w2, _ := chatOpenSession(t, r, "/", func(q *TestRequest) *TestRequest { return q.WithCookie("sid", sid) })
	cid2 := chatCookie(w2, CookieChatCid).Value
	chatCleanupCID(t, cid2)
	t.Cleanup(func() { db.Get().Where("kind = ? AND value = ?", IdentitySID, sid).Delete(&GuestIdentity{}) })
	owner2, err := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, cid2)
	require.NoError(t, err)
	require.NoError(t, db.Get().Model(&GuestIdentity{}).Where("guest_id = ? AND kind = ?", owner2.GuestID, IdentitySID).Count(&n).Error)
	assert.Equal(t, int64(1), n)
}

func TestChat_CrossBrandIsolation(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	resp, _ := chatPostMessage(t, r, cid, "x-1", "kaitu secret")
	require.Equal(t, 0, resp.Code, resp.Message)

	// 同一 cid 在 overleap 下：读取拿不到、也不创建
	w = NewTestRequest("GET", "/api/chat/messages").WithCookie(CookieChatCid, cid).
		WithHeader("X-K2-Brand", string(BrandOverleap)).Execute(r)
	resp, data := chatDecode(t, w)
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Nil(t, data["messages"])
	assert.NotContains(t, w.Body.String(), "kaitu secret")

	// session 在 overleap 下得到的是另一个空会话，且看不到 kaitu 的消息
	w, data = chatOpenSession(t, r, "/", func(q *TestRequest) *TestRequest {
		return q.WithCookie(CookieChatCid, cid).WithHeader("X-K2-Brand", string(BrandOverleap))
	})
	assert.Nil(t, data["conversation"])
	assert.Empty(t, data["messages"])
	t.Cleanup(func() {
		var ids []uint64
		db.Get().Model(&GuestIdentity{}).Where("brand = ? AND value = ?", string(BrandOverleap), cid).Pluck("guest_id", &ids)
		db.Get().Where("guest_id IN ?", ids).Delete(&GuestIdentity{})
		db.Get().Where("id IN ?", ids).Delete(&Guest{})
	})
}

func TestChatEmail_AddsClaimedIdentity(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)

	var notified int
	chatAfterStateChange = append(chatAfterStateChange, func(*Conversation) { notified++ })
	t.Cleanup(func() { chatAfterStateChange = chatAfterStateChange[:len(chatAfterStateChange)-1] })

	// 无会话时：写入邮箱，不通知
	w = NewTestRequest("POST", "/api/chat/email").WithCookie(CookieChatCid, cid).
		WithBody(map[string]any{"email": " Visitor@Example.COM "}).Execute(r)
	resp, _ := chatDecode(t, w)
	require.Equal(t, 0, resp.Code, resp.Message)
	owner, err := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, cid)
	require.NoError(t, err)
	var ident GuestIdentity
	require.NoError(t, db.Get().Where("guest_id = ? AND kind = ?", owner.GuestID, IdentityEmail).First(&ident).Error)
	assert.Equal(t, "visitor@example.com", ident.Value)
	assert.Equal(t, StrengthClaimed, ident.Strength)
	assert.Equal(t, 0, notified)

	// 有 open 会话时：通知状态变化
	resp, _ = chatPostMessage(t, r, cid, "e-1", "hi")
	require.Equal(t, 0, resp.Code)
	w = NewTestRequest("POST", "/api/chat/email").WithCookie(CookieChatCid, cid).
		WithBody(map[string]any{"email": "visitor@example.com"}).Execute(r)
	resp, _ = chatDecode(t, w)
	require.Equal(t, 0, resp.Code)
	assert.GreaterOrEqual(t, notified, 1)

	// 非法邮箱
	w = NewTestRequest("POST", "/api/chat/email").WithCookie(CookieChatCid, cid).
		WithBody(map[string]any{"email": "not-an-email"}).Execute(r)
	resp, _ = chatDecode(t, w)
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
}

func TestChatMessages_RejectsWhenDisabledWithoutPreview(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	setChatViper(t, "chat.enabled", false)

	resp, _ := chatPostMessage(t, r, cid, "d-1", "hi")
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Equal(t, int64(0), chatConvCount(t, cid))

	// 预览会话是唯一入口
	_, data := chatOpenSession(t, r, "/p", func(q *TestRequest) *TestRequest {
		return q.WithCookie(CookieChatCid, cid).WithBody(map[string]any{"path": "/p", "preview": true})
	})
	assert.Equal(t, true, data["enabled"])
	resp, _ = chatPostMessage(t, r, cid, "d-2", "hi")
	assert.Equal(t, 0, resp.Code, resp.Message)
	// 开着的会话在开关关闭后仍可继续发
	resp, _ = chatPostMessage(t, r, cid, "d-3", "again")
	assert.Equal(t, 0, resp.Code, resp.Message)
}

func TestChatMessages_Validation(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)

	post := func(body map[string]any) chatResp {
		resp, _ := chatDecode(t, NewTestRequest("POST", "/api/chat/messages").WithCookie(CookieChatCid, cid).WithBody(body).Execute(r))
		return resp
	}
	invalid := int(ErrorInvalidArgument)
	assert.Equal(t, invalid, post(map[string]any{"kind": "image", "content": "x", "clientId": "a"}).Code)
	assert.Equal(t, invalid, post(map[string]any{"kind": "text", "content": "", "clientId": "a"}).Code)
	assert.Equal(t, invalid, post(map[string]any{"kind": "text", "content": "   ", "clientId": "a"}).Code)
	assert.Equal(t, invalid, post(map[string]any{"kind": "text", "content": strings.Repeat("字", 2001), "clientId": "a"}).Code)
	assert.Equal(t, invalid, post(map[string]any{"kind": "text", "content": "x"}).Code)
	assert.Equal(t, invalid, post(map[string]any{"kind": "text", "content": "x", "clientId": strings.Repeat("a", 37)}).Code)
	assert.Equal(t, int64(0), chatConvCount(t, cid), "校验失败不得建会话")
	// 边界：2000 字符、36 字符 clientId、option_reply 都通过
	assert.Equal(t, 0, post(map[string]any{"kind": "text", "content": strings.Repeat("字", 2000), "clientId": strings.Repeat("a", 36)}).Code)
	assert.Equal(t, 0, post(map[string]any{"kind": "option_reply", "content": "billing", "clientId": "o-1"}).Code)
}

func TestChatSession_ResumeMergesGuest(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	oldCid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, oldCid)
	resp, _ := chatPostMessage(t, r, oldCid, "r-1", "my old question")
	require.Equal(t, 0, resp.Code, resp.Message)
	var conv Conversation
	owner, _ := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, oldCid)
	require.NoError(t, db.Get().Where("subject_kind = ? AND subject_id = ?", SubjectGuest, owner.GuestID).First(&conv).Error)
	tok := signChatResumeToken(conv.UUID, time.Hour)
	require.NotEmpty(t, tok)

	// 另一台设备：全新 cid + resume 令牌
	w, data := chatOpenSession(t, r, "/", func(q *TestRequest) *TestRequest {
		return q.WithBody(map[string]any{"path": "/", "resume": tok})
	})
	newCid := chatCookie(w, CookieChatCid).Value
	require.NotEqual(t, oldCid, newCid)
	chatCleanupCID(t, newCid)
	t.Cleanup(func() { // newCid 的 guest 已并入，按 id 清它的 identity
		db.Get().Where("kind = ? AND value = ?", IdentityCID, newCid).Delete(&GuestIdentity{})
	})
	c, _ := data["conversation"].(map[string]any)
	require.NotNil(t, c)
	assert.Equal(t, conv.UUID, c["uuid"])
	assert.Contains(t, fmt.Sprint(data["messages"]), "my old question")

	var merges int64
	require.NoError(t, db.Get().Model(&GuestMerge{}).Where("reason = ?", MergeResumeLink).
		Where("from_guest_id IN (SELECT guest_id FROM guest_identities WHERE value = ?) OR into_guest_id IN (SELECT guest_id FROM guest_identities WHERE value = ?)", newCid, newCid).
		Count(&merges).Error)
	assert.Equal(t, int64(1), merges)

	// 新 cid 之后的请求（不带 resume）仍能读到旧会话，并在其上继续发言
	resp, _ = chatPostMessage(t, r, newCid, "r-2", "follow up")
	require.Equal(t, 0, resp.Code, resp.Message)
	var convCount int64
	root, err := guestRootID(context.Background(), owner.GuestID)
	require.NoError(t, err)
	ids2, err := guestClusterIDs(context.Background(), root)
	require.NoError(t, err)
	require.NoError(t, db.Get().Model(&Conversation{}).Where("subject_kind = ? AND subject_id IN ?", SubjectGuest, ids2).Count(&convCount).Error)
	assert.Equal(t, int64(1), convCount, "合并后继续发言不应另开会话")

	// 无效 resume 静默忽略
	w2, d2 := chatOpenSession(t, r, "/", func(q *TestRequest) *TestRequest {
		return q.WithBody(map[string]any{"path": "/", "resume": "bogus.token"})
	})
	ck := chatCookie(w2, CookieChatCid)
	require.NotNil(t, ck)
	chatCleanupCID(t, ck.Value)
	assert.Nil(t, d2["conversation"])
}

func TestChatSession_RateLimited(t *testing.T) {
	r := chatSetup(t, false)
	for i := 0; i < 30; i++ {
		resp, _ := chatDecode(t, NewTestRequest("POST", "/api/chat/session").WithBody(map[string]any{"path": "/"}).Execute(r))
		require.Equal(t, 0, resp.Code, "第 %d 次", i)
	}
	resp, _ := chatDecode(t, NewTestRequest("POST", "/api/chat/session").WithBody(map[string]any{"path": "/"}).Execute(r))
	assert.Equal(t, int(ErrorTooManyRequests), resp.Code)
}

func TestChat_LoggedInUserIsUserSubject(t *testing.T) {
	r := chatSetup(t, true)
	user := CreateTestUser(t)
	tok := GenerateTestToken(user.ID, "", time.Hour)
	w, _ := chatOpenSession(t, r, "/", func(q *TestRequest) *TestRequest { return q.WithCookie(CookieAccessToken, tok) })
	assert.Nil(t, chatCookie(w, CookieChatCid), "已登录用户不种 cid")

	resp, _ := chatDecode(t, NewTestRequest("POST", "/api/chat/messages").WithCookie(CookieAccessToken, tok).
		WithBody(map[string]any{"kind": "text", "content": "as user", "clientId": "u-1"}).Execute(r))
	require.Equal(t, 0, resp.Code, resp.Message)
	var conv Conversation
	require.NoError(t, db.Get().Where("subject_kind = ? AND subject_id = ?", SubjectUser, user.ID).First(&conv).Error)
	t.Cleanup(func() {
		db.Get().Where("conversation_id = ?", conv.ID).Delete(&ConversationMessage{})
		db.Get().Delete(&conv)
	})
	assert.Equal(t, "kaitu", conv.Brand)
}

func TestChatWSToken_Endpoint(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	resp, data := chatDecode(t, NewTestRequest("GET", "/api/chat/ws-token").WithCookie(CookieChatCid, cid).Execute(r))
	require.Equal(t, 0, resp.Code, resp.Message)
	tok, _ := data["token"].(string)
	got, err := parseChatWSToken(tok, time.Now())
	require.NoError(t, err)
	assert.Equal(t, BrandKaitu, got.Brand)
	assert.Equal(t, SubjectGuest, got.Kind)
}

// 会话挂在较晚创建（将被并入）的 guest 上：合并后必须经簇内 id 找回，而不是另开新会话。
func TestChatSession_ResumeFindsConversationOnAbsorbedGuest(t *testing.T) {
	r := chatSetup(t, true)
	wA, _ := chatOpenSession(t, r, "/", nil) // 设备 A：先到，合并后存活
	cidA := chatCookie(wA, CookieChatCid).Value
	chatCleanupCID(t, cidA)
	time.Sleep(1100 * time.Millisecond) // first_seen_at 精度为秒，保证 B 严格更晚
	wB, _ := chatOpenSession(t, r, "/", nil)
	cidB := chatCookie(wB, CookieChatCid).Value
	chatCleanupCID(t, cidB)
	resp, _ := chatPostMessage(t, r, cidB, "ab-1", "asked on device B")
	require.Equal(t, 0, resp.Code, resp.Message)
	ownerB, _ := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, cidB)
	var conv Conversation
	require.NoError(t, db.Get().Where("subject_kind = ? AND subject_id = ?", SubjectGuest, ownerB.GuestID).First(&conv).Error)

	_, data := chatOpenSession(t, r, "/", func(q *TestRequest) *TestRequest {
		return q.WithCookie(CookieChatCid, cidA).WithBody(map[string]any{"path": "/", "resume": signChatResumeToken(conv.UUID, time.Hour)})
	})
	c, _ := data["conversation"].(map[string]any)
	require.NotNil(t, c, "合并后应经簇找到挂在被并入 guest 上的会话")
	assert.Equal(t, conv.UUID, c["uuid"])

	// 之后 A 单独发言：续在同一会话里，不另开
	resp, _ = chatPostMessage(t, r, cidA, "ab-2", "now on device A")
	require.Equal(t, 0, resp.Code, resp.Message)
	var n int64
	ownerA, _ := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, cidA)
	root, err := guestRootID(context.Background(), ownerA.GuestID)
	require.NoError(t, err)
	ids, err := guestClusterIDs(context.Background(), root)
	require.NoError(t, err)
	require.NoError(t, db.Get().Model(&Conversation{}).Where("subject_kind = ? AND subject_id IN ?", SubjectGuest, ids).Count(&n).Error)
	assert.Equal(t, int64(1), n)
}

func chatGetMessages(t *testing.T, r *gin.Engine, cid string) (chatResp, string) {
	t.Helper()
	w := NewTestRequest("GET", "/api/chat/messages").WithCookie(CookieChatCid, cid).Execute(r)
	resp, _ := chatDecode(t, w)
	return resp, w.Body.String()
}

// 新 guest 持有 cid，另一个 guest 已有自己的会话：带着别人的 resume 令牌来也不合并。
func TestChatSession_ResumeIgnoredWhenCurrentGuestHasHistory(t *testing.T) {
	r := chatSetup(t, true)
	wA, _ := chatOpenSession(t, r, "/", nil)
	cidA := chatCookie(wA, CookieChatCid).Value
	chatCleanupCID(t, cidA)
	resp, _ := chatPostMessage(t, r, cidA, "ha-1", "victim secret AAA")
	require.Equal(t, 0, resp.Code, resp.Message)
	var convA Conversation
	ownerA, _ := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, cidA)
	require.NoError(t, db.Get().Where("subject_kind = ? AND subject_id = ?", SubjectGuest, ownerA.GuestID).First(&convA).Error)

	wB, _ := chatOpenSession(t, r, "/", nil)
	cidB := chatCookie(wB, CookieChatCid).Value
	chatCleanupCID(t, cidB)
	resp, _ = chatPostMessage(t, r, cidB, "hb-1", "attacker BBB")
	require.Equal(t, 0, resp.Code, resp.Message)

	_, data := chatOpenSession(t, r, "/", func(q *TestRequest) *TestRequest {
		return q.WithCookie(CookieChatCid, cidB).WithBody(map[string]any{"path": "/", "resume": signChatResumeToken(convA.UUID, time.Hour)})
	})
	assert.Contains(t, fmt.Sprint(data["messages"]), "attacker BBB")
	assert.NotContains(t, fmt.Sprint(data["messages"]), "victim secret AAA")
	ownerB, _ := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, cidB)
	var merges int64
	require.NoError(t, db.Get().Model(&GuestMerge{}).Where("from_guest_id IN ? OR into_guest_id IN ?",
		[]uint64{ownerA.GuestID, ownerB.GuestID}, []uint64{ownerA.GuestID, ownerB.GuestID}).Count(&merges).Error)
	assert.Equal(t, int64(0), merges)

	// A 的 cid 读不到 B 的消息
	_, body := chatGetMessages(t, r, cidA)
	assert.NotContains(t, body, "attacker BBB")
}

func TestChatSession_ResumeRespectsUndoneMerge(t *testing.T) {
	r := chatSetup(t, true)
	wA, _ := chatOpenSession(t, r, "/", nil)
	cidA := chatCookie(wA, CookieChatCid).Value
	chatCleanupCID(t, cidA)
	resp, _ := chatPostMessage(t, r, cidA, "u-1", "history of A")
	require.Equal(t, 0, resp.Code, resp.Message)
	ownerA, _ := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, cidA)
	var convA Conversation
	require.NoError(t, db.Get().Where("subject_kind = ? AND subject_id = ?", SubjectGuest, ownerA.GuestID).First(&convA).Error)
	tok := signChatResumeToken(convA.UUID, time.Hour)

	wB, _ := chatOpenSession(t, r, "/", nil)
	cidB := chatCookie(wB, CookieChatCid).Value
	chatCleanupCID(t, cidB)
	resume := func(q *TestRequest) *TestRequest {
		return q.WithCookie(CookieChatCid, cidB).WithBody(map[string]any{"path": "/", "resume": tok})
	}
	_, data := chatOpenSession(t, r, "/", resume)
	require.NotNil(t, data["conversation"], "全新 guest 首次使用令牌应合并")

	// 客服撤销该合并
	ownerB, _ := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, cidB)
	var m GuestMerge
	require.NoError(t, db.Get().Where("from_guest_id IN ? AND into_guest_id IN ?",
		[]uint64{ownerA.GuestID, ownerB.GuestID}, []uint64{ownerA.GuestID, ownerB.GuestID}).First(&m).Error)
	require.NoError(t, undoGuestMerge(context.Background(), m.ID, 1))

	_, data = chatOpenSession(t, r, "/", resume)
	assert.Nil(t, data["conversation"], "撤销过的合并不得被令牌重做")
	_, body := chatGetMessages(t, r, cidB)
	assert.NotContains(t, body, "history of A")
}

func TestChat_ClusterWithTwoOpenConversationsUsesNewest(t *testing.T) {
	r := chatSetup(t, true)
	ctx := context.Background()
	var cids [2]string
	var convs [2]*Conversation
	for i := range cids {
		w, _ := chatOpenSession(t, r, "/", nil)
		cids[i] = chatCookie(w, CookieChatCid).Value
		chatCleanupCID(t, cids[i])
		resp, _ := chatPostMessage(t, r, cids[i], fmt.Sprintf("two-%d", i), fmt.Sprintf("conv number %d", i))
		require.Equal(t, 0, resp.Code, resp.Message)
		o, _ := findIdentityOwner(ctx, BrandKaitu, IdentityCID, cids[i])
		c, err := openConversationFor(ctx, chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: o.GuestID})
		require.NoError(t, err)
		require.NotNil(t, c)
		convs[i] = c
		time.Sleep(1100 * time.Millisecond)
	}
	o0, _ := findIdentityOwner(ctx, BrandKaitu, IdentityCID, cids[0])
	o1, _ := findIdentityOwner(ctx, BrandKaitu, IdentityCID, cids[1])
	_, err := mergeGuests(ctx, o0.GuestID, o1.GuestID, MergeSameSID, nil, nil)
	require.NoError(t, err)

	for _, cid := range cids {
		_, data := chatOpenSession(t, r, "/", func(q *TestRequest) *TestRequest { return q.WithCookie(CookieChatCid, cid) })
		c, _ := data["conversation"].(map[string]any)
		require.NotNil(t, c)
		assert.Equal(t, convs[1].UUID, c["uuid"], "簇内多个 open 会话取最新")
	}
}

func TestChatMessages_PerSubjectSendLimit(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	owner, _ := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, cid)
	key := "chat:send:" + chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: owner.GuestID}.Channel()
	t.Cleanup(func() { redis.Client().Del(context.Background(), key) })
	for i := 0; i < chatSendPerSubjectPerMin; i++ {
		resp, _ := chatPostMessage(t, r, cid, fmt.Sprintf("lim-%d", i), "spam")
		require.Equal(t, 0, resp.Code, "第 %d 条", i)
	}
	resp, _ := chatPostMessage(t, r, cid, "lim-over", "spam")
	assert.Equal(t, int(ErrorTooManyRequests), resp.Code)
}

func TestChatEmail_CapPerCluster(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	post := func(e string) int {
		resp, _ := chatDecode(t, NewTestRequest("POST", "/api/chat/email").WithCookie(CookieChatCid, cid).
			WithBody(map[string]any{"email": e}).Execute(r))
		return resp.Code
	}
	for i := 0; i < chatEmailMaxPerCluster; i++ {
		require.Equal(t, 0, post(fmt.Sprintf("v%d@example.com", i)))
	}
	assert.Equal(t, int(ErrorInvalidArgument), post("v99@example.com"), "第 4 个不同邮箱被拒")
	assert.Equal(t, 0, post("V1@example.com"), "重复已有邮箱幂等成功")
}

func TestChatSession_GlobalGuestCreateCap(t *testing.T) {
	r := chatSetup(t, true)
	oldLimit := chatGuestCreateLimiter.limit
	chatGuestCreateLimiter.limit = 1
	t.Cleanup(func() { chatGuestCreateLimiter.limit = oldLimit })

	marker := chatLocaleMarker()
	hdr := func(q *TestRequest) *TestRequest { return q.WithHeader("Accept-Language", marker) }
	w, _ := chatOpenSession(t, r, "/", hdr)
	ck := chatCookie(w, CookieChatCid)
	require.NotNil(t, ck)
	chatCleanupCID(t, ck.Value)

	// 已存在的 cid 不受影响（不新建）
	_, data := chatOpenSession(t, r, "/", func(q *TestRequest) *TestRequest { return hdr(q).WithCookie(CookieChatCid, ck.Value) })
	assert.Equal(t, true, data["enabled"])

	// 第二个新访客：429，无 cookie，无新行
	w2 := NewTestRequest("POST", "/api/chat/session").WithBody(map[string]any{"path": "/"}).WithHeader("Accept-Language", marker).Execute(r)
	resp, _ := chatDecode(t, w2)
	assert.Equal(t, int(ErrorTooManyRequests), resp.Code)
	assert.Nil(t, chatCookie(w2, CookieChatCid))
	assert.Equal(t, int64(1), chatGuestsWithLocale(t, marker))
}

func TestChatMessages_GlobalSendCap(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	oldLimit := chatSendGlobalLimiter.limit
	chatSendGlobalLimiter.limit = 1
	t.Cleanup(func() { chatSendGlobalLimiter.limit = oldLimit })
	resp, _ := chatPostMessage(t, r, cid, "g-1", "one")
	assert.Equal(t, 0, resp.Code, resp.Message)
	resp, _ = chatPostMessage(t, r, cid, "g-2", "two")
	assert.Equal(t, int(ErrorTooManyRequests), resp.Code)
}

func TestChatReads_PerIPLimit(t *testing.T) {
	r := chatSetup(t, true)
	for i := 0; i < chatReadPerIPPerMin; i++ {
		NewTestRequest("GET", "/api/chat/ws-token").Execute(r)
	}
	for _, path := range []string{"/api/chat/messages", "/api/chat/ws-token"} {
		resp, _ := chatDecode(t, NewTestRequest("GET", path).Execute(r))
		assert.Equal(t, int(ErrorTooManyRequests), resp.Code, path)
	}
}

func TestChatPost_BodyLimitAndMalformedSession(t *testing.T) {
	r := chatSetup(t, true)
	marker := chatLocaleMarker()
	// 格式错误的 JSON：session 在解析主体之前就拒绝
	req, err := http.NewRequest("POST", "/api/chat/session", strings.NewReader("{not json"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", marker)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp, _ := chatDecode(t, w)
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Nil(t, chatCookie(w, CookieChatCid))
	assert.Equal(t, int64(0), chatGuestsWithLocale(t, marker))

	// 空请求体合法
	req, _ = http.NewRequest("POST", "/api/chat/session", http.NoBody)
	req.Header.Set("Accept-Language", marker)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp, _ = chatDecode(t, w)
	assert.Equal(t, 0, resp.Code)
	if ck := chatCookie(w, CookieChatCid); ck != nil {
		chatCleanupCID(t, ck.Value)
	}

	// 超 16KB 的请求体被拒
	w = NewTestRequest("POST", "/api/chat/messages").WithCookie(CookieChatCid, "AAAAAAAAAAAAAAAAAAAAAA").
		WithBody(map[string]any{"kind": "text", "content": "x", "clientId": "c", "pad": strings.Repeat("p", 20<<10)}).Execute(r)
	resp, _ = chatDecode(t, w)
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
}

func TestChatMessages_FallsBackToClosedAndHidesNotes(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	resp, _ := chatPostMessage(t, r, cid, "cl-1", "before close")
	require.Equal(t, 0, resp.Code, resp.Message)
	ctx := context.Background()
	owner, _ := findIdentityOwner(ctx, BrandKaitu, IdentityCID, cid)
	conv, err := openConversationFor(ctx, chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: owner.GuestID})
	require.NoError(t, err)
	require.NotNil(t, conv)
	_, _, err = appendMessage(ctx, conv, appendMessageInput{SenderType: SenderStaff, SenderName: "staff", Kind: MsgNote, Content: "internal note ZZZ"})
	require.NoError(t, err)
	require.NoError(t, closeConversation(ctx, conv))

	_, body := chatGetMessages(t, r, cid)
	assert.Contains(t, body, "before close", "无 open 会话时回落到最近关闭的那条")
	assert.NotContains(t, body, "internal note ZZZ", "内部备注不外露")
}

// ---- 终审修复：总开关硬关 / 仅预览 / 品牌白名单 ----

// chatVisitorCalls 对 session 以外的四个访客接口各打一次，返回 接口 → (code, message)。
func chatVisitorCalls(t *testing.T, r *gin.Engine, cid, tag string, hdr map[string]string) map[string]chatResp {
	t.Helper()
	with := func(q *TestRequest) *TestRequest {
		q = q.WithCookie(CookieChatCid, cid)
		for k, v := range hdr {
			q = q.WithHeader(k, v)
		}
		return q
	}
	out := map[string]chatResp{}
	for name, req := range map[string]*TestRequest{
		"messages": NewTestRequest("GET", "/api/chat/messages"),
		"ws-token": NewTestRequest("GET", "/api/chat/ws-token"),
		"email":    NewTestRequest("POST", "/api/chat/email").WithBody(map[string]any{"email": tag + "@example.com"}),
		"send":     NewTestRequest("POST", "/api/chat/messages").WithBody(map[string]any{"kind": "text", "content": "gate " + tag, "clientId": tag}),
	} {
		resp, _ := chatDecode(t, with(req).Execute(r))
		out[name] = resp
	}
	return out
}

func chatAssertAllRejected(t *testing.T, calls map[string]chatResp, why string) {
	t.Helper()
	require.Len(t, calls, 4)
	for name, resp := range calls {
		assert.Equal(t, int(ErrorInvalidArgument), resp.Code, "%s: %s", why, name)
		assert.Equal(t, "chat is not available", resp.Message, "%s: %s", why, name)
	}
}

func chatAssertAllOK(t *testing.T, calls map[string]chatResp, why string) {
	t.Helper()
	require.Len(t, calls, 4)
	for name, resp := range calls {
		assert.Equal(t, 0, resp.Code, "%s: %s (%s)", why, name, resp.Message)
	}
}

func chatMsgCountByCID(t *testing.T, brand Brand, cid string) int64 {
	t.Helper()
	owner, err := findIdentityOwner(context.Background(), brand, IdentityCID, cid)
	require.NoError(t, err)
	require.NotNil(t, owner)
	var n int64
	require.NoError(t, db.Get().Model(&ConversationMessage{}).
		Where("conversation_id IN (SELECT id FROM conversations WHERE subject_kind = ? AND subject_id = ?)", SubjectGuest, owner.GuestID).
		Count(&n).Error)
	return n
}

// 两个开关都关 = 硬关：请求体里的 preview、有效的 resume 令牌、已有 cookie 与 open 会话都进不来。
func TestChatGate_HardOffRejectsEverything(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	resp, md := chatPostMessage(t, r, cid, "off-0", "before the switch")
	require.Equal(t, 0, resp.Code, resp.Message)
	convUUID := md["conversation"].(map[string]any)["uuid"].(string)
	// 对照：开着的时候四个接口都通
	chatAssertAllOK(t, chatVisitorCalls(t, r, cid, "on", nil), "开关打开")
	before := chatMsgCountByCID(t, BrandKaitu, cid)

	setChatViper(t, "chat.enabled", false)
	setChatViper(t, "chat.preview_enabled", false)

	marker := chatLocaleMarker()
	for name, body := range map[string]map[string]any{
		"plain":   {"path": "/"},
		"preview": {"path": "/", "preview": true},
		"resume":  {"path": "/", "resume": signChatResumeToken(convUUID, time.Hour)},
	} {
		w := NewTestRequest("POST", "/api/chat/session").WithBody(body).WithHeader("Accept-Language", marker).Execute(r)
		_, data := chatDecode(t, w)
		assert.Equal(t, false, data["enabled"], name)
		assert.Nil(t, chatCookie(w, CookieChatCid), "硬关不得种 cid: %s", name)
	}
	assert.Equal(t, int64(0), chatGuestsWithLocale(t, marker), "硬关不得建 guest")
	// 已有 cookie + open 会话的访客：session 同样关闭，且不泄露会话
	_, data := chatOpenSession(t, r, "/", func(q *TestRequest) *TestRequest {
		return q.WithCookie(CookieChatCid, cid).WithBody(map[string]any{"path": "/", "preview": true})
	})
	assert.Equal(t, false, data["enabled"])
	assert.Nil(t, data["conversation"])

	chatAssertAllRejected(t, chatVisitorCalls(t, r, cid, "off", nil), "硬关")
	assert.Equal(t, before, chatMsgCountByCID(t, BrandKaitu, cid), "硬关期间不得落消息")
}

// enabled=false 且 preview_enabled=true：只有走过 preview（或有效 resume）session 的访客能用其余接口。
func TestChatGate_PreviewOnlyNeedsMarker(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	resp, _ := chatPostMessage(t, r, cid, "pv-0", "opened while enabled")
	require.Equal(t, 0, resp.Code, resp.Message)

	setChatViper(t, "chat.enabled", false) // preview_enabled 由 chatSetup 置 true
	chatAssertAllRejected(t, chatVisitorCalls(t, r, cid, "nomark", nil), "仅预览、无标记")

	_, data := chatOpenSession(t, r, "/", func(q *TestRequest) *TestRequest {
		return q.WithCookie(CookieChatCid, cid).WithBody(map[string]any{"path": "/", "preview": true})
	})
	require.Equal(t, true, data["enabled"])
	chatAssertAllOK(t, chatVisitorCalls(t, r, cid, "marked", nil), "仅预览、有标记")
}

// 品牌不在 chat.brands：session 返回 enabled:false，其余访客接口拒绝；名单内的品牌不受影响。
func TestChatGate_BrandAllowlist(t *testing.T) {
	r := chatSetup(t, true)
	other := map[string]string{"X-K2-Brand": string(BrandOverleap)}
	asOther := func(q *TestRequest) *TestRequest { return q.WithHeader("X-K2-Brand", string(BrandOverleap)) }

	// 两个品牌都开放时各开一个会话
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	w, data := chatOpenSession(t, r, "/", asOther)
	require.Equal(t, true, data["enabled"], "对照：名单内时该品牌可用")
	otherCid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, otherCid)
	chatAssertAllOK(t, chatVisitorCalls(t, r, otherCid, "in", other), "名单内")
	before := chatMsgCountByCID(t, BrandOverleap, otherCid)

	setChatViper(t, "chat.brands", []string{string(BrandKaitu)})

	marker := chatLocaleMarker()
	w = NewTestRequest("POST", "/api/chat/session").WithBody(map[string]any{"path": "/", "preview": true}).
		WithHeader("X-K2-Brand", string(BrandOverleap)).WithHeader("Accept-Language", marker).Execute(r)
	_, data = chatDecode(t, w)
	assert.Equal(t, false, data["enabled"])
	assert.Nil(t, chatCookie(w, CookieChatCid))
	assert.Equal(t, int64(0), chatGuestsWithLocale(t, marker))
	chatAssertAllRejected(t, chatVisitorCalls(t, r, otherCid, "out", other), "名单外")
	assert.Equal(t, before, chatMsgCountByCID(t, BrandOverleap, otherCid))

	// 名单内的品牌照常
	chatAssertAllOK(t, chatVisitorCalls(t, r, cid, "kept", nil), "名单内的品牌")

	// 空名单 = 全部关闭
	setChatViper(t, "chat.brands", []string{})
	_, data = chatOpenSession(t, r, "/", func(q *TestRequest) *TestRequest { return q.WithCookie(CookieChatCid, cid) })
	assert.Equal(t, false, data["enabled"])
	chatAssertAllRejected(t, chatVisitorCalls(t, r, cid, "empty", nil), "空名单")
}

// ---- 终审修复：滥用面 ----

// 全局发言额度只在请求通过校验、主体解析与按主体限速之后才扣：
// 否则任何人用无效请求就能把所有访客的发言额度耗光。
func TestChatMessages_GlobalCapNotSpentByRejectedRequests(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	oldLimit := chatSendGlobalLimiter.limit
	chatSendGlobalLimiter.limit = 1
	t.Cleanup(func() { chatSendGlobalLimiter.limit = oldLimit })

	// 校验失败、没有主体：都不该动全局额度
	resp, _ := chatDecode(t, NewTestRequest("POST", "/api/chat/messages").WithCookie(CookieChatCid, cid).
		WithBody(map[string]any{"kind": "text", "content": "", "clientId": "bad"}).Execute(r))
	require.Equal(t, int(ErrorInvalidArgument), resp.Code)
	resp, _ = chatPostMessage(t, r, "AAAAAAAAAAAAAAAAAAAAAA", "nosubj", "x")
	require.Equal(t, int(ErrorInvalidArgument), resp.Code)

	resp, _ = chatPostMessage(t, r, cid, "g-ok", "the one allowed")
	assert.Equal(t, 0, resp.Code, "被拒的请求不得耗掉全局额度: %s", resp.Message)
	resp, _ = chatPostMessage(t, r, cid, "g-over", "over")
	assert.Equal(t, int(ErrorTooManyRequests), resp.Code, "对照：额度确实只有 1")
}

// 按主体限速触顶的请求同样不扣全局额度。
func TestChatMessages_GlobalCapNotSpentBySubjectLimited(t *testing.T) {
	r := chatSetup(t, true)
	var cids [2]string
	for i := range cids {
		w, _ := chatOpenSession(t, r, "/", nil)
		cids[i] = chatCookie(w, CookieChatCid).Value
		chatCleanupCID(t, cids[i])
	}
	owner, _ := findIdentityOwner(context.Background(), BrandKaitu, IdentityCID, cids[0])
	key := "chat:send:" + chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: owner.GuestID}.Channel()
	require.NoError(t, redis.Client().Set(context.Background(), key, chatSendPerSubjectPerMin, time.Minute).Err())
	t.Cleanup(func() { redis.Client().Del(context.Background(), key) })
	oldLimit := chatSendGlobalLimiter.limit
	chatSendGlobalLimiter.limit = 1
	t.Cleanup(func() { chatSendGlobalLimiter.limit = oldLimit })

	resp, _ := chatPostMessage(t, r, cids[0], "sl-1", "spammer")
	require.Equal(t, int(ErrorTooManyRequests), resp.Code)
	resp, _ = chatPostMessage(t, r, cids[1], "sl-2", "innocent")
	assert.Equal(t, 0, resp.Code, "别人被限速不得耗掉全局额度: %s", resp.Message)
}

// 计数键没有过期时间（INCR 成功而 EXPIRE 失败留下的）：下一次调用必须补上，否则该主体永久限流。
func TestChatSubjectSendAllow_HealsKeyWithoutTTL(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	s := chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: uint64(time.Now().UnixNano())}
	key := "chat:send:" + s.Channel()
	t.Cleanup(func() { redis.Client().Del(ctx, key) })

	require.NoError(t, redis.Client().Set(ctx, key, chatSendPerSubjectPerMin+5, 0).Err())
	assert.False(t, chatSubjectSendAllow(ctx, s), "计数已超限")
	ttl, err := redis.Client().TTL(ctx, key).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Duration(0), "没有过期时间的键必须被补上过期")
	assert.LessOrEqual(t, ttl, time.Minute)

	// 正常路径：首次计数即带过期
	require.NoError(t, redis.Client().Del(ctx, key).Err())
	assert.True(t, chatSubjectSendAllow(ctx, s))
	ttl, err = redis.Client().TTL(ctx, key).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Duration(0))
	// 已有过期时间的键不被续期（固定窗口）
	require.NoError(t, redis.Client().Expire(ctx, key, 10*time.Second).Err())
	assert.True(t, chatSubjectSendAllow(ctx, s))
	ttl, _ = redis.Client().TTL(ctx, key).Result()
	assert.LessOrEqual(t, ttl, 10*time.Second)
}

// 每实例新建会话上限：超限 429 且不建会话；已有 open 会话的访客照常发言。
func TestChatMessages_NewConversationCap(t *testing.T) {
	r := chatSetup(t, true)
	var cids [2]string
	for i := range cids {
		w, _ := chatOpenSession(t, r, "/", nil)
		cids[i] = chatCookie(w, CookieChatCid).Value
		chatCleanupCID(t, cids[i])
	}
	oldLimit := chatConvCreateLimiter.limit
	chatConvCreateLimiter.limit = 1
	t.Cleanup(func() { chatConvCreateLimiter.limit = oldLimit })

	resp, _ := chatPostMessage(t, r, cids[0], "nc-1", "first conversation")
	require.Equal(t, 0, resp.Code, resp.Message)
	resp, _ = chatPostMessage(t, r, cids[1], "nc-2", "second conversation")
	assert.Equal(t, int(ErrorTooManyRequests), resp.Code)
	assert.Equal(t, int64(0), chatConvCount(t, cids[1]), "超限不得建会话")

	resp, _ = chatPostMessage(t, r, cids[0], "nc-3", "still talking")
	assert.Equal(t, 0, resp.Code, "已有 open 会话的访客不受新建上限影响: %s", resp.Message)
}

// ---- 终审修复：关闭竞态 ----

// chatCloseOnAppend 让目标会话在下一次 appendMessage 开事务之前被人工关闭一次
// （模拟"读到 open 会话"与"追加"之间插入的关闭）。
func chatCloseOnAppend(t *testing.T, convID uint64) {
	t.Helper()
	var fired atomic.Bool
	chatAppendPreLock = func(id uint64) {
		if id != convID || !fired.CompareAndSwap(false, true) {
			return
		}
		conv := slackReload(t, convID)
		_, err := chatCloseByStaff(context.Background(), conv)
		require.NoError(t, err)
	}
	t.Cleanup(func() { chatAppendPreLock = nil })
}

func chatConvByUUID(t *testing.T, uuid string) *Conversation {
	t.Helper()
	var conv Conversation
	require.NoError(t, db.Get().Where("uuid = ?", uuid).First(&conv).Error)
	return &conv
}

// 访客发送撞上关闭：对访客透明地开新会话，消息进新会话，原会话的 closed 事件之后没有任何东西。
func TestChatSend_ClosedBetweenLookupAndAppendOpensNewConversation(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/pricing", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	resp, md := chatPostMessage(t, r, cid, "race-0", "first")
	require.Equal(t, 0, resp.Code, resp.Message)
	old := chatConvByUUID(t, md["conversation"].(map[string]any)["uuid"].(string))

	chatCloseOnAppend(t, old.ID)
	resp, md = chatPostMessage(t, r, cid, "race-1", "sent while closing")
	require.Equal(t, 0, resp.Code, resp.Message)
	got := md["conversation"].(map[string]any)
	assert.NotEqual(t, old.UUID, got["uuid"], "消息必须进新会话")
	assert.Equal(t, ConvOpen, got["status"])
	assert.Equal(t, int64(2), chatConvCount(t, cid))

	var oldMsgs []ConversationMessage
	require.NoError(t, db.Get().Where("conversation_id = ?", old.ID).Order("id").Find(&oldMsgs).Error)
	require.Len(t, oldMsgs, 2)
	assert.Equal(t, "first", oldMsgs[0].Content)
	assert.Equal(t, chatEventMeta(ChatEventClosed), oldMsgs[1].Meta, "原会话以 closed 事件收尾")

	fresh := chatConvByUUID(t, got["uuid"].(string))
	var newMsgs []ConversationMessage
	require.NoError(t, db.Get().Where("conversation_id = ?", fresh.ID).Find(&newMsgs).Error)
	require.Len(t, newMsgs, 1)
	assert.Equal(t, "sent while closing", newMsgs[0].Content)
	assert.Equal(t, "/pricing", fresh.EntryPath)
}

// 撞上关闭后的重开同样受"新建会话"上限约束：超限 429，消息哪儿都不落。
func TestChatSend_ReopenAfterCloseRespectsNewConversationCap(t *testing.T) {
	r := chatSetup(t, true)
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	oldLimit := chatConvCreateLimiter.limit
	chatConvCreateLimiter.limit = 1
	t.Cleanup(func() { chatConvCreateLimiter.limit = oldLimit })
	resp, md := chatPostMessage(t, r, cid, "cap-0", "first")
	require.Equal(t, 0, resp.Code, resp.Message)
	old := chatConvByUUID(t, md["conversation"].(map[string]any)["uuid"].(string))

	chatCloseOnAppend(t, old.ID)
	resp, _ = chatPostMessage(t, r, cid, "cap-1", "sent while closing")
	assert.Equal(t, int(ErrorTooManyRequests), resp.Code)
	assert.Equal(t, int64(1), chatConvCount(t, cid))
	var n int64
	require.NoError(t, db.Get().Model(&ConversationMessage{}).Where("content = ? AND client_id = ?", "sent while closing", "cap-1").Count(&n).Error)
	assert.Zero(t, n)
}

// ---- 终审修复：访客中途登录 ----

// 游客聊到一半登录：继续用原来的 guest 主体（历史不消失、客服回复推到访客订阅着的频道），
// 并记一行 guest_user_links（session_login，幂等）。guest 没有 open 会话时照旧用 user 主体。
func TestChat_GuestLoginMidConversationKeepsGuestSubject(t *testing.T) {
	r := chatSetup(t, true)
	ctx := context.Background()
	w, _ := chatOpenSession(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	resp, md := chatPostMessage(t, r, cid, "gl-0", "asked as guest")
	require.Equal(t, 0, resp.Code, resp.Message)
	convUUID := md["conversation"].(map[string]any)["uuid"].(string)
	owner, err := findIdentityOwner(ctx, BrandKaitu, IdentityCID, cid)
	require.NoError(t, err)

	user := CreateTestUser(t)
	tok := GenerateTestToken(user.ID, "", time.Hour)
	loggedIn := func(q *TestRequest) *TestRequest {
		return q.WithCookie(CookieChatCid, cid).WithCookie(CookieAccessToken, tok)
	}
	t.Cleanup(func() {
		db.Get().Where("user_id = ?", user.ID).Delete(&GuestUserLink{})
		var ids []uint64
		db.Get().Model(&Conversation{}).Where("subject_kind = ? AND subject_id = ?", SubjectUser, user.ID).Pluck("id", &ids)
		if len(ids) > 0 {
			db.Get().Where("conversation_id IN ?", ids).Delete(&ConversationMessage{})
			db.Get().Where("id IN ?", ids).Delete(&Conversation{})
		}
	})
	userConvs := func() int64 {
		var n int64
		require.NoError(t, db.Get().Model(&Conversation{}).Where("subject_kind = ? AND subject_id = ?", SubjectUser, user.ID).Count(&n).Error)
		return n
	}

	// session：同一会话、历史还在
	_, data := chatOpenSession(t, r, "/", loggedIn)
	c, _ := data["conversation"].(map[string]any)
	require.NotNil(t, c, "登录后历史不得消失")
	assert.Equal(t, convUUID, c["uuid"])
	assert.Contains(t, fmt.Sprint(data["messages"]), "asked as guest")
	// ws 令牌仍是 guest 主体：客服回复才推得到访客订阅的频道
	ws, _ := data["ws"].(map[string]any)
	if ws != nil {
		got, err := parseChatWSToken(ws["token"].(string), time.Now())
		require.NoError(t, err)
		assert.Equal(t, chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: owner.GuestID}, got)
	}
	resp, data = chatDecode(t, loggedIn(NewTestRequest("GET", "/api/chat/ws-token")).Execute(r))
	require.Equal(t, 0, resp.Code, resp.Message)
	got, err := parseChatWSToken(data["token"].(string), time.Now())
	require.NoError(t, err)
	assert.Equal(t, chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: owner.GuestID}, got)

	// messages：同一会话
	resp, data = chatDecode(t, loggedIn(NewTestRequest("GET", "/api/chat/messages")).Execute(r))
	require.Equal(t, 0, resp.Code, resp.Message)
	assert.Contains(t, fmt.Sprint(data["messages"]), "asked as guest")
	// send：续在同一会话
	resp, data = chatDecode(t, loggedIn(NewTestRequest("POST", "/api/chat/messages")).
		WithBody(map[string]any{"kind": "text", "content": "after login", "clientId": "gl-1"}).Execute(r))
	require.Equal(t, 0, resp.Code, resp.Message)
	assert.Equal(t, convUUID, data["conversation"].(map[string]any)["uuid"])
	assert.Zero(t, userConvs(), "不得另开 user 主体的会话")

	var links []GuestUserLink
	require.NoError(t, db.Get().Where("user_id = ?", user.ID).Find(&links).Error)
	require.Len(t, links, 1, "多次请求只写一行")
	assert.Equal(t, owner.GuestID, links[0].GuestID)
	assert.Equal(t, LinkSessionLogin, links[0].Evidence)
	assert.Equal(t, string(BrandKaitu), links[0].Brand)

	// guest 会话关闭后：没有 open 的 guest 会话，行为不变——用 user 主体
	conv := chatConvByUUID(t, convUUID)
	require.NoError(t, closeConversation(ctx, conv))
	_, data = chatOpenSession(t, r, "/", loggedIn)
	assert.Nil(t, data["conversation"])
	resp, _ = chatDecode(t, loggedIn(NewTestRequest("POST", "/api/chat/messages")).
		WithBody(map[string]any{"kind": "text", "content": "as user now", "clientId": "gl-2"}).Execute(r))
	require.Equal(t, 0, resp.Code, resp.Message)
	assert.EqualValues(t, 1, userConvs())
	assert.Equal(t, int64(1), chatConvCount(t, cid), "guest 簇没有新会话")
}
