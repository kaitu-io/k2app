package center

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// NOTE (A3): The former TestDedicatedLine_SoldQuotaGovernsCutoff_SurvivesProviderSync
// and its driveSlaveUsage helper were removed here. They asserted the OLD
// verdict-based /slave/usage cutoff (Center deciding serve/stop from CloudInstance
// quota), a model A3 deletes: /slave/usage is now a pure recorder writing NodeUsage
// and the node itself is the cutoff authority. The provider-sync-skip guard those
// steps exercised is independently covered by upsertCloudInstance tests. A4/A5
// re-establish the over-quota exclusion regression against NodeUsage at the
// /api/tunnels and /api/subs read paths.

// dedLineUser creates a user at the given app tier with the given shared
// membership expiry, registering cleanup of the user, its devices, and any
// private subs it accrues.
func dedLineUser(t *testing.T, tier string, sharedExpiredAt int64) *User {
	t.Helper()
	u := &User{UUID: "dedline-" + time.Now().Format("150405.000000000"), Tier: tier, ExpiredAt: sharedExpiredAt}
	require.NoError(t, db.Get().Create(u).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&Device{})
		db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&PrivateNodeSubscription{})
		db.Get().Unscoped().Delete(u)
	})
	return u
}

// TestDedicatedLine_EntitlementDecouple_FullPath proves the router-gating path
// is governed by active-line ownership, NOT app tier. It exercises the real
// gin middleware chain (RouterRequired) plus the real mint handler.
//
// Positive: a LITE-tier user (TierQuotas[lite].MaxRouterDevice == 0 under the
// old coupling) who owns an active private line:
//   - mints a gateway credential successfully (no ErrorPlanNoRouter), and
//   - passes RouterRequired and gets MaxRouterDevice==1, MaxLanClient==-1 from
//     /api/router/quota.
//
// Negation: a lite-tier user with NO active line is rejected with the exact
// ErrorPlanNoRouter code at both the middleware and the mint handler.
func TestDedicatedLine_EntitlementDecouple_FullPath(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	// Sanity: lite is the lowest tier and grants zero routers under the OLD
	// tier-coupled model. If this assumption breaks, the test loses its point.
	require.Equal(t, 0, TierQuotas[TierLite].MaxRouterDevice,
		"前提：lite 档旧耦合下 MaxRouterDevice==0，持线脱钩才有意义")

	// runRouterChain runs RouterRequired() then api_router_quota over a gin
	// context carrying the user as the authenticated principal — the real
	// middleware + handler, in order.
	runRouterChain := func(t *testing.T, user *User) (*httptest.ResponseRecorder, *gin.Context) {
		t.Helper()
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/router/quota", nil)
		c.Set("authContext", &authContext{User: user, UserID: user.ID})
		RouterRequired()(c)
		if !c.IsAborted() {
			api_router_quota(c)
		}
		return w, c
	}

	t.Run("LiteTierWithActiveLine_Allowed", func(t *testing.T) {
		// lite + shared membership long expired — only the line should matter.
		u := dedLineUser(t, TierLite, time.Now().Add(-72*time.Hour).Unix())
		createActivePrivateNodeSub(t, u.ID)

		// (a) mint gateway credential succeeds.
		mc, mw := gatewayCredentialContext(t, u)
		api_gateway_credential(mc)
		require.Equal(t, http.StatusOK, mw.Code, "body=%s", mw.Body.String())
		code, data := parseJobResponse(t, mw.Body.Bytes())
		require.Equal(t, float64(0), code, "lite 档持线应铸造成功（非 ErrorPlanNoRouter/PaymentRequired）; body=%s", mw.Body.String())
		require.NotNil(t, data)
		url, _ := data["url"].(string)
		require.Contains(t, url, "k2subs://", "应返回 k2subs 凭证")

		// (b) /api/router/quota passes RouterRequired and returns the quota.
		w, c := runRouterChain(t, u)
		require.False(t, c.IsAborted(), "lite 持线应通过 RouterRequired; body=%s", w.Body.String())
		quota, err := ParseResponseData[routerQuotaResponse](w)
		require.NoError(t, err, "body=%s", w.Body.String())
		require.Equal(t, 1, quota.MaxRouterDevice, "一账号一路由器")
		require.Equal(t, -1, quota.MaxLanClient, "LAN 客户端无限")
	})

	t.Run("LiteTierNoLine_Rejected", func(t *testing.T) {
		u := dedLineUser(t, TierLite, time.Now().Add(30*24*time.Hour).Unix()) // 共享会员在期但无线

		// (a) mint rejected with ErrorPlanNoRouter.
		mc, mw := gatewayCredentialContext(t, u)
		api_gateway_credential(mc)
		code, _ := parseJobResponse(t, mw.Body.Bytes())
		require.Equal(t, float64(ErrorPlanNoRouter), code, "无线应被 ErrorPlanNoRouter 拒; body=%s", mw.Body.String())
		var minted int64
		db.Get().Model(&Device{}).Where("user_id = ? AND is_gateway = true", u.ID).Count(&minted)
		require.Equal(t, int64(0), minted, "拒绝时不应铸造路由器设备")

		// (b) RouterRequired aborts with the same exact code, quota handler never runs.
		w, c := runRouterChain(t, u)
		require.True(t, c.IsAborted(), "无线应在 RouterRequired 被拦截")
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.Equal(t, ErrorPlanNoRouter, ErrorCode(resp.Code), "RouterRequired 应返回 ErrorPlanNoRouter; body=%s", w.Body.String())
	})
}

