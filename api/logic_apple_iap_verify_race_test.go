package center

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// 线上 2026-11-05 / 11-09 五例（user 14173/14183/14691/14732/14813）：App 对同一笔交易几乎同时
// 发了两次 /api/user/apple-iap/verify。第一次入账成功；第二次的事务在第一次提交前就已用普通读
// （planByAppleProductID）建立了 REPEATABLE READ 快照，随后订阅行的 FOR UPDATE 等到第一次提交、
// 读到最新行，但入账去重查询是普通读、仍用旧快照 → 看不到第一次写的 subscription_credits →
// 再次插入 → 1062 uniq_provider_txn → 返回 "verification failed"（用户其实已到账）。

type verifyRaceFixture struct {
	user    *User
	otx     string
	product string
}

func setupVerifyRace(t *testing.T) *verifyRaceFixture {
	t.Helper()
	skipIfNoDB(t)
	viper.Set("appstore.bundleId", "io.kaitu.test")
	t.Cleanup(func() { viper.Set("appstore.bundleId", "") })

	uniq := time.Now().UnixNano()
	f := &verifyRaceFixture{
		product: fmt.Sprintf("io.kaitu.test.race.1y.%d", uniq),
		otx:     fmt.Sprintf("OTX-RACE-%d", uniq),
	}
	plan := &Plan{PID: fmt.Sprintf("trace%d", uniq%1e12), Label: "Race 1Y", Price: 1000, OriginPrice: 1000,
		Month: 12, Tier: "basic", AppleProductID: f.product}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Delete(plan) })

	f.user = CreateTestUser(t)
	token := deriveAppleAccountToken(f.user.UUID)
	now := time.Now().Unix()

	old := fetchAppleTransaction
	fetchAppleTransaction = func(ctx context.Context, bundleId, transactionID string) (*appstore.TransactionInfo, error) {
		return &appstore.TransactionInfo{
			BundleId: appleBundleID(), ProductId: f.product, OriginalTransactionId: f.otx,
			TransactionId: transactionID, AppAccountToken: token,
			PurchaseDate: now * 1000, ExpiresDate: (now + 365*86400) * 1000,
			InAppOwnershipType: appstore.OwnershipType_PURCHASED, Environment: "Sandbox",
		}, nil
	}
	t.Cleanup(func() { fetchAppleTransaction = old })
	t.Cleanup(func() {
		db.Get().Where("provider = ? AND provider_subscription_id = ?", "apple", f.otx).Delete(&Subscription{})
		db.Get().Where("user_id = ?", f.user.ID).Delete(&SubscriptionCredit{})
		db.Get().Where("user_id = ? AND type = ?", f.user.ID, VipAppleSub).Delete(&UserProHistory{})
	})
	return f
}

func (f *verifyRaceFixture) credits(t *testing.T) int64 {
	var n int64
	require.NoError(t, db.Get().Model(&SubscriptionCredit{}).Where("user_id = ?", f.user.ID).Count(&n).Error)
	return n
}

// 确定性重放线上交错：第二个请求的事务先用普通读建立快照，第一个请求随后完整入账并提交，
// 第二个请求再执行入账。必须按"已入账"幂等成功，而不是 1062。
func TestCreditAppleTransaction_StaleSnapshotDuplicateVerify(t *testing.T) {
	f := setupVerifyRace(t)
	ctx := context.Background()

	tx2 := db.Get().Begin()
	t.Cleanup(func() { tx2.Rollback() })
	// 与 verifyAndGrantTransaction 相同的第一条语句：普通读建立 REPEATABLE READ 快照。
	_, err := planByAppleProductID(ctx, tx2, f.product, BrandKaitu)
	require.NoError(t, err)

	// 第一个请求完整跑完并提交。
	require.NoError(t, verifyAndGrantTransaction(ctx, f.user.ID, "TXN-RACE-1"))
	var after1 User
	require.NoError(t, db.Get().First(&after1, f.user.ID).Error)

	info, err := fetchAppleTransaction(ctx, "", "TXN-RACE-1")
	require.NoError(t, err)
	err = creditAppleTransaction(ctx, tx2, f.user.ID, info)
	if err == nil {
		// RDS 10.6（线上）：加锁读去重看到前者的 credit，走幂等分支。
		require.NoError(t, tx2.Commit().Error)
	} else {
		// MariaDB ≥11.6（CI）：snapshot isolation 直接报 1020；只允许这种可重试冲突，
		// 且按 withDeadlockRetry 的做法开新事务重来必须幂等成功。
		require.True(t, isDeadlockError(err), "只允许可重试的事务冲突，得到: %v", err)
		tx2.Rollback()
		require.NoError(t, db.Get().Transaction(func(tx *gorm.DB) error {
			return creditAppleTransaction(ctx, tx, f.user.ID, info)
		}), "重试后同一交易的第二次 verify 必须幂等成功")
	}

	var after2 User
	require.NoError(t, db.Get().First(&after2, f.user.ID).Error)
	assert.Equal(t, after1.ExpiredAt, after2.ExpiredAt, "不得二次入账")
	assert.Equal(t, int64(1), f.credits(t))
}

// 真并发：同一交易 4 个并发 verify 全部成功，只入账一次。
func TestVerifyAndGrantTransaction_ConcurrentSameTxn(t *testing.T) {
	f := setupVerifyRace(t)
	ctx := context.Background()

	const n = 4
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = verifyAndGrantTransaction(ctx, f.user.ID, "TXN-RACE-C")
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		assert.NoError(t, err, "verify #%d", i)
	}
	assert.Equal(t, int64(1), f.credits(t))
	var u User
	require.NoError(t, db.Get().First(&u, f.user.ID).Error)
	assert.InDelta(t, time.Now().Unix()+365*86400, u.ExpiredAt, 30, "只入账一期")
}
