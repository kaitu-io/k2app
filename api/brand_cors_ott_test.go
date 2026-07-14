package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsAllowedRedirectPerBrand(t *testing.T) {
	// kaitu 用户：只许 kaitu.io 域
	assert.True(t, isAllowedRedirect("https://kaitu.io/account", BrandKaitu))
	assert.True(t, isAllowedRedirect("https://www.kaitu.io/x", BrandKaitu))
	assert.False(t, isAllowedRedirect("https://overleap.io/account", BrandKaitu))
	// overleap 用户：只许 overleap.io 域
	assert.True(t, isAllowedRedirect("https://overleap.io/account", BrandOverleap))
	assert.True(t, isAllowedRedirect("https://www.overleap.io/x", BrandOverleap))
	assert.False(t, isAllowedRedirect("https://kaitu.io/account", BrandOverleap))
	// 通用拒绝
	assert.False(t, isAllowedRedirect("http://kaitu.io/x", BrandKaitu))      // 非 https
	assert.False(t, isAllowedRedirect("https://evilkaitu.io/x", BrandKaitu)) // 后缀伪造
	assert.False(t, isAllowedRedirect("https://kaitu.io.evil.com/x", BrandKaitu))
}

func TestCORSAllowsBothBrandOrigins(t *testing.T) {
	origins := corsAllowedOrigins()
	assert.True(t, origins["https://www.kaitu.io"])
	assert.True(t, origins["https://kaitu.io"])
	assert.True(t, origins["https://www.overleap.io"])
	assert.True(t, origins["https://overleap.io"])
	assert.True(t, origins["http://localhost:3000"])
	assert.False(t, origins["https://evil.com"])
}
