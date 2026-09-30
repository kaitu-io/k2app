package center

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestNoOutboundSideEffectsInTests: tests load ../center/config.yml, which carries
// real Slack webhooks and real SMTP credentials. testInitConfig must neutralise both.
func TestNoOutboundSideEffectsInTests(t *testing.T) {
	skipIfNoConfig(t)

	if !isMailDevMode() {
		t.Fatal("mail.dev_mode must be true in tests: MailSend would deliver through the real SMTP account")
	}
	if viper.GetString("slack.bot_token") != "" {
		t.Fatal("slack.bot_token must be empty in tests: DMs would reach real Slack users")
	}
	for name, raw := range viper.GetStringMapString("slack.webhooks") {
		u, err := url.Parse(raw)
		if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
			t.Errorf("slack webhook %q points at %q; tests must only post to the local sink", name, raw)
		}
	}
}

// TestNoRealCustomerEmailsInTestFixtures: fixture emails must use reserved test
// domains. A customer address copied from a support case once received a
// verification mail on every test run.
func TestNoRealCustomerEmailsInTestFixtures(t *testing.T) {
	realMailbox := regexp.MustCompile(`[A-Za-z0-9._%+-]+@(qq|163|126|gmail|outlook|hotmail|icloud|yahoo|foxmail|sina)\.com`)
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if m := realMailbox.FindString(line); m != "" {
				t.Errorf("%s:%d uses a real-provider mailbox %q; use an @example.com address", f, i+1, m)
			}
		}
	}
}
