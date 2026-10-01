package center

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// 转化漏斗的事实适配器：行为事件来自 funnel_events，事实（signup / purchase / renewal / refund）
// 不落事件表，查询时从权威表（users / orders / subscription_credits）现场投影。
//
// 口径：
//   - 付款 = orders（is_paid、paid_at 非空、channel 不是 apple_iap，含历史空 channel）
//     ∪ subscription_credits（kind ∈ purchase/renewal）。Apple IAP 一笔付款同时写订单和入账行，
//     订单一侧被排除，所以只计一次；Stripe 只有入账行。
//   - 首笔 = 该用户两个来源里**全时段**最早的一笔 → purchase；其余 → renewal。
//   - 品牌经 users.brand；软删用户的事实照常返回（事情确实发生过）。

// funnelRecord 是漏斗计算的统一输入行：行为事件与事实共用。
type funnelRecord struct {
	At                                                                                            time.Time
	Event                                                                                         string
	Surface                                                                                       string // web|app|""（事实）
	AnonKind                                                                                      string // sid|did|""
	AnonID                                                                                        string
	UserID                                                                                        uint64
	Plan, Source, Channel, Path, RefHost, UtmSource, UtmCampaign, Country, Device, OS, AppVersion string
}

// funnelPayment 是一笔付款（订单或入账行），尚未区分首购 / 续费。
type funnelPayment struct {
	UserID        uint64
	At            time.Time
	Plan, Channel string
}

const (
	funnelFactSignup   = "signup"
	funnelFactPurchase = "purchase"
	funnelFactRenewal  = "renewal"
	funnelFactRefund   = "refund"

	funnelAnonKindSid = "sid"
	funnelAnonKindDid = "did"

	funnelChannelApple = "apple"

	funnelLoadBatch = 1000
)

var funnelPaymentCreditKinds = []string{"purchase", "renewal"}

// funnelUserBrandScope 按用户品牌过滤没有 brand 列的表（同 adminUserBrandScope 的子查询写法）。
// Unscoped：软删用户仍然归属其品牌。
func funnelUserBrandScope(ctx context.Context, brand Brand, hasBrand bool, userIDColumn string) func(*gorm.DB) *gorm.DB {
	return func(tx *gorm.DB) *gorm.DB {
		if !hasBrand {
			return tx
		}
		return tx.Where(userIDColumn+" IN (?)",
			db.Get().WithContext(ctx).Unscoped().Model(&User{}).Select("id").Where("brand = ?", string(brand)))
	}
}

// funnelPaidOrderScope：算作付款的订单。Apple IAP 订单被排除（由入账行计）。
// channel IS NULL 兜底：列是 AutoMigrate 后加的可空列，历史行可能是 NULL 而不是空串。
func funnelPaidOrderScope(tx *gorm.DB) *gorm.DB {
	return tx.Where("is_paid = ? AND paid_at IS NOT NULL", true).
		Where("(channel <> ? OR channel IS NULL)", OrderChannelAppleIAP)
}

func funnelPaymentCreditScope(tx *gorm.DB) *gorm.DB {
	return tx.Where("kind IN ?", funnelPaymentCreditKinds)
}

// funnelOrderChannel 把订单渠道归一到事件表 channel 的取值域（stripe|apple|wordgate|nextpay）。
func funnelOrderChannel(ch string) string {
	switch ch {
	case "":
		return OrderChannelWordgate
	case OrderChannelAppleIAP:
		return funnelChannelApple
	default:
		return ch
	}
}

func funnelOrderPlan(o *Order) string {
	plan, err := o.GetPlan()
	if err != nil || plan == nil {
		return ""
	}
	return plan.PID
}

func funnelEventQuery(ctx context.Context, brand Brand, hasBrand bool, from, to time.Time, events []string) *gorm.DB {
	q := db.Get().WithContext(ctx).Model(&FunnelEvent{}).
		Where("occurred_at >= ? AND occurred_at < ?", from, to).
		Where("event IN ?", events)
	if hasBrand {
		return q.Scopes(ScopeBrand(brand))
	}
	// 不过滤品牌时也给出 brand 条件：idx_brand_event_time 以 brand 为前导列，缺了它索引用不上。
	all := AllBrands()
	names := make([]string, 0, len(all))
	for _, b := range all {
		names = append(names, string(b))
	}
	return q.Where("brand IN ?", names)
}

