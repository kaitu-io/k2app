package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
	"github.com/wordgate/qtoolkit/slack"
)

// Slack 频道镜像（客服聊天）：Slack 是客服的主界面。每个会话在访客第一条消息时建一个专属私有频道，
// 之后所有消息（访客 / AI / 客服 / 系统）按 id 顺序逐条发进去——人要逐条盯着 AI，不行就接手。
// 库里的 slack_mirrored_at 是 outbox 标记：为空 = 还没处理；实时钩子发不出去的由 chatSlackSweep 补。
// 总览频道（chatSlackLobby）未配置 = 整个镜像关闭，所有函数直接返回 nil。
// Slack 的任何失败只记日志并返回 error，绝不影响 appendMessage。
// 设计见 docs/superpowers/specs/2026-10-01-support-chat-design.md §8

const (
	// chatSlackLockTTL 会话级镜像锁的有效期。长积压 + 限流会超过它，所以每次 Slack 调用前都续期。
	chatSlackLockTTL = 30 * time.Second
	// chatSlackCallTimeout 单次 Slack 调用超时。
	chatSlackCallTimeout = 10 * time.Second
	// chatSlackMaxWait 限流等待上限：必须满足 调用超时 + 等待 < 锁有效期，否则等待期间锁会过期。
	chatSlackMaxWait = 15 * time.Second
	// chatSlackMaxRetries 遇限流时同一步最多重试几次。
	chatSlackMaxRetries = 3
	// chatSlackRunTimeout 钩子触发的一次镜像（含建频道）的总上限。
	chatSlackRunTimeout = 5 * time.Minute
	// chatSlackSaveTimeout 落库用独立超时：Slack 侧已经成功，不能因为调用方 ctx 过期而丢掉结果。
	chatSlackSaveTimeout = 5 * time.Second
	// chatSlackMaxPasses 放锁后复查的最多轮数（持锁期间新到的消息，其钩子因拿不到锁已返回）。
	chatSlackMaxPasses = 5
	chatSlackBatch     = 200
	// chatSlackNameAttempts 频道重名时最多尝试几个名字（原名、-2、-3）。
	chatSlackNameAttempts = 3

	chatSlackStaffKey = "chat:slack:staff"
	chatSlackStaffTTL = 5 * time.Minute

	// chatSlackSweepAge sweep 只碰早于它的未镜像消息 / 已关闭会话，不与实时路径抢。
	chatSlackSweepAge = 30 * time.Second
	// chatSlackSweepMaxAge sweep 的下界：更早的未镜像消息、更早关闭的会话不再碰。
	// 这样在已有历史数据的库上打开镜像不会给每个旧会话建频道、刷爆 Slack。
	chatSlackSweepMaxAge = 24 * time.Hour
	chatSlackSweepLimit  = 50
	// chatSlackUnlockTimeout 放锁的独立超时。
	chatSlackUnlockTimeout = 2 * time.Second
)

var (
	errChatSlackLockLost = errors.New("chat slack: lock lost")
	errChatSlackBusy     = errors.New("chat slack: tail not flushed yet")
	// errChatSlackSweepQuery sweep 自己的列表查询失败（库的问题，不是某个会话的 Slack 调用失败）。
	errChatSlackSweepQuery = errors.New("chat slack sweep: query failed")
)

func init() {
	chatAfterAppend = append(chatAfterAppend, func(conv *Conversation, msg *ConversationMessage) {
		if chatSlackLobby() == "" {
			return
		}
		// 只拷贝值：异步执行时调用方可能已改动 conv
		convID, msgID, sender := conv.ID, msg.ID, msg.SenderType
		// 卡片状态只在人工处理时取决于 last_message_by（见 chatSlackStatus），其余情况不必刷
		mayFlip := conv.Status == ConvOpen && conv.Handler == HandlerHuman && msg.Kind != MsgNote &&
			(sender == SenderVisitor || sender == SenderAI || sender == SenderStaff)
		chatAsync(func() {
			ctx, cancel := context.WithTimeout(context.Background(), chatSlackRunTimeout)
			defer cancel()
			created, err := chatSlackMirrorRun(ctx, convID)
			if err != nil || created || !mayFlip { // 刚建好的频道已在镜像末尾刷过一次
				return
			}
			if chatSlackLastByChanged(ctx, convID, msgID, sender) {
				if err := chatSlackRefreshCard(ctx, &Conversation{ID: convID}); err != nil {
					log.Errorf(ctx, "chat slack refresh card: conv=%d err=%v", convID, err)
				}
			}
		})
	})
	chatAfterStateChange = append(chatAfterStateChange, func(conv *Conversation) {
		if chatSlackLobby() == "" {
			return
		}
		convID := conv.ID
		chatAsync(func() {
			ctx, cancel := context.WithTimeout(context.Background(), chatSlackRunTimeout)
			defer cancel()
			if err := chatSlackRefreshCard(ctx, &Conversation{ID: convID}); err != nil {
				log.Errorf(ctx, "chat slack refresh card: conv=%d err=%v", convID, err)
			}
		})
	})
}

