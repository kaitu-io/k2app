package center

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppConfig_SocialSignInFollowsBrand(t *testing.T) {
	skipIfNoConfig(t)
	keys := []string{
		"social_auth.kaitu.google.enabled", "social_auth.kaitu.apple.key_id", "social_auth.kaitu.apple.private_key",
		"social_auth.overleap.google.enabled", "social_auth.overleap.apple.key_id", "social_auth.overleap.apple.private_key",
	}
	for _, k := range keys {
		restoreViper(t, k)
		viper.Set(k, nil)
	}
	// 只配 overleap 的 Apple
	viper.Set("social_auth.overleap.apple.key_id", "K")
	viper.Set("social_auth.overleap.apple.private_key", "PEM")

	router := setupBrandIsolationRouter()
	fetch := func(host string) DataAppConfig {
		req := httptest.NewRequest(http.MethodGet, "/api/app/config", nil)
		req.Host = host
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		data, err := ParseResponseData[DataAppConfig](w)
		require.NoError(t, err)
		return *data
	}

	o := fetch("www.overleap.io")
	assert.True(t, o.SocialSignIn.Apple)
	assert.False(t, o.SocialSignIn.Google)

	k := fetch("www.kaitu.io")
	assert.False(t, k.SocialSignIn.Apple)
	assert.False(t, k.SocialSignIn.Google)
}
