package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigInviteBaseURLPerBrand(t *testing.T) {
	testInitConfig()
	assert.Contains(t, configInviteBaseURL(BrandKaitu), "kaitu.io")
	assert.Equal(t, "https://www.overleap.io/s", configInviteBaseURL(BrandOverleap))
}

func TestConfigSupportEmailPerBrand(t *testing.T) {
	testInitConfig()
	assert.Contains(t, configSupportEmail(BrandKaitu), "kaitu")
	assert.Equal(t, "support@overleap.io", configSupportEmail(BrandOverleap))
}
