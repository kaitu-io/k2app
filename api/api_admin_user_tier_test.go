package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// These tests exercise input validation paths that run before any DB or
// admin-context lookup, so they don't need DB or auth fixtures.

func TestAdminChangeUserTier_InvalidTierRejected(t *testing.T) {
	r := gin.New()
	r.PUT("/app/users/:uuid/tier", api_admin_change_user_tier)

	body := map[string]any{"tier": "garbage_tier", "reason": "test reason here"}
	bodyBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", "/app/users/some-uuid/tier", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotEqual(t, float64(0), resp["code"], "invalid tier should fail")
	assert.Contains(t, w.Body.String(), "garbage_tier")
}

func TestAdminChangeUserTier_RequiresReason(t *testing.T) {
	r := gin.New()
	r.PUT("/app/users/:uuid/tier", api_admin_change_user_tier)

	body := map[string]any{"tier": "family"} // missing reason
	bodyBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", "/app/users/some-uuid/tier", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotEqual(t, float64(0), resp["code"], "missing reason should fail")
}
