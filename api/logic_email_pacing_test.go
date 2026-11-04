package center

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/mail"
)

// enqueuedEDMTask 一次 edmTaskEnqueue 调用的快照。
type enqueuedEDMTask struct {
	taskType  string
	payload   TemplatedEmailTaskPayload
	processAt time.Time
	taskID    string
	nOpts     int
}

// captureEDMEnqueue 替换 edmTaskEnqueue：不碰任何 Redis，只记录调用。fail 非空时按调用序号返回错误。
func captureEDMEnqueue(t *testing.T, fail func(i int) error) *[]enqueuedEDMTask {
	t.Helper()
	var got []enqueuedEDMTask
	prev := edmTaskEnqueue
	edmTaskEnqueue = func(taskType string, payload any, opts ...hibikenAsynq.Option) (*hibikenAsynq.TaskInfo, error) {
		i := len(got)
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		rec := enqueuedEDMTask{taskType: taskType, nOpts: len(opts)}
		require.NoError(t, json.Unmarshal(raw, &rec.payload))
		for _, o := range opts {
			switch o.Type() {
			case hibikenAsynq.ProcessAtOpt:
				rec.processAt = o.Value().(time.Time)
			case hibikenAsynq.TaskIDOpt:
				rec.taskID = o.Value().(string)
			}
		}
		if fail != nil {
			if err := fail(i); err != nil {
				got = append(got, rec)
				return nil, err
			}
		}
		got = append(got, rec)
		return &hibikenAsynq.TaskInfo{ID: rec.taskID}, nil
	}
	t.Cleanup(func() { edmTaskEnqueue = prev })
	return &got
}

func intp(v int) *int { return &v }

func shanghai(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	return loc
}

// ---------- validation ----------

func TestEDMPacing_ValidateRejectsBadInput(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	far := now.Add(15 * 24 * time.Hour)
	near := now.Add(13 * 24 * time.Hour)
	past := now.Add(-48 * time.Hour)
	win := func(p EDMPacing) *EDMPacing {
		p.MinIntervalSec, p.MaxIntervalSec = 20, 60
		return &p
	}
	bad := map[string]*EDMPacing{
		"min 0":            {MinIntervalSec: 0, MaxIntervalSec: 10},
		"max < min":        {MinIntervalSec: 30, MaxIntervalSec: 20},
		"max > 3600":       {MinIntervalSec: 1, MaxIntervalSec: 3601},
		"startAt > 14d":    {MinIntervalSec: 1, MaxIntervalSec: 2, StartAt: &far},
		"window no tz":     win(EDMPacing{WindowStartHour: intp(20), WindowEndHour: intp(22)}),
		"window only tz":   win(EDMPacing{WindowTZ: "Asia/Shanghai"}),
		"window start=end": win(EDMPacing{WindowStartHour: intp(20), WindowEndHour: intp(20), WindowTZ: "Asia/Shanghai"}),
		"window start>end": win(EDMPacing{WindowStartHour: intp(22), WindowEndHour: intp(20), WindowTZ: "Asia/Shanghai"}),
		"window hour 24":   win(EDMPacing{WindowStartHour: intp(20), WindowEndHour: intp(24), WindowTZ: "Asia/Shanghai"}),
		"window hour <0":   win(EDMPacing{WindowStartHour: intp(-1), WindowEndHour: intp(2), WindowTZ: "Asia/Shanghai"}),
		"bad tz":           win(EDMPacing{WindowStartHour: intp(20), WindowEndHour: intp(22), WindowTZ: "Mars/Olympus"}),
		"Local tz":         win(EDMPacing{WindowStartHour: intp(20), WindowEndHour: intp(22), WindowTZ: "Local"}),
	}
	for name, p := range bad {
		assert.Error(t, validateEDMPacing(p, now), name)
	}
	good := map[string]*EDMPacing{
		"nil":             nil,
		"min=max":         {MinIntervalSec: 5, MaxIntervalSec: 5},
		"max 3600":        {MinIntervalSec: 1, MaxIntervalSec: 3600},
		"startAt 13d":     {MinIntervalSec: 1, MaxIntervalSec: 2, StartAt: &near},
		"startAt past":    {MinIntervalSec: 1, MaxIntervalSec: 2, StartAt: &past},
		"window 0-23 UTC": win(EDMPacing{WindowStartHour: intp(0), WindowEndHour: intp(23), WindowTZ: "UTC"}),
		"window shanghai": win(EDMPacing{WindowStartHour: intp(20), WindowEndHour: intp(22), WindowTZ: "Asia/Shanghai"}),
	}
	for name, p := range good {
		assert.NoError(t, validateEDMPacing(p, now), name)
	}
}

