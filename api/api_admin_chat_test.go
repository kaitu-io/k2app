package center

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// adminChatRouter 镜像 route.go 的 /app opsAdmin 组（BrandResolver + StaffAuthRequired），
// 路由用生产同一个注册函数挂上去，所以 RoleRequired 是真的那一道。
func adminChatRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	app := r.Group("/app", BrandResolver(), StaffAuthRequired())
	registerAdminChatRoutes(app)
	return r
}

func acDo(t *testing.T, r *gin.Engine, method, key, path string) *TestResponse {
	t.Helper()
	w := NewTestRequest(method, path).WithHeader("X-Access-Key", key).Execute(r)
	resp, err := ParseResponse(w)
	require.NoError(t, err, "body: %s", w.Body.String())
	return resp
}

func acSupportKey(t *testing.T) string {
	t.Helper()
	return createAccessKeyUserForTest(t, BrandKaitu, RoleUser|RoleSupport)
}

// acConv 建一个 guest 会话，并给 guest 挂一个唯一邮箱标识；返回会话与邮箱。
func acConv(t *testing.T, brand Brand, mod func(*Conversation)) (*Conversation, string) {
	t.Helper()
	conv := slackConv(t, brand, "/pricing")
	email := strings.ToLower(generateId("acg")) + "@example.test"
	require.NoError(t, db.Get().Create(&GuestIdentity{
		GuestID: conv.SubjectID, Brand: string(brand), Kind: IdentityEmail, Value: email,
		Strength: StrengthClaimed, FirstSeenAt: time.Now(), LastSeenAt: time.Now(),
	}).Error)
	if mod != nil {
		mod(conv)
		require.NoError(t, db.Get().Save(conv).Error)
	}
	return conv, email
}

type acListData struct {
	Items      []map[string]any `json:"items"`
	Pagination struct {
		Page     int   `json:"page"`
		PageSize int   `json:"pageSize"`
		Total    int64 `json:"total"`
	} `json:"pagination"`
}

func acList(t *testing.T, r *gin.Engine, key, query string) acListData {
	t.Helper()
	resp := acDo(t, r, "GET", key, "/app/chat/conversations?"+query)
	require.Equal(t, int(ErrorNone), resp.Code, resp.Message)
	var d acListData
	require.NoError(t, json.Unmarshal(resp.Data, &d))
	return d
}

func acUUIDs(d acListData) []string {
	var out []string
	for _, it := range d.Items {
		out = append(out, it["uuid"].(string))
	}
	return out
}

func TestAdminChat_RequiresSupportRole(t *testing.T) {
	skipIfNoConfig(t)
	r := adminChatRouter()
	conv, _ := acConv(t, BrandKaitu, nil)
	plain := createAccessKeyUserForTest(t, BrandKaitu, RoleUser)
	marketing := createAccessKeyUserForTest(t, BrandKaitu, RoleUser|RoleMarketing)
	support := acSupportKey(t)
	paths := []struct{ method, path string }{
		{"GET", "/app/chat/conversations"},
		{"GET", "/app/chat/conversations/" + conv.UUID},
		{"PUT", "/app/chat/conversations/" + conv.UUID + "/close"},
	}
	for _, p := range paths {
		assert.Equal(t, int(ErrorForbidden), acDo(t, r, p.method, plain, p.path).Code, "plain %s", p.path)
		assert.Equal(t, int(ErrorForbidden), acDo(t, r, p.method, marketing, p.path).Code, "other role %s", p.path)
	}
	assert.Equal(t, int(ErrorNone), acDo(t, r, "GET", support, paths[0].path).Code)
	assert.Equal(t, int(ErrorNone), acDo(t, r, "GET", support, paths[1].path).Code)
	// 没有任何写接口：除 close 外不存在别的方法/路径
	for _, p := range []struct{ method, path string }{{"DELETE", paths[1].path}, {"POST", paths[0].path}, {"PUT", paths[1].path}} {
		w := NewTestRequest(p.method, p.path).WithHeader("X-Access-Key", support).Execute(r)
		assert.Equal(t, 404, w.Code, "%s %s", p.method, p.path)
	}
}

