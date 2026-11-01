package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// seedPressureSubsNode creates a node + k2v5 tunnel + NodeUsage row and returns
// the tunnel domain. Cleanup is registered on t.
func seedPressureSubsNode(t *testing.T, ip, uniq string, class string, ownerID *uint64, pressure float64, pressureAt int64) string {
	t.Helper()
	domain := "press-" + ip + "-" + uniq + ".example"
	node := SlaveNode{Ipv4: ip, SecretToken: "press-" + ip, Country: "JP", Region: "japan",
		Name: "press-" + ip + "-" + uniq, Class: class, PrivateOwnerUserID: ownerID}
	require.NoError(t, db.Get().Create(&node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&node) })
	tun := SlaveTunnel{Domain: domain, SecretToken: "press-tt-" + ip, Name: "press-tun-" + ip + "-" + uniq,
		Protocol: TunnelProtocolK2V5, Port: 443, NodeID: node.ID,
		IsTest: BoolPtr(false), ServerURL: "k2v5://" + domain + ":443"}
	require.NoError(t, db.Get().Create(&tun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })
	usage := NodeUsage{NodeID: node.ID, Ipv4: ip, PressureRatio: pressure, PressureAt: pressureAt}
	require.NoError(t, db.Get().Create(&usage).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&usage) })
	return domain
}

func subsScoreFor(t *testing.T, authHeader, domain string) float64 {
	t.Helper()
	r := gin.New()
	r.GET("/api/subs", api_subs)
	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", authHeader)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp SubsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	for _, tn := range resp.Tunnels {
		if strings.Contains(tn.URL, domain) {
			return tn.RecommendScore
		}
	}
	t.Fatalf("tunnel %s not in /api/subs response: %s", domain, w.Body.String())
	return 0
}

// A fresh high-pressure node must be scored by pressure when the switch is on.
// A nil Pressure at the call site would yield the quota score (1.0) and fail.
func TestApiSubs_PressureEnabled_FreshHighPressureLowersScore(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)
	restoreViper(t, "node_pressure.enabled")
	viper.Set("node_pressure.enabled", true)

	uniq := time.Now().Format("20060102150405.000000")
	_, auth := subsTestDevice(t, false)
	dom := seedPressureSubsNode(t, "10.98.1.1", uniq, NodeClassShared, nil, 0.8, time.Now().Unix())

	// quota unlimited → quotaScore 1; p=0.8 → 0.9*0.2 + 0.1 = 0.28
	assert.InDelta(t, 0.28, subsScoreFor(t, auth, dom), 1e-9)

	viper.Set("node_pressure.enabled", false)
	assert.InDelta(t, 1.0, subsScoreFor(t, auth, dom), 1e-9, "switch off must be the plain quota score")
}

// A private node's fresh pressure must not shift the fleet median used for
// shared nodes whose own pressure is missing.
func TestApiSubs_PressureFleet_ExcludesPrivateNodes(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)
	restoreViper(t, "node_pressure.enabled")
	viper.Set("node_pressure.enabled", true)

	uniq := time.Now().Format("20060102150405.000000")
	udid, auth := subsTestDevice(t, false)
	var dev Device
	require.NoError(t, db.Get().Where("udid = ?", udid).First(&dev).Error)

	// Shared node: row exists but pressure missing (PressureAt=0) → fleet median.
	dom := seedPressureSubsNode(t, "10.98.2.1", uniq, NodeClassShared, nil, 0, 0)
	// Private node with fresh, very high pressure.
	seedPressureSubsNode(t, "10.98.2.2", uniq, NodeClassPrivate, &dev.UserID, 0.95, time.Now().Unix())

	var ips []string
	require.NoError(t, db.Get().Model(&SlaveNode{}).Where("class = ?", NodeClassShared).Pluck("ipv4", &ips).Error)
	fleet := fleetPressure(getNodeUsagesByIPs(ips), time.Now().Unix())
	require.Less(t, fleet, 0.9, "dev DB shared fleet is itself saturated; test cannot discriminate")

	want := 0.9*(1-quantizePressure(fleet)) + 0.1
	assert.InDelta(t, want, subsScoreFor(t, auth, dom), 1e-9,
		"private node pressure 0.95 must not enter the fleet median")
}
