package center

import (
	"context"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// Overleap 品牌英文邮件模板。
//
// Phase 1 覆盖高频 6 类系统邮件（验证码 / 新设备登录 / web 登录确认 / 设备转移 /
// 密码登录码 / 密码已修改）。以下模板 Phase 1 保持 kaitu-only，不做 branded 变体
// ——其功能入口本身已被品牌 gate / 渠道锁挡住，overleap 用户不可达：
//   - delegatePayInviteTemplate — 代付邀请，PaymentChannels 目前不含 overleap 支付渠道
//   - adminResetPasswordTemplate — 管理员代重置密码，admin 专属操作
//   - privateNode* 系列（专属线路相关模板）— 专属节点是 kaitu 专属产品
//
// kaitu 模板字节不变的保证：brandedEmailTemplate[T].Kaitu 直接复用
// logic_email.go 中既有的包变量，For(BrandKaitu) 原样返回，不做任何转换。
type brandedEmailTemplate[T any] struct {
	Kaitu    EmailTemplate[T]
	Overleap EmailTemplate[T]
}

// For 按品牌选择模板；未知品牌回退 kaitu（与 Brand.Config() 的回退语义一致）。
func (bt brandedEmailTemplate[T]) For(b Brand) EmailTemplate[T] {
	if b == BrandOverleap {
		return bt.Overleap
	}
	return bt.Kaitu
}

var brandedVerificationCodeTemplate = brandedEmailTemplate[VerificationCodeMeta]{
	Kaitu: verificationCodeTemplate, // logic_email.go 既有中文模板原样复用
	Overleap: EmailTemplate[VerificationCodeMeta]{
		Subject: "Your Overleap verification code",
		Body: `Hi {{.UserEmail}},

Your verification code is: {{.Code}}

It expires in {{.ExpireMinutes}} minutes. Never share this code with anyone.

— The Overleap Team
support@overleap.io`,
	},
}

var brandedNewDeviceLoginTemplate = brandedEmailTemplate[NewDeviceLoginMeta]{
	Kaitu: newDeviceLoginTemplate,
	Overleap: EmailTemplate[NewDeviceLoginMeta]{
		Subject: "New device login detected",
		Body: `Hi there,

We detected a login to your account from a new device.

Details:
- Login time: {{.LoginTime}}
- Device: {{.Remark}}

If this wasn't you, please change your password immediately.

— The Overleap Team`,
	},
}

var brandedWebLoginTemplate = brandedEmailTemplate[WebLoginMeta]{
	Kaitu: webLoginTemplate,
	Overleap: EmailTemplate[WebLoginMeta]{
		Subject: "Web dashboard login notification",
		Body: `Hi there,

Your account was just signed in via the web dashboard.

Details:
- Login time: {{.LoginTime}}
- Login IP: {{.ClientIP}}
- Method: Web dashboard

If this wasn't you, please contact your administrator immediately.

— The Overleap Team`,
	},
}

var brandedDeviceTransferTemplate = brandedEmailTemplate[DeviceTransferMeta]{
	Kaitu: deviceTransferTemplate,
	Overleap: EmailTemplate[DeviceTransferMeta]{
		Subject: "Device transfer notification",
		Body: `Hi there,

One of your devices has been transferred to another account.

Details:
- Transfer time: {{.TransferTime}}
- Device: {{.DeviceRemark}}

If this wasn't you, please contact support immediately.

— The Overleap Team`,
	},
}

var brandedPasswordLoginTemplate = brandedEmailTemplate[PasswordLoginMeta]{
	Kaitu: passwordLoginTemplate,
	Overleap: EmailTemplate[PasswordLoginMeta]{
		Subject: "Overleap account login alert",
		Body: `Hi there,

Your account was just signed in with your password on a new device.

Details:
- Device: {{.DeviceName}}
- Platform: {{.Platform}}
- IP: {{.ClientIP}}
- Time: {{.LoginTime}}

If this wasn't you, please change your password immediately.

— The Overleap Team`,
	},
}

var brandedPasswordChangedTemplate = brandedEmailTemplate[PasswordChangedMeta]{
	Kaitu: passwordChangedTemplate,
	Overleap: EmailTemplate[PasswordChangedMeta]{
		Subject: "Your Overleap account password was changed",
		Body: `Hi there,

Your Overleap account password was just updated.

Details:
- Changed at: {{.ChangeTime}}
- Source IP: {{.ClientIP}}

If this wasn't you, please contact support immediately to reset your account.

— The Overleap Team`,
	},
}

// overleapTemplateCorpus 汇总全部 overleap 模板的 Subject+Body，供
// TestOverleapTemplatesNoChineseBrandLeak 逐一断言零中文品牌泄漏。
func overleapTemplateCorpus() map[string]string {
	return map[string]string{
		"verification":    brandedVerificationCodeTemplate.Overleap.Subject + brandedVerificationCodeTemplate.Overleap.Body,
		"newDeviceLogin":  brandedNewDeviceLoginTemplate.Overleap.Subject + brandedNewDeviceLoginTemplate.Overleap.Body,
		"webLogin":        brandedWebLoginTemplate.Overleap.Subject + brandedWebLoginTemplate.Overleap.Body,
		"deviceTransfer":  brandedDeviceTransferTemplate.Overleap.Subject + brandedDeviceTransferTemplate.Overleap.Body,
		"passwordLogin":   brandedPasswordLoginTemplate.Overleap.Subject + brandedPasswordLoginTemplate.Overleap.Body,
		"passwordChanged": brandedPasswordChangedTemplate.Overleap.Subject + brandedPasswordChangedTemplate.Overleap.Body,
	}
}

// brandOfUser 按用户 ID 查其 brand，用于收件人不是当前请求已认证用户的通知场景
// （例如设备转移通知发给设备的“原所有者”，而非本次登录的用户）。查询失败一律回退
// BrandKaitu（与 Brand.Config() 的未知值回退语义一致），绝不因品牌查询失败而阻断通知。
func brandOfUser(ctx context.Context, userID uint64) Brand {
	var u User
	if err := db.Get().Select("brand").First(&u, userID).Error; err != nil {
		log.Warnf(ctx, "brandOfUser: failed to look up brand for user %d, defaulting to kaitu: %v", userID, err)
		return BrandKaitu
	}
	return Brand(u.Brand)
}
