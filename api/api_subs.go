package center

import (
	"encoding/base64"
	"net/http"
	"strings"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"

	"github.com/gin-gonic/gin"
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

// SubsTunnel is one entry in the k2subs JSON response.
type SubsTunnel struct {
	URL    string `json:"url"`
	Weight int    `json:"weight"`
}

// SubsResponse is the k2subs subscription endpoint response body (raw, no envelope).
type SubsResponse struct {
	Tunnels []SubsTunnel `json:"tunnels"`
	Refresh int          `json:"refresh"`
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

	// Validate JWT token and load user+device via existing JWT auth logic.
	authCtx := handleJWTAuth(c, token)
	if authCtx == nil || authCtx.User == nil {
		log.Warnf(c, "subs: JWT auth failed for udid=%s", udid)
		subsError(c, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Verify the UDID in Basic Auth matches the token's device UDID.
	if authCtx.UDID != udid {
		log.Warnf(c, "subs: UDID mismatch: basic_auth=%s token=%s", udid, authCtx.UDID)
		subsError(c, http.StatusUnauthorized, "credential mismatch")
		return
	}

	// Require device context — web-auth tokens (no device) must not access subs.
	if authCtx.Device == nil {
		log.Warnf(c, "subs: device context required, udid=%s", udid)
		subsError(c, http.StatusUnauthorized, "device context required")
		return
	}

	// Check membership (mirrors ProRequired middleware logic exactly).
	if authCtx.User.IsExpired() {
		log.Infof(c, "subs: user %d membership expired", authCtx.User.ID)
		subsError(c, http.StatusPaymentRequired, "membership expired")
		return
	}

	// Mirror api_k2_tunnels query shape (api_tunnel.go:48-71):
	//   - Preload("Node") so the response loop can read node fields without N+1
	//   - tunnelProtocolsForQuery(K2V5) returns [K2V5] today; futureproof if
	//     the helper ever expands (it already backs k2v4 clients transparently).
	//   - Non-admins never see IsTest=true tunnels. Admin users do, for live
	//     testing via real clients.
	isAdmin := authCtx.User.IsAdmin != nil && *authCtx.User.IsAdmin
	q := db.Get().Model(&SlaveTunnel{}).
		Preload("Node").
		Where("node_id IS NOT NULL").
		Where("protocol IN ?", tunnelProtocolsForQuery(TunnelProtocolK2V5))
	if !isAdmin {
		q = q.Where(&SlaveTunnel{IsTest: BoolPtr(false)})
	}

	// Country filter — DB stores ISO-3166 alpha-2 in UPPERCASE (`JP`, `HK`, `US`).
	// Accept any case from the client and normalize to uppercase so the WHERE
	// clause matches the canonical stored form (index-friendly, no row-level
	// function call like LOWER()).
	country := strings.ToUpper(strings.TrimSpace(c.Query("country")))
	if country != "" {
		q = q.Joins("Node").
			Where("slave_nodes.country = ?", country)
		log.Debugf(c, "subs: filtering by country=%q", country)
	}

	var tunnels []SlaveTunnel
	if err := q.Find(&tunnels).Error; err != nil {
		log.Errorf(c, "subs: DB query failed: %v", err)
		subsError(c, http.StatusInternalServerError, "failed to load tunnels")
		return
	}

	items := make([]SubsTunnel, 0, len(tunnels))
	for _, t := range tunnels {
		// Defensive — mirrors api_k2_tunnels:120-122. The `node_id IS NOT NULL`
		// WHERE plus Preload makes this redundant in normal flow, but guards
		// against data drift (e.g. node row soft-deleted but tunnel still
		// references it).
		if t.Node == nil || t.Node.ID == 0 {
			continue
		}
		if t.ServerURL == "" {
			continue
		}
		items = append(items, SubsTunnel{
			URL:    injectSubsCreds(t.ServerURL, udid, token),
			Weight: 1,
		})
	}

	log.Infof(c, "subs: user=%d country=%q isAdmin=%v returning %d tunnels",
		authCtx.User.ID, country, isAdmin, len(items))

	// RAW JSON — see the wire-protocol note at the top of this file.
	c.JSON(http.StatusOK, SubsResponse{
		Tunnels: items,
		Refresh: 1800,
	})
}
