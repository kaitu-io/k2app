package center

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var chatSubjectSeq atomic.Uint64

// newChatSubject 返回一个全新的 guest 主体（ID 唯一），测试结束时清掉它名下的会话与消息。
func newChatSubject(t *testing.T) chatSubject {
	t.Helper()
	return newChatSubjectBrand(t, BrandKaitu)
}

func newChatSubjectBrand(t *testing.T, brand Brand) chatSubject {
	t.Helper()
	chatMigrated(t)
	// 真实 guest 行：发布钩子要把 guest 解析到簇根，伪造的 id 会让每条消息都记一条警告
	id, err := resolveGuest(context.Background(), brand, generateId("nsj")+fmt.Sprint(chatSubjectSeq.Add(1)), "", "zh-CN", "CN")
	require.NoError(t, err)
	s := chatSubject{Brand: brand, Kind: SubjectGuest, ID: id}
	t.Cleanup(func() {
		d := db.Get()
		var ids []uint64
		d.Model(&Conversation{}).Where("brand = ? AND subject_kind = ? AND subject_id = ?", string(s.Brand), s.Kind, s.ID).Pluck("id", &ids)
		if len(ids) > 0 {
			d.Where("conversation_id IN ?", ids).Delete(&ConversationMessage{})
			d.Where("id IN ?", ids).Delete(&Conversation{})
		}
		d.Where("guest_id = ?", id).Delete(&GuestIdentity{})
		d.Where("id = ?", id).Delete(&Guest{})
	})
	return s
}

// withAppendHook 注册一个计数钩子，测试结束恢复原切片。
func withAppendHook(t *testing.T) *atomic.Int32 {
	t.Helper()
	orig := chatAfterAppend
	var n atomic.Int32
	chatAfterAppend = append(append([]func(*Conversation, *ConversationMessage){}, orig...),
		func(*Conversation, *ConversationMessage) { n.Add(1) })
	t.Cleanup(func() { chatAfterAppend = orig })
	return &n
}

func withStateHook(t *testing.T) *[]string {
	t.Helper()
	orig := chatAfterStateChange
	var mu sync.Mutex
	var seen []string
	chatAfterStateChange = append(append([]func(*Conversation){}, orig...), func(c *Conversation) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, c.UUID+":"+c.Status+":"+c.Handler)
	})
	t.Cleanup(func() { chatAfterStateChange = orig })
	return &seen
}

func TestChatSubject_Channel(t *testing.T) {
	assert.Equal(t, "s:kaitu:guest:42", chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: 42}.Channel())
}

func TestChatEventMeta(t *testing.T) {
	assert.JSONEq(t, `{"event":"closed"}`, chatEventMeta(ChatEventClosed))
}

func TestEnsureConversation_OnePerSubject(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	s := newChatSubject(t)

	got, err := openConversationFor(ctx, s)
	require.NoError(t, err)
	assert.Nil(t, got)

	c1, created, err := ensureConversation(ctx, s, "/support")
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, ConvOpen, c1.Status)
	assert.Equal(t, HandlerAI, c1.Handler)
	assert.Equal(t, "/support", c1.EntryPath)
	assert.Len(t, c1.UUID, 36)

	c2, created, err := ensureConversation(ctx, s, "/other")
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, c1.UUID, c2.UUID)

	open, err := openConversationFor(ctx, s)
	require.NoError(t, err)
	require.NotNil(t, open)
	assert.Equal(t, c1.ID, open.ID)

	require.NoError(t, closeConversation(ctx, c1))
	c3, created, err := ensureConversation(ctx, s, "")
	require.NoError(t, err)
	assert.True(t, created)
	assert.NotEqual(t, c1.UUID, c3.UUID)
}

