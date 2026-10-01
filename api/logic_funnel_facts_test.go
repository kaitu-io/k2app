package center

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// factsWindow 给每个测试一个独占的历史时间窗 [from, to)（共享 dev 库里没有别的行落在这里的保证，
// 所以断言仍然按自建用户 ID 过滤）。base 是窗口内的第一个整秒。
func factsWindow(t *testing.T) (from, to time.Time) {
	t.Helper()
	off := time.Duration(time.Now().UnixNano() % int64(5*365*24*time.Hour))
	from = time.Date(2004, 1, 1, 0, 0, 0, 0, time.UTC).Add(off).Truncate(time.Second)
	return from, from.Add(24 * time.Hour)
}

func factsUser(t *testing.T, brand Brand, createdAt time.Time) *User {
	t.Helper()
	u := &User{UUID: generateId("funnel-facts-user"), Brand: string(brand), CreatedAt: createdAt, RegistrationCountry: "jp"}
	require.NoError(t, db.Get().Create(u).Error)
	t.Cleanup(func() { factsCleanup(t, db.Get().Unscoped().Delete(u).Error) })
	return u
}

func factsCleanup(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("funnel facts test cleanup failed: %v", err)
	}
}

type factsOrderOpt func(*Order)

func factsOrder(t *testing.T, userID uint64, channel string, opts ...factsOrderOpt) *Order {
	t.Helper()
	o := &Order{
		UUID: uuid.NewString(), Title: "funnel-facts", UserID: userID, Channel: channel,
		Meta: `{"plan":{"pid":"facts-plan"}}`,
	}
	for _, f := range opts {
		f(o)
	}
	require.NoError(t, db.Get().Create(o).Error)
	t.Cleanup(func() { factsCleanup(t, db.Get().Unscoped().Delete(o).Error) })
	return o
}

func factsPaid(at time.Time) factsOrderOpt {
	return func(o *Order) { o.IsPaid = BoolPtr(true); o.PaidAt = &at }
}

func factsRefunded(at time.Time) factsOrderOpt {
	return func(o *Order) { o.IsRefunded = BoolPtr(true); o.RefundedAt = &at }
}

func factsCredit(t *testing.T, userID uint64, provider, kind, origTxn string, at time.Time) *SubscriptionCredit {
	t.Helper()
	c := &SubscriptionCredit{
		CreatedAt: at, UserID: userID, Provider: provider, Kind: kind,
		TransactionID: generateId("ff-txn"), OriginalTransactionID: origTxn, CreditedSeconds: 3600,
	}
	require.NoError(t, db.Get().Create(c).Error)
	t.Cleanup(func() { factsCleanup(t, db.Get().Unscoped().Delete(c).Error) })
	return c
}

// factsOf 只留下属于给定用户的记录（dev 库是共享的）。
func factsOf(recs []funnelRecord, userIDs ...uint64) []funnelRecord {
	var out []funnelRecord
	for _, r := range recs {
		for _, id := range userIDs {
			if r.UserID == id {
				out = append(out, r)
			}
		}
	}
	return out
}

var factsAll = []string{"signup", "purchase", "renewal", "refund"}
var factsPay = []string{"purchase", "renewal"}

func TestFacts_FirstIsPurchaseRestRenewal(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandKaitu, from.Add(-time.Hour))
	t1, t2 := from.Add(time.Hour), from.Add(2*time.Hour)
	factsOrder(t, u.ID, OrderChannelNextpay, factsPaid(t2))
	factsOrder(t, u.ID, OrderChannelNextpay, factsPaid(t1))

	recs, err := loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, factsPay)
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 2)
	assert.Equal(t, "purchase", got[0].Event)
	assert.True(t, got[0].At.Equal(t1), "purchase at T1, got %v", got[0].At)
	assert.Equal(t, "renewal", got[1].Event)
	assert.True(t, got[1].At.Equal(t2), "renewal at T2, got %v", got[1].At)
	assert.Equal(t, "facts-plan", got[0].Plan)
	assert.Equal(t, OrderChannelNextpay, got[0].Channel)
	assert.Empty(t, got[0].Surface)
	assert.Empty(t, got[0].AnonKind)
	assert.Empty(t, got[0].AnonID)
}

