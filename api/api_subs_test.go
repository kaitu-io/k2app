package center

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// =====================================================================
// TestExtractSubsBasicAuth — table-driven unit tests for Basic Auth parsing
// =====================================================================

func TestExtractSubsBasicAuth(t *testing.T) {
	cases := []struct {
		name      string
		header    string
		wantOK    bool
		wantUDID  string
		wantToken string
	}{
		{
			name:      "valid credentials",
			header:    "Basic " + base64.StdEncoding.EncodeToString([]byte("myudid:mytoken")),
			wantOK:    true,
			wantUDID:  "myudid",
			wantToken: "mytoken",
		},
		{
			name:   "Bearer token (not Basic)",
			header: "Bearer sometoken",
			wantOK: false,
		},
		{
			name:   "empty header",
			header: "",
			wantOK: false,
		},
		{
			name:   "no colon in payload",
			header: "Basic " + base64.StdEncoding.EncodeToString([]byte("nocolonhere")),
			wantOK: false,
		},
		{
			name:   "empty password (user:)",
			header: "Basic " + base64.StdEncoding.EncodeToString([]byte("myudid:")),
			wantOK: false,
		},
		{
			name:   "empty username (:token)",
			header: "Basic " + base64.StdEncoding.EncodeToString([]byte(":mytoken")),
			wantOK: false,
		},
		{
			name:      "colon in token is ok (udid:tok:en)",
			header:    "Basic " + base64.StdEncoding.EncodeToString([]byte("myudid:tok:en")),
			wantOK:    true,
			wantUDID:  "myudid",
			wantToken: "tok:en",
		},
	}

	gin.SetMode(gin.TestMode)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request, _ = http.NewRequest("GET", "/", nil)
			if tc.header != "" {
				c.Request.Header.Set("Authorization", tc.header)
			}

			udid, token, ok := extractSubsBasicAuth(c)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantUDID, udid)
				assert.Equal(t, tc.wantToken, token)
			}
		})
	}
}

// =====================================================================
// TestInjectSubsCreds — table-driven unit tests for credential injection
// =====================================================================

func TestInjectSubsCreds(t *testing.T) {
	cases := []struct {
		name      string
		serverURL string
		udid      string
		token     string
		want      string
	}{
		{
			name:      "k2v5 URL injects credentials before host",
			serverURL: "k2v5://host.example.com:443?ech=x",
			udid:      "myudid",
			token:     "mytoken",
			want:      "k2v5://myudid:mytoken@host.example.com:443?ech=x",
		},
		{
			name:      "no scheme separator passes through unchanged",
			serverURL: "host.example.com:443",
			udid:      "myudid",
			token:     "mytoken",
			want:      "host.example.com:443",
		},
		{
			name:      "empty string passes through unchanged",
			serverURL: "",
			udid:      "myudid",
			token:     "mytoken",
			want:      "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := injectSubsCreds(tc.serverURL, tc.udid, tc.token)
			assert.Equal(t, tc.want, got)
		})
	}
}

// =====================================================================
// TestApiSubs_NoAuth_Returns401 — handler test: missing auth → raw HTTP 401
//
// /api/subs is an external-protocol (k2subs://) wire endpoint. Unlike the rest
// of /api/*, it returns real HTTP status codes with plain-text body hints,
// NOT the {code, message, data} envelope — daemon (k2/config/subscription.go)
// formats errors as `subscription fetch: status %d: %s` using the body as hint.
// =====================================================================

func TestApiSubs_NoAuth_Returns401(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/api/subs", api_subs)

	req, err := http.NewRequest("GET", "/api/subs", nil)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "missing credentials")

	// Body must NOT be JSON-wrapped in the Center {code, message, data} envelope.
	// Daemon parses the body as a plain hint string.
	assert.NotContains(t, w.Body.String(), `"code":`)
	assert.NotContains(t, w.Body.String(), `"data":`)
}

// =====================================================================
// TestApiSubs_MalformedAuth_ReturnsRaw401 — malformed Basic Auth → raw 401
// =====================================================================