func TestEnsureConversation_ConcurrentCreatesOne(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	s := newChatSubject(t)

	const n = 10
	var wg sync.WaitGroup
	var createdCount atomic.Int32
	uuids := make([]string, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			c, created, err := ensureConversation(ctx, s, "")
			errs[i] = err
			if err == nil {
				uuids[i] = c.UUID
			}
			if created {
				createdCount.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, createdCount.Load())
	for _, u := range uuids {
		assert.Equal(t, uuids[0], u)
	}
	var cnt int64
	db.Get().Model(&Conversation{}).Where("brand = ? AND subject_kind = ? AND subject_id = ? AND status = ?",
		string(s.Brand), s.Kind, s.ID, ConvOpen).Count(&cnt)
	assert.EqualValues(t, 1, cnt)
}

func strp(s string) *string { return &s }

func TestAppendMessage_ClientIDIdempotent(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	hook := withAppendHook(t)

	in := appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "hi", ClientID: strp(generateId("cm"))}
	m1, dup, err := appendMessage(ctx, conv, in)
	require.NoError(t, err)
	assert.False(t, dup)
	m2, dup, err := appendMessage(ctx, conv, in)
	require.NoError(t, err)
	assert.True(t, dup)
	assert.Equal(t, m1.ID, m2.ID)

	var cnt int64
	db.Get().Model(&ConversationMessage{}).Where("conversation_id = ?", conv.ID).Count(&cnt)
	assert.EqualValues(t, 1, cnt)
	assert.EqualValues(t, 1, hook.Load())
}

func TestAppendMessage_ClientIDIdempotentConcurrent(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	hook := withAppendHook(t)

	in := appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "hi", ClientID: strp(generateId("cm"))}
	const n = 8
	var wg sync.WaitGroup
	var dups atomic.Int32
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c := *conv
			_, dup, err := appendMessage(ctx, &c, in)
			assert.NoError(t, err)
			if dup {
				dups.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	var cnt int64
	db.Get().Model(&ConversationMessage{}).Where("conversation_id = ?", conv.ID).Count(&cnt)
	assert.EqualValues(t, 1, cnt)
	assert.EqualValues(t, n-1, dups.Load())
	assert.EqualValues(t, 1, hook.Load())
}

func TestAppendMessage_SlackTSIdempotent(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	hook := withAppendHook(t)

	in := appendMessageInput{SenderType: SenderStaff, Kind: MsgText, Content: "reply", SlackTS: strp(generateId("ts"))}
	m1, dup, err := appendMessage(ctx, conv, in)
	require.NoError(t, err)
	assert.False(t, dup)
	m2, dup, err := appendMessage(ctx, conv, in)
	require.NoError(t, err)
	assert.True(t, dup)
	assert.Equal(t, m1.ID, m2.ID)

	var cnt int64
	db.Get().Model(&ConversationMessage{}).Where("conversation_id = ?", conv.ID).Count(&cnt)
	assert.EqualValues(t, 1, cnt)
	assert.EqualValues(t, 1, hook.Load())
}

func TestAppendMessage_UpdatesLastMessage(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", conv.ID).
		Update("last_message_at", time.Now().Add(-time.Hour)).Error)
	conv.LastMessageAt = time.Now().Add(-time.Hour)

	msg, _, err := appendMessage(ctx, conv, appendMessageInput{SenderType: SenderAI, Kind: MsgText, Content: "x"})
	require.NoError(t, err)
	assert.Equal(t, SenderAI, conv.LastMessageBy)
	assert.WithinDuration(t, msg.CreatedAt, conv.LastMessageAt, time.Second)

	var fresh Conversation
	require.NoError(t, db.Get().First(&fresh, conv.ID).Error)
	assert.Equal(t, SenderAI, fresh.LastMessageBy)
	assert.WithinDuration(t, time.Now(), fresh.LastMessageAt, 5*time.Second)
}

func TestAppendMessage_NoteDoesNotChangeLastMessageBy(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)

	_, _, err = appendMessage(ctx, conv, appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "hello"})
	require.NoError(t, err)
	_, _, err = appendMessage(ctx, conv, appendMessageInput{SenderType: SenderStaff, Kind: MsgNote, Content: "internal"})
	require.NoError(t, err)
	assert.Equal(t, SenderVisitor, conv.LastMessageBy)
	_, _, err = appendMessage(ctx, conv, appendMessageInput{SenderType: SenderSystem, Kind: MsgEvent, Meta: chatEventMeta(ChatEventTransferHuman)})
	require.NoError(t, err)
	assert.Equal(t, SenderVisitor, conv.LastMessageBy)

	var fresh Conversation
	require.NoError(t, db.Get().First(&fresh, conv.ID).Error)
	assert.Equal(t, SenderVisitor, fresh.LastMessageBy)
}

