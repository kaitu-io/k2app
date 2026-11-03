package center

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecallFlowStage(t *testing.T) {
	f, s, ok := recallFlowStage("abandoned:3d:2026-11-03")
	assert.True(t, ok)
	assert.Equal(t, "abandoned", f)
	assert.Equal(t, "3d", s)
	f, s, ok = recallFlowStage("trial-recall:s1:2026-11-03T20")
	assert.True(t, ok)
	assert.Equal(t, "trial-recall", f)
	assert.Equal(t, "s1", s)
	_, _, ok = recallFlowStage("renewal:7d:2026-11-03")
	assert.False(t, ok, "续费提醒不是召回")
	_, _, ok = recallFlowStage("mcp:2026-05-01:winback")
	assert.False(t, ok)
}

func TestComputeRecallStats(t *testing.T) {
	strp := func(s string) *string { return &s }
	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	d := func(day int) time.Time { return time.Date(2026, 11, day, 10, 0, 0, 0, time.UTC) }
	sent := func(uid uint64, batch string, at time.Time) recallLogRow {
		return recallLogRow{UserID: uid, BatchID: batch, Status: "sent", Email: "x@e.com", CreatedAt: at}
	}
	hold := func(uid uint64, batch string, at time.Time) recallLogRow {
		return recallLogRow{UserID: uid, BatchID: batch, Status: "skipped", ErrorMsg: strp(recallHoldoutReason), Email: "x@e.com", CreatedAt: at}
	}
	rows := []recallLogRow{
		// 召回组：1 收了 1h 和 3d，3 天后付款（用码）；2 收了 1h，没付；3 收了 1h，第 20 天才付（窗口外）。
		sent(1, "abandoned:1h:2026-11-01-10", d(1)),
		sent(1, "abandoned:3d:2026-11-04", d(4)),
		sent(2, "abandoned:1h:2026-11-01-10", d(1)),
		sent(3, "abandoned:1h:2026-11-01-10", d(1)),
		// 对照组：4 付了（无码），5 没付。
		hold(4, "abandoned:1h:2026-11-01-10", d(1)),
		hold(5, "abandoned:1h:2026-11-01-10", d(1)),
		// 窗口未走完：不进分母。
		sent(6, "abandoned:1h:2026-11-25-10", d(25)),
		// 不参与：失败行、无邮箱的对照组、非对照的 skipped、续费提醒。
		{UserID: 7, BatchID: "abandoned:1h:x", Status: "failed", Email: "x@e.com", CreatedAt: d(1)},
		{UserID: 8, BatchID: "trial-recall:s1:x", Status: "skipped", ErrorMsg: strp(recallHoldoutReason), Email: "", CreatedAt: d(1)},
		{UserID: 9, BatchID: "abandoned:1h:x", Status: "skipped", ErrorMsg: strp("duplicate"), Email: "x@e.com", CreatedAt: d(1)},
		sent(10, "renewal:7d:2026-11-01", d(1)),
		// since 之前：忽略。
		sent(11, "abandoned:1h:2026-09-01-10", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
	}
	orders := []recallPaidOrder{
		{UserID: 1, PaidAt: d(7), PayAmount: 3900, CampaignCode: strp("NEW10"), CampaignReduceAmount: 1000},
		{UserID: 3, PaidAt: d(21), PayAmount: 4900},
		{UserID: 4, PaidAt: d(2), PayAmount: 4900, CampaignCode: strp("")},
		{UserID: 10, PaidAt: d(2), PayAmount: 4900},
	}

	st := computeRecallStats(rows, orders, since, now, 14)
	require.Len(t, st.Flows, 1, "只有 abandoned 链路有可统计的行（试用那行无邮箱）")
	f := st.Flows[0]
	assert.Equal(t, "abandoned", f.Flow)

	assert.Equal(t, 3, f.Treated.Users)
	assert.Equal(t, 1, f.Treated.Paid)
	assert.Equal(t, 1, f.Treated.Pending)
	assert.Equal(t, uint64(3900), f.Treated.RevenueCents)
	assert.Equal(t, 1, f.Treated.PaidWithCode)
	assert.Equal(t, uint64(1000), f.Treated.DiscountCents)

	assert.Equal(t, 2, f.Holdout.Users)
	assert.Equal(t, 1, f.Holdout.Paid)
	assert.Equal(t, 0, f.Holdout.PaidWithCode, "空字符串码不算用码")
	require.NotNil(t, f.LiftPoints)
	assert.InDelta(t, (1.0/3-0.5)*100, *f.LiftPoints, 1e-9)

	require.Len(t, f.Stages, 2)
	assert.Equal(t, "1h", f.Stages[0].Stage)
	assert.Equal(t, "3d", f.Stages[1].Stage)
	assert.Equal(t, 1, f.Stages[1].Treated.Users)
	assert.Equal(t, 1, f.Stages[1].Treated.Paid, "3d 起点后 3 天付款")
}

func TestComputeRecallStats_NoHoldoutMeansNoLift(t *testing.T) {
	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	rows := []recallLogRow{{UserID: 1, BatchID: "winback:7d:2026-10-02", Status: "sent", Email: "x@e.com", CreatedAt: since.Add(24 * time.Hour)}}
	st := computeRecallStats(rows, nil, since, now, 14)
	require.Len(t, st.Flows, 1)
	assert.Nil(t, st.Flows[0].LiftPoints, "没有对照组就不给增量，不能拿 0 当对照")
}

func TestRecallStageOrder(t *testing.T) {
	assert.Less(t, recallStageOrder("1h"), recallStageOrder("1d"))
	assert.Less(t, recallStageOrder("7d"), recallStageOrder("30d"))
	assert.Less(t, recallStageOrder("s1"), recallStageOrder("s2"))
}
