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
)

const trialCohortNote = "按试用开始日（UTC）分组；每条 Apple 订阅链只落在一个状态里。转化率 = 已付费 ÷ 已到期（试用结束时间已过的链），没有到期的批次为空。关闭自动续费的人仍在试用期内，到期不会扣费。沙盒不计。"

// trialChain 是计算所需的订阅链快照。
type trialChain struct {
	StartedAt   time.Time
	TrialEndsAt int64 // unix 秒
	IsTrial     bool  // 仍在试用（从没付过费）
	AutoRenew   bool
	Status      string // active|grace|billing_retry|expired|revoked
	PaidThrough int64  // > 0 = 已有付费入账
}

// TrialCohortRow 一个试用开始日的状态分布。各状态互斥，加起来 = Started。
type TrialCohortRow struct {
	Cohort       string   `json:"cohort"` // "2026-10-28"
	Started      int      `json:"started"`
	InTrialRenew int      `json:"inTrialRenewOn"`  // 试用中，自动续费开着（到期会扣费）
	InTrialOff   int      `json:"inTrialRenewOff"` // 试用中，已关闭自动续费
	BillingRetry int      `json:"billingRetry"`    // 试用已到期，Apple 扣费失败重试中
	Converted    int      `json:"converted"`       // 已转付费
	EndedUnpaid  int      `json:"endedUnpaid"`     // 试用结束未付费
	Revoked      int      `json:"revoked"`         // 退款 / 撤销
	Matured      int      `json:"matured"`         // 试用结束时间已过的链数
	ConvRate     *float64 `json:"conversionRate"`  // Converted / Matured；Matured = 0 时为 null
}

type TrialCohortResponse struct {
	Rows   []TrialCohortRow `json:"rows"` // 按日期升序，永不为 null
	Totals TrialCohortRow   `json:"totals"`
	Note   string           `json:"note"`
}

// computeTrialCohorts 是纯函数：把链按开始日归组并分状态。
func computeTrialCohorts(chains []trialChain, now time.Time) TrialCohortResponse {
	byDay := map[string]*TrialCohortRow{}
	var order []string
	totals := TrialCohortRow{Cohort: "total"}
	for _, ch := range chains {
		day := funnelUTCDay(ch.StartedAt).Format(funnelQueryDateForm)
		row, ok := byDay[day]
		if !ok {
			row = &TrialCohortRow{Cohort: day}
			byDay[day] = row
			order = append(order, day)
		}
		for _, r := range []*TrialCohortRow{row, &totals} {
			r.Started++
			switch {
			case ch.Status == "revoked":
				r.Revoked++
			case !ch.IsTrial && ch.PaidThrough > 0:
				r.Converted++
			case ch.Status == "billing_retry":
				r.BillingRetry++
			case ch.Status == "expired" || ch.TrialEndsAt <= now.Unix():
				r.EndedUnpaid++
			case ch.AutoRenew:
				r.InTrialRenew++
			default:
				r.InTrialOff++
			}
			if ch.TrialEndsAt > 0 && ch.TrialEndsAt <= now.Unix() {
				r.Matured++
			}
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
	if r.Matured == 0 {
		return nil
	}
	v := float64(r.Converted) / float64(r.Matured)
	return &v
}

// loadTrialChains 装载 since 起开始的正式环境 Apple 试用链。
func loadTrialChains(ctx context.Context, brand Brand, hasBrand bool, since time.Time) ([]trialChain, error) {
	var subs []Subscription
	if err := db.Get().WithContext(ctx).Model(&Subscription{}).
		Select("created_at", "trial_ends_at", "is_trial", "auto_renew", "status", "paid_through").
		Where("provider = ? AND trial_ends_at > 0 AND created_at >= ?", SubscriptionProviderApple, since).
		Where("environment <> ?", string(appstore.Environment_Sandbox)).
		Scopes(funnelUserBrandScope(ctx, brand, hasBrand, "user_id")).
		Find(&subs).Error; err != nil {
		return nil, err
	}
	out := make([]trialChain, len(subs))
	for i, s := range subs {
		out[i] = trialChain{
			StartedAt: s.CreatedAt, TrialEndsAt: s.TrialEndsAt, IsTrial: s.IsTrial,
			AutoRenew: s.AutoRenew, Status: s.Status, PaidThrough: s.PaidThrough,
		}
	}
	return out, nil
}
