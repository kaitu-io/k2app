package center

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	wordgate "github.com/wordgate/wordgate-sdk"
)

// TestPrivateNodePurchaseChain_Webhook_EndToEnd is the #42 keystone smoke:
// it drives a paid private_node order through the REAL webhook entry point
// (handleWordgateOrderPaidEvent — signature/HTTP layer intentionally skipped,
// that layer is product-agnostic and shared with every order) and asserts the
// WHOLE chain couples in one flow against the real dev MySQL:
//
//	webhook → MarkOrderAsPaid → applyOrderToBuyer → createPrivateNodeSubscription
//	        → post-commit: enqueueProvision + onPrivateNodeOrderOnboarding (install ticket)
//	worker  → handleProvisionPrivateNode → emitNodeProvisionJob → NodeOperation(queued)
//
// Every single link already has a unit test; what was missing — and what this
// keystone retires — is the SEAM: does one real paid order thread end-to-end
// and produce all the artifacts, idempotently, without touching the shared clock.
//
// Note on the async worker: depending on whether an earlier test in the package
// registered asynq handlers, the in-process miniredis worker may OR may not be
// running, so the enqueued provision task may get auto-processed in the background.
// The chain is robust to both: the atomic pending→provisioning gate + the open-slot
// dedup in dispatchNodeOperation guarantee exactly one NodeOperation regardless of
// who drives the worker. We therefore assert the sub is in a valid early state
// (pending OR provisioning) right after the webhook, and drive the worker handler
// ourselves to guarantee the provision step runs — mirroring production (webhook
// commits money-critical rows; Asynq later provisions), idempotently.
func TestPrivateNodePurchaseChain_Webhook_EndToEnd(t *testing.T) {
	skipIfNoConfig(t)

	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	now := time.Now()
	stamp := now.Format("20060102150405.000000")

	// ---- 1. owner: future ExpiredAt so we can prove the private clock is independent ----
	owner := User{
		UUID:      "usr-pnwh-" + stamp,
		Language:  "zh-CN",
		ExpiredAt: now.Add(30 * 24 * time.Hour).Unix(),
	}
	require.NoError(t, db.Get().Create(&owner).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&owner) })
	expiredAtBefore := owner.ExpiredAt

	// ---- 2. private_node Plan + spec (annual: Month=12) ----
	plan := Plan{
		PID:     "pn-wh-" + stamp,
		Label:   "专属线路·数据中心 2T (webhook smoke)",
		Price:   20400,
		Month:   12,
		Product: ProductPrivateNode,
		Tier:    TierBasic,
	}
	require.NoError(t, db.Get().Create(&plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&plan) })

	const traffic = int64(2 * 1024 * 1024 * 1024 * 1024) // 2 TiB
	spec := PrivateNodePlanSpec{
		PlanID: plan.ID,
		IPType: IPTypeNonResidential,
		// "japan" is first; we deliberately buy "ap-northeast-1" (NOT first) to prove
		// the sub region comes from the buyer's pick on the Order, not the list fallback.
		AllowedRegions:    `["japan","ap-northeast-1"]`,
		TrafficTotalBytes: traffic,
	}
	require.NoError(t, db.Get().Create(&spec).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&spec) })

	// ---- 3. UNPAID order carrying a wordgate order no + the buyer-picked region ----
	const pickedRegion = "ap-northeast-1"
	wgOrderNo := "wg-pnwh-" + stamp
	order := Order{
		UUID:              "ord-pnwh-" + stamp,
		Title:             "Private Node Webhook Smoke",
		UserID:            owner.ID,
		PayAmount:         20400,
		WordgateOrderNo:   wgOrderNo,
		PrivateNodeRegion: pickedRegion,
		Meta:              "{}",
		// IsPaid intentionally nil/false — the webhook is what flips it.
	}
	require.NoError(t, order.SetPlan(&plan))
	require.NoError(t, db.Get().Create(&order).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&order) })
	t.Cleanup(func() {
		db.Get().Unscoped().Where("order_id = ?", order.ID).Delete(&PrivateNodeSubscription{})
	})

	// ---- 4. fire the real webhook handler (order.paid) ----
	paidAt := now
	event := &wordgate.WebhookEventData{
		EventType: wordgate.WebhookEventOrderPaid,
		Data: wordgate.WebhookOrderPaidData{
			WordgateOrderNo: wgOrderNo,
			Amount:          20400,
			Currency:        "USD",
			IsPaid:          true,
			PaidAt:          &paidAt,
		},
		Timestamp: now.Unix(),
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, handleWordgateOrderPaidEvent(c, event))

	// ---- 5. order is now paid ----
	var paidOrder Order
	require.NoError(t, db.Get().First(&paidOrder, order.ID).Error)
	require.NotNil(t, paidOrder.IsPaid)
	assert.True(t, *paidOrder.IsPaid, "webhook must mark the order paid")

	// ---- 6. exactly one pending subscription, with the buyer-picked region ----
	var subs []PrivateNodeSubscription
	require.NoError(t, db.Get().Where("order_id = ?", order.ID).Find(&subs).Error)
	require.Len(t, subs, 1, "webhook must create exactly one private-node subscription")
	sub := subs[0]
	t.Cleanup(func() {
		db.Get().Unscoped().Where("sub_id = ?", sub.ID).Delete(&NodeOperation{})
		db.Get().Unscoped().Where("feedback_id = ?", privateNodeInstallFeedbackID(sub.ID)).Delete(&FeedbackTicket{})
	})

	// pending right after the webhook, OR already provisioning if a background asynq
	// worker raced ahead (see top-of-func note). Both are valid post-purchase states.
	assert.Contains(t, []string{PNStatusPending, PNStatusProvisioning}, sub.Status,
		"sub must be in a valid early state after purchase")
	assert.Equal(t, owner.ID, sub.UserID)
	assert.Equal(t, plan.ID, sub.PlanID)
	assert.Equal(t, IPTypeNonResidential, sub.IPType)
	assert.Equal(t, traffic, sub.TrafficTotalBytes)
	assert.Equal(t, pickedRegion, sub.Region, "region must come from Order.PrivateNodeRegion, not AllowedRegions[0]")
	assert.NotEmpty(t, sub.ProvisionClaimToken, "claim token must be minted at purchase")
	// independent clock: ~ purchasedAt + 12 months, and the buyer's shared ExpiredAt untouched.
	expectedExpiry := time.Unix(sub.PurchasedAt, 0).AddDate(0, plan.Month, 0).Unix()
	assert.Equal(t, expectedExpiry, sub.ExpiresAt, "private-node ExpiresAt is an independent 12-month clock")

	// ---- 7. buyer flags set, shared membership clock UNCHANGED ----
	var reloadedOwner User
	require.NoError(t, db.Get().First(&reloadedOwner, owner.ID).Error)
	require.NotNil(t, reloadedOwner.IsFirstOrderDone)
	assert.True(t, *reloadedOwner.IsFirstOrderDone)
	require.NotNil(t, reloadedOwner.IsActivated)
	assert.True(t, *reloadedOwner.IsActivated)
	assert.Equal(t, expiredAtBefore, reloadedOwner.ExpiredAt, "private-node purchase must NOT touch User.ExpiredAt")

	// ---- 8. white-glove install ticket created (deterministic FeedbackID, idempotent) ----
	var ticket FeedbackTicket
	require.NoError(t, db.Get().Where("feedback_id = ?", privateNodeInstallFeedbackID(sub.ID)).First(&ticket).Error)
	assert.True(t, ticket.AutoGenerated)
	assert.Equal(t, "open", ticket.Status)
	require.NotNil(t, ticket.UserID)
	assert.Equal(t, owner.ID, *ticket.UserID)
	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(ticket.Meta), &meta))
	assert.Equal(t, "private_node_install", meta["type"])

	// ---- 9. worker picks up the provision task → NodeOperation(queued) with the right intent ----
	payload, err := json.Marshal(ProvisionPayload{SubID: sub.ID})
	require.NoError(t, err)
	require.NoError(t, handleProvisionPrivateNode(ctx, payload))

	var ops []NodeOperation
	require.NoError(t, db.Get().Where("sub_id = ? AND action = ?", sub.ID, NodeOpProvision).Find(&ops).Error)
	require.Len(t, ops, 1, "worker must enqueue exactly one provision NodeOperation")
	assert.Equal(t, NodeOpQueued, ops[0].Status)
	assert.Equal(t, "system:order", ops[0].CreatedBy)
	var params ProvisionParams
	require.NoError(t, json.Unmarshal([]byte(ops[0].Params), &params))
	assert.Equal(t, pickedRegion, params.Region)
	assert.Equal(t, IPTypeNonResidential, params.IPType)
	assert.Equal(t, traffic, params.TrafficTotalBytes)

	// sub advanced pending → provisioning (atomic gate), still independent clock.
	var afterWorker PrivateNodeSubscription
	require.NoError(t, db.Get().First(&afterWorker, sub.ID).Error)
	assert.Equal(t, PNStatusProvisioning, afterWorker.Status)

	// ---- 10. idempotency: replaying the SAME webhook + worker must NOT duplicate ----
	require.NoError(t, handleWordgateOrderPaidEvent(c, event)) // FOR UPDATE sees is_paid=true → skips
	require.NoError(t, handleProvisionPrivateNode(ctx, payload))

	var subCount, ticketCount, opCount int64
	require.NoError(t, db.Get().Model(&PrivateNodeSubscription{}).Where("order_id = ?", order.ID).Count(&subCount).Error)
	require.NoError(t, db.Get().Model(&FeedbackTicket{}).Where("feedback_id = ?", privateNodeInstallFeedbackID(sub.ID)).Count(&ticketCount).Error)
	require.NoError(t, db.Get().Model(&NodeOperation{}).Where("sub_id = ? AND action = ?", sub.ID, NodeOpProvision).Count(&opCount).Error)
	assert.Equal(t, int64(1), subCount, "replay must not create a second subscription")
	assert.Equal(t, int64(1), ticketCount, "replay must not create a second install ticket")
	assert.Equal(t, int64(1), opCount, "replay must not create a second provision operation")
}