func TestFacts_FirstOutsideRangeMeansRenewal(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandKaitu, from.Add(-48*time.Hour))
	factsOrder(t, u.ID, OrderChannelNextpay, factsPaid(from.Add(-time.Hour)))
	t2 := from.Add(time.Hour)
	factsOrder(t, u.ID, OrderChannelNextpay, factsPaid(t2))

	recs, err := loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, factsPay)
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 1)
	assert.Equal(t, "renewal", got[0].Event)
	assert.True(t, got[0].At.Equal(t2))
}

// 首笔在入账行（区间外）、区间内是订单：首笔判定必须跨两个来源。
func TestFacts_FirstOutsideRangeAcrossSources(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandOverleap, from.Add(-48*time.Hour))
	factsCredit(t, u.ID, SubscriptionProviderStripe, "purchase", generateId("ff-sub"), from.Add(-time.Hour))
	factsOrder(t, u.ID, OrderChannelNextpay, factsPaid(from.Add(time.Hour)))

	recs, err := loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, factsPay)
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 1)
	assert.Equal(t, "renewal", got[0].Event)
}

func TestFacts_AppleCountedOnce(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandKaitu, from.Add(-time.Hour))
	at := from.Add(time.Hour)
	factsOrder(t, u.ID, OrderChannelAppleIAP, factsPaid(at))
	factsCredit(t, u.ID, SubscriptionProviderApple, "purchase", generateId("ff-sub"), at)

	recs, err := loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, factsPay)
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 1)
	assert.Equal(t, "purchase", got[0].Event)
	assert.Equal(t, "apple", got[0].Channel)
}

func TestFacts_StripeWithoutOrder(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandOverleap, from.Add(-time.Hour))
	at := from.Add(time.Hour)
	factsCredit(t, u.ID, SubscriptionProviderStripe, "purchase", generateId("ff-sub"), at)

	recs, err := loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, factsPay)
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 1)
	assert.Equal(t, "purchase", got[0].Event)
	assert.Equal(t, "stripe", got[0].Channel)
	assert.True(t, got[0].At.Equal(at))
	assert.Empty(t, got[0].Plan, "no subscription row → plan stays empty")
}

// 入账行的 Plan 经该用户同 provider 的 Subscription.ProductID 反查。
func TestFacts_CreditPlanFromSubscription(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandOverleap, from.Add(-time.Hour))
	tag := fmt.Sprintf("%d", time.Now().UnixNano())
	plan := &Plan{
		PID: "ff-" + tag, Label: "funnel-facts", Price: 100, OriginPrice: 100, Month: 1,
		Product: "app", IsActive: BoolPtr(true), Brand: string(BrandOverleap), StripePriceID: "price_ff_" + tag,
	}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { factsCleanup(t, db.Get().Unscoped().Delete(plan).Error) })
	subID := "sub_ff_" + tag
	sub := &Subscription{UserID: u.ID, Provider: SubscriptionProviderStripe, ProviderSubscriptionID: subID, ProductID: plan.StripePriceID}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { factsCleanup(t, db.Get().Unscoped().Delete(sub).Error) })
	factsCredit(t, u.ID, SubscriptionProviderStripe, "purchase", subID, from.Add(time.Hour))

	recs, err := loadFunnelFacts(context.Background(), BrandOverleap, true, from, to, factsPay)
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 1)
	assert.Equal(t, plan.PID, got[0].Plan)
}

func TestFacts_GraceCreditNotPayment(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandKaitu, from.Add(-time.Hour))
	factsCredit(t, u.ID, SubscriptionProviderApple, "grace", generateId("ff-sub"), from.Add(time.Hour))

	recs, err := loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, factsPay)
	require.NoError(t, err)
	assert.Empty(t, factsOf(recs, u.ID))
}

