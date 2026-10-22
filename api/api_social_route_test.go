package center

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 接线守卫：请求体带 idToken + rawNonce 的联合登录路由只能挂在不记 body 的组上。
// log.MiddlewareRequestLog(true) 会把 POST 请求体与 JSON 响应体原样写进 app.log——
// token 过期前拿日志里的那一行就能重放登录。
func TestSocialTokenRoutes_NotBodyLogged(t *testing.T) {
	src, err := os.ReadFile("route.go")
	require.NoError(t, err)
	s := string(src)

	for _, reg := range []string{
		`socialSensitive.POST("/auth/social/login", api_social_login)`,
		`socialSensitive.POST("/auth/social/web-login", api_social_web_login)`,
		`socialSensitive.POST("/user/social/bind", AuthRequired(), EnforceDeviceClass(), api_user_social_bind)`,
	} {
		assert.Contains(t, s, reg)
	}
	for _, h := range []string{"api_social_login)", "api_social_web_login)", "api_user_social_bind)"} {
		assert.Equal(t, 1, strings.Count(s, h), "不得在别的组重复注册: %s", h)
	}
	use := regexp.MustCompile(`socialSensitive\.Use\(([^\n]*)\)`).FindStringSubmatch(s)
	require.NotNil(t, use)
	assert.Contains(t, use[1], "requestLogWithoutBodies()")
	assert.NotContains(t, use[1], "MiddlewareRequestLog")
}
