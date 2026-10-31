package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	netmail "net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/mail"
	"github.com/wordgate/qtoolkit/redis"
	"github.com/wordgate/qtoolkit/slack"
)

// 系统邮件双通道。设计见 docs/superpowers/specs/2026-10-31-mail-dual-channel-design.md。
//
// 硬规则：通道只在同一品牌内切换。某品牌缺配置时不发并报警，绝不借用其他品牌的
// 发件身份——模板选错只是文案错，发件人串了就是品牌互相提及。

// mailAlertWindowSeconds 同一品牌同一通道的报警间隔，防止一次故障刷屏。
const mailAlertWindowSeconds = 600

// mailSendTimeout 单次发送的上限。gomail 只有 10 秒拨号超时，连上后没有读写超时，
// 服务端挂起时不设上限就永远切不到另一个通道。变量是为了测试能缩短。
var mailSendTimeout = 20 * time.Second

// mailChannel 是一个发信通道，对应一个 viper 配置前缀。
type mailChannel struct {
	name   string // 报警和日志里显示的名字
	prefix string
}

// configProblem 返回通道配置的问题，没有问题时返回空串。
//
// gomail 会把含非 ASCII 字符的 From 整串编码，地址也被编进去，然后解析失败。
// 所以中文显示名必须写成 RFC 2047 形式：
// "=?UTF-8?B?5byA6YCU?= <noreply@notify.kaitu.io>"（「开途」）。
func (c mailChannel) configProblem() string {
	from := viper.GetString(c.prefix + ".send_from")
	if from == "" {
		return fmt.Sprintf("通道未配置（%s.send_from 为空）", c.prefix)
	}
	if strings.IndexFunc(from, func(r rune) bool { return r > 127 }) >= 0 {
		return fmt.Sprintf("%s.send_from 含非 ASCII 字符，显示名要写成 RFC 2047 编码", c.prefix)
	}
	return ""
}

// mailChannelsFor 返回品牌的主备通道。未知品牌按 kaitu 处理，与 Brand.Config() 一致。
func mailChannelsFor(b Brand) (primary, backup mailChannel) {
	if b == BrandOverleap {
		return mailChannel{"primary", "mail_overleap"}, mailChannel{"backup", "mail_backup_overleap"}
	}
	return mailChannel{"primary", "mail"}, mailChannel{"backup", "mail_backup"}
}

// 测试接缝，生产恒为下面的实现。
var (
	mailChannelSend = func(ch mailChannel, msg *mail.Message) error {
		return mail.Config(ch.prefix).Send(msg)
	}
	mailAlertNotify = func(text string) error {
		return slack.Send("alert", text)
	}
)

type mailBackupFirstKey struct{}

