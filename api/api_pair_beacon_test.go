package center

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/redis"
)

// discoverResp mirrors the discover endpoint payload shape.
type discoverResp struct {
	Code int `json:"code"`
	Data struct {
		Candidates []struct {
			LanIP string `json:"lanIP"`
			Port  int    `json:"port"`
		} `json:"candidates"`
	} `json:"data"`
}

// postBeacon drives api_pair_beacon with a fixed public source IP.
func postBeacon(t *testing.T, publicIP, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("POST", "/api/pair/beacon", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = publicIP + ":40000" // gin ClientIP() derives from RemoteAddr (no trusted proxy header)
	c.Request = req
	api_pair_beacon(c)
	return w
}

// getDiscover drives api_pair_discover as a logged-in user from a fixed public source IP.
func getDiscover(t *testing.T, publicIP string) discoverResp {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/api/pair/discover", nil)
	req.RemoteAddr = publicIP + ":40001"
	c.Request = req
	// Inject a logged-in user (no DB needed; ReqUser only reads ctx.User).
	c.Set("authContext", &authContext{UserID: 1, User: &User{}})
	api_pair_discover(c)

	resp, err := ParseResponse(w)
	require.NoError(t, err)
	var out discoverResp
	out.Code = resp.Code
	if len(resp.Data) > 0 {
		require.NoError(t, json.Unmarshal(resp.Data, &out.Data))
	}
	return out
}

func cleanBeaconKeys(t *testing.T, ips ...string) {
	t.Helper()
	testInitConfig()
	for _, ip := range ips {
		_ = redis.Client().Del(context.Background(), beaconRedisKey(ip)).Err()
	}
}

func TestBeaconStoreAndDiscoverSameIP(t *testing.T) {
	testInitConfig()
	cleanBeaconKeys(t, "9.9.9.9")
	t.Cleanup(func() { cleanBeaconKeys(t, "9.9.9.9") })

	w := postBeacon(t, "9.9.9.9", `{"lanIP":"192.168.8.1","port":1779}`)
	require.Equal(t, 200, w.Code)

	got := getDiscover(t, "9.9.9.9")
	require.Equal(t, int(ErrorNone), got.Code)
	require.Len(t, got.Data.Candidates, 1)
	assert.Equal(t, "192.168.8.1", got.Data.Candidates[0].LanIP)
	assert.Equal(t, 1779, got.Data.Candidates[0].Port)
}

func TestDiscoverDifferentIPNoLeak(t *testing.T) {
	testInitConfig()
	cleanBeaconKeys(t, "9.9.9.9", "8.8.8.8")
	t.Cleanup(func() { cleanBeaconKeys(t, "9.9.9.9", "8.8.8.8") })

	w := postBeacon(t, "9.9.9.9", `{"lanIP":"192.168.8.1","port":1779}`)
	require.Equal(t, 200, w.Code)

	// A user behind a different public IP must NOT see another network's beacon.
	got := getDiscover(t, "8.8.8.8")
	require.Equal(t, int(ErrorNone), got.Code)
	assert.Empty(t, got.Data.Candidates, "discover must not cross public-IP boundaries")
}
