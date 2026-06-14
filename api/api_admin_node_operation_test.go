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

// nodeOperationTestRouter wires the four node-operation admin endpoints onto a
// bare gin router (no admin middleware) so handlers can be driven directly via
// httptest. Paths mirror the production /node-operations* registration.
func nodeOperationTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/app/node-operations", adminListNodeOperations)
	r.POST("/app/node-operations", adminCreateNodeOperation)
	r.POST("/app/node-operations/:id/claim", adminClaimNodeOperation)
	r.POST("/app/node-operations/:id/update", adminUpdateNodeOperation)
	return r
}

// parseJobResponse pulls code + data out of a standard Response envelope.
func parseJobResponse(t *testing.T, body []byte) (code float64, data map[string]any) {
	t.Helper()
	var env map[string]any
	require.NoError(t, json.Unmarshal(body, &env), "response: %s", string(body))
	code, _ = env["code"].(float64)
	data, _ = env["data"].(map[string]any)
	return code, data
}

// callAdminUpdateNodeOperation POSTs an update for op id and returns the int code.
func callAdminUpdateNodeOperation(t *testing.T, id uint64, req AdminUpdateNodeOperationRequest) int {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	r := nodeOperationTestRouter()
	httpReq := httptest.NewRequest(http.MethodPost,
		"/app/node-operations/"+strconv.FormatUint(id, 10)+"/update", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httpReq)
	require.Equal(t, http.StatusOK, w.Code)
	code, _ := parseJobResponse(t, w.Body.Bytes())
	return int(code)
}

// callAdminCreateNodeOperation POSTs a create and returns the int code.
func callAdminCreateNodeOperation(t *testing.T, req AdminCreateNodeOperationRequest) int {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	r := nodeOperationTestRouter()
	httpReq := httptest.NewRequest(http.MethodPost, "/app/node-operations", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httpReq)
	require.Equal(t, http.StatusOK, w.Code)
	code, _ := parseJobResponse(t, w.Body.Bytes())
	return int(code)
}

func TestAdminUpdateNodeOperation_ProvisionDoneRejected(t *testing.T) {
	skipIfNoConfig(t)
	sub := seedTestPrivateSub(t)
	op := &NodeOperation{Action: NodeOpProvision, SubID: sub.ID, Status: NodeOpInProgress, CreatedBy: "system:order"}
	require.NoError(t, db.Get().Create(op).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(op) })
	code := callAdminUpdateNodeOperation(t, op.ID, AdminUpdateNodeOperationRequest{Status: NodeOpDone})
	assert.Equal(t, int(ErrorInvalidOperation), code, "provision done via update must be rejected")
}

func TestAdminUpdateNodeOperation_StopDoneSetsCompletedAt(t *testing.T) {
	skipIfNoConfig(t)
	sub := seedTestPrivateSub(t)
	op := &NodeOperation{Action: NodeOpStop, SubID: sub.ID, Status: NodeOpInProgress, CreatedBy: "admin:t@t"}
	require.NoError(t, db.Get().Create(op).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(op) })
	code := callAdminUpdateNodeOperation(t, op.ID, AdminUpdateNodeOperationRequest{Status: NodeOpDone, Result: map[string]any{"stoppedAt": 1}})
	assert.Equal(t, 0, code)
	var got NodeOperation
	require.NoError(t, db.Get().First(&got, op.ID).Error)
	assert.Equal(t, NodeOpDone, got.Status)
	assert.Greater(t, got.CompletedAt, int64(0))
}

func TestAdminUpdateNodeOperation_RejectsBadStatus(t *testing.T) {
	skipIfNoConfig(t)
	sub := seedTestPrivateSub(t)
	op := &NodeOperation{Action: NodeOpStop, SubID: sub.ID, Status: NodeOpInProgress, CreatedBy: "admin:t@t"}
	require.NoError(t, db.Get().Create(op).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(op) })
	code := callAdminUpdateNodeOperation(t, op.ID, AdminUpdateNodeOperationRequest{Status: "bogus"})
	assert.Equal(t, int(ErrorInvalidArgument), code, "unknown status must be rejected")
}

func TestAdminUpdateNodeOperation_NotFound(t *testing.T) {
	skipIfNoConfig(t)
	code := callAdminUpdateNodeOperation(t, 999999999, AdminUpdateNodeOperationRequest{Status: NodeOpFailed})
	assert.Equal(t, int(ErrorNotFound), code, "missing op must 404")
}