func TestFacts_UnpaidOrderIgnored(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandKaitu, from.Add(-time.Hour))
	at := from.Add(time.Hour)
	// 未付：is_paid=false 但带 paid_at；以及 is_paid 默认、无 paid_at
	factsOrder(t, u.ID, OrderChannelNextpay, func(o *Order) { o.IsPaid = BoolPtr(false); o.PaidAt = &at })
	factsOrder(t, u.ID, OrderChannelNextpay)

	recs, err := loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, factsPay)
	require.NoError(t, err)
	assert.Empty(t, factsOf(recs, u.ID))
}

func TestFacts_LegacyEmptyChannelIsWordgate(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandKaitu, from.Add(-time.Hour))
	factsOrder(t, u.ID, "", factsPaid(from.Add(time.Hour)))

	recs, err := loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, factsPay)
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 1)
	assert.Equal(t, "purchase", got[0].Event)
	assert.Equal(t, OrderChannelWordgate, got[0].Channel)
}

func TestFacts_BrandFilter(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	k := factsUser(t, BrandKaitu, from.Add(time.Minute))
	o := factsUser(t, BrandOverleap, from.Add(time.Minute))
	factsOrder(t, k.ID, OrderChannelNextpay, factsPaid(from.Add(time.Hour)))
	factsOrder(t, o.ID, OrderChannelNextpay, factsPaid(from.Add(time.Hour)))
	factsCredit(t, k.ID, SubscriptionProviderApple, "renewal", generateId("ff-sub"), from.Add(2*time.Hour))
	factsCredit(t, o.ID, SubscriptionProviderStripe, "renewal", generateId("ff-sub"), from.Add(2*time.Hour))
	factsOrder(t, k.ID, OrderChannelNextpay, factsRefunded(from.Add(3*time.Hour)))
	factsOrder(t, o.ID, OrderChannelNextpay, factsRefunded(from.Add(3*time.Hour)))

	recs, err := loadFunnelFacts(context.Background(), BrandOverleap, true, from, to, factsAll)
	require.NoError(t, err)
	assert.Empty(t, factsOf(recs, k.ID), "kaitu user must be filtered out")
	got := factsOf(recs, o.ID)
	require.Len(t, got, 4)
	assert.Equal(t, []string{"signup", "purchase", "renewal", "refund"},
		[]string{got[0].Event, got[1].Event, got[2].Event, got[3].Event})

	// hasBrand=false → 不过滤
	recs, err = loadFunnelFacts(context.Background(), BrandOverleap, false, from, to, factsAll)
	require.NoError(t, err)
	assert.Len(t, factsOf(recs, k.ID), 4)
	assert.Len(t, factsOf(recs, o.ID), 4)

	pays, err := loadPayments(context.Background(), BrandKaitu, true, from, to)
	require.NoError(t, err)
	var kn, on int
	for _, p := range pays {
		if p.UserID == k.ID {
			kn++
		}
		if p.UserID == o.ID {
			on++
		}
	}
	assert.Equal(t, 2, kn)
	assert.Equal(t, 0, on)
}

func TestFacts_SignupAndRefund(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	created := from.Add(30 * time.Minute)
	u := factsUser(t, BrandKaitu, created)
	refundedAt := from.Add(5 * time.Hour)
	// 付款在区间前、退款在区间内：refund 取 refunded_at
	factsOrder(t, u.ID, OrderChannelNextpay, factsPaid(from.Add(-time.Hour)), factsRefunded(refundedAt))
	// 区间外的退款不算
	factsOrder(t, u.ID, OrderChannelNextpay, factsRefunded(to.Add(time.Hour)))

	recs, err := loadFunnelFacts(context.Background(), BrandKaitu, true, from, to, []string{"signup", "refund"})
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 2)
	assert.Equal(t, "signup", got[0].Event)
	assert.True(t, got[0].At.Equal(created), "signup at created_at, got %v", got[0].At)
	assert.Equal(t, "jp", got[0].Country)
	assert.Equal(t, "refund", got[1].Event)
	assert.True(t, got[1].At.Equal(refundedAt), "refund at refunded_at, got %v", got[1].At)
	assert.Equal(t, "facts-plan", got[1].Plan)
	assert.Equal(t, OrderChannelNextpay, got[1].Channel)
}

