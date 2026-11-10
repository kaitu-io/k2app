package center

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
)

// 线上 2026-11：13 条 Apple 订阅的主人在试用期内自行注销了账号（用户行软删）。Apple 随后照常发
// EXPIRED / DID_CHANGE_RENEWAL_STATUS，webhook 查不到主人 → 500 → Apple 按重试策略连发数天，
// 每次都是一条 error 日志。账号已注销是终态：确认收到（200）、不入账、不再让 Apple 重试。
func TestAppleWebhook_DeletedOwnerIsAcked(t *testing.T) {
	skipIfNoDB(t)
	h := newTrialWebhook(t)

	user := CreateTestUser(t)
	uniq := time.Now().UnixNano()
	otx := fmt.Sprintf("OTX-GONE-%d", uniq)
	txn := fmt.Sprintf("TXN-GONE-%d", uniq)
	sub := &Subscription{UserID: user.ID, Provider: SubscriptionProviderApple, ProviderSubscriptionID: otx,
		ProductID: "io.kaitu.test.1y", ProviderLatestRef: txn, Status: "active", Environment: "Production",
		CurrentPeriodEnd: time.Now().Add(-time.Hour).Unix(), IsTrial: true, AutoRenew: true}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(sub) })
	require.NoError(t, db.Get().Delete(&User{}, user.ID).Error) // 用户自行注销 = 软删

	h.byID[txn] = &appstore.TransactionInfo{
		BundleId: "io.kaitu.test", ProductId: "io.kaitu.test.1y", OriginalTransactionId: otx, TransactionId: txn,
		PurchaseDate: time.Now().Add(-7 * 24 * time.Hour).UnixMilli(), ExpiresDate: time.Now().Add(-time.Hour).UnixMilli(),
		InAppOwnershipType: appstore.OwnershipType_PURCHASED, Environment: "Production",
	}
	assert.Equal(t, 200, h.post("EXPIRED", "VOLUNTARY", fmt.Sprintf("uuid-gone-%d", uniq), txn),
		"主人已注销：确认收到，别让 Apple 连发数天")

	var after Subscription
	require.NoError(t, db.Get().First(&after, sub.ID).Error)
	assert.Equal(t, sub.Status, after.Status, "不替已注销账号做任何状态变更")
}
