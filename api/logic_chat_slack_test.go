package center

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/redis"
)

// ---- 测试辅助 ----

// slackConv 建一个会话（不经钩子）；brand 传 "" 用默认品牌。
func slackConv(t *testing.T, brand Brand, entry string) *Conversation {
	t.Helper()
	skipIfNoConfig(t)
	if brand == "" {
		brand = BrandKaitu
	}
	conv, _, err := ensureConversation(context.Background(), newChatSubjectBrand(t, brand), entry)
	require.NoError(t, err)
	return conv
}

// slackSeed 直接落一条消息（绕过 appendMessage 的钩子，避免 AI 桩与镜像钩子介入）。
func slackSeed(t *testing.T, conv *Conversation, sender, kind, content string, mod ...func(*ConversationMessage)) *ConversationMessage {
	t.Helper()
	m := &ConversationMessage{ConversationID: conv.ID, SenderType: sender, Kind: kind, Content: content, CreatedAt: time.Now()}
	for _, f := range mod {
		f(m)
	}
	require.NoError(t, db.Get().Create(m).Error)
	return m
}

func slackReload(t *testing.T, convID uint64) *Conversation {
	t.Helper()
	var c Conversation
	require.NoError(t, db.Get().First(&c, convID).Error)
	return &c
}

// slackMsgPosts 返回发往频道的消息文本（去掉状态卡）。
func slackMsgPosts(f *fakeSlack, channel string) []string {
	var out []string
	for _, text := range f.Posts(channel) {
		if !strings.Contains(text, "在后台查看") {
			out = append(out, text)
		}
	}
	return out
}

func slackUnmirrored(t *testing.T, convID uint64) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Get().Model(&ConversationMessage{}).
		Where("conversation_id = ? AND slack_mirrored_at IS NULL", convID).Count(&n).Error)
	return n
}

// slackSetup 落一条访客消息并完成建频道，随后清空假 Slack 的调用记录。
func slackSetup(t *testing.T, f *fakeSlack, conv *Conversation) *Conversation {
	t.Helper()
	slackSeed(t, conv, SenderVisitor, MsgText, "开场")
	require.NoError(t, chatSlackMirror(context.Background(), conv.ID))
	f.Reset()
	return slackReload(t, conv.ID)
}

func uintStr(v uint64) string { return strconv.FormatUint(v, 10) }

func slackTestBrand() Brand { return Brand("t8" + generateId("")[10:]) }

// ---- 测试 ----

func TestSlackMirror_CreatesChannelThenPostsInOrder(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	slackSeed(t, conv, SenderVisitor, MsgText, "你好")
	slackSeed(t, conv, SenderVisitor, MsgText, "装不上")
	slackSeed(t, conv, SenderAI, MsgText, "请截图")

	require.NoError(t, chatSlackMirror(context.Background(), conv.ID))

	assert.Equal(t, []string{
		"conversations.create", "conversations.members", "auth.test", "conversations.invite",
		"conversations.setTopic", "chat.postMessage", "pins.add", "chat.postMessage",
		"chat.postMessage", "chat.postMessage", "chat.postMessage",
		"chat.update", "chat.update", // 建好并发完积压后刷新一次状态卡 + 总览行
	}, f.Methods())

	got := slackReload(t, conv.ID)
	require.NotEmpty(t, got.SlackChannelID)
	assert.NotEmpty(t, got.SlackCardTS)
	assert.NotEmpty(t, got.SlackLobbyTS)
	assert.EqualValues(t, 0, slackUnmirrored(t, conv.ID))

	calls := f.Calls()
	create := calls[0]
	assert.Equal(t, chatSlackChannelName(conv), create.Str("name"))
	assert.Equal(t, true, create.Params["is_private"])
	assert.Equal(t, fakeSlackLobbyID, calls[1].Str("channel"))
	assert.Equal(t, "U1,U2", calls[3].Str("users"), "不含 bot 自己")
	assert.Equal(t, got.SlackChannelID, calls[3].Str("channel"))
	assert.Contains(t, calls[4].Str("topic"), "未留邮箱 · 入口 /support")
	assert.Equal(t, got.SlackChannelID, calls[5].Str("channel"))
	assert.Contains(t, calls[5].Str("text"), "🤖 *AI 接待中* · "+BrandKaitu.Config().DisplayName)
	assert.Equal(t, got.SlackCardTS, calls[6].Str("timestamp"))
	assert.Equal(t, fakeSlackLobbyID, calls[7].Str("channel"))
	assert.Contains(t, calls[7].Str("text"), "<"+chatSlackChannelURL(got.SlackChannelID)+"|"+chatSlackChannelName(conv)+">")
	for _, c := range calls[8:11] {
		assert.Equal(t, got.SlackChannelID, c.Str("channel"))
		assert.NotContains(t, c.Params, "thread_ts")
	}
	assert.Equal(t, []string{"👤 你好", "👤 装不上", "🤖 请截图"}, slackMsgPosts(f, got.SlackChannelID))
}

