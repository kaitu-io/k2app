package center

import (
	"context"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
)

// planByAppleProductID 必须品牌过滤：同一 apple product id 落错品牌绝不入账
// （api/CLAUDE.md 既有 pre-flight 悬项，本测试关闭之）。
func TestPlanByAppleProductID_BrandScoped(t *testing.T) {
	skipIfNoConfig(t)
	tx := db.Get()

	// 制造一个 overleap 品牌、带 apple product id 的套餐（测试后清理）。
	p := &Plan{
		PID: "t-ol-apple-1y", Tier: "basic", Label: "t", Month: 12,
		Product: "app", IsActive: BoolPtr(true), Brand: string(BrandOverleap),
		AppleProductID: "io.overleap.sub.test.brandscope",
	}
	require.NoError(t, tx.Create(p).Error)
	t.Cleanup(func() { tx.Unscoped().Delete(p) })

	got, err := planByAppleProductID(context.Background(), tx, p.AppleProductID, BrandOverleap)
	require.NoError(t, err)
	assert.Equal(t, p.PID, got.PID)

	// 关键断言：kaitu 品牌查同一 product id 必须查不到。
	_, err = planByAppleProductID(context.Background(), tx, p.AppleProductID, BrandKaitu)
	assert.Error(t, err, "cross-brand apple product id must not resolve")
}

func TestAppleBundleIDForBrand(t *testing.T) {
	viper.Set("appstore.bundleId", "test.kaitu.bundle")
	viper.Set("appstore.bundleIds.overleap", "test.overleap.bundle")
	t.Cleanup(func() {
		viper.Set("appstore.bundleId", nil)
		viper.Set("appstore.bundleIds.overleap", nil)
	})

	assert.Equal(t, "test.kaitu.bundle", appleBundleIDForBrand(BrandKaitu),
		"kaitu 必须沿用 legacy 键 appstore.bundleId（零破坏）")
	assert.Equal(t, "test.overleap.bundle", appleBundleIDForBrand(BrandOverleap))
}

// verify 链路必须用用户品牌的 bundle id 复核；bundle 不匹配 = 跨 app 交易，拒绝。
func TestVerifyAndGrant_BundleBrandIsolation(t *testing.T) {
	skipIfNoConfig(t)
	viper.Set("appstore.bundleIds.overleap", "test.overleap.bundle")
	t.Cleanup(func() { viper.Set("appstore.bundleIds.overleap", nil) })

	user, _ := createBrandIsoAccessKeyUser(t, BrandOverleap, false)

	orig := fetchAppleTransaction
	t.Cleanup(func() { fetchAppleTransaction = orig })
	var gotBundleID string
	fetchAppleTransaction = func(ctx context.Context, bundleID, txnID string) (*appstore.TransactionInfo, error) {
		gotBundleID = bundleID
		// 返回 kaitu app 的交易 —— 必须被 bundle 校验拒绝。
		return &appstore.TransactionInfo{BundleId: "test.kaitu.bundle", ProductId: "io.overleap.sub.basic.1y"}, nil
	}

	err := verifyAndGrantTransaction(context.Background(), user.ID, "txn-cross-bundle")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bundle mismatch")
	assert.Equal(t, "test.overleap.bundle", gotBundleID,
		"必须以用户品牌的 bundle id 向 Apple 复核")
}
