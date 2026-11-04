package center

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/wordgate/qtoolkit/asynq"
	"github.com/wordgate/qtoolkit/log"
)

// =====================================================================
// EDM 节奏发送（pacing）
// =====================================================================
//
// 不带 pacing 的发送请求走老路：整批一个 edm:send-templated 任务，SendTemplatedEmails 以 ~10 封/秒连发。
// 带 pacing 时整批拆成「每封一个任务」，每个任务仍是 edm:send-templated 类型、payload 里只有 1 个条目，
// 用 ProcessAt 排到各自的时刻：
//   - 第一封在 startAt（缺省/已过去 → 现在），之后每封在上一封基础上 + [minIntervalSec, maxIntervalSec] 均匀随机；
//   - 配了每日发送窗口（windowStartHour/windowEndHour/windowTZ 三个一起）时，任何落在
//     [windowStartHour, windowEndHour) 之外的时刻顺延到下一个窗口起点，之后照旧累加——大批次会自动铺到多个晚上。
//
// 为什么复用 edm:send-templated 而不新增任务类型：handler 已存在且就是我们要的语义（单条目也走
// SendTemplatedEmails → sendSingleTemplatedEmail：退订跳过、UnsubscribeURL 注入、send log、幂等键），
// 不用新接线；滚动部署时旧版 worker 也能正确处理这些任务（新任务类型落到旧 worker 上会直接失败）。
// 一个任务一封信也让 asynq 的 30 分钟默认 Timeout 不再成为约束——任务里从不 sleep。
//
// 幂等：任务重试（worker 崩溃/超时）时 email_send_logs 的 (batch, template, user) 键已存在 → skipped，不会重发。
//
// 取消一个已排期的批次：每个任务的 asynq TaskID 是 edmPacedTaskID(batchId, i) = "edm:<batchId>:<i>"，
// payload 里也带 batchId。在 /app/asynqmon（超管）→ Queues → default → Scheduled 里按 ID 前缀找到并删除
// （可多选批量删）；或用 hibiken asynq Inspector 对 i ∈ [0, 条目数) 逐个 DeleteTask("default", id)。
// 已经发出的不受影响；之后用同一 batchId 重新提交，已发/已记录的收件人会被幂等键跳过。

const (
	edmPacingMaxIntervalSec = 3600
	edmPacingMaxStartAhead  = 14 * 24 * time.Hour
)

// EDMPacing 节奏发送参数（API JSON camelCase；随审批 params 持久化）。
type EDMPacing struct {
	MinIntervalSec int `json:"minIntervalSec"`
	MaxIntervalSec int `json:"maxIntervalSec"`
	// StartAt RFC3339；为空或已过去（例如审批拖到了 startAt 之后）→ 从执行时刻开始。
	StartAt *time.Time `json:"startAt,omitempty"`
	// 每日发送窗口 [WindowStartHour, WindowEndHour)，WindowTZ 为 IANA 时区；三者要么都给要么都不给。
	WindowStartHour *int   `json:"windowStartHour,omitempty"`
	WindowEndHour   *int   `json:"windowEndHour,omitempty"`
	WindowTZ        string `json:"windowTZ,omitempty"`
}

// EDMPacedSchedule 节奏发送的排期结果（超管直通时作为 /app/edm/send 的响应 data）。
type EDMPacedSchedule struct {
	BatchID string `json:"batchId"`
	// Scheduled 本次新入队的任务数；AlreadyScheduled 是 TaskID 已存在（同批次重复提交）而跳过的条目数。
	Scheduled        int       `json:"scheduled"`
	AlreadyScheduled int       `json:"alreadyScheduled"`
	FirstAt          time.Time `json:"firstAt"`
	LastAt           time.Time `json:"lastAt"`
}

func (p *EDMPacing) hasWindow() bool {
	return p.WindowStartHour != nil || p.WindowEndHour != nil || p.WindowTZ != ""
}