func TestSlackMirror_ConcurrentCallsNoDuplicateChannel(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	for _, s := range []string{"a", "b", "c"} {
		slackSeed(t, conv, SenderVisitor, MsgText, s)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = chatSlackMirror(context.Background(), conv.ID)
		}()
	}
	wg.Wait()
	require.NoError(t, chatSlackMirror(context.Background(), conv.ID))

	assert.Len(t, f.CallsOf("conversations.create"), 1)
	got := slackReload(t, conv.ID)
	assert.Equal(t, []string{"👤 a", "👤 b", "👤 c"}, slackMsgPosts(f, got.SlackChannelID))
	assert.Len(t, f.Posts(fakeSlackLobbyID), 1)
}

func TestSlackMirror_ResumesAfterPartialSetup(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	slackSeed(t, conv, SenderVisitor, MsgText, "你好")
	f.Script("conversations.invite", fakeSlackHTTP(500))

	require.Error(t, chatSlackMirror(context.Background(), conv.ID))
	got := slackReload(t, conv.ID)
	require.NotEmpty(t, got.SlackChannelID, "频道 id 必须在建好后立刻落库")
	assert.Empty(t, got.SlackCardTS)
	assert.Empty(t, slackMsgPosts(f, got.SlackChannelID))

	f.Reset()
	require.NoError(t, chatSlackMirror(context.Background(), conv.ID))
	assert.Empty(t, f.CallsOf("conversations.create"), "不得重复建频道")
	assert.Len(t, f.CallsOf("conversations.invite"), 1)
	assert.Equal(t, []string{"👤 你好"}, slackMsgPosts(f, got.SlackChannelID))
	assert.EqualValues(t, 0, slackUnmirrored(t, conv.ID))
}

func TestSlackMirror_NameTakenRetries(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	slackSeed(t, conv, SenderVisitor, MsgText, "你好")
	f.Script("conversations.create", fakeSlackErr("name_taken"))

	require.NoError(t, chatSlackMirror(context.Background(), conv.ID))
	creates := f.CallsOf("conversations.create")
	require.Len(t, creates, 2)
	assert.Equal(t, chatSlackChannelName(conv), creates[0].Str("name"))
	assert.Equal(t, chatSlackChannelName(conv)+"-2", creates[1].Str("name"))
	assert.NotEmpty(t, slackReload(t, conv.ID).SlackChannelID)
}

func TestSlackMirror_FailureDoesNotBlockVisitor(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	ctx := context.Background()
	require.NoError(t, setHandler(ctx, conv, HandlerHuman)) // 不让 AI 桩插话
	f := newFakeSlack(t)
	f.FailAll(500)

	for _, s := range []string{"一", "二"} {
		_, dup, err := appendMessage(ctx, conv, appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: s})
		require.NoError(t, err)
		assert.False(t, dup)
	}
	msgs, err := messagesAfter(ctx, conv.ID, 0, true)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.EqualValues(t, 2, slackUnmirrored(t, conv.ID))

	f.FailAll(0)
	f.Reset()
	require.NoError(t, db.Get().Model(&ConversationMessage{}).Where("conversation_id = ?", conv.ID).
		Update("created_at", time.Now().Add(-time.Minute)).Error)
	n, err := chatSlackSweepIn(ctx, string(brand))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []string{"👤 一", "👤 二"}, slackMsgPosts(f, slackReload(t, conv.ID).SlackChannelID))
	assert.EqualValues(t, 0, slackUnmirrored(t, conv.ID))
}

