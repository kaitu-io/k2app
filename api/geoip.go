package center

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/geoip"
	"github.com/wordgate/qtoolkit/log"
)

// profileByCountry maps country code → suggestedProfile name.
var profileByCountry = map[string]string{
	"cn": "cnroute",
	"ir": "iroute",
	"ru": "ruroute",
	"tr": "troute",
	"pk": "pkroute",
	"vn": "vnroute",
	"mm": "mmroute",
	"eg": "egroute",
	"id": "idroute",
	"sa": "saroute",
	"ae": "aeroute",
	"th": "throute",
	"bd": "bdroute",
	"by": "byroute",
}

// SuggestedProfileForCountry returns the routing profile name for a country code.
// Returns "global" for unknown or empty codes.
func SuggestedProfileForCountry(cc string) string {
	if p, ok := profileByCountry[strings.ToLower(strings.TrimSpace(cc))]; ok {
		return p
	}
	return "global"
}

// CountryFromGinContext detects the client's country from request IP using
// MaxMind GeoLite2-Country via qtoolkit/geoip. Returns lowercase ISO 3166-1
// alpha-2 code or "" if detection fails.
func CountryFromGinContext(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	ip := c.ClientIP()
	if ip == "" {
		return ""
	}
	cc, err := geoip.Country(ip)
	if err != nil {
		return ""
	}
	return strings.ToLower(cc)
}

// maybeUpdateUserCountry updates user.current_country asynchronously when a
// new country is detected. Called from auth middleware after successful auth.
func maybeUpdateUserCountry(c *gin.Context, user *User) {
	if user == nil || user.ID == 0 {
		return
	}
	cc := CountryFromGinContext(c)
	if cc == "" {
		return
	}
	if cc == user.CurrentCountry {
		return
	}
	user.CurrentCountry = cc
	userID := user.ID

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := db.Get().WithContext(ctx).Model(&User{}).
			Where("id = ?", userID).
			Update("current_country", cc).Error
		if err != nil {
			log.Warnf(ctx, "failed to update current_country for user %d: %v", userID, err)
		}
	}()
}
