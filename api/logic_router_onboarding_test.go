package center

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func TestOnboarding_RouterOrderUsesRouterCopy(t *testing.T) {
	skipIfNoConfig(t)
	sub := seedTestPrivateSub(t)
	f := &RouterFulfillment{OrderID: 900000 + sub.ID, UserID: sub.UserID, SubID: sub.ID,
		HardwareSKU: "redmi-ax6s", Stage: RouterStagePaid}
	require.NoError(t, db.Get().Create(f).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(f) })
	t.Cleanup(func() {
		db.Get().Unscoped().Where("feedback_id = ?", privateNodeInstallFeedbackID(sub.ID)).Delete(&FeedbackTicket{})
	})

	onPrivateNodeOrderOnboarding(context.Background(), sub.ID)

	var ticket FeedbackTicket
	require.NoError(t, db.Get().Where("feedback_id = ?", privateNodeInstallFeedbackID(sub.ID)).First(&ticket).Error)
	assert.Contains(t, ticket.Content, "路由器")
	assert.NotContains(t, ticket.Content, "Kaitu")
	assert.Contains(t, ticket.Meta, `"type":"router_order"`)
}

func TestOnboarding_PlainLineKeepsLineCopy(t *testing.T) {
	skipIfNoConfig(t)
	sub := seedTestPrivateSub(t)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("feedback_id = ?", privateNodeInstallFeedbackID(sub.ID)).Delete(&FeedbackTicket{})
	})
	onPrivateNodeOrderOnboarding(context.Background(), sub.ID)
	var ticket FeedbackTicket
	require.NoError(t, db.Get().Where("feedback_id = ?", privateNodeInstallFeedbackID(sub.ID)).First(&ticket).Error)
	assert.Contains(t, ticket.Meta, `"type":"private_node_install"`)
}