func TestSlackMirror_SkipsSlackOriginAndNotes(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	ts := "1700000001." + generateId("")[12:]
	slackSeed(t, conv, SenderVisitor, MsgText, "你好")
	slackSeed(t, conv, SenderStaff, MsgText, "来自 Slack 的回复", func(m *ConversationMessage) { m.SlackTS = &ts; m.SenderName = "小王" })
	slackSeed(t, conv, SenderStaff, MsgNote, "内部备注", func(m *ConversationMessage) { m.SenderName = "小王" })
	slackSeed(t, conv, SenderStaff, MsgText, "后台回复", func(m *ConversationMessage) { m.SenderName = "小李" })

	require.NoError(t, chatSlackMirror(context.Background(), conv.ID))
	assert.Equal(t, []string{"👤 你好", "🧑‍💼 小李: 后台回复"}, slackMsgPosts(f, slackReload(t, conv.ID).SlackChannelID))
	assert.EqualValues(t, 0, slackUnmirrored(t, conv.ID), "不发的消息也要标记，outbox 才能排空")
}

func TestSlackMirror_RateLimitRetries(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	conv = slackSetup(t, f, conv)
	slackSeed(t, conv, SenderVisitor, MsgText, "限流这条")
	f.Script("chat.postMessage", fakeSlackRateLimited(1))

	start := time.Now()
	require.NoError(t, chatSlackMirror(context.Background(), conv.ID))
	assert.GreaterOrEqual(t, time.Since(start), time.Second)
	assert.Equal(t, []string{"👤 限流这条", "👤 限流这条"}, f.Posts(conv.SlackChannelID), "一次 429 + 一次成功")
	assert.EqualValues(t, 0, slackUnmirrored(t, conv.ID))

	require.NoError(t, chatSlackMirror(context.Background(), conv.ID))
	assert.Len(t, f.Posts(conv.SlackChannelID), 2, "成功后不得再发")
}

func TestSlackMirror_TransferMentionsChannel(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	slackSeed(t, conv, SenderVisitor, MsgText, "<!channel> 转人工 <@U1> & co")
	slackSeed(t, conv, SenderSystem, MsgEvent, "已转人工", func(m *ConversationMessage) { m.Meta = chatEventMeta(ChatEventTransferHuman) })
	slackSeed(t, conv, SenderSystem, MsgEvent, "会话已关闭", func(m *ConversationMessage) { m.Meta = chatEventMeta(ChatEventClosed) })
	slackSeed(t, conv, SenderAI, MsgOptions, "请选择", func(m *ConversationMessage) {
		m.Meta = `{"options":[{"label":"A","value":"a"},{"label":"B","value":"b"}]}`
	})

	require.NoError(t, chatSlackMirror(context.Background(), conv.ID))
	posts := slackMsgPosts(f, slackReload(t, conv.ID).SlackChannelID)
	require.Len(t, posts, 4)
	// 访客内容必须转义：否则访客可以 @ 全频道、伪造链接
	assert.Equal(t, "👤 &lt;!channel&gt; 转人工 &lt;@U1&gt; &amp; co", posts[0])
	assert.Equal(t, "<!channel> ℹ️ 已转人工", posts[1])
	assert.Equal(t, "ℹ️ 会话已关闭", posts[2])
	assert.Equal(t, "🤖 请选择\n选项: A / B", posts[3])
}

func TestSlackStatus(t *testing.T) {
	cases := []struct {
		status, handler, by, emoji, label string
	}{
		{ConvClosed, HandlerHuman, SenderVisitor, "⚪", "已关闭"},
		{ConvOpen, HandlerAI, SenderVisitor, "🤖", "AI 接待中"},
		{ConvOpen, HandlerHuman, SenderVisitor, "🔴", "等待人工"},
		{ConvOpen, HandlerHuman, SenderStaff, "🟡", "已回复待访客"},
		{ConvOpen, HandlerHuman, SenderAI, "🔴", "等待人工"}, // AI 带告别语转人工：最后一条是 AI，仍在等人
		{ConvOpen, HandlerHuman, "", "🔴", "等待人工"},
	}
	for _, c := range cases {
		emoji, label := chatSlackStatus(&Conversation{Status: c.status, Handler: c.handler, LastMessageBy: c.by})
		assert.Equal(t, c.emoji, emoji, "%+v", c)
		assert.Equal(t, c.label, label, "%+v", c)
	}
}