func TestAppendMessage_AllowedOnClosedConversation(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	require.NoError(t, closeConversation(ctx, conv))

	msg, dup, err := appendMessage(ctx, conv, appendMessageInput{SenderType: SenderSystem, Kind: MsgEvent, Meta: chatEventMeta(ChatEventClosed)})
	require.NoError(t, err)
	assert.False(t, dup)
	assert.NotZero(t, msg.ID)
}

func TestMessagesAfter_HidesNotesFromVisitor(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)

	m1, _, err := appendMessage(ctx, conv, appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "a"})
	require.NoError(t, err)
	_, _, err = appendMessage(ctx, conv, appendMessageInput{SenderType: SenderStaff, Kind: MsgNote, Content: "n"})
	require.NoError(t, err)
	_, _, err = appendMessage(ctx, conv, appendMessageInput{SenderType: SenderStaff, Kind: MsgText, Content: "b"})
	require.NoError(t, err)

	v, err := messagesAfter(ctx, conv.ID, 0, true)
	require.NoError(t, err)
	assert.Len(t, v, 2)
	all, err := messagesAfter(ctx, conv.ID, 0, false)
	require.NoError(t, err)
	assert.Len(t, all, 3)
	assert.Less(t, all[0].ID, all[1].ID)
	assert.Less(t, all[1].ID, all[2].ID)

	v, err = messagesAfter(ctx, conv.ID, m1.ID, true)
	require.NoError(t, err)
	assert.Len(t, v, 1)
	all, err = messagesAfter(ctx, conv.ID, m1.ID, false)
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestMessagesAfter_CapsAt200(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	rows := make([]ConversationMessage, 205)
	for i := range rows {
		rows[i] = ConversationMessage{ConversationID: conv.ID, SenderType: SenderVisitor, Kind: MsgText, Content: "x", CreatedAt: time.Now()}
	}
	require.NoError(t, db.Get().CreateInBatches(&rows, 100).Error)
	got, err := messagesAfter(ctx, conv.ID, 0, false)
	require.NoError(t, err)
	assert.Len(t, got, 200)
	assert.Equal(t, rows[0].ID, got[0].ID)
}

func TestSetHandler_NotifiesStateChange(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	seen := withStateHook(t)

	require.NoError(t, setHandler(ctx, conv, HandlerHuman))
	assert.Equal(t, HandlerHuman, conv.Handler)
	assert.Equal(t, []string{conv.UUID + ":open:human"}, *seen)
	var fresh Conversation
	require.NoError(t, db.Get().First(&fresh, conv.ID).Error)
	assert.Equal(t, HandlerHuman, fresh.Handler)

	// 无变化不通知
	require.NoError(t, setHandler(ctx, conv, HandlerHuman))
	assert.Len(t, *seen, 1)
	assert.Error(t, setHandler(ctx, conv, "bogus"))
}

func TestCloseConversation_SetsClosedAtAndNotifiesOnce(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	seen := withStateHook(t)

	require.NoError(t, closeConversation(ctx, conv))
	assert.Equal(t, ConvClosed, conv.Status)
	require.NotNil(t, conv.ClosedAt)
	var fresh Conversation
	require.NoError(t, db.Get().First(&fresh, conv.ID).Error)
	assert.Equal(t, ConvClosed, fresh.Status)
	assert.NotNil(t, fresh.ClosedAt)
	assert.Len(t, *seen, 1)

	require.NoError(t, closeConversation(ctx, conv))
	assert.Len(t, *seen, 1)
}

