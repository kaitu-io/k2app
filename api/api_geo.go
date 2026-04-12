package center

import (
	"github.com/gin-gonic/gin"
)

type geoResponse struct {
	Country string `json:"country"`
	Profile string `json:"profile"`
}

// api_get_geo returns the detected country and suggested profile for the
// requesting IP. Anonymous — no auth required. Used by the webapp to
// auto-detect the user's country on first launch (even before login).
//
// Response:
//
//	{ "code": 0, "data": { "country": "cn", "profile": "cnroute" } }
//
// If the IP doesn't match any of the 14 target countries, country is ""
// and profile is "global".
func api_get_geo(c *gin.Context) {
	cc := CountryFromGinContext(c)
	profile := SuggestedProfileForCountry(cc)
	resp := geoResponse{Country: cc, Profile: profile}
	Success(c, &resp)
}