func TestApiSubs_MalformedAuth_ReturnsRaw401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/subs", api_subs)

	cases := []struct {
		name   string
		header string
	}{
		{"bearer instead of basic", "Bearer abc"},
		{"empty password", "Basic " + base64.StdEncoding.EncodeToString([]byte("udid:"))},
		{"garbage base64", "Basic !!!not-base64!!!"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api/subs", nil)
			req.Header.Set("Authorization", tc.header)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "missing credentials")
			// No JSON envelope leak.
			assert.NotContains(t, w.Body.String(), `"code"`)
		})
	}
}

// =====================================================================
// TestWriteSubsOK — response framing (Cache-Control + JSON shape)
//
// writeSubsOK is the single path for success responses. These tests assert
// the wire contract end-to-end on the struct-to-JSON path: Cache-Control
// header, raw (no envelope) body, and both the new recommendScore field and
// the legacy weight field present for a release-cycle's worth of backward
// compatibility.
// =====================================================================

func TestWriteSubsOK_SetsCacheControlHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		writeSubsOK(c, SubsResponse{
			Tunnels: []SubsTunnel{{URL: "k2v5://x", Weight: 50, RecommendScore: 0.5}},
			Refresh: 1800,
		})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-store, private", w.Header().Get("Cache-Control"),
		"successful /api/subs responses must disable caching end-to-end")
	assert.Contains(t, w.Body.String(), `"url":"k2v5://x"`)
}

func TestSubsResponse_JSONShapeIncludesRecommendScoreAndWeight(t *testing.T) {
	// The SubsTunnel struct must serialize both fields for one release cycle:
	// new daemons read recommendScore, pre-release daemons still see weight.
	resp := SubsResponse{
		Tunnels: []SubsTunnel{
			{URL: "k2v5://a", Weight: 75, RecommendScore: 0.75},
			{URL: "k2v5://b", Weight: 50, RecommendScore: 0.5},
		},
		Refresh: 1800,
	}

	var decoded map[string]any
	body, err := json.Marshal(resp)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &decoded))

	tunnels, ok := decoded["tunnels"].([]any)
	require.True(t, ok)
	require.Len(t, tunnels, 2)

	first := tunnels[0].(map[string]any)
	assert.Equal(t, "k2v5://a", first["url"])
	assert.Equal(t, float64(75), first["weight"], "legacy weight int must be present")
	assert.InDelta(t, 0.75, first["recommendScore"], 1e-9, "recommendScore float must be present")
}

func TestSubsTunnel_LegacyWeightDerivedFromScore(t *testing.T) {
	// Contract: Weight = round(RecommendScore * subsLegacyWeightScale). This is
	// the only invariant backward-compat depends on — any handler that populates
	// SubsTunnel must honor it. This is a shape test against that invariant on
	// a hand-rolled tunnel list, not a live handler integration (which would
	// need DB mocks).
	cases := []struct {
		score      float64
		wantWeight int
	}{
		{0.0, 0},
		{0.25, 25},
		{0.5, 50},
		{0.75, 75},
		{1.0, 100},
		{0.456, 46},
	}

	for _, tc := range cases {
		got := int(math.Round(tc.score * subsLegacyWeightScale))
		assert.Equal(t, tc.wantWeight, got,
			"score=%v must project to weight=%d", tc.score, tc.wantWeight)
	}
}

// =====================================================================
// TestApiSubs_GatewayBranch_PrecedesSharedMembershipGate — ORDERING LOCK
//
// Regression test for the ordering bug fixed by moving the gateway branch
// ABOVE the shared-membership IsExpired() 402 gate. Private nodes use an
// independent clock (per-node PrivateNodeSubscription) and MUST NOT depend on
// shared membership. A router (gateway) user whose SHARED membership has lapsed
// (User.ExpiredAt in the past) but who holds an ACTIVE private-node subscription
// must still receive their private tunnels (HTTP 200), NOT a 402.
//
// If the gateway branch ever moves back below the IsExpired() gate, this test
// fails: the expired-shared-membership user would hit the 402 "membership
// expired" path before the gateway branch could resolve their private tunnels.
//
// Drives the real route handler (api_subs) over HTTP Basic Auth, exactly as
// the daemon does. Needs dev MySQL.
// =====================================================================

