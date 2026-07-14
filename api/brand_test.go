package center

import (
	"testing"

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
	assert.False(t, o.AllowsPayment("wordgate")) // Phase 6 前 overleap 渠道为空
	assert.False(t, o.AllowsPayment("stripe"))

	// 未知 brand 回退 kaitu 配置
	assert.Equal(t, BrandKaitu, Brand("nope").Config().ID)
}