func TestCloseIdleConversations(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	specs := []struct {
		handler string
		ago     time.Duration
		closed  bool
	}{
		{HandlerAI, 25 * time.Hour, true},
		{HandlerAI, time.Hour, false},
		{HandlerHuman, 25 * time.Hour, false},
		{HandlerHuman, 73 * time.Hour, true},
	}
	convs := make([]*Conversation, len(specs))
	brand := Brand(fmt.Sprintf("tb%d", time.Now().UnixNano()%1_000_000_000_000))
	for i, sp := range specs {
		c, _, err := ensureConversation(ctx, newChatSubjectBrand(t, brand), "")
		require.NoError(t, err)
		require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", c.ID).
			Updates(map[string]any{"handler": sp.handler, "last_message_at": time.Now().Add(-sp.ago)}).Error)
		convs[i] = c
	}
	seen := withStateHook(t)

	// 限定在本测试专属 brand，不碰库里其它会话
	closed, err := closeIdleConversationsIn(ctx, 24*time.Hour, 72*time.Hour, string(brand))
	require.NoError(t, err)
	got := map[uint64]Conversation{}
	for _, c := range closed {
		got[c.ID] = c
	}
	notified := map[string]bool{}
	for _, s := range *seen {
		notified[s[:36]] = true
	}
	for i, sp := range specs {
		var fresh Conversation
		require.NoError(t, db.Get().First(&fresh, convs[i].ID).Error)
		_, inResult := got[convs[i].ID]
		assert.Equal(t, sp.closed, inResult, "spec %d returned", i)
		assert.Equal(t, sp.closed, fresh.Status == ConvClosed, "spec %d status", i)
		assert.Equal(t, sp.closed, notified[convs[i].UUID], "spec %d notified", i)
		if sp.closed {
			assert.NotNil(t, fresh.ClosedAt)
			assert.Equal(t, ConvClosed, got[convs[i].ID].Status)
		}
	}
}

func TestChatAsyncDefault_RecoversPanic(t *testing.T) {
	done := make(chan struct{})
	chatAsyncDefault(func() { panic("boom") })
	chatAsyncDefault(func() { close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("following async func did not run")
	}
}

func TestChatHooks_PanicIsolated(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)

	origA, origS := chatAfterAppend, chatAfterStateChange
	t.Cleanup(func() { chatAfterAppend, chatAfterStateChange = origA, origS })
	var appendRan, stateRan atomic.Int32
	chatAfterAppend = []func(*Conversation, *ConversationMessage){
		func(*Conversation, *ConversationMessage) { panic("append hook") },
		func(*Conversation, *ConversationMessage) { appendRan.Add(1) },
	}
	chatAfterStateChange = []func(*Conversation){
		func(*Conversation) { panic("state hook") },
		func(*Conversation) { stateRan.Add(1) },
	}

	msg, dup, err := appendMessage(ctx, conv, appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "x"})
	require.NoError(t, err)
	assert.False(t, dup)
	assert.NotZero(t, msg.ID)
	assert.EqualValues(t, 1, appendRan.Load())

	require.NoError(t, setHandler(ctx, conv, HandlerHuman))
	assert.EqualValues(t, 1, stateRan.Load())
	require.NoError(t, closeConversation(ctx, conv))
	assert.EqualValues(t, 2, stateRan.Load())
}

// Slack 的 ts 只在频道内唯一：两个会话（两个频道）出现相同的 ts 各自落库，互不当作重复。
func TestAppendMessage_SlackTSUniquePerConversation(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	c1, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	c2, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	hook := withAppendHook(t)

	ts := strp(generateId("ts"))
	m1, dup, err := appendMessage(ctx, c1, appendMessageInput{SenderType: SenderStaff, Kind: MsgText, Content: "a", SlackTS: ts})
	require.NoError(t, err)
	assert.False(t, dup)
	m2, dup, err := appendMessage(ctx, c2, appendMessageInput{SenderType: SenderStaff, Kind: MsgText, Content: "b", SlackTS: ts})
	require.NoError(t, err)
	assert.False(t, dup, "别的会话里的同一 ts 不是重复")
	assert.NotEqual(t, m1.ID, m2.ID)
	assert.Equal(t, c2.ID, m2.ConversationID)
	assert.EqualValues(t, 2, hook.Load())

	// 各自会话内仍幂等，且回查到的是本会话那条
	again, dup, err := appendMessage(ctx, c2, appendMessageInput{SenderType: SenderStaff, Kind: MsgText, Content: "b", SlackTS: ts})
	require.NoError(t, err)
	assert.True(t, dup)
	assert.Equal(t, m2.ID, again.ID)
}