func TestApiSubs_GatewayBranch_PrecedesSharedMembershipGate(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000")

	// Gateway owner with EXPIRED shared membership (ExpiredAt in the past).
	owner := User{UUID: "usr-subs-gw-" + uniq, ExpiredAt: now - 99999}
	require.NoError(t, db.Get().Create(&owner).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&owner) })

	// Gateway device — created directly so IsGateway=true (CreateTestDevice
	// can't set it). TokenIssueAt must match the minted token exactly or
	// handleJWTAuth rejects on the TokenIssueAt check.
	udid := "udid-subs-gw-" + uniq
	device := Device{
		UDID:         udid,
		UserID:       owner.ID,
		Remark:       "Test Gateway Device",
		IsGateway:    true,
		TokenIssueAt: now,
	}
	require.NoError(t, db.Get().Create(&device).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&device) })

	// Mint a token whose TokenIssueAt == device.TokenIssueAt (== now).
	// GenerateTestToken sets TokenIssueAt = time.Now().Unix(); pin device to
	// the same second above by overwriting the row's TokenIssueAt afterward.
	token := GenerateTestToken(owner.ID, udid, time.Hour)
	// Decode the token's iat to keep device in sync regardless of second-rollover.
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("token_issue_at", tokenIssueAtOf(t, token)).Error)

	// Private node owned by the gateway user + a k2v5 tunnel on it.
	priv := SlaveNode{
		Ipv4: "10.99.8.1", SecretToken: "subs-s1", Country: "JP", Region: "japan",
		Name: "subs-priv-jp-" + uniq, Class: NodeClassPrivate, PrivateOwnerUserID: &owner.ID,
	}
	require.NoError(t, db.Get().Create(&priv).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&priv) })

	tun := SlaveTunnel{
		Domain: "subs-priv-jp.example", SecretToken: "subs-tt1", Name: "subs-priv-jp-tun-" + uniq,
		Protocol: TunnelProtocolK2V5, Port: 443, NodeID: priv.ID,
		IsTest: BoolPtr(false), ServerURL: "k2v5://subs-priv-jp.example:443",
	}
	require.NoError(t, db.Get().Create(&tun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(udid+":"+token))

	r := gin.New()
	r.GET("/api/subs", api_subs)

	// --- Path 1: ACTIVE private sub + EXPIRED shared membership → HTTP 200 ---
	sub := PrivateNodeSubscription{
		UserID: owner.ID, OrderID: owner.ID, Status: PNStatusActive, Region: "japan",
		IPType: IPTypeNonResidential, SlaveNodeID: &priv.ID,
		PurchasedAt: now, ExpiresAt: now + 86400,
	}
	require.NoError(t, db.Get().Create(&sub).Error)

	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", authHeader)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code,
		"gateway user with active private sub but expired shared membership must get 200, body=%s", w.Body.String())
	assert.NotContains(t, w.Body.String(), "membership expired",
		"gateway branch must precede the shared-membership 402 gate")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	tunnels, ok := resp["tunnels"].([]any)
	require.True(t, ok, "response must contain a tunnels array, body=%s", w.Body.String())
	assert.NotEmpty(t, tunnels, "private tunnels must be non-empty for entitled gateway user")

	// --- Path 2: same expired gateway user, NO private sub → 402 (no entitlement) ---
	require.NoError(t, db.Get().Unscoped().Delete(&sub).Error)

	req2, _ := http.NewRequest("GET", "/api/subs", nil)
	req2.Header.Set("Authorization", authHeader)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusPaymentRequired, w2.Code,
		"gateway user with no private entitlement must get 402, body=%s", w2.Body.String())
	assert.Contains(t, w2.Body.String(), "no private node entitlement",
		"402 must come from the gateway no-entitlement path, not the shared-membership gate")
}

// =====================================================================
// TestApiSubs_SharedPool_ExcludesPrivateNodes — CAPABILITY MATRIX (App→private ❌)
//
// A non-gateway (App/desktop) user must NEVER see private nodes in their shared
// pool. Private nodes belong to a single owner and are reachable only through the
// gateway branch (ResolveGatewayPrivateTunnels). If the shared-pool path failed
// to exclude Class=private nodes, one user's dedicated VPS (IP, country) would
// leak into every App user's server list — breaking the matrix and privacy.
//
// Drives the real route handler over HTTP Basic Auth with a non-gateway device.
// Needs dev MySQL.
// =====================================================================

