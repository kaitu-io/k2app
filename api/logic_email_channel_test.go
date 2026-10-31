package center

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/mail"
	"github.com/wordgate/qtoolkit/redis"
)

type mailHarness struct {
	sent   []string // 每次尝试的配置前缀
	fail   map[string]error
	alerts []string
}

// newMailHarness 打开真实发送路径（dev_mode=false），但把通道和 Slack 都换成桩。
func newMailHarness(t *testing.T) *mailHarness {
	t.Helper()
	testInitConfig()
	for _, k := range []string{"mail.dev_mode", "mail.send_from", "mail_backup.send_from", "mail_overleap.send_from", "mail_backup_overleap.send_from", "edm_overleap.send_from"} {
		restoreViper(t, k)
	}
	viper.Set("mail.dev_mode", false)
	viper.Set("mail.send_from", "a@kaitu.test")
	viper.Set("mail_backup.send_from", "b@kaitu.test")
	viper.Set("mail_overleap.send_from", "a@overleap.test")
	viper.Set("mail_backup_overleap.send_from", "b@overleap.test")

	h := &mailHarness{fail: map[string]error{}}
	origSend, origAlert := mailChannelSend, mailAlertNotify
	mailChannelSend = func(ch mailChannel, _ *mail.Message) error {
		h.sent = append(h.sent, ch.prefix)
		return h.fail[ch.prefix]
	}
	mailAlertNotify = func(text string) error {
		h.alerts = append(h.alerts, text)
		return nil
	}
	t.Cleanup(func() { mailChannelSend, mailAlertNotify = origSend, origAlert })
	for _, b := range []Brand{BrandKaitu, BrandOverleap} {
		for _, c := range []string{"primary", "backup", "all", "edm"} {
			require.NoError(t, redis.ReleaseLock("mail:alert:"+string(b)+":"+c))
		}
	}
	return h
}

var testMsg = &mail.Message{To: "u@example.com", Subject: "s", Body: "b"}

var errBalance = errors.New("gomail: could not send email 1: 551 Main account unavailable [@ud010102]")

func TestFailover_PrimaryOKNoAlert(t *testing.T) {
	h := newMailHarness(t)
	require.NoError(t, sendMailWithFailover(context.Background(), BrandKaitu, testMsg))
	assert.Equal(t, []string{"mail"}, h.sent)
	assert.Empty(t, h.alerts)
}

func TestFailover_BackupDeliversAndAlerts(t *testing.T) {
	h := newMailHarness(t)
	h.fail["mail"] = errBalance
	require.NoError(t, sendMailWithFailover(context.Background(), BrandKaitu, testMsg))
	assert.Equal(t, []string{"mail", "mail_backup"}, h.sent)
	require.Len(t, h.alerts, 1)
	assert.Contains(t, h.alerts[0], "已由 backup 发出")
}

func TestFailover_StaysInsideBrand(t *testing.T) {
	h := newMailHarness(t)
	h.fail["mail_overleap"] = errBalance
	h.fail["mail_backup_overleap"] = errBalance
	require.Error(t, sendMailWithFailover(context.Background(), BrandOverleap, testMsg))
	assert.Equal(t, []string{"mail_overleap", "mail_backup_overleap"}, h.sent)
}

func TestFailover_MissingConfigNeverBorrowsOtherBrand(t *testing.T) {
	h := newMailHarness(t)
	viper.Set("mail_overleap.send_from", "")
	viper.Set("mail_backup_overleap.send_from", "")
	require.Error(t, sendMailWithFailover(context.Background(), BrandOverleap, testMsg))
	assert.Empty(t, h.sent, "must not fall back to the kaitu mail.* identity")
	assert.NotEmpty(t, h.alerts)
}

func TestFailover_BothFailReturnsErrorAndAlerts(t *testing.T) {
	h := newMailHarness(t)
	h.fail["mail"], h.fail["mail_backup"] = errBalance, errBalance
	require.Error(t, sendMailWithFailover(context.Background(), BrandKaitu, testMsg))
	require.Len(t, h.alerts, 1)
	assert.Contains(t, h.alerts[0], "两个通道都失败")
}

func TestFailover_RecipientErrorNoSwitchNoAlert(t *testing.T) {
	h := newMailHarness(t)
	h.fail["mail"] = errors.New("gomail: could not send email 1: 550 no such user here")
	require.Error(t, sendMailWithFailover(context.Background(), BrandKaitu, testMsg))
	assert.Equal(t, []string{"mail"}, h.sent)
	assert.Empty(t, h.alerts)
}