func TestSetHandler_StaleStructStillPersists(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	// 另一路已把 DB 改成 human；本结构体仍是陈旧的 ai
	require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", conv.ID).Update("handler", HandlerHuman).Error)
	seen := withStateHook(t)

	// 结构体说 ai、DB 是 human，要求切回 ai：必须落库并通知
	require.NoError(t, setHandler(ctx, conv, HandlerAI))
	var fresh Conversation
	require.NoError(t, db.Get().First(&fresh, conv.ID).Error)
	assert.Equal(t, HandlerAI, fresh.Handler)
	assert.Len(t, *seen, 1)

	// 结构体与目标相同但 DB 不同：同样要落库
	require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", conv.ID).Update("handler", HandlerHuman).Error)
	conv.Handler = HandlerAI
	require.NoError(t, setHandler(ctx, conv, HandlerAI))
	require.NoError(t, db.Get().First(&fresh, conv.ID).Error)
	assert.Equal(t, HandlerAI, fresh.Handler)
	assert.Len(t, *seen, 2)

	// DB 已是目标值：不通知
	require.NoError(t, setHandler(ctx, conv, HandlerAI))
	assert.Len(t, *seen, 2)
}

// 并发 append 同一会话：任一消息 X 的钩子触发时，该会话 id < X 的消息必须都已对新读可见，
// 否则按游标读取的访客会永久漏掉那条（提交顺序与 id 顺序颠倒）。
func TestAppendMessage_ConcurrentCommitOrderMatchesIdOrder(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()

	// 在插入与更新之间随机停顿，把竞态窗口放大到可稳定观察
	chatAppendMidTx = func() { time.Sleep(time.Duration(rand.Intn(15)) * time.Millisecond) }
	t.Cleanup(func() { chatAppendMidTx = nil })

	for iter := 0; iter < 5; iter++ {
		conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
		require.NoError(t, err)

		type obs struct {
			id      uint64
			visible map[uint64]bool
		}
		var mu sync.Mutex
		var observed []obs
		orig := chatAfterAppend
		chatAfterAppend = append(append([]func(*Conversation, *ConversationMessage){}, orig...),
			func(c *Conversation, m *ConversationMessage) {
				if c.ID != conv.ID {
					return
				}
				var ids []uint64
				db.Get().Model(&ConversationMessage{}).Where("conversation_id = ?", c.ID).Pluck("id", &ids)
				vis := map[uint64]bool{}
				for _, id := range ids {
					vis[id] = true
				}
				mu.Lock()
				observed = append(observed, obs{m.ID, vis})
				mu.Unlock()
			})

		const n = 8
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c := *conv
				_, _, err := appendMessage(ctx, &c, appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "x"})
				assert.NoError(t, err)
			}()
		}
		wg.Wait()
		chatAfterAppend = orig

		var finalIDs []uint64
		require.NoError(t, db.Get().Model(&ConversationMessage{}).Where("conversation_id = ?", conv.ID).Pluck("id", &finalIDs).Error)
		require.Len(t, finalIDs, n)
		require.Len(t, observed, n)
		for _, o := range observed {
			for _, id := range finalIDs {
				if id < o.id {
					assert.True(t, o.visible[id], "iter %d: hook for %d ran before lower id %d was visible", iter, o.id, id)
				}
			}
		}
	}
}

// ---- 终审修复：关闭竞态 ----

// 内存里的会话还是 open、库里已关闭：RequireOpen 的追加不落库、不触发钩子。
func TestAppendMessage_RequireOpenRejectsClosed(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	stale := *conv
	require.NoError(t, closeConversation(ctx, conv))
	hook := withAppendHook(t)

	for _, sender := range []string{SenderVisitor, SenderStaff, SenderAI} {
		c := stale
		msg, dup, err := appendMessage(ctx, &c, appendMessageInput{SenderType: sender, Kind: MsgText, Content: "late", RequireOpen: true})
		assert.ErrorIs(t, err, errChatConversationClosed, sender)
		assert.Nil(t, msg)
		assert.False(t, dup)
	}
	var cnt int64
	require.NoError(t, db.Get().Model(&ConversationMessage{}).Where("conversation_id = ?", conv.ID).Count(&cnt).Error)
	assert.Zero(t, cnt, "已关闭的会话不得再落发言")
	assert.Zero(t, hook.Load())

	// 对照：open 会话上 RequireOpen 正常追加
	open, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	_, _, err = appendMessage(ctx, open, appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "ok", RequireOpen: true})
	require.NoError(t, err)
}