func TestApiSubs_SharedPool_ExcludesPrivateNodes(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000")

	// Non-gateway App user with VALID shared membership.
	user := User{UUID: "usr-subs-app-" + uniq, ExpiredAt: now + 86400}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	udid := "udid-subs-app-" + uniq
	device := Device{
		UDID:         udid,
		UserID:       user.ID,
		Remark:       "Test App Device",
		IsGateway:    false, // App/desktop, NOT a router
		TokenIssueAt: now,
	}
	require.NoError(t, db.Get().Create(&device).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&device) })

	token := GenerateTestToken(user.ID, udid, time.Hour)
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("token_issue_at", tokenIssueAtOf(t, token)).Error)

	// A private node (owned by this same user — even self-owned private nodes must
	// not appear in the shared pool) + a k2v5 tunnel on it with a unique domain.
	privDomain := "leak-priv-" + uniq + ".example"
	priv := SlaveNode{
		Ipv4: "10.99.9.1", SecretToken: "leak-s1", Country: "JP", Region: "japan",
		Name: "leak-priv-jp-" + uniq, Class: NodeClassPrivate, PrivateOwnerUserID: &user.ID,
	}
	require.NoError(t, db.Get().Create(&priv).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&priv) })

	tun := SlaveTunnel{
		Domain: privDomain, SecretToken: "leak-tt1", Name: "leak-priv-jp-tun-" + uniq,
		Protocol: TunnelProtocolK2V5, Port: 443, NodeID: priv.ID,
		IsTest: BoolPtr(false), ServerURL: "k2v5://" + privDomain + ":443",
	}
	require.NoError(t, db.Get().Create(&tun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(udid+":"+token))
	r := gin.New()
	r.GET("/api/subs", api_subs)

	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", authHeader)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"non-gateway user with valid membership must get 200, body=%s", w.Body.String())
	assert.NotContains(t, w.Body.String(), privDomain,
		"shared pool must NOT leak the private node (App→private ❌)")
}

// =====================================================================
// TestApiSubs_DeviceClassCrossCheck_* — CAPABILITY MATRIX (router header × is_gateway)
//
// /api/subs must reject a self-identified router (X-K2-Client: kaitu-router)
// riding an App-class credential (is_gateway=false). A stock k2r ALWAYS sends
// kaitu-router; without this check a hand-crafted k2subs:// URL carrying an App
// token routes an entire LAN through the App-only shared pool, bypassing the
// dedicated-line gate. EnforceDeviceClass enforces this on /api/tunnels but is
// NOT mounted on /api/subs (Basic auth happens inside the handler, so the
// middleware has no auth context), hence the check is inlined in api_subs.
// Needs dev MySQL.
// =====================================================================

// subsTestDevice seeds a user + device (gateway or app) with a matching token,
// returning the udid and a valid Basic-auth header. Mirrors the inline setup in
// the integration tests above. Needs dev MySQL.
func subsTestDevice(t *testing.T, isGateway bool) (udid, authHeader string) {
	t.Helper()
	now := time.Now().Unix()
	cls := "app"
	if isGateway {
		cls = "gw"
	}
	uniq := cls + "-" + time.Now().Format("20060102150405.000000")
	user := User{UUID: "usr-subs-cc-" + uniq, ExpiredAt: now + 86400}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	udid = "udid-subs-cc-" + uniq
	device := Device{UDID: udid, UserID: user.ID, Remark: "cc-test", IsGateway: isGateway, TokenIssueAt: now}
	require.NoError(t, db.Get().Create(&device).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&device) })

	token := GenerateTestToken(user.ID, udid, time.Hour)
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("token_issue_at", tokenIssueAtOf(t, token)).Error)
	authHeader = "Basic " + base64.StdEncoding.EncodeToString([]byte(udid+":"+token))
	return udid, authHeader
}

const subsRouterHeader = "kaitu-router/0.4.5 (linux; mips; OpenWrt 19.07; gl-xe300)"

// THE bypass this fix closes: App credential + router header → must be 403.
func TestApiSubs_DeviceClassCrossCheck_RouterHeaderOnAppDevice_403(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	_, authHeader := subsTestDevice(t, false) // App/desktop device
	r := gin.New()
	r.GET("/api/subs", api_subs)

	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("X-K2-Client", subsRouterHeader)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code,
		"router header on App device must be 403 (shared pool is App-only), body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "device class mismatch")
}

