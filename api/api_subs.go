package center

import (
	"encoding/base64"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/wordgate/qtoolkit/log"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	db "github.com/wordgate/qtoolkit/db"
)

// ============================================================================
// /api/subs — k2subs:// subscription wire endpoint
//
// This handler is the *external* wire endpoint for the k2subs:// subscription
// protocol consumed by the k2 daemon (see k2/config/subscription.go). It is
// intentionally a **documented exception** to Center's `{code, message, data}`
// envelope convention, for the same reason payment webhooks bypass it:
//
//   - Success:  HTTP 200 + raw `{"tunnels": [...], "refresh": N}` at JSON root.
//               Daemon unmarshals these keys directly; a wrapped envelope would
//               silently yield `tunnels: nil` (encoding/json tolerates unknown
//               top-level keys) and report "no tunnels available" regardless
//               of the actual items count.
//
//   - Error:    Real HTTP status code (401 / 402 / 500) with a plain-text body.
//               Daemon formats these as `subscription fetch: status %d: %s`
//               using the body as a human-readable hint
//               (k2/config/subscription.go:132-137).
//
// Changes to this response shape MUST be coordinated with the daemon side.
// ============================================================================

// Scale used to project the canonical recommendScore [0,1] onto the legacy
// Weight int field for backward compatibility with daemons that pre-date the
// recommendScore field. One release cycle after recommendScore ships, Weight
// can be removed.
const subsLegacyWeightScale = 100

// SubsTunnel is one entry in the k2subs JSON response.
type SubsTunnel struct {
	URL string `json:"url"`
	// Weight is the legacy integer weight. Derived as round(RecommendScore *
	// subsLegacyWeightScale). Kept so pre-recommendScore daemons can still do
	// reasonable weighted picks. Remove in a future release once rollout is
	// confirmed.
	Weight int `json:"weight"`
	// RecommendScore is the canonical [0,1] recommendation signal produced by
	// ComputeRecommendScore. Higher = better. New daemon/webapp clients prefer
	// this over Weight.
	RecommendScore float64 `json:"recommendScore"`
	// IPType is the exit-IP nature of the backing node
	// (residential|non_residential|unknown). Additive field — old daemons ignore
	// unknown JSON keys. New daemon Pick logic can prefer residential IPs.
	// omitempty: omits the field when empty (unknown nodes from older schema rows).
	IPType string `json:"ipType,omitempty"`
}

// SubsResponse is the k2subs subscription endpoint response body (raw, no envelope).
type SubsResponse struct {
	Tunnels []SubsTunnel `json:"tunnels"`
	Refresh int          `json:"refresh"`
	// ControlKeyHash is the sha256 hex of the owning account's router control
	// key — the authoritative value k2r's headless panel auths against.
	// Omitted when the account has no key yet. Cross-repo contract: the k2
	// submodule's config.subsResponse consumes this exact field name.
	ControlKeyHash string `json:"control_key_hash,omitempty"`

	// SlotBindings is the enterprise multi-slot manifest (operator-maintained
	// binding matrix). Present only for enterprise gateway devices; its
	// presence switches k2r into multi-slot mode.
	SlotBindings []SubsSlotBinding `json:"slot_bindings,omitempty"`
}

// injectControlKeyHash writes the account's control-key hash into resp if one
// already exists. Read-only — used on the shared branch (App/desktop client),
// which must never mint a key on behalf of a user who never requested one.
func injectControlKeyHash(resp *SubsResponse, user *User) {
	if user != nil && user.RouterControlKey != nil && *user.RouterControlKey != "" {
		resp.ControlKeyHash = HashRouterControlKey(*user.RouterControlKey)
	}
}

// ensureAndInjectControlKeyHash is the gateway-branch (k2r client) mint-on-
// serve counterpart: if the account has no control key yet, mint one
// idempotently via EnsureRouterControlKey before injecting its hash. This
// closes the TOFU window for pre-existing k2subs:// routers whose owner never
// opens a fresh app to trigger the self-service mint endpoint (spec §4).
// Minting failure DEGRADES to a keyless response (logged at Warn) rather than
// failing the whole subs fetch — router control-key delivery must never take
// the tunnel list down with it.
func ensureAndInjectControlKeyHash(c *gin.Context, resp *SubsResponse, user *User) {
	if user == nil {
		return
	}
	if user.RouterControlKey == nil || *user.RouterControlKey == "" {
		key, err := EnsureRouterControlKey(c.Request.Context(), user.ID)
		if err != nil {
			log.Warnf(c, "subs: mint router control key for user %d failed: %v", user.ID, err)
		} else {
			resp.ControlKeyHash = HashRouterControlKey(key)
			return
		}
	}
	injectControlKeyHash(resp, user)
}

