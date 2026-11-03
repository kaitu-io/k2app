package center

import (
	"context"
	"sort"
	"strings"
	"time"

	db "github.com/wordgate/qtoolkit/db"
)

// 召回效果统计（spec 2026-11-03-recall-offer-flatten §统计）。
//
// 三条召回链路（未付款 abandoned、过期 winback、试用 trial-recall）都按 inRecallHoldout 留 10% 对照组：
// 召回组 = email_send_logs 里 status=sent 的行；对照组 = status=skipped 且 error_msg=holdout 的行。
// 两组都只算有邮箱的人（对照组是"本来会收到邮件"的人），批次 ID 的前两段是 链路:阶段。
//
// 付款 = 该用户任意渠道（官网 / Apple / Stripe）未退款的已付订单，落在 [首次召回或对照记录, +窗口天数]。
// 窗口还没走完的人计入 Pending，不进转化率——否则越新的批次转化率越被低估。

// recallFlows 统计覆盖的链路（batch_id 第一段）。
var recallFlows = []string{"abandoned", "winback", "trial-recall"}

type recallLogRow struct {
	UserID    uint64
	BatchID   string
	Status    string
	ErrorMsg  *string
	Email     string
	CreatedAt time.Time
}

type recallPaidOrder struct {
	UserID               uint64
	PaidAt               time.Time
	PayAmount            uint64
	CampaignCode         *string // orders.campaign_code 可为 NULL
	CampaignReduceAmount uint64
}

// RecallArmStats 一组人（召回组或对照组）在窗口内的付款情况。
type RecallArmStats struct {
	Users         int     `json:"users"`         // 窗口已走完的人数（转化率分母）
	Paid          int     `json:"paid"`          // 其中窗口内付过款的人数
	Rate          float64 `json:"rate"`          // Paid / Users，0~1
	RevenueCents  uint64  `json:"revenueCents"`  // 这些人在窗口内的实付合计
	PaidWithCode  int     `json:"paidWithCode"`  // 窗口内付款且用了任意活动码的人数
	DiscountCents uint64  `json:"discountCents"` // 活动码让掉的金额合计
	Pending       int     `json:"pending"`       // 窗口还没走完、暂不计入的人数
}

type RecallStageStats struct {
	Stage   string         `json:"stage"`
	Treated RecallArmStats `json:"treated"`
	Holdout RecallArmStats `json:"holdout"`
}

type RecallFlowStats struct {
	Flow    string         `json:"flow"`
	Treated RecallArmStats `json:"treated"` // 按人去重：每人取本链路第一次记录作为起点
	Holdout RecallArmStats `json:"holdout"`
	// LiftPoints 召回组转化率 − 对照组转化率（百分点）；对照组为 0 人时为 nil（无法比较）。
	LiftPoints *float64           `json:"liftPoints"`
	Stages     []RecallStageStats `json:"stages"`
}

type RecallStats struct {
	Since       time.Time         `json:"since"`
	WindowDays  int               `json:"windowDays"`
	GeneratedAt time.Time         `json:"generatedAt"`
	Flows       []RecallFlowStats `json:"flows"`
	Note        string            `json:"note"`
}

const recallStatsNote = "对照组 10% 不收召回邮件；liftPoints = 召回组转化率 − 对照组转化率（百分点），才是召回带来的增量。" +
	"对照组在各链路上线后才开始积累（试用召回 2026-11-03，未付款 / 过期召回自本次部署起），之前只有召回组、没有可比对象。" +
	"每组人数少于约 100 时差值主要是噪声，先看 users 再看 rate。"

// recallArm 解析一行日志属于哪一组；第二个返回值 false = 不参与统计（失败 / 无邮箱 / 非对照的跳过）。
func recallArm(r recallLogRow) (treated bool, ok bool) {
	if r.Email == "" {
		return false, false
	}
	switch r.Status {
	case string(EmailSendLogStatusSent):
		return true, true
	case string(EmailSendLogStatusSkipped):
		if r.ErrorMsg != nil && *r.ErrorMsg == recallHoldoutReason {
			return false, true
		}
	}
	return false, false
}

// recallFlowStage 从 batch_id 取 链路、阶段，如 "abandoned:3d:2026-11-03" → abandoned, 3d。
func recallFlowStage(batchID string) (flow, stage string, ok bool) {
	parts := strings.SplitN(batchID, ":", 3)
	if len(parts) < 3 {
		return "", "", false
	}
	for _, f := range recallFlows {
		if parts[0] == f {
			return parts[0], parts[1], true
		}
	}
	return "", "", false
}

type recallUnit struct {
	userID  uint64
	treated bool
	start   time.Time
}