func TestSlackChannelNameAndURL(t *testing.T) {
	// UTC 10-01 17:00 = 上海 10-02 01:00：日期按客服所在时区
	conv := &Conversation{UUID: "ABCDEF12-3456-7890-abcd-ef1234567890", CreatedAt: time.Date(2026, 10, 1, 17, 0, 0, 0, time.UTC)}
	assert.Equal(t, "chat-1002-abcdef", chatSlackChannelName(conv))
	assert.Equal(t, "https://slack.com/app_redirect?channel=C123", chatSlackChannelURL("C123"))
}

func TestSlackCard_HighPriorityAndHistory(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	vals := chatTestValues(t, 1)
	gid, err := resolveGuest(ctx, BrandKaitu, vals[0], "", "zh-CN", "CN")
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Get().Where("brand = ? AND subject_kind = ? AND subject_id = ?", string(BrandKaitu), SubjectGuest, gid).Delete(&Conversation{})
	})
	last := time.Date(2026, 9, 30, 20, 0, 0, 0, time.UTC) // 上海 10-01
	mk := func(status, entry, channel string) *Conversation {
		c := &Conversation{UUID: generateId("u"), Brand: string(BrandKaitu), SubjectKind: SubjectGuest, SubjectID: gid,
			Status: status, Handler: HandlerAI, EntryPath: entry, LastMessageAt: last, SlackChannelID: channel}
		require.NoError(t, db.Get().Create(c).Error)
		return c
	}
	mk(ConvClosed, "/a", "")
	mk(ConvClosed, "/b", "COLD")
	conv := mk(ConvOpen, "/pay-result/abc", "CNEW")

	text := chatSlackCardText(ctx, conv)
	lines := strings.Split(text, "\n")
	require.Len(t, lines, 5)
	assert.Equal(t, "🤖 *AI 接待中* · "+BrandKaitu.Config().DisplayName+" · 🔥 支付结果页", lines[0])
	assert.Equal(t, "入口: /pay-result/abc", lines[1])
	assert.Equal(t, "访客: 未留邮箱 · 游客 #"+uintStr(gid), lines[2])
	assert.Equal(t, "此前会话: 2 次 · 上次 10月01日 <https://slack.com/app_redirect?channel=COLD|打开>", lines[3])
	assert.Equal(t, "<"+managerBaseURL()+"/manager/conversations?c="+conv.UUID+"|在后台查看> · 直接在此频道发言即回复访客；`!ai` 交还 AI，`!close` 关闭，其他 `!` 开头为内部备注", lines[4])

	email := vals[0] + "@example.com"
	require.NoError(t, addGuestEmail(ctx, gid, BrandKaitu, email))
	assert.Contains(t, chatSlackCardText(ctx, conv), "访客: "+strings.ToLower(email)+" · 游客 #")

	// 普通入口、无历史、登录用户
	plain := &Conversation{ID: 1 << 60, UUID: "u", Brand: string(BrandKaitu), SubjectKind: SubjectUser, SubjectID: 987654321987,
		Status: ConvOpen, Handler: HandlerHuman, LastMessageBy: SenderVisitor, EntryPath: "/support"}
	text = chatSlackCardText(ctx, plain)
	assert.NotContains(t, text, "🔥")
	assert.Contains(t, text, "🔴 *等待人工*")
	assert.Contains(t, text, "访客: 未留邮箱 · 用户 #987654321987")
	assert.Contains(t, text, "此前会话: 无\n")
}

func TestSlackArchive_FlushesThenArchives(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	conv = slackSetup(t, f, conv)
	slackSeed(t, conv, SenderSystem, MsgEvent, "会话已关闭", func(m *ConversationMessage) { m.Meta = chatEventMeta(ChatEventClosed) })
	require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", conv.ID).Update("status", ConvClosed).Error)

	require.NoError(t, chatSlackArchive(context.Background(), conv))
	assert.Equal(t, []string{"chat.postMessage", "chat.update", "chat.update", "conversations.archive"}, f.Methods())
	calls := f.Calls()
	assert.Equal(t, "ℹ️ 会话已关闭", calls[0].Str("text"))
	assert.Contains(t, calls[1].Str("text"), "⚪ *已关闭*")
	assert.Equal(t, conv.SlackChannelID, calls[3].Str("channel"))
}