// validateEDMPacing 校验节奏参数；返回的错误由调用方映射为 ErrorInvalidArgument。
func validateEDMPacing(p *EDMPacing, now time.Time) error {
	if p == nil {
		return nil
	}
	if p.MinIntervalSec < 1 {
		return fmt.Errorf("pacing.minIntervalSec must be >= 1")
	}
	if p.MaxIntervalSec < p.MinIntervalSec {
		return fmt.Errorf("pacing.maxIntervalSec must be >= minIntervalSec")
	}
	if p.MaxIntervalSec > edmPacingMaxIntervalSec {
		return fmt.Errorf("pacing.maxIntervalSec must be <= %d", edmPacingMaxIntervalSec)
	}
	if p.StartAt != nil && p.StartAt.After(now.Add(edmPacingMaxStartAhead)) {
		return fmt.Errorf("pacing.startAt must be within 14 days")
	}
	if p.hasWindow() {
		if p.WindowStartHour == nil || p.WindowEndHour == nil || p.WindowTZ == "" {
			return fmt.Errorf("pacing.windowStartHour, windowEndHour and windowTZ must be given together")
		}
		s, e := *p.WindowStartHour, *p.WindowEndHour
		if s < 0 || s > 23 || e < 0 || e > 23 || s >= e {
			return fmt.Errorf("pacing window hours must be 0-23 with windowStartHour < windowEndHour")
		}
		if p.WindowTZ == "Local" {
			return fmt.Errorf("pacing.windowTZ must be an IANA time zone")
		}
		if _, err := time.LoadLocation(p.WindowTZ); err != nil {
			return fmt.Errorf("pacing.windowTZ invalid: %v", err)
		}
	}
	return nil
}

// rollIntoEDMWindow 把 t 顺延进每日窗口 [startH, endH)（loc 时区）；已在窗口内则原样返回。
func rollIntoEDMWindow(t time.Time, loc *time.Location, startH, endH int) time.Time {
	lt := t.In(loc)
	h := lt.Hour()
	if h >= startH && h < endH {
		return t
	}
	dayOffset := 0
	if h >= endH {
		dayOffset = 1
	}
	return time.Date(lt.Year(), lt.Month(), lt.Day()+dayOffset, startH, 0, 0, 0, loc)
}

// buildEDMPacingSchedule 计算 n 封信各自的发送时刻（单调不减）。randN(n) 返回 [0, n) 的均匀随机数，测试可注入。
// 参数须先过 validateEDMPacing。
func buildEDMPacingSchedule(p *EDMPacing, n int, now time.Time, randN func(int64) int64) ([]time.Time, error) {
	var loc *time.Location
	if p.hasWindow() {
		var err error
		if loc, err = time.LoadLocation(p.WindowTZ); err != nil {
			return nil, fmt.Errorf("pacing.windowTZ invalid: %v", err)
		}
	}
	place := func(t time.Time) time.Time {
		if loc == nil {
			return t
		}
		return rollIntoEDMWindow(t, loc, *p.WindowStartHour, *p.WindowEndHour)
	}

	t := now
	if p.StartAt != nil && p.StartAt.After(now) {
		t = *p.StartAt
	}
	minGap := time.Duration(p.MinIntervalSec) * time.Second
	spread := int64(p.MaxIntervalSec-p.MinIntervalSec)*int64(time.Second) + 1

	out := make([]time.Time, n)
	for i := 0; i < n; i++ {
		if i > 0 {
			t = t.Add(minGap + time.Duration(randN(spread)))
		}
		t = place(t)
		out[i] = t
	}
	return out, nil
}

// edmPacedTaskID 节奏发送单封任务的 asynq TaskID：可在 asynqmon 按批次识别/删除，同批次重复提交不会重复入队。
func edmPacedTaskID(batchID string, i int) string {
	return fmt.Sprintf("edm:%s:%d", batchID, i)
}

