package center

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestConfigSocialAuth_Ready(t *testing.T) {
	keys := []string{
		"social_auth.apple_team_id",
		"social_auth.kaitu.google.enabled", "social_auth.kaitu.apple.key_id", "social_auth.kaitu.apple.private_key",
		"social_auth.overleap.google.enabled", "social_auth.overleap.apple.key_id", "social_auth.overleap.apple.private_key",
	}
	for _, k := range keys {
		restoreViper(t, k)
	}
	reset := func() {
		for _, k := range keys {
			viper.Set(k, nil)
		}
	}

	cases := []struct {
		name          string
		set           map[string]any
		brand         Brand
		google, apple bool
	}{
		{"all empty", nil, BrandKaitu, false, false},
		{"google enabled only", map[string]any{"social_auth.kaitu.google.enabled": true}, BrandKaitu, true, false},
		{"apple key_id only", map[string]any{"social_auth.kaitu.apple.key_id": "K"}, BrandKaitu, false, false},
		{"apple private_key only", map[string]any{"social_auth.kaitu.apple.private_key": "PEM"}, BrandKaitu, false, false},
		{"apple key_id+private_key", map[string]any{"social_auth.kaitu.apple.key_id": "K", "social_auth.kaitu.apple.private_key": "PEM"}, BrandKaitu, false, true},
		{"overleap config does not leak into kaitu", map[string]any{
			"social_auth.overleap.google.enabled": true, "social_auth.overleap.apple.key_id": "K", "social_auth.overleap.apple.private_key": "PEM",
		}, BrandKaitu, false, false},
		{"overleap config applies to overleap", map[string]any{
			"social_auth.overleap.google.enabled": true, "social_auth.overleap.apple.key_id": "K", "social_auth.overleap.apple.private_key": "PEM",
		}, BrandOverleap, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reset()
			for k, v := range tc.set {
				viper.Set(k, v)
			}
			c := configSocialAuth(tc.brand)
			assert.Equal(t, tc.google, c.Ready(SocialProviderGoogle))
			assert.Equal(t, tc.apple, c.Ready(SocialProviderApple))
			assert.False(t, c.Ready("github"))
		})
	}

	t.Run("team id default and override", func(t *testing.T) {
		reset()
		assert.Equal(t, "NJT954Q3RH", configSocialAuth(BrandKaitu).Apple.TeamID)
		viper.Set("social_auth.apple_team_id", "TEAM123456")
		assert.Equal(t, "TEAM123456", configSocialAuth(BrandOverleap).Apple.TeamID)
	})
}
