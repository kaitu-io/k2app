package center

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func setupRouterTestContext(user *User) (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)
	if user != nil {
		c.Set("authContext", &authContext{User: user, UserID: user.ID})
	}
	return w, c
}

func TestRouterRequired_NoUser(t *testing.T) {
	_, c := setupRouterTestContext(nil)
	RouterRequired()(c)
	assert.True(t, c.IsAborted())
}

// routerReqTestUser creates a user (and cleans up the user + any private subs it
// accrues). tier/sharedExpiredAt are arbitrary — router access is line-gated.
func routerReqTestUser(t *testing.T, tier string, sharedExpiredAt int64) *User {
	t.Helper()
	u := &User{UUID: "rr-" + time.Now().Format("150405.000000000"), Tier: tier, ExpiredAt: sharedExpiredAt}
	require.NoError(t, db.Get().Create(u).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&PrivateNodeSubscription{})
		db.Get().Unscoped().Delete(u)
	})
	return u
}

// 任意 tier + 即使共享会员过期，只要持 active 专属线 → 放行（彻底脱钩 tier/共享会员）。
func TestRouterRequired_LineOwnerAnyTierAllowed(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	u := routerReqTestUser(t, TierBasic, time.Now().Add(-24*time.Hour).Unix()) // basic + 共享会员已过期
	createActivePrivateNodeSub(t, u.ID)
	_, c := setupRouterTestContext(u)
	RouterRequired()(c)
	assert.False(t, c.IsAborted(), "basic 档+共享会员过期+持线应放行")
}

// 无 active 专属线 → 拒（即便 family 档）。
func TestRouterRequired_NoLineDenied(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	u := routerReqTestUser(t, TierFamily, time.Now().Add(30*24*time.Hour).Unix())
	w, c := setupRouterTestContext(u)
	RouterRequired()(c)
	assert.True(t, c.IsAborted(), "family 档但无线应被拒")
	assert.Contains(t, w.Body.String(), "402001")
}