// No regression: App device + service header → cross-check passes → 200.
func TestApiSubs_DeviceClassCrossCheck_ServiceHeaderOnAppDevice_OK(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	_, authHeader := subsTestDevice(t, false)
	r := gin.New()
	r.GET("/api/subs", api_subs)

	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("X-K2-Client", "kaitu-service/0.4.5 (macos; arm64)")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code,
		"service header on App device must pass the cross-check, body=%s", w.Body.String())
}

// A legitimate router (gateway device + router header) must NOT be blocked by
// the cross-check — it proceeds to the gateway branch (402 here: no private node).
func TestApiSubs_DeviceClassCrossCheck_RouterHeaderOnGatewayDevice_NotBlocked(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	_, authHeader := subsTestDevice(t, true) // Gateway device, no private node
	r := gin.New()
	r.GET("/api/subs", api_subs)

	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("X-K2-Client", subsRouterHeader)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusPaymentRequired, w.Code,
		"matched router on gateway device must reach the gateway branch (402), body=%s", w.Body.String())
	assert.NotContains(t, w.Body.String(), "device class mismatch")
}

// A present-but-malformed client header is rejected (mirrors EnforceDeviceClass).
// A MISSING header still passes (backward compat for pre-header clients).
func TestApiSubs_DeviceClassCrossCheck_MalformedHeader_400(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	_, authHeader := subsTestDevice(t, false)
	r := gin.New()
	r.GET("/api/subs", api_subs)

	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("X-K2-Client", "totally-not-valid")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code,
		"malformed client header must be 400, body=%s", w.Body.String())
}

// tokenIssueAtOf decodes the JWT (without verification) and returns its
// TokenIssueAt claim, so the seeded Device row can match handleJWTAuth's check
// regardless of any second-rollover between minting and device creation.
func tokenIssueAtOf(t *testing.T, token string) int64 {
	t.Helper()
	parts := splitJWT(token)
	require.Len(t, parts, 3, "token must have 3 JWT segments")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims struct {
		TokenIssueAt int64 `json:"token_issue_at"`
	}
	require.NoError(t, json.Unmarshal(payload, &claims))
	require.NotZero(t, claims.TokenIssueAt, "token_issue_at claim must be present")
	return claims.TokenIssueAt
}

func splitJWT(token string) []string {
	out := make([]string, 0, 3)
	start := 0
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			out = append(out, token[start:i])
			start = i + 1
		}
	}
	out = append(out, token[start:])
	return out
}

// =====================================================================
// TestSubs*ControlKeyHash — /api/subs carries control_key_hash for k2r
// (Task B3, spec §4 mint-on-serve). Gateway branch mints the account's
// router control key on first serve if absent; shared branch only reads,
// never mints.
// =====================================================================

// userIDForUDID resolves the UserID of the Device row seeded by
// subsTestDevice, so callers can drive EnsureRouterControlKey / DB
// assertions against the same account the Basic-Auth request maps to.
func userIDForUDID(t *testing.T, udid string) uint64 {
	t.Helper()
	var dev Device
	require.NoError(t, db.Get().Where("udid = ?", udid).First(&dev).Error)
	return dev.UserID
}

// seedGatewayPrivateLine gives userID exactly one serviceable private node +
// k2v5 tunnel, mirroring the minimal setup in
// TestApiSubs_GatewayBranch_PrecedesSharedMembershipGate — just enough for
// the gateway branch to reach writeSubsOK instead of the no-entitlement 402.
func seedGatewayPrivateLine(t *testing.T, userID uint64) {
	t.Helper()
	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000000")

	priv := SlaveNode{
		Ipv4: "10.99.7." + uniq[len(uniq)-2:], SecretToken: "cks-" + uniq, Country: "JP", Region: "japan",
		Name: "cks-priv-jp-" + uniq, Class: NodeClassPrivate, PrivateOwnerUserID: &userID,
	}
	require.NoError(t, db.Get().Create(&priv).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&priv) })

	tun := SlaveTunnel{
		Domain: "cks-priv-jp-" + uniq + ".example", SecretToken: "cks-tt-" + uniq, Name: "cks-priv-jp-tun-" + uniq,
		Protocol: TunnelProtocolK2V5, Port: 443, NodeID: priv.ID,
		IsTest: BoolPtr(false), ServerURL: "k2v5://cks-priv-jp-" + uniq + ".example:443",
	}
	require.NoError(t, db.Get().Create(&tun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })

	sub := PrivateNodeSubscription{
		UserID: userID, OrderID: userID*1000 + uint64(now%1000), Status: PNStatusActive, Region: "japan",
		IPType: IPTypeNonResidential, SlaveNodeID: &priv.ID,
		PurchasedAt: now, ExpiresAt: now + 86400,
	}
	require.NoError(t, db.Get().Create(&sub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&sub) })
}

