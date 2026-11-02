package center

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func TestClassifyNodeStatus(t *testing.T) {
	base := nodeStatusInput{Visible: true, ActiveDevices: 3}
	with := func(f func(*nodeStatusInput)) nodeStatusInput { in := base; f(&in); return in }

	assert.Equal(t, nodeStatusNormal, classifyNodeStatus(base))
	assert.Equal(t, nodeStatusIdle, classifyNodeStatus(with(func(i *nodeStatusInput) { i.ActiveDevices = 0 })))
	assert.Equal(t, nodeStatusOverloaded, classifyNodeStatus(with(func(i *nodeStatusInput) { i.PressureFresh, i.Pressure = true, 0.9 })))
	// A stale sample is no evidence of load.
	assert.Equal(t, nodeStatusNormal, classifyNodeStatus(with(func(i *nodeStatusInput) { i.Pressure = 1 })))
	// Nodes clients are not sent to are never reported as placement failures.
	assert.Equal(t, nodeStatusExhausted, classifyNodeStatus(with(func(i *nodeStatusInput) { i.OverQuota, i.ActiveDevices = true, 0 })))
	assert.Equal(t, nodeStatusOffline, classifyNodeStatus(with(func(i *nodeStatusInput) { i.Offline, i.ActiveDevices = true, 0 })))
	assert.Equal(t, nodeStatusHidden, classifyNodeStatus(with(func(i *nodeStatusInput) { i.Visible, i.ActiveDevices = false, 0 })))
	assert.Equal(t, nodeStatusPrivate, classifyNodeStatus(with(func(i *nodeStatusInput) { i.Private, i.ActiveDevices = true, 0 })))
}

func distItem(name, country, status string, devices int) NodeDistributionItem {
	it := NodeDistributionItem{Status: status, ActiveDevices: devices}
	it.Name, it.Country = name, country
	return it
}

func TestSummarizeNodeScope_Verdicts(t *testing.T) {
	balanced := summarizeNodeScope("JP", []NodeDistributionItem{
		distItem("a", "JP", nodeStatusNormal, 10), distItem("b", "JP", nodeStatusNormal, 8),
	})
	assert.Equal(t, nodeVerdictOK, balanced.Verdict)

	skewed := summarizeNodeScope("SG", []NodeDistributionItem{
		distItem("a", "SG", nodeStatusNormal, 71), distItem("b", "SG", nodeStatusIdle, 0),
	})
	assert.Equal(t, nodeVerdictSkewed, skewed.Verdict)
	assert.Equal(t, "a", skewed.MaxNode)
	assert.InDelta(t, 1.0, skewed.TopShare, 1e-9)

	bad := summarizeNodeScope("KR", []NodeDistributionItem{
		distItem("a", "KR", nodeStatusOverloaded, 50), distItem("b", "KR", nodeStatusIdle, 0),
	})
	assert.Equal(t, nodeVerdictImbalanced, bad.Verdict)

	// All quiet at night is not a placement problem.
	quiet := summarizeNodeScope("HK", []NodeDistributionItem{
		distItem("a", "HK", nodeStatusIdle, 0), distItem("b", "HK", nodeStatusIdle, 0),
	})
	assert.Equal(t, nodeVerdictOK, quiet.Verdict)

	// Offline and exhausted nodes stay out of the balance arithmetic.
	mixed := summarizeNodeScope("US", []NodeDistributionItem{
		distItem("a", "US", nodeStatusNormal, 20), distItem("b", "US", nodeStatusOffline, 0),
		distItem("c", "US", nodeStatusExhausted, 40), distItem("d", "US", nodeStatusPrivate, 2),
	})
	assert.Equal(t, 4, mixed.Nodes)
	assert.Equal(t, 1, mixed.EligibleNodes)
	assert.Equal(t, 20, mixed.ActiveDevices)
	assert.Equal(t, nodeVerdictOK, mixed.Verdict)
}

func TestSummarizeNodeDistribution_FleetFirstThenCountries(t *testing.T) {
	out := summarizeNodeDistribution([]NodeDistributionItem{
		distItem("b", "SG", nodeStatusNormal, 1), distItem("a", "JP", nodeStatusNormal, 2),
	})
	require.Len(t, out, 3)
	assert.Equal(t, []string{"all", "JP", "SG"}, []string{out[0].Scope, out[1].Scope, out[2].Scope})
	assert.Equal(t, 3, out[0].ActiveDevices)
}

func TestBuildNodeTrend_FoldsBucketsAndDropsOutOfScope(t *testing.T) {
	names := map[string]string{"1.1.1.1": "a", "2.2.2.2": "b"}
	pts := buildNodeTrend([]nodeBucketCount{
		{Bucket: 300, NodeIpv4: "1.1.1.1", Devices: 3},
		{Bucket: 300, NodeIpv4: "2.2.2.2", Devices: 1},
		{Bucket: 300, NodeIpv4: "9.9.9.9", Devices: 99},
		{Bucket: 600, NodeIpv4: "2.2.2.2", Devices: 4},
	}, names)
	require.Len(t, pts, 2)
	assert.Equal(t, 4, pts[0].ActiveDevices)
	assert.Equal(t, 2, pts[0].ActiveNodes)
	assert.Equal(t, "a", pts[0].MaxNode)
	assert.InDelta(t, 0.75, pts[0].TopShare, 1e-9)
	assert.Equal(t, 2, pts[1].NodesInScope)
}