// loadFunnelEvents 加载 [from, to) 内给定事件名的行为事件，按时间升序。
func loadFunnelEvents(ctx context.Context, brand Brand, hasBrand bool, from, to time.Time, events []string) ([]funnelRecord, error) {
	if len(events) == 0 {
		return nil, nil
	}
	var rows []FunnelEvent
	if err := funnelEventQuery(ctx, brand, hasBrand, from, to, events).
		Order("occurred_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	recs := make([]funnelRecord, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		rec := funnelRecord{
			At: r.OccurredAt, Event: r.Event, Surface: r.Surface, AnonID: r.AnonID, UserID: r.UserID,
			Plan: r.Plan, Source: r.Source, Channel: r.Channel, Path: r.Path, RefHost: r.RefHost,
			UtmSource: r.UtmSource, UtmCampaign: r.UtmCampaign, Country: r.Country,
			Device: r.Device, OS: r.OS, AppVersion: r.AppVersion,
		}
		if r.AnonID != "" {
			switch r.Surface {
			case FunnelSurfaceWeb:
				rec.AnonKind = funnelAnonKindSid
			case FunnelSurfaceApp:
				rec.AnonKind = funnelAnonKindDid
			}
		}
		recs = append(recs, rec)
	}
	return recs, nil
}

// countFunnelEvents 与 loadFunnelEvents 同口径的行数（加载前的规模闸门）。
func countFunnelEvents(ctx context.Context, brand Brand, hasBrand bool, from, to time.Time, events []string) (int64, error) {
	if len(events) == 0 {
		return 0, nil
	}
	var n int64
	err := funnelEventQuery(ctx, brand, hasBrand, from, to, events).Count(&n).Error
	return n, err
}

// loadFunnelFacts 投影 [from, to) 内的事实，按时间升序。只产出 facts 里列出的名字；
// 本期实现 signup / purchase / renewal / refund，其余名字不产出任何行。
func loadFunnelFacts(ctx context.Context, brand Brand, hasBrand bool, from, to time.Time, facts []string) ([]funnelRecord, error) {
	want := make(map[string]bool, len(facts))
	for _, f := range facts {
		want[f] = true
	}
	var recs []funnelRecord

	if want[funnelFactSignup] {
		var users []User
		q := db.Get().WithContext(ctx).Unscoped().Model(&User{}).
			Select("id", "created_at", "registration_country").
			Where("created_at >= ? AND created_at < ?", from, to)
		if hasBrand {
			q = q.Scopes(ScopeBrand(brand))
		}
		if err := q.Find(&users).Error; err != nil {
			return nil, err
		}
		for i := range users {
			recs = append(recs, funnelRecord{
				At: users[i].CreatedAt, Event: funnelFactSignup, UserID: users[i].ID, Country: users[i].RegistrationCountry,
			})
		}
	}

	if want[funnelFactPurchase] || want[funnelFactRenewal] {
		pays, err := loadPayments(ctx, brand, hasBrand, from, to)
		if err != nil {
			return nil, err
		}
		seen := make(map[uint64]bool, len(pays))
		userIDs := make([]uint64, 0, len(pays))
		for _, p := range pays {
			if !seen[p.UserID] {
				seen[p.UserID] = true
				userIDs = append(userIDs, p.UserID)
			}
		}
		first, err := firstPaymentAt(ctx, userIDs)
		if err != nil {
			return nil, err
		}
		// pays 按时间升序：每个用户至多一笔 purchase（同一时刻的并列付款只有第一笔算首购）。
		purchased := make(map[uint64]bool, len(userIDs))
		for _, p := range pays {
			event := funnelFactRenewal
			if f, ok := first[p.UserID]; ok && p.At.Equal(f) && !purchased[p.UserID] {
				purchased[p.UserID] = true
				event = funnelFactPurchase
			}
			if !want[event] {
				continue
			}
			recs = append(recs, funnelRecord{At: p.At, Event: event, UserID: p.UserID, Plan: p.Plan, Channel: p.Channel})
		}
	}

	if want[funnelFactRefund] {
		var orders []Order
		if err := db.Get().WithContext(ctx).Model(&Order{}).
			Select("id", "user_id", "refunded_at", "channel", "meta").
			Where("is_refunded = ? AND refunded_at >= ? AND refunded_at < ?", true, from, to).
			Scopes(funnelUserBrandScope(ctx, brand, hasBrand, "user_id")).
			Find(&orders).Error; err != nil {
			return nil, err
		}
		for i := range orders {
			o := &orders[i]
			if o.RefundedAt == nil {
				continue
			}
			recs = append(recs, funnelRecord{
				At: *o.RefundedAt, Event: funnelFactRefund, UserID: o.UserID,
				Plan: funnelOrderPlan(o), Channel: funnelOrderChannel(o.Channel),
			})
		}
	}

	sort.SliceStable(recs, func(i, j int) bool { return recs[i].At.Before(recs[j].At) })
	return recs, nil
}

