package center

import (
	"testing"

	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestNodeProvisionJob_SubIDUnique — 一 sub 一 job 幂等保证（SubID uniqueIndex）。
// 第二条同 SubID 行必须被 1062 拒绝。需 dev MySQL。
func TestNodeProvisionJob_SubIDUnique(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	const fixedSubID uint64 = 990001

	// 自愈：先清掉残留行（含软删/无软删）。
	db.Get().Unscoped().Where("sub_id = ?", fixedSubID).Delete(&NodeProvisionJob{})
	t.Cleanup(func() {
		db.Get().Unscoped().Where("sub_id = ?", fixedSubID).Delete(&NodeProvisionJob{})
	})

	first := NodeProvisionJob{
		SubID:             fixedSubID,
		Status:            NPJStatusQueued,
		Region:            "japan",
		IPType:            IPTypeNonResidential,
		TrafficTotalBytes: 1 << 40,
	}
	require.NoError(t, db.Get().Create(&first).Error)

	second := NodeProvisionJob{
		SubID:             fixedSubID,
		Status:            NPJStatusQueued,
		Region:            "japan",
		IPType:            IPTypeNonResidential,
		TrafficTotalBytes: 1 << 40,
	}
	require.Error(t, db.Get().Create(&second).Error)
}
