package center

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// createAccessKeyUserForTest 建一个用 X-Access-Key 认证的用户（绕开 device/token 发行链路）。
func createAccessKeyUserForTest(t *testing.T, brand Brand, roles uint64) string {
	t.Helper()
	plaintext := accessKeyPrefix + generateId("admin-brand")
	hash := HashAccessKey(plaintext)
	user := &User{
		UUID:      generateId("user"),
		Brand:     string(brand),
		Roles:     roles,
		AccessKey: &hash,
	}
	require.NoError(t, db.Get().Create(user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(user) })
	return plaintext
}

// setupStaffAuthTestRouter 镜像 route.go 里 /app opsAdmin 组与 /api/user 组的中间件链。
func setupStaffAuthTestRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	ok := func(c *gin.Context) { c.JSON(200, gin.H{"code": 0, "message": "ok", "data": gin.H{}}) }

	app := r.Group("/app", BrandResolver(), StaffAuthRequired())
	app.GET("/my-permissions", ok)
	app.GET("/feedback-tickets", RoleRequired(RoleSupport), ok)

	user := r.Group("/api/user", BrandResolver(), AuthRequired())
	user.GET("/info", ok)
	return r
}

func doAccessKeyGet(t *testing.T, r *gin.Engine, path, key string) *TestResponse {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, path, nil)
	require.NoError(t, err)
	// 不带 Host / X-K2-Brand → 请求品牌 = kaitu（manager 唯一入口 kaitu.io 的形态）
	req.Header.Set("X-Access-Key", key)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp, err := ParseResponse(w)
	require.NoError(t, err, "body: %s", w.Body.String())
	return resp
}

// /app/* 是跨品牌管理面：只有角色位（非 IsAdmin）的 overleap 账号员工，从 kaitu.io 进
// manager 不能被 403003 拒掉。
func TestStaffAuth_RoleStaffCrossBrand_Allowed(t *testing.T) {
	skipIfNoConfig(t)
	r := setupStaffAuthTestRouter()
	key := createAccessKeyUserForTest(t, BrandOverleap, RoleUser|RoleSupport)

	resp := doAccessKeyGet(t, r, "/app/feedback-tickets", key)
	assert.Equal(t, 0, resp.Code, "role staff must reach /app/* across brands")
	resp = doAccessKeyGet(t, r, "/app/my-permissions", key)
	assert.Equal(t, 0, resp.Code)
}

// 豁免只给员工：普通 overleap 用户摸 /app/* 仍按品牌硬拒。
func TestStaffAuth_PlainUserCrossBrand_StillRejected(t *testing.T) {
	skipIfNoConfig(t)
	r := setupStaffAuthTestRouter()
	key := createAccessKeyUserForTest(t, BrandOverleap, RoleUser)

	resp := doAccessKeyGet(t, r, "/app/my-permissions", key)
	assert.Equal(t, int(ErrorBrandMismatch), resp.Code)
}

// 员工身份不能把 /api/*（用户面）打开成跨品牌。
func TestStaffAuth_DoesNotOpenUserApiCrossBrand(t *testing.T) {
	skipIfNoConfig(t)
	r := setupStaffAuthTestRouter()
	key := createAccessKeyUserForTest(t, BrandOverleap, RoleUser|RoleSupport)

	resp := doAccessKeyGet(t, r, "/api/user/info", key)
	assert.Equal(t, int(ErrorBrandMismatch), resp.Code)
}

// 纯函数：谁算员工。
func TestIsCrossBrandStaff(t *testing.T) {
	yes := true
	assert.False(t, isCrossBrandStaff(nil))
	assert.False(t, isCrossBrandStaff(&User{Roles: RoleUser}))
	assert.False(t, isCrossBrandStaff(&User{Roles: 0}))
	assert.True(t, isCrossBrandStaff(&User{Roles: RoleUser, IsAdmin: &yes}))
	for _, role := range []uint64{RoleMarketing, RoleDevopsViewer, RoleDevopsEditor, RoleSupport} {
		assert.True(t, isCrossBrandStaff(&User{Roles: RoleUser | role}), "role %d", role)
	}
}

// 路由接线守卫：route.go 里每个 /app 路由组都不得挂 AuthRequired()（品牌硬拒会把
// overleap 账号的角色员工挡在 manager 外），必须用 StaffAuthRequired() 或 AdminRequired()。
func TestRouteWiring_AppGroupsUseStaffAuth(t *testing.T) {
	src, err := os.ReadFile("route.go")
	require.NoError(t, err)
	groupRe := regexp.MustCompile(`(\w+)\s*:=\s*r\.Group\("/app[^"]*"\)`)
	matches := groupRe.FindAllStringSubmatch(string(src), -1)
	require.NotEmpty(t, matches)
	for _, m := range matches {
		useRe := regexp.MustCompile(regexp.QuoteMeta(m[1]) + `\.Use\(([^\n]*)\)`)
		use := useRe.FindStringSubmatch(string(src))
		require.NotNil(t, use, "group %s has no .Use(...) line", m[1])
		assert.NotRegexp(t, `(^|[^f])AuthRequired\(\)`, use[1], "/app group %s must not use AuthRequired(); use StaffAuthRequired()", m[1])
	}
}
