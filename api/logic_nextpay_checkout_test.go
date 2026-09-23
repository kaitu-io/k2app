package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKaituSiteLocale(t *testing.T) {
	assert.Equal(t, "zh-TW", kaituSiteLocale("zh-TW"))
	assert.Equal(t, "zh-HK", kaituSiteLocale("zh-HK"))
	assert.Equal(t, "zh-CN", kaituSiteLocale("zh-CN"))
	assert.Equal(t, "zh-CN", kaituSiteLocale("en-US"), "开途站只有三个中文 locale，其余回落 zh-CN")
	assert.Equal(t, "zh-CN", kaituSiteLocale(""))
}

func TestPayURLs(t *testing.T) {
	assert.Equal(t, "https://www.kaitu.io/zh-TW/pay-result/u-1", payResultURL("zh-TW", "u-1"))
	assert.Equal(t, "https://www.kaitu.io/api/orders/u-1/pay?src=mail", payRedirectURL("u-1"))
}
