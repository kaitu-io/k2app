package center

import (
	"encoding/base64"
	"strings"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"

	"github.com/gin-gonic/gin"
)

// SubsTunnel is one entry in the k2subs JSON response.
type SubsTunnel struct {
	URL    string `json:"url"`
	Weight int    `json:"weight"`
}

// SubsResponse is the k2subs subscription endpoint response.
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
		Error(c, ErrorNotLogin, "missing credentials")
		return
	}

	// Validate JWT token and load user+device via existing JWT auth logic.
	// handleJWTAuth sets authContext on c and returns it, or returns nil on failure.
	authCtx := handleJWTAuth(c, token)
	if authCtx == nil || authCtx.User == nil {
		log.Warnf(c, "subs: JWT auth failed for udid=%s", udid)
		Error(c, ErrorNotLogin, "invalid credentials")
		return
	}

	// Verify the UDID in Basic Auth matches the token's device UDID.
	if authCtx.UDID != udid {
		log.Warnf(c, "subs: UDID mismatch: basic_auth=%s token=%s", udid, authCtx.UDID)
		Error(c, ErrorNotLogin, "credential mismatch")
		return
	}

	// Check membership (mirrors ProRequired middleware logic exactly).
	if authCtx.User.IsExpired() {
		log.Infof(c, "subs: user %d membership expired", authCtx.User.ID)
		Error(c, ErrorPaymentRequired, "membership expired")
		return
	}

	// Build tunnel query (mirrors api_k2_tunnels for k2v5 protocol).
	q := db.Get().Model(&SlaveTunnel{}).
		Preload("Node").
		Where("node_id IS NOT NULL").
		Where("protocol = ?", TunnelProtocolK2V5).
		Where(&SlaveTunnel{IsTest: BoolPtr(false)})

	// Optional country filter.
	country := strings.ToLower(strings.TrimSpace(c.Query("country")))
	if country != "" {
		q = q.Joins("JOIN slave_nodes ON slave_nodes.id = slave_tunnels.node_id").
			Where("LOWER(slave_nodes.country) = ?", country)
		log.Debugf(c, "subs: filtering by country=%q", country)
	}

	var tunnels []SlaveTunnel
	if err := q.Find(&tunnels).Error; err != nil {
		log.Errorf(c, "subs: DB query failed: %v", err)
		Error(c, ErrorSystemError, "failed to load tunnels")
		return
	}

	items := make([]SubsTunnel, 0, len(tunnels))
	for _, t := range tunnels {
		if t.ServerURL == "" {
			continue
		}
		url := injectSubsCreds(t.ServerURL, udid, token)
		items = append(items, SubsTunnel{URL: url, Weight: 1})
	}

	log.Infof(c, "subs: user=%d country=%q returning %d tunnels", authCtx.User.ID, country, len(items))
	resp := SubsResponse{Tunnels: items, Refresh: 1800}
	Success(c, &resp)
}
