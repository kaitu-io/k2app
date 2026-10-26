package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func putAppleConsumptionConsent(t *testing.T, userID uint64, body string) (int, map[string]any) {
	t.Helper()
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.PUT("/api/user/apple-consumption-consent", AuthRequired(), EnforceDeviceClass(), api_put_apple_consumption_consent)
	req, _ := http.NewRequest(http.MethodPut, "/api/user/apple-consumption-consent", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+GenerateTestToken(userID, "", time.Hour))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var env map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), w.Body.String())
	return w.Code, env
}

func TestPutAppleConsumptionConsent(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)

	before := time.Now().Unix()
	code, env := putAppleConsumptionConsent(t, user.ID, `{"consented":true}`)
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, env["code"], env)
	data, _ := env["data"].(map[string]any)
	require.NotNil(t, data, env)
	assert.Equal(t, true, data["appleConsumptionConsent"])

	var u User
	require.NoError(t, db.Get().First(&u, user.ID).Error)
	require.NotNil(t, u.AppleConsumptionConsent)
	assert.True(t, *u.AppleConsumptionConsent)
	assert.GreaterOrEqual(t, u.AppleConsumptionConsentAt, before)

	// 撤回同意：false 也落库（不是回到 null）
	_, env = putAppleConsumptionConsent(t, user.ID, `{"consented":false}`)
	data, _ = env["data"].(map[string]any)
	require.NotNil(t, data, env)
	assert.Equal(t, false, data["appleConsumptionConsent"])
	require.NoError(t, db.Get().First(&u, user.ID).Error)
	require.NotNil(t, u.AppleConsumptionConsent)
	assert.False(t, *u.AppleConsumptionConsent)
}

func TestPutAppleConsumptionConsent_MissingFieldRejected(t *testing.T) {
	skipIfNoConfig(t)
	user := CreateTestUser(t)
	_, env := putAppleConsumptionConsent(t, user.ID, `{}`)
	assert.EqualValues(t, ErrorInvalidArgument, env["code"], env)
	var u User
	require.NoError(t, db.Get().First(&u, user.ID).Error)
	assert.Nil(t, u.AppleConsumptionConsent, "缺字段不能被当成 false 写进去")
}