func TestAdminEDMSend_InvalidPacingRejected(t *testing.T) {
	testInitConfig()
	captured := captureEDMEnqueue(t, nil)
	for name, pacing := range map[string]any{
		"max<min":     map[string]any{"minIntervalSec": 60, "maxIntervalSec": 20},
		"bad startAt": map[string]any{"minIntervalSec": 20, "maxIntervalSec": 60, "startAt": "tomorrow 8pm"},
		"half window": map[string]any{"minIntervalSec": 20, "maxIntervalSec": 60, "windowStartHour": 20},
	} {
		body, _ := json.Marshal(map[string]any{
			"batchId": "t-edm-pacing-bad", "brand": "kaitu",
			"items":  []map[string]any{{"email": "a@test.local", "slug": "x"}},
			"pacing": pacing,
		})
		c, w := brandTestGinContext(t, http.MethodPost, "/app/edm/send", "kaitu.io", body)
		api_admin_send_templated_emails(c)
		assert.Equal(t, ErrorInvalidArgument, decodeResponseCode(t, w), name)
	}
	assert.Empty(t, *captured)
}

// ---------- schedule math ----------

func TestEDMPacingSchedule_MonotonicGapsWithinBounds(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	start := now.Add(2 * time.Hour)
	p := &EDMPacing{MinIntervalSec: 20, MaxIntervalSec: 60, StartAt: &start}
	times, err := buildEDMPacingSchedule(p, 2000, now, rand.Int64N)
	require.NoError(t, err)
	require.Len(t, times, 2000)
	assert.True(t, times[0].Equal(start), "first send at startAt")
	var sawLow, sawHigh bool
	for i := 1; i < len(times); i++ {
		gap := times[i].Sub(times[i-1])
		require.GreaterOrEqual(t, gap, 20*time.Second, "gap %d", i)
		require.LessOrEqual(t, gap, 60*time.Second, "gap %d", i)
		sawLow = sawLow || gap < 30*time.Second
		sawHigh = sawHigh || gap > 50*time.Second
	}
	assert.True(t, sawLow && sawHigh, "gaps must actually spread across [min,max]")

	// 边界：randN 恒取两端
	lo, _ := buildEDMPacingSchedule(p, 3, now, func(int64) int64 { return 0 })
	assert.Equal(t, 20*time.Second, lo[1].Sub(lo[0]))
	hi, _ := buildEDMPacingSchedule(p, 3, now, func(n int64) int64 { return n - 1 })
	assert.Equal(t, 60*time.Second, hi[1].Sub(hi[0]))
}

func TestEDMPacingSchedule_StartAtPastOrAbsentStartsNow(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	past := now.Add(-3 * 24 * time.Hour)
	for _, p := range []*EDMPacing{
		{MinIntervalSec: 1, MaxIntervalSec: 1, StartAt: &past},
		{MinIntervalSec: 1, MaxIntervalSec: 1},
	} {
		times, err := buildEDMPacingSchedule(p, 2, now, rand.Int64N)
		require.NoError(t, err)
		assert.True(t, times[0].Equal(now), "got %s", times[0])
		assert.True(t, times[1].Equal(now.Add(time.Second)))
	}
}