// 访客重发一条已经落在（现已关闭的）会话里的消息：按 clientId 认出是重复，不当成"撞上关闭"。
func TestAppendMessage_RequireOpenClosedStillDedupes(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	in := appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "once", ClientID: strp("retry-1"), RequireOpen: true}
	first, _, err := appendMessage(ctx, conv, in)
	require.NoError(t, err)
	stale := *conv
	require.NoError(t, closeConversation(ctx, conv))

	again, dup, err := appendMessage(ctx, &stale, in)
	require.NoError(t, err)
	assert.True(t, dup)
	assert.Equal(t, first.ID, again.ID)
}

// CloseConversation：事件与关闭在同一事务里完成；会话已关闭时不再落第二条事件。
func TestAppendMessage_CloseConversationAtomic(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	conv, _, err := ensureConversation(ctx, newChatSubject(t), "")
	require.NoError(t, err)
	stale := *conv
	in := appendMessageInput{SenderType: SenderSystem, Kind: MsgEvent, Content: "会话已关闭",
		Meta: chatEventMeta(ChatEventClosed), CloseConversation: true}

	ev, _, err := appendMessage(ctx, conv, in)
	require.NoError(t, err)
	assert.Equal(t, ConvClosed, conv.Status, "传入的结构体同步更新")
	require.NotNil(t, conv.ClosedAt)
	var fresh Conversation
	require.NoError(t, db.Get().First(&fresh, conv.ID).Error)
	assert.Equal(t, ConvClosed, fresh.Status)
	assert.NotNil(t, fresh.ClosedAt)

	_, _, err = appendMessage(ctx, &stale, in)
	assert.ErrorIs(t, err, errChatConversationClosed)
	var ids []uint64
	require.NoError(t, db.Get().Model(&ConversationMessage{}).Where("conversation_id = ?", conv.ID).Pluck("id", &ids).Error)
	assert.Equal(t, []uint64{ev.ID}, ids)
}

// chatIdleConv 建一个闲置了 age 的会话（专属品牌，closeIdleConversationsIn 不碰库里别的会话）。
func chatIdleConv(t *testing.T, brand Brand, handler string, age time.Duration) (chatSubject, *Conversation) {
	t.Helper()
	subj := newChatSubjectBrand(t, brand)
	conv, _, err := ensureConversation(context.Background(), subj, "")
	require.NoError(t, err)
	at := time.Now().Add(-age)
	require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", conv.ID).
		Updates(map[string]any{"handler": handler, "last_message_at": at}).Error)
	conv.Handler, conv.LastMessageAt = handler, at
	return subj, conv
}

func chatTestBrand() Brand {
	return Brand(fmt.Sprintf("tc%d", time.Now().UnixNano()%1_000_000_000_000))
}

// 候选查询之后、关闭之前访客又发了消息：会话已被唤醒，不得被闲置关闭。
func TestCloseIdle_SkipsConversationWokenAfterScan(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	brand := chatTestBrand()
	_, conv := chatIdleConv(t, brand, HandlerAI, 25*time.Hour)

	orig := chatCloseIdleOne
	t.Cleanup(func() { chatCloseIdleOne = orig })
	chatCloseIdleOne = func(d *gorm.DB, c *Conversation, cutoff time.Time) (bool, error) {
		if c.ID == conv.ID {
			woke := *conv
			_, _, err := appendMessage(ctx, &woke, appendMessageInput{SenderType: SenderVisitor, Kind: MsgText, Content: "我回来了", RequireOpen: true})
			require.NoError(t, err)
		}
		return orig(d, c, cutoff)
	}
	closed, err := closeIdleConversationsIn(ctx, 24*time.Hour, 72*time.Hour, string(brand))
	require.NoError(t, err)
	assert.Empty(t, closed)
	var fresh Conversation
	require.NoError(t, db.Get().First(&fresh, conv.ID).Error)
	assert.Equal(t, ConvOpen, fresh.Status, "被唤醒的会话不得被关闭")
}

