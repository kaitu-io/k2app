package center

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApiGetGeo_HotfixAlwaysReturnsCN pins the 2026-06-14 hotfix: /geo must return
// cn/cnroute regardless of the requester's real geo. The webapp feeds this country
// straight into smart-mode match.region; a non-cn region requires a bundle the
// embedded fallback lacks, which 504'd "无法连接" (#2878). If this ever returns live
// geo again, that failure class regresses. Remove this test only when the endpoint
// is deleted.
func TestApiGetGeo_HotfixAlwaysReturnsCN(t *testing.T) {
	testInitConfig()
	for _, ip := range []string{"175.139.1.1" /*MY*/, "8.8.8.8" /*US*/, "203.0.113.7"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest("GET", "/api/geo", nil)
		req.RemoteAddr = ip + ":40000"
		c.Request = req

		api_get_geo(c)

		resp, err := ParseResponse(w)
		require.NoError(t, err, "ip=%s", ip)
		assert.Equal(t, 0, resp.Code, "ip=%s", ip)
		var data geoResponse
		require.NoError(t, json.Unmarshal(resp.Data, &data), "ip=%s", ip)
		assert.Equal(t, "cn", data.Country, "ip=%s must force cn", ip)
		assert.Equal(t, "cnroute", data.Profile, "ip=%s must force cnroute", ip)
	}
}

// The hotfix is brand-scoped: 开途 stays pinned to cn for every IP, while a
// GeoDetect brand follows the IP but can only ever land on a country that has
// a rule bundle — never on a bundle-less region, which is what 504'd before.
func TestGeoCountryFor(t *testing.T) {
	cases := []struct {
		brand    Brand
		detected string
		want     string
	}{
		{BrandKaitu, "", "cn"},
		{BrandKaitu, "gb", "cn"},
		{BrandKaitu, "us", "cn"},
		{BrandKaitu, "ru", "cn"},
		{BrandOverleap, "gb", "gb"},
		{BrandOverleap, "ru", "ru"},
		{BrandOverleap, "CN", "cn"},
		{BrandOverleap, "", "gb"},   // undetectable → brand default
		{BrandOverleap, "us", "gb"}, // no bundle → brand default
		{BrandOverleap, "my", "gb"}, // the country from the original incident
		{Brand("nope"), "ru", "cn"}, // unknown brand behaves as 开途
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, geoCountryFor(tc.brand, tc.detected), "brand=%s detected=%q", tc.brand, tc.detected)
	}
}

// Every brand default must itself be a bundle-backed country, or the fallback
// would hand clients exactly the bundle-less region it exists to prevent.
func TestGeoDefaultsAreRoutable(t *testing.T) {
	assert.Equal(t, "cn", brandRegistry[BrandKaitu].GeoDefaultCountry)
	assert.False(t, brandRegistry[BrandKaitu].GeoDetect)
	assert.Equal(t, "gb", brandRegistry[BrandOverleap].GeoDefaultCountry)
	for id, cfg := range brandRegistry {
		assert.True(t, geoRoutableCountries[cfg.GeoDefaultCountry], "brand %s default %q has no bundle", id, cfg.GeoDefaultCountry)
	}
}

func TestApiGetGeo_OtherBrandFallsBackToItsDefault(t *testing.T) {
	testInitConfig()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/api/geo", nil)
	req.Host = "overleap.io"
	req.RemoteAddr = "203.0.113.7:40000" // TEST-NET-3: never geolocates
	c.Request = req

	api_get_geo(c)

	resp, err := ParseResponse(w)
	require.NoError(t, err)
	var data geoResponse
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	assert.Equal(t, "gb", data.Country)
	assert.Equal(t, "global", data.Profile)
}