func TestBuildNodeDistributionItem_OldSidecarLoadStaysNil(t *testing.T) {
	now := time.Now().Unix()
	n := SlaveNode{Name: "x", Ipv4: "3.3.3.3", Class: "shared"}
	item := buildNodeDistributionItem(n, nil, &SlaveNodeLoad{MetricsVersion: 0, CPUPercent: 99, MemoryUsagePercent: 30, CreatedAt: time.Now()}, nodeActivity{Devices: 4}, 7, now)
	assert.Nil(t, item.CPUPercent, "metrics v0 CPU is a boot-time average, never show it")
	assert.Nil(t, item.BandwidthMbps)
	require.NotNil(t, item.MemoryPercent)
	assert.Equal(t, 7, item.PeakDevices24h)
}

func TestRecordNodePresence_AccumulatesAndCountsDistinctDevices(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, db.Get().AutoMigrate(&NodeDevicePresence{}))
	ip := "10.99.8.1"
	t.Cleanup(func() { db.Get().Where("node_ipv4 = ?", ip).Delete(&NodeDevicePresence{}) })
	now := time.Now().Unix()

	items := []DeviceTrafficItem{
		{UDID: "u1", SessionID: "s1", Rx: 1, Tx: 2},
		{UDID: "u1", SessionID: "s2", Rx: 1, Tx: 1},
		{UDID: "u2", SessionID: "s3", Rx: 5, Tx: 5},
		{UDID: "", SessionID: "", Rx: 9, Tx: 9},
	}
	require.NoError(t, recordNodePresence(context.Background(), ip, items, map[string]uint{"u1": 7}, now))
	require.NoError(t, recordNodePresence(context.Background(), ip, items[:1], nil, now))

	var row NodeDevicePresence
	require.NoError(t, db.Get().Where("node_ipv4 = ? AND udid = ? AND session_id = ?", ip, "u1", "s1").First(&row).Error)
	assert.Equal(t, int64(2), row.RxBytes)
	var total int64
	db.Get().Model(&NodeDevicePresence{}).Where("node_ipv4 = ?", ip).Count(&total)
	assert.Equal(t, int64(3), total, "unattributed traffic is not a device")

	act, err := loadNodeActivity(now)
	require.NoError(t, err)
	assert.Equal(t, nodeActivity{Devices: 2, Sessions: 3}, act[ip])

	// Past the window the device is gone even though its bucket is still recent.
	later, err := loadNodeActivity(now + nodePresenceWindowSeconds + 1)
	require.NoError(t, err)
	assert.Equal(t, 0, later[ip].Devices)
}

func TestDeviceTrafficIngest_WritesPresenceOnce(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, db.Get().AutoMigrate(&NodeDevicePresence{}))
	ip := "10.99.8.2"
	t.Cleanup(func() {
		db.Get().Where("node_ipv4 = ?", ip).Delete(&DeviceTrafficDaily{})
		db.Get().Where("node_ipv4 = ?", ip).Delete(&DeviceSessionDaily{})
		db.Get().Where("node_ipv4 = ?", ip).Delete(&NodeDevicePresence{})
		db.Get().Where("ipv4 = ?", ip).Delete(&DeviceTrafficCursor{})
	})
	req := DeviceTrafficRequest{BootID: "b", BatchSeq: 1, Ts: time.Now().Unix(),
		Devices: []DeviceTrafficItem{{UDID: "u1", SessionID: "s1", Rx: 10, Tx: 0}}}
	require.NoError(t, ingestDeviceTraffic(nil, ip, &req))
	var rows []NodeDevicePresence
	require.Eventually(t, func() bool {
		db.Get().Where("node_ipv4 = ?", ip).Find(&rows)
		return len(rows) == 1
	}, 3*time.Second, 20*time.Millisecond)

	require.NoError(t, ingestDeviceTraffic(nil, ip, &req)) // ack-lost resend
	time.Sleep(100 * time.Millisecond)                     // would-be duplicate write settles
	require.NoError(t, db.Get().Where("node_ipv4 = ?", ip).Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(10), rows[0].RxBytes, "a resend must not inflate presence")
}

