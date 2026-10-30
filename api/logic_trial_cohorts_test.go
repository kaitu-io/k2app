package center

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func TestTrialChainState(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	future := now.Add(48 * time.Hour).Unix()
	justEnded := now.Add(-2 * time.Hour).Unix() // 结算窗口内
	longEnded := now.Add(-30 * time.Hour).Unix()
	cases := []struct {
		name string
		ch   trialChain
		want string
	}{
		{"renew on", trialChain{TrialEndsAt: future, IsTrial: true, AutoRenew: true, Status: "active"}, trialStateRenewOn},
		{"renew off", trialChain{TrialEndsAt: future, IsTrial: true, Status: "active"}, trialStateRenewOff},
		{"just ended, no webhook yet", trialChain{TrialEndsAt: justEnded, IsTrial: true, AutoRenew: true, Status: "active"}, trialStateAwaitingCharge},
		{"ended long ago, webhook lost", trialChain{TrialEndsAt: longEnded, IsTrial: true, AutoRenew: true, Status: "active"}, trialStateEndedUnpaid},
		{"expired via webhook inside window", trialChain{TrialEndsAt: justEnded, IsTrial: true, Status: "expired"}, trialStateEndedUnpaid},
		{"billing retry", trialChain{TrialEndsAt: justEnded, IsTrial: true, AutoRenew: true, Status: "billing_retry"}, trialStateBillingRetry},
		{"converted inside window", trialChain{TrialEndsAt: justEnded, AutoRenew: true, Status: "active", PaidThrough: future}, trialStateConverted},
		{"paid then refunded", trialChain{TrialEndsAt: longEnded, Status: "revoked", PaidThrough: longEnded}, trialStateRevoked},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, trialChainState(c.ch, now))
		})
	}
}

func TestComputeTrialCohorts_StatesRateAndPayments(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	d27 := time.Date(2026, 10, 27, 7, 0, 0, 0, time.UTC)   // 11-03 07:00 结束，已结算
	d28 := time.Date(2026, 10, 28, 6, 0, 0, 0, time.UTC)   // 11-04 06:00 结束，结算窗口内
	d29 := time.Date(2026, 10, 29, 23, 30, 0, 0, time.UTC) // 11-05 23:30 结束，试用中
	end := func(d time.Time) int64 { return d.Add(7 * 24 * time.Hour).Unix() }

	chains := []trialChain{
		{UserID: 1, StartedAt: d27, TrialEndsAt: end(d27), AutoRenew: true, Status: "active", PaidThrough: end(d27) + 365*86400}, // 转付费
		{UserID: 2, StartedAt: d27, TrialEndsAt: end(d27), IsTrial: true, Status: "expired"},                                     // 结束未付
		{UserID: 3, StartedAt: d27, TrialEndsAt: end(d27), IsTrial: true, AutoRenew: true, Status: "billing_retry"},              // 扣费重试
		{UserID: 4, StartedAt: d27, TrialEndsAt: end(d27), Status: "revoked", PaidThrough: end(d27)},                             // 付了又退
		{UserID: 5, StartedAt: d28, TrialEndsAt: end(d28), IsTrial: true, AutoRenew: true, Status: "active"},                     // 待扣费
		{UserID: 6, StartedAt: d28, TrialEndsAt: end(d28), AutoRenew: true, Status: "active", PaidThrough: end(d28) + 365*86400}, // 窗口内已转化
		{UserID: 7, StartedAt: d29, TrialEndsAt: end(d29), IsTrial: true, AutoRenew: true, Status: "active"},
		{UserID: 8, StartedAt: d29, TrialEndsAt: end(d29), IsTrial: true, Status: "active"},
		{UserID: 1, StartedAt: d29, TrialEndsAt: end(d29), IsTrial: true, Status: "active"}, // 用户 1 换 Apple ID 又开一条
	}
	pays := map[uint64]trialUserPayment{
		1: {Paid: 4900, PlanTitle: "年付"},
		4: {Paid: 4900, Refunded: 4900},
		6: {Paid: 4900},
	}
	res := computeTrialCohorts(chains, pays, now)

	require.Len(t, res.Rows, 3)
	r27, r28, r29 := res.Rows[0], res.Rows[1], res.Rows[2]

	assert.Equal(t, "2026-10-27", r27.Cohort)
	assert.Equal(t, 4, r27.Started)
	assert.Equal(t, 1, r27.Converted)
	assert.Equal(t, 1, r27.EndedUnpaid)
	assert.Equal(t, 1, r27.BillingRetry)
	assert.Equal(t, 1, r27.Revoked)
	assert.Equal(t, 4, r27.Settled)
	assert.Equal(t, 1, r27.SettledConverted)
	require.NotNil(t, r27.ConvRate)
	assert.InDelta(t, 0.25, *r27.ConvRate, 1e-9)
	assert.Equal(t, uint64(9800), r27.PaidAmount)
	assert.Equal(t, uint64(4900), r27.RefundedAmount)

	assert.Equal(t, 1, r28.AwaitingCharge)
	assert.Equal(t, 1, r28.Converted, "窗口内已转化照常显示")
	assert.Equal(t, 0, r28.Settled, "窗口内的链不进分母")
	assert.Nil(t, r28.ConvRate)

	assert.Equal(t, "2026-10-29", r29.Cohort, "23:30Z 落在 UTC 当天")
	assert.Equal(t, 1, r29.InTrialRenew)
	assert.Equal(t, 2, r29.InTrialOff)
	assert.Zero(t, r29.PaidAmount, "用户 1 的付款已计在 10-27 那条链上，不重复计")

	assert.Equal(t, 9, res.Totals.Started)
	assert.Equal(t, 4, res.Totals.Settled)
	assert.Equal(t, uint64(14700), res.Totals.PaidAmount)
	require.NotNil(t, res.Totals.ConvRate)
	assert.InDelta(t, 0.25, *res.Totals.ConvRate, 1e-9)

	for _, r := range append(res.Rows, res.Totals) {
		sum := r.InTrialRenew + r.InTrialOff + r.AwaitingCharge + r.BillingRetry + r.Converted + r.EndedUnpaid + r.Revoked
		assert.Equal(t, r.Started, sum, "states are exclusive and complete: %s", r.Cohort)
	}

	users := map[uint64]User{5: {UUID: "u-5"}, 6: {UUID: "u-6"}}
	got := trialChainDetails(chains, pays, users, "2026-10-28", now)
	require.Len(t, got, 2)
	assert.Equal(t, "u-5", got[0].UserUUID)
	assert.Equal(t, trialStateAwaitingCharge, got[0].State)
	assert.Equal(t, trialStateConverted, got[1].State)
	assert.Equal(t, uint64(4900), got[1].PaidAmount)
	assert.Empty(t, trialChainDetails(chains, pays, users, "2026-10-01", now))
}

