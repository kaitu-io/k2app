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
	n := &SlaveNode{} // 零值：两个指针都 nil → 视为 default（kaitu 可见 / overleap 不可见）
	assert.True(t, n.VisibleTo(BrandKaitu))
	assert.False(t, n.VisibleTo(BrandOverleap))

	n.VisibleKaitu = BoolPtr(false)
	n.VisibleOverleap = BoolPtr(true)
	assert.False(t, n.VisibleTo(BrandKaitu))
	assert.True(t, n.VisibleTo(BrandOverleap))
}
