package center

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/redis"
)

// chatIdle 把会话的 last_message_at 挪到 age 之前。
func chatIdle(t *testing.T, convID uint64, handler string, age time.Duration) {
	t.Helper()
	require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", convID).
		Updates(map[string]any{"handler": handler, "last_message_at": time.Now().Add(-age)}).Error)
}

func TestChatWorker_AutoCloseAppendsEventAndArchives(t *testing.T) {
	brand := slackTestBrand()
	idle := slackConv(t, brand, "/support")
	active := slackConv(t, brand, "/support")
	humanIdle := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	idle = slackSetup(t, f, idle)
	chatIdle(t, idle.ID, HandlerAI, 25*time.Hour)
	chatIdle(t, active.ID, HandlerAI, time.Hour)
	chatIdle(t, humanIdle.ID, HandlerHuman, 25*time.Hour) // 人工处理中的会话 72 小时才关
	ctx := context.Background()

	n, err := chatCloseIdleRun(ctx, string(brand))
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	fresh := slackReload(t, idle.ID)
	assert.Equal(t, ConvClosed, fresh.Status)
	assert.NotNil(t, fresh.SlackArchivedAt)
	assert.Equal(t, ConvOpen, slackReload(t, active.ID).Status)
	assert.Equal(t, ConvOpen, slackReload(t, humanIdle.ID).Status)

	msgs := convMessages(t, idle.ID)
	last := msgs[len(msgs)-1]
	assert.Equal(t, SenderSystem, last.SenderType)
	assert.Equal(t, MsgEvent, last.Kind)
	assert.Equal(t, "会话因长时间无消息自动关闭", last.Content)
	assert.JSONEq(t, `{"event":"auto_closed"}`, last.Meta)
	assert.Equal(t, 1, countBy(msgs, SenderSystem, MsgEvent), "exactly one close event")

	// 关闭事件先进频道，再归档；归档只发生一次
	methods := f.Methods()
	assert.Equal(t, "conversations.archive", methods[len(methods)-1])
	assert.Equal(t, 1, strings.Count(strings.Join(methods, ","), "conversations.archive"))
	assert.Contains(t, strings.Join(slackMsgPosts(f, idle.SlackChannelID), "\n"), "会话因长时间无消息自动关闭")
	for _, c := range f.Calls() {
		if c.Method == "conversations.archive" {
			assert.Equal(t, idle.SlackChannelID, c.Str("channel"))
		}
	}

	// 再跑一轮：没有新的可关会话，不再追加事件
	n, err = chatCloseIdleRun(ctx, string(brand))
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Len(t, convMessages(t, idle.ID), len(msgs))
}

// Slack 镜像未开启（未配总览频道）时，自动关闭照常落事件，不报错。
func TestChatWorker_AutoCloseWithoutSlack(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	chatIdle(t, conv.ID, HandlerHuman, 73*time.Hour)

	n, err := chatCloseIdleRun(context.Background(), string(brand))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, ConvClosed, slackReload(t, conv.ID).Status)
	msgs := convMessages(t, conv.ID)
	require.Len(t, msgs, 1)
	assert.JSONEq(t, `{"event":"auto_closed"}`, msgs[0].Meta)
}

// 归档失败不算任务失败：会话已关、事件已落，归档由 sweep 第二遍补。
func TestChatWorker_AutoCloseArchiveFailureLeftToSweep(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	conv = slackSetup(t, f, conv)
	chatIdle(t, conv.ID, HandlerAI, 25*time.Hour)
	f.Script("conversations.archive", fakeSlackHTTP(500))
	ctx := context.Background()

	n, err := chatCloseIdleRun(ctx, string(brand))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	fresh := slackReload(t, conv.ID)
	assert.Equal(t, ConvClosed, fresh.Status)
	assert.Nil(t, fresh.SlackArchivedAt)

	// 周期 sweep 补上归档（sweep 只碰 30 秒前关闭的会话）
	slackClose(t, conv.ID, time.Minute)
	n, err = chatSlackSweepRun(ctx, string(brand))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.NotNil(t, slackReload(t, conv.ID).SlackArchivedAt)
}

