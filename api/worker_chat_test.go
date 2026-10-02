package center

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
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
