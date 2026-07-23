package center

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// 直接测 ingest 逻辑函数（handler 薄壳只做 bind/auth）。
func TestDeviceTrafficIngest_UpsertAndIdempotency(t *testing.T) {
	skipIfNoConfig(t)
	ip := "10.99.0.1"
	t.Cleanup(func() {
		db.Get().Where("node_ipv4 = ?", ip).Delete(&DeviceTrafficDaily{})
		db.Get().Where("ipv4 = ?", ip).Delete(&DeviceTrafficCursor{})
	})

	req := DeviceTrafficRequest{
		BootID: "boot-A", BatchSeq: 1, Ts: time.Now().Unix(),
		Devices: []DeviceTrafficItem{{UDID: "udid-t1", Rx: 100, Tx: 200}},
	}
	require.NoError(t, ingestDeviceTraffic(nil, ip, &req))

	var row DeviceTrafficDaily
	require.NoError(t, db.Get().Where("node_ipv4 = ? AND udid = ?", ip, "udid-t1").First(&row).Error)
	assert.Equal(t, int64(100), row.RxBytes)
	assert.Equal(t, int64(200), row.TxBytes)
	assert.Equal(t, trafficDate(time.Now()), row.Date)

	// 同日第二批 → 累加
	req2 := DeviceTrafficRequest{BootID: "boot-A", BatchSeq: 2, Ts: time.Now().Unix(),
		Devices: []DeviceTrafficItem{{UDID: "udid-t1", Rx: 1, Tx: 2}}}
	require.NoError(t, ingestDeviceTraffic(nil, ip, &req2))
	require.NoError(t, db.Get().Where("node_ipv4 = ? AND udid = ?", ip, "udid-t1").First(&row).Error)
	assert.Equal(t, int64(101), row.RxBytes)
	assert.Equal(t, int64(202), row.TxBytes)

	// 重发 batch 1（ack 丢失场景）→ 必须被幂等跳过，不再累加
	require.NoError(t, ingestDeviceTraffic(nil, ip, &req))
	require.NoError(t, db.Get().Where("node_ipv4 = ? AND udid = ?", ip, "udid-t1").First(&row).Error)
	assert.Equal(t, int64(101), row.RxBytes, "duplicate batch must not double-count")

	// 重启换 boot_id、seq 归 1 → 必须被接受
	req3 := DeviceTrafficRequest{BootID: "boot-B", BatchSeq: 1, Ts: time.Now().Unix(),
		Devices: []DeviceTrafficItem{{UDID: "udid-t1", Rx: 10, Tx: 0}}}
	require.NoError(t, ingestDeviceTraffic(nil, ip, &req3))
	require.NoError(t, db.Get().Where("node_ipv4 = ? AND udid = ?", ip, "udid-t1").First(&row).Error)
	assert.Equal(t, int64(111), row.RxBytes, "new boot_id must be accepted")
}

func TestDeviceTrafficIngest_ResolvesUserID(t *testing.T) {
	skipIfNoConfig(t)
	ip := "10.99.0.2"
	// 造一个带 user 的 device
	dev := Device{UDID: "udid-owned-1", UserID: 4242}
	require.NoError(t, db.Get().Create(&dev).Error)
	t.Cleanup(func() {
		db.Get().Where("udid = ?", "udid-owned-1").Delete(&Device{})
		db.Get().Where("node_ipv4 = ?", ip).Delete(&DeviceTrafficDaily{})
		db.Get().Where("ipv4 = ?", ip).Delete(&DeviceTrafficCursor{})
	})

	req := DeviceTrafficRequest{BootID: "boot-C", BatchSeq: 1, Ts: time.Now().Unix(),
		Devices: []DeviceTrafficItem{
			{UDID: "udid-owned-1", Rx: 5, Tx: 5},
			{UDID: "udid-unknown-x", Rx: 3, Tx: 3}, // 未注册设备 → user_id=0 仍记账
		}}
	require.NoError(t, ingestDeviceTraffic(nil, ip, &req))

	var owned, unknown DeviceTrafficDaily
	require.NoError(t, db.Get().Where("node_ipv4 = ? AND udid = ?", ip, "udid-owned-1").First(&owned).Error)
	require.NoError(t, db.Get().Where("node_ipv4 = ? AND udid = ?", ip, "udid-unknown-x").First(&unknown).Error)
	assert.Equal(t, uint(4242), owned.UserID)
	assert.Equal(t, uint(0), unknown.UserID)
}

func TestDeviceTrafficRequest_WireShape(t *testing.T) {
	b, err := json.Marshal(DeviceTrafficRequest{
		BootID: "b1", BatchSeq: 2, Ts: 3,
		Devices: []DeviceTrafficItem{{UDID: "u", Rx: 1, Tx: 2}},
	})
	require.NoError(t, err)
	assert.Equal(t,
		`{"boot_id":"b1","batch_seq":2,"ts":3,"devices":[{"udid":"u","rx":1,"tx":2}]}`,
		string(b))
}