// 周期 sweep 补发实时路径漏掉的消息。
func TestChatWorker_SweepMirrorsMissedMessages(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	conv = slackSetup(t, f, conv)
	slackSeed(t, conv, SenderVisitor, MsgText, "漏发的消息")
	slackBackdate(t, conv.ID, time.Minute)
	require.EqualValues(t, 1, slackUnmirrored(t, conv.ID))

	n, err := chatSlackSweepRun(context.Background(), string(brand))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.EqualValues(t, 0, slackUnmirrored(t, conv.ID))
	assert.Contains(t, strings.Join(slackMsgPosts(f, conv.SlackChannelID), "\n"), "漏发的消息")
}

// 两个定时任务都注册了处理函数与 cron：chatSlackSweep 此前没有任何生产调用方。
func TestChatWorker_Registered(t *testing.T) {
	specs := map[string]string{}
	for _, c := range chatCronSpecs {
		specs[c.TaskType] = c.Spec
		assert.NotNil(t, c.Handler, c.TaskType)
		assert.Greater(t, c.Unique, time.Duration(0), c.TaskType)
	}
	assert.Equal(t, map[string]string{
		TaskTypeChatSlackSweep: "* * * * *",
		TaskTypeChatCloseIdle:  "*/10 * * * *",
	}, specs)
	assert.Equal(t, 24*time.Hour, chatIdleCloseAI)
	assert.Equal(t, 72*time.Hour, chatIdleCloseHuman)
}

// goFuncCalls 返回源文件里某个函数体内调用过的函数名（"pkg.Fn" 或 "Fn"）。
func goFuncCalls(t *testing.T, file, fn string) map[string]bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	require.NoError(t, err)
	calls := map[string]bool{}
	found := false
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name.Name != fn {
			continue
		}
		found = true
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				switch x := c.Fun.(type) {
				case *ast.Ident:
					calls[x.Name] = true
				case *ast.SelectorExpr:
					if pkg, ok := x.X.(*ast.Ident); ok {
						calls[pkg.Name+"."+x.Sel.Name] = true
					}
				}
			}
			return true
		})
	}
	require.True(t, found, "func %s not found in %s —— 守卫本身失效了", fn, file)
	return calls
}

// 接线守卫：qtoolkit/asynq 的 handlers / cronTasks 注册表是未导出的，读不到，所以做源码级断言——
// InitWorker 必须调用 RegisterChatWorker，后者必须对表里每一项既 Handle 又 Cron。
// 删掉 worker_integration.go 里那一行，chatSlackSweep 就又没有生产调用方了，而别的测试全绿。
func TestChatWorker_WiredIntoInitWorker(t *testing.T) {
	assert.True(t, goFuncCalls(t, "worker_integration.go", "InitWorker")["RegisterChatWorker"],
		"InitWorker must call RegisterChatWorker()")
	reg := goFuncCalls(t, "worker_chat.go", "RegisterChatWorker")
	assert.True(t, reg["asynq.Handle"], "RegisterChatWorker must register handlers")
	assert.True(t, reg["asynq.Cron"], "RegisterChatWorker must register cron specs")
	assert.True(t, goFuncCalls(t, "worker_chat.go", "handleChatSlackSweep")["chatSlackSweepRun"])
	assert.True(t, goFuncCalls(t, "worker_chat.go", "handleChatCloseIdle")["chatCloseIdleRun"])
}

func closeEvents(t *testing.T, convID uint64) []ConversationMessage {
	t.Helper()
	var out []ConversationMessage
	for _, m := range convMessages(t, convID) {
		if m.Kind == MsgEvent && (m.Meta == chatEventMeta(ChatEventClosed) || m.Meta == chatEventMeta(ChatEventAutoClosed)) {
			out = append(out, m)
		}
	}
	return out
}