// mailContextForCode 决定验证码走哪个通道：同一邮箱在验证码有效期内，奇数次通道一
// 在前，偶数次通道二在前。这样能覆盖「已接收但没送达」——这种情况发送方不报错，
// 只有用户点重发才知道。Redis 出错时走默认顺序，不阻断发信。
func mailContextForCode(ctx context.Context, b Brand, email string) context.Context {
	if isMailDevMode() {
		return ctx // redis.Client() 未配置时会 panic，而 dev 模式本来就不碰通道
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	key := fmt.Sprintf("mail:code_send:%s:%s", b, hex.EncodeToString(sum[:]))
	client := redis.Client()
	n, err := client.Incr(ctx, key).Result()
	if err != nil {
		log.Warnf(ctx, "mail code send counter failed, using default channel order: %v", err)
		return ctx
	}
	if err := client.Expire(ctx, key, time.Duration(VerificationCodeExpiry)*time.Second).Err(); err != nil {
		log.Warnf(ctx, "mail code send counter expire failed: %v", err)
	}
	if n%2 == 0 {
		return context.WithValue(ctx, mailBackupFirstKey{}, true)
	}
	return ctx
}

// errMailAllChannelsFailed 不带供应商错误和配置前缀：验证码接口会把错误文本返回给
// 客户端，细节只进日志和报警。
var errMailAllChannelsFailed = errors.New("all mail channels failed")

// sendMailWithFailover 是系统邮件的唯一出口：一个通道失败就换另一个并报警，
// 两个都失败才返回错误。dev_mode 下只打印，不碰任何通道。
func sendMailWithFailover(ctx context.Context, b Brand, msg *mail.Message) error {
	if isMailDevMode() {
		log.Infof(ctx, "[DEV-MAIL] TO: %s | SUBJECT: %s\n--- BODY ---\n%s\n--- END ---",
			msg.To, msg.Subject, msg.Body)
		return nil
	}

	// 收件地址语法错误换通道也没用。这里先拦，gomail 的报错分不清是 From 还是 To。
	if err := validateMailRecipient(msg.To); err != nil {
		return err
	}

	first, second := mailChannelsFor(b)
	if backupFirst, _ := ctx.Value(mailBackupFirstKey{}).(bool); backupFirst {
		first, second = second, first
	}

	var failed error
	var failedChannel mailChannel
	for _, ch := range []mailChannel{first, second} {
		if problem := ch.configProblem(); problem != "" {
			alertMailFailure(ctx, b, ch.name, problem)
			if failed == nil {
				failed, failedChannel = fmt.Errorf("mail channel %s unusable", ch.prefix), ch
			}
			continue
		}
		err := sendWithTimeout(func() error { return mailChannelSend(ch, msg) })
		if err == nil {
			if failed != nil {
				alertMailFailure(ctx, b, failedChannel.name, fmt.Sprintf(
					"通道失败，邮件已由 %s 发出：%s", ch.name, redactMailError(failed)))
			}
			return nil
		}
		// 收件人自己的问题换通道也没用，也不是我们的故障。
		if isMailRecipientError(err) {
			// 前一个通道的失败不能因为这封信的收件人有问题就被吞掉。
			if failed != nil {
				alertMailFailure(ctx, b, failedChannel.name, "通道失败："+redactMailError(failed))
			}
			return errors.New(redactMailError(err))
		}
		log.Errorf(ctx, "mail channel %s failed (brand=%s): %s", ch.prefix, b, redactMailError(err))
		failed, failedChannel = errors.Join(failed, err), ch
	}

	detail := redactMailError(failed)
	log.Errorf(ctx, "all mail channels failed (brand=%s): %s", b, detail)
	alertMailFailure(ctx, b, "all", "两个通道都失败，邮件没有发出："+detail)
	return errMailAllChannelsFailed
}

// validateMailRecipient 检查收件地址语法，空地址交给 qtoolkit 的校验报错。
func validateMailRecipient(to string) error {
	if to == "" {
		return nil
	}
	if _, err := netmail.ParseAddress(to); err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}
	return nil
}

// sendWithTimeout 给一次发送加上限。超时后后台的发送不能取消，那封信可能仍会送达，
// 用户最多收到两封，好过收不到。
func sendWithTimeout(send func() error) error {
	done := make(chan error, 1)
	go func() {
		// 这里在 gin 和 asynq 的 recover 之外，panic 会打崩整个进程。
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("mail send panicked: %v", r)
			}
		}()
		done <- send()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(mailSendTimeout):
		return fmt.Errorf("mail send timed out after %s", mailSendTimeout)
	}
}

// alertMailFailure 发 Slack 报警。限频键写失败时照发，宁可多发也不能漏。
// Slack 失败只写日志，不影响发信结果。
func alertMailFailure(ctx context.Context, b Brand, channel, detail string) {
	acquired, err := redis.TryLock(fmt.Sprintf("mail:alert:%s:%s", b, channel), mailAlertWindowSeconds)
	if err == nil && !acquired {
		return
	}
	text := fmt.Sprintf(":rotating_light: *邮件发送异常*\n品牌：%s\n通道：%s\n%s\n_%s_",
		b, channel, detail, time.Now().Format("2006-01-02 15:04:05 MST"))
	if err := mailAlertNotify(text); err != nil {
		log.Errorf(ctx, "failed to send mail alert: %v", err)
	}
}

var mailAddressPattern = regexp.MustCompile(`[\w.+-]+@[\w-]+(\.[\w-]+)+`)

// redactMailError 去掉错误里的邮箱地址：报警和日志不留收件人明文。
func redactMailError(err error) string {
	return mailAddressPattern.ReplaceAllString(err.Error(), "***")
}

var (
	mailRecipientErrPattern = regexp.MustCompile(`(?i)\b(501|550|553|554)\b.*(recipient|rcpt|mailbox|no such user|user unknown|invalid address)`)
	mailAccountErrPattern   = regexp.MustCompile(`(?i)sender|account|quota|domain|auth`)
)

// isMailRecipientError 判断是否单收件人错误。判断不了的一律按账号级处理（切换并
// 报警）：把账号级故障误判成收件人错误，等于重演这次没有报警的事故。
// 注意 551 "Main account unavailable" 是欠费，必须落在 false。
func isMailRecipientError(err error) bool {
	s := err.Error()
	if strings.Contains(s, "recipient (To) is required") || strings.Contains(s, "subject is required") {
		return true
	}
	return mailRecipientErrPattern.MatchString(s) && !mailAccountErrPattern.MatchString(s)
}