// TestSubsResponseCarriesControlKeyHash: an account that already has a
// router control key gets its sha256 hash injected on the gateway branch,
// and the raw-JSON contract (no {code,message,data} envelope) stays intact.
func TestSubsResponseCarriesControlKeyHash(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	udid, authHeader := subsTestDevice(t, true) // gateway device
	userID := userIDForUDID(t, udid)
	seedGatewayPrivateLine(t, userID)

	key, err := EnsureRouterControlKey(context.Background(), userID)
	require.NoError(t, err)

	r := gin.New()
	r.GET("/api/subs", api_subs)
	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", authHeader)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "raw JSON contract broken: body=%s", w.Body.String())

	got, _ := resp["control_key_hash"].(string)
	assert.Equal(t, HashRouterControlKey(key), got, "control_key_hash must be sha256 of the account's key")

	_, hasCode := resp["code"]
	assert.False(t, hasCode, "subs response must stay envelope-free")
}

// TestSubsGatewayBranchMintsKeyWhenAbsent: mint-on-serve (spec §4) — a
// gateway request for an account with NO router control key yet must mint
// one via EnsureRouterControlKey and serve its hash, closing the TOFU
// window for pre-existing k2subs routers that never open a fresh app.
func TestSubsGatewayBranchMintsKeyWhenAbsent(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	udid, authHeader := subsTestDevice(t, true) // gateway device, no key minted
	userID := userIDForUDID(t, udid)
	seedGatewayPrivateLine(t, userID)

	r := gin.New()
	r.GET("/api/subs", api_subs)
	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", authHeader)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	got, _ := resp["control_key_hash"].(string)
	require.NotEmpty(t, got, "gateway branch must mint-on-serve: control_key_hash missing for fresh account")

	var u User
	require.NoError(t, db.Get().First(&u, userID).Error)
	require.NotNil(t, u.RouterControlKey, "minted key must be persisted")
	assert.Equal(t, HashRouterControlKey(*u.RouterControlKey), got, "served hash must match the persisted key")
}

// TestSubsSharedBranchDoesNotMint: the shared branch (App/desktop client) is
// read-only for the control key — it must never mint one, or every shared
// user would passively acquire a router-control key they never asked for.
func TestSubsSharedBranchDoesNotMint(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	udid, authHeader := subsTestDevice(t, false) // App device, no key minted
	userID := userIDForUDID(t, udid)

	r := gin.New()
	r.GET("/api/subs", api_subs)
	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", authHeader)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	_, has := resp["control_key_hash"]
	assert.False(t, has, "shared branch must not mint: field must be omitted for keyless account")

	var u User
	require.NoError(t, db.Get().First(&u, userID).Error)
	assert.Nil(t, u.RouterControlKey, "shared branch must never mint a key")
}

// =====================================================================
// =====================================================================
// TestSubs_GatewaySlotBindings / TestSubs_GatewayNoBindings_FieldOmitted
//
// Enterprise slot_bindings manifest on the /api/subs gateway branch.
// Needs dev MySQL.
// =====================================================================

