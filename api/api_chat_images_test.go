package center

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/openai/filesearch"
)

// memImageStore 内存里的图片存储：记录写入，签出的"下载地址"可预测，便于断言。
type memImageStore struct {
	mu   sync.Mutex
	objs map[string]memImageObj
	fail bool
}

type memImageObj struct {
	contentType string
	data        []byte
}

func (s *memImageStore) Put(_ context.Context, key, ct string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return assert.AnError
	}
	s.objs[key] = memImageObj{ct, append([]byte(nil), data...)}
	return nil
}

func (s *memImageStore) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	return "https://s3.test/" + key + "?ttl=" + ttl.String(), nil
}

func (s *memImageStore) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.objs {
		out = append(out, k)
	}
	return out
}

// withImageStore 装上内存存储（nil = 模拟未配置桶），测试结束恢复。
func withImageStore(t *testing.T, store *memImageStore) {
	t.Helper()
	orig := chatImages
	if store == nil {
		chatImages = func() chatImageStore { return nil }
	} else {
		chatImages = func() chatImageStore { return store }
	}
	t.Cleanup(func() { chatImages = orig })
}

func newMemImageStore() *memImageStore { return &memImageStore{objs: map[string]memImageObj{}} }

// 最小合法 PNG 头（DetectContentType 只看前几个字节）。
var testPNG = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), bytes.Repeat([]byte{0}, 64)...)

