package center

import (
	"testing"

	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestCloudInstanceWarnEpochColumns verifies the traffic-warning dedup columns
// (Warn70SentEpoch / Warn80SentEpoch / Warn90SentEpoch / Exhausted100SentEpoch)
// round-trip through the DB. The warn worker compares these against TrafficEpoch
// to fire 70/80/90/100% alerts at most once per billing epoch.
func TestCloudInstanceWarnEpochColumns(t *testing.T) {
	skipIfNoConfig(t)
	ci := &CloudInstance{
		Provider: "test", AccountName: "a", InstanceID: "i-warn-1",
		IPAddress: "1.2.3.4", Region: "jp",
		TrafficTotalBytes: 100, TrafficEpoch: 7,
		Warn70SentEpoch: 0, Warn80SentEpoch: 7, Warn90SentEpoch: 0, Exhausted100SentEpoch: 0,
	}
	require.NoError(t, db.Get().Create(ci).Error)
	t.Cleanup(func() { db.Get().Unscoped().Where("instance_id = ?", "i-warn-1").Delete(&CloudInstance{}) })

	var got CloudInstance
	require.NoError(t, db.Get().Where("instance_id = ?", "i-warn-1").First(&got).Error)
	require.Equal(t, int64(0), got.Warn70SentEpoch)
	require.Equal(t, int64(7), got.Warn80SentEpoch)
	require.Equal(t, int64(0), got.Warn90SentEpoch)
	require.Equal(t, int64(0), got.Exhausted100SentEpoch)
}