func TestSubs_GatewaySlotBindings(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000")

	user := User{UUID: "usr-subs-ent-" + uniq, ExpiredAt: now + 86400}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	udid := "udid-subs-ent-" + uniq
	device := Device{UDID: udid, UserID: user.ID, Remark: "ent-gw-test", IsGateway: true, TokenIssueAt: now}
	require.NoError(t, db.Get().Create(&device).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&device) })

	token := GenerateTestToken(user.ID, udid, time.Hour)
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("token_issue_at", tokenIssueAtOf(t, token)).Error)

	domain := "ent-subs-" + uniq + ".example"
	node := SlaveNode{
		Ipv4: uniqueTestIP(t), SecretToken: "ent-subs-s1", Country: "ae", Region: "dubai",
		Name: "ent-subs-node-" + uniq, Class: NodeClassPrivate, PrivateOwnerUserID: &user.ID,
	}
	require.NoError(t, db.Get().Create(&node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&node) })

	tun := SlaveTunnel{
		Domain: domain, SecretToken: "ent-subs-tt1", Name: "ent-subs-tun-" + uniq,
		Protocol: TunnelProtocolK2V5, Port: 443, NodeID: node.ID,
		IsTest: BoolPtr(false), ServerURL: "k2v5://" + domain + ":443",
	}
	require.NoError(t, db.Get().Create(&tun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })

	sub := PrivateNodeSubscription{
		UserID: user.ID, OrderID: user.ID, Status: PNStatusActive, Region: "dubai",
		IPType: IPTypeNonResidential, SlaveNodeID: &node.ID,
		PurchasedAt: now, ExpiresAt: now + 86400,
	}
	require.NoError(t, db.Get().Create(&sub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&sub) })

	cust := &EnterpriseCustomer{Company: "Ent-Subs-" + uniq, UserID: user.ID}
	require.NoError(t, db.Get().Create(cust).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(cust) })

	line := &EnterpriseLine{CustomerID: cust.ID, NodeID: node.ID, CountryCode: "ae", LineNo: 1}
	require.NoError(t, db.Get().Create(line).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(line) })

	binding := &EnterpriseRouterBinding{GatewayDeviceID: device.ID, Slot: 1, LineID: line.ID}
	require.NoError(t, db.Get().Create(binding).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(binding) })

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(udid+":"+token))
	r := gin.New()
	r.GET("/api/subs", api_subs)

	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", authHeader)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	var resp SubsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.SlotBindings, 1)
	b := resp.SlotBindings[0]
	require.Equal(t, 1, b.Slot)
	require.Equal(t, "ae", b.Country)
	require.GreaterOrEqual(t, b.TunnelIndex, 0)
	require.Less(t, b.TunnelIndex, len(resp.Tunnels))
}

func TestSubs_GatewayNoBindings_FieldOmitted(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	uniq := time.Now().Format("20060102150405.000000")

	user := User{UUID: "usr-subs-noent-" + uniq, ExpiredAt: now + 86400}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	udid := "udid-subs-noent-" + uniq
	device := Device{UDID: udid, UserID: user.ID, Remark: "gw-no-ent-test", IsGateway: true, TokenIssueAt: now}
	require.NoError(t, db.Get().Create(&device).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&device) })

	token := GenerateTestToken(user.ID, udid, time.Hour)
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("token_issue_at", tokenIssueAtOf(t, token)).Error)

	domain := "noent-subs-" + uniq + ".example"
	node := SlaveNode{
		Ipv4: uniqueTestIP(t), SecretToken: "noent-subs-s1", Country: "jp", Region: "tokyo",
		Name: "noent-subs-node-" + uniq, Class: NodeClassPrivate, PrivateOwnerUserID: &user.ID,
	}
	require.NoError(t, db.Get().Create(&node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&node) })

	tun := SlaveTunnel{
		Domain: domain, SecretToken: "noent-subs-tt1", Name: "noent-subs-tun-" + uniq,
		Protocol: TunnelProtocolK2V5, Port: 443, NodeID: node.ID,
		IsTest: BoolPtr(false), ServerURL: "k2v5://" + domain + ":443",
	}
	require.NoError(t, db.Get().Create(&tun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })

	sub := PrivateNodeSubscription{
		UserID: user.ID, OrderID: user.ID, Status: PNStatusActive, Region: "tokyo",
		IPType: IPTypeNonResidential, SlaveNodeID: &node.ID,
		PurchasedAt: now, ExpiresAt: now + 86400,
	}
	require.NoError(t, db.Get().Create(&sub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&sub) })
	// No EnterpriseCustomer/Line/Binding — consumer-mode dedicated line, no enterprise.

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(udid+":"+token))
	r := gin.New()
	r.GET("/api/subs", api_subs)

	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", authHeader)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.NotContains(t, w.Body.String(), "slot_bindings") // omitempty:消费版响应无此字段
}