// 上一轮关了会话却没记成事件（追加失败 / 进程中途被杀）：下一轮补记并归档。
func TestChatWorker_BackfillsMissingAutoCloseEvent(t *testing.T) {
	brand := slackTestBrand()
	lost := slackConv(t, brand, "/support")   // 关了、没事件：要补
	manual := slackConv(t, brand, "/support") // 人工关闭、已有 closed 事件：不碰
	old := slackConv(t, brand, "/support")    // 关了、没事件，但早于时间下界：不碰
	stillOpen := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	lost = slackSetup(t, f, lost)
	// 真实前提：上一轮 close-idle 关了会话却没记成事件，离下一轮（每 10 分钟）还有几分钟；
	// 其间每分钟的 sweep 已经跑过——没有关闭事件，它不归档。
	slackClose(t, lost.ID, 5*time.Minute)
	slackSeed(t, manual, SenderSystem, MsgEvent, "会话已关闭", func(m *ConversationMessage) { m.Meta = chatEventMeta(ChatEventClosed) })
	slackClose(t, manual.ID, 5*time.Minute)
	slackClose(t, old.ID, 25*time.Hour)
	ctx := context.Background()

	n, err := chatSlackSweepRun(ctx, string(brand))
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	require.Empty(t, f.CallsOf("conversations.archive"), "sweep must not archive before the close event exists")
	require.Nil(t, slackReload(t, lost.ID).SlackArchivedAt)

	n, err = chatCloseIdleRun(ctx, string(brand))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	methods := f.Methods()
	assert.Equal(t, "conversations.archive", methods[len(methods)-1], "archive comes after the event is posted")
	assert.Len(t, f.CallsOf("conversations.archive"), 1)

	ev := closeEvents(t, lost.ID)
	require.Len(t, ev, 1)
	assert.Equal(t, chatEventMeta(ChatEventAutoClosed), ev[0].Meta)
	assert.Equal(t, chatAutoCloseNotice, ev[0].Content)
	assert.NotNil(t, slackReload(t, lost.ID).SlackArchivedAt)
	assert.Contains(t, strings.Join(slackMsgPosts(f, lost.SlackChannelID), "\n"), chatAutoCloseNotice)

	manualEv := closeEvents(t, manual.ID)
	require.Len(t, manualEv, 1, "manual close keeps its own event only")
	assert.Equal(t, chatEventMeta(ChatEventClosed), manualEv[0].Meta)
	assert.Empty(t, closeEvents(t, old.ID), "older than the backfill window")
	assert.Empty(t, closeEvents(t, stillOpen.ID))

	// 幂等：再跑不重复
	n, err = chatCloseIdleRun(ctx, string(brand))
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Len(t, closeEvents(t, lost.ID), 1)
}

// 多实例同时补记：只落一条事件。
func TestChatWorker_BackfillConcurrentRecordsOnce(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	slackClose(t, conv.ID, 20*time.Minute)
	ctx := context.Background()

	const workers = 6
	var wg sync.WaitGroup
	total := make(chan int, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := chatAutoCloseRecord(ctx, string(brand))
			assert.NoError(t, err)
			total <- n
		}()
	}
	wg.Wait()
	close(total)
	sum := 0
	for n := range total {
		sum += n
	}
	assert.Equal(t, 1, sum)
	assert.Len(t, closeEvents(t, conv.ID), 1)
}

