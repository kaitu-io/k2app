package center

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
