package center

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestSlaveNodeClaim_AdvancesRouterFulfillmentToReady 验证 reconcilePrivateIdentity
// 首次激活分支（slave_api_node.go markProvisionDone 之后）接上的
// syncRouterFulfillment 调用：路由器版订单的 RouterFulfillment（stage=paid）在
// 节点带 claim 注册、订阅从 provisioning 转 active 的同一事务里被推进到 ready。
// 骨架复用 TestSlaveNodeUpsert_PrivateClaim/A_ClaimActivates（直接驱动
// api_slave_node_upsert handler，而不是绕过 HTTP 层直调内部函数）。
func TestSlaveNodeClaim_AdvancesRouterFulfillmentToReady(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()
	owner := CreateTestUser(t)

	claimToken := "claim-tok-router-" + generateId("c")
	sub := PrivateNodeSubscription{
		UserID: owner.ID, Status: PNStatusProvisioning, Region: "hongkong",
		IPType: IPTypeNonResidential, PurchasedAt: now, ExpiresAt: now + 86400,
		ProvisionClaimToken: claimToken,
	}
	require.NoError(t, db.Get().Create(&sub).Error)

	f := &RouterFulfillment{OrderID: 810000 + sub.ID, UserID: owner.ID, SubID: sub.ID,
		HardwareSKU: "redmi-ax6s", Stage: RouterStagePaid}
	require.NoError(t, db.Get().Create(f).Error)

	ip := "10.99.21.9"
	db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
	t.Cleanup(func() {
		db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
		db.Get().Unscoped().Delete(&sub)
		db.Get().Unscoped().Delete(f)
	})

	body, err := json.Marshal(SlaveNodeUpsertRequest{
		Country: "HK", Name: "router-claim", SecretToken: "secret-router",
		PrivateClaim: claimToken,
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "ipv4", Value: ip}}
	c.Request = httptest.NewRequest("PUT", "/slave/nodes/"+ip, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	api_slave_node_upsert(c)

	resp, err := ParseResponse(w)
	require.NoError(t, err)
	require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "注册应成功: %s", resp.Message)

	// 订阅应已激活（前提条件，复用既有断言口径）。
	var reloadedSub PrivateNodeSubscription
	require.NoError(t, db.Get().First(&reloadedSub, sub.ID).Error)
	require.Equal(t, PNStatusActive, reloadedSub.Status, "订阅应激活")

	// 台账应在同一次激活里被推进到 ready。
	var reloadedF RouterFulfillment
	require.NoError(t, db.Get().First(&reloadedF, f.ID).Error)
	require.Equal(t, RouterStageReady, reloadedF.Stage, "路由器台账应在节点激活时推进到 ready")
}