func TestComputeTrialCohorts_EmptyIsNotNull(t *testing.T) {
	res := computeTrialCohorts(nil, nil, time.Now())
	assert.NotNil(t, res.Rows)
	assert.Empty(t, res.Rows)
	assert.Nil(t, res.Totals.ConvRate)
}

// 真库：读库查询（列名、正式环境过滤、品牌范围、订单金额归属）与真实入账路径写出的数据对得上。
func TestTrialCohorts_LoadersAgainstRealCredits(t *testing.T) {
	trialDB(t)
	conv := setupIAPOrderFixture(t, 30, 10)
	conv.noInvite(t)
	lapsed := setupIAPOrderFixture(t, 30, 10)
	t0 := time.Now().Unix()

	// conv：试用 → 转付费（建 Apple 订单）；lapsed：试用中，关闭续费。
	require.NoError(t, creditInfo(t, conv.buyer.ID, conv.trialTxn("TC-1", t0-8*aDay, t0-aDay)))
	require.NoError(t, creditInfo(t, conv.buyer.ID, conv.paidTxn("TC-2", t0-aDay, t0+364*aDay)))
	require.NoError(t, creditInfo(t, lapsed.buyer.ID, lapsed.trialTxn("TC-3", t0, t0+7*aDay)))
	require.NoError(t, db.Get().Model(&Subscription{}).Where("provider_subscription_id = ?", lapsed.origTxn).
		Update("auto_renew", false).Error)

	mine := map[uint64]bool{conv.buyer.ID: true, lapsed.buyer.ID: true}
	all, err := loadTrialChains(context.Background(), BrandKaitu, true, time.Now().Add(-48*time.Hour))
	require.NoError(t, err)
	var chains []trialChain
	for _, ch := range all {
		if mine[ch.UserID] {
			chains = append(chains, ch)
		}
	}
	require.Len(t, chains, 2)

	pays, err := loadTrialPayments(context.Background(), chains)
	require.NoError(t, err)
	assert.Equal(t, uint64(4900), pays[conv.buyer.ID].Paid, "转付费那笔 Apple 订单计入")
	assert.Zero(t, pays[lapsed.buyer.ID].Paid, "试用不建单")

	now := time.Now()
	states := map[uint64]string{}
	for _, ch := range chains {
		states[ch.UserID] = trialChainState(ch, now)
	}
	assert.Equal(t, trialStateConverted, states[conv.buyer.ID])
	assert.Equal(t, trialStateRenewOff, states[lapsed.buyer.ID])

	others, err := loadTrialChains(context.Background(), BrandOverleap, true, time.Now().Add(-48*time.Hour))
	require.NoError(t, err)
	for _, ch := range others {
		assert.False(t, mine[ch.UserID], "开途用户不出现在 Overleap 的统计里")
	}
}
