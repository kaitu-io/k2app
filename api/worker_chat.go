package center

import (
	"context"
	"time"

	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/wordgate/qtoolkit/asynq"
	"github.com/wordgate/qtoolkit/log"
)

// 客服聊天的定时任务：
//   - 每分钟 chatSlackSweep：补镜像实时路径漏发的消息、补刷状态卡、补归档；
//   - 每 10 分钟关闭空闲会话（AI 处理中 24 小时、人工处理中 72 小时无消息），
//     各追加一条 system 事件并归档 Slack 频道。

const (
	TaskTypeChatSlackSweep = "chat:slack-sweep"
	TaskTypeChatCloseIdle  = "chat:close-idle"

	// 空闲多久自动关闭（按 last_message_at）。
	chatIdleCloseAI    = 24 * time.Hour
	chatIdleCloseHuman = 72 * time.Hour

	chatAutoCloseNotice = "会话因长时间无消息自动关闭"
)

// chatCron 一个定时任务的注册信息。
type chatCron struct {
	Spec     string // Cron 格式: 分 时 日 月 周
	TaskType string
	Handler  func(ctx context.Context, payload []byte) error
	Unique   time.Duration // 防止多实例重复入队同一任务
}

// chatCronSpecs 客服聊天的全部定时任务，由 RegisterChatWorker 注册。
var chatCronSpecs = []chatCron{
	{"* * * * *", TaskTypeChatSlackSweep, handleChatSlackSweep, 2 * time.Minute},
	{"*/10 * * * *", TaskTypeChatCloseIdle, handleChatCloseIdle, 11 * time.Minute},
}

// RegisterChatWorker 注册客服聊天的任务处理函数与 Cron。
func RegisterChatWorker() {
	for _, c := range chatCronSpecs {
		asynq.Handle(c.TaskType, c.Handler)
		asynq.Cron(c.Spec, c.TaskType, nil, hibikenAsynq.Unique(c.Unique))
	}
}

func handleChatSlackSweep(ctx context.Context, _ []byte) error {
	_, err := chatSlackSweepRun(ctx, "")
	return err
}

func handleChatCloseIdle(ctx context.Context, _ []byte) error {
	_, err := chatCloseIdleRun(ctx, "")
	return err
}

// chatSlackSweepRun 跑一轮 Slack 兜底；brand 非空时只处理该品牌（测试隔离用，生产传空）。
// 单个会话的失败只记日志、不算任务失败：下一分钟还会再来，交给 asynq 重试只会叠出重复的 sweep。
func chatSlackSweepRun(ctx context.Context, brand string) (int, error) {
	n, err := chatSlackSweepIn(ctx, brand)
	if err != nil {
		log.Warnf(ctx, "[CHAT] slack sweep: handled=%d err=%v", n, err)
	} else if n > 0 {
		log.Infof(ctx, "[CHAT] slack sweep: handled=%d", n)
	}
	return n, nil
}

// chatCloseIdleRun 关闭空闲会话，返回关闭了几个；brand 非空时只处理该品牌（测试隔离用，生产传空）。
// closeIdleConversations 只改状态并通知状态钩子（推给访客、刷新 Slack 状态卡）；
// 这里补上关闭事件与频道归档。事件先落库再归档：归档会先把尾巴（含这条事件）发进频道。
// 事件或归档失败只记日志——会话已经关了；没归档成的由 chatSlackSweep 第二遍补。
func chatCloseIdleRun(ctx context.Context, brand string) (int, error) {
	closed, err := closeIdleConversationsIn(ctx, chatIdleCloseAI, chatIdleCloseHuman, brand)
	for i := range closed {
		c := &closed[i]
		if _, _, aerr := appendMessage(ctx, c, appendMessageInput{
			SenderType: SenderSystem, Kind: MsgEvent, Content: chatAutoCloseNotice, Meta: chatEventMeta(ChatEventAutoClosed),
		}); aerr != nil {
			log.Errorf(ctx, "[CHAT] auto close: append event conv=%d: %v", c.ID, aerr)
		}
		if aerr := chatSlackArchive(ctx, c); aerr != nil {
			log.Warnf(ctx, "[CHAT] auto close: archive conv=%d (left to sweep): %v", c.ID, aerr)
		}
	}
	if len(closed) > 0 {
		log.Infof(ctx, "[CHAT] auto closed %d idle conversations", len(closed))
	}
	// closeIdleConversations 出错时也返回已关闭的那部分，上面已经处理完
	return len(closed), err
}
