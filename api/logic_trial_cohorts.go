package center

import (
	"context"
	"sort"
	"time"

	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
)

// 试用转化：按 Apple 免费试用开始日（UTC，与漏斗 / 留存同口径）分组，看每批试用现在走到了哪一步。
// 试用链 = trial_ends_at > 0 的 Apple 订阅：试用交易入账时写 trial_ends_at，转付费后只清 is_trial、
// 不清 trial_ends_at（logic_apple_iap.go），所以它永久标记"这条链是从试用开始的"。
// 只看正式环境；沙盒（审核员、测试账号）不计。

const (
	trialCohortDefaultDays = 30
	trialCohortMaxDays     = 90
	// trialSettleWindow：试用到期后 Apple 扣费、通知送达所需的时间。窗口内的链既不算转化也不算流失，
	// 不进转化率的分母——否则每天盯数据时，刚到期、扣费通知还没到的人会被当成没付费。
	trialSettleWindow = 24 * time.Hour
)

const trialCohortNote = "按试用开始日（UTC）分组；每条 Apple 订阅链只落在一个状态里。转化率 = 已结算链中的已付费 ÷ 已结算链（试用结束超过 24 小时），未结算的批次为空。「到期待扣费」= 试用结束不足 24 小时、Apple 扣费结果还没到。关闭自动续费的人仍在试用期内，到期不会扣费。金额为订单的套餐标价（分），不是 Apple 实际结算额（未扣 Apple 抽成）。沙盒不计。"

// 试用链状态。各状态互斥。
const (
	trialStateRenewOn        = "in_trial_renew_on"  // 试用中，自动续费开着（到期会扣费）
	trialStateRenewOff       = "in_trial_renew_off" // 试用中，已关闭自动续费
	trialStateAwaitingCharge = "awaiting_charge"    // 试用结束不足 24 小时，扣费结果未到
	trialStateBillingRetry   = "billing_retry"      // Apple 扣费失败重试中
	trialStateConverted      = "converted"          // 已转付费
	trialStateEndedUnpaid    = "ended_unpaid"       // 试用结束未付费
	trialStateRevoked        = "revoked"            // 退款 / 撤销
)

// trialChain 是计算所需的订阅链快照。
type trialChain struct {
	UserID      uint64
	StartedAt   time.Time
	TrialEndsAt int64 // unix 秒
	IsTrial     bool  // 仍在试用（从没付过费）
	AutoRenew   bool
	Status      string // active|grace|billing_retry|expired|revoked
	PaidThrough int64  // > 0 = 已有付费入账
}

// trialChainState 把一条链归到唯一状态。
func trialChainState(ch trialChain, now time.Time) string {
	ended := ch.TrialEndsAt <= now.Unix()
	switch {
	case ch.Status == "revoked":
		return trialStateRevoked
	case !ch.IsTrial && ch.PaidThrough > 0:
		return trialStateConverted
	case ch.Status == "billing_retry":
		return trialStateBillingRetry
	case ch.Status == "expired":
		return trialStateEndedUnpaid
	case ended && !trialChainSettled(ch, now):
		return trialStateAwaitingCharge
	case ended:
		// 结算窗口已过仍是 active / grace：webhook 丢了或还没对账，按未付费计（对账每日兜底纠正）。
		return trialStateEndedUnpaid
	case ch.AutoRenew:
		return trialStateRenewOn
	default:
		return trialStateRenewOff
	}
}

// trialChainSettled：试用结束已超过结算窗口，结果算数。
func trialChainSettled(ch trialChain, now time.Time) bool {
	return ch.TrialEndsAt > 0 && ch.TrialEndsAt <= now.Add(-trialSettleWindow).Unix()
}

// TrialCohortRow 一个试用开始日的状态分布。各状态互斥，加起来 = Started。
type TrialCohortRow struct {
	Cohort           string   `json:"cohort"` // "2026-10-28"
	Started          int      `json:"started"`
	InTrialRenew     int      `json:"inTrialRenewOn"`
	InTrialOff       int      `json:"inTrialRenewOff"`
	AwaitingCharge   int      `json:"awaitingCharge"`
	BillingRetry     int      `json:"billingRetry"`
	Converted        int      `json:"converted"`
	EndedUnpaid      int      `json:"endedUnpaid"`
	Revoked          int      `json:"revoked"`
	Settled          int      `json:"settled"`          // 已结算链数（转化率分母）
	SettledConverted int      `json:"settledConverted"` // 已结算链中的已付费（转化率分子）
	ConvRate         *float64 `json:"conversionRate"`   // SettledConverted / Settled；Settled = 0 时为 null
	PaidAmount       uint64   `json:"paidAmount"`       // 这批链的已付款 Apple 订单标价合计（分，未扣退款）
	RefundedAmount   uint64   `json:"refundedAmount"`   // 其中已退款的订单标价合计（分）
}