// makeGatewaySubsRequest builds an authenticated GET /api/subs gin context for a
// real gateway device, using HTTP Basic Auth (udid:access_token) exactly as k2r
// would. The device token is minted via generateTokens and the Device row's
// TokenIssueAt is aligned to the JWT so handleJWTAuth resolves it.
func makeGatewaySubsRequest(t *testing.T, user *User, udid string) *httptest.ResponseRecorder {
	t.Helper()
	tokens, issuedAt, err := generateTokens(context.Background(), user.ID, udid, user.Roles)
	require.NoError(t, err)

	dev := Device{
		UDID:            udid,
		UserID:          user.ID,
		IsGateway:       true,
		AppPlatform:     "router",
		TokenIssueAt:    issuedAt.Unix(),
		TokenLastUsedAt: issuedAt.Unix(),
	}
	require.NoError(t, db.Get().Create(&dev).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&dev) })

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subs", nil)
	cred := base64.StdEncoding.EncodeToString([]byte(udid + ":" + tokens.AccessToken))
	c.Request.Header.Set("Authorization", "Basic "+cred)

	api_subs(c)
	return w
}

// makePrivateLine creates a private SlaveNode + a k2v5 SlaveTunnel on it + a
// CloudInstance (with the given used/total quota) + an active
// PrivateNodeSubscription linking owner→instance→node. Returns the node so the
// caller can assert on which tunnels appear. linkCI=false leaves the sub's
// CloudInstanceID nil (the "missing CloudInstance stays visible" case).
func makePrivateLine(t *testing.T, owner *User, ip, label string, used, total int64, linkCI bool) *SlaveNode {
	t.Helper()
	now := time.Now().Unix()
	db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
	db.Get().Unscoped().Where("ip_address = ?", ip).Delete(&CloudInstance{})

	node := SlaveNode{
		Ipv4: ip, SecretToken: "dedline-subs-" + label,
		Country: "JP", Region: "jp", Name: "dedline-subs-" + label,
		Class: NodeClassPrivate, PrivateOwnerUserID: &owner.ID,
	}
	require.NoError(t, db.Get().Create(&node).Error)

	tunnel := SlaveTunnel{
		Domain:    "dedline-" + label + "-" + time.Now().Format("150405.000000000") + ".example.com",
		Name:      "dedline-tunnel-" + label,
		Protocol:  TunnelProtocolK2V5,
		Port:      443,
		NodeID:    node.ID,
		IsTest:    BoolPtr(false),
		ServerURL: "k2v5://dedline-" + label + ".example.com:443?ech=x",
	}
	require.NoError(t, db.Get().Create(&tunnel).Error)

	ci := CloudInstance{
		Provider: "aws_lightsail", AccountName: "test-account",
		InstanceID: "dedline-subs-ci-" + label + "-" + time.Now().Format("150405.000000000"),
		Name:       "dedline-subs-ci-" + label, IPAddress: ip, Region: "jp",
		TrafficUsedBytes: used, TrafficTotalBytes: total, TrafficResetAt: now + 30*86400,
	}
	require.NoError(t, db.Get().Create(&ci).Error)

	sub := PrivateNodeSubscription{
		UserID: owner.ID, OrderID: owner.ID*1000 + uint64(now%1000) + uint64(node.ID),
		Status: PNStatusActive, Region: "jp", IPType: IPTypeNonResidential,
		TrafficTotalBytes: total, PurchasedAt: now, ExpiresAt: now + 86400,
		SlaveNodeID: &node.ID,
	}
	if linkCI {
		ciID := ci.ID
		sub.CloudInstanceID = &ciID
	}
	require.NoError(t, db.Get().Create(&sub).Error)

	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&sub)
		db.Get().Unscoped().Delete(&ci)
		db.Get().Unscoped().Delete(&tunnel)
		db.Get().Unscoped().Delete(&node)
	})
	return &node
}