func TestAdminCreateNodeOperation_RejectsProvision(t *testing.T) {
	skipIfNoConfig(t)
	sub := seedTestPrivateSub(t)
	code := callAdminCreateNodeOperation(t, AdminCreateNodeOperationRequest{SubID: sub.ID, Action: NodeOpProvision})
	assert.Equal(t, int(ErrorInvalidArgument), code, "manual provision create must be rejected")
}

func TestAdminCreateNodeOperation_NoCloudInstance(t *testing.T) {
	skipIfNoConfig(t)
	// seedTestPrivateSub leaves CloudInstanceID nil → stop/destroy/change_ip have
	// nothing to act on, so create must reject with ErrorInvalidOperation.
	sub := seedTestPrivateSub(t)
	code := callAdminCreateNodeOperation(t, AdminCreateNodeOperationRequest{SubID: sub.ID, Action: NodeOpStop})
	assert.Equal(t, int(ErrorInvalidOperation), code, "create on sub without cloud instance must be rejected")
}

func TestAdminListNodeOperations_ActionFilter(t *testing.T) {
	skipIfNoConfig(t)
	sub := seedTestPrivateSub(t)
	op := &NodeOperation{Action: NodeOpStop, SubID: sub.ID, Status: NodeOpQueued, CreatedBy: "system:lifecycle"}
	require.NoError(t, db.Get().Create(op).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(op) })

	r := nodeOperationTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/app/node-operations?action=stop&status=queued", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	code, data := parseJobResponse(t, w.Body.Bytes())
	require.Equal(t, float64(0), code, "body: %s", w.Body.String())

	items, _ := data["items"].([]any)
	found := false
	for _, it := range items {
		m, _ := it.(map[string]any)
		if id, ok := m["id"].(float64); ok && uint64(id) == op.ID {
			found = true
			assert.Equal(t, NodeOpStop, m["action"])
			assert.Equal(t, NodeOpQueued, m["status"])
		}
	}
	assert.True(t, found, "queued stop op %d should appear in filtered list", op.ID)
}

// TestAdminClaimNodeOperation_Atomicity reuses the provision-claim identity
// contract: a queued provision op flips to claimed exactly once, the claim
// response carries the node identity, and a second claim conflicts.
func TestAdminClaimNodeOperation_Atomicity(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now().Unix()
	token := "tok-claim-" + time.Now().Format("150405.000000")
	sub := &PrivateNodeSubscription{
		UserID: 999000, PlanID: 1, Region: "japan", IPType: IPTypeNonResidential,
		TrafficTotalBytes: 2 << 40, Status: PNStatusProvisioning,
		PurchasedAt: now, ExpiresAt: now + 86400, ProvisionClaimToken: token,
	}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sub) })

	op := &NodeOperation{
		Action: NodeOpProvision, SubID: sub.ID, Status: NodeOpQueued, CreatedBy: "system:order",
		Params: mustJSON(ProvisionParams{Region: sub.Region, IPType: sub.IPType}),
	}
	require.NoError(t, db.Get().Create(op).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(op) })

	r := nodeOperationTestRouter()
	doClaim := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"holder": "agent-A", "leaseSeconds": 300})
		req := httptest.NewRequest(http.MethodPost,
			"/app/node-operations/"+strconv.FormatUint(op.ID, 10)+"/claim", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// First claim succeeds and carries identity.
	w := doClaim()
	require.Equal(t, http.StatusOK, w.Code)
	code, data := parseJobResponse(t, w.Body.Bytes())
	require.Equal(t, float64(0), code, "body: %s", w.Body.String())

	identity, _ := data["identity"].(map[string]any)
	require.NotNil(t, identity, "provision claim response must carry identity")
	assert.Equal(t, token, identity["claimToken"])
	assert.NotEmpty(t, identity["centerUrl"])

	opObj, _ := data["operation"].(map[string]any)
	require.NotNil(t, opObj)
	assert.Equal(t, NodeOpClaimed, opObj["status"])
	assert.Equal(t, "agent-A", opObj["holder"])

	var reloaded NodeOperation
	require.NoError(t, db.Get().First(&reloaded, op.ID).Error)
	assert.Equal(t, NodeOpClaimed, reloaded.Status)
	assert.Equal(t, "agent-A", reloaded.Holder)

	// Second claim on the now-claimed op → conflict.
	w2 := doClaim()
	require.Equal(t, http.StatusOK, w2.Code)
	code2, _ := parseJobResponse(t, w2.Body.Bytes())
	assert.Equal(t, float64(ErrorConflict), code2, "double-claim must conflict; body: %s", w2.Body.String())
}
