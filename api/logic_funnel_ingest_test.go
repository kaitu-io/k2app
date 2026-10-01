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
	viper.Set("funnel.enabled", false)
	t.Cleanup(func() { viper.Set("funnel.enabled", true) })
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
	var n int64
	require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("anon_id = ?", m).Count(&n).Error)
	assert.Equal(t, int64(1), n)

	linkFunnelIdentity(ctx, "sid", "", 4242, Brand("kaitu"))
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