// 接线守卫：生产路由把这组路由挂在 opsAdmin 组上（测试路由器用的是同一个注册函数）。
func TestAdminChat_RoutesWiredIntoOpsAdmin(t *testing.T) {
	src, err := os.ReadFile("route.go")
	require.NoError(t, err)
	assert.Contains(t, string(src), "registerAdminChatRoutes(opsAdmin)")
}

func TestAdminChat_ListFiltersByStatusAndEmail(t *testing.T) {
	skipIfNoConfig(t)
	r := adminChatRouter()
	key := acSupportKey(t)
	openAI, emailA := acConv(t, BrandKaitu, nil)
	closedHuman, emailB := acConv(t, BrandKaitu, func(c *Conversation) {
		c.Status, c.Handler = ConvClosed, HandlerHuman
	})

	// email 子串匹配 guest 邮箱标识
	d := acList(t, r, key, "email="+url.QueryEscape(emailA))
	assert.Equal(t, []string{openAI.UUID}, acUUIDs(d))
	assert.EqualValues(t, 1, d.Pagination.Total)
	assert.Equal(t, emailA, d.Items[0]["email"])
	assert.Equal(t, "guest", d.Items[0]["subjectKind"])
	assert.Equal(t, "/pricing", d.Items[0]["entryPath"])

	// status / handler 叠加邮箱
	assert.Equal(t, []string{closedHuman.UUID}, acUUIDs(acList(t, r, key, "email="+url.QueryEscape(emailB)+"&status=closed")))
	assert.Empty(t, acUUIDs(acList(t, r, key, "email="+url.QueryEscape(emailB)+"&status=open")))
	assert.Equal(t, []string{closedHuman.UUID}, acUUIDs(acList(t, r, key, "email="+url.QueryEscape(emailB)+"&handler=human")))
	assert.Empty(t, acUUIDs(acList(t, r, key, "email="+url.QueryEscape(emailB)+"&handler=ai")))

	// 邮箱不存在 → 空列表而不是全表
	assert.Empty(t, acList(t, r, key, "email=nobody-"+url.QueryEscape(generateId("x"))+"@example.test").Items)
}

func TestAdminChat_ListUserSubjectEmail(t *testing.T) {
	skipIfNoConfig(t)
	r := adminChatRouter()
	key := acSupportKey(t)
	email := strings.ToLower(generateId("acu")) + "@example.test"
	u := &User{UUID: generateId("acu"), Brand: string(BrandKaitu)}
	require.NoError(t, db.Get().Create(u).Error)
	idx := secretHashIt(t.Context(), []byte(email))
	enc, err := secretEncryptString(t.Context(), email)
	require.NoError(t, err)
	require.NoError(t, db.Get().Create(&LoginIdentify{UserID: u.ID, Type: "email", IndexID: idx, EncryptedValue: enc, Brand: string(BrandKaitu)}).Error)
	conv := &Conversation{UUID: generateId("acu"), Brand: string(BrandKaitu), SubjectKind: SubjectUser, SubjectID: u.ID,
		Status: ConvOpen, Handler: HandlerHuman, LastMessageAt: time.Now(), CreatedAt: time.Now()}
	require.NoError(t, db.Get().Create(conv).Error)
	t.Cleanup(func() {
		db.Get().Where("id = ?", conv.ID).Delete(&Conversation{})
		db.Get().Where("user_id = ?", u.ID).Delete(&LoginIdentify{})
		db.Get().Unscoped().Delete(u)
	})

	d := acList(t, r, key, "email="+url.QueryEscape(email))
	require.Equal(t, []string{conv.UUID}, acUUIDs(d))
	assert.Equal(t, email, d.Items[0]["email"], "登录邮箱按工单接口同样的做法明文展示")
	assert.Equal(t, "user", d.Items[0]["subjectKind"])
}