// loadFunnelIdentities 返回 AnonKind+":"+AnonID → 最早关联的 UserID，只查 recs 中出现的匿名身份。
func loadFunnelIdentities(ctx context.Context, recs []funnelRecord) (map[string]uint64, error) {
	out := make(map[string]uint64)
	byKind := make(map[string][]string)
	seen := make(map[string]bool)
	for i := range recs {
		r := &recs[i]
		if r.AnonKind == "" || r.AnonID == "" {
			continue
		}
		key := r.AnonKind + ":" + r.AnonID
		if seen[key] {
			continue
		}
		seen[key] = true
		byKind[r.AnonKind] = append(byKind[r.AnonKind], r.AnonID)
	}
	for kind, ids := range byKind {
		for start := 0; start < len(ids); start += funnelLoadBatch {
			end := min(start+funnelLoadBatch, len(ids))
			var rows []FunnelIdentity
			// 一个匿名身份只落在一个批次里，所以批内按时间升序、先到先得即全局最早。
			if err := db.Get().WithContext(ctx).Model(&FunnelIdentity{}).
				Where("kind = ? AND anon_id IN ?", kind, ids[start:end]).
				Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
				return nil, err
			}
			for i := range rows {
				key := rows[i].Kind + ":" + rows[i].AnonID
				if _, ok := out[key]; !ok {
					out[key] = rows[i].UserID
				}
			}
		}
	}
	return out, nil
}

// loadPayments 返回 [from, to) 内的全部付款（订单 ∪ 入账行），按时间升序。
// 同一时刻的并列顺序是确定的：订单在前（按 id），入账行在后（按 id）——首购判定依赖这个顺序。
func loadPayments(ctx context.Context, brand Brand, hasBrand bool, from, to time.Time) ([]funnelPayment, error) {
	var orders []Order
	if err := db.Get().WithContext(ctx).Model(&Order{}).
		Select("id", "user_id", "paid_at", "channel", "meta").
		Scopes(funnelPaidOrderScope, funnelUserBrandScope(ctx, brand, hasBrand, "user_id")).
		Where("paid_at >= ? AND paid_at < ?", from, to).
		Order("paid_at, id").
		Find(&orders).Error; err != nil {
		return nil, err
	}
	var credits []SubscriptionCredit
	if err := db.Get().WithContext(ctx).Model(&SubscriptionCredit{}).
		Scopes(funnelPaymentCreditScope, funnelUserBrandScope(ctx, brand, hasBrand, "user_id")).
		Where("created_at >= ? AND created_at < ?", from, to).
		Order("created_at, id").
		Find(&credits).Error; err != nil {
		return nil, err
	}

	pays := make([]funnelPayment, 0, len(orders)+len(credits))
	for i := range orders {
		o := &orders[i]
		if o.PaidAt == nil {
			continue
		}
		pays = append(pays, funnelPayment{
			UserID: o.UserID, At: *o.PaidAt, Plan: funnelOrderPlan(o), Channel: funnelOrderChannel(o.Channel),
		})
	}
	plans, err := funnelCreditPlans(ctx, credits)
	if err != nil {
		return nil, err
	}
	for i := range credits {
		c := &credits[i]
		pays = append(pays, funnelPayment{UserID: c.UserID, At: c.CreatedAt, Plan: plans[c.ID], Channel: c.Provider})
	}
	sort.SliceStable(pays, func(i, j int) bool { return pays[i].At.Before(pays[j].At) })
	return pays, nil
}

type funnelSubKey struct {
	UserID   uint64
	Provider string
	SubID    string // 空 = 该用户该 provider 的最新一条订阅（兜底）
}

type funnelPlanKey struct {
	Provider, ProductID, Brand string
}