// The billing ledger must not notice anything the observation path does.
func TestDeviceTrafficIngest_LedgerSurvivesPresenceFailure(t *testing.T) {
	skipIfNoConfig(t)
	cases := map[string]func(context.Context, string, []DeviceTrafficItem, map[string]uint, int64) error{
		"error": func(context.Context, string, []DeviceTrafficItem, map[string]uint, int64) error {
			return errors.New("boom")
		},
		"panic": func(context.Context, string, []DeviceTrafficItem, map[string]uint, int64) error {
			panic("boom")
		},
	}
	for name, writer := range cases {
		t.Run(name, func(t *testing.T) {
			ip := "10.99.8.3"
			t.Cleanup(func() {
				db.Get().Where("node_ipv4 = ?", ip).Delete(&DeviceTrafficDaily{})
				db.Get().Where("node_ipv4 = ?", ip).Delete(&DeviceSessionDaily{})
				db.Get().Where("ipv4 = ?", ip).Delete(&DeviceTrafficCursor{})
			})
			var calls atomic.Int32
			orig := nodePresenceWriter
			nodePresenceWriter = func(ctx context.Context, i string, it []DeviceTrafficItem, u map[string]uint, n int64) error {
				calls.Add(1)
				return writer(ctx, i, it, u, n)
			}
			t.Cleanup(func() { nodePresenceWriter = orig })

			req := DeviceTrafficRequest{BootID: "b", BatchSeq: 1, Ts: time.Now().Unix(),
				Devices: []DeviceTrafficItem{{UDID: "u1", SessionID: "s1", Rx: 10, Tx: 5}}}
			require.NoError(t, ingestDeviceTraffic(nil, ip, &req))
			require.Eventually(t, func() bool { return calls.Load() == 1 }, 3*time.Second, 10*time.Millisecond)

			var row DeviceTrafficDaily
			require.NoError(t, db.Get().Where("node_ipv4 = ?", ip).First(&row).Error)
			assert.Equal(t, int64(10), row.RxBytes)
			var cur DeviceTrafficCursor
			require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&cur).Error)
			assert.Equal(t, int64(1), cur.BatchSeq)
		})
	}
}

// With every slot taken the ingest path still returns at once.
func TestDispatchNodePresence_DropsInsteadOfBlocking(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	orig := nodePresenceWriter
	nodePresenceWriter = func(context.Context, string, []DeviceTrafficItem, map[string]uint, int64) error {
		calls.Add(1)
		<-release
		return nil
	}
	t.Cleanup(func() { nodePresenceWriter = orig })

	done := make(chan struct{})
	go func() {
		for i := 0; i < nodePresenceMaxInFlight*5; i++ {
			dispatchNodePresence("10.99.8.4", nil, nil, 0)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch blocked the caller")
	}
	require.Eventually(t, func() bool { return calls.Load() == nodePresenceMaxInFlight }, time.Second, 5*time.Millisecond)
	close(release)
	require.Eventually(t, func() bool { return len(nodePresenceSlots) == 0 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, int32(nodePresenceMaxInFlight), calls.Load())
}

func getDistribution[T any](t *testing.T, handler gin.HandlerFunc, url string) T {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, url, nil)
	handler(c)
	require.Equal(t, http.StatusOK, w.Code)
	var env struct {
		Code int `json:"code"`
		Data T   `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), w.Body.String())
	require.Equal(t, 0, env.Code, w.Body.String())
	return env.Data
}

// Through the real handlers against a real database.
func TestNodeDistribution_EndToEnd(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, db.Get().AutoMigrate(&NodeDevicePresence{}))
	busy, quiet := "10.99.8.5", "10.99.8.6"
	nodes := []SlaveNode{
		{Name: "zz-test.vultr.wm01", Ipv4: busy, Country: "ZZ", Class: "shared", SecretToken: "t1"},
		{Name: "zz-test.aws.wm01", Ipv4: quiet, Country: "ZZ", Class: "shared", SecretToken: "t2"},
	}
	require.NoError(t, db.Get().Create(&nodes).Error)
	t.Cleanup(func() {
		for _, n := range nodes {
			db.Get().Unscoped().Delete(&n)
		}
		db.Get().Where("node_ipv4 IN ?", []string{busy, quiet}).Delete(&NodeDevicePresence{})
	})
	now := time.Now().Unix()
	require.NoError(t, recordNodePresence(context.Background(), busy, []DeviceTrafficItem{
		{UDID: "a", SessionID: "1", Rx: 1}, {UDID: "b", SessionID: "2", Rx: 1},
	}, nil, now))

	out := getDistribution[NodeDistributionResponse](t, api_admin_node_distribution, "/app/nodes/distribution")
	byIP := map[string]NodeDistributionItem{}
	for _, it := range out.Items {
		byIP[it.Ipv4] = it
	}
	assert.Equal(t, 2, byIP[busy].ActiveDevices)
	assert.Equal(t, nodeStatusIdle, byIP[quiet].Status)
	assert.Nil(t, byIP[quiet].CPUPercent)
	var zz *NodeDistributionSummary
	for i := range out.Summaries {
		if out.Summaries[i].Scope == "ZZ" {
			zz = &out.Summaries[i]
		}
	}
	require.NotNil(t, zz)
	assert.Equal(t, nodeVerdictSkewed, zz.Verdict)

	trend := getDistribution[NodeDistributionTrendResponse](t, api_admin_node_distribution_trend, "/app/nodes/distribution/trend?hours=999&country=ZZ")
	assert.Equal(t, 168, trend.Hours)
	assert.NotNil(t, trend.Points)
}