// computeRecallStats 纯计算，便于测试；orders 只需包含相关用户的已付未退款订单。
func computeRecallStats(rows []recallLogRow, orders []recallPaidOrder, since, now time.Time, windowDays int) RecallStats {
	window := time.Duration(windowDays) * 24 * time.Hour
	byUser := map[uint64][]recallPaidOrder{}
	for _, o := range orders {
		byUser[o.UserID] = append(byUser[o.UserID], o)
	}

	arm := func(units []recallUnit, treated bool) RecallArmStats {
		var s RecallArmStats
		for _, u := range units {
			if u.treated != treated {
				continue
			}
			if u.start.Add(window).After(now) {
				s.Pending++
				continue
			}
			s.Users++
			paid, withCode := false, false
			for _, o := range byUser[u.userID] {
				if o.PaidAt.Before(u.start) || o.PaidAt.After(u.start.Add(window)) {
					continue
				}
				paid = true
				s.RevenueCents += o.PayAmount
				if o.CampaignCode != nil && *o.CampaignCode != "" {
					withCode = true
					s.DiscountCents += o.CampaignReduceAmount
				}
			}
			if paid {
				s.Paid++
			}
			if withCode {
				s.PaidWithCode++
			}
		}
		if s.Users > 0 {
			s.Rate = float64(s.Paid) / float64(s.Users)
		}
		return s
	}

	// flow → user → unit（本链路首次记录）；flow → stage → user → unit（该阶段首次记录）。
	flowUnits := map[string]map[uint64]*recallUnit{}
	stageUnits := map[string]map[string]map[uint64]*recallUnit{}
	upsert := func(m map[uint64]*recallUnit, r recallLogRow, treated bool) {
		u, ok := m[r.UserID]
		if !ok {
			m[r.UserID] = &recallUnit{userID: r.UserID, treated: treated, start: r.CreatedAt}
			return
		}
		if r.CreatedAt.Before(u.start) {
			u.start = r.CreatedAt
		}
		u.treated = u.treated || treated // 收到过任何一封就算召回组
	}
	for _, r := range rows {
		treated, ok := recallArm(r)
		if !ok || r.CreatedAt.Before(since) {
			continue
		}
		flow, stage, ok := recallFlowStage(r.BatchID)
		if !ok {
			continue
		}
		if flowUnits[flow] == nil {
			flowUnits[flow] = map[uint64]*recallUnit{}
			stageUnits[flow] = map[string]map[uint64]*recallUnit{}
		}
		if stageUnits[flow][stage] == nil {
			stageUnits[flow][stage] = map[uint64]*recallUnit{}
		}
		upsert(flowUnits[flow], r, treated)
		upsert(stageUnits[flow][stage], r, treated)
	}

	list := func(m map[uint64]*recallUnit) []recallUnit {
		out := make([]recallUnit, 0, len(m))
		for _, u := range m {
			out = append(out, *u)
		}
		return out
	}

	out := RecallStats{Since: since, WindowDays: windowDays, GeneratedAt: now, Note: recallStatsNote, Flows: []RecallFlowStats{}}
	for _, flow := range recallFlows {
		units, ok := flowUnits[flow]
		if !ok {
			continue
		}
		fs := RecallFlowStats{Flow: flow, Treated: arm(list(units), true), Holdout: arm(list(units), false), Stages: []RecallStageStats{}}
		if fs.Holdout.Users > 0 && fs.Treated.Users > 0 {
			lift := (fs.Treated.Rate - fs.Holdout.Rate) * 100
			fs.LiftPoints = &lift
		}
		stages := make([]string, 0, len(stageUnits[flow]))
		for st := range stageUnits[flow] {
			stages = append(stages, st)
		}
		sort.Slice(stages, func(i, j int) bool { return recallStageOrder(stages[i]) < recallStageOrder(stages[j]) })
		for _, st := range stages {
			su := list(stageUnits[flow][st])
			fs.Stages = append(fs.Stages, RecallStageStats{Stage: st, Treated: arm(su, true), Holdout: arm(su, false)})
		}
		out.Flows = append(out.Flows, fs)
	}
	return out
}

// recallStageOrder 阶段排序：1h < 1d < 3d < 7d < 14d < 30d，s1 < s2，其余按字典序垫后。
func recallStageOrder(stage string) float64 {
	n := 0
	i := 0
	for i < len(stage) && stage[i] >= '0' && stage[i] <= '9' {
		n = n*10 + int(stage[i]-'0')
		i++
	}
	switch {
	case i > 0 && stage[i:] == "h":
		return float64(n) / 24
	case i > 0 && stage[i:] == "d":
		return float64(n)
	case stage == "s1":
		return 1000
	case stage == "s2":
		return 1001
	}
	return 2000
}

// loadRecallStats 从 email_send_logs + orders 计算召回统计。
func loadRecallStats(ctx context.Context, since time.Time, windowDays int) (RecallStats, error) {
	var rows []recallLogRow
	q := db.Get().WithContext(ctx).Model(&EmailSendLog{}).
		Select("user_id, batch_id, status, error_msg, email, created_at").
		Where("created_at >= ?", since).
		Where("status = ? OR (status = ? AND error_msg = ?)",
			string(EmailSendLogStatusSent), string(EmailSendLogStatusSkipped), recallHoldoutReason)
	likes := db.Get().Where("batch_id LIKE ?", recallFlows[0]+":%")
	for _, f := range recallFlows[1:] {
		likes = likes.Or("batch_id LIKE ?", f+":%")
	}
	if err := q.Where(likes).Find(&rows).Error; err != nil {
		return RecallStats{}, err
	}

	seen := map[uint64]bool{}
	ids := make([]uint64, 0, len(rows))
	for _, r := range rows {
		if !seen[r.UserID] {
			seen[r.UserID] = true
			ids = append(ids, r.UserID)
		}
	}
	var orders []recallPaidOrder
	for start := 0; start < len(ids); start += 1000 {
		end := min(start+1000, len(ids))
		var chunk []recallPaidOrder
		if err := db.Get().WithContext(ctx).Model(&Order{}).
			Select("user_id, paid_at, pay_amount, campaign_code, campaign_reduce_amount").
			Where("is_paid = ? AND paid_at >= ? AND user_id IN ?", true, since, ids[start:end]).
			Where("is_refunded = ? OR is_refunded IS NULL", false).
			Find(&chunk).Error; err != nil {
			return RecallStats{}, err
		}
		orders = append(orders, chunk...)
	}
	return computeRecallStats(rows, orders, since, time.Now(), windowDays), nil
}
