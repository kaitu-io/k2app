package center

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"text/template"

	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/mail"
	"github.com/wordgate/qtoolkit/util"
)

// EmailTemplate 邮件模板
type EmailTemplate[T any] struct {
	Subject string
	Body    string
}

// 邮件模板定义
var (
	deviceKickTemplate = EmailTemplate[DeviceKickMeta]{
		Subject: "设备已被移除通知",
		Body: `您好：

您的设备 {{.Remark}} 已被系统移除。

详细信息：
- 移除时间：{{.KickTime}}
- 移除原因：设备数量超过限制

如有疑问，请联系客服。

此致
系统通知`,
	}

	verificationCodeTemplate = EmailTemplate[VerificationCodeMeta]{
		Subject: "登录验证码",
		Body: `尊敬的 {{.UserEmail}}：

您的登录验证码是：{{.Code}}

验证码有效期为 {{.ExpireMinutes}} 分钟，请勿将验证码泄露给他人。

此致
系统通知`,
	}

	newDeviceLoginTemplate = EmailTemplate[NewDeviceLoginMeta]{
		Subject: "新设备登录提醒",
		Body: `尊敬的用户：

检测到您的账号在新设备上登录。

详细信息：
- 登录时间：{{.LoginTime}}
- 设备备注：{{.Remark}}

如非本人操作，请立即修改密码。

此致
系统通知`,
	}

	webLoginTemplate = EmailTemplate[WebLoginMeta]{
		Subject: "Web管理后台登录通知",
		Body: `尊敬的用户：

您的账号已通过Web管理后台登录。

详细信息：
- 登录时间：{{.LoginTime}}
- 登录IP：{{.ClientIP}}
- 登录方式：Web管理后台

如非本人操作，请立即联系管理员。

此致
系统通知`,
	}

	deviceTransferTemplate = EmailTemplate[DeviceTransferMeta]{
		Subject: "设备转移通知",
		Body: `尊敬的用户：

您的设备已被转移到其他账号。

详细信息：
- 转移时间：{{.TransferTime}}
- 设备备注：{{.DeviceRemark}}

如非本人操作，请立即联系客服。

此致
系统通知`,
	}

	passwordLoginTemplate = EmailTemplate[PasswordLoginMeta]{
		Subject: "Kaitu 账号登录提醒",
		Body: `尊敬的用户：

您的账号刚刚通过密码在新设备上登录。

详细信息：
- 设备: {{.DeviceName}}
- 平台: {{.Platform}}
- IP: {{.ClientIP}}
- 时间: {{.LoginTime}}

如果这不是您本人操作，请立即修改密码。

此致
系统通知`,
	}

	passwordChangedTemplate = EmailTemplate[PasswordChangedMeta]{
		Subject: "Kaitu 账号密码已修改",
		Body: `尊敬的用户：

您的 Kaitu 账号密码刚刚已更新。

详细信息：
- 修改时间：{{.ChangeTime}}
- 来源 IP：{{.ClientIP}}

如果这不是您本人操作，请立即联系客服重置账号。

此致
系统通知`,
	}

	delegatePayInviteTemplate = EmailTemplate[DelegatePayInviteMeta]{
		Subject: "{{.InviterEmail}} 请你帮忙代付一下 Kaitu 会员",
		Body: `你好，

{{.InviterEmail}} 想请你帮忙代付一下 Kaitu 会员，希望你愿意 🙏

订单：{{.PlanName}}
金额：{{.Amount}}

付款链接：
{{.PayUrl}}

链接是 Stripe 安全支付页。付完以后 {{.InviterEmail}} 会立刻收到会员时长。

如果你不认识 {{.InviterEmail}}，忽略这封邮件就好，不会扣任何费用。

谢谢。

—— Kaitu`,
	}

	adminResetPasswordTemplate = EmailTemplate[AdminResetPasswordMeta]{
		Subject: "Kaitu 账号密码已被管理员重置",
		Body: `尊敬的用户：

您的 Kaitu 账号密码刚刚被管理员重置。

详细信息：
- 操作时间：{{.ChangeTime}}
- 操作人：{{if .AdminEmail}}{{.AdminEmail}}{{else}}（系统管理员）{{end}}

如果您不知情，或这并非您主动联系客服请求的操作，请立即联系我们的客服。

此致
系统通知`,
	}
)

// DeviceKickMeta 设备踢除邮件元数据
type DeviceKickMeta struct {
	Remark   string
	KickTime string
}

// VerificationCodeMeta 验证码邮件元数据
type VerificationCodeMeta struct {
	UserEmail     string
	Code          string
	ExpireMinutes int
}

// NewDeviceLoginMeta 新设备登录邮件元数据
type NewDeviceLoginMeta struct {
	LoginTime string
	Remark    string
}

// WebLoginMeta Web登录邮件元数据
type WebLoginMeta struct {
	LoginTime string
	ClientIP  string
}

// DeviceTransferMeta 设备转移邮件元数据
type DeviceTransferMeta struct {
	TransferTime string
	DeviceRemark string
}

