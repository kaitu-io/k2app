package center

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

// restoreViper pins a viper key back to its pre-test value. The center package
// shares one viper instance across every test, so resetting to a literal
// (false / "") instead of the original silently reconfigures later tests —
// e.g. flipping mail.dev_mode off makes unrelated workers try to really send.
func restoreViper(t *testing.T, key string) {
	t.Helper()
	orig := viper.Get(key)
	t.Cleanup(func() { viper.Set(key, orig) })
}

func TestSendSystemEmailAs_DevModeDoesNotTouchSender(t *testing.T) {
	restoreViper(t, "mail.dev_mode")
	restoreViper(t, "mail_overleap.send_from")

	viper.Set("mail.dev_mode", true)
	viper.Set("mail_overleap.send_from", "support@overleap.io")
	assert.NoError(t, sendSystemEmailAs(t.Context(), BrandOverleap, "a@b.c", "s", "b"))
	assert.NoError(t, sendSystemEmailAs(t.Context(), BrandKaitu, "a@b.c", "s", "b"))
}
