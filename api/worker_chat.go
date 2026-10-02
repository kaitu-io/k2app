package center

import (
	"context"
	"errors"
	"fmt"
	"time"

	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/wordgate/qtoolkit/asynq"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
)

// 客服聊天的定时任务：
//   - 每分钟 chatSlackSweep：补镜像实时路径漏发的消息、补刷状态卡、补归档；
//   - 每 10 分钟关闭空闲会话（AI 处理中 24 小时、人工处理中 72 小时无消息），
//     各追加一条 system 事件并归档 Slack 频道；上一轮没记成事件的这一轮补。

const (
	TaskTypeChatSlackSweep = "chat:slack-sweep"
	TaskTypeChatCloseIdle  = "chat:close-idle"

	// 空闲多久自动关闭（按 last_message_at）。
	chatIdleCloseAI    = 24 * time.Hour
	chatIdleCloseHuman = 72 * time.Hour

	chatAutoCloseNotice = "会话因长时间无消息自动关闭"

	// chatAutoCloseBackfillAge 补记关闭事件只看这么久以内关闭的会话，不扫历史。
	chatAutoCloseBackfillAge = 24 * time.Hour
	// chatAutoCloseLockTTL 补记事件的会话级短锁。
	chatAutoCloseLockTTL = time.Minute
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
// 失败只记日志、不算任务失败：下一分钟还会再来，交给 asynq 重试只会叠出重复的 sweep。
// 列表查询失败（库的问题）记 Error；单个会话的 Slack 调用失败记 Warn。
func chatSlackSweepRun(ctx context.Context, brand string) (int, error) {
	n, err := chatSlackSweepIn(ctx, brand)
	switch {
	case chatSweepQueryFailed(err):
		log.Errorf(ctx, "[CHAT] slack sweep: handled=%d err=%v", n, err)
	case err != nil:
		log.Warnf(ctx, "[CHAT] slack sweep: handled=%d err=%v", n, err)
	case n > 0:
		log.Infof(ctx, "[CHAT] slack sweep: handled=%d", n)
	}
	return n, nil
}

// chatSweepQueryFailed sweep 的错误里是否含列表查询失败。
func chatSweepQueryFailed(err error) bool {
	return errors.Is(err, errChatSlackSweepQuery)
}

// chatCloseIdleRun 关闭空闲会话并给它们补上关闭事件与频道归档；返回本轮记下了几条关闭事件。
// brand 非空时只处理该品牌（测试隔离用，生产传空）。
//
// closeIdleConversations 只改状态并通知状态钩子（推给访客、刷新 Slack 状态卡），不落事件、不归档。
// 事件不在"刚关掉的那一批"上直接追加，而是统一走 chatAutoCloseRecord 按库里的事实补：
// 上一轮关了会话却没来得及记事件（追加失败、进程中途被杀）的，这一轮一并补上。
func chatCloseIdleRun(ctx context.Context, brand string) (int, error) {
	closed, cerr := closeIdleConversationsIn(ctx, chatIdleCloseAI, chatIdleCloseHuman, brand)
	if len(closed) > 0 {
		log.Infof(ctx, "[CHAT] auto closed %d idle conversations", len(closed))
	}
	// closeIdleConversations 出错时已关闭的那部分照样要记事件
	n, rerr := chatAutoCloseRecord(ctx, brand)
	return n, errors.Join(cerr, rerr)
}

// chatAutoCloseRecord 给"已关闭、24 小时内关闭、还没有任何关闭事件"的会话记一条 auto_closed 事件并归档频道。
//
// 模型里没有"关闭原因"列，判据是消息表：人工关闭（chatCloseByStaff）的 closed 事件与关闭在同一事务里落下，
// 闲置关闭是唯一先关后记的路径——所以关了却没有 closed / auto_closed 事件的，就是欠一条 auto_closed。
// 新增关闭路径时必须保持"事件不晚于关闭"，否则会被这里补成自动关闭。
// 时间下界避免在历史数据上逐个补事件。单个会话失败只记日志，下一轮再来。
// chatSlackSweep 的归档遍只碰已有关闭事件的会话，所以事件一定先于归档进频道。
func chatAutoCloseRecord(ctx context.Context, brand string) (int, error) {
	q := db.Get().WithContext(ctx).
		Where("status = ? AND closed_at > ?", ConvClosed, time.Now().Add(-chatAutoCloseBackfillAge)).
		Where("NOT EXISTS (SELECT 1 FROM conversation_messages m WHERE m.conversation_id = conversations.id AND m.kind = ? AND m.meta IN ?)",
			MsgEvent, chatCloseEventMetas())
	if brand != "" {
		q = q.Where("brand = ?", brand)
	}
	var convs []Conversation
	if err := q.Order("id").Find(&convs).Error; err != nil {
		return 0, fmt.Errorf("list closed conversations without close event: %w", err)
	}
	n := 0
	for i := range convs {
		c := &convs[i]
		recorded, err := chatAutoCloseRecordOne(ctx, c)
		if err != nil {
			log.Errorf(ctx, "[CHAT] auto close: record event conv=%d: %v", c.ID, err)
			continue
		}
		if !recorded {
			continue
		}
		n++
		// 归档会先把尾巴（含刚记的事件）发进频道；没归档成的由 chatSlackSweep 第二遍补
		if err := chatSlackArchive(ctx, c); err != nil {
			log.Warnf(ctx, "[CHAT] auto close: archive conv=%d (left to sweep): %v", c.ID, err)
		}
	}
	return n, nil
}

func chatCloseEventMetas() []string {
	return []string{chatEventMeta(ChatEventClosed), chatEventMeta(ChatEventAutoClosed)}
}

// chatAutoCloseRecordOne 记一条 auto_closed 事件；recorded=false 表示别的实例正在记或已经记过。
// 幂等靠会话级短锁 + 持锁后复查：两个实例同时跑，只有一个能落事件。
// （不用 ClientID 做唯一键：访客的 clientId 是任意串，可以抢占任何固定值。）
func chatAutoCloseRecordOne(ctx context.Context, conv *Conversation) (recorded bool, err error) {
	rdb := redis.Client()
	key, token := fmt.Sprintf("chat:autoclose:lock:%d", conv.ID), generateId("acl")
	ok, err := rdb.SetNX(ctx, key, token, chatAutoCloseLockTTL).Result()
	if err != nil {
		return false, fmt.Errorf("lock: %w", err)
	}
	if !ok {
		return false, nil
	}
	defer func() {
		const script = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
		_ = rdb.Eval(context.WithoutCancel(ctx), script, []string{key}, token).Err()
	}()

	var have int64
	if err := db.Get().WithContext(ctx).Model(&ConversationMessage{}).
		Where("conversation_id = ? AND kind = ? AND meta IN ?", conv.ID, MsgEvent, chatCloseEventMetas()).
		Count(&have).Error; err != nil {
		return false, fmt.Errorf("recheck close event: %w", err)
	}
	if have > 0 {
		return false, nil
	}
	if _, _, err := appendMessage(ctx, conv, appendMessageInput{
		SenderType: SenderSystem, Kind: MsgEvent, Content: chatAutoCloseNotice, Meta: chatEventMeta(ChatEventAutoClosed),
	}); err != nil {
		return false, fmt.Errorf("append event: %w", err)
	}
	return true, nil
}