// PasswordLoginMeta password login notification email metadata
type PasswordLoginMeta struct {
	DeviceName string
	Platform   string
	ClientIP   string
	LoginTime  string
}

// PasswordChangedMeta is the template payload for password change notifications.
type PasswordChangedMeta struct {
	ChangeTime string
	ClientIP   string
}

// AdminResetPasswordMeta 管理员代为重置密码邮件元数据
type AdminResetPasswordMeta struct {
	ChangeTime string
	AdminEmail string // 可能为空字符串：解密失败 / admin 无邮箱身份
}

// DelegatePayInviteMeta 代付邀请邮件元数据
type DelegatePayInviteMeta struct {
	InviterEmail string
	PlanName     string
	Amount       string // pre-formatted, e.g. "$49.90"
	PayUrl       string
}

// emailToUser 发送邮件到用户：**收件人品牌只解析一次，同时决定文案与发件身份**。
//
// 形参是 brandedEmailTemplate 而不是 EmailTemplate，这是一道编译期的门——把单
// 品牌模板交给面向用户的通知，代码根本编不过。这道门之前不存在：发件身份走
// brandOfUser，文案却由调用方各自 .For(user.Brand) 选，两个真相源。
// adminResetPasswordTemplate 就是从这条缝里漏出去的——Overleap 用户被管理员
// 重置密码后，收到的是一封中文的「Kaitu 账号密码已被管理员重置」。
//
// 功能本身就只存在于单一品牌的通知走 emailToUserSingleBrand。
func emailToUser[T any](ctx context.Context, userID int64, bt brandedEmailTemplate[T], meta T) error {
	b, email, err := resolveUserMailTarget(ctx, userID)
	if err != nil {
		return err
	}
	tmpl := bt.For(b)
	log.Infof(ctx, "sending email to user %d (brand=%s) with template subject: %s", userID, b, tmpl.Subject)
	return emailTo(ctx, b, email, tmpl, meta)
}

// emailToUserSingleBrand 是 emailToUser 的逃生舱：**功能本身**只存在于一个品牌，
// 因而没有别的品牌的文案可选（专属线路是 kaitu 独有产品，别的品牌既没有
// PrivateNodeSubscription 记录也没有入口）。发件身份仍按收件人品牌解析——文案
// 单品牌不等于发件人可以错。调用处必须写清「别的品牌为什么到不了这里」。
func emailToUserSingleBrand[T any](ctx context.Context, userID int64, tmpl EmailTemplate[T], meta T) error {
	b, email, err := resolveUserMailTarget(ctx, userID)
	if err != nil {
		return err
	}
	log.Infof(ctx, "sending single-brand email to user %d (brand=%s) with template subject: %s", userID, b, tmpl.Subject)
	return emailTo(ctx, b, email, tmpl, meta)
}

// resolveUserMailTarget 解出用户的收件地址与品牌，是上面两个入口唯一的取数处。
func resolveUserMailTarget(ctx context.Context, userID int64) (Brand, string, error) {
	// 从 identify 获取用户邮箱
	identify, err := GetEmailIdentifyByUserID(ctx, userID)
	if util.DbIsNotFoundErr(err) || identify == nil || identify.EncryptedValue == "" {
		log.Warnf(ctx, "user %d has no email address, cannot send email", userID)
		return "", "", fmt.Errorf("user has no email address")
	}
	if err != nil {
		log.Errorf(ctx, "failed to get user identify for user %d: %v", userID, err)
		return "", "", fmt.Errorf("failed to get user identify: %v", err)
	}
	decEmail, err := secretDecryptString(ctx, identify.EncryptedValue)
	if err != nil {
		log.Errorf(ctx, "failed to decrypt email for user %d: %v", userID, err)
		return "", "", fmt.Errorf("failed to decrypt email for user %d: %v", userID, err)
	}
	return brandOfUser(ctx, uint64(userID)), decEmail, nil
}

