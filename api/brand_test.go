package center

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestBrandFromHost(t *testing.T) {
	cases := []struct {
		host  string
		brand Brand
		found bool
	}{
		{"kaitu.io", BrandKaitu, true},
		{"www.kaitu.io", BrandKaitu, true},
		{"WWW.KAITU.IO", BrandKaitu, true},
		{"kaitu.io:443", BrandKaitu, true},
		{"overleap.io", BrandOverleap, true},
		{"www.overleap.io", BrandOverleap, true},
		{"unknown.example.com", BrandKaitu, false}, // 未知 host 回退 kaitu 且 found=false
		{"", BrandKaitu, false},
	}
	for _, tc := range cases {
		b, ok := BrandFromHost(tc.host)
		assert.Equal(t, tc.brand, b, "host=%s", tc.host)
		assert.Equal(t, tc.found, ok, "host=%s", tc.host)
	}
}

func TestBrandValid(t *testing.T) {
	assert.True(t, BrandKaitu.Valid())
	assert.True(t, BrandOverleap.Valid())
	assert.False(t, Brand("").Valid())
	assert.False(t, Brand("KAITU").Valid()) // 大小写敏感，解析层负责 lower
}

func TestBrandConfig(t *testing.T) {
	k := BrandKaitu.Config()
	assert.Equal(t, "https://www.kaitu.io", k.BaseURL)
	assert.Equal(t, "开途", k.DisplayName)
	assert.True(t, k.AllowsPayment("wordgate"))
	assert.False(t, k.AllowsPayment("stripe"))

	o := BrandOverleap.Config()
	assert.Equal(t, "https://www.overleap.io", o.BaseURL)
	assert.Equal(t, "Overleap", o.DisplayName)
	assert.Equal(t, "support@overleap.io", o.SupportEmail)
	assert.False(t, o.AllowsPayment("wordgate")) // wordgate 恒锁 kaitu
	assert.True(t, o.AllowsPayment("stripe"))    // Phase 6 起 overleap 走 stripe

	// 未知 brand 回退 kaitu 配置
	assert.Equal(t, BrandKaitu, Brand("nope").Config().ID)
}

func TestReqBrandResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BrandResolver())
	var got Brand
	r.GET("/probe", func(c *gin.Context) { got = ReqBrand(c); c.Status(200) })

	cases := []struct {
		name   string
		host   string
		header string
		want   Brand
	}{
		{"kaitu host", "www.kaitu.io", "", BrandKaitu},
		{"overleap host", "overleap.io", "", BrandOverleap},
		{"host 优先于 header", "overleap.io", "kaitu", BrandOverleap},
		{"未知 host + overleap header", "1.2.3.4:8080", "overleap", BrandOverleap},
		{"未知 host + 大写 header", "1.2.3.4", "OVERLEAP", BrandOverleap},
		{"未知 host + 非法 header 回退 kaitu", "1.2.3.4", "evil", BrandKaitu},
		{"全空回退 kaitu（老客户端）", "10.0.0.1", "", BrandKaitu},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/probe", nil)
		req.Host = tc.host
		if tc.header != "" {
			req.Header.Set("X-K2-Brand", tc.header)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, tc.want, got, tc.name)
	}
}