// 软删用户的事实仍然返回（事情发生过）。
func TestFacts_SoftDeletedUserStillCounted(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandOverleap, from.Add(time.Minute))
	factsOrder(t, u.ID, OrderChannelNextpay, factsPaid(from.Add(time.Hour)))
	require.NoError(t, db.Get().Delete(u).Error)

	recs, err := loadFunnelFacts(context.Background(), BrandOverleap, true, from, to, factsAll)
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 2)
	assert.Equal(t, "signup", got[0].Event)
	assert.Equal(t, "purchase", got[1].Event)
}

func TestFacts_OnlyRequestedFacts(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandKaitu, from.Add(time.Minute))
	factsOrder(t, u.ID, OrderChannelNextpay, factsPaid(from.Add(time.Hour)))
	factsOrder(t, u.ID, OrderChannelNextpay, factsPaid(from.Add(2*time.Hour)))

	recs, err := loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, []string{"signup"})
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 1)
	assert.Equal(t, "signup", got[0].Event)

	recs, err = loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, []string{"renewal"})
	require.NoError(t, err)
	got = factsOf(recs, u.ID)
	require.Len(t, got, 1)
	assert.Equal(t, "renewal", got[0].Event)

	// 本期未实现的事实名与空列表 → 空
	recs, err = loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, []string{"trial_start", "email_sent"})
	require.NoError(t, err)
	assert.Empty(t, recs)
	recs, err = loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, nil)
	require.NoError(t, err)
	assert.Empty(t, recs)
}

