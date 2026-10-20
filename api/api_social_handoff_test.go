package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 浏览器 handoff（spec §6.3）集成测试：真路由、真库、miniredis（testInitConfig 提供）。

type handoffCreateResp struct {
	Code int `json:"code"`
	Data struct {
		SessionID  string `json:"sessionId"`
		PollSecret string `json:"pollSecret"`
		URL        string `json:"url"`
	} `json:"data"`
}

type handoffPollResp struct {
	Code int                   `json:"code"`
	Data DataSocialHandoffPoll `json:"data"`
}

// postHandoff 打 /api/auth/social/<path>；host 非空时按 Host 解析品牌，否则用 X-K2-Brand。
func postHandoff(t *testing.T, path string, brand Brand, host string, body map[string]string, out any) {
	t.Helper()
	r := SetupTestRouter()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/social/"+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if host != "" {
		req.Host = host
	} else {
		req.Header.Set("X-K2-Brand", string(brand))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), out), "body=%s", w.Body.String())
}

func createHandoff(t *testing.T, brand Brand, provider string) handoffCreateResp {
	t.Helper()
	var resp handoffCreateResp
	postHandoff(t, "handoff", brand, "", map[string]string{"provider": provider}, &resp)
	require.Equal(t, 0, resp.Code)
	require.NotEmpty(t, resp.Data.SessionID)
	require.NotEmpty(t, resp.Data.PollSecret)
	return resp
}

// handoffURLNonce 取出 url 里交给浏览器页的 nonce（= sha256hex(会话 RawNonce)）。
func handoffURLNonce(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	n := u.Query().Get("nonce")
	require.NotEmpty(t, n)
	return n
}

func completeHandoff(t *testing.T, brand Brand, host, sessionID, idToken string) int {
	t.Helper()
	var resp struct {
		Code int `json:"code"`
	}
	postHandoff(t, "handoff/complete", brand, host, map[string]string{"sessionId": sessionID, "idToken": idToken}, &resp)
	return resp.Code
}

func pollHandoff(t *testing.T, brand Brand, sessionID, pollSecret, udid string) handoffPollResp {
	t.Helper()
	var resp handoffPollResp
	postHandoff(t, "handoff/poll", brand, "", map[string]string{
		"sessionId": sessionID, "pollSecret": pollSecret, "udid": udid, "remark": "handoff test",
	}, &resp)
	return resp
}

func TestHandoff_LoginHappyPath(t *testing.T) {
	skipIfNoConfig(t)
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	enableTestSocialAuth(t, BrandKaitu)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()

	email := "ho-" + sfx + "@example.com"
	u := tr.seedUser(BrandKaitu, email)
	sub := tr.sub(SocialProviderGoogle, "ho-g-"+sfx)
	udid := trackSocialLoginDevice(t, "udid-ho-"+sfx)

	h := createHandoff(t, BrandKaitu, SocialProviderGoogle)
	cl := googleTestClaims(BrandKaitu.Config().Social.GoogleWebClientID)
	cl["sub"] = sub
	cl["email"] = email
	cl["nonce"] = handoffURLNonce(t, h.Data.URL)
	require.Equal(t, 0, completeHandoff(t, BrandKaitu, "", h.Data.SessionID, signTestIDToken(t, key, cl)))

	got := pollHandoff(t, BrandKaitu, h.Data.SessionID, h.Data.PollSecret, udid)
	require.Equal(t, 0, got.Code)
	assert.Equal(t, "done", got.Data.Status)
	require.NotNil(t, got.Data.Auth)
	assert.NotEmpty(t, got.Data.Auth.AccessToken)
	assert.Equal(t, 1, identitiesOfUser(t, u.ID)[SocialProviderGoogle])

	again := pollHandoff(t, BrandKaitu, h.Data.SessionID, h.Data.PollSecret, udid)
	assert.Equal(t, int(ErrorSocialHandoffExpired), again.Code, "会话领取一次即作废")
}

func TestHandoff_PollBeforeComplete(t *testing.T) {
	skipIfNoConfig(t)
	h := createHandoff(t, BrandKaitu, SocialProviderApple)
	got := pollHandoff(t, BrandKaitu, h.Data.SessionID, h.Data.PollSecret, "udid-ho-pending-"+socialTestSuffix())
	require.Equal(t, 0, got.Code)
	assert.Equal(t, "pending", got.Data.Status)
	assert.Nil(t, got.Data.Auth)
}