func TestSlackMirror_ArchivedChannelDrains(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	conv = slackSetup(t, f, conv)
	slackSeed(t, conv, SenderSystem, MsgEvent, "归档后才来的事件")
	f.Script("chat.postMessage", fakeSlackErr("is_archived"))

	// 频道已归档就再也发不进去：标记为已处理，否则 sweep 会永远重试
	require.NoError(t, chatSlackMirror(context.Background(), conv.ID))
	assert.EqualValues(t, 0, slackUnmirrored(t, conv.ID))
	assert.Len(t, f.CallsOf("chat.postMessage"), 1)
}

func TestSlackDisabledWhenNoLobby(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	viper.Set("slack.chat_lobby_channel_id", "") // newFakeSlack 的 Cleanup 也会置空
	ctx := context.Background()
	require.NoError(t, setHandler(ctx, conv, HandlerHuman))
	_, _, err := appendMessage(ctx, conv, appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "你好"})
	require.NoError(t, err)

	require.NoError(t, chatSlackMirror(ctx, conv.ID))
	require.NoError(t, chatSlackRefreshCard(ctx, conv))
	require.NoError(t, chatSlackArchive(ctx, conv))
	n, err := chatSlackSweep(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, f.Calls())
	assert.EqualValues(t, 1, slackUnmirrored(t, conv.ID))
}

func TestSlackMirror_StopsAtFirstFailureKeepsOrder(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	conv = slackSetup(t, f, conv)
	for _, s := range []string{"1", "2", "3"} {
		slackSeed(t, conv, SenderVisitor, MsgText, s)
	}
	f.Script("chat.postMessage", fakeSlackResp{Body: `{"ok":true,"ts":"1700000009.000001"}`}, fakeSlackHTTP(500))

	require.Error(t, chatSlackMirror(context.Background(), conv.ID))
	assert.Equal(t, []string{"👤 1", "👤 2"}, f.Posts(conv.SlackChannelID), "第 2 条失败后不得跳去发第 3 条")
	assert.EqualValues(t, 2, slackUnmirrored(t, conv.ID), "第 1 条成功即标记，不等整批")

	require.NoError(t, chatSlackMirror(context.Background(), conv.ID))
	assert.Equal(t, []string{"👤 1", "👤 2", "👤 2", "👤 3"}, f.Posts(conv.SlackChannelID))
	assert.EqualValues(t, 0, slackUnmirrored(t, conv.ID))
}

func TestSlackMirror_LateMessageDuringRoundIsMirrored(t *testing.T) {
	conv := slackConv(t, "", "/support")
	ctx := context.Background()
	require.NoError(t, setHandler(ctx, conv, HandlerHuman))
	f := newFakeSlack(t)
	conv = slackSetup(t, f, conv)
	slackSeed(t, conv, SenderVisitor, MsgText, "先到")

	var once sync.Once
	f.OnCall(func(c fakeSlackCall) {
		if c.Method != "chat.postMessage" || c.Str("text") != "👤 先到" {
			return
		}
		// 本轮持锁期间又来一条：它自己的钩子拿不到锁直接返回，必须由持锁者放锁后复查补发
		once.Do(func() {
			late := *conv
			_, _, err := appendMessage(ctx, &late, appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "后到"})
			assert.NoError(t, err)
		})
	})
	require.NoError(t, chatSlackMirror(ctx, conv.ID))
	assert.Equal(t, []string{"👤 先到", "👤 后到"}, f.Posts(conv.SlackChannelID))
	assert.EqualValues(t, 0, slackUnmirrored(t, conv.ID))
}

func TestSlackRefreshCard_UpdatesCardAndLobby(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	ctx := context.Background()

	require.NoError(t, chatSlackRefreshCard(ctx, conv))
	assert.Empty(t, f.Calls(), "还没有状态卡时不做任何事")

	conv = slackSetup(t, f, conv)
	require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", conv.ID).
		Updates(map[string]any{"handler": HandlerHuman, "last_message_by": SenderVisitor}).Error)

	stale := &Conversation{ID: conv.ID} // 调用方手里的对象可能是旧的：以库为准
	require.NoError(t, chatSlackRefreshCard(ctx, stale))
	ups := f.CallsOf("chat.update")
	require.Len(t, ups, 2)
	assert.Equal(t, conv.SlackChannelID, ups[0].Str("channel"))
	assert.Equal(t, conv.SlackCardTS, ups[0].Str("ts"))
	assert.True(t, strings.HasPrefix(ups[0].Str("text"), "🔴 *等待人工*"), ups[0].Str("text"))
	assert.Equal(t, fakeSlackLobbyID, ups[1].Str("channel"))
	assert.Equal(t, conv.SlackLobbyTS, ups[1].Str("ts"))
	assert.True(t, strings.HasPrefix(ups[1].Str("text"), "🔴 <"), ups[1].Str("text"))
	assert.Contains(t, ups[1].Str("text"), "游客 #"+uintStr(conv.SubjectID)+" · 入口 /support")
}

