package center

import (
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// setupPrivateNodeTestRouter mirrors the production middleware chain for the
// owner-scoped private-node read endpoint: AuthRequired + EnforceDeviceClass.
func setupPrivateNodeTestRouter() *gin.Engine {
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	user := r.Group("/api/user")
	{
		user.GET("/private-nodes", AuthRequired(), EnforceDeviceClass(), api_get_user_private_nodes)
	}
	return r
}

// TestGetUserPrivateNodes covers the owner-scoped private-node read endpoint:
// item mapping (active+instance vs pending), strict owner isolation, empty
// non-null list, and isServiceable parity with the model method.
func TestGetUserPrivateNodes(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	router := setupPrivateNodeTestRouter()

	// ---- seed plan for label lookup ----
	plan := Plan{PID: "test-pn-list-1m", Label: "专属节点列表测试", Price: 9900, Month: 1,
		Tier: "basic", Kind: PlanKindPrivateNode, IsActive: BoolPtr(true), Highlight: BoolPtr(false)}
	require.NoError(t, db.Get().Create(&plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&plan) })

	// ---- users A and B ----
	userA := CreateTestUser(t)
	userB := CreateTestUser(t)
	tokenA := GenerateTestToken(userA.ID, "", time.Hour)

	now := time.Now().Unix()

	// ---- cloud instance bound to user A's active sub ----
	ci := CloudInstance{
		Provider: "aws_lightsail", AccountName: "test-acct",
		InstanceID:       generateId("test-ci"),
		IPAddress:        "203.0.113.7",
		Region:           "ap-northeast-1",
		TrafficUsedBytes: 123456789,
	}
	require.NoError(t, db.Get().Create(&ci).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&ci) })

	// active sub for user A, bound to the cloud instance
	subActive := PrivateNodeSubscription{
		UserID: userA.ID, PlanID: plan.ID, OrderID: uint64(now),
		CloudInstanceID:   &ci.ID,
		Region:            "ap-northeast-1",
		IPType:            IPTypeNonResidential,
		TrafficTotalBytes: 2 << 40,
		Status:            PNStatusActive,
		PurchasedAt:       now - 3600,
		ExpiresAt:         now + 30*86400,
	}
	require.NoError(t, db.Get().Create(&subActive).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&subActive) })

	// pending sub for user A, no cloud instance yet
	subPending := PrivateNodeSubscription{
		UserID: userA.ID, PlanID: plan.ID, OrderID: uint64(now) + 1,
		Region:            "us-east-1",
		IPType:            IPTypeNonResidential,
		TrafficTotalBytes: 2 << 40,
		Status:            PNStatusPending,
		PurchasedAt:       now - 60,
		ExpiresAt:         now + 30*86400,
	}
	require.NoError(t, db.Get().Create(&subPending).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&subPending) })

	// user B's sub — must never appear in user A's response (owner isolation)
	subOther := PrivateNodeSubscription{
		UserID: userB.ID, PlanID: plan.ID, OrderID: uint64(now) + 2,
		Region:            "us-east-1",
		IPType:            IPTypeNonResidential,
		TrafficTotalBytes: 2 << 40,
		Status:            PNStatusActive,
		PurchasedAt:       now - 60,
		ExpiresAt:         now + 30*86400,
	}
	require.NoError(t, db.Get().Create(&subOther).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&subOther) })

	// ---- request as user A ----
	w := NewTestRequest(http.MethodGet, "/api/user/private-nodes").
		WithBearerToken(tokenA).
		Execute(router)
	require.Equal(t, http.StatusOK, w.Code)

	data, err := ParseResponseData[DataPrivateNodeList](w)
	require.NoError(t, err)

	// owner isolation: only user A's 2 subs, never user B's
	require.Len(t, data.Items, 2, "user A must see exactly their own 2 subs")
	for _, it := range data.Items {
		require.NotEqual(t, subOther.ID, it.ID, "user B's sub must never appear")
	}

	// order id DESC → pending (higher id) first, then active
	byID := map[uint64]DataPrivateNodeSubscription{}
	for _, it := range data.Items {
		byID[it.ID] = it
	}

	gotActive, ok := byID[subActive.ID]
	require.True(t, ok, "active sub must be present")
	require.Equal(t, PNStatusActive, gotActive.Status)
	require.NotNil(t, gotActive.Node, "active sub bound to instance must have node info")
	require.Equal(t, "203.0.113.7", gotActive.Node.IP)
	require.Equal(t, "ap-northeast-1", gotActive.Node.Region)
	require.Equal(t, int64(123456789), gotActive.TrafficUsedBytes)
	require.False(t, gotActive.QuotaExhausted, "well-under-quota sub must not be exhausted")
	require.Equal(t, "专属节点列表测试", gotActive.PlanLabel)
	require.Equal(t, subActive.IsServiceable(now), gotActive.IsServiceable)
	require.True(t, gotActive.IsServiceable, "active sub within period is serviceable")

	gotPending, ok := byID[subPending.ID]
	require.True(t, ok, "pending sub must be present")
	require.Equal(t, PNStatusPending, gotPending.Status)
	require.Nil(t, gotPending.Node, "pending sub has no node yet")
	require.Equal(t, int64(0), gotPending.TrafficUsedBytes, "pending sub has no usage")
	require.False(t, gotPending.QuotaExhausted, "unprovisioned sub has no quota to exhaust")
	require.Equal(t, int64(0), gotPending.QuotaResetAt, "unprovisioned sub has no reset time")
	require.Equal(t, subPending.IsServiceable(now), gotPending.IsServiceable)
	require.False(t, gotPending.IsServiceable, "pending sub is not serviceable")
}

