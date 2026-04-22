package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMemberTestRouter wires the 2 deprecated member management handlers
// into a bare gin router (no auth middleware). The handlers return before
// any user or DB access, so no auth context or mock DB is required.
//
// Delegate-side endpoints (GET/DELETE /api/user/delegate) remain active —
// they preserve the beneficiary-side relationship used by the purchase page
// for one-click proxy-payment requests, and by the account page for reject.
func setupMemberTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	user := r.Group("/api/user")
	{
		user.POST("/members", api_member_add)
		user.DELETE("/members/:userUUID", api_member_remove)
	}
	return r
}

func TestMemberAdd_Deprecated(t *testing.T) {
	router := setupMemberTestRouter()

	body := `{"memberEmail":"x@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/user/members", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[DataAny]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorProxyMembersDeprecated, resp.Code)
}

func TestMemberRemove_Deprecated(t *testing.T) {
	router := setupMemberTestRouter()

	req := httptest.NewRequest(http.MethodDelete, "/api/user/members/any-uuid", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp Response[DataAny]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, ErrorProxyMembersDeprecated, resp.Code)
}