func TestAdminChat_ListShape_Order_Pagination(t *testing.T) {
	skipIfNoConfig(t)
	r := adminChatRouter()
	key := acSupportKey(t)
	now := time.Now()
	old, _ := acConv(t, BrandKaitu, func(c *Conversation) { c.LastMessageAt = now.Add(-2 * time.Hour); c.LastMessageBy = SenderVisitor })
	mid, _ := acConv(t, BrandKaitu, func(c *Conversation) { c.LastMessageAt = now.Add(-time.Hour); c.SlackChannelID = "C0ADMINCHAT" })
	newest, _ := acConv(t, BrandKaitu, func(c *Conversation) { c.LastMessageAt = now; c.LastMessageBy = SenderStaff })

	// 夹在这三条之间的时间窗口无法过滤，所以按 brand=kaitu 取全量里前几行：新→旧
	d := acList(t, r, key, "brand=kaitu&page=1&pageSize=100")
	pos := map[string]int{}
	for i, id := range acUUIDs(d) {
		pos[id] = i
	}
	require.Contains(t, pos, newest.UUID)
	require.Contains(t, pos, mid.UUID)
	require.Contains(t, pos, old.UUID)
	assert.Less(t, pos[newest.UUID], pos[mid.UUID])
	assert.Less(t, pos[mid.UUID], pos[old.UUID])
	assert.Equal(t, 100, d.Pagination.PageSize)
	assert.Equal(t, 1, d.Pagination.Page)

	byUUID := map[string]map[string]any{}
	for _, it := range d.Items {
		byUUID[it["uuid"].(string)] = it
	}
	row := byUUID[newest.UUID]
	for _, k := range []string{"uuid", "brand", "subjectKind", "subjectId", "email", "status", "handler", "entryPath",
		"lastMessageAt", "lastMessageBy", "slackPermalink", "createdAt"} {
		assert.Contains(t, row, k)
	}
	assert.Equal(t, "kaitu", row["brand"])
	assert.Equal(t, "staff", row["lastMessageBy"])
	// 时间戳是 unix 秒（number），与工单接口一致
	assert.InDelta(t, float64(now.Unix()), row["lastMessageAt"], 2)
	assert.InDelta(t, float64(newest.CreatedAt.Unix()), row["createdAt"], 2)
	assert.Equal(t, "", row["slackPermalink"], "没有频道时为空串")
	assert.Equal(t, chatSlackChannelURL("C0ADMINCHAT"), byUUID[mid.UUID]["slackPermalink"])

	// 分页有上限：pageSize 超限被收敛，不会无界
	big := acList(t, r, key, "pageSize=100000")
	assert.LessOrEqual(t, len(big.Items), 100)
	assert.Equal(t, 100, big.Pagination.PageSize)
	// page=2、pageSize=1 取到第二新
	p2 := acList(t, r, key, "brand=kaitu&page=2&pageSize=1")
	require.Len(t, p2.Items, 1)
	p1 := acList(t, r, key, "brand=kaitu&page=1&pageSize=1")
	require.Len(t, p1.Items, 1)
	assert.NotEqual(t, p1.Items[0]["uuid"], p2.Items[0]["uuid"])
}

func TestAdminChat_BrandFilterDoesNotLeak(t *testing.T) {
	skipIfNoConfig(t)
	r := adminChatRouter()
	key := acSupportKey(t)
	kc, kEmail := acConv(t, BrandKaitu, nil)
	oc, oEmail := acConv(t, BrandOverleap, nil)

	ids := acUUIDs(acList(t, r, key, "brand=overleap&pageSize=100"))
	assert.Contains(t, ids, oc.UUID)
	assert.NotContains(t, ids, kc.UUID)
	ids = acUUIDs(acList(t, r, key, "brand=kaitu&pageSize=100"))
	assert.Contains(t, ids, kc.UUID)
	assert.NotContains(t, ids, oc.UUID)

	// 邮箱命中也不得越过品牌过滤
	assert.Empty(t, acList(t, r, key, "brand=kaitu&email="+url.QueryEscape(oEmail)).Items)
	assert.Empty(t, acList(t, r, key, "brand=overleap&email="+url.QueryEscape(kEmail)).Items)

	// 详情同理：带了品牌过滤就不能读到另一个品牌的会话
	assert.Equal(t, int(ErrorNotFound), acDo(t, r, "GET", key, "/app/chat/conversations/"+oc.UUID+"?brand=kaitu").Code)
	assert.Equal(t, int(ErrorNone), acDo(t, r, "GET", key, "/app/chat/conversations/"+oc.UUID+"?brand=overleap").Code)
}

