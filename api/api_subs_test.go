package center

import (
	"encoding/base64"
	"encoding/json"
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
// TestApiSubs_NoAuth_Returns401 — handler test: missing auth → ErrorNotLogin
// =====================================================================

func TestApiSubs_NoAuth_Returns401(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/api/subs", api_subs)

	req, err := http.NewRequest("GET", "/api/subs", nil)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	code, ok := resp["code"].(float64)
	require.True(t, ok, "response should have numeric 'code' field")
	assert.Equal(t, float64(ErrorNotLogin), code)
}