func TestEDMPacingSchedule_WindowRollsForwardAcrossDay(t *testing.T) {
	loc := shanghai(t)
	now := time.Date(2026, 11, 4, 1, 0, 0, 0, time.UTC) // 北京 09:00
	window := func(start time.Time) *EDMPacing {
		return &EDMPacing{MinIntervalSec: 60, MaxIntervalSec: 60, StartAt: &start,
			WindowStartHour: intp(20), WindowEndHour: intp(22), WindowTZ: "Asia/Shanghai"}
	}
	fixed := func(int64) int64 { return 0 }

	// 21:59:30 起：第二封 22:00:30 落在窗口外 → 次日 20:00，之后从那里继续 +60s
	start := time.Date(2026, 11, 6, 21, 59, 30, 0, loc)
	times, err := buildEDMPacingSchedule(window(start), 3, now, fixed)
	require.NoError(t, err)
	assert.True(t, times[0].Equal(start))
	assert.True(t, times[1].Equal(time.Date(2026, 11, 7, 20, 0, 0, 0, loc)), "got %s", times[1].In(loc))
	assert.True(t, times[2].Equal(time.Date(2026, 11, 7, 20, 1, 0, 0, loc)), "got %s", times[2].In(loc))

	// 起点在窗口之前（同日 09:00）→ 当天 20:00；起点在窗口之后（23:30）→ 次日 20:00
	before, _ := buildEDMPacingSchedule(window(time.Date(2026, 11, 6, 9, 0, 0, 0, loc)), 1, now, fixed)
	assert.True(t, before[0].Equal(time.Date(2026, 11, 6, 20, 0, 0, 0, loc)), "got %s", before[0].In(loc))
	after, _ := buildEDMPacingSchedule(window(time.Date(2026, 11, 6, 23, 30, 0, 0, loc)), 1, now, fixed)
	assert.True(t, after[0].Equal(time.Date(2026, 11, 7, 20, 0, 0, 0, loc)), "got %s", after[0].In(loc))

	// 2000 封、20-60s、每晚 2 小时：全部落在窗口内，单调，铺满多个晚上
	p := window(time.Date(2026, 11, 6, 20, 0, 0, 0, loc))
	p.MinIntervalSec, p.MaxIntervalSec = 20, 60
	all, err := buildEDMPacingSchedule(p, 2000, now, rand.Int64N)
	require.NoError(t, err)
	days := map[string]bool{}
	for i, ts := range all {
		lt := ts.In(loc)
		require.True(t, lt.Hour() >= 20 && lt.Hour() < 22, "item %d at %s outside window", i, lt)
		if i > 0 {
			require.False(t, ts.Before(all[i-1]), "item %d not monotonic", i)
		}
		days[lt.Format("2006-01-02")] = true
	}
	assert.GreaterOrEqual(t, len(days), 5, "2000 × ~40s ≈ 22h must span several evenings")
}

// ---------- enqueue paths ----------

const pacingTestParamsNoPacing = `{"batchId":"t-edm-nopace","brand":"kaitu","items":[{"email":"a@x","userId":7,"slug":"s","vars":{"K":"V"}},{"email":"b@x","slug":"s"}]}`

// 不带 pacing：仍是整批一个任务、无任何选项、payload 与老实现一致。
func TestEDMSendCallback_NoPacing_SingleBatchTaskUnchanged(t *testing.T) {
	captured := captureEDMEnqueue(t, nil)
	require.NoError(t, executeApprovalEDMSend(context.Background(), json.RawMessage(pacingTestParamsNoPacing)))
	require.Len(t, *captured, 1)
	got := (*captured)[0]
	assert.Equal(t, TaskTypeTemplatedEmailSend, got.taskType)
	assert.Equal(t, 0, got.nOpts, "old path passes no ProcessAt/TaskID")

	var req SendTemplatedEmailsHTTPRequest
	require.NoError(t, json.Unmarshal([]byte(pacingTestParamsNoPacing), &req))
	assert.Equal(t, TemplatedEmailTaskPayload{Request: *edmSendRequestFromHTTP(&req)}, got.payload)
	assert.Len(t, got.payload.Request.Items, 2)
}

