package center

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 强制留邮箱（chatVisitorHasEmail）：没留邮箱的游客发不了消息，session 告诉挂件要先画邮箱表单；
// 登录用户用账号邮箱，不必填。

// chatUserWithEmail 建一个带加密邮箱登录标识的 kaitu 用户（getUserEmail 能解出来）。
func chatUserWithEmail(t *testing.T, email string) *User {
	t.Helper()
	ctx := context.Background()
	u := &User{UUID: generateId("user"), Brand: string(BrandKaitu)}
	require.NoError(t, db.Get().Create(u).Error)
	enc, err := secretEncryptString(ctx, email)
	require.NoError(t, err)
	require.NoError(t, db.Get().Create(&LoginIdentify{
		UserID: u.ID, Type: "email", IndexID: secretHashIt(ctx, []byte(email)), EncryptedValue: enc, Brand: string(BrandKaitu),
	}).Error)
	t.Cleanup(func() {
		db.Get().Where("user_id = ?", u.ID).Delete(&LoginIdentify{})
		db.Get().Unscoped().Delete(&User{}, u.ID)
	})
	return u
}

func TestChatEmailGate_GuestMustLeaveEmailFirst(t *testing.T) {
	r := chatSetup(t, true)
	w, data := chatOpenSessionNoEmail(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	assert.Equal(t, true, data["emailRequired"])

	resp, _ := chatPostMessage(t, r, cid, "eg-0", "hello")
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Equal(t, chatMsgEmailRequired, resp.Message)
	assert.Zero(t, chatConvCount(t, cid), "没留邮箱不得建会话")

	resp, _ = chatDecode(t, NewTestRequest("POST", "/api/chat/email").WithCookie(CookieChatCid, cid).
		WithBody(map[string]any{"email": "gate-" + strings.ToLower(cid) + "@example.com"}).Execute(r))
	require.Equal(t, 0, resp.Code, resp.Message)

	_, data = chatOpenSessionNoEmail(t, r, "/", func(q *TestRequest) *TestRequest { return q.WithCookie(CookieChatCid, cid) })
	assert.Equal(t, false, data["emailRequired"])
	resp, _ = chatPostMessage(t, r, cid, "eg-1", "hello")
	require.Equal(t, 0, resp.Code, resp.Message)
	assert.Equal(t, int64(1), chatConvCount(t, cid))
}

func TestChatEmailGate_ImageUploadAlsoGated(t *testing.T) {
	r := chatSetup(t, true)
	store := newMemImageStore()
	withImageStore(t, store)
	w, _ := chatOpenSessionNoEmail(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	resp, _ := chatDecode(t, chatUploadImage(t, r, cid, "eg-img", testPNG))
	assert.Empty(t, store.keys(), "没留邮箱不得写存储")
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Equal(t, chatMsgEmailRequired, resp.Message)
	assert.Zero(t, chatConvCount(t, cid))
}

func TestChatEmailGate_LoggedInUserNeedsNoEmail(t *testing.T) {
	r := chatSetup(t, true)
	user := CreateTestUser(t) // 无邮箱标识也放行：user 主体不过这道门
	tok := GenerateTestToken(user.ID, "", time.Hour)
	t.Cleanup(func() {
		var ids []uint64
		db.Get().Model(&Conversation{}).Where("subject_kind = ? AND subject_id = ?", SubjectUser, user.ID).Pluck("id", &ids)
		if len(ids) > 0 {
			db.Get().Where("conversation_id IN ?", ids).Delete(&ConversationMessage{})
			db.Get().Where("id IN ?", ids).Delete(&Conversation{})
		}
	})
	_, data := chatOpenSessionNoEmail(t, r, "/", func(q *TestRequest) *TestRequest { return q.WithCookie(CookieAccessToken, tok) })
	assert.Equal(t, false, data["emailRequired"])
	resp, _ := chatDecode(t, NewTestRequest("POST", "/api/chat/messages").WithCookie(CookieAccessToken, tok).
		WithBody(map[string]any{"kind": "text", "content": "hi", "clientId": "eg-u"}).Execute(r))
	require.Equal(t, 0, resp.Code, resp.Message)
}

// 游客（没留邮箱）聊到一半登录：主体仍是 guest，账号邮箱自动记到簇上，不必再填。
func TestChatEmailGate_GuestLoggedInMidChatUsesAccountEmail(t *testing.T) {
	r := chatSetup(t, true)
	ctx := context.Background()
	w, _ := chatOpenSessionNoEmail(t, r, "/", nil)
	cid := chatCookie(w, CookieChatCid).Value
	chatCleanupCID(t, cid)
	owner, err := findIdentityOwner(ctx, BrandKaitu, IdentityCID, cid)
	require.NoError(t, err)
	require.NotNil(t, owner)
	// 门上线前就开着的会话（没留邮箱）
	_, _, err = ensureConversation(ctx, chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: owner.GuestID}, "/")
	require.NoError(t, err)

	email := fmt.Sprintf("acct-%d@example.com", time.Now().UnixNano())
	user := chatUserWithEmail(t, email)
	t.Cleanup(func() { db.Get().Where("user_id = ?", user.ID).Delete(&GuestUserLink{}) })
	tok := GenerateTestToken(user.ID, "", time.Hour)
	loggedIn := func(q *TestRequest) *TestRequest { return q.WithCookie(CookieChatCid, cid).WithCookie(CookieAccessToken, tok) }

	_, data := chatOpenSessionNoEmail(t, r, "/", loggedIn)
	assert.Equal(t, false, data["emailRequired"])
	assert.Equal(t, email, guestEmail(ctx, owner.GuestID))
	resp, _ := chatDecode(t, loggedIn(NewTestRequest("POST", "/api/chat/messages")).
		WithBody(map[string]any{"kind": "text", "content": "after login", "clientId": "eg-l"}).Execute(r))
	require.Equal(t, 0, resp.Code, resp.Message)
}

// Slack 卡片：登录用户显示账号邮箱（解密），不再是"未留邮箱"。
func TestSlackCard_UserSubjectShowsAccountEmail(t *testing.T) {
	skipIfNoConfig(t)
	email := fmt.Sprintf("card-%d@example.com", time.Now().UnixNano())
	user := chatUserWithEmail(t, email)
	conv := &Conversation{ID: 1 << 60, UUID: "u", Brand: string(BrandKaitu), SubjectKind: SubjectUser, SubjectID: user.ID,
		Status: ConvOpen, Handler: HandlerHuman, LastMessageBy: SenderVisitor, EntryPath: "/support"}
	view := chatSlackRender(context.Background(), conv)
	assert.Contains(t, view.card, fmt.Sprintf("访客: %s · 用户 #%d", email, user.ID))
	assert.Contains(t, view.topic, email)
	assert.Contains(t, view.lobby, email)
}
