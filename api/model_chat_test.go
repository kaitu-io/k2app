package center

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// 真 MySQL：访客身份与会话消息的唯一约束是后续合并/幂等逻辑的地基。
func TestChatModels_Migrate(t *testing.T) {
	skipIfNoConfig(t)
	chatMigrated(t)
	d := db.Get()
	now := time.Now()
	marker := generateId("chat")[:16] // brand 列 varchar(16)

	// 两个 guest
	g1 := &Guest{Brand: marker, FirstSeenAt: now, LastSeenAt: now}
	g2 := &Guest{Brand: marker, FirstSeenAt: now, LastSeenAt: now}
	require.NoError(t, d.Create(g1).Error)
	require.NoError(t, d.Create(g2).Error)
	t.Cleanup(func() {
		d.Where("brand = ?", marker).Delete(&GuestIdentity{})
		d.Where("brand = ?", marker).Delete(&Guest{})
	})

	ident := func(guestID uint64, kind, value string) *GuestIdentity {
		return &GuestIdentity{GuestID: guestID, Brand: marker, Kind: kind, Value: value,
			Strength: StrengthClaimed, FirstSeenAt: now, LastSeenAt: now}
	}

	// (a) 同一 (guest_id, kind, value) 第二次冲突
	email := marker + "@example.com"
	require.NoError(t, d.Create(ident(g1.ID, IdentityEmail, email)).Error)
	assert.Error(t, d.Create(ident(g1.ID, IdentityEmail, email)).Error)

	// (b) 同一邮箱可挂在两个不同 guest 上
	assert.NoError(t, d.Create(ident(g2.ID, IdentityEmail, email)).Error)

	// 会话 + 消息
	conv := &Conversation{UUID: generateId("conv"), Brand: marker, SubjectKind: SubjectGuest, SubjectID: g1.ID,
		Status: ConvOpen, Handler: HandlerAI, LastMessageAt: now}
	require.NoError(t, d.Create(conv).Error)
	t.Cleanup(func() {
		d.Where("conversation_id = ?", conv.ID).Delete(&ConversationMessage{})
		d.Delete(&Conversation{}, conv.ID)
	})
	msg := func(clientID, slackTS *string) *ConversationMessage {
		return &ConversationMessage{ConversationID: conv.ID, SenderType: SenderVisitor, Kind: MsgText,
			Content: "hi", ClientID: clientID, SlackTS: slackTS}
	}
	sp := func(s string) *string { return &s }

	// (c) ClientID=nil 可共存；相同非空 ClientID 第二条冲突
	require.NoError(t, d.Create(msg(nil, nil)).Error)
	assert.NoError(t, d.Create(msg(nil, nil)).Error)
	cid := generateId("cid")
	require.NoError(t, d.Create(msg(&cid, nil)).Error)
	assert.Error(t, d.Create(msg(&cid, nil)).Error)

	// (d) 同一会话内相同 SlackTS 第二条冲突
	ts := marker
	require.NoError(t, d.Create(msg(sp(generateId("c1")), &ts)).Error)
	assert.Error(t, d.Create(msg(sp(generateId("c2")), &ts)).Error)

	// (e) Slack 的 ts 只在频道内唯一：另一个会话（另一个频道）可以有相同的 ts
	conv2 := &Conversation{UUID: generateId("conv"), Brand: marker, SubjectKind: SubjectGuest, SubjectID: g2.ID,
		Status: ConvOpen, Handler: HandlerAI, LastMessageAt: now}
	require.NoError(t, d.Create(conv2).Error)
	t.Cleanup(func() {
		d.Where("conversation_id = ?", conv2.ID).Delete(&ConversationMessage{})
		d.Delete(&Conversation{}, conv2.ID)
	})
	other := msg(nil, &ts)
	other.ConversationID = conv2.ID
	assert.NoError(t, d.Create(other).Error)
}

// 索引形状：复合唯一 (conversation_id, slack_ts)；没有冗余的 conversation_id 单列索引与全局唯一的 slack_ts
// （后者在开发库里是旧模型留下的，AutoMigrate 不删——部署清单里有手工 DROP INDEX）；
// conversations 上有 sweep / 闲置关闭 / 补记关闭事件用的两个 status 前缀索引。
func TestChatModels_Indexes(t *testing.T) {
	skipIfNoConfig(t)
	chatMigrated(t)
	cols := func(table, index string) []string {
		var out []string
		require.NoError(t, db.Get().Raw(`SELECT COLUMN_NAME FROM information_schema.statistics
			WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ? ORDER BY SEQ_IN_INDEX`, table, index).
			Scan(&out).Error)
		return out
	}
	unique := func(table, index string) bool {
		var nonUnique []int
		require.NoError(t, db.Get().Raw(`SELECT NON_UNIQUE FROM information_schema.statistics
			WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ? LIMIT 1`, table, index).
			Scan(&nonUnique).Error)
		return len(nonUnique) == 1 && nonUnique[0] == 0
	}
	assert.Equal(t, []string{"conversation_id", "slack_ts"}, cols("conversation_messages", "uniq_conv_slack_ts"))
	assert.True(t, unique("conversation_messages", "uniq_conv_slack_ts"))
	assert.Equal(t, []string{"conversation_id", "client_id"}, cols("conversation_messages", "uniq_conv_client"))
	assert.Equal(t, []string{"slack_mirrored_at", "created_at"}, cols("conversation_messages", "idx_msg_unmirrored"))
	assert.Empty(t, cols("conversation_messages", "idx_conversation_messages_slack_ts"), "旧的全局唯一 slack_ts 索引必须删掉")
	assert.Empty(t, cols("conversation_messages", "idx_conversation_messages_conversation_id"), "冗余的单列索引必须删掉")
	assert.Equal(t, []string{"status", "closed_at"}, cols("conversations", "idx_conv_status_closed"))
	assert.Equal(t, []string{"status", "last_message_at"}, cols("conversations", "idx_conv_status_last_msg"))
}