func TestFirstPaymentAt_EarliestAcrossSources(t *testing.T) {
	skipIfNoConfig(t)
	from, _ := factsWindow(t)
	a := factsUser(t, BrandKaitu, from)
	b := factsUser(t, BrandKaitu, from)
	none := factsUser(t, BrandKaitu, from)
	factsOrder(t, a.ID, OrderChannelNextpay, factsPaid(from.Add(3*time.Hour)))
	factsCredit(t, a.ID, SubscriptionProviderStripe, "renewal", generateId("ff-sub"), from.Add(time.Hour))
	factsCredit(t, a.ID, SubscriptionProviderStripe, "grace", generateId("ff-sub"), from.Add(time.Minute))
	factsOrder(t, b.ID, "", factsPaid(from.Add(2*time.Hour)))
	factsOrder(t, b.ID, OrderChannelAppleIAP, factsPaid(from.Add(time.Hour)))

	first, err := firstPaymentAt(context.Background(), []uint64{a.ID, b.ID, none.ID})
	require.NoError(t, err)
	require.Contains(t, first, a.ID)
	require.Contains(t, first, b.ID)
	assert.True(t, first[a.ID].Equal(from.Add(time.Hour)), "got %v", first[a.ID])
	assert.True(t, first[b.ID].Equal(from.Add(2*time.Hour)), "got %v", first[b.ID])
	assert.NotContains(t, first, none.ID)

	empty, err := firstPaymentAt(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestLoadFunnelEvents_FilterAndAnonKind(t *testing.T) {
	skipIfNoConfig(t)
	m := funnelTestMarker(t)
	from, to := factsWindow(t)
	rows := []FunnelEvent{
		{OccurredAt: from.Add(time.Hour), Brand: "kaitu", Surface: FunnelSurfaceWeb, Event: "pricing_view", AnonID: m, Path: "/pricing", UtmSource: "x", Country: "us", Device: "desktop", OS: "macos", RefHost: "r.example", UtmCampaign: "c"},
		{OccurredAt: from.Add(2 * time.Hour), Brand: "kaitu", Surface: FunnelSurfaceApp, Event: "paywall_view", AnonID: m, UserID: 9, Plan: "p", Source: "s", Channel: "stripe", AppVersion: "1.2.3"},
		{OccurredAt: from.Add(3 * time.Hour), Brand: "kaitu", Surface: FunnelSurfaceWeb, Event: "page_view", AnonID: m},       // 事件名不在列表
		{OccurredAt: from.Add(-time.Hour), Brand: "kaitu", Surface: FunnelSurfaceWeb, Event: "pricing_view", AnonID: m},       // 区间前
		{OccurredAt: to, Brand: "kaitu", Surface: FunnelSurfaceWeb, Event: "pricing_view", AnonID: m},                         // to 是开区间
		{OccurredAt: from.Add(4 * time.Hour), Brand: "overleap", Surface: FunnelSurfaceWeb, Event: "pricing_view", AnonID: m}, // 别的品牌
	}
	require.NoError(t, db.Get().Create(&rows).Error)
	// GPC 访客：AnonID 为空，靠 path 标记清理
	gpc := FunnelEvent{OccurredAt: from.Add(5 * time.Hour), Brand: "kaitu", Surface: FunnelSurfaceWeb, Event: "pricing_view", Path: "/" + m}
	require.NoError(t, db.Get().Create(&gpc).Error)
	t.Cleanup(func() { factsCleanup(t, db.Get().Delete(&gpc).Error) })

	mine := func(recs []funnelRecord) []funnelRecord {
		var out []funnelRecord
		for _, r := range recs {
			if r.AnonID == m || r.Path == "/"+m {
				out = append(out, r)
			}
		}
		return out
	}
	names := []string{"pricing_view", "paywall_view"}
	recs, err := loadFunnelEvents(context.Background(), BrandKaitu, true, from, to, names)
	require.NoError(t, err)
	got := mine(recs)
	require.Len(t, got, 3)
	assert.Equal(t, funnelRecord{
		At: got[0].At, Event: "pricing_view", Surface: "web", AnonKind: "sid", AnonID: m,
		Path: "/pricing", UtmSource: "x", Country: "us", Device: "desktop", OS: "macos", RefHost: "r.example", UtmCampaign: "c",
	}, got[0])
	assert.True(t, got[0].At.Equal(from.Add(time.Hour)))
	assert.Equal(t, funnelRecord{
		At: got[1].At, Event: "paywall_view", Surface: "app", AnonKind: "did", AnonID: m, UserID: 9,
		Plan: "p", Source: "s", Channel: "stripe", AppVersion: "1.2.3",
	}, got[1])
	assert.Empty(t, got[2].AnonKind, "empty AnonID → empty AnonKind")
	assert.Empty(t, got[2].AnonID)

	recs, err = loadFunnelEvents(context.Background(), BrandKaitu, false, from, to, names)
	require.NoError(t, err)
	assert.Len(t, mine(recs), 4, "hasBrand=false includes the other brand's row")

	// count 与 load 同口径：共享库 → 比较加我的行前后的差不可行，改为与 load 的总行数对照
	all, err := loadFunnelEvents(context.Background(), BrandKaitu, true, from, to, names)
	require.NoError(t, err)
	n, err := countFunnelEvents(context.Background(), BrandKaitu, true, from, to, names)
	require.NoError(t, err)
	assert.Equal(t, int64(len(all)), n)
	assert.GreaterOrEqual(t, n, int64(3))

	recs, err = loadFunnelEvents(context.Background(), BrandKaitu, true, from, to, nil)
	require.NoError(t, err)
	assert.Empty(t, recs)
	n, err = countFunnelEvents(context.Background(), BrandKaitu, true, from, to, nil)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestLoadFunnelIdentities_EarliestWins(t *testing.T) {
	skipIfNoConfig(t)
	m := funnelTestMarker(t)
	base := time.Now().UTC().Truncate(time.Second)
	// 故意先插入较晚的那条：胜负由 created_at 决定而不是插入顺序
	require.NoError(t, db.Get().Create(&FunnelIdentity{Kind: "sid", AnonID: m, UserID: 900002, Brand: "kaitu", CreatedAt: base.Add(-time.Hour)}).Error)
	require.NoError(t, db.Get().Create(&FunnelIdentity{Kind: "sid", AnonID: m, UserID: 900001, Brand: "kaitu", CreatedAt: base.Add(-2 * time.Hour)}).Error)
	// 同一 anon_id 的 did 身份是另一个键
	require.NoError(t, db.Get().Create(&FunnelIdentity{Kind: "did", AnonID: m, UserID: 900003, Brand: "kaitu", CreatedAt: base.Add(-3 * time.Hour)}).Error)

	recs := []funnelRecord{
		{AnonKind: "sid", AnonID: m},
		{AnonKind: "sid", AnonID: m},
		{AnonKind: "sid", AnonID: m + "-unknown"},
		{UserID: 5}, // 事实：无匿名身份
	}
	ids, err := loadFunnelIdentities(context.Background(), recs)
	require.NoError(t, err)
	assert.Equal(t, map[string]uint64{"sid:" + m: 900001}, ids)

	ids, err = loadFunnelIdentities(context.Background(), []funnelRecord{{AnonKind: "did", AnonID: m}})
	require.NoError(t, err)
	assert.Equal(t, map[string]uint64{"did:" + m: 900003}, ids)

	ids, err = loadFunnelIdentities(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, ids)
}

// 批量边界：> 1000 个匿名身份分批 IN，仍然全部命中。
func TestLoadFunnelIdentities_Batches(t *testing.T) {
	skipIfNoConfig(t)
	m := funnelTestMarker(t)
	first, last := m+"-0", fmt.Sprintf("%s-%d", m, 2100)
	rows := []FunnelIdentity{
		{Kind: "sid", AnonID: first, UserID: 900011, Brand: "kaitu"},
		{Kind: "sid", AnonID: last, UserID: 900012, Brand: "kaitu"},
	}
	require.NoError(t, db.Get().Create(&rows).Error)
	t.Cleanup(func() {
		factsCleanup(t, db.Get().Where("anon_id IN ?", []string{first, last}).Delete(&FunnelIdentity{}).Error)
	})

	recs := make([]funnelRecord, 0, 2101)
	for i := 0; i <= 2100; i++ {
		recs = append(recs, funnelRecord{AnonKind: "sid", AnonID: fmt.Sprintf("%s-%d", m, i)})
	}
	ids, err := loadFunnelIdentities(context.Background(), recs)
	require.NoError(t, err)
	assert.Equal(t, map[string]uint64{"sid:" + first: 900011, "sid:" + last: 900012}, ids)
}

// 历史行的 channel 可能是 NULL（AutoMigrate 加的可空列）：必须与空串同样算 WordGate 付款。
func TestFacts_LegacyNullChannelIsWordgate(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandKaitu, from.Add(-time.Hour))
	at := from.Add(time.Hour)
	o := factsOrder(t, u.ID, "", factsPaid(at))
	require.NoError(t, db.Get().Model(o).Update("channel", gorm.Expr("NULL")).Error)
	var nulls int64
	require.NoError(t, db.Get().Model(&Order{}).Where("id = ? AND channel IS NULL", o.ID).Count(&nulls).Error)
	require.Equal(t, int64(1), nulls, "fixture must really hold NULL")

	recs, err := loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, factsPay)
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 1)
	assert.Equal(t, "purchase", got[0].Event)
	assert.Equal(t, OrderChannelWordgate, got[0].Channel)

	first, err := firstPaymentAt(context.Background(), []uint64{u.ID})
	require.NoError(t, err)
	require.Contains(t, first, u.ID)
	assert.True(t, first[u.ID].Equal(at))
}

