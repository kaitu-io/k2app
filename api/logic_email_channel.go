package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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

// mailChannel 是一个发信通道，对应一个 viper 配置前缀。
type mailChannel struct {
	name   string // 报警和日志里显示的名字
	prefix string
}

func (c mailChannel) configured() bool {
	return viper.GetString(c.prefix+".send_from") != ""
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

// sendMailWithFailover 是系统邮件的唯一出口：一个通道失败就换另一个并报警，
// 两个都失败才返回错误。dev_mode 下只打印，不碰任何通道。
func sendMailWithFailover(ctx context.Context, b Brand, msg *mail.Message) error {
	if isMailDevMode() {
		log.Infof(ctx, "[DEV-MAIL] TO: %s | SUBJECT: %s\n--- BODY ---\n%s\n--- END ---",
			msg.To, msg.Subject, msg.Body)
		return nil
	}

	first, second := mailChannelsFor(b)
	if backupFirst, _ := ctx.Value(mailBackupFirstKey{}).(bool); backupFirst {
		first, second = second, first
	}

	var failed error
	var failedChannel mailChannel
	for _, ch := range []mailChannel{first, second} {
		if !ch.configured() {
			alertMailFailure(ctx, b, ch.name, fmt.Sprintf("通道未配置（%s.send_from 为空）", ch.prefix))
			if failed == nil {
				failed, failedChannel = fmt.Errorf("mail channel %s not configured", ch.prefix), ch
			}
			continue
		}
		err := mailChannelSend(ch, msg)
		if err == nil {
			if failed != nil {
				alertMailFailure(ctx, b, failedChannel.name, fmt.Sprintf(
					"通道失败，邮件已由 %s 发出：%s", ch.name, redactMailError(failed)))
			}
			return nil
		}
		// 收件人自己的问题换通道也没用，也不是我们的故障。
		if isMailRecipientError(err) {
			return err
		}
		log.Errorf(ctx, "mail channel %s failed (brand=%s): %s", ch.prefix, b, redactMailError(err))
		failed, failedChannel = errors.Join(failed, err), ch
	}

	alertMailFailure(ctx, b, "all", "两个通道都失败，邮件没有发出："+redactMailError(failed))
	return failed
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