func TestAdminChat_DetailIncludesNotes(t *testing.T) {
	skipIfNoConfig(t)
	r := adminChatRouter()
	key := acSupportKey(t)
	conv, email := acConv(t, BrandKaitu, nil)
	slackSeed(t, conv, SenderVisitor, MsgText, "你好")
	slackSeed(t, conv, SenderStaff, MsgNote, "内部备注：已核实订单", func(m *ConversationMessage) { m.SenderName = "alice" })
	slackSeed(t, conv, SenderSystem, MsgEvent, "会话已关闭", func(m *ConversationMessage) { m.Meta = chatEventMeta(ChatEventClosed) })

	resp := acDo(t, r, "GET", key, "/app/chat/conversations/"+conv.UUID)
	require.Equal(t, int(ErrorNone), resp.Code, resp.Message)
	var d struct {
		Conversation map[string]any `json:"conversation"`
		Messages     []map[string]any
	}
	require.NoError(t, json.Unmarshal(resp.Data, &d))
	assert.Equal(t, conv.UUID, d.Conversation["uuid"])
	assert.Equal(t, email, d.Conversation["email"])
	require.Len(t, d.Messages, 3)
	// 旧→新
	assert.Equal(t, "text", d.Messages[0]["kind"])
	assert.Equal(t, "note", d.Messages[1]["kind"])
	assert.Equal(t, "内部备注：已核实订单", d.Messages[1]["content"])
	assert.Equal(t, "staff", d.Messages[1]["senderType"])
	assert.Equal(t, "alice", d.Messages[1]["senderName"])
	assert.Equal(t, "event", d.Messages[2]["kind"])
	assert.NotNil(t, d.Messages[2]["meta"])
	assert.InDelta(t, float64(time.Now().Unix()), d.Messages[0]["createdAt"], 5, "unix 秒")
	for _, m := range d.Messages {
		assert.Contains(t, m, "id")
	}

	assert.Equal(t, int(ErrorNotFound), acDo(t, r, "GET", key, "/app/chat/conversations/no-such-uuid").Code)
}