// TestDedicatedLine_SubsExcludesOverQuotaLine drives the real GET /api/subs
// gateway path (Basic Auth → handleJWTAuth → IsGateway branch →
// ResolveGatewayPrivateTunnels) and asserts the handler's actual JSON output:
//   - the over-quota line's tunnel is EXCLUDED, and
//   - the healthy line's tunnel IS present, and
//   - a line whose sub has a nil CloudInstance stays visible (no quota = no
//     exclusion, per the Task 2 spec).
func TestDedicatedLine_SubsExcludesOverQuotaLine(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	owner := CreateTestUser(t)

	const total = int64(2) << 40
	// Healthy: 10% used. Over-quota: 100% used (= node hard-cut point). NoCI: linkCI=false.
	healthy := makePrivateLine(t, owner, "203.0.114.81", "healthy", int64(200)<<30, total, true)
	over := makePrivateLine(t, owner, "203.0.114.82", "over", total, total, true) // 100% exhausted
	noci := makePrivateLine(t, owner, "203.0.114.83", "noci", int64(1968)<<30, total, false)

	w := makeGatewaySubsRequest(t, owner, "router-dedline-subs")
	require.Equal(t, http.StatusOK, w.Code, "gateway subs 应 200; body=%s", w.Body.String())

	var resp SubsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%s", w.Body.String())

	// Collect the server URLs present (creds are injected, so match on the
	// stable domain substring).
	present := func(domainFrag string) bool {
		for _, tn := range resp.Tunnels {
			if bytes.Contains([]byte(tn.URL), []byte(domainFrag)) {
				return true
			}
		}
		return false
	}

	require.True(t, present("dedline-healthy.example.com"), "健康线应出现; tunnels=%+v", resp.Tunnels)
	require.False(t, present("dedline-over.example.com"), "超配额线应被剔除; tunnels=%+v", resp.Tunnels)
	require.True(t, present("dedline-noci.example.com"), "无 CloudInstance 的线应可见（无配额=不剔除）; tunnels=%+v", resp.Tunnels)
	require.Equal(t, 2, len(resp.Tunnels), "应只剩健康线 + 无配额线两条; tunnels=%+v", resp.Tunnels)

	// Silence unused warnings for the returned nodes (kept for readability).
	_ = healthy
	_ = over
	_ = noci
}
