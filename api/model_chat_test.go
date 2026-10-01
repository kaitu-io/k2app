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
	require.NoError(t, Migrate())
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

	// (d) 相同 SlackTS 第二条冲突
	ts := marker
	require.NoError(t, d.Create(msg(sp(generateId("c1")), &ts)).Error)
	assert.Error(t, d.Create(msg(sp(generateId("c2")), &ts)).Error)
}
