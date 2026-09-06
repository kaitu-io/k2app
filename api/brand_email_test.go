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

func TestBrandedDeviceKickAndTicketNotify(t *testing.T) {
	assert.Equal(t, deviceKickTemplate, brandedDeviceKickTemplate.For(BrandKaitu))
	o := brandedDeviceKickTemplate.For(BrandOverleap)
	assert.Contains(t, o.Subject, "signed out")
	assert.NotContains(t, o.Body, "开途")

	sK, bK := ticketReplyNotification(BrandKaitu, 42, "[2026-09-06 10:00] Support:\nhello")
	assert.Equal(t, "[Kaitu] 您的工单有新回复 (#42)", sK)
	assert.Contains(t, bK, "请登录 Kaitu 客户端查看完整对话")
	sO, bO := ticketReplyNotification(BrandOverleap, 42, "[2026-09-06 10:00] Support:\nhello")
	assert.Equal(t, "[Overleap] New reply on your ticket (#42)", sO)
	assert.Contains(t, bO, "Open the Overleap app to view the full conversation.")
	assert.NotContains(t, bO, "Kaitu")
}

func TestTicketBrandFallsBackToKaitu(t *testing.T) {
	assert.Equal(t, BrandOverleap, ticketBrand(FeedbackTicket{Brand: "overleap"}))
	assert.Equal(t, BrandKaitu, ticketBrand(FeedbackTicket{Brand: "kaitu"}))
	assert.Equal(t, BrandKaitu, ticketBrand(FeedbackTicket{Brand: ""}), "pre-column rows carry no brand")
	assert.Equal(t, BrandKaitu, ticketBrand(FeedbackTicket{Brand: "bogus"}), "unknown brand never selects the overleap copy")
}