// 持锁后复查：列表查询与拿锁之间别的实例已经记过事件，则不再记。
func TestChatWorker_BackfillRechecksUnderLock(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	slackClose(t, conv.ID, 20*time.Minute)
	slackSeed(t, conv, SenderSystem, MsgEvent, chatAutoCloseNotice, func(m *ConversationMessage) { m.Meta = chatEventMeta(ChatEventAutoClosed) })

	recorded, err := chatAutoCloseRecordOne(context.Background(), slackReload(t, conv.ID))
	require.NoError(t, err)
	assert.False(t, recorded)
	assert.Len(t, closeEvents(t, conv.ID), 1)

	// 别的实例持锁中：直接让路
	bare := slackConv(t, brand, "/support")
	slackClose(t, bare.ID, 20*time.Minute)
	key := "chat:autoclose:lock:" + uintStr(bare.ID)
	require.NoError(t, redis.Client().Set(context.Background(), key, "other", time.Minute).Err())
	t.Cleanup(func() { redis.Client().Del(context.Background(), key) })
	recorded, err = chatAutoCloseRecordOne(context.Background(), slackReload(t, bare.ID))
	require.NoError(t, err)
	assert.False(t, recorded)
	assert.Empty(t, closeEvents(t, bare.ID))
	held, err := testMiniRedis.Get(key)
	require.NoError(t, err)
	assert.Equal(t, "other", held, "must not release a lock it does not hold")
}

// 镜像锁被占（生产里就是关闭事件自己的追加钩子在发）：归档走 errChatSlackBusy，留给 sweep。
func TestChatWorker_AutoCloseWhileMirrorLockHeld(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	conv = slackSetup(t, f, conv)
	chatIdle(t, conv.ID, HandlerAI, 25*time.Hour)
	ctx := context.Background()
	lockKey := "chat:slack:lock:" + uintStr(conv.ID)
	require.NoError(t, redis.Client().Set(ctx, lockKey, "held-by-other", time.Minute).Err())
	t.Cleanup(func() { redis.Client().Del(context.Background(), lockKey) })

	n, err := chatCloseIdleRun(ctx, string(brand))
	require.NoError(t, err, "a busy mirror lock is not a task failure")
	assert.Equal(t, 1, n)
	fresh := slackReload(t, conv.ID)
	assert.Equal(t, ConvClosed, fresh.Status)
	assert.Nil(t, fresh.SlackArchivedAt)
	assert.Len(t, closeEvents(t, conv.ID), 1)
	assert.EqualValues(t, 1, slackUnmirrored(t, conv.ID), "event is waiting for the lock holder / sweep")
	assert.NotContains(t, f.Methods(), "conversations.archive")
	assert.ErrorIs(t, chatSlackArchive(ctx, fresh), errChatSlackBusy)

	// 锁放开后 sweep 补：先发事件，再归档
	require.NoError(t, redis.Client().Del(ctx, lockKey).Err())
	slackBackdate(t, conv.ID, time.Minute)
	slackClose(t, conv.ID, time.Minute)
	n, err = chatSlackSweepRun(ctx, string(brand))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.NotNil(t, slackReload(t, conv.ID).SlackArchivedAt)
	assert.Contains(t, strings.Join(slackMsgPosts(f, conv.SlackChannelID), "\n"), chatAutoCloseNotice)
	methods := f.Methods()
	assert.Equal(t, "conversations.archive", methods[len(methods)-1])
	assert.Len(t, closeEvents(t, conv.ID), 1)
}

// sweep 自己的列表查询失败带哨兵（记 Error）；单个会话的 Slack 失败不带（记 Warn）。两者都不交给 asynq 重试。
func TestChatWorker_SweepQueryFailureClassified(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	conv = slackSetup(t, f, conv)

	dead, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := chatSlackSweepIn(dead, string(brand))
	require.Error(t, err)
	assert.True(t, chatSweepQueryFailed(err), "err=%v", err)
	n, err := chatSlackSweepRun(dead, string(brand))
	assert.NoError(t, err, "never handed to asynq retry")
	assert.Equal(t, 0, n)

	slackSeed(t, conv, SenderVisitor, MsgText, "发不出去")
	slackBackdate(t, conv.ID, time.Minute)
	f.FailAll(500)
	_, err = chatSlackSweepIn(context.Background(), string(brand))
	require.Error(t, err)
	assert.False(t, chatSweepQueryFailed(err), "per-conversation Slack failure is not a query failure: %v", err)
}

