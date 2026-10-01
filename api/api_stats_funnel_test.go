package center

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func statsFunnelRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api")
	g.Use(BrandResolver())
	g.POST("/stats/events", api_stats_ingest)
	return r
}

// sfMarker: 唯一 plan 标记 + 清理该标记的事件。
func sfMarker(t *testing.T) string {
	t.Helper()
	m := fmt.Sprintf("tsf%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Get().Where("plan = ?", m).Delete(&FunnelEvent{}) })
	return m
}

func sfItem(m, hash, event string) map[string]any {
	return map[string]any{
		"eid": uuid.NewString(), "device_hash": hash, "os": "macos", "app_version": "0.4.0",
		"event": event, "plan": m, "created_at": time.Now().UTC().Format(time.RFC3339),
	}
}

func sfPost(items ...map[string]any) *TestRequest {
	return NewTestRequest("POST", "/api/stats/events").WithBody(map[string]any{"funnel": items})
}

func sfRows(t *testing.T, m string) []FunnelEvent {
	t.Helper()
	funnelFlushForTest()
	var rows []FunnelEvent
	require.NoError(t, db.Get().Where("plan = ?", m).Find(&rows).Error)
	return rows
}

func sfCode(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	resp, err := ParseResponse(w)
	require.NoError(t, err)
	return resp.Code
}

func TestStatsIngest_FunnelRecorded(t *testing.T) {
	skipIfNoConfig(t)
	m := sfMarker(t)
	it := sfItem(m, "sfdev-"+m, "paywall_view")
	it["source"] = "account"
	w := sfPost(it).Execute(statsFunnelRouter())
	assert.EqualValues(t, ErrorNone, sfCode(t, w))
	rows := sfRows(t, m)
	require.Len(t, rows, 1)
	r := rows[0]
	assert.Equal(t, "app", r.Surface)
	assert.Equal(t, "account", r.Source)
	assert.Equal(t, "desktop", r.Device)
	assert.Equal(t, "macos", r.OS)
	assert.Equal(t, "paywall_view", r.Event)
	assert.Equal(t, "sfdev-"+m, r.AnonID)
	assert.Equal(t, "kaitu", r.Brand)
	assert.Equal(t, "0.4.0", r.AppVersion)
	assert.Equal(t, uint64(0), r.UserID)
	require.NotNil(t, r.Eid)
	assert.Equal(t, it["eid"], *r.Eid)
}

func TestStatsIngest_FunnelMobileDeviceAndOSNormalize(t *testing.T) {
	skipIfNoConfig(t)
	m := sfMarker(t)
	a := sfItem(m, "sfdev-"+m, "paywall_view")
	a["os"] = "ios"
	b := sfItem(m, "sfdev-"+m, "login_view")
	b["os"] = "web"
	sfPost(a, b).Execute(statsFunnelRouter())
	rows := sfRows(t, m)
	require.Len(t, rows, 2)
	for _, r := range rows {
		if r.Event == "paywall_view" {
			assert.Equal(t, "mobile", r.Device)
			assert.Equal(t, "ios", r.OS)
		} else {
			assert.Equal(t, "desktop", r.Device)
			assert.Equal(t, "other", r.OS)
		}
	}
}

func TestStatsIngest_FunnelSameEidTwice(t *testing.T) {
	skipIfNoConfig(t)
	m := sfMarker(t)
	it := sfItem(m, "sfdev-"+m, "paywall_view")
	r := statsFunnelRouter()
	assert.EqualValues(t, ErrorNone, sfCode(t, sfPost(it).Execute(r)))
	assert.EqualValues(t, ErrorNone, sfCode(t, sfPost(it).Execute(r)))
	assert.Len(t, sfRows(t, m), 1)
}

func TestStatsIngest_FunnelRejectsFactAndWebOnly(t *testing.T) {
	skipIfNoConfig(t)
	m := sfMarker(t)
	bad := sfItem(m, "sfdev-"+m, "x")
	bad["eid"] = "not-a-uuid"
	w := sfPost(
		sfItem(m, "sfdev-"+m, "purchase"),
		sfItem(m, "sfdev-"+m, "page_view"),
		sfItem(m, "sfdev-"+m, "nope"),
		bad,
		sfItem(m, "sfdev-"+m, "login_view"),
	).Execute(statsFunnelRouter())
	assert.EqualValues(t, ErrorNone, sfCode(t, w))
	rows := sfRows(t, m)
	require.Len(t, rows, 1)
	assert.Equal(t, "login_view", rows[0].Event)
}

func TestStatsIngest_FunnelIgnoresClientUser(t *testing.T) {
	skipIfNoConfig(t)
	m := sfMarker(t)
	it := sfItem(m, "sfdev-"+m, "paywall_view")
	it["user_id"] = 999
	sfPost(it).Execute(statsFunnelRouter())
	rows := sfRows(t, m)
	require.Len(t, rows, 1)
	assert.Equal(t, uint64(0), rows[0].UserID)
}

func TestStatsIngest_FunnelBearerLinksDevice(t *testing.T) {
	skipIfNoConfig(t)
	m := sfMarker(t)
	user := CreateTestUser(t)
	udid := "sf-udid-" + m
	CreateTestDevice(t, user.ID, udid)
	hash := "sfdev-" + m
	t.Cleanup(func() { db.Get().Where("user_id = ?", user.ID).Delete(&FunnelIdentity{}) })
	tok := GenerateTestToken(user.ID, udid, time.Hour)
	w := sfPost(sfItem(m, hash, "paywall_view"), sfItem(m, hash, "login_view")).
		WithBearerToken(tok).Execute(statsFunnelRouter())
	assert.EqualValues(t, ErrorNone, sfCode(t, w))
	rows := sfRows(t, m)
	require.Len(t, rows, 2)
	for _, r := range rows {
		assert.Equal(t, user.ID, r.UserID)
	}
	var n int64
	require.NoError(t, db.Get().Model(&FunnelIdentity{}).
		Where("kind = 'did' AND anon_id = ? AND user_id = ?", hash, user.ID).Count(&n).Error)
	assert.Equal(t, int64(1), n)
}

func TestStatsIngest_FunnelOnlyAndTruncation(t *testing.T) {
	skipIfNoConfig(t)
	m := sfMarker(t)
	it := sfItem(m, "sfdev-"+m, "plan_select")
	it["source"] = "ssssssssssssssssssssssssssssssssssssssssssssssss" // 48 > 32
	it["channel"] = "cccccccccccccccccccccc"                          // 22 > 16
	it["app_version"] = "9.9.9-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	w := sfPost(it).Execute(statsFunnelRouter())
	assert.EqualValues(t, ErrorNone, sfCode(t, w))
	rows := sfRows(t, m)
	require.Len(t, rows, 1)
	assert.Len(t, rows[0].Source, 32)
	assert.Len(t, rows[0].Channel, 16)
	assert.Len(t, rows[0].AppVersion, 32)
}

func TestStatsIngest_FunnelDisabledStillStoresLegacy(t *testing.T) {
	skipIfNoConfig(t)
	m := sfMarker(t)
	hash := "sfleg-" + m
	t.Cleanup(func() { db.Get().Where("device_hash = ?", hash).Delete(&StatAppOpen{}) })
	viper.Set("funnel.enabled", false)
	t.Cleanup(func() { viper.Set("funnel.enabled", true) })
	body := map[string]any{
		"app_opens": []map[string]any{{"device_hash": hash, "os": "macos", "app_version": "0.4.0", "created_at": time.Now().UTC().Format(time.RFC3339)}},
		"funnel":    []map[string]any{sfItem(m, hash, "paywall_view")},
	}
	w := NewTestRequest("POST", "/api/stats/events").WithBody(body).Execute(statsFunnelRouter())
	assert.EqualValues(t, ErrorNone, sfCode(t, w))
	assert.Len(t, sfRows(t, m), 0)
	var n int64
	require.NoError(t, db.Get().Model(&StatAppOpen{}).Where("device_hash = ?", hash).Count(&n).Error)
	assert.Equal(t, int64(1), n)
}

func TestStatsIngest_LegacyBodyUnchanged(t *testing.T) {
	skipIfNoConfig(t)
	m := sfMarker(t)
	hash := "sfleg-" + m
	t.Cleanup(func() { db.Get().Where("device_hash = ?", hash).Delete(&StatAppOpen{}) })
	var before int64
	require.NoError(t, db.Get().Model(&FunnelEvent{}).Count(&before).Error)
	body := map[string]any{
		"app_opens": []map[string]any{{"device_hash": hash, "os": "macos", "app_version": "0.4.0", "created_at": time.Now().UTC().Format(time.RFC3339)}},
	}
	w := NewTestRequest("POST", "/api/stats/events").WithBody(body).Execute(statsFunnelRouter())
	assert.EqualValues(t, ErrorNone, sfCode(t, w))
	funnelFlushForTest()
	var n, after int64
	require.NoError(t, db.Get().Model(&StatAppOpen{}).Where("device_hash = ?", hash).Count(&n).Error)
	assert.Equal(t, int64(1), n)
	require.NoError(t, db.Get().Model(&FunnelEvent{}).Count(&after).Error)
	assert.GreaterOrEqual(t, after, before) // shared DB: other writers may add; none from this request
	assert.Len(t, sfRows(t, m), 0)
}

func TestStatsIngest_FunnelCountsTowardLimit(t *testing.T) {
	skipIfNoConfig(t)
	m := sfMarker(t)
	items := make([]map[string]any, 101)
	for i := range items {
		items[i] = sfItem(m, "sfdev-"+m, "paywall_view")
	}
	w := sfPost(items...).Execute(statsFunnelRouter())
	assert.EqualValues(t, ErrorInvalidArgument, sfCode(t, w))
	assert.Len(t, sfRows(t, m), 0)
}
