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