// TrialChainDetail 单条试用链，用于按批次展开名单。
type TrialChainDetail struct {
	UserID         uint64 `json:"userId"`
	UserUUID       string `json:"userUuid"` // 用 lookup_user 查详情；不带邮箱（加密存储，且设备账号没有）
	StartedAt      int64  `json:"startedAt"`   // unix 秒
	TrialEndsAt    int64  `json:"trialEndsAt"` // unix 秒
	State          string `json:"state"`
	AutoRenew      bool   `json:"autoRenew"`
	PaidAmount     uint64 `json:"paidAmount"`     // 分
	RefundedAmount uint64 `json:"refundedAmount"` // 分
	PlanTitle      string `json:"planTitle,omitempty"`
}

type TrialCohortResponse struct {
	Rows   []TrialCohortRow   `json:"rows"` // 按日期升序，永不为 null
	Totals TrialCohortRow     `json:"totals"`
	Chains []TrialChainDetail `json:"chains,omitempty"` // 仅带 ?cohort= 时返回该批名单
	Note   string             `json:"note"`
}

// trialUserPayment 一个用户的 Apple 订单汇总（试用开始之后的付款）。
type trialUserPayment struct {
	Paid      uint64
	Refunded  uint64
	PlanTitle string
}

// computeTrialCohorts 是纯函数：把链按开始日归组并分状态。pays 以 UserID 为键（可为 nil）。
func computeTrialCohorts(chains []trialChain, pays map[uint64]trialUserPayment, now time.Time) TrialCohortResponse {
	byDay := map[string]*TrialCohortRow{}
	var order []string
	totals := TrialCohortRow{Cohort: "total"}
	payCounted := map[uint64]bool{} // 同一用户多条试用链（换 Apple ID）时，付款只计一次
	for _, ch := range chains {
		day := funnelUTCDay(ch.StartedAt).Format(funnelQueryDateForm)
		row, ok := byDay[day]
		if !ok {
			row = &TrialCohortRow{Cohort: day}
			byDay[day] = row
			order = append(order, day)
		}
		state := trialChainState(ch, now)
		settled := trialChainSettled(ch, now)
		var pay trialUserPayment
		if !payCounted[ch.UserID] {
			payCounted[ch.UserID] = true
			pay = pays[ch.UserID]
		}
		for _, r := range []*TrialCohortRow{row, &totals} {
			r.Started++
			switch state {
			case trialStateRenewOn:
				r.InTrialRenew++
			case trialStateRenewOff:
				r.InTrialOff++
			case trialStateAwaitingCharge:
				r.AwaitingCharge++
			case trialStateBillingRetry:
				r.BillingRetry++
			case trialStateConverted:
				r.Converted++
			case trialStateEndedUnpaid:
				r.EndedUnpaid++
			case trialStateRevoked:
				r.Revoked++
			}
			if settled {
				r.Settled++
				if state == trialStateConverted {
					r.SettledConverted++
				}
			}
			r.PaidAmount += pay.Paid
			r.RefundedAmount += pay.Refunded
		}
	}
	rows := make([]TrialCohortRow, 0, len(order))
	sort.Strings(order)
	for _, d := range order {
		rows = append(rows, *byDay[d])
	}
	for i := range rows {
		rows[i].ConvRate = trialConvRate(rows[i])
	}
	totals.ConvRate = trialConvRate(totals)
	return TrialCohortResponse{Rows: rows, Totals: totals, Note: trialCohortNote}
}

func trialConvRate(r TrialCohortRow) *float64 {
	if r.Settled == 0 {
		return nil
	}
	v := float64(r.SettledConverted) / float64(r.Settled)
	return &v
}

