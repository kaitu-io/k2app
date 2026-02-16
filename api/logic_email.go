package center

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

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

	memberAddedTemplate = EmailTemplate[MemberAddedMeta]{
		Subject: "您已被添加为代付成员",
		Body: `尊敬的用户：

您已被 {{.DelegateEmail}} 添加为代付成员，对方将为您的账户付费。

详细信息：
- 代付人邮箱：{{.DelegateEmail}}
- 添加时间：{{.AddedTime}}

如果您不认识此用户或不希望对方为您付费，您可以：
1. 登录账户管理页面
2. 进入"代付管理"页面
3. 点击"拒绝代付"按钮

拒绝代付链接：{{.RejectURL}}

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
	TransferTime  string
	DeviceRemark  string
}

// MemberAddedMeta 被添加为成员邮件元数据
type MemberAddedMeta struct {
	DelegateEmail string
	AddedTime     string
	RejectURL     string
}

// PasswordLoginMeta password login notification email metadata
type PasswordLoginMeta struct {
	DeviceName string
	Platform   string
	ClientIP   string
	LoginTime  string
}

// emailToUser 发送邮件到用户
func emailToUser[T any](ctx context.Context, userID int64, tmpl EmailTemplate[T], meta T) error {
	log.Infof(ctx, "sending email to user %d with template subject: %s", userID, tmpl.Subject)
	// 从 identify 获取用户邮箱
	identify, err := GetEmailIdentifyByUserID(ctx, userID)
	if util.DbIsNotFoundErr(err) || identify == nil || identify.EncryptedValue == "" {
		log.Warnf(ctx, "user %d has no email address, cannot send email", userID)
		return fmt.Errorf("user has no email address")
	}
	if err != nil {
		log.Errorf(ctx, "failed to get user identify for user %d: %v", userID, err)
		return fmt.Errorf("failed to get user identify: %v", err)
	}
	decEmail, err := secretDecryptString(ctx, identify.EncryptedValue)
	if err != nil {
		log.Errorf(ctx, "failed to decrypt email for user %d: %v", userID, err)
		return fmt.Errorf("failed to decrypt email for user %d: %v", userID, err)
	}
	return emailTo(ctx, decEmail, tmpl, meta)
}

// emailTo 发送邮件到邮箱
func emailTo[T any](ctx context.Context, email string, tmpl EmailTemplate[T], meta T) error {
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

	// 使用通用邮件配置发送邮件（mail.send_from）
	log.Infof(ctx, "sending email with subject '%s' to %s", subjectBuf.String(), hideEmail(email))
	err = sendSystemEmail(ctx, email, subjectBuf.String(), bodyBuf.String())
	if err != nil {
		log.Errorf(ctx, "failed to send email with subject '%s' to %s: %v", subjectBuf.String(), hideEmail(email), err)
	}
	return err
}

// sendSystemEmail 发送系统纯文本邮件（验证码、通知等）
// 使用 qtoolkit/mail，自动从 viper 配置中读取 mail.* 配置
func sendSystemEmail(ctx context.Context, to, subject, body string) error {
	log.Debugf(ctx, "sending plain text system email to %s", hideEmail(to))

	// 使用 qtoolkit/mail 包的 Send 函数
	// 它会自动从 viper 读取 mail.* 配置（mail.send_from, mail.smtp_host 等）
	err := mail.Send(&mail.Message{
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
// 发送自定义纯文本邮件到指定地址
func SendSystemEmail(ctx context.Context, to, subject, textBody string) error {
	return sendSystemEmail(ctx, to, subject, textBody)
}