// funnelCreditPlans 返回 入账行 ID → 套餐 PID。入账行本身没有套餐，经该用户同 provider 的
// Subscription.ProductID 反查：优先精确匹配这条入账行所属的订阅
// （original_transaction_id == provider_subscription_id），否则退到该用户该 provider 最新的订阅。
// 查不到（没有订阅行 / 套餐不存在）留空；其它错误上抛。
func funnelCreditPlans(ctx context.Context, credits []SubscriptionCredit) (map[uint64]string, error) {
	out := make(map[uint64]string, len(credits))
	if len(credits) == 0 {
		return out, nil
	}
	seen := make(map[uint64]bool)
	var userIDs []uint64
	for i := range credits {
		if !seen[credits[i].UserID] {
			seen[credits[i].UserID] = true
			userIDs = append(userIDs, credits[i].UserID)
		}
	}

	products := make(map[funnelSubKey]string)
	brands := make(map[uint64]string, len(userIDs))
	for start := 0; start < len(userIDs); start += funnelLoadBatch {
		batch := userIDs[start:min(start+funnelLoadBatch, len(userIDs))]
		var subs []Subscription
		if err := db.Get().WithContext(ctx).Model(&Subscription{}).
			Select("id", "user_id", "provider", "provider_subscription_id", "product_id").
			Where("user_id IN ?", batch).Order("id ASC").Find(&subs).Error; err != nil {
			return nil, err
		}
		for i := range subs {
			s := &subs[i]
			products[funnelSubKey{s.UserID, s.Provider, s.ProviderSubscriptionID}] = s.ProductID
			products[funnelSubKey{s.UserID, s.Provider, ""}] = s.ProductID // id 升序 → 最后写入的是最新一条
		}
		var users []User
		if err := db.Get().WithContext(ctx).Unscoped().Model(&User{}).
			Select("id", "brand").Where("id IN ?", batch).Find(&users).Error; err != nil {
			return nil, err
		}
		for i := range users {
			brands[users[i].ID] = users[i].Brand
		}
	}

	cache := make(map[funnelPlanKey]string)
	for i := range credits {
		c := &credits[i]
		productID, ok := products[funnelSubKey{c.UserID, c.Provider, c.OriginalTransactionID}]
		if !ok || c.OriginalTransactionID == "" {
			productID = products[funnelSubKey{c.UserID, c.Provider, ""}]
		}
		if productID == "" {
			continue
		}
		key := funnelPlanKey{Provider: c.Provider, ProductID: productID}
		if c.Provider == SubscriptionProviderApple {
			key.Brand = brands[c.UserID] // Apple 商品 ID 按品牌解析；Stripe 的 price id 全局唯一
		}
		pid, cached := cache[key]
		if !cached {
			plan, err := funnelPlanLookup(ctx, c.Provider, productID, Brand(key.Brand))
			switch {
			case err == nil:
				if plan != nil {
					pid = plan.PID
				}
			case errors.Is(err, gorm.ErrRecordNotFound):
				// 套餐不存在 / 已下架：留空。
			default:
				// 其余（DB 故障、context 取消、Apple 商品映射歧义）一律上抛：
				// 当成"查不到"会把空套餐缓存给同 key 的所有入账行，分组静默出错。
				return nil, fmt.Errorf("funnel: plan lookup for %s product %s: %w", c.Provider, productID, err)
			}
			cache[key] = pid
		}
		out[c.ID] = pid
	}
	return out, nil
}

// funnelPlanLookup 按 provider 的商品标识反查套餐。包级变量仅为测试注入失败用。
var funnelPlanLookup = func(ctx context.Context, provider, productID string, brand Brand) (*Plan, error) {
	tx := db.Get().WithContext(ctx)
	switch provider {
	case SubscriptionProviderStripe:
		// planByStripePriceID 只在走 Stripe 售卖的那个品牌的套餐里查：别的品牌的入账行套餐留空是设计如此。
		return planByStripePriceID(ctx, tx, productID)
	case SubscriptionProviderApple:
		return planByAppleProductID(ctx, tx, productID, brand)
	}
	return nil, gorm.ErrRecordNotFound
}

type funnelFirstAtRow struct {
	UserID  uint64
	FirstAt time.Time
}

// firstPaymentAt 返回给定用户在两个付款来源里**全时段**最早的付款时间；从未付款的用户不在结果里。
func firstPaymentAt(ctx context.Context, userIDs []uint64) (map[uint64]time.Time, error) {
	out := make(map[uint64]time.Time, len(userIDs))
	merge := func(rows []funnelFirstAtRow) {
		for _, r := range rows {
			if cur, ok := out[r.UserID]; !ok || r.FirstAt.Before(cur) {
				out[r.UserID] = r.FirstAt
			}
		}
	}
	for start := 0; start < len(userIDs); start += funnelLoadBatch {
		batch := userIDs[start:min(start+funnelLoadBatch, len(userIDs))]

		var fromOrders []funnelFirstAtRow
		if err := db.Get().WithContext(ctx).Model(&Order{}).
			Select("user_id, MIN(paid_at) as first_at").
			Scopes(funnelPaidOrderScope).
			Where("user_id IN ?", batch).
			Group("user_id").Scan(&fromOrders).Error; err != nil {
			return nil, err
		}
		merge(fromOrders)

		var fromCredits []funnelFirstAtRow
		if err := db.Get().WithContext(ctx).Model(&SubscriptionCredit{}).
			Select("user_id, MIN(created_at) as first_at").
			Scopes(funnelPaymentCreditScope).
			Where("user_id IN ?", batch).
			Group("user_id").Scan(&fromCredits).Error; err != nil {
			return nil, err
		}
		merge(fromCredits)
	}
	return out, nil
}