// trialChainDetails 列出开始日 = cohort 的链（按开始时间升序）。users 以 UserID 为键提供 uuid。
func trialChainDetails(chains []trialChain, pays map[uint64]trialUserPayment, users map[uint64]User, cohort string, now time.Time) []TrialChainDetail {
	out := []TrialChainDetail{}
	for _, ch := range chains {
		if funnelUTCDay(ch.StartedAt).Format(funnelQueryDateForm) != cohort {
			continue
		}
		u := users[ch.UserID]
		pay := pays[ch.UserID]
		out = append(out, TrialChainDetail{
			UserID: ch.UserID, UserUUID: u.UUID,
			StartedAt: ch.StartedAt.Unix(), TrialEndsAt: ch.TrialEndsAt,
			State: trialChainState(ch, now), AutoRenew: ch.AutoRenew,
			PaidAmount: pay.Paid, RefundedAmount: pay.Refunded, PlanTitle: pay.PlanTitle,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt < out[j].StartedAt })
	return out
}

// loadTrialChains 装载 since 起开始的正式环境 Apple 试用链。
func loadTrialChains(ctx context.Context, brand Brand, hasBrand bool, since time.Time) ([]trialChain, error) {
	var subs []Subscription
	if err := db.Get().WithContext(ctx).Model(&Subscription{}).
		Select("user_id", "created_at", "trial_ends_at", "is_trial", "auto_renew", "status", "paid_through").
		Where("provider = ? AND trial_ends_at > 0 AND created_at >= ?", SubscriptionProviderApple, since).
		Where("environment <> ?", string(appstore.Environment_Sandbox)).
		Scopes(funnelUserBrandScope(ctx, brand, hasBrand, "user_id")).
		Find(&subs).Error; err != nil {
		return nil, err
	}
	out := make([]trialChain, len(subs))
	for i, s := range subs {
		out[i] = trialChain{
			UserID: s.UserID, StartedAt: s.CreatedAt, TrialEndsAt: s.TrialEndsAt, IsTrial: s.IsTrial,
			AutoRenew: s.AutoRenew, Status: s.Status, PaidThrough: s.PaidThrough,
		}
	}
	return out, nil
}

// loadTrialPayments 汇总这些链的用户在试用开始之后的已付款 Apple 订单（试用本身不建单，所以全是付费）。
func loadTrialPayments(ctx context.Context, chains []trialChain) (map[uint64]trialUserPayment, error) {
	startOf := make(map[uint64]time.Time, len(chains))
	ids := make([]uint64, 0, len(chains))
	for _, ch := range chains {
		if t, ok := startOf[ch.UserID]; !ok || ch.StartedAt.Before(t) {
			if !ok {
				ids = append(ids, ch.UserID)
			}
			startOf[ch.UserID] = ch.StartedAt
		}
	}
	out := make(map[uint64]trialUserPayment, len(ids))
	for start := 0; start < len(ids); start += funnelLoadBatch {
		end := min(start+funnelLoadBatch, len(ids))
		var orders []Order
		if err := db.Get().WithContext(ctx).Model(&Order{}).
			Select("user_id", "title", "pay_amount", "paid_at", "is_refunded").
			Where("user_id IN ? AND channel = ? AND is_paid = ? AND paid_at IS NOT NULL", ids[start:end], OrderChannelAppleIAP, true).
			Order("paid_at ASC").
			Find(&orders).Error; err != nil {
			return nil, err
		}
		for _, o := range orders {
			if o.PaidAt.Before(startOf[o.UserID]) {
				continue
			}
			p := out[o.UserID]
			p.Paid += o.PayAmount
			if o.IsRefunded != nil && *o.IsRefunded {
				p.Refunded += o.PayAmount
			}
			if p.PlanTitle == "" {
				p.PlanTitle = o.Title
			}
			out[o.UserID] = p
		}
	}
	return out, nil
}

// loadTrialUsers 取名单展示用的 uuid。
func loadTrialUsers(ctx context.Context, chains []trialChain, cohort string) (map[uint64]User, error) {
	var ids []uint64
	for _, ch := range chains {
		if funnelUTCDay(ch.StartedAt).Format(funnelQueryDateForm) == cohort {
			ids = append(ids, ch.UserID)
		}
	}
	out := make(map[uint64]User, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var users []User
	if err := db.Get().WithContext(ctx).Unscoped().Model(&User{}).
		Select("id", "uuid").Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, u := range users {
		out[u.ID] = u
	}
	return out, nil
}