// chatSlackLastByChanged 这条消息是否让 last_message_by 变了（与它之前最近一条计入的消息比）。
func chatSlackLastByChanged(ctx context.Context, convID, msgID uint64, sender string) bool {
	var prev []ConversationMessage
	err := db.Get().WithContext(ctx).Select("sender_type").
		Where("conversation_id = ? AND id < ? AND kind <> ? AND sender_type IN ?", convID, msgID, MsgNote,
			[]string{SenderVisitor, SenderAI, SenderStaff}).Order("id DESC").Limit(1).Find(&prev).Error
	return err != nil || len(prev) == 0 || prev[0].SenderType != sender
}

// ---- 锁 ----

// chatSlackLock 会话级镜像锁（Redis "chat:slack:lock:<convID>"），带 token，可续期。
type chatSlackLock struct{ key, token string }

// chatSlackAcquire 取锁。ok=false 表示别的调用正持锁（它会把消息发完）。
func chatSlackAcquire(ctx context.Context, convID uint64) (*chatSlackLock, bool, error) {
	l := &chatSlackLock{key: fmt.Sprintf("chat:slack:lock:%d", convID), token: generateId("slk")}
	ok, err := redis.Client().SetNX(ctx, l.key, l.token, chatSlackLockTTL).Result()
	if err != nil {
		return nil, false, fmt.Errorf("chat slack lock: %w", err)
	}
	return l, ok, nil
}