// 产品硬要求：每个状态变化都进 Slack 频道。真实顺序——会话已关闭、关闭事件还没记（进程在两步之间被杀，
// 或一批里排在后面），每分钟的 sweep 先跑：它不得归档，否则补记的事件发进已归档频道会被静默丢弃。
// close-idle 补记后事件进频道；归档由随后的 sweep 完成，且是最后一次调用。
func TestChatWorker_SweepWaitsForCloseEventBeforeArchive(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	conv = slackSetup(t, f, conv)
	slackBackdate(t, conv.ID, 2*time.Minute)
	slackClose(t, conv.ID, time.Minute) // 已过 sweep 的 30 秒门槛，没有关闭事件
	ctx := context.Background()

	for i := 0; i < 2; i++ { // sweep 跑几轮都不归档
		n, err := chatSlackSweepRun(ctx, string(brand))
		require.NoError(t, err)
		assert.Equal(t, 0, n)
	}
	require.Empty(t, f.CallsOf("conversations.archive"))
	require.Nil(t, slackReload(t, conv.ID).SlackArchivedAt)

	// close-idle 补记事件；它自己那次归档失败，留给 sweep
	f.Script("conversations.archive", fakeSlackHTTP(500))
	n, err := chatCloseIdleRun(ctx, string(brand))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Contains(t, strings.Join(slackMsgPosts(f, conv.SlackChannelID), "\n"), chatAutoCloseNotice)
	assert.EqualValues(t, 0, slackUnmirrored(t, conv.ID))
	require.Nil(t, slackReload(t, conv.ID).SlackArchivedAt)

	// 事件已在频道里：sweep 现在归档，且归档是最后一次调用
	n, err = chatSlackSweepRun(ctx, string(brand))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.NotNil(t, slackReload(t, conv.ID).SlackArchivedAt)
	calls := f.Calls()
	require.NotEmpty(t, calls)
	assert.Equal(t, "conversations.archive", calls[len(calls)-1].Method)
	eventAt, archivedAt := -1, -1
	for i, c := range calls {
		if c.Method == "chat.postMessage" && strings.Contains(c.Str("text"), chatAutoCloseNotice) {
			eventAt = i
		}
		if c.Method == "conversations.archive" {
			archivedAt = i // 取最后一次（成功的那次）
		}
	}
	require.GreaterOrEqual(t, eventAt, 0, "close event must reach the channel")
	assert.Less(t, eventAt, archivedAt, "event is posted before the channel is archived")
	assert.Len(t, closeEvents(t, conv.ID), 1)
}

// ---- 终审修复：失败可见性 ----

func slackStuckCleanup(t *testing.T, convIDs ...uint64) {
	t.Helper()
	t.Cleanup(func() {
		for _, id := range convIDs {
			redis.Client().Del(context.Background(), chatSlackStuckKeyPrefix+uintStr(id))
		}
	})
}

func slackStuckAlerts(f *fakeSlack) []string {
	var out []string
	for _, p := range f.Posts(fakeSlackLobbyID) {
		if strings.Contains(p, "仍未同步到 Slack") {
			out = append(out, p)
		}
	}
	return out
}

