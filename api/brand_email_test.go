package center

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Equal(t, adminResetPasswordTemplate, brandedAdminResetPasswordTemplate.For(BrandKaitu))
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

// TestBrandedAdminResetPassword 锁住管理员代重置密码的通知邮件按**收件人**品牌
// 分文案。回归背景：这封信曾被当成「admin 专属操作，overleap 用户不可达」而留成
// kaitu-only，但后台按 uuid 找人、不按品牌过滤——收件人正是被重置的那个用户，
// 于是 overleap 用户收到一封中文的「Kaitu 账号密码已被管理员重置」。
func TestBrandedAdminResetPassword(t *testing.T) {
	o := brandedAdminResetPasswordTemplate.For(BrandOverleap)
	assert.NotContains(t, o.Subject, "Kaitu")
	assert.NotContains(t, o.Body, "Kaitu")
	assert.NotContains(t, o.Subject, "密码")
	assert.Contains(t, o.Subject, "Overleap")

	render := func(meta AdminResetPasswordMeta) string {
		var buf bytes.Buffer
		tmpl, err := template.New("body").Parse(o.Body)
		require.NoError(t, err)
		require.NoError(t, tmpl.Execute(&buf, meta))
		return buf.String()
	}

	withAdmin := render(AdminResetPasswordMeta{ChangeTime: "2026-09-11 10:00:00", AdminEmail: "ops@example.com"})
	assert.Contains(t, withAdmin, "2026-09-11 10:00:00")
	assert.Contains(t, withAdmin, "ops@example.com")

	// AdminEmail 可能为空（解密失败 / admin 无邮箱身份）——回退文案也必须是英文。
	noAdmin := render(AdminResetPasswordMeta{ChangeTime: "2026-09-11 10:00:00"})
	assert.Contains(t, noAdmin, "Overleap Support")
	for _, r := range noAdmin {
		assert.False(t, r >= 0x4e00 && r <= 0x9fff, "overleap fallback body must not contain Han characters: %q", noAdmin)
	}
}

// TestOverleapCorpusCoversEveryBrandedTemplate 守住 overleapTemplateCorpus 这张
// **手写枚举**：漏登记一个模板没有任何编译期信号，中文泄漏检查就静默跳过它。
// 这里扫本文件源码里的 `var brandedXxx = brandedEmailTemplate[...]` 声明，逐一
// 核对 corpus 里有没有同名 key。
//
// 盲区（写清楚，别假装没有）：声明在**别的文件**里、或不写成这一形状的
// brandedEmailTemplate，这个守卫看不见。
func TestOverleapCorpusCoversEveryBrandedTemplate(t *testing.T) {
	src, err := os.ReadFile("email_templates_overleap.go")
	require.NoError(t, err)

	decls := regexp.MustCompile(`(?m)^var (branded\w+) = brandedEmailTemplate\[`).FindAllStringSubmatch(string(src), -1)
	require.NotEmpty(t, decls, "正则没匹配到任何声明——守卫本身失效了，不是「全都登记了」")

	corpus := overleapTemplateCorpus()
	for _, d := range decls {
		name := d[1]
		body, ok := corpus[name]
		assert.True(t, ok, "%s 未登记进 overleapTemplateCorpus——中文泄漏检查扫不到它", name)
		assert.NotEmpty(t, body, "%s 在 corpus 里是空串", name)
	}
}