func TestSlackHooks_MirrorOnAppendAndRefreshOnStateChange(t *testing.T) {
	conv := slackConv(t, "", "/support")
	ctx := context.Background()
	require.NoError(t, setHandler(ctx, conv, HandlerHuman))
	f := newFakeSlack(t)

	say := func(sender, content string) {
		_, _, err := appendMessage(ctx, conv, appendMessageInput{SenderType: sender, SenderName: "小李", Kind: MsgText, Content: content})
		require.NoError(t, err)
	}
	say(SenderVisitor, "你好") // 首条：钩子建频道并镜像
	got := slackReload(t, conv.ID)
	require.NotEmpty(t, got.SlackChannelID)
	assert.Equal(t, []string{"👤 你好"}, slackMsgPosts(f, got.SlackChannelID))

	f.Reset()
	say(SenderVisitor, "在吗") // last_message_by 没变：只镜像，不刷卡
	assert.Equal(t, []string{"chat.postMessage"}, f.Methods())

	f.Reset()
	say(SenderStaff, "在的") // visitor → staff：刷卡
	assert.Equal(t, []string{"chat.postMessage", "chat.update", "chat.update"}, f.Methods())
	assert.Contains(t, f.CallsOf("chat.update")[0].Str("text"), "🟡 *已回复待访客*")

	f.Reset()
	require.NoError(t, closeConversation(ctx, conv)) // 状态钩子总是刷卡
	ups := f.CallsOf("chat.update")
	require.Len(t, ups, 2)
	assert.Contains(t, ups[0].Str("text"), "⚪ *已关闭*")
}

func TestSlackMirror_NoInviteWhenLobbyOnlyHasBot(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	f.SetMembers(fakeSlackBotID)
	slackSeed(t, conv, SenderVisitor, MsgText, "你好")

	require.NoError(t, chatSlackMirror(context.Background(), conv.ID))
	assert.Empty(t, f.CallsOf("conversations.invite"))
	assert.Len(t, f.CallsOf("conversations.members"), 1)
	assert.Equal(t, []string{"👤 你好"}, slackMsgPosts(f, slackReload(t, conv.ID).SlackChannelID))
}

func TestSlackMirror_BotUserIDFailureAborts(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	slackSeed(t, conv, SenderVisitor, MsgText, "你好")
	f.Script("auth.test", fakeSlackErr("invalid_auth"))

	require.Error(t, chatSlackMirror(context.Background(), conv.ID))
	assert.Empty(t, f.CallsOf("conversations.invite"), "不知道 bot 是谁就不盲目拉人")
	assert.Empty(t, f.CallsOf("chat.postMessage"))
}

func TestSlackSweep_SkipsFreshMessages(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	ctx := context.Background()
	slackSeed(t, conv, SenderVisitor, MsgText, "刚发的")

	n, err := chatSlackSweepIn(ctx, string(brand))
	require.NoError(t, err)
	assert.Zero(t, n, "30 秒内的消息归实时路径，sweep 不抢")
	assert.Empty(t, f.Calls())

	require.NoError(t, db.Get().Model(&ConversationMessage{}).Where("conversation_id = ?", conv.ID).
		Update("created_at", time.Now().Add(-time.Minute)).Error)
	n, err = chatSlackSweepIn(ctx, string(brand))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.EqualValues(t, 0, slackUnmirrored(t, conv.ID))
}

// ---- 评审修复（第 1 轮） ----

// slackBackdate 把会话的消息挪到 age 之前（sweep 只碰 30 秒前、24 小时内的消息）。
func slackBackdate(t *testing.T, convID uint64, age time.Duration) {
	t.Helper()
	require.NoError(t, db.Get().Model(&ConversationMessage{}).Where("conversation_id = ?", convID).
		Update("created_at", time.Now().Add(-age)).Error)
}

// slackClose 直接把会话置为已关闭（不经钩子），closed_at 为 age 之前。
func slackClose(t *testing.T, convID uint64, age time.Duration) {
	t.Helper()
	require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", convID).
		Updates(map[string]any{"status": ConvClosed, "closed_at": time.Now().Add(-age)}).Error)
}

