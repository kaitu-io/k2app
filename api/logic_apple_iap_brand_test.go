package center

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