func TestAdminChat_DetailMessagesCapped(t *testing.T) {
	skipIfNoConfig(t)
	r := adminChatRouter()
	key := acSupportKey(t)
	conv, _ := acConv(t, BrandKaitu, nil)
	old := adminChatMessageLimit
	adminChatMessageLimit = 3
	t.Cleanup(func() { adminChatMessageLimit = old })
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		i := i
		slackSeed(t, conv, SenderVisitor, MsgText, fmt.Sprintf("m%d", i), func(m *ConversationMessage) { m.CreatedAt = base.Add(time.Duration(i) * time.Minute) })
	}
	resp := acDo(t, r, "GET", key, "/app/chat/conversations/"+conv.UUID)
	require.Equal(t, int(ErrorNone), resp.Code)
	var d struct {
		Messages  []map[string]any `json:"messages"`
		Truncated bool             `json:"truncated"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &d))
	require.Len(t, d.Messages, 3)
	assert.True(t, d.Truncated)
	// 保留的是最新 3 条，顺序旧→新
	assert.Equal(t, "m2", d.Messages[0]["content"])
	assert.Equal(t, "m4", d.Messages[2]["content"])
}

func TestAdminChat_PermissionGroup(t *testing.T) {
	assert.Contains(t, allGroups, "chat")
	assert.Contains(t, roleGroupMap[RoleSupport], "chat")
	for role, groups := range roleGroupMap {
		if role != RoleSupport {
			assert.NotContains(t, groups, "chat", "role %d", role)
		}
	}

	skipIfNoConfig(t)
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	app := r.Group("/app", BrandResolver(), StaffAuthRequired())
	app.GET("/my-permissions", api_admin_my_permissions)
	groupsOf := func(key string) []string {
		resp := acDo(t, r, "GET", key, "/app/my-permissions")
		require.Equal(t, int(ErrorNone), resp.Code, resp.Message)
		var p struct {
			Groups []string `json:"groups"`
		}
		require.NoError(t, json.Unmarshal(resp.Data, &p))
		return p.Groups
	}
	assert.Contains(t, groupsOf(acSupportKey(t)), "chat")
	assert.NotContains(t, groupsOf(createAccessKeyUserForTest(t, BrandKaitu, RoleUser|RoleMarketing)), "chat")
}

func acAuditCount(t *testing.T, convUUID string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Get().Model(&AdminAuditLog{}).Where("action = ? AND target_type = ? AND target_id = ?",
		"chat_conversation_close", "conversation", convUUID).Count(&n).Error)
	return n
}

func TestAdminChat_CloseWritesAudit(t *testing.T) {
	skipIfNoConfig(t)
	r := adminChatRouter()
	key := acSupportKey(t)
	conv, _ := acConv(t, BrandKaitu, nil)
	t.Cleanup(func() {
		db.Get().Where("action = ? AND target_id = ?", "chat_conversation_close", conv.UUID).Delete(&AdminAuditLog{})
	})

	resp := acDo(t, r, "PUT", key, "/app/chat/conversations/"+conv.UUID+"/close")
	require.Equal(t, int(ErrorNone), resp.Code, resp.Message)

	got := slackReload(t, conv.ID)
	assert.Equal(t, ConvClosed, got.Status)
	assert.NotNil(t, got.ClosedAt)
	var ev ConversationMessage
	require.NoError(t, db.Get().Where("conversation_id = ? AND kind = ?", conv.ID, MsgEvent).First(&ev).Error)
	assert.Equal(t, chatEventMeta(ChatEventClosed), ev.Meta)

	require.Eventually(t, func() bool { return acAuditCount(t, conv.UUID) == 1 }, 5*time.Second, 50*time.Millisecond, "审计日志是异步写入")

	// 幂等：再关一次不报错、不重复事件
	resp = acDo(t, r, "PUT", key, "/app/chat/conversations/"+conv.UUID+"/close")
	require.Equal(t, int(ErrorNone), resp.Code, resp.Message)
	var n int64
	db.Get().Model(&ConversationMessage{}).Where("conversation_id = ? AND kind = ?", conv.ID, MsgEvent).Count(&n)
	assert.EqualValues(t, 1, n)

	assert.Equal(t, int(ErrorNotFound), acDo(t, r, "PUT", key, "/app/chat/conversations/no-such-uuid/close").Code)
}

func TestAdminChat_CloseArchivesSlackChannel(t *testing.T) {
	skipIfNoConfig(t)
	f := newFakeSlack(t)
	r := adminChatRouter()
	key := acSupportKey(t)
	conv, _ := acConv(t, BrandKaitu, func(c *Conversation) { c.SlackChannelID = "C0ADMINCLOSE" })

	resp := acDo(t, r, "PUT", key, "/app/chat/conversations/"+conv.UUID+"/close")
	require.Equal(t, int(ErrorNone), resp.Code, resp.Message)
	t.Cleanup(func() {
		db.Get().Where("action = ? AND target_id = ?", "chat_conversation_close", conv.UUID).Delete(&AdminAuditLog{})
	})
	arch := f.CallsOf("conversations.archive")
	require.Len(t, arch, 1)
	assert.Equal(t, "C0ADMINCLOSE", arch[0].Str("channel"))
	assert.NotNil(t, slackReload(t, conv.ID).SlackArchivedAt)
}
