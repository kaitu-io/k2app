package center

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBrandedTemplateSelection(t *testing.T) {
	tpl := brandedVerificationCodeTemplate.For(BrandOverleap)
	assert.NotContains(t, tpl.Subject, "登录验证码")
	assert.Contains(t, tpl.Subject, "verification")

	tplK := brandedVerificationCodeTemplate.For(BrandKaitu)
	assert.Contains(t, tplK.Subject, "验证码")
	// 未知品牌回退 kaitu
	assert.Equal(t, tplK.Subject, brandedVerificationCodeTemplate.For(Brand("x")).Subject)
}

func TestOverleapTemplatesNoChineseBrandLeak(t *testing.T) {
	for name, body := range overleapTemplateCorpus() { // 返回全部 overleap 模板 Subject+Body 拼接
		assert.False(t, strings.Contains(body, "开途"), "%s leaks 开途", name)
		assert.False(t, strings.Contains(body, "kaitu.io"), "%s leaks kaitu.io", name)
		assert.False(t, strings.Contains(body, "Kaitu"), "%s leaks Kaitu", name)
	}
}

func TestKaituTemplateBytesUnchanged(t *testing.T) {
	// For(kaitu) must return the exact pre-existing package vars, byte for byte.
	assert.Equal(t, verificationCodeTemplate, brandedVerificationCodeTemplate.For(BrandKaitu))
	assert.Equal(t, newDeviceLoginTemplate, brandedNewDeviceLoginTemplate.For(BrandKaitu))
	assert.Equal(t, webLoginTemplate, brandedWebLoginTemplate.For(BrandKaitu))
	assert.Equal(t, deviceTransferTemplate, brandedDeviceTransferTemplate.For(BrandKaitu))
	assert.Equal(t, passwordLoginTemplate, brandedPasswordLoginTemplate.For(BrandKaitu))
	assert.Equal(t, passwordChangedTemplate, brandedPasswordChangedTemplate.For(BrandKaitu))
}
