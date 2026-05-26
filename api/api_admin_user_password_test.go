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

// helper: POST a JSON body to the handler and return the parsed code+message.
type adminPwResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func postAdminPassword(t *testing.T, r *gin.Engine, uuid string, body any) adminPwResp {
	t.Helper()
	bodyBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/app/users/"+uuid+"/password", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "HTTP status must always be 200")
	var resp adminPwResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func TestAdminSetUserPassword_InvalidJSON(t *testing.T) {
	r := gin.New()
	r.POST("/app/users/:uuid/password", api_admin_set_user_password)

	req, _ := http.NewRequest(http.MethodPost, "/app/users/some-uuid/password", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp adminPwResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
}

func TestAdminSetUserPassword_MismatchedPasswords(t *testing.T) {
	r := gin.New()
	r.POST("/app/users/:uuid/password", api_admin_set_user_password)

	resp := postAdminPassword(t, r, "some-uuid", map[string]string{
		"password":        "abcdef1234XY",
		"confirmPassword": "differentpwd",
		"reason":          "support ticket #1234",
	})
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Contains(t, resp.Message, "passwords do not match")
}

func TestAdminSetUserPassword_ReasonTooShort(t *testing.T) {
	r := gin.New()
	r.POST("/app/users/:uuid/password", api_admin_set_user_password)

	// 1: whitespace-only reason — should be rejected after trim.
	resp := postAdminPassword(t, r, "some-uuid", map[string]string{
		"password":        "abcdef1234XY",
		"confirmPassword": "abcdef1234XY",
		"reason":          "  ",
	})
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Contains(t, resp.Message, "reason too short")

	// 2: 2-char reason — also short. Same path as case 1; assert same message
	// so a regression that swaps in a different validator (e.g. password
	// strength) returning the same code can't slip through.
	resp = postAdminPassword(t, r, "some-uuid", map[string]string{
		"password":        "abcdef1234XY",
		"confirmPassword": "abcdef1234XY",
		"reason":          "ab",
	})
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Contains(t, resp.Message, "reason too short")

	// 3: missing reason field — binding:"required" fails. This hits Gin's
	// binding layer, not our trim+len check, so the message format is
	// different. We assert it's NOT the trim path to prove we're testing the
	// binding path.
	resp = postAdminPassword(t, r, "some-uuid", map[string]string{
		"password":        "abcdef1234XY",
		"confirmPassword": "abcdef1234XY",
	})
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.NotEmpty(t, resp.Message)
	assert.NotContains(t, resp.Message, "reason too short")
}