func factsPlanMeta(pid string) factsOrderOpt {
	return func(o *Order) { o.Meta = `{"plan":{"pid":"` + pid + `"}}` }
}

// 两笔付款共享最早时刻：恰一笔 purchase，且是 id 较小的那张订单。
func TestLoadPayments_TieBreakLowerIDIsPurchase(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandKaitu, from.Add(-time.Hour))
	at := from.Add(time.Hour)
	o1 := factsOrder(t, u.ID, OrderChannelNextpay, factsPaid(at), factsPlanMeta("tie-low"))
	o2 := factsOrder(t, u.ID, OrderChannelNextpay, factsPaid(at), factsPlanMeta("tie-high"))
	require.Less(t, o1.ID, o2.ID)

	recs, err := loadFunnelFacts(context.Background(), BrandKaitu, false, from, to, factsPay)
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 2)
	assert.Equal(t, "purchase", got[0].Event)
	assert.Equal(t, "tie-low", got[0].Plan)
	assert.Equal(t, "renewal", got[1].Event)
	assert.Equal(t, "tie-high", got[1].Plan)
}

// 订单与入账行共享最早时刻：订单在前，所以 purchase 是订单那笔。
func TestLoadPayments_TieBreakOrderBeforeCredit(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandOverleap, from.Add(-time.Hour))
	at := from.Add(time.Hour)
	factsCredit(t, u.ID, SubscriptionProviderStripe, "purchase", generateId("ff-sub"), at)
	factsOrder(t, u.ID, OrderChannelNextpay, factsPaid(at))

	pays, err := loadPayments(context.Background(), BrandOverleap, true, from, to)
	require.NoError(t, err)
	var mine []funnelPayment
	for _, p := range pays {
		if p.UserID == u.ID {
			mine = append(mine, p)
		}
	}
	require.Len(t, mine, 2)
	assert.Equal(t, OrderChannelNextpay, mine[0].Channel)
	assert.Equal(t, "stripe", mine[1].Channel)

	recs, err := loadFunnelFacts(context.Background(), BrandOverleap, true, from, to, factsPay)
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 2)
	assert.Equal(t, "purchase", got[0].Event)
	assert.Equal(t, OrderChannelNextpay, got[0].Channel)
	assert.Equal(t, "renewal", got[1].Event)
}

