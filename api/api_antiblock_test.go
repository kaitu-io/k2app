package center

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func TestParseServerURLDescriptor(t *testing.T) {
	url := "k2v5://13-54-164-215.sslip.io:443?ech=AEX-abc&pin=sha256:AAA=,sha256:BBB=&hop=40000-40019&ip=13.54.164.215"
	ip, pin, ech, ok := parseServerURLDescriptor(url)
	if !ok {
		t.Fatalf("expected ok")
	}
	if ip != "13.54.164.215" {
		t.Fatalf("ip=%q", ip)
	}
	if pin != "sha256:AAA=,sha256:BBB=" {
		t.Fatalf("pin=%q", pin)
	}
	if ech != "AEX-abc" {
		t.Fatalf("ech=%q", ech)
	}
	// missing ech/pin/ip → ok=false
	if _, _, _, ok2 := parseServerURLDescriptor("k2v5://x:443?ip=1.2.3.4"); ok2 {
		t.Fatalf("expected !ok when ech/pin missing")
	}
}

// TestHandleAntiblockSeed verifies auth gates and filtering of the seed endpoint.
// Requires real dev MySQL (integration test); skips if config.yml unavailable.
func TestHandleAntiblockSeed(t *testing.T) {
	setupTestDB(t)

	uniq := fmt.Sprintf("%d", time.Now().UnixNano())
	// Use last 3 digits of nanosecond timestamp for IP segment to avoid collisions
	seg := uniq[len(uniq)-3:]

	// ---- Healthy shared node ----
	healthyIP := fmt.Sprintf("10.88.%s.1", seg)
	healthyNode := SlaveNode{
		Ipv4:    healthyIP,
		Name:    "antiblock-seed-healthy-" + uniq,
		Country: "AU",
		Class:   NodeClassShared,
	}
	require.NoError(t, db.Get().Create(&healthyNode).Error)

	healthyTunnel := SlaveTunnel{
		NodeID:    healthyNode.ID,
		Protocol:  TunnelProtocolK2V5,
		Name:      "ab-tun-healthy-" + uniq,
		Domain:    "ab-h" + uniq + ".example.com",
		Port:      10001,
		IsTest:    BoolPtr(false),
		ServerURL: fmt.Sprintf("k2v5://13-54-164-215.sslip.io:443?ech=AEX-abc&pin=sha256:AAA=,sha256:BBB=&hop=40000-40019&ip=%s", healthyIP),
	}
	require.NoError(t, db.Get().Create(&healthyTunnel).Error)

	healthyUsage := NodeUsage{
		NodeID:          healthyNode.ID,
		Ipv4:            healthyIP,
		QuotaTotalBytes: 0, // unlimited
		UsedBytes:       0,
		LastReportAt:    time.Now().Unix(),
	}
	require.NoError(t, db.Get().Create(&healthyUsage).Error)

	// ---- Over-quota shared node (must be excluded) ----
	overIP := fmt.Sprintf("10.88.%s.2", seg)
	overNode := SlaveNode{
		Ipv4:    overIP,
		Name:    "antiblock-seed-over-" + uniq,
		Country: "AU",
		Class:   NodeClassShared,
	}
	require.NoError(t, db.Get().Create(&overNode).Error)

	overTunnel := SlaveTunnel{
		NodeID:    overNode.ID,
		Protocol:  TunnelProtocolK2V5,
		Name:      "ab-tun-over-" + uniq,
		Domain:    "ab-o" + uniq + ".example.com",
		Port:      10001,
		IsTest:    BoolPtr(false),
		ServerURL: fmt.Sprintf("k2v5://over.sslip.io:443?ech=AEX-over&pin=sha256:OVR=,sha256:OVR2=&ip=%s", overIP),
	}
	require.NoError(t, db.Get().Create(&overTunnel).Error)

	quota := int64(1 << 30) // 1 GiB
	overUsage := NodeUsage{
		NodeID:          overNode.ID,
		Ipv4:            overIP,
		QuotaTotalBytes: quota,
		UsedBytes:       quota, // fully used — triggers isNodeOverQuota
		LastReportAt:    time.Now().Unix(),
	}
	require.NoError(t, db.Get().Create(&overUsage).Error)

	// ---- Private node (must be excluded) ----
	privateIP := fmt.Sprintf("10.88.%s.3", seg)
	privateNode := SlaveNode{
		Ipv4:    privateIP,
		Name:    "antiblock-seed-private-" + uniq,
		Country: "AU",
		Class:   NodeClassPrivate,
	}
	require.NoError(t, db.Get().Create(&privateNode).Error)

	privateTunnel := SlaveTunnel{
		NodeID:    privateNode.ID,
		Protocol:  TunnelProtocolK2V5,
		Name:      "ab-tun-priv-" + uniq,
		Domain:    "ab-p" + uniq + ".example.com",
		Port:      10001,
		IsTest:    BoolPtr(false),
		ServerURL: fmt.Sprintf("k2v5://private.sslip.io:443?ech=AEX-prv&pin=sha256:PRV=&ip=%s", privateIP),
	}
	require.NoError(t, db.Get().Create(&privateTunnel).Error)

	t.Cleanup(func() {
		db.Get().Unscoped().Where("node_id = ?", healthyNode.ID).Delete(&SlaveTunnel{})
		db.Get().Unscoped().Where("node_id = ?", overNode.ID).Delete(&SlaveTunnel{})
		db.Get().Unscoped().Where("node_id = ?", privateNode.ID).Delete(&SlaveTunnel{})
		db.Get().Unscoped().Where("ipv4 = ?", healthyIP).Delete(&NodeUsage{})
		db.Get().Unscoped().Where("ipv4 = ?", overIP).Delete(&NodeUsage{})
		db.Get().Unscoped().Delete(&healthyNode)
		db.Get().Unscoped().Delete(&overNode)
		db.Get().Unscoped().Delete(&privateNode)
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/antiblock/seed", handleAntiblockSeed)

	doReq := func(headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/antiblock/seed", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("env_unset_503", func(t *testing.T) {
		t.Setenv("ANTIBLOCK_SEED_KEY", "") // empty = 503
		w := doReq(nil)
		require.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("wrong_key_401", func(t *testing.T) {
		t.Setenv("ANTIBLOCK_SEED_KEY", "correct-key")
		w := doReq(map[string]string{"X-Antiblock-Seed-Key": "wrong-key"})
		require.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("correct_key_200", func(t *testing.T) {
		t.Setenv("ANTIBLOCK_SEED_KEY", "testkey")
		w := doReq(map[string]string{"X-Antiblock-Seed-Key": "testkey"})
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

		var resp struct {
			Code int `json:"code"`
			Data struct {
				Entries []string `json:"entries"`
				Nodes   []struct {
					IP  string `json:"ip"`
					Pin string `json:"pin"`
					ECH string `json:"ech"`
				} `json:"nodes"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "unmarshal: %s", w.Body.String())
		assert.Equal(t, 0, resp.Code, "business code must be 0")

		// entries must contain default control-plane
		require.NotEmpty(t, resp.Data.Entries, "entries must not be empty")

		// Healthy shared node must appear with ech+pin
		var foundHealthy bool
		for _, n := range resp.Data.Nodes {
			if n.IP == healthyIP {
				foundHealthy = true
				assert.NotEmpty(t, n.ECH, "ech must be non-empty for healthy node")
				assert.NotEmpty(t, n.Pin, "pin must be non-empty for healthy node")
			}
		}
		assert.True(t, foundHealthy, "healthy shared node ip=%s must appear in nodes", healthyIP)

		// Over-quota and private nodes must be absent
		for _, n := range resp.Data.Nodes {
			assert.NotEqual(t, overIP, n.IP, "over-quota node must not appear")
			assert.NotEqual(t, privateIP, n.IP, "private node must not appear")
		}
	})
}