// refresh 续期；锁已不属于自己（过期后被别人拿走）返回 errChatSlackLockLost，调用方必须停下本轮。
func (l *chatSlackLock) refresh(ctx context.Context) error {
	const script = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("pexpire", KEYS[1], ARGV[2]) else return 0 end`
	n, err := redis.Client().Eval(ctx, script, []string{l.key}, l.token, chatSlackLockTTL.Milliseconds()).Int()
	if err != nil {
		return fmt.Errorf("chat slack lock refresh: %w", err)
	}
	if n == 0 {
		return errChatSlackLockLost
	}
	return nil
}

func (l *chatSlackLock) release() {
	const script = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
	ctx, cancel := context.WithTimeout(context.Background(), chatSlackUnlockTimeout)
	defer cancel()
	_ = redis.Client().Eval(ctx, script, []string{l.key}, l.token).Err()
}

// chatSlackDo 执行一步 Slack 调用：单次 10 秒超时；遇限流等 max(Retry-After, 1s) 后重试这一步，最多 3 次。
// lock 非 nil 时每次尝试前续期，锁丢了就不再调用（否则会与新的持锁者并发发消息）。
func chatSlackDo(ctx context.Context, lock *chatSlackLock, fn func(ctx context.Context) error) error {
	for attempt := 0; ; attempt++ {
		if lock != nil {
			if err := lock.refresh(ctx); err != nil {
				return err
			}
		}
		cctx, cancel := context.WithTimeout(ctx, chatSlackCallTimeout)
		err := fn(cctx)
		cancel()
		var rl *slack.RateLimitError
		if !errors.As(err, &rl) || attempt >= chatSlackMaxRetries {
			return err
		}
		wait := max(rl.RetryAfter, time.Second)
		if wait > chatSlackMaxWait {
			return err // 等不起（锁会过期）：停下本轮，留给 sweep
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// chatSlackGone Slack 说频道已归档：再也发不进去，按"已处理"对待，否则 sweep 会永远重试。
func chatSlackGone(err error) bool {
	var apiErr *slack.APIError
	return errors.As(err, &apiErr) && apiErr.Code == "is_archived"
}

// chatSlackSave 写会话的一个 slack_* 字段。用独立 ctx：Slack 侧已成功，丢了这次写入下轮就会重复建。
func chatSlackSave(ctx context.Context, convID uint64, column, value string) error {
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), chatSlackSaveTimeout)
	defer cancel()
	if err := db.Get().WithContext(sctx).Model(&Conversation{}).Where("id = ?", convID).Update(column, value).Error; err != nil {
		return fmt.Errorf("save %s: %w", column, err)
	}
	return nil
}

// ---- 建频道 ----

// chatSlackStaff 返回客服名单：总览频道成员去掉 bot 自己。Redis 缓存 5 分钟。
// 拿不到 bot 的 id 就报错——不知道谁是 bot 时不盲目拉人。
func chatSlackStaff(ctx context.Context, lock *chatSlackLock) ([]string, error) {
	rdb := redis.Client()
	if raw, err := rdb.Get(ctx, chatSlackStaffKey).Result(); err == nil {
		var cached []string
		if json.Unmarshal([]byte(raw), &cached) == nil {
			return cached, nil
		}
	}
	var members []string
	var bot string
	if err := chatSlackDo(ctx, lock, func(c context.Context) (err error) {
		members, err = slack.ChannelMembers(c, chatSlackLobby())
		return err
	}); err != nil {
		return nil, fmt.Errorf("lobby members: %w", err)
	}
	if err := chatSlackDo(ctx, lock, func(c context.Context) (err error) {
		bot, err = slack.BotUserID(c)
		return err
	}); err != nil {
		return nil, fmt.Errorf("bot user id: %w", err)
	}
	staff := make([]string, 0, len(members))
	for _, id := range members {
		if id != bot {
			staff = append(staff, id)
		}
	}
	if b, err := json.Marshal(staff); err == nil {
		_ = rdb.Set(ctx, chatSlackStaffKey, b, chatSlackStaffTTL).Err()
	}
	return staff, nil
}

// chatSlackSetup 在锁内把频道建完整：建频道 → 拉客服 → 主题 → 状态卡并置顶 → 总览频道一行。
// 任一步失败本轮中止；每个 Slack 产物的 id/ts 一拿到就落库，下轮据此跳过已完成的步骤
// （拉人与主题没有对应字段，状态卡还没发出时会重做，二者都幂等）。
func chatSlackSetup(ctx context.Context, lock *chatSlackLock, conv *Conversation) error {
	if conv.SlackChannelID == "" {
		base, id := chatSlackChannelName(conv), ""
		for i := 1; ; i++ {
			name := base
			if i > 1 {
				name = fmt.Sprintf("%s-%d", base, i)
			}
			err := chatSlackDo(ctx, lock, func(c context.Context) (err error) {
				id, err = slack.CreateChannel(c, name, true)
				return err
			})
			if err == nil {
				break
			}
			if !errors.Is(err, slack.ErrChannelNameTaken) || i >= chatSlackNameAttempts {
				return fmt.Errorf("create channel: %w", err)
			}
		}
		if err := chatSlackSave(ctx, conv.ID, "slack_channel_id", id); err != nil {
			// 频道已建但没记下来：下轮会再建一个。记下 id 并尽力归档这个孤儿，免得客服看到空频道
			log.Errorf(ctx, "chat slack: channel %s created but not saved, archiving orphan: conv=%d err=%v", id, conv.ID, err)
			if aerr := chatSlackDo(ctx, lock, func(c context.Context) error {
				return slack.ArchiveChannel(c, id)
			}); aerr != nil {
				log.Errorf(ctx, "chat slack: archive orphan channel %s: conv=%d err=%v", id, conv.ID, aerr)
			}
			return err
		}
		conv.SlackChannelID = id
	}
	channel := conv.SlackChannelID
	view := chatSlackRender(ctx, conv)

	if conv.SlackCardTS == "" {
		staff, err := chatSlackStaff(ctx, lock)
		if err != nil {
			return err
		}
		if len(staff) > 0 {
			if err := chatSlackDo(ctx, lock, func(c context.Context) error {
				return slack.InviteToChannel(c, channel, staff)
			}); err != nil {
				return fmt.Errorf("invite staff: %w", err)
			}
		}
		if err := chatSlackDo(ctx, lock, func(c context.Context) error {
			return slack.SetChannelTopic(c, channel, view.topic)
		}); err != nil {
			return fmt.Errorf("set topic: %w", err)
		}
		var ts string
		if err := chatSlackDo(ctx, lock, func(c context.Context) (err error) {
			ts, err = slack.PostMessage(c, channel, view.card, nil)
			return err
		}); err != nil {
			return fmt.Errorf("post card: %w", err)
		}
		if err := chatSlackSave(ctx, conv.ID, "slack_card_ts", ts); err != nil {
			return err
		}
		conv.SlackCardTS = ts
		// 置顶失败不中止：卡片 ts 已落库，下轮不会再走到这里，为一个置顶卡住整个镜像不值得
		if err := chatSlackDo(ctx, lock, func(c context.Context) error {
			return slack.PinMessage(c, channel, ts)
		}); err != nil {
			if errors.Is(err, errChatSlackLockLost) {
				return err
			}
			log.Warnf(ctx, "chat slack pin card: conv=%d err=%v", conv.ID, err)
		}
	}

	if conv.SlackLobbyTS == "" {
		var ts string
		if err := chatSlackDo(ctx, lock, func(c context.Context) (err error) {
			ts, err = slack.PostMessage(c, chatSlackLobby(), view.lobby, nil)
			return err
		}); err != nil {
			return fmt.Errorf("post lobby line: %w", err)
		}
		if err := chatSlackSave(ctx, conv.ID, "slack_lobby_ts", ts); err != nil {
			return err
		}
		conv.SlackLobbyTS = ts
	}
	return nil
}

// ---- 镜像 ----

// chatSlackPending 取会话里还没处理的消息（id 升序，最多一批）。
func chatSlackPending(ctx context.Context, convID uint64, limit int) ([]ConversationMessage, error) {
	var msgs []ConversationMessage
	if err := db.Get().WithContext(ctx).Where("conversation_id = ? AND slack_mirrored_at IS NULL", convID).
		Order("id ASC").Limit(limit).Find(&msgs).Error; err != nil {
		return nil, fmt.Errorf("list unmirrored messages: %w", err)
	}
	return msgs, nil
}

// chatSlackRound 持锁执行一轮：需要则建频道，然后严格按 id 升序逐条发。
// 第 N 条失败就停（绝不跳去发 N+1）；每条发成功立刻标记 slack_mirrored_at（不攒批）。
// 至少一次语义：发成功后、标记前崩溃，下轮会把这一条再发一遍（最多重复这一条）。
// 不该发的（备注、来自 Slack 的）只标记不发，outbox 才能排空。
func chatSlackRound(ctx context.Context, lock *chatSlackLock, convID uint64) (created bool, err error) {
	conv, err := chatLoadConversation(ctx, convID)
	if err != nil {
		return false, fmt.Errorf("load conversation: %w", err)
	}
	msgs, err := chatSlackPending(ctx, convID, chatSlackBatch)
	if err != nil || len(msgs) == 0 {
		return false, err
	}
	if conv.SlackChannelID == "" || conv.SlackCardTS == "" || conv.SlackLobbyTS == "" {
		created = true
		if err := chatSlackSetup(ctx, lock, conv); err != nil {
			return created, err
		}
	}
	for i := range msgs {
		m := &msgs[i]
		if text, post := chatSlackMessageText(Brand(conv.Brand), m); post {
			err := chatSlackDo(ctx, lock, func(c context.Context) error {
				_, err := slack.PostMessage(c, conv.SlackChannelID, text, nil)
				return err
			})
			if err != nil && !chatSlackGone(err) {
				return created, fmt.Errorf("post message %d: %w", m.ID, err)
			}
		}
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), chatSlackSaveTimeout)
		err := db.Get().WithContext(sctx).Model(&ConversationMessage{}).
			Where("id = ? AND slack_mirrored_at IS NULL", m.ID).Update("slack_mirrored_at", time.Now()).Error
		cancel()
		if err != nil {
			return created, fmt.Errorf("mark message %d mirrored: %w", m.ID, err)
		}
	}
	return created, nil
}

// chatSlackMirror 把该会话所有未镜像的消息按 id 升序发进专属频道；还没有频道则先建。
// 全程持 Redis 锁 "chat:slack:lock:<convID>"；拿不到锁返回 nil（持锁者会发完）。
func chatSlackMirror(ctx context.Context, convID uint64) error {
	_, err := chatSlackMirrorRun(ctx, convID)
	return err
}

// chatSlackMirrorRun 同 chatSlackMirror，另外返回本次是否做了建频道（调用方据此省掉一次刷卡）。
// 放锁后复查：持锁期间新到的消息（其钩子拿不到锁已返回）由这里再来一轮，最多 5 轮。
func chatSlackMirrorRun(ctx context.Context, convID uint64) (created bool, err error) {
	if chatSlackLobby() == "" {
		return false, nil
	}
	for pass := 0; pass < chatSlackMaxPasses; pass++ {
		lock, ok, lerr := chatSlackAcquire(ctx, convID)
		if lerr != nil {
			err = lerr
			break
		}
		if !ok {
			break
		}
		c, rerr := chatSlackRound(ctx, lock, convID)
		lock.release()
		created = created || c
		if rerr != nil {
			err = rerr
			break
		}
		rest, perr := chatSlackPending(ctx, convID, 1)
		if perr != nil || len(rest) == 0 {
			err = perr
			break
		}
	}
	if err != nil {
		log.Errorf(ctx, "chat slack mirror: conv=%d err=%v", convID, err)
	}
	if created {
		// 状态卡按建卡那一刻的状态渲染，积压发完后再刷一次（卡还没建出来时这是 no-op）
		if rerr := chatSlackRefreshCard(ctx, &Conversation{ID: convID}); rerr != nil && err == nil {
			err = rerr
		}
	}
	return created, err
}

// chatSlackRefreshCard 改写频道内状态卡与总览频道那一行。以库为准（传入的 conv 可能是旧的）；
// 状态卡还没建时什么都不做——镜像建卡时会用当时的状态。
// 不持镜像锁：两次并发刷新谁后写谁生效，各自都是刷新前现读的状态，最坏短暂显示旧状态。
func chatSlackRefreshCard(ctx context.Context, conv *Conversation) error {
	if chatSlackLobby() == "" || conv == nil {
		return nil
	}
	fresh, err := chatLoadConversation(ctx, conv.ID)
	if err != nil {
		return fmt.Errorf("load conversation: %w", err)
	}
	if fresh.SlackChannelID == "" || fresh.SlackCardTS == "" {
		return nil
	}
	view := chatSlackRender(ctx, fresh)
	var errs []error
	if err := chatSlackDo(ctx, nil, func(c context.Context) error {
		return slack.UpdateMessage(c, fresh.SlackChannelID, fresh.SlackCardTS, view.card)
	}); err != nil && !chatSlackGone(err) {
		errs = append(errs, fmt.Errorf("update card: %w", err))
	}
	if fresh.SlackLobbyTS != "" {
		if err := chatSlackDo(ctx, nil, func(c context.Context) error {
			return slack.UpdateMessage(c, chatSlackLobby(), fresh.SlackLobbyTS, view.lobby)
		}); err != nil {
			errs = append(errs, fmt.Errorf("update lobby line: %w", err))
		}
	}
	return errors.Join(errs...)
}

// chatSlackArchive 会话关闭后调用：先把尾巴发完，再刷新状态卡，最后归档频道并记下 slack_archived_at。
// 幂等：已记过归档时间就什么都不做。尾巴没发完（发送失败，或别的调用正持锁在发）返回 error 不归档——
// 归档后再也发不进去；这种情况由 chatSlackSweep 的第二遍重试。刷卡失败只记日志，不挡归档。
func chatSlackArchive(ctx context.Context, conv *Conversation) error {
	if chatSlackLobby() == "" || conv == nil {
		return nil
	}
	fresh, err := chatLoadConversation(ctx, conv.ID)
	if err != nil {
		return fmt.Errorf("load conversation: %w", err)
	}
	if fresh.SlackArchivedAt != nil {
		return nil
	}
	if err := chatSlackMirror(ctx, conv.ID); err != nil {
		return err
	}
	if rest, err := chatSlackPending(ctx, conv.ID, 1); err != nil {
		return err
	} else if len(rest) > 0 {
		return errChatSlackBusy
	}
	if err := chatSlackRefreshCard(ctx, conv); err != nil {
		log.Errorf(ctx, "chat slack archive: refresh card: conv=%d err=%v", conv.ID, err)
	}
	// 镜像可能刚建了频道：重读拿频道 id
	if fresh, err = chatLoadConversation(ctx, conv.ID); err != nil {
		return fmt.Errorf("load conversation: %w", err)
	}
	if fresh.SlackChannelID == "" {
		return nil
	}
	if err := chatSlackDo(ctx, nil, func(c context.Context) error {
		return slack.ArchiveChannel(c, fresh.SlackChannelID) // already_archived 视作成功
	}); err != nil {
		return fmt.Errorf("archive channel: %w", err)
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), chatSlackSaveTimeout)
	defer cancel()
	if err := db.Get().WithContext(sctx).Model(&Conversation{}).Where("id = ?", conv.ID).
		Update("slack_archived_at", time.Now()).Error; err != nil {
		return fmt.Errorf("save slack_archived_at: %w", err)
	}
	return nil
}

// chatSlackSweep 兜底实时路径，两遍，各最多 50 个会话，最旧的先来：
//  1. 有未镜像消息（30 秒前、24 小时内）的会话：chatSlackMirror，成功后刷新状态卡
//     （实时路径失败时卡片也停在旧状态，只补消息不够）；
//  2. 已关闭（30 秒前、24 小时内）、有频道、已记关闭事件、还没归档成功的会话：chatSlackArchive。
//
// 返回成功处理的会话数（两遍都碰到的只算一次）；单个会话失败只记日志并汇总进返回的 error，不阻止后面的。
func chatSlackSweep(ctx context.Context) (int, error) {
	return chatSlackSweepIn(ctx, "")
}

// chatSlackSweepIn 同 chatSlackSweep；brand 非空时只处理该品牌（测试隔离用，生产传空）。
func chatSlackSweepIn(ctx context.Context, brand string) (int, error) {
	if chatSlackLobby() == "" {
		return 0, nil
	}
	now := time.Now()
	from, to := now.Add(-chatSlackSweepMaxAge), now.Add(-chatSlackSweepAge)
	done := map[uint64]bool{}
	var errs []error

	q := db.Get().WithContext(ctx).Table("conversation_messages AS m").
		Where("m.slack_mirrored_at IS NULL AND m.created_at > ? AND m.created_at < ?", from, to)
	if brand != "" {
		q = q.Joins("JOIN conversations AS c ON c.id = m.conversation_id").Where("c.brand = ?", brand)
	}
	var ids []uint64
	if err := q.Group("m.conversation_id").Order("MIN(m.id)").Limit(chatSlackSweepLimit).
		Pluck("m.conversation_id", &ids).Error; err != nil {
		return 0, fmt.Errorf("%w: list conversations to sweep: %w", errChatSlackSweepQuery, err)
	}
	for _, id := range ids {
		if err := chatSlackMirror(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("conv %d: %w", id, err))
			continue
		}
		if err := chatSlackRefreshCard(ctx, &Conversation{ID: id}); err != nil {
			errs = append(errs, fmt.Errorf("conv %d: refresh card: %w", id, err))
			continue
		}
		done[id] = true
	}

	// 只归档已经记了关闭事件（closed / auto_closed）的会话：闲置关闭是先关后记，事件可能晚于关闭
	// （一批里排在后面、或进程在两步之间被杀）。频道先归档的话，之后补记的事件就再也发不进去。
	// 事件由 close-idle 任务补（chatAutoCloseRecord），补上后下一轮 sweep 归档；
	// 尾巴是否发完由 chatSlackArchive 自己把关。
	aq := db.Get().WithContext(ctx).Model(&Conversation{}).
		Where("status = ? AND slack_channel_id <> '' AND slack_archived_at IS NULL AND closed_at > ? AND closed_at < ?",
			ConvClosed, from, to).
		Where("EXISTS (SELECT 1 FROM conversation_messages m WHERE m.conversation_id = conversations.id AND m.kind = ? AND m.meta IN ?)",
			MsgEvent, chatCloseEventMetas())
	if brand != "" {
		aq = aq.Where("brand = ?", brand)
	}
	var closed []uint64
	if err := aq.Order("id").Limit(chatSlackSweepLimit).Pluck("id", &closed).Error; err != nil {
		return len(done), errors.Join(append(errs, fmt.Errorf("%w: list conversations to archive: %w", errChatSlackSweepQuery, err))...)
	}
	for _, id := range closed {
		if err := chatSlackArchive(ctx, &Conversation{ID: id}); err != nil {
			delete(done, id)
			errs = append(errs, fmt.Errorf("conv %d: archive: %w", id, err))
			continue
		}
		done[id] = true
	}
	return len(done), errors.Join(errs...)
}