func factsStripeSub(t *testing.T, userID uint64, priceID string) string {
	t.Helper()
	subID := generateId("ff-sub")
	sub := &Subscription{UserID: userID, Provider: SubscriptionProviderStripe, ProviderSubscriptionID: subID, ProductID: priceID}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { factsCleanup(t, db.Get().Unscoped().Delete(sub).Error) })
	return subID
}

// 订阅指向一个不存在的商品 → Plan 留空、不报错。
func TestLoadPayments_UnknownProductLeavesPlanEmpty(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandOverleap, from.Add(-time.Hour))
	subID := factsStripeSub(t, u.ID, generateId("price_ff_missing"))
	factsCredit(t, u.ID, SubscriptionProviderStripe, "purchase", subID, from.Add(time.Hour))

	recs, err := loadFunnelFacts(context.Background(), BrandOverleap, true, from, to, factsPay)
	require.NoError(t, err)
	got := factsOf(recs, u.ID)
	require.Len(t, got, 1)
	assert.Equal(t, "purchase", got[0].Event)
	assert.Empty(t, got[0].Plan)
}

// 套餐反查的非"查不到"错误必须上抛，不能当成空套餐吞掉。
func TestLoadPayments_PlanLookupErrorPropagates(t *testing.T) {
	skipIfNoConfig(t)
	from, to := factsWindow(t)
	u := factsUser(t, BrandOverleap, from.Add(-time.Hour))
	priceID := generateId("price_ff_boom")
	subID := factsStripeSub(t, u.ID, priceID)
	factsCredit(t, u.ID, SubscriptionProviderStripe, "purchase", subID, from.Add(time.Hour))

	boom := errors.New("funnel facts test: db down")
	orig := funnelPlanLookup
	t.Cleanup(func() { funnelPlanLookup = orig })
	funnelPlanLookup = func(ctx context.Context, provider, productID string, brand Brand) (*Plan, error) {
		if productID == priceID {
			return nil, boom
		}
		return orig(ctx, provider, productID, brand)
	}

	_, err := loadPayments(context.Background(), BrandOverleap, true, from, to)
	require.ErrorIs(t, err, boom)
	_, err = loadFunnelFacts(context.Background(), BrandOverleap, true, from, to, factsPay)
	require.ErrorIs(t, err, boom)
}
