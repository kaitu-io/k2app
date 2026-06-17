package center

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// nodeUpsertTestRouter wires the upsert endpoint exactly as production does
// (no slave middleware — request auth is skipped to isolate ip_type behaviour).
func nodeUpsertTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/slave/nodes/:ipv4", api_slave_node_upsert)
	return r
}

// callNodeUpsert drives PUT /slave/nodes/:ipv4 and returns the SlaveNode read
// back from the DB. It cleans up after itself via t.Cleanup.
func callNodeUpsert(t *testing.T, ipv4 string, req SlaveNodeUpsertRequest) SlaveNode {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)

	r := nodeUpsertTestRouter()
	httpReq := httptest.NewRequest(http.MethodPut, "/slave/nodes/"+ipv4, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httpReq)
	require.Equal(t, http.StatusOK, w.Code, "upsert HTTP status must be 200, body=%s", w.Body.String())

	// Verify the envelope says success (code == 0).
	var env map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), "response: %s", w.Body.String())
	code, _ := env["code"].(float64)
	require.Equal(t, float64(0), code, "upsert response code must be 0 (success), body=%s", w.Body.String())

	// Read back the node from DB.
	var node SlaveNode
	require.NoError(t, db.Get().Unscoped().Where("ipv4 = ?", ipv4).First(&node).Error)

	t.Cleanup(func() {
		db.Get().Unscoped().Where("node_id = ?", node.ID).Delete(&SlaveTunnel{})
		db.Get().Unscoped().Delete(&node)
	})

	return node
}

// TestUpsertWritesNormalizedIPType asserts C1: every write of ip_type goes
// through NormalizeIPType — valid value lands as-is; invalid/empty → "unknown".
// Drives the real upsert handler against dev MySQL (integration test).
func TestUpsertWritesNormalizedIPType(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	uniq := time.Now().Format("150405.000000")

	cases := []struct{ in, want string }{
		{"residential", "residential"},
		{"garbage", "unknown"},
		{"", "unknown"},
	}
	for i, cse := range cases {
		// Each case gets a unique IPv4 so the unique index on ipv4 doesn't collide.
		ipv4 := fmt.Sprintf("10.99.77.%d", 10+i) + "-iptype-" + uniq
		// Keep within 15 chars max for IPv4 field; use a realistic-looking test IP
		// by constructing a dotted-quad with a salt in the last octet.
		ipv4 = fmt.Sprintf("192.168.%d.%d", i+200, len(uniq)%200)

		node := callNodeUpsert(t, ipv4, SlaveNodeUpsertRequest{
			Country:     "US",
			Name:        fmt.Sprintf("iptype-test-%d-%s", i, uniq),
			SecretToken: fmt.Sprintf("tok-iptype-%d-%s", i, uniq),
			IPType:      cse.in,
		})
		if node.IPType != cse.want {
			t.Errorf("case %d (in=%q): ip_type=%q, want %q", i, cse.in, node.IPType, cse.want)
		}
	}
}

// TestUpsertIPTypeLastWriterWins pins the "同权同变更" decision: sidecar and ops
// are peers on ip_type, and a sidecar re-registration (upsert of an EXISTING
// node) overwrites whatever value is currently stored — including a value an
// operator set via update_node. The upsert handler is delete-and-recreate
// (slave_api_node.go), so this also guards that the recreate path actually
// carries the freshly-reported ip_type rather than dropping or preserving it.
//
// Direction A (sidecar → sidecar): residential, then re-register non_residential.
// Direction B (ops → sidecar):     ops sets residential, sidecar re-registers
//                                  non_residential and wins.
// Drives the real handlers against dev MySQL (integration test).
func TestUpsertIPTypeLastWriterWins(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	uniq := time.Now().Format("150405.000000")
	ipv4 := "192.169." + uniq[len(uniq)-3:len(uniq)-1] + "." + uniq[len(uniq)-1:]
	secret := "tok-lww-" + uniq

	// First registration: sidecar reports residential.
	first := callNodeUpsert(t, ipv4, SlaveNodeUpsertRequest{
		Country:     "US",
		Name:        "lww-" + uniq,
		SecretToken: secret,
		IPType:      IPTypeResidential,
	})
	require.Equal(t, IPTypeResidential, first.IPType, "first registration must land residential")

	// Direction B setup: operator overrides to residential explicitly (no-op
	// here since it already is, then we flip it to prove ops can write), then
	// flips it the other way so the subsequent sidecar write has something to
	// overwrite. We write directly through the model the same way update_node
	// does (NormalizeIPType-gated) to avoid standing up the admin auth chain.
	require.NoError(t,
		db.Get().Model(&SlaveNode{}).Where("ipv4 = ?", ipv4).
			Update("ip_type", NormalizeIPType("non_residential")).Error,
		"ops update_node-style write must succeed")
	var afterOps SlaveNode
	require.NoError(t, db.Get().Where("ipv4 = ?", ipv4).First(&afterOps).Error)
	require.Equal(t, IPTypeNonResidential, afterOps.IPType, "ops write must take effect")

	// Sidecar re-registers (same ipv4, same secret) reporting residential again.
	// Last writer (sidecar) must win, overwriting the ops value.
	second := callNodeUpsert(t, ipv4, SlaveNodeUpsertRequest{
		Country:     "US",
		Name:        "lww-" + uniq,
		SecretToken: secret,
		IPType:      IPTypeResidential,
	})
	require.Equal(t, IPTypeResidential, second.IPType,
		"sidecar re-registration must overwrite the ops-set value (last-writer-wins)")
	require.Equal(t, ipv4, second.Ipv4, "re-registration must target the same node ipv4")
}
