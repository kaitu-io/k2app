package center

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func TestOnboarding_CreatesTicketIdempotent(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	sub := seedTestPrivateSub(t)

	onPrivateNodeOrderOnboarding(ctx, sub.ID)
	onPrivateNodeOrderOnboarding(ctx, sub.ID) // 重复调用应幂等(FeedbackID 唯一)

	var count int64
	require.NoError(t, db.Get().Model(&FeedbackTicket{}).Where("feedback_id = ?", privateNodeInstallFeedbackID(sub.ID)).Count(&count).Error)
	assert.Equal(t, int64(1), count, "install ticket must be created exactly once")
	t.Cleanup(func() { db.Get().Unscoped().Where("feedback_id = ?", privateNodeInstallFeedbackID(sub.ID)).Delete(&FeedbackTicket{}) })
}
