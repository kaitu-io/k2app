package center

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func seedTraffic(t *testing.T, date string, udid, ip string, userID uint, rx, tx int64) {
	t.Helper()
	require.NoError(t, db.Get().Create(&DeviceTrafficDaily{
		Date: date, UDID: udid, NodeIpv4: ip, UserID: userID, RxBytes: rx, TxBytes: tx,
	}).Error)
}

func TestQueryTrafficTopUsers(t *testing.T) {
	skipIfNoConfig(t)
	month := time.Now().In(cnZone).Format("2006-01")
	date := trafficDate(time.Now())
	t.Cleanup(func() {
		db.Get().Where("node_ipv4 LIKE ?", "10.98.%").Delete(&DeviceTrafficDaily{})
	})
	seedTraffic(t, date, "top-d1", "10.98.0.1", 9001, 100, 200) // user 9001: 300
	seedTraffic(t, date, "top-d2", "10.98.0.2", 9001, 50, 50)   // +100 → 400, 2 nodes 2 devices
	seedTraffic(t, date, "top-d3", "10.98.0.1", 9002, 10, 10)   // user 9002: 20
	seedTraffic(t, date, "top-d4", "10.98.0.1", 0, 5, 5)        // unattributed: 10

	rows, total, err := queryTrafficTopUsers(month, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, int64(430))

	// rows 按总量降序；找到我们种的两个用户
	var u9001, u9002 *TrafficTopUser
	for i := range rows {
		if rows[i].UserID == 9001 {
			u9001 = &rows[i]
		}
		if rows[i].UserID == 9002 {
			u9002 = &rows[i]
		}
	}
	require.NotNil(t, u9001)
	require.NotNil(t, u9002)
	assert.Equal(t, int64(400), u9001.TotalBytes)
	assert.Equal(t, 2, u9001.DeviceCount)
	assert.Equal(t, 2, u9001.NodeCount)
	assert.Equal(t, int64(20), u9002.TotalBytes)
}

func TestQueryTrafficUserDetail(t *testing.T) {
	skipIfNoConfig(t)
	month := time.Now().In(cnZone).Format("2006-01")
	date := trafficDate(time.Now())
	t.Cleanup(func() {
		db.Get().Where("node_ipv4 = ?", "10.97.0.1").Delete(&DeviceTrafficDaily{})
	})
	seedTraffic(t, date, "det-d1", "10.97.0.1", 9100, 100, 100)
	seedTraffic(t, date, "det-d2", "10.97.0.1", 9100, 30, 30)

	detail, err := queryTrafficUserDetail(9100, month)
	require.NoError(t, err)
	assert.Equal(t, int64(260), detail.TotalBytes)
	require.Len(t, detail.Daily, 1)
	assert.Equal(t, date, detail.Daily[0].Date)
	assert.Len(t, detail.Devices, 2)
	assert.Len(t, detail.Nodes, 1)
}