func TestEDMSendCallback_Pacing_OneTaskPerItemScheduled(t *testing.T) {
	captured := captureEDMEnqueue(t, nil)
	past := time.Now().Add(-6 * time.Hour).UTC().Format(time.RFC3339) // 审批拖过了 startAt
	params := fmt.Sprintf(`{"batchId":"t-edm-pace","brand":"kaitu","items":[
		{"email":"a@x","userId":1,"slug":"s"},{"email":"b@x","userId":2,"slug":"s"},{"email":"c@x","userId":3,"slug":"s"}],
		"pacing":{"minIntervalSec":20,"maxIntervalSec":60,"startAt":%q}}`, past)

	sink := &EDMPacedSchedule{}
	before := time.Now()
	require.NoError(t, executeApprovalEDMSend(withEDMScheduleSink(context.Background(), sink), json.RawMessage(params)))
	require.Len(t, *captured, 3)
	for i, task := range *captured {
		assert.Equal(t, TaskTypeTemplatedEmailSend, task.taskType)
		assert.Equal(t, fmt.Sprintf("edm:t-edm-pace:%d", i), task.taskID)
		require.Len(t, task.payload.Request.Items, 1)
		assert.Equal(t, uint64(i+1), task.payload.Request.Items[0].UserID)
		assert.Equal(t, "t-edm-pace", task.payload.Request.BatchID)
		assert.Equal(t, "kaitu", task.payload.Request.Brand)
		if i > 0 {
			gap := task.processAt.Sub((*captured)[i-1].processAt)
			assert.True(t, gap >= 20*time.Second && gap <= 60*time.Second, "gap %s", gap)
		}
	}
	first := (*captured)[0].processAt
	assert.False(t, first.Before(before), "past startAt starts now, not in the past")
	assert.WithinDuration(t, before, first, 5*time.Second)

	assert.Equal(t, 3, sink.Scheduled)
	assert.True(t, sink.FirstAt.Equal(first))
	assert.True(t, sink.LastAt.Equal((*captured)[2].processAt))
}

// 同批次重复提交：TaskID 冲突的条目算 alreadyScheduled，不报错。
func TestEDMPacedEnqueue_TaskIDConflictCountsAsAlreadyScheduled(t *testing.T) {
	captureEDMEnqueue(t, func(i int) error {
		if i == 1 {
			return hibikenAsynq.ErrTaskIDConflict
		}
		return nil
	})
	req := &SendEmailsRequest{BatchID: "t-edm-dup", Items: []SendEmailItem{{Email: "a"}, {Email: "b"}, {Email: "c"}}}
	sched, err := EnqueuePacedTemplatedEmailTasks(context.Background(), req, &EDMPacing{MinIntervalSec: 1, MaxIntervalSec: 2}, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 2, sched.Scheduled)
	assert.Equal(t, 1, sched.AlreadyScheduled)
}

// 超管直通：响应 data 带排期结果（任务数 + 首末封时刻，按窗口时区）。
func TestAdminEDMSend_SuperAdminPacedReportsSchedule(t *testing.T) {
	skipIfNoConfig(t)
	captured := captureEDMEnqueue(t, nil)
	admin, _ := seedBrandUser(t, BrandKaitu)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(injectSuperAdmin(admin.ID, admin.UUID))
	r.POST("/app/edm/send", api_admin_send_templated_emails)
	RegisterApprovalCallback("edm_send", executeApprovalEDMSend)
	t.Cleanup(func() {
		db.Get().Where("action = ? AND requestor_id = ?", "edm_send", admin.ID).Delete(&AdminApproval{})
	})

	resp := doJSON(t, r, "POST", "/app/edm/send", map[string]any{
		"batchId": "t-edm-pace-sa-" + generateId("b"), "brand": "kaitu",
		"items": []map[string]any{{"email": "a@test.local", "userId": 1, "slug": "s"}, {"email": "b@test.local", "userId": 2, "slug": "s"}},
		"pacing": map[string]any{"minIntervalSec": 20, "maxIntervalSec": 60,
			"windowStartHour": 20, "windowEndHour": 22, "windowTZ": "Asia/Shanghai"},
	})
	require.Equal(t, 0, resp.Code)
	var got EDMPacedSchedule
	require.NoError(t, json.Unmarshal(resp.Data, &got))
	assert.Equal(t, 2, got.Scheduled)
	require.Len(t, *captured, 2)
	assert.True(t, got.FirstAt.Equal((*captured)[0].processAt))
	assert.True(t, got.LastAt.Equal((*captured)[1].processAt))
	_, off := got.FirstAt.Zone()
	assert.Equal(t, 8*3600, off, "times reported in the window TZ")
	assert.Contains(t, []int{20, 21}, got.FirstAt.Hour())

	var ap AdminApproval
	require.NoError(t, db.Get().Where("action = ? AND requestor_id = ?", "edm_send", admin.ID).Last(&ap).Error)
	assert.Contains(t, ap.Summary, "节奏")
	assert.Contains(t, ap.Params, `"pacing"`, "pacing persists in approval params for deferred approval")
}

