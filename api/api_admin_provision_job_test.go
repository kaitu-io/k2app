package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// provisionJobTestRouter wires the three provision-job endpoints onto a bare
// gin router (no admin middleware) so handlers can be driven directly via httptest.
func provisionJobTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/app/provision-jobs", adminListProvisionJobs)
	r.POST("/app/provision-jobs/:id/claim", adminClaimProvisionJob)
	r.POST("/app/provision-jobs/:id/report", adminReportProvisionJob)
	return r
}

// seedProvisionJobFixture creates a PrivateNodeSubscription with a known claim
// token plus a queued NodeProvisionJob for it. Cleanup is registered on t.
func seedProvisionJobFixture(t *testing.T, claimToken, domain string) (sub *PrivateNodeSubscription, job *NodeProvisionJob) {
	t.Helper()
	now := time.Now().Unix()

	sub = &PrivateNodeSubscription{
		UserID:              999000,
		PlanID:              1,
		Region:              "japan",
		IPType:              IPTypeNonResidential,
		TrafficTotalBytes:   2 * 1024 * 1024 * 1024 * 1024,
		Status:              PNStatusProvisioning,
		PurchasedAt:         now,
		ExpiresAt:           now + 86400,
		ProvisionClaimToken: claimToken,
	}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sub) })

	job = &NodeProvisionJob{
		SubID:             sub.ID,
		Status:            NPJStatusQueued,
		Region:            sub.Region,
		BundleID:          "nano_3_0",
		ImageID:           "ubuntu_22_04",
		ComposeVariant:    "private",
		TrafficTotalBytes: sub.TrafficTotalBytes,
		IPType:            sub.IPType,
		Domain:            domain,
	}
	require.NoError(t, db.Get().Create(job).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(job) })
	return sub, job
}

// parseJobResponse pulls the `data` map out of a standard Response envelope.
func parseJobResponse(t *testing.T, body []byte) (code float64, data map[string]any) {
	t.Helper()
	var env map[string]any
	require.NoError(t, json.Unmarshal(body, &env), "response: %s", string(body))
	code, _ = env["code"].(float64)
	data, _ = env["data"].(map[string]any)
	return code, data
}

func TestAdminProvisionJob_List(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	_, job := seedProvisionJobFixture(t, "tok-list-"+time.Now().Format("150405.000000"), "")
	r := provisionJobTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/app/provision-jobs?status=queued", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	code, data := parseJobResponse(t, w.Body.Bytes())
	require.Equal(t, float64(0), code, "body: %s", w.Body.String())

	items, _ := data["items"].([]any)
	found := false
	for _, it := range items {
		m, _ := it.(map[string]any)
		if id, ok := m["id"].(float64); ok && uint64(id) == job.ID {
			found = true
			assert.Equal(t, NPJStatusQueued, m["status"])
		}
	}
	assert.True(t, found, "queued job %d should appear in status=queued list", job.ID)
}

func TestAdminProvisionJob_ClaimAtomicity(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	token := "tok-claim-" + time.Now().Format("150405.000000")
	sub, job := seedProvisionJobFixture(t, token, "node.example.com")
	r := provisionJobTestRouter()

	doClaim := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"holder": "agent-A", "leaseSeconds": 300})
		req := httptest.NewRequest(http.MethodPost,
			"/app/provision-jobs/"+strconv.FormatUint(job.ID, 10)+"/claim", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// First claim succeeds.
	w := doClaim()
	require.Equal(t, http.StatusOK, w.Code)
	code, data := parseJobResponse(t, w.Body.Bytes())
	require.Equal(t, float64(0), code, "body: %s", w.Body.String())

	identity, _ := data["identity"].(map[string]any)
	require.NotNil(t, identity, "claim response must carry identity")
	assert.Equal(t, token, identity["claimToken"])
	assert.Equal(t, "node.example.com", identity["domain"])
	assert.NotEmpty(t, identity["centerUrl"])

	jobObj, _ := data["job"].(map[string]any)
	require.NotNil(t, jobObj)
	assert.Equal(t, NPJStatusClaimed, jobObj["status"])
	assert.Equal(t, "agent-A", jobObj["holder"])

	// DB reflects claim.
	var reloaded NodeProvisionJob
	require.NoError(t, db.Get().First(&reloaded, job.ID).Error)
	assert.Equal(t, NPJStatusClaimed, reloaded.Status)
	assert.Equal(t, "agent-A", reloaded.Holder)
	_ = sub

	// Second claim on the now-claimed job → conflict.
	w2 := doClaim()
	require.Equal(t, http.StatusOK, w2.Code)
	code2, _ := parseJobResponse(t, w2.Body.Bytes())
	assert.Equal(t, float64(ErrorConflict), code2, "double-claim must conflict; body: %s", w2.Body.String())
}

func TestAdminProvisionJob_Report(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	_, job := seedProvisionJobFixture(t, "tok-report-"+time.Now().Format("150405.000000"), "")
	r := provisionJobTestRouter()

	// Valid report: provisioning + instance + ipv4.
	body, _ := json.Marshal(map[string]any{
		"status":     NPJStatusProvisioning,
		"instanceId": "i-x",
		"ipv4":       "1.2.3.4",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/app/provision-jobs/"+strconv.FormatUint(job.ID, 10)+"/report", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	code, _ := parseJobResponse(t, w.Body.Bytes())
	require.Equal(t, float64(0), code, "body: %s", w.Body.String())

	var reloaded NodeProvisionJob
	require.NoError(t, db.Get().First(&reloaded, job.ID).Error)
	assert.Equal(t, NPJStatusProvisioning, reloaded.Status)
	assert.Equal(t, "i-x", reloaded.InstanceID)
	assert.Equal(t, "1.2.3.4", reloaded.IPv4)

	// Invalid status → ErrorInvalidArgument.
	bad, _ := json.Marshal(map[string]any{"status": "bogus"})
	reqBad := httptest.NewRequest(http.MethodPost,
		"/app/provision-jobs/"+strconv.FormatUint(job.ID, 10)+"/report", bytes.NewReader(bad))
	reqBad.Header.Set("Content-Type", "application/json")
	wBad := httptest.NewRecorder()
	r.ServeHTTP(wBad, reqBad)

	require.Equal(t, http.StatusOK, wBad.Code)
	codeBad, _ := parseJobResponse(t, wBad.Body.Bytes())
	assert.Equal(t, float64(ErrorInvalidArgument), codeBad, "body: %s", wBad.Body.String())
}