// 超过 10 分钟仍未镜像的消息：sweep 汇总报出来，并往总览频道发一行告警；同一会话 30 分钟内只告警一次。
func TestChatWorker_SweepAlertsOnStuckMessages(t *testing.T) {
	brand := slackTestBrand()
	stuck := slackConv(t, brand, "/support")
	recent := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	slackStuckCleanup(t, stuck.ID, recent.ID)
	slackSeed(t, stuck, SenderVisitor, MsgText, "卡住的第一条")
	slackSeed(t, stuck, SenderVisitor, MsgText, "卡住的第二条")
	slackBackdate(t, stuck.ID, 11*time.Minute)
	slackSeed(t, recent, SenderVisitor, MsgText, "才 5 分钟")
	slackBackdate(t, recent.ID, 5*time.Minute)
	// 建频道一直失败：两个会话的消息都发不出去
	for i := 0; i < 20; i++ {
		f.Script("conversations.create", fakeSlackErr("boom"))
	}
	ctx := context.Background()

	_, err := chatSlackSweepRun(ctx, string(brand))
	require.NoError(t, err)
	require.EqualValues(t, 2, slackUnmirrored(t, stuck.ID), "对照：消息确实没发出去")
	alerts := slackStuckAlerts(f)
	require.Len(t, alerts, 1)
	assert.Contains(t, alerts[0], "1 个会话")
	assert.Contains(t, alerts[0], "2 条消息")
	assert.Contains(t, alerts[0], "#"+uintStr(stuck.ID))
	assert.NotContains(t, alerts[0], "#"+uintStr(recent.ID), "不到 10 分钟的不算卡住")
	assert.NotContains(t, alerts[0], "卡住的第一条", "告警不带消息正文")

	// 下一分钟再扫：仍然卡着，但 30 分钟内不重复告警
	_, err = chatSlackSweepRun(ctx, string(brand))
	require.NoError(t, err)
	assert.Len(t, slackStuckAlerts(f), 1)
	convs, msgs, alerted := chatSlackStuckCheck(ctx, string(brand))
	assert.Equal(t, 1, convs, "汇总数字每轮都报（Error 日志），只是不再往频道发")
	assert.Equal(t, 2, msgs)
	assert.False(t, alerted)

	// 又一个会话卡住：只为新卡住的会话告警一次
	slackBackdate(t, recent.ID, 12*time.Minute)
	convs, msgs, alerted = chatSlackStuckCheck(ctx, string(brand))
	assert.Equal(t, 2, convs)
	assert.Equal(t, 3, msgs)
	assert.True(t, alerted)
	alerts = slackStuckAlerts(f)
	require.Len(t, alerts, 2)
	assert.Contains(t, alerts[1], "#"+uintStr(recent.ID))
}

// 没有卡住的消息：不告警。
func TestChatWorker_SweepNoAlertWhenHealthy(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	slackStuckCleanup(t, conv.ID)
	slackSeed(t, conv, SenderVisitor, MsgText, "晚了 11 分钟但这轮能发出去")
	slackBackdate(t, conv.ID, 11*time.Minute)

	_, err := chatSlackSweepRun(context.Background(), string(brand))
	require.NoError(t, err)
	require.EqualValues(t, 0, slackUnmirrored(t, conv.ID))
	assert.Empty(t, slackStuckAlerts(f))
}

// 告警发不出去（Slack 整体不可用）：不占去重窗口，Slack 恢复后的下一轮还能告警。
func TestChatWorker_StuckAlertFailureDoesNotConsumeDedupe(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	slackStuckCleanup(t, conv.ID)
	slackSeed(t, conv, SenderVisitor, MsgText, "卡住")
	slackBackdate(t, conv.ID, 11*time.Minute)
	ctx := context.Background()

	f.FailAll(500)
	convs, _, alerted := chatSlackStuckCheck(ctx, string(brand))
	assert.Equal(t, 1, convs)
	assert.False(t, alerted)
	f.FailAll(0)
	_, _, alerted = chatSlackStuckCheck(ctx, string(brand))
	assert.True(t, alerted)
}

// 接线守卫：sweep 任务必须做卡住检查；启动时必须校验配置。
func TestChatWorker_VisibilityWired(t *testing.T) {
	assert.True(t, goFuncCalls(t, "worker_chat.go", "chatSlackSweepRun")["chatSlackStuckCheck"])
	assert.True(t, goFuncCalls(t, "logic_chat_realtime.go", "StartChatBroadcast")["chatLogConfigProblems"])
}
