package center

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// subsTunnelFixture：有效会员 App 用户 + App 设备 + 一个共享 k2v5 隧道。
func subsTunnelFixture(t *testing.T, tag string) (*User, *Device, string) {
	t.Helper()
	now := time.Now().Unix()
	uniq := tag + "-" + time.Now().Format("20060102150405.000000")

	user := &User{UUID: "usr-subs-tt-" + uniq, ExpiredAt: now + 86400}
	require.NoError(t, db.Get().Create(user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(user) })

	device := &Device{UDID: "udid-subs-tt-" + uniq, UserID: user.ID, TokenIssueAt: now}
	require.NoError(t, db.Get().Create(device).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(device) })

	domain := "subs-tt-" + uniq + ".example"
	node := &SlaveNode{
		Ipv4: "10.99.21.1", SecretToken: "stt-s-" + uniq, Country: "JP", Region: "japan",
		Name: "subs-tt-" + uniq, Class: NodeClassShared,
	}
	db.Get().Unscoped().Where("ipv4 = ?", node.Ipv4).Delete(&SlaveNode{})
	require.NoError(t, db.Get().Create(node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(node) })

	tun := &SlaveTunnel{
		Domain: domain, SecretToken: "stt-t-" + uniq, Name: "subs-tt-tun-" + uniq,
		Protocol: TunnelProtocolK2V5, Port: 443, NodeID: node.ID,
		IsTest: BoolPtr(false), ServerURL: "k2v5://" + domain + ":443",
	}
	require.NoError(t, db.Get().Create(tun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(tun) })

	return user, device, domain
}

// subsGet 以 Basic udid:token 驱动真实 handler，返回 recorder。
func subsGet(t *testing.T, udid, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.GET("/api/subs", api_subs)
	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(udid+":"+token)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// urlTokenForDomain 从 subs 响应里挑出指定 domain 的 tunnel URL 并解出 password。
func urlTokenForDomain(t *testing.T, body []byte, domain string) string {
	t.Helper()
	var resp struct {
		Tunnels []struct {
			URL string `json:"url"`
		} `json:"tunnels"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	for _, tn := range resp.Tunnels {
		if !strings.Contains(tn.URL, domain) {
			continue
		}
		u, err := url.Parse(tn.URL)
		require.NoError(t, err)
		require.NotNil(t, u.User, "tunnel URL 必须携带 userinfo")
		pass, _ := u.User.Password()
		return pass
	}
	t.Fatalf("domain %s not found in response: %s", domain, string(body))
	return ""
}

func claimsOf(t *testing.T, token string) *TokenClaims {
	t.Helper()
	var claims TokenClaims
	_, _, err := jwt.NewParser().ParseUnverified(token, &claims)
	require.NoError(t, err)
	return &claims
}

// TestApiSubs_AccessToken_ConvertsToTunnelToken：存量 access token 请求 →
// 响应 URL 内嵌的是全新 tunnel token（P1 自愈的转换点）。
func TestApiSubs_AccessToken_ConvertsToTunnelToken(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	user, device, domain := subsTunnelFixture(t, "conv")
	access := GenerateTestToken(user.ID, device.UDID, time.Hour)
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("token_issue_at", tokenIssueAtOf(t, access)).Error)

	w := subsGet(t, device.UDID, access)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	got := urlTokenForDomain(t, w.Body.Bytes(), domain)
	require.NotEqual(t, access, got, "URL 内不得再回填 access token")
	require.Equal(t, TokenTypeTunnel, claimsOf(t, got).Type)
	// 转换出的 token 必须真实可用。
	_, _, err := validateTunnelToken(nil, got)
	require.NoError(t, err)
}

// TestApiSubs_TunnelToken_FreshNoRenew_StaleRenews：tunnel token 请求的
// 滚动续期门（>50% 剩余不动、<50% 换新且锚点不变）。
func TestApiSubs_TunnelToken_FreshNoRenew_StaleRenews(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	user, device, domain := subsTunnelFixture(t, "renew")
	_ = user
	anchor := time.Now().Unix()
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("tunnel_issue_at", anchor).Error)
	device.TunnelIssueAt = anchor

	t.Run("Fresh_SameTokenBack", func(t *testing.T) {
		fresh := generateTestTunnelToken(t, device, 89*24*time.Hour) // 剩 ~89 天
		w := subsGet(t, device.UDID, fresh)
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
		require.Equal(t, fresh, urlTokenForDomain(t, w.Body.Bytes(), domain), "剩余 >50% 不得续期")
	})

	t.Run("Stale_NewTokenBack_AnchorKept", func(t *testing.T) {
		stale := generateTestTunnelToken(t, device, 30*24*time.Hour) // 剩 30 天 < 45 天
		w := subsGet(t, device.UDID, stale)
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
		got := urlTokenForDomain(t, w.Body.Bytes(), domain)
		require.NotEqual(t, stale, got, "剩余 <50% 必须续期")
		nc := claimsOf(t, got)
		require.Equal(t, TokenTypeTunnel, nc.Type)
		require.Equal(t, anchor, nc.TokenIssueAt, "续期不得重置吊销锚点")
	})

	t.Run("InvalidTunnelToken_401", func(t *testing.T) {
		revoked := generateTestTunnelToken(t, device, time.Hour)
		require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
			Update("tunnel_issue_at", anchor+1).Error)
		t.Cleanup(func() {
			db.Get().Model(&Device{}).Where("id = ?", device.ID).Update("tunnel_issue_at", anchor)
		})
		w := subsGet(t, device.UDID, revoked)
		require.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

// TestSubsAuthenticate_CredType：credType 分类是 spec §11 采纳率出口判据的
// 数据源——tunnel 请求必须归类 tunnel、access 请求必须归类 access（这条分类
// 同时写进 subsAuthenticate 的 INFO 日志供部署后 grep 计算采纳率）。直接驱动
// subsAuthenticate（锚定 CredType 这道分类门本身，不锚定下游）。
func TestSubsAuthenticate_CredType(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	user, device, _ := subsTunnelFixture(t, "credtype")
	anchor := time.Now().Unix()
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("tunnel_issue_at", anchor).Error)
	device.TunnelIssueAt = anchor

	newCtx := func() *gin.Context {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/subs", nil)
		return c
	}

	t.Run("tunnel", func(t *testing.T) {
		auth := subsAuthenticate(newCtx(), generateTestTunnelToken(t, device, time.Hour))
		require.NotNil(t, auth)
		require.Equal(t, "tunnel", auth.CredType)
	})

	t.Run("access", func(t *testing.T) {
		access := GenerateTestToken(user.ID, device.UDID, time.Hour)
		require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
			Update("token_issue_at", tokenIssueAtOf(t, access)).Error)
		auth := subsAuthenticate(newCtx(), access)
		require.NotNil(t, auth)
		require.Equal(t, "access", auth.CredType)
	})
}