func TestFailover_AlertIsRateLimited(t *testing.T) {
	h := newMailHarness(t)
	h.fail["mail"] = errBalance
	for range 3 {
		require.NoError(t, sendMailWithFailover(context.Background(), BrandKaitu, testMsg))
	}
	assert.Len(t, h.sent, 6)
	assert.Len(t, h.alerts, 1)
}

func TestFailover_AlertHasNoRecipientAddress(t *testing.T) {
	h := newMailHarness(t)
	h.fail["mail"] = errors.New("451 temporary failure for victim@example.com")
	require.NoError(t, sendMailWithFailover(context.Background(), BrandKaitu, testMsg))
	require.Len(t, h.alerts, 1)
	assert.False(t, strings.Contains(h.alerts[0], "victim@example.com"))
}

func TestFailover_DevModeTouchesNothing(t *testing.T) {
	h := newMailHarness(t)
	viper.Set("mail.dev_mode", true)
	require.NoError(t, sendMailWithFailover(context.Background(), BrandKaitu, testMsg))
	assert.Empty(t, h.sent)
}

func TestCodeResendAlternatesChannels(t *testing.T) {
	h := newMailHarness(t)
	email := "resend-" + t.Name() + "@example.com"
	for range 3 {
		ctx := mailContextForCode(context.Background(), BrandOverleap, email)
		require.NoError(t, sendMailWithFailover(ctx, BrandOverleap, testMsg))
	}
	assert.Equal(t, []string{"mail_overleap", "mail_backup_overleap", "mail_overleap"}, h.sent)
}

func TestCodeResendCounterIsPerBrandAndTimeBound(t *testing.T) {
	newMailHarness(t)
	email := "scope-" + t.Name() + "@example.com"
	mailContextForCode(context.Background(), BrandKaitu, email)
	ctx := mailContextForCode(context.Background(), BrandOverleap, email)
	assert.Nil(t, ctx.Value(mailBackupFirstKey{}), "first send for this brand")
	keys, err := redis.Client().Keys(context.Background(), "mail:code_send:overleap:*").Result()
	require.NoError(t, err)
	require.NotEmpty(t, keys)
	assert.NotContains(t, keys[0], "example.com", "key must not hold the plain address")
	ttl, err := redis.Client().TTL(context.Background(), keys[0]).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl.Seconds(), 0.0)
}

func TestIsMailRecipientError(t *testing.T) {
	for s, want := range map[string]bool{
		"551 Main account unavailable [@ud010102]": false,
		"535 Authentication failure":               false,
		"550 sender address rejected":              false,
		"554 daily quota exceeded for recipient":   false,
		"dial tcp: i/o timeout":                    false,
		"550 mailbox not found":                    true,
		"553 invalid address":                      true,
		"recipient (To) is required":               true,
	} {
		assert.Equal(t, want, isMailRecipientError(errors.New(s)), s)
	}
}

func TestSendEmail_OverleapWithoutEDMConfigRefusesAndAlerts(t *testing.T) {
	h := newMailHarness(t)
	viper.Set("edm_overleap.send_from", "")
	orig := edmChannelSend
	edmChannelSend = func(*mail.Sender, *mail.Message) error { t.Fatal("must not send"); return nil }
	t.Cleanup(func() { edmChannelSend = orig })
	require.Error(t, sendEmail(context.Background(), "u@example.com", "s", "b", BrandOverleap))
	require.Len(t, h.alerts, 1)
}

func TestSendEmail_FailureAlerts(t *testing.T) {
	h := newMailHarness(t)
	orig := edmChannelSend
	edmChannelSend = func(*mail.Sender, *mail.Message) error { return errBalance }
	t.Cleanup(func() { edmChannelSend = orig })
	require.Error(t, sendEmail(context.Background(), "u@example.com", "s", "b", BrandKaitu))
	require.Len(t, h.alerts, 1)
	assert.Contains(t, h.alerts[0], "EDM")
}

func TestSendSystemEmailAs_SetsBrandReplyTo(t *testing.T) {
	newMailHarness(t)
	var got *mail.Message
	orig := mailChannelSend
	mailChannelSend = func(_ mailChannel, m *mail.Message) error { got = m; return nil }
	t.Cleanup(func() { mailChannelSend = orig })
	require.NoError(t, sendSystemEmailAs(context.Background(), BrandOverleap, "u@example.com", "s", "b"))
	assert.Equal(t, configSupportEmail(BrandOverleap), got.ReplyTo)
}
