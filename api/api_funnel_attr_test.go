package center

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// faTestIP：每个测试一个独立的公网 IPv6 /64，避免与别的测试在共享 miniredis 里串号。
func faTestIP() string { return faTestPrefix() + "::1" }

func faTestPrefix() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("2001:db8:%x:%x", r.Intn(0xffff), r.Intn(0xffff))
}

func faClickKeys(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, k := range testMiniRedis.Keys() {
		if strings.HasPrefix(k, "fattr:c:") {
			out = append(out, k)
		}
	}
	return out
}

func faPxClick(m, ip, platform string) *TestRequest {
	return NewTestRequest("GET", "/api/px?e=install_click&p="+platform+"&s="+m+"&u=/zh-CN/install").
		WithHeader("User-Agent", pxDesktopUA).WithHeader("X-Forwarded-For", ip)
}

// faExec 按生产链路的形态发请求：TCP 对端是代理（ALB），访客 IP 在 X-Forwarded-For 里。
// TestRequest.Execute 不设 RemoteAddr，gin 的 ClientIP() 会因此返回空。
func faExec(r *TestRequest, router http.Handler) *httptest.ResponseRecorder {
	req, err := r.Build()
	if err != nil {
		panic(err)
	}
	req.RemoteAddr = "172.31.32.50:40000"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// GPC / optout 的点击一个出口哈希都不写；普通点击写一个。
func TestPx_InstallClick_AttributionRespectsConsent(t *testing.T) {
	skipIfNoConfig(t)
	pxMarker(t)
	ip := faTestIP()
	before := len(faClickKeys(t))

	faExec(faPxClick("tfa", ip, "ios").WithHeader("Sec-GPC", "1"), pxRouter())
	assert.Len(t, faClickKeys(t), before, "GPC click must not be recorded")
	faExec(faPxClick("tfa", ip, "ios").WithCookie(CookieFunnelSid, funnelSidOptOut), pxRouter())
	assert.Len(t, faClickKeys(t), before, "opted-out click must not be recorded")
	faExec(pxReq("tfa", "install_view").WithHeader("X-Forwarded-For", ip), pxRouter())
	assert.Len(t, faClickKeys(t), before, "only install_click is recorded")

	faExec(faPxClick("tfa", ip, "ios"), pxRouter())
	assert.Len(t, faClickKeys(t), before+1)
}

// 端到端：网页点下载（macos）→ 同出口 App 首开 → 写入推断关联；不同出口 / 不同平台不写。
func TestStatsIngest_FirstOpen_WritesInferredLink(t *testing.T) {
	skipIfNoConfig(t)
	pxMarker(t)
	prefix := faTestPrefix()
	ip := prefix + "::1"
	w := faExec(faPxClick("tfa", ip, "macos"), pxRouter())
	sid := sidFromSetCookie(w)
	require.NotEmpty(t, sid)

	m := sfMarker(t)
	cases := []struct {
		name, ip, os string
		want         bool
	}{
		{"same /64 (rotated temp address) + platform", prefix + ":abcd:1234:5678:9abc", "macos", true},
		{"other egress", faTestIP(), "macos", false},
		{"other platform", ip, "windows", false},
	}
	for _, tc := range cases {
		did := sfHash(m + tc.name)
		t.Cleanup(func() { db.Get().Where("did = ?", did).Delete(&FunnelInferredLink{}) })
		it := sfItem(m, did, "app_first_open")
		it["os"] = tc.os
		faExec(sfPost(it).WithHeader("X-Forwarded-For", tc.ip), statsFunnelRouter())

		var link FunnelInferredLink
		found := false
		for i := 0; i < 40 && !found; i++ { // 插入是异步的
			found = db.Get().Where("did = ?", did).First(&link).Error == nil
			if !found {
				time.Sleep(50 * time.Millisecond)
			}
		}
		assert.Equal(t, tc.want, found, tc.name)
		if tc.want && found {
			assert.Equal(t, sid, link.SID)
			assert.Equal(t, "macos", link.Platform)
			assert.Equal(t, funnelAttrMethod, link.Method)
			assert.Equal(t, string(BrandKaitu), link.Brand)
		}
	}
	// 推断关联绝不进 funnel_identities。
	var n int64
	require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("anon_id = ?", sid).Count(&n).Error)
	assert.Zero(t, n)
}