// EnqueuePacedTemplatedEmailTasks 把一批邮件拆成每封一个 edm:send-templated 任务按节奏排期入队。
// 中途 Redis 出错时返回已入队部分的结果和错误；用同一 batchId 重新提交时已入队的条目按 TaskID 冲突跳过。
func EnqueuePacedTemplatedEmailTasks(ctx context.Context, req *SendEmailsRequest, p *EDMPacing, now time.Time) (*EDMPacedSchedule, error) {
	if err := validateEDMPacing(p, now); err != nil {
		return nil, err
	}
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("items cannot be empty")
	}
	times, err := buildEDMPacingSchedule(p, len(req.Items), now, rand.Int64N)
	if err != nil {
		return nil, err
	}

	sched := &EDMPacedSchedule{BatchID: req.BatchID, FirstAt: times[0], LastAt: times[len(times)-1]}
	if loc, lerr := time.LoadLocation(p.WindowTZ); p.WindowTZ != "" && lerr == nil {
		sched.FirstAt, sched.LastAt = sched.FirstAt.In(loc), sched.LastAt.In(loc)
	}
	for i, item := range req.Items {
		payload := TemplatedEmailTaskPayload{Request: SendEmailsRequest{
			BatchID: req.BatchID,
			Brand:   req.Brand,
			Items:   []SendEmailItem{item},
		}}
		_, err := edmTaskEnqueue(TaskTypeTemplatedEmailSend, payload,
			asynq.ProcessAt(times[i]), asynq.TaskID(edmPacedTaskID(req.BatchID, i)))
		if errors.Is(err, hibikenAsynq.ErrTaskIDConflict) {
			sched.AlreadyScheduled++
			continue
		}
		if err != nil {
			log.Errorf(ctx, "[EMAIL-SEND] paced batch=%s enqueue failed at %d/%d: %v", req.BatchID, i, len(req.Items), err)
			return sched, fmt.Errorf("enqueue paced task %d/%d failed: %w", i, len(req.Items), err)
		}
		sched.Scheduled++
	}

	log.Infof(ctx, "[EMAIL-SEND] paced batch=%s scheduled=%d already=%d first=%s last=%s",
		req.BatchID, sched.Scheduled, sched.AlreadyScheduled,
		sched.FirstAt.Format(time.RFC3339), sched.LastAt.Format(time.RFC3339))
	return sched, nil
}

// describeEDMPacing 审批摘要用：节奏参数 + 按「立即执行」估算的首/末封时刻。
func describeEDMPacing(p *EDMPacing, n int, now time.Time) string {
	s := fmt.Sprintf("，节奏：每封间隔 %d-%ds", p.MinIntervalSec, p.MaxIntervalSec)
	if p.StartAt != nil {
		s += "，起始 " + p.StartAt.Format(time.RFC3339)
	}
	if p.hasWindow() {
		s += fmt.Sprintf("，每日窗口 %02d:00-%02d:00 %s", *p.WindowStartHour, *p.WindowEndHour, p.WindowTZ)
	}
	if times, err := buildEDMPacingSchedule(p, n, now, rand.Int64N); err == nil && n > 0 {
		first, last := times[0], times[n-1]
		if loc, lerr := time.LoadLocation(p.WindowTZ); p.WindowTZ != "" && lerr == nil {
			first, last = first.In(loc), last.In(loc)
		}
		s += fmt.Sprintf("，预计首封 %s、末封约 %s（按立即执行估算；审批晚于起始时间则从批准时刻起算）",
			first.Format(time.RFC3339), last.Format(time.RFC3339))
	}
	return s
}

// edmScheduleSinkKey 超管直通时，审批回调把排期结果写回 handler 用的 context 槽位（回调签名只能返回 error）。
type edmScheduleSinkKey struct{}

func withEDMScheduleSink(ctx context.Context, sink *EDMPacedSchedule) context.Context {
	return context.WithValue(ctx, edmScheduleSinkKey{}, sink)
}

func edmScheduleSinkFrom(ctx context.Context) *EDMPacedSchedule {
	sink, _ := ctx.Value(edmScheduleSinkKey{}).(*EDMPacedSchedule)
	return sink
}