// 候选查询之后被人工接手（handler 变了）：按 AI 的 24 小时判出来的候选不得按人工会话关掉。
func TestCloseIdle_SkipsConversationTakenOverAfterScan(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	brand := chatTestBrand()
	_, conv := chatIdleConv(t, brand, HandlerAI, 25*time.Hour)

	orig := chatCloseIdleOne
	t.Cleanup(func() { chatCloseIdleOne = orig })
	chatCloseIdleOne = func(d *gorm.DB, c *Conversation, cutoff time.Time) (bool, error) {
		require.NoError(t, db.Get().Model(&Conversation{}).Where("id = ?", c.ID).Update("handler", HandlerHuman).Error)
		return orig(d, c, cutoff)
	}
	closed, err := closeIdleConversationsIn(ctx, 24*time.Hour, 72*time.Hour, string(brand))
	require.NoError(t, err)
	assert.Empty(t, closed)
	var fresh Conversation
	require.NoError(t, db.Get().First(&fresh, conv.ID).Error)
	assert.Equal(t, ConvOpen, fresh.Status)
}

// 一个会话关闭失败不中断整批：后面的照常关闭，错误汇总返回。
func TestCloseIdle_OneFailureDoesNotStopBatch(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	brand := chatTestBrand()
	_, first := chatIdleConv(t, brand, HandlerAI, 25*time.Hour)
	_, second := chatIdleConv(t, brand, HandlerAI, 25*time.Hour)
	require.Less(t, first.ID, second.ID)

	orig := chatCloseIdleOne
	t.Cleanup(func() { chatCloseIdleOne = orig })
	chatCloseIdleOne = func(d *gorm.DB, c *Conversation, cutoff time.Time) (bool, error) {
		if c.ID == first.ID {
			return false, errors.New("boom")
		}
		return orig(d, c, cutoff)
	}
	closed, err := closeIdleConversationsIn(ctx, 24*time.Hour, 72*time.Hour, string(brand))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
	require.Len(t, closed, 1)
	assert.Equal(t, second.ID, closed[0].ID)
	var fresh Conversation
	require.NoError(t, db.Get().First(&fresh, first.ID).Error)
	assert.Equal(t, ConvOpen, fresh.Status)
}

// chatCloseEventIndex 返回会话消息里第一条关闭事件（closed / auto_closed）的下标；没有返回 -1。
func chatCloseEventIndex(msgs []ConversationMessage) int {
	metas := chatCloseEventMetas()
	for i, m := range msgs {
		if m.Kind == MsgEvent && (m.Meta == metas[0] || m.Meta == metas[1]) {
			return i
		}
	}
	return -1
}

// 关闭与访客发送并发（产品硬要求：任何消息都要进 Slack，而归档后的频道发不进去）：
// 每条访客消息要么在关闭事件之前进了原会话，要么进了新会话，绝不落在原会话的关闭事件之后。
func TestChatVisitorAppend_ConcurrentWithClose(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	chatAppendMidTx = func() { time.Sleep(time.Duration(rand.Intn(4)) * time.Millisecond) }
	t.Cleanup(func() { chatAppendMidTx = nil })
	chatConvCreateLimiter.reset()
	t.Cleanup(chatConvCreateLimiter.reset)
	oldLimit := chatConvCreateLimiter.limit
	chatConvCreateLimiter.limit = 1 << 20
	t.Cleanup(func() { chatConvCreateLimiter.limit = oldLimit })

	for _, mode := range []string{"staff", "idle"} {
		t.Run(mode, func(t *testing.T) {
			brand := chatTestBrand()
			for iter := 0; iter < 10; iter++ {
				subj, conv := chatIdleConv(t, brand, HandlerAI, 25*time.Hour)

				const n = 4
				var wg sync.WaitGroup
				sent := make([]*ConversationMessage, n)
				for i := 0; i < n; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						time.Sleep(time.Duration(rand.Intn(8)) * time.Millisecond)
						c := *conv
						cid := fmt.Sprintf("cc-%d-%d", iter, i)
						_, m, err := chatVisitorAppend(ctx, subj, &c, appendMessageInput{
							SenderType: SenderVisitor, Kind: MsgText, Content: cid, ClientID: &cid})
						if assert.NoError(t, err) {
							sent[i] = m
						}
					}()
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					time.Sleep(time.Duration(rand.Intn(8)) * time.Millisecond)
					if mode == "staff" {
						c := *conv
						_, err := chatCloseByStaff(ctx, &c)
						assert.NoError(t, err)
					} else {
						_, err := chatCloseIdleRun(ctx, string(brand))
						assert.NoError(t, err)
					}
				}()
				wg.Wait()

				var old []ConversationMessage
				require.NoError(t, db.Get().Where("conversation_id = ?", conv.ID).Order("id").Find(&old).Error)
				var fresh Conversation
				require.NoError(t, db.Get().First(&fresh, conv.ID).Error)
				at := chatCloseEventIndex(old)
				if mode == "staff" {
					require.Equal(t, ConvClosed, fresh.Status, "iter %d", iter)
					require.GreaterOrEqual(t, at, 0, "iter %d: 人工关闭必有 closed 事件", iter)
				}
				if at >= 0 {
					for _, m := range old[at+1:] {
						assert.NotEqual(t, SenderVisitor, m.SenderType,
							"iter %d: 访客消息 %d 落在已关闭会话的关闭事件之后", iter, m.ID)
					}
				}
				for i, m := range sent {
					if m == nil {
						continue
					}
					var owner Conversation
					require.NoError(t, db.Get().First(&owner, m.ConversationID).Error)
					assert.Equal(t, subj.ID, owner.SubjectID)
					if owner.ID != conv.ID {
						assert.Greater(t, owner.ID, conv.ID, "iter %d msg %d: 进的是新开的会话", iter, i)
					} else if fresh.Status == ConvClosed && fresh.ClosedAt != nil {
						assert.False(t, m.CreatedAt.After(*fresh.ClosedAt), "iter %d msg %d: 关闭之后的消息不得留在原会话", iter, i)
					}
				}
			}
		})
	}
}