func TestHandoff_WrongPollSecret(t *testing.T) {
	skipIfNoConfig(t)
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	enableTestSocialAuth(t, BrandKaitu)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	sub := tr.sub(SocialProviderGoogle, "ho-ws-"+sfx)
	tr.email("ho-ws-" + sfx + "@example.com")
	udid := trackSocialLoginDevice(t, "udid-ho-ws-"+sfx)

	h := createHandoff(t, BrandKaitu, SocialProviderGoogle)
	cl := googleTestClaims(BrandKaitu.Config().Social.GoogleWebClientID)
	cl["sub"] = sub
	cl["email"] = "ho-ws-" + sfx + "@example.com"
	cl["nonce"] = handoffURLNonce(t, h.Data.URL)
	require.Equal(t, 0, completeHandoff(t, BrandKaitu, "", h.Data.SessionID, signTestIDToken(t, key, cl)))

	bad := pollHandoff(t, BrandKaitu, h.Data.SessionID, strings.Repeat("0", 64), udid)
	assert.Equal(t, int(ErrorSocialHandoffExpired), bad.Code)

	good := pollHandoff(t, BrandKaitu, h.Data.SessionID, h.Data.PollSecret, udid)
	require.Equal(t, 0, good.Code, "错误 secret 不应作废会话")
	assert.Equal(t, "done", good.Data.Status)
	require.NotNil(t, good.Data.Auth)
}

func TestHandoff_NonceFromSessionRequired(t *testing.T) {
	skipIfNoConfig(t)
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	enableTestSocialAuth(t, BrandKaitu)

	h := createHandoff(t, BrandKaitu, SocialProviderGoogle)
	cl := googleTestClaims(BrandKaitu.Config().Social.GoogleWebClientID)
	cl["sub"] = "ho-nonce-" + socialTestSuffix()
	cl["nonce"] = socialNonceHash("attacker-chosen-nonce")
	assert.Equal(t, int(ErrorSocialTokenInvalid), completeHandoff(t, BrandKaitu, "", h.Data.SessionID, signTestIDToken(t, key, cl)))

	got := pollHandoff(t, BrandKaitu, h.Data.SessionID, h.Data.PollSecret, "udid-ho-nonce-"+socialTestSuffix())
	assert.Equal(t, int(ErrorSocialTokenInvalid), got.Code)

	gone := pollHandoff(t, BrandKaitu, h.Data.SessionID, h.Data.PollSecret, "udid-ho-nonce-"+socialTestSuffix())
	assert.Equal(t, int(ErrorSocialHandoffExpired), gone.Code, "failed 结果领取后会话删除")
}

func TestHandoff_BrandMismatch(t *testing.T) {
	skipIfNoConfig(t)
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	enableTestSocialAuth(t, BrandKaitu)
	enableTestSocialAuth(t, BrandOverleap)

	h := createHandoff(t, BrandKaitu, SocialProviderGoogle)
	cl := googleTestClaims(BrandOverleap.Config().Social.GoogleWebClientID)
	cl["sub"] = "ho-brand-" + socialTestSuffix()
	cl["nonce"] = handoffURLNonce(t, h.Data.URL)
	assert.Equal(t, int(ErrorSocialHandoffExpired),
		completeHandoff(t, "", "www.overleap.io", h.Data.SessionID, signTestIDToken(t, key, cl)))

	got := pollHandoff(t, BrandKaitu, h.Data.SessionID, h.Data.PollSecret, "udid-ho-brand-"+socialTestSuffix())
	require.Equal(t, 0, got.Code)
	assert.Equal(t, "pending", got.Data.Status, "跨品牌 complete 不得改动会话")
}

func TestHandoff_URLUsesBrandBaseURL(t *testing.T) {
	skipIfNoConfig(t)
	h := createHandoff(t, BrandOverleap, SocialProviderApple)
	assert.True(t, strings.HasPrefix(h.Data.URL, "https://www.overleap.io/auth/social?session="), h.Data.URL)
	assert.Contains(t, h.Data.URL, "nonce=")
	assert.Contains(t, h.Data.URL, "provider=apple")
	assert.Len(t, handoffURLNonce(t, h.Data.URL), 64)
}