// ---------- per-item task execution ----------

// 每个排期任务只发一封、经 sendSingleTemplatedEmail（退订跳过 + 日志），任务重试不重发。
func TestEDMPacedTask_SendsExactlyOneAndDedupesOnRetry(t *testing.T) {
	skipIfNoConfig(t)
	prevDev := viper.Get("mail.dev_mode")
	viper.Set("mail.dev_mode", false) // 走到 edmChannelSend 接缝；接缝已替换，不会真发
	t.Cleanup(func() { viper.Set("mail.dev_mode", prevDev) })
	var sent []*mail.Message
	prevSend := edmChannelSend
	edmChannelSend = func(_ *mail.Sender, msg *mail.Message) error { sent = append(sent, msg); return nil }
	t.Cleanup(func() { edmChannelSend = prevSend })

	slug := "edm-pace-test-" + generateId("s")
	tp := &EmailMarketingTemplate{Name: "edm-pace-test", Slug: slug, Language: "en-US", Subject: "hi",
		Content: "hello {{.UnsubscribeURL}}", IsActive: BoolPtr(true), Brand: string(BrandKaitu)}
	require.NoError(t, db.Get().Create(tp).Error)
	t.Cleanup(func() {
		db.Get().Where("template_id = ?", tp.ID).Delete(&EmailSendLog{})
		db.Get().Unscoped().Delete(tp)
	})
	a := createBrandUserWithEmail(t, BrandKaitu, "")
	b := createBrandUserWithEmail(t, BrandKaitu, "")
	optedOut := createBrandUserWithEmail(t, BrandKaitu, "")
	for _, u := range []*User{a, b, optedOut} {
		require.NoError(t, db.Get().Model(u).Update("language", "en-US").Error)
	}
	require.NoError(t, optOutEDM(context.Background(), optedOut.ID, time.Now()))

	batch := newEDMTestBatch(t)
	captured := captureEDMEnqueue(t, nil)
	req := &SendEmailsRequest{BatchID: batch, Brand: string(BrandKaitu), Items: []SendEmailItem{
		{Email: "pace-a@test.local", UserID: a.ID, Slug: slug},
		{Email: "pace-b@test.local", UserID: b.ID, Slug: slug},
		{Email: "pace-o@test.local", UserID: optedOut.ID, Slug: slug},
	}}
	_, err := EnqueuePacedTemplatedEmailTasks(context.Background(), req, &EDMPacing{MinIntervalSec: 1, MaxIntervalSec: 1}, time.Now())
	require.NoError(t, err)
	require.Len(t, *captured, 3)

	run := func(task enqueuedEDMTask) {
		raw, err := json.Marshal(task.payload)
		require.NoError(t, err)
		require.NoError(t, handleTemplatedEmailTask(context.Background(), raw))
	}

	run((*captured)[0])
	require.Len(t, sent, 1, "one task = one email")
	assert.Equal(t, "pace-a@test.local", sent[0].To)
	assert.Contains(t, sent[0].Body, edmUnsubscribeURL(a))

	run((*captured)[0]) // asynq 重试同一任务
	assert.Len(t, sent, 1, "retry must dedupe on the send log")

	run((*captured)[1])
	run((*captured)[2])
	require.Len(t, sent, 2, "opted-out recipient is skipped")
	assert.Equal(t, "pace-b@test.local", sent[1].To)

	var logs []EmailSendLog
	require.NoError(t, db.Get().Where("batch_id = ?", batch).Find(&logs).Error)
	byUser := map[uint64][]EmailSendLog{}
	for _, l := range logs {
		byUser[l.UserID] = append(byUser[l.UserID], l)
	}
	require.Len(t, byUser[a.ID], 1)
	assert.Equal(t, EmailSendLogStatusSent, byUser[a.ID][0].Status)
	require.Len(t, byUser[b.ID], 1)
	require.Len(t, byUser[optedOut.ID], 1)
	assert.Equal(t, EmailSendLogStatusSkipped, byUser[optedOut.ID][0].Status)
}