func TestSlackSweep_RefreshesCardAfterMirror(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	conv = slackSetup(t, f, conv)
	// 实时路径失败过：消息没发出去，卡片也停在旧状态（AI 接待中），而库里已是等待人工
	require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", conv.ID).
		Updates(map[string]any{"handler": HandlerHuman, "last_message_by": SenderVisitor}).Error)
	slackSeed(t, conv, SenderVisitor, MsgText, "补发")
	slackBackdate(t, conv.ID, time.Minute)

	n, err := chatSlackSweepIn(context.Background(), string(brand))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []string{"chat.postMessage", "chat.update", "chat.update"}, f.Methods())
	ups := f.CallsOf("chat.update")
	require.Len(t, ups, 2)
	assert.Contains(t, ups[0].Str("text"), "🔴 *等待人工*")
}

func TestSlackSweep_IgnoresOldBacklog(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	slackSeed(t, conv, SenderVisitor, MsgText, "开启镜像之前的历史消息")
	slackBackdate(t, conv.ID, 25*time.Hour)

	n, err := chatSlackSweepIn(context.Background(), string(brand))
	require.NoError(t, err)
	assert.Zero(t, n, "超过 24 小时的积压不碰：在有历史的库上打开镜像不能刷爆 Slack")
	assert.Empty(t, f.Calls())
	assert.EqualValues(t, 1, slackUnmirrored(t, conv.ID))
}

func TestSlackSweep_IndexExists(t *testing.T) {
	skipIfNoConfig(t)
	chatMigrated(t)
	assert.True(t, db.Get().Migrator().HasIndex(&ConversationMessage{}, "idx_msg_unmirrored"))
	assert.True(t, db.Get().Migrator().HasColumn(&Conversation{}, "slack_archived_at"))
	var name, ddl string
	require.NoError(t, db.Get().Raw("SHOW CREATE TABLE conversation_messages").Row().Scan(&name, &ddl))
	for _, line := range strings.Split(ddl, "\n") {
		if strings.Contains(line, "idx_msg_unmirrored") {
			t.Log(strings.TrimSpace(line))
		}
	}
}

func TestSlackArchive_RetriedBySweep(t *testing.T) {
	brand := slackTestBrand()
	conv := slackConv(t, brand, "/support")
	f := newFakeSlack(t)
	ctx := context.Background()
	conv = slackSetup(t, f, conv)
	slackSeed(t, conv, SenderSystem, MsgEvent, "会话已关闭", func(m *ConversationMessage) { m.Meta = chatEventMeta(ChatEventClosed) })
	slackBackdate(t, conv.ID, time.Minute)
	slackClose(t, conv.ID, time.Minute)

	f.Script("chat.postMessage", fakeSlackHTTP(500))
	require.Error(t, chatSlackArchive(ctx, conv))
	assert.Empty(t, f.CallsOf("conversations.archive"), "尾巴没发完不得归档")
	assert.Nil(t, slackReload(t, conv.ID).SlackArchivedAt)

	f.Reset()
	n, err := chatSlackSweepIn(ctx, string(brand))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []string{"ℹ️ 会话已关闭"}, f.Posts(conv.SlackChannelID))
	arch := f.CallsOf("conversations.archive")
	require.Len(t, arch, 1)
	assert.Equal(t, conv.SlackChannelID, arch[0].Str("channel"))
	assert.NotNil(t, slackReload(t, conv.ID).SlackArchivedAt)

	f.Reset()
	n, err = chatSlackSweepIn(ctx, string(brand))
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, f.Calls(), "归档成功后 sweep 不再碰它")
}

func TestSlackArchive_CardRefreshFailureDoesNotBlock(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	conv = slackSetup(t, f, conv)
	slackClose(t, conv.ID, 0)
	f.Script("chat.update", fakeSlackHTTP(500), fakeSlackHTTP(500))

	require.NoError(t, chatSlackArchive(context.Background(), conv))
	assert.Len(t, f.CallsOf("chat.update"), 2)
	assert.Len(t, f.CallsOf("conversations.archive"), 1)
	assert.NotNil(t, slackReload(t, conv.ID).SlackArchivedAt)
}

