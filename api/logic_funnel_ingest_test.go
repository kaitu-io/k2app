package center

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func funnelTestMarker(t *testing.T) string {
	t.Helper()
	m := fmt.Sprintf("t-funnel-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		db.Get().Where("anon_id = ?", m).Delete(&FunnelEvent{})
		db.Get().Where("anon_id = ?", m).Delete(&FunnelIdentity{})
	})
	return m
}

func funnelCount(t *testing.T, anon string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Get().Model(&FunnelEvent{}).Where("anon_id = ?", anon).Count(&n).Error)
	return n
}

func TestFunnelEnqueue_WritesRow(t *testing.T) {
	skipIfNoConfig(t)
	m := funnelTestMarker(t)
	eid := m + "-e"
	occ := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	require.True(t, funnelEnqueue(FunnelEvent{
		OccurredAt: occ, Eid: &eid, Brand: "kaitu", Surface: FunnelSurfaceApp, Event: "paywall_view",
		AnonID: m, UserID: 7, Plan: "p1", Source: "s", Channel: "stripe",
	}))
	funnelFlushForTest()
	var rows []FunnelEvent
	require.NoError(t, db.Get().Where("anon_id = ?", m).Find(&rows).Error)
	require.Len(t, rows, 1)
	r := rows[0]
	assert.Equal(t, "kaitu", r.Brand)
	assert.Equal(t, FunnelSurfaceApp, r.Surface)
	assert.Equal(t, "paywall_view", r.Event)
	assert.Equal(t, uint64(7), r.UserID)
	assert.Equal(t, "p1", r.Plan)
	assert.Equal(t, "s", r.Source)
	assert.Equal(t, "stripe", r.Channel)
	require.NotNil(t, r.Eid)
	assert.Equal(t, eid, *r.Eid)
	assert.WithinDuration(t, occ, r.OccurredAt, 2*time.Second)
	assert.False(t, r.ReceivedAt.IsZero())
}

func TestFunnelEnqueue_DuplicateEidIgnored(t *testing.T) {
	skipIfNoConfig(t)
	m := funnelTestMarker(t)
	eid := m + "-dup"
	ev := FunnelEvent{Eid: &eid, Brand: "kaitu", Surface: FunnelSurfaceApp, Event: "paywall_view", AnonID: m}
	require.True(t, funnelEnqueue(ev))
	require.True(t, funnelEnqueue(ev))
	funnelFlushForTest()
	assert.Equal(t, int64(1), funnelCount(t, m))
}

func TestFunnelEnqueue_NilEidNotDeduped(t *testing.T) {
	skipIfNoConfig(t)
	m := funnelTestMarker(t)
	ev := FunnelEvent{Brand: "kaitu", Surface: FunnelSurfaceWeb, Event: "pricing_view", AnonID: m}
	require.True(t, funnelEnqueue(ev))
	require.True(t, funnelEnqueue(ev))
	funnelFlushForTest()
	assert.Equal(t, int64(2), funnelCount(t, m))
}

func TestFunnelEnqueue_DisabledDropsSilently(t *testing.T) {
	skipIfNoConfig(t)
	m := funnelTestMarker(t)
	// 还原原始状态（含"未设置"）：viper 无 Unset，用 AllSettings 判断后重置
	wasSet := viper.IsSet("funnel.enabled")
	prev := viper.Get("funnel.enabled")
	viper.Set("funnel.enabled", false)
	t.Cleanup(func() {
		if wasSet {
			viper.Set("funnel.enabled", prev)
		} else {
			viper.Set("funnel.enabled", nil)
		}
	})
	assert.False(t, funnelEnqueue(FunnelEvent{Brand: "kaitu", Surface: FunnelSurfaceWeb, Event: "pricing_view", AnonID: m}))
	funnelFlushForTest()
	assert.Equal(t, int64(0), funnelCount(t, m))
}

func TestLinkFunnelIdentity_Idempotent(t *testing.T) {
	skipIfNoConfig(t)
	m := funnelTestMarker(t)
	ctx := context.Background()
	for range 3 {
		linkFunnelIdentity(ctx, "sid", m, 4242, Brand("kaitu"))
	}
	linkFunnelIdentity(ctx, "sid", m, 0, Brand("kaitu"))
	funnelFlushForTest()
	var n int64
	require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("anon_id = ?", m).Count(&n).Error)
	assert.Equal(t, int64(1), n)

	linkFunnelIdentity(ctx, "sid", "", 4242, Brand("kaitu"))
	funnelFlushForTest()
	require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("user_id = ? AND anon_id = ''", 4242).Count(&n).Error)
	assert.Equal(t, int64(0), n)
}

func TestFunnelEnqueue_FullQueueDoesNotBlock(t *testing.T) {
	orig := funnelQ
	q := newFunnelQueue(1)
	q.startOnce.Do(func() {}) // 不启动消费者
	funnelQ = q
	t.Cleanup(func() { funnelQ = orig })

	ev := FunnelEvent{Brand: "kaitu", Surface: FunnelSurfaceWeb, Event: "pricing_view"}
	assert.True(t, funnelEnqueue(ev))
	done := make(chan bool, 1)
	go func() { done <- funnelEnqueue(ev) }()
	select {
	case ok := <-done:
		assert.False(t, ok)
	case <-time.After(50 * time.Millisecond):
		t.Fatal("funnelEnqueue blocked on full queue")
	}
}

func TestFunnelEnqueue_BadRowDoesNotPoisonBatch(t *testing.T) {
	skipIfNoConfig(t)
	m := funnelTestMarker(t)
	mk := func(path string) FunnelEvent {
		return FunnelEvent{Brand: "kaitu", Surface: FunnelSurfaceWeb, Event: "pricing_view", AnonID: m, Path: path}
	}
	long := make([]byte, 300) // Path 列 varchar(255)
	for i := range long {
		long[i] = 'x'
	}
	require.True(t, funnelEnqueue(mk("/ok1")))
	require.True(t, funnelEnqueue(mk(string(long))))
	require.True(t, funnelEnqueue(mk("/ok2")))
	funnelFlushForTest()
	var n int64
	require.NoError(t, db.Get().Model(&FunnelEvent{}).Where("anon_id = ? AND path IN ?", m, []string{"/ok1", "/ok2"}).Count(&n).Error)
	assert.Equal(t, int64(2), n)
}

func TestLinkFunnelIdentity_FullQueueDoesNotBlock(t *testing.T) {
	orig := funnelQ
	q := newFunnelQueue(1)
	q.startOnce.Do(func() {})
	funnelQ = q
	t.Cleanup(func() { funnelQ = orig })

	done := make(chan struct{})
	go func() {
		for i := range 5 {
			linkFunnelIdentity(context.Background(), "sid", fmt.Sprintf("t-blk-%d-%d", time.Now().UnixNano(), i), 99, Brand("kaitu"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("linkFunnelIdentity blocked on full queue")
	}
}
