package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// 真 MySQL：验证 AutoMigrate 后 brand 列存在且 default 生效
func TestBrandColumnsMigration(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	m := db.Get().Migrator()
	for _, tbl := range []interface{}{
		&User{}, &Plan{}, &Campaign{}, &Announcement{},
		&LicenseKeyBatch{}, &LicenseKey{}, &EmailMarketingTemplate{}, &LoginIdentify{},
	} {
		assert.True(t, m.HasColumn(tbl, "brand"), "%T missing brand column", tbl)
	}
	assert.True(t, m.HasColumn(&SlaveNode{}, "visible_kaitu"))
	assert.True(t, m.HasColumn(&SlaveNode{}, "visible_overleap"))
}

func TestSlaveNodeVisibleTo(t *testing.T) {
	// 生效可见性 = 节点声明该品牌（Brands 能力上限） ∧ 运营没下架（Visible* 开关）。
	n := &SlaveNode{} // 零值：Brands 空 → 只声明 kaitu；开关 nil → 未下架
	assert.True(t, n.VisibleTo(BrandKaitu))
	assert.False(t, n.VisibleTo(BrandOverleap)) // 未声明 overleap，开关再开也不可见

	// 声明双品牌后，开关才有决定权
	n.Brands = "kaitu,overleap"
	assert.True(t, n.VisibleTo(BrandKaitu))
	assert.True(t, n.VisibleTo(BrandOverleap))

	n.VisibleKaitu = BoolPtr(false)
	n.VisibleOverleap = BoolPtr(true)
	assert.False(t, n.VisibleTo(BrandKaitu)) // kill switch 下架
	assert.True(t, n.VisibleTo(BrandOverleap))

	// 只有开关、没有声明 → 仍不可见（能力上限优先）
	n.Brands = ""
	assert.False(t, n.VisibleTo(BrandOverleap))
}