func TestSlackArchive_Idempotent(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	ctx := context.Background()
	conv = slackSetup(t, f, conv)
	slackClose(t, conv.ID, 0)

	require.NoError(t, chatSlackArchive(ctx, conv))
	require.Len(t, f.CallsOf("conversations.archive"), 1)
	f.Reset()
	require.NoError(t, chatSlackArchive(ctx, conv)) // 传入的 conv 是旧对象：以库里的 slack_archived_at 为准
	assert.Empty(t, f.Calls())
}

func TestSlackMirror_OrphanChannelArchivedWhenSaveFails(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	slackSeed(t, conv, SenderVisitor, MsgText, "你好")
	// slack_channel_id 是 varchar(32)：返回一个超长 id，让"建成功但落库失败"真实发生
	orphan := "C" + strings.Repeat("X", 40)
	f.Script("conversations.create", fakeSlackResp{Body: `{"ok":true,"channel":{"id":"` + orphan + `"}}`})

	require.Error(t, chatSlackMirror(context.Background(), conv.ID))
	assert.Empty(t, slackReload(t, conv.ID).SlackChannelID)
	arch := f.CallsOf("conversations.archive")
	require.Len(t, arch, 1, "没记下来的频道要尽力归档，免得留孤儿")
	assert.Equal(t, orphan, arch[0].Str("channel"))
	assert.Empty(t, f.CallsOf("chat.postMessage"))
}

func TestSlackRender_EscapesVisitorControlledText(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	vals := chatTestValues(t, 1)
	gid, err := resolveGuest(ctx, BrandKaitu, vals[0], "", "zh-CN", "CN")
	require.NoError(t, err)
	require.NoError(t, addGuestEmail(ctx, gid, BrandKaitu, "a<b>&"+vals[0]+"@e.com"))
	conv := &Conversation{ID: 1 << 60, UUID: "u", Brand: string(BrandKaitu), SubjectKind: SubjectGuest, SubjectID: gid,
		Status: ConvOpen, Handler: HandlerAI, EntryPath: "/x<!channel>&y", SlackChannelID: "C1"}

	view := chatSlackRender(ctx, conv)
	for name, text := range map[string]string{"card": view.card, "lobby": view.lobby, "topic": view.topic} {
		assert.NotContains(t, text, "<!channel>", name)
		assert.Contains(t, text, "/x&lt;!channel&gt;&amp;y", name)
		assert.NotContains(t, text, "a<b>", name)
		assert.Contains(t, text, "a&lt;b&gt;&amp;"+vals[0]+"@e.com", name)
	}

	text, post := chatSlackMessageText(&ConversationMessage{SenderType: SenderStaff, SenderName: "<@U1>&", Kind: MsgText, Content: "hi"})
	assert.True(t, post)
	assert.Equal(t, "🧑‍💼 &lt;@U1&gt;&amp;: hi", text)
}

func TestSlackMirror_LockLostMidRoundStops(t *testing.T) {
	conv := slackConv(t, "", "/support")
	f := newFakeSlack(t)
	ctx := context.Background()
	conv = slackSetup(t, f, conv)
	for _, s := range []string{"1", "2", "3"} {
		slackSeed(t, conv, SenderVisitor, MsgText, s)
	}
	key := "chat:slack:lock:" + uintStr(conv.ID)
	t.Cleanup(func() { redis.Client().Del(ctx, key) })
	f.OnCall(func(c fakeSlackCall) {
		if c.Str("text") == "👤 1" { // 发第 1 条时锁过期并被别的实例拿走
			require.NoError(t, redis.Client().Set(ctx, key, "someone-else", time.Minute).Err())
		}
	})

	err := chatSlackMirror(ctx, conv.ID)
	require.ErrorIs(t, err, errChatSlackLockLost)
	assert.Equal(t, []string{"👤 1"}, f.Posts(conv.SlackChannelID), "锁丢了就停，不与新持锁者并发发消息")
	assert.EqualValues(t, 2, slackUnmirrored(t, conv.ID))
	assert.Equal(t, "someone-else", redis.Client().Get(ctx, key).Val(), "放锁不得删掉别人的锁")

	f.OnCall(nil)
	require.NoError(t, redis.Client().Del(ctx, key).Err())
	require.NoError(t, chatSlackMirror(ctx, conv.ID))
	assert.Equal(t, []string{"👤 1", "👤 2", "👤 3"}, f.Posts(conv.SlackChannelID))
}