// TestGetUserPrivateNodes_QuotaExhaustedField asserts the orthogonal quota
// signal: a provisioned sub whose bound instance is at >= 95% usage surfaces
// quotaExhausted=true (matching the runtime cutoff in slave_api_usage.go) and
// quotaResetAt mirrors the instance's TrafficResetAt.
func TestGetUserPrivateNodes_QuotaExhaustedField(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	router := setupPrivateNodeTestRouter()

	plan := Plan{PID: "test-pn-quota-1m", Label: "专属节点额度测试", Price: 9900, Month: 1,
		Tier: "basic", Kind: PlanKindPrivateNode, IsActive: BoolPtr(true), Highlight: BoolPtr(false)}
	require.NoError(t, db.Get().Create(&plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&plan) })

	user := CreateTestUser(t)
	token := GenerateTestToken(user.ID, "", time.Hour)

	now := time.Now().Unix()

	// Bound instance at 96% usage (960/1000 >= 95%) with a known reset time.
	ci := CloudInstance{
		Provider: "aws_lightsail", AccountName: "test-acct",
		InstanceID:        generateId("test-ci-quota"),
		IPAddress:         "203.0.113.7",
		Region:            "ap-northeast-1",
		TrafficTotalBytes: 1000,
		TrafficUsedBytes:  960,
		TrafficResetAt:    1893456000,
	}
	require.NoError(t, db.Get().Create(&ci).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&ci) })

	sub := PrivateNodeSubscription{
		UserID: user.ID, PlanID: plan.ID, OrderID: uint64(time.Now().UnixNano()),
		CloudInstanceID:   &ci.ID,
		Region:            "ap-northeast-1",
		IPType:            IPTypeNonResidential,
		TrafficTotalBytes: 1000,
		Status:            PNStatusActive,
		PurchasedAt:       now - 3600,
		ExpiresAt:         now + 30*86400,
	}
	require.NoError(t, db.Get().Create(&sub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&sub) })

	w := NewTestRequest(http.MethodGet, "/api/user/private-nodes").
		WithBearerToken(token).
		Execute(router)
	require.Equal(t, http.StatusOK, w.Code)

	data, err := ParseResponseData[DataPrivateNodeList](w)
	require.NoError(t, err)
	require.Len(t, data.Items, 1)

	got := data.Items[0]
	require.True(t, got.QuotaExhausted, "96%% usage must be quotaExhausted")
	require.Equal(t, int64(1893456000), got.QuotaResetAt)
}

// TestGetUserPrivateNodes_Empty asserts a user with no subs gets a non-null
// empty items array (not JSON null).
func TestGetUserPrivateNodes_Empty(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	router := setupPrivateNodeTestRouter()

	user := CreateTestUser(t)
	token := GenerateTestToken(user.ID, "", time.Hour)

	w := NewTestRequest(http.MethodGet, "/api/user/private-nodes").
		WithBearerToken(token).
		Execute(router)
	require.Equal(t, http.StatusOK, w.Code)

	// raw body must contain "items":[] not "items":null
	require.Contains(t, w.Body.String(), `"items":[]`)

	data, err := ParseResponseData[DataPrivateNodeList](w)
	require.NoError(t, err)
	require.NotNil(t, data.Items)
	require.Len(t, data.Items, 0)
}
