package center

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
)

const (
	ottPrefix = "ott:"
	ottTTL    = 300 // 5 minutes
)

type ottData struct {
	UserID   uint64 `json:"user_id"`
	Redirect string `json:"redirect"`
}

type DataOTTRequest struct {
	Redirect string `json:"redirect" binding:"required"`
}

type DataOTTResponse struct {
	URL string `json:"url"`
}

// isAllowedRedirect validates redirect URL: must be https, host must be kaitu.io or *.kaitu.io
func isAllowedRedirect(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "kaitu.io" || strings.HasSuffix(host, ".kaitu.io")
}

// api_issue_ott issues a one-time token for webapp → web auth handoff
func api_issue_ott(c *gin.Context) {
	auth := getAuthContext(c)
	log.Infof(c, "user %d requesting OTT", auth.UserID)

	var req DataOTTRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request")
		return
	}

	if !isAllowedRedirect(req.Redirect) {
		Error(c, ErrorInvalidArgument, "redirect URL must be https on kaitu.io domain")
		return
	}

	// Generate 32-byte random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		log.Errorf(c, "failed to generate OTT: %v", err)
		Error(c, ErrorSystemError, "failed to generate token")
		return
	}
	token := hex.EncodeToString(tokenBytes)

	// Store in Redis
	data := ottData{UserID: auth.UserID, Redirect: req.Redirect}
	dataJSON, _ := json.Marshal(data)
	if err := redis.CacheSet(ottPrefix+token, string(dataJSON), ottTTL); err != nil {
		log.Errorf(c, "failed to store OTT in Redis: %v", err)
		Error(c, ErrorSystemError, "failed to store token")
		return
	}

	// Build exchange URL
	baseURL := viper.GetString("frontend_config.app_links.base_url")
	exchangeURL := baseURL + "/api/auth/ott/exchange?ott=" + token + "&redirect=" + url.QueryEscape(req.Redirect)

	log.Infof(c, "OTT issued for user %d, redirect: %s", auth.UserID, req.Redirect)
	Success(c, &DataOTTResponse{URL: exchangeURL})
}

// api_exchange_ott exchanges a one-time token for cookie session
func api_exchange_ott(c *gin.Context) {
	token := c.Query("ott")
	redirect := c.Query("redirect")

	if token == "" || redirect == "" {
		c.Redirect(302, "/auth/login?reason=invalid")
		return
	}

	// Get from Redis
	var dataJSON string
	exist, err := redis.CacheGet(ottPrefix+token, &dataJSON)
	if err != nil || !exist {
		log.Warnf(c, "OTT exchange failed: token not found or expired")
		c.Redirect(302, "/auth/login?reason=expired")
		return
	}

	// Delete immediately (one-time use)
	_ = redis.CacheDel(ottPrefix + token)

	// Parse stored data
	var data ottData
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		log.Errorf(c, "OTT exchange failed: corrupt data: %v", err)
		c.Redirect(302, "/auth/login?reason=invalid")
		return
	}

	// Verify redirect matches stored value
	if data.Redirect != redirect {
		log.Warnf(c, "OTT exchange failed: redirect mismatch (stored=%s, got=%s)", data.Redirect, redirect)
		c.Redirect(302, "/auth/login?reason=invalid")
		return
	}

	// Look up user to get roles
	var user User
	if err := db.Get().First(&user, data.UserID).Error; err != nil {
		log.Errorf(c, "OTT exchange failed: user %d not found: %v", data.UserID, err)
		c.Redirect(302, "/auth/login?reason=expired")
		return
	}

	// Generate web cookie token (same as web login)
	authResult, _, err := generateWebCookieToken(c, user.ID, user.Roles)
	if err != nil {
		log.Errorf(c, "OTT exchange failed: token generation error: %v", err)
		c.Redirect(302, "/auth/login?reason=expired")
		return
	}

	// Set cookies and redirect
	setAuthCookies(c, authResult)
	log.Infof(c, "OTT exchange successful for user %d, redirecting to %s", user.ID, redirect)
	c.Redirect(302, redirect)
}
