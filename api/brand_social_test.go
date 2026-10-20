package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSocialClientIDs_Audiences(t *testing.T) {
	kaitu := brandRegistry[BrandKaitu].Social
	overleap := brandRegistry[BrandOverleap].Social

	assert.Equal(t, []string{
		"125062807005-fqpjkq2pjjqdueen84q1kpp3rlkvq5sg.apps.googleusercontent.com",
		"125062807005-r08pi88vadprilgo5mr1oqndpinnkb0l.apps.googleusercontent.com",
		"125062807005-9vlr5ea2hp42hn5r2sc5prn8m82nnrv7.apps.googleusercontent.com",
	}, kaitu.Audiences(SocialProviderGoogle))
	assert.Contains(t, overleap.Audiences(SocialProviderGoogle), "808676014960-kb76trvs3q7q89a5qb914kjv5gfhu841.apps.googleusercontent.com")
	assert.Equal(t, []string{"com.allnationconnect.anc.wgios", "io.kaitu.signin"}, kaitu.Audiences(SocialProviderApple))
	assert.Contains(t, overleap.Audiences(SocialProviderApple), "io.overleap.signin")
	assert.Contains(t, overleap.Audiences(SocialProviderApple), "io.overleap")
	assert.Nil(t, kaitu.Audiences("wechat"))

	// empty values are dropped
	assert.Equal(t, []string{"a"}, SocialClientIDs{GoogleWebClientID: "a"}.Audiences(SocialProviderGoogle))

	// brand isolation: no aud shared between brands
	seen := map[string]Brand{}
	for _, b := range AllBrands() {
		for _, p := range []string{SocialProviderGoogle, SocialProviderApple} {
			auds := brandRegistry[b].Social.Audiences(p)
			require.NotEmpty(t, auds, "%s/%s", b, p)
			for _, a := range auds {
				prev, dup := seen[a]
				require.False(t, dup && prev != b, "aud %q shared by %s and %s", a, prev, b)
				seen[a] = b
			}
		}
	}
}
