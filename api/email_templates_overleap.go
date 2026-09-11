package center

import (
	"context"
	"fmt"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// Overleap 品牌英文邮件模板。
//
// 覆盖面由 emailToUser 的类型签名兜底：面向用户的通知一律只收
// brandedEmailTemplate，单品牌模板编译期就交不进去。所以这里的清单不是"记得补"，
// 而是"补了才编得过"。
//
// 曾经的反例正是 adminResetPasswordTemplate：它被当成"admin 专属操作，overleap
// 用户不可达"而留成 kaitu-only，但**收件人是被重置的那个用户**——后台按 uuid 找人，
// 不按品牌过滤，于是 overleap 用户收到一封中文的「Kaitu 账号密码已被管理员重置」。
// 判断可达性要看收件人，不看谁点的按钮。
//
// 仍然 kaitu-only 的两处，都是**功能本身**在 overleap 不存在，各自走显式出口：
//   - delegatePayInviteTemplate — 代付邀请，收件人是第三方付款人，emailTo 显式钉
//     BrandKaitu；PaymentChannels 不含 overleap 的 wordgate 渠道，订单生不出来。
//   - privateNodeTrafficWarn / Exhausted — 专属线路是 kaitu 独有产品，走
//     emailToUserSingleBrand（论证写在 worker_private_node_traffic_warning.go）。
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

var brandedDeviceKickTemplate = brandedEmailTemplate[DeviceKickMeta]{
	Kaitu: deviceKickTemplate,
	Overleap: EmailTemplate[DeviceKickMeta]{
		Subject: "Your device {{.Remark}} was signed out",
		Body: `Hi,

Your device "{{.Remark}}" was signed out of Overleap because your account reached its device limit.

- Time: {{.KickTime}}
- Reason: device limit exceeded

If this wasn't you, please contact support@overleap.io.

— The Overleap Team`,
	},
}

var brandedAdminResetPasswordTemplate = brandedEmailTemplate[AdminResetPasswordMeta]{
	Kaitu: adminResetPasswordTemplate,
	Overleap: EmailTemplate[AdminResetPasswordMeta]{
		Subject: "Your Overleap account password was reset by support",
		Body: `Hi there,

Your Overleap account password was just reset by our support team.

Details:
- Time: {{.ChangeTime}}
- Reset by: {{if .AdminEmail}}{{.AdminEmail}}{{else}}Overleap Support{{end}}

If you did not ask us to do this, contact support@overleap.io immediately.

— The Overleap Team`,
	},
}

// ticketReplyNotification renders the ticket-reply email for a brand.
// kaitu strings are the historical literals from worker_ticket_notify.go, unchanged.
func ticketReplyNotification(b Brand, ticketID uint64, replies string) (subject, body string) {
	if b == BrandOverleap {
		return fmt.Sprintf("[Overleap] New reply on your ticket (#%d)", ticketID),
			fmt.Sprintf("Hi,\n\nYour ticket (#%d) has a new reply:\n\n---\n%s\n---\n\nOpen the Overleap app to view the full conversation.\n", ticketID, replies)
	}
	return fmt.Sprintf("[Kaitu] 您的工单有新回复 (#%d)", ticketID),
		fmt.Sprintf("您好，\n\n您的工单 (#%d) 收到了新的回复：\n\n---\n%s\n---\n\n请登录 Kaitu 客户端查看完整对话。\n", ticketID, replies)
}

// overleapTemplateCorpus 汇总全部 overleap 文案的 Subject+Body，供
// TestOverleapTemplatesNoChineseBrandLeak 逐一断言零中文品牌泄漏。
//
// key 用包变量名，好让 TestOverleapCorpusCoversEveryBrandedTemplate 扫本文件源码
// 逐一核对——这张表是手写枚举，漏登记的模板不会有任何编译期信号。
// 已知盲区：在**别的文件**里声明的 brandedEmailTemplate，或不写成
// `var brandedXxx = brandedEmailTemplate[...]` 这一形状的声明，那个守卫看不见。
func overleapTemplateCorpus() map[string]string {
	ticketReplySubject, ticketReplyBody := ticketReplyNotification(BrandOverleap, 0, "")
	return map[string]string{
		"brandedVerificationCodeTemplate":   brandedVerificationCodeTemplate.Overleap.Subject + brandedVerificationCodeTemplate.Overleap.Body,
		"brandedNewDeviceLoginTemplate":     brandedNewDeviceLoginTemplate.Overleap.Subject + brandedNewDeviceLoginTemplate.Overleap.Body,
		"brandedWebLoginTemplate":           brandedWebLoginTemplate.Overleap.Subject + brandedWebLoginTemplate.Overleap.Body,
		"brandedDeviceTransferTemplate":     brandedDeviceTransferTemplate.Overleap.Subject + brandedDeviceTransferTemplate.Overleap.Body,
		"brandedPasswordLoginTemplate":      brandedPasswordLoginTemplate.Overleap.Subject + brandedPasswordLoginTemplate.Overleap.Body,
		"brandedPasswordChangedTemplate":    brandedPasswordChangedTemplate.Overleap.Subject + brandedPasswordChangedTemplate.Overleap.Body,
		"brandedDeviceKickTemplate":         brandedDeviceKickTemplate.Overleap.Subject + brandedDeviceKickTemplate.Overleap.Body,
		"brandedAdminResetPasswordTemplate": brandedAdminResetPasswordTemplate.Overleap.Subject + brandedAdminResetPasswordTemplate.Overleap.Body,
		"ticketReply":                       ticketReplySubject + ticketReplyBody,
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
