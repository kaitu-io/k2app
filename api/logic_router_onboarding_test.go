package center

import (
	"context"
	"strings"
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

// TestPrivateNodeOnboardingCopy 是纯函数单测(无需 DB/config)：锁定 privateNodeOnboardingCopy
// 两个分支各自的四元组字面量，覆盖此前完全没有断言的 welcomeSlug / slackTitle。
func TestPrivateNodeOnboardingCopy(t *testing.T) {
	t.Run("router", func(t *testing.T) {
		content, ticketType, welcomeSlug, slackTitle := privateNodeOnboardingCopy(true)
		assert.True(t, strings.HasPrefix(content, "您好!感谢购买开途路由器版"))
		assert.NotContains(t, content, "Kaitu")
		assert.Equal(t, "router_order", ticketType)
		assert.Equal(t, "router-welcome", welcomeSlug)
		assert.Equal(t, "Router Edition Order — Fulfillment Needed", slackTitle)
	})

	t.Run("plain_line", func(t *testing.T) {
		content, ticketType, welcomeSlug, slackTitle := privateNodeOnboardingCopy(false)
		assert.Equal(t, privateNodeInstallContent, content)
		assert.NotContains(t, content, "Kaitu")
		assert.Equal(t, "private_node_install", ticketType)
		assert.Equal(t, "private-node-welcome", welcomeSlug)
		assert.Equal(t, "Dedicated Line Order — Install Needed", slackTitle)
	})
}
