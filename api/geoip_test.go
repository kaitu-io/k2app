package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuggestedProfileForCountry(t *testing.T) {
	cases := []struct {
		cc   string
		want string
	}{
		{"cn", "cnroute"},
		{"ir", "iroute"},
		{"ru", "ruroute"},
		{"tr", "troute"},
		{"pk", "pkroute"},
		{"vn", "vnroute"},
		{"mm", "mmroute"},
		{"eg", "egroute"},
		{"id", "idroute"},
		{"sa", "saroute"},
		{"ae", "aeroute"},
		{"th", "throute"},
		{"bd", "bdroute"},
		{"by", "byroute"},
		{"", "global"},
		{"us", "global"},
		{"jp", "global"},
		{"CN", "cnroute"}, // case-insensitive
		{" ir ", "iroute"},
	}
	for _, tc := range cases {
		t.Run("cc="+tc.cc, func(t *testing.T) {
			assert.Equal(t, tc.want, SuggestedProfileForCountry(tc.cc))
		})
	}
}

func TestCountryFromGinContext_NilSafe(t *testing.T) {
	assert.Equal(t, "", CountryFromGinContext(nil))
}

func TestBuildDataUser_IncludesCountryAndProfile(t *testing.T) {
	u := &User{
		ID:                  42,
		UUID:                "user-42",
		Language:            "en-US",
		RegistrationCountry: "ir",
		CurrentCountry:      "ir",
	}
	data := buildDataUserWithDevice(u, nil)
	require.NotNil(t, data)
	assert.Equal(t, "ir", data.CurrentCountry)
	assert.Equal(t, "ir", data.RegistrationCountry)
	assert.Equal(t, "iroute", data.SuggestedProfile)

	// User with empty country gets global fallback
	u2 := &User{ID: 43, UUID: "user-43", Language: "en-US"}
	data2 := buildDataUserWithDevice(u2, nil)
	assert.Equal(t, "", data2.CurrentCountry)
	assert.Equal(t, "global", data2.SuggestedProfile)
}