// extractSubsBasicAuth parses "Authorization: Basic <b64>" → (udid, token, ok).
// Returns false if header is absent, malformed, or either part is empty.
func extractSubsBasicAuth(c *gin.Context) (string, string, bool) {
	auth := c.GetHeader("Authorization")
	if !strings.HasPrefix(auth, "Basic ") {
		return "", "", false
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(payload), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// injectSubsCreds inserts udid:token into a k2v5:// URL (before the host).
// "k2v5://host:443?ech=x" → "k2v5://udid:token@host:443?ech=x"
// If serverURL has no "://" separator, returns it unchanged.
func injectSubsCreds(serverURL, udid, token string) string {
	const sep = "://"
	idx := strings.Index(serverURL, sep)
	if idx < 0 {
		return serverURL
	}
	scheme := serverURL[:idx]
	rest := serverURL[idx+len(sep):]
	return scheme + sep + udid + ":" + token + "@" + rest
}

// subsError writes a plain-text HTTP error response. Daemon reads the first
// 256 bytes of the body as a human-readable hint (subscription.go:132-137).
// Do NOT use gin's Error() wrapper here — that emits the `{code, message}`
// envelope which pollutes the daemon's hint parsing.
func subsError(c *gin.Context, status int, msg string) {
	c.String(status, msg)
}

// api_subs returns a k2subs-format tunnel list for the authenticated device user.
//
// Authentication: HTTP Basic Auth — username=UDID, password=access_token.
// The token is validated via handleJWTAuth (same JWT the webapp uses).
// Membership is checked via user.IsExpired() (same as ProRequired middleware).
//
// Optional query param: ?country=jp  (ISO 3166-1 alpha-2, case-insensitive)
//
// Route: GET /api/subs
func api_subs(c *gin.Context) {
	udid, token, ok := extractSubsBasicAuth(c)
	if !ok {
		log.Warnf(c, "subs: missing or malformed Basic Auth header")
		subsError(c, http.StatusUnauthorized, "missing credentials")
		return
	}

	// Validate the credential and load user+device. Transition-period dual
	// accept (spec §10.1): the Basic-auth password may be a tunnel token
	// (Phase 0 URLs) or a legacy access token (pre-Phase-0 URLs).
	auth := subsAuthenticate(c, token)
	if auth == nil || auth.User == nil {
		log.Warnf(c, "subs: auth failed for udid=%s", udid)
		subsError(c, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Verify the UDID in Basic Auth matches the token's device UDID.
	if auth.UDID != udid {
		log.Warnf(c, "subs: UDID mismatch: basic_auth=%s token=%s", udid, auth.UDID)
		subsError(c, http.StatusUnauthorized, "credential mismatch")
		return
	}

	// Require device context — web-auth tokens (no device) must not access subs.
	if auth.Device == nil {
		log.Warnf(c, "subs: device context required, udid=%s", udid)
		subsError(c, http.StatusUnauthorized, "device context required")
		return
	}

	// Device-class cross-check. This mirrors the EnforceDeviceClass middleware,
	// which is mounted on /api/tunnels but NOT on /api/subs (this handler does
	// its own Basic auth inside the body, so a middleware would have no auth
	// context yet). A stock k2r ALWAYS sends X-K2-Client: kaitu-router; reject a
	// self-identified router riding an App-class credential (IsGateway=false) —
	// otherwise a hand-crafted k2subs:// URL carrying an App token would route a
	// whole LAN through the App-only shared pool, bypassing the dedicated-line
	// gate. A MISSING header still passes (backward compat for pre-header
	// clients); only a present header is enforced.
	if rawHeader := c.GetHeader("X-K2-Client"); rawHeader != "" {
		info := parseClientHeader(rawHeader)
		if info == nil {
			log.Warnf(c, "subs: invalid X-K2-Client header, udid=%s", udid)
			subsError(c, http.StatusBadRequest, "invalid client class")
			return
		}
		if info.IsGateway() != auth.Device.IsGateway {
			log.Warnf(c, "subs: device class mismatch udid=%s db_gateway=%v header_gateway=%v",
				udid, auth.Device.IsGateway, info.IsGateway())
			subsError(c, http.StatusForbidden, "device class mismatch")
			return
		}
	}

	// Phase 0: whatever credential authenticated this request, the URLs we
	// return embed a tunnel token — access→tunnel conversion on legacy
	// requests, 50%-threshold rolling renewal on tunnel-token requests.
	urlToken := subsTokenForURLs(c, auth, token)

	// 能力矩阵：路由器（gateway）只用专属节点；App/桌面用共享池。k2r 未发布，
	// 此处硬切，无存量路由器回落需求。
	//
	// ORDERING (load-bearing): this gateway branch MUST run BEFORE the shared-
	// membership IsExpired() gate below. Private nodes use an independent clock
	// (per-node PrivateNodeSubscription) and do NOT depend on shared membership —
	// a router owner whose shared membership has lapsed but who holds an active/
	// grace private-node subscription must still get their private tunnels.
	// Gating gateway users by User.ExpiredAt would break the capability matrix
	// (see ResolveGatewayPrivateTunnels + its test, which deliberately use an
	// expired-shared-membership owner). The branch returns in ALL paths, so a
	// non-gateway device falls straight through to the unchanged shared-pool flow.
	if auth.Device.IsGateway {
		privTunnels, err := ResolveGatewayPrivateTunnels(c, auth.User.ID, time.Now().Unix())
		if err != nil {
			log.Errorf(c, "subs: resolve private tunnels failed: %v", err)
			subsError(c, http.StatusInternalServerError, "failed to load private nodes")
			return
		}
		items := buildPrivateSubsTunnels(privTunnels, udid, urlToken)
		if len(items) == 0 {
			log.Infof(c, "subs: gateway user %d has no serviceable private node", auth.User.ID)
			subsError(c, http.StatusPaymentRequired, "no private node entitlement")
			return
		}
		log.Infof(c, "subs: gateway user=%d returning %d private tunnels", auth.User.ID, len(items))
		resp := SubsResponse{Tunnels: items, Refresh: 1800}
		ensureAndInjectControlKeyHash(c, &resp, auth.User)
		// TunnelIndex must index this same response's `items` — mirror
		// buildPrivateSubsTunnels's filter so positions line up even if
		// privTunnels contains an entry it would have skipped.
		aligned := make([]SlaveTunnel, 0, len(privTunnels))
		for _, t := range privTunnels {
			if t.Node == nil || t.Node.ID == 0 || t.ServerURL == "" {
				continue
			}
			aligned = append(aligned, t)
		}
		resp.SlotBindings = resolveSlotBindings(c, auth.Device.ID, aligned)
		writeSubsOK(c, resp)
		return
	}

	// Check membership (mirrors ProRequired middleware logic exactly).
	// Non-gateway (App/desktop) devices only — gateway devices returned above.
	if auth.User.IsExpired() {
		log.Infof(c, "subs: user %d membership expired", auth.User.ID)
		subsError(c, http.StatusPaymentRequired, "membership expired")
		return
	}

	// Admin bypass for test tunnels — mirrors api_k2_tunnels:44,67-71.
	isAdmin := auth.User.IsAdmin != nil && *auth.User.IsAdmin

	tunnels, err := fetchK2V5Tunnels(c, isAdmin)
	if err != nil {
		log.Errorf(c, "subs: DB query failed: %v", err)
		subsError(c, http.StatusInternalServerError, "failed to load tunnels")
		return
	}

	// Country filter in-memory. DB stores ISO-3166 alpha-2 uppercase; normalize
	// both sides so future casing drift won't reintroduce silent empty results.
	// Empty country = no filter. This intentionally does NOT push into SQL — the
	// JOIN variants (explicit or `Joins("Node")`) are too easy to get subtly
	// wrong (see 3e20b8e postmortem), and the tunnel set is tiny (<100).
	country := strings.ToUpper(strings.TrimSpace(c.Query("country")))

	// Batch-fetch NodeUsage rows by ipv4 (durable key, node-authority metering mirror) so we
	// can compute per-tunnel RecommendScore via the same code path /api/tunnels
	// uses. Non-metered nodes fall through to ComputeRecommendScore(nil) → 0.5
	// (neutral), keeping them eligible in pickWeighted without being favored.
	nodeIPs := make([]string, 0, len(tunnels))
	for _, t := range tunnels {
		if t.Node != nil && t.Node.ID != 0 {
			nodeIPs = append(nodeIPs, t.Node.Ipv4)
		}
	}
	usageMap := getNodeUsagesByIPs(nodeIPs)
	now := time.Now().Unix()

	items := make([]SubsTunnel, 0, len(tunnels))
	for _, t := range tunnels {
		// Defensive — mirrors api_k2_tunnels:120-122. The `node_id IS NOT NULL`
		// WHERE plus Preload makes this redundant in normal flow, but guards
		// against data drift (e.g. node row soft-deleted but tunnel still
		// references it).
		if t.Node == nil || t.Node.ID == 0 {
			continue
		}
		// Capability matrix (App→private ❌): private nodes belong to a single
		// owner and are delivered ONLY via the gateway branch above. They must
		// never appear in the shared pool, or one user's dedicated VPS would leak
		// into every App user's server list. fetchK2V5Tunnels intentionally does
		// not filter by class (its doc: "capability filtering belongs in the
		// caller") — this is that filter.
		if t.Node.Class == NodeClassPrivate {
			continue
		}
		// Brand visibility filter: /api/subs has no BrandResolver middleware
		// (Basic Auth is parsed inside this handler), so user.Brand — the
		// authenticated user's own birth attribute — is the only brand signal
		// available here. Admin bypasses, mirroring the isTest/quota-hide
		// bypass a few lines below (isAdmin already computed above).
		if !isAdmin && !t.Node.VisibleTo(Brand(auth.User.Brand)) {
			continue
		}
		if t.ServerURL == "" {
			continue
		}
		if country != "" && strings.ToUpper(t.Node.Country) != country {
			continue
		}

		// Same over-quota/offline hard-exclude as /api/tunnels. Subscription
		// clients run weighted-pick over this list — once billing tips into
		// overage every byte costs real money, so even a low score is not
		// safe enough. Admin bypass keeps the path open for triage.
		u := usageMap[t.Node.Ipv4] // nil if no usage row yet
		if shouldHideTunnelForUser(u, isAdmin, now) {
			log.Warnf(c, "subs: tunnel %d (node=%s, ip=%s) hidden from non-admin (over-quota/offline)",
				t.ID, t.Node.Name, t.Node.Ipv4)
			continue
		}

		score := ComputeRecommendScore(buildTunnelInstanceDataFromUsage(u))

		items = append(items, SubsTunnel{
			URL:            injectSubsCreds(t.ServerURL, udid, urlToken),
			Weight:         int(math.Round(score * subsLegacyWeightScale)),
			RecommendScore: score,
			IPType:         t.Node.IPType,
		})
	}

	log.Infof(c, "subs: user=%d country=%q isAdmin=%v returning %d tunnels",
		auth.User.ID, country, isAdmin, len(items))

	resp := SubsResponse{Tunnels: items, Refresh: 1800}
	injectControlKeyHash(&resp, auth.User)
	writeSubsOK(c, resp)
}

// writeSubsOK emits the raw /api/subs success response. RecommendScore is
// derived from NodeUsage (node-reported metering, updated each report cycle),
// so end-to-end caching by CloudFront could freeze a stale view for hours. The
// Cache-Control header is the client-side half of that contract; CloudFront
// Behavior for /api/subs must also set Minimum TTL = 0 for the header to be
// respected.
func writeSubsOK(c *gin.Context, resp SubsResponse) {
	c.Header("Cache-Control", "no-store, private")
	// RAW JSON — see the wire-protocol note at the top of this file.
	c.JSON(http.StatusOK, resp)
}

// buildPrivateSubsTunnels 把专属节点隧道列表转为 subs 响应项，与共享池同构。
// 专属节点为单一主人独占、确定性使用，不参与共享池的 ComputeRecommendScore，
// 统一取中性推荐分 0.5；URL 复用 injectSubsCreds 注入用户凭证。
func buildPrivateSubsTunnels(tunnels []SlaveTunnel, udid, token string) []SubsTunnel {
	items := make([]SubsTunnel, 0, len(tunnels))
	for _, t := range tunnels {
		if t.Node == nil || t.Node.ID == 0 || t.ServerURL == "" {
			continue
		}
		const neutralScore = 0.5
		items = append(items, SubsTunnel{
			URL:            injectSubsCreds(t.ServerURL, udid, token),
			Weight:         int(math.Round(neutralScore * subsLegacyWeightScale)),
			RecommendScore: neutralScore,
			IPType:         t.Node.IPType,
		})
	}
	return items
}

// subsAuthResult 是 /api/subs 的统一认证结果：无论请求用哪种 token 认证，
// handler 后续只消费 User/Device/UDID；TunnelClaims 非 nil 时表示请求以
// tunnel token 认证（供 subsTokenForURLs 走续期分支）。
type subsAuthResult struct {
	User         *User
	Device       *Device
	UDID         string
	TunnelClaims *TokenClaims
	CredType     string // "tunnel" | "access"
}

// subsAuthenticate 过渡期双 token 认证：不验签窥探 claims.Type 分派——
// tunnel → validateTunnelToken（TunnelIssueAt 锚点）；其他 → handleJWTAuth
// （既有 access 路径原样保留，含 X-K2-Client 设备信息刷新）。成功时按凭据
// 类型打一条 INFO（credType=tunnel|access）——spec §11 采纳率出口判据的唯一
// 权威数据源（部署后 grep 计数）。
func subsAuthenticate(c *gin.Context, token string) *subsAuthResult {
	var peek TokenClaims
	if _, _, err := jwt.NewParser().ParseUnverified(token, &peek); err == nil && peek.Type == TokenTypeTunnel {
		claims, device, err := validateTunnelToken(c, token)
		if err != nil {
			log.Warnf(c, "subs: tunnel token validation failed: %v", err)
			return nil
		}
		var user User
		if err := db.Get().First(&user, device.UserID).Error; err != nil {
			log.Errorf(c, "subs: load user %d failed: %v", device.UserID, err)
			return nil
		}
		log.Infof(c, "subs: authenticated udid=%s credType=tunnel", device.UDID)
		return &subsAuthResult{User: &user, Device: device, UDID: device.UDID, TunnelClaims: claims, CredType: "tunnel"}
	}
	authCtx := handleJWTAuth(c, token)
	if authCtx == nil || authCtx.User == nil {
		return nil
	}
	log.Infof(c, "subs: authenticated udid=%s credType=access", authCtx.UDID)
	return &subsAuthResult{User: authCtx.User, Device: authCtx.Device, UDID: authCtx.UDID, CredType: "access"}
}

// subsTokenForURLs 决定回填进 tunnel URL 的凭据（恒为 tunnel token）：
//   - tunnel token 请求：剩余 <50% 续期（锚点不变），否则原样回填；
//   - access token 请求（存量 URL）：签发该设备的 tunnel token（转换点）。
//
// 签发/续期失败时降级回填请求自带的 token（可用性优先，同
// ensureAndInjectControlKeyHash 的哲学：凭据升级失败不得拖垮隧道列表）。
func subsTokenForURLs(c *gin.Context, auth *subsAuthResult, requestToken string) string {
	if auth.TunnelClaims != nil {
		renewed, ok, err := maybeRenewTunnelToken(c, auth.TunnelClaims, auth.Device)
		if err != nil {
			log.Warnf(c, "subs: tunnel token renew failed for udid=%s: %v", auth.UDID, err)
			return requestToken
		}
		if ok {
			log.Infof(c, "subs: rolled tunnel token for udid=%s", auth.UDID)
			return renewed
		}
		return requestToken
	}
	tok, err := issueTunnelTokenForDevice(c, auth.Device, auth.User.Roles)
	if err != nil {
		log.Warnf(c, "subs: issue tunnel token failed for udid=%s: %v", auth.UDID, err)
		return requestToken
	}
	return tok
}