func chatUploadImage(t *testing.T, r http.Handler, cid, clientID string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if clientID != "" {
		require.NoError(t, mw.WriteField("clientId", clientID))
	}
	if data != nil {
		fw, err := mw.CreateFormFile("file", "shot.png")
		require.NoError(t, err)
		_, _ = fw.Write(data)
	}
	require.NoError(t, mw.Close())
	req := httptest.NewRequest("POST", "/api/chat/images", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-K2-Brand", "kaitu")
	if cid != "" {
		req.AddCookie(&http.Cookie{Name: CookieChatCid, Value: cid})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// 建一个访客（有 cid cookie），返回 cid。
func chatNewVisitor(t *testing.T, r *gin.Engine) string {
	t.Helper()
	w, data := chatOpenSession(t, r, "/pricing", nil)
	require.Equal(t, true, data["enabled"])
	ck := chatCookie(w, CookieChatCid)
	require.NotNil(t, ck)
	chatCleanupCID(t, ck.Value)
	return ck.Value
}

func TestChatSniffImage(t *testing.T) {
	assert.Equal(t, "image/png", chatSniffImage(testPNG))
	assert.Equal(t, "image/jpeg", chatSniffImage([]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00")))
	assert.Equal(t, "image/gif", chatSniffImage([]byte("GIF89a\x01\x00\x01\x00")))
	assert.Equal(t, "image/webp", chatSniffImage([]byte("RIFF\x00\x00\x00\x00WEBPVP8 ")))
	assert.Equal(t, "", chatSniffImage([]byte("<svg xmlns='http://www.w3.org/2000/svg'></svg>")), "SVG 可带脚本，不收")
	assert.Equal(t, "", chatSniffImage([]byte("<html><script>alert(1)</script>")))
	assert.Equal(t, "", chatSniffImage([]byte("%PDF-1.4")))
}

func TestChatImageToken(t *testing.T) {
	skipIfNoConfig(t) // 签名密钥取自 jwt.secret
	now := time.Now()
	tok := signChatImageToken(42, time.Hour)
	require.NotEmpty(t, tok)
	id, err := parseChatImageToken(tok, now)
	require.NoError(t, err)
	assert.Equal(t, uint64(42), id)

	_, err = parseChatImageToken(tok, now.Add(2*time.Hour))
	assert.Error(t, err, "过期")
	_, err = parseChatImageToken(signChatWSToken(chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: 42}, time.Hour), now)
	assert.Error(t, err, "别的用途的令牌不能冒充")
	_, err = parseChatImageToken(tok[:len(tok)-2]+"AA", now)
	assert.Error(t, err, "签名被改")
}

func TestChatImages_UploadStoresAndReturnsSignedPath(t *testing.T) {
	r := chatSetup(t, true)
	store := newMemImageStore()
	withImageStore(t, store)
	cid := chatNewVisitor(t, r)

	_, data := chatDecode(t, chatUploadImage(t, r, cid, "c-img-1", testPNG))
	msg := data["message"].(map[string]any)
	assert.Equal(t, "image", msg["kind"])
	assert.Equal(t, "visitor", msg["senderType"])
	path := msg["content"].(string)
	require.True(t, strings.HasPrefix(path, "/api/chat/images/"), path)

	// 库里存的是对象 key，不是链接；对象按嗅探出的类型写入
	var row ConversationMessage
	require.NoError(t, db.Get().Where("id = ?", uint64(msg["id"].(float64))).First(&row).Error)
	assert.Regexp(t, `^chat/kaitu/\d{6}/[0-9a-f-]{36}\.png$`, row.Content)
	require.Equal(t, []string{row.Content}, store.keys())
	assert.Equal(t, "image/png", store.objs[row.Content].contentType)
	assert.JSONEq(t, `{"type":"image/png","size":80}`, row.Meta)

	// 查看链接：302 到短期下载地址
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://s3.test/"+row.Content+"?ttl=5m0s", w.Header().Get("Location"))

	// 会话里拉消息也给同样形态的链接
	_, list := chatDecode(t, NewTestRequest("GET", "/api/chat/messages").WithCookie(CookieChatCid, cid).Execute(r))
	items := list["messages"].([]any)
	last := items[len(items)-1].(map[string]any)
	assert.True(t, strings.HasPrefix(last["content"].(string), "/api/chat/images/"))
}

func TestChatImages_UploadRejects(t *testing.T) {
	r := chatSetup(t, true)
	store := newMemImageStore()
	withImageStore(t, store)
	cid := chatNewVisitor(t, r)

	cases := []struct {
		name     string
		clientID string
		data     []byte
	}{
		{"不是图片", "c1", []byte("<html><body>hi</body></html>")},
		{"SVG", "c2", []byte("<svg xmlns='http://www.w3.org/2000/svg'></svg>")},
		{"超过 5MB", "c3", append(append([]byte(nil), testPNG...), make([]byte, chatImageMaxBytes)...)},
		{"空文件", "c4", []byte{}},
		{"缺 clientId", "", testPNG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := chatUploadImage(t, r, cid, tc.clientID, tc.data)
			resp, _ := chatDecode(t, w)
			assert.NotEqual(t, 0, resp.Code)
		})
	}
	assert.Empty(t, store.keys(), "被拒的上传不得写存储")

	t.Run("没有访客主体", func(t *testing.T) {
		resp, _ := chatDecode(t, chatUploadImage(t, r, "", "c5", testPNG))
		assert.NotEqual(t, 0, resp.Code)
		assert.Empty(t, store.keys())
	})
	t.Run("存储写失败不落消息", func(t *testing.T) {
		store.fail = true
		t.Cleanup(func() { store.fail = false })
		resp, _ := chatDecode(t, chatUploadImage(t, r, cid, "c6", testPNG))
		assert.NotEqual(t, 0, resp.Code)
		var n int64
		db.Get().Model(&ConversationMessage{}).Where("client_id = ?", "c6").Count(&n)
		assert.Zero(t, n)
	})
}

func TestChatImages_DisabledWithoutBucket(t *testing.T) {
	r := chatSetup(t, true)
	withImageStore(t, nil)
	_, data := chatOpenSession(t, r, "/pricing", nil)
	assert.Equal(t, false, data["images"], "没配桶：挂件不画发图片按钮")
	resp, _ := chatDecode(t, chatUploadImage(t, r, "whatever", "c1", testPNG))
	assert.NotEqual(t, 0, resp.Code)

	withImageStore(t, newMemImageStore())
	w, data := chatOpenSession(t, r, "/pricing", nil)
	if ck := chatCookie(w, CookieChatCid); ck != nil {
		chatCleanupCID(t, ck.Value)
	}
	assert.Equal(t, true, data["images"])
}

func TestChatImages_ViewRejects(t *testing.T) {
	skipIfNoConfig(t)
	r := chatRouter()
	withImageStore(t, newMemImageStore())
	conv, _, err := ensureConversation(context.Background(), newChatSubject(t), "/test")
	require.NoError(t, err)
	text, _, err := appendMessage(context.Background(), conv, appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "hi"})
	require.NoError(t, err)

	for name, path := range map[string]string{
		"文字消息的 id": "/api/chat/images/" + signChatImageToken(text.ID, time.Hour),
		"不存在的消息":   "/api/chat/images/" + signChatImageToken(1<<62, time.Hour),
		"伪造令牌":     "/api/chat/images/abc.def",
		"已过期":      "/api/chat/images/" + signChatImageToken(text.ID, -time.Minute),
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		assert.Equal(t, http.StatusNotFound, w.Code, name)
	}
}

func TestChatImages_SlackShowsLinkUnderBrandSite(t *testing.T) {
	chatTokenSecret(t) // 查看链接要签名；mock-only CI 没有 jwt.secret
	m :=&ConversationMessage{ID: 7, SenderType: SenderVisitor, Kind: MsgImage, Content: "chat/overleap/202610/x.png"}
	text, post := chatSlackMessageText(BrandOverleap, m)
	require.True(t, post)
	assert.Contains(t, text, "<"+BrandOverleap.Config().BaseURL+"/api/chat/images/")
	assert.Contains(t, text, "|[图片] 点击查看>")
	assert.NotContains(t, text, "chat/overleap/202610/x.png", "频道里不出现对象 key")
}

func TestChatAI_SeesVisitorImage(t *testing.T) {
	conv := newAIConv(t)
	withImageStore(t, newMemImageStore())
	var gotQ string
	var gotImgs []string
	var gotHist []filesearch.Message
	orig := chatAIAsk
	chatAIAsk = func(_ context.Context, _, q string, imgs []string, h []filesearch.Message) (string, error) {
		gotQ, gotImgs, gotHist = q, imgs, h
		return "收到截图", nil
	}
	t.Cleanup(func() { chatAIAsk = orig })

	visitorSays(t, conv, MsgImage, "chat/kaitu/202610/a.png")
	assert.Equal(t, "[访客发来一张截图]", gotQ)
	assert.Equal(t, []string{"https://s3.test/chat/kaitu/202610/a.png?ttl=15m0s"}, gotImgs)

	// 下一轮：上一张图进历史，仍带图片地址
	visitorSays(t, conv, MsgText, "怎么办")
	require.NotEmpty(t, gotHist)
	var found bool
	for _, h := range gotHist {
		if h.Role == "user" && len(h.Images) == 1 && strings.Contains(h.Images[0], "a.png") {
			found = true
		}
	}
	assert.True(t, found, "历史里的截图要带上")
}