// emailTo 发送邮件到邮箱。b 决定发件身份（systemSenderForBrand），与模板选择相互独立：
// 模板选错只是文案错，发件人选错会让 Overleap 用户收到 kaitu 域名的邮件。
func emailTo[T any](ctx context.Context, b Brand, email string, tmpl EmailTemplate[T], meta T) error {
	log.Debugf(ctx, "preparing to send email to %s", hideEmail(email))
	// 解析主题模板
	subjectTmpl, err := template.New("subject").Parse(tmpl.Subject)
	if err != nil {
		log.Errorf(ctx, "failed to parse subject template '%s': %v", tmpl.Subject, err)
		return fmt.Errorf("failed to parse subject template: %v", err)
	}

	// 解析正文模板
	bodyTmpl, err := template.New("body").Parse(tmpl.Body)
	if err != nil {
		log.Errorf(ctx, "failed to parse body template for subject '%s': %v", tmpl.Subject, err)
		return fmt.Errorf("failed to parse body template: %v", err)
	}

	// 渲染主题
	var subjectBuf bytes.Buffer
	if err := subjectTmpl.Execute(&subjectBuf, meta); err != nil {
		log.Errorf(ctx, "failed to render subject '%s' for email to %s: %v", tmpl.Subject, hideEmail(email), err)
		return fmt.Errorf("failed to render subject: %v", err)
	}

	// 渲染正文
	var bodyBuf bytes.Buffer
	if err := bodyTmpl.Execute(&bodyBuf, meta); err != nil {
		log.Errorf(ctx, "failed to render body for subject '%s', email to %s: %v", tmpl.Subject, hideEmail(email), err)
		return fmt.Errorf("failed to render body: %v", err)
	}

	// 按品牌选发件身份（kaitu = 全局 mail.send_from；overleap = mail_overleap.send_from）
	log.Infof(ctx, "sending email with subject '%s' to %s", subjectBuf.String(), hideEmail(email))
	err = sendSystemEmailAs(ctx, b, email, subjectBuf.String(), bodyBuf.String())
	if err != nil {
		log.Errorf(ctx, "failed to send email with subject '%s' to %s: %v", subjectBuf.String(), hideEmail(email), err)
	}
	return err
}

// isMailDevMode 检查是否为邮件开发模式（仅打印日志不发送）
// 通过 config.yml 中 mail.dev_mode: true 控制
func isMailDevMode() bool {
	return viper.GetBool("mail.dev_mode")
}

// systemSenderOverleap sends transactional mail (codes, login alerts, password
// notices, ticket replies) under the Overleap identity, bound to the
// "mail_overleap" viper prefix — same shape as edm_overleap in
// logic_email_task.go. qtoolkit/mail.Message has no From field, so a branded
// From can only come from a second prefix.
var systemSenderOverleap = mail.Config("mail_overleap")

// systemSenderForBrand returns the brand's transactional sender, or nil for
// "use the global mail.* prefix". Overleap falls back to nil until ops sets
// mail_overleap.send_from (SES domain verification for overleap.io) — fail-open
// so registration codes keep flowing under the shared identity meanwhile.
// Unknown brands get kaitu semantics (same fallback rule as Brand.Config()).
func systemSenderForBrand(b Brand) *mail.Sender {
	if b == BrandOverleap && viper.GetString("mail_overleap.send_from") != "" {
		return systemSenderOverleap
	}
	return nil
}

// overleapSenderFallbackWarnOnce 让 "overleap 回落全局发件人" 只在进程生命周期里
// 告警一次：这是部署清单里的已知状态，不是每封邮件都值得一条 warn。
var overleapSenderFallbackWarnOnce sync.Once

// MailSend 统一邮件发送入口，dev 模式下仅打印日志
// 所有 mail.Send() 调用都应使用此函数替代
func MailSend(ctx context.Context, msg *mail.Message) error {
	return MailSendWith(ctx, nil, msg)
}

// MailSendWith is MailSend with an explicit sender (nil = global mail.Send).
// dev_mode is checked before the sender is touched, so both identities are
// equally inert in development.
func MailSendWith(ctx context.Context, sender *mail.Sender, msg *mail.Message) error {
	if isMailDevMode() {
		log.Infof(ctx, "[DEV-MAIL] TO: %s | SUBJECT: %s\n--- BODY ---\n%s\n--- END ---",
			msg.To, msg.Subject, msg.Body)
		return nil
	}
	if sender != nil {
		return sender.Send(msg)
	}
	return mail.Send(msg)
}

// sendSystemEmailAs 发送系统纯文本邮件（验证码、通知等），发件身份按品牌选择：
// kaitu 走全局 mail.* 前缀（行为与历史 sendSystemEmail 完全一致），overleap 走
// mail_overleap.* 前缀，未配置时回落全局并一次性告警。
func sendSystemEmailAs(ctx context.Context, b Brand, to, subject, body string) error {
	log.Debugf(ctx, "sending plain text system email to %s as brand %s", hideEmail(to), b)

	sender := systemSenderForBrand(b)
	if b == BrandOverleap && sender == nil {
		overleapSenderFallbackWarnOnce.Do(func() {
			log.Warnf(ctx, "mail_overleap.send_from not configured; overleap system mail falls back to the global mail.* sender")
		})
	}

	err := MailSendWith(ctx, sender, &mail.Message{
		To:      to,
		Subject: subject,
		Body:    body,
	})
	if err != nil {
		log.Errorf(ctx, "failed to send system email to %s: %v", hideEmail(to), err)
		return fmt.Errorf("failed to send system email: %w", err)
	}

	log.Infof(ctx, "plain text system email sent to %s", hideEmail(to))
	return nil
}

// SendSystemEmail 公开的发送系统邮件函数（供 CLI 命令使用）
// 发送自定义纯文本邮件到指定地址；CLI 是运维工具，恒用 kaitu（全局）发件身份。
func SendSystemEmail(ctx context.Context, to, subject, textBody string) error {
	return sendSystemEmailAs(ctx, BrandKaitu, to, subject, textBody)
}