// sqlCapture 是只记 SQL 文本的 gorm logger。
type sqlCapture struct {
	logger.Interface
	mu   sync.Mutex
	sqls []string
}

func (c *sqlCapture) LogMode(logger.LogLevel) logger.Interface { return c }
func (c *sqlCapture) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	c.mu.Lock()
	c.sqls = append(c.sqls, sql)
	c.mu.Unlock()
}

var chatConvUpdateByPK = regexp.MustCompile("^UPDATE `conversations` SET .* WHERE id = \\d+$")

// 关闭与追加的加锁顺序必须一致：都先锁会话主键行。关闭的 UPDATE 若在 WHERE 里带 status / last_message_at
// 这些有二级索引的列，MariaDB 有时会走二级索引（idx_conv_status_*）——先锁索引记录再等主键行；而追加事务
// 先锁主键行、提交前再改同一条索引记录。两边互等即死锁（实测 InnoDB 报 1213，被回滚的可能是访客那条消息）。
// 走不走二级索引由优化器按统计信息定，复现不稳定，所以这里直接锁死语句形状：
// 条件在事务里对主键行 FOR UPDATE 之后用 Go 判断，UPDATE 只许按主键。
// 并发压测 TestChatVisitorAppend_ConcurrentWithClose 是另一道网（约七次里红一次）。
func TestCloseConversation_UpdatesByPrimaryKeyOnly(t *testing.T) {
	skipIfNoConfig(t)
	ctx := context.Background()
	closers := map[string]func(d *gorm.DB, c *Conversation) (bool, error){
		"close": closeConversationIn,
		"idle": func(d *gorm.DB, c *Conversation) (bool, error) {
			return closeIdleConversationIn(d, c, time.Now().Add(-24*time.Hour))
		},
	}
	for name, closer := range closers {
		t.Run(name, func(t *testing.T) {
			_, conv := chatIdleConv(t, chatTestBrand(), HandlerAI, 25*time.Hour)
			capture := &sqlCapture{Interface: logger.Discard}
			changed, err := closer(db.Get().WithContext(ctx).Session(&gorm.Session{Logger: capture}), conv)
			require.NoError(t, err)
			require.True(t, changed)

			var updates, locks int
			for _, sql := range capture.sqls {
				if strings.HasPrefix(sql, "UPDATE") {
					updates++
					assert.Regexp(t, chatConvUpdateByPK, sql, "关闭的 UPDATE 只许按主键")
				}
				if strings.Contains(sql, "FOR UPDATE") {
					locks++
					assert.Contains(t, sql, "WHERE `conversations`.`id` = ", "先按主键锁行")
					assert.NotContains(t, sql, " AND ")
				}
			}
			assert.Equal(t, 1, updates, "对照：确实抓到了 UPDATE: %v", capture.sqls)
			assert.Equal(t, 1, locks, "对照：确实抓到了行锁: %v", capture.sqls)
		})
	}
}
