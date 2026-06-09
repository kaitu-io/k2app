package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func ptrU64(v uint64) *uint64 { return &v }

func TestAuthorizeNodeAccess_SharedNode(t *testing.T) {
	now := int64(1_000_000)
	user := &User{ID: 7, ExpiredAt: now + 3600} // 未过期

	shared := &SlaveNode{Class: NodeClassShared}
	assert.True(t, AuthorizeNodeAccess(user, shared, nil, now), "未过期用户应能用共享节点")

	expired := &User{ID: 7, ExpiredAt: now - 1}
	assert.False(t, AuthorizeNodeAccess(expired, shared, nil, now), "过期用户不能用共享节点")

	// 空 Class 视为 shared（兼容默认值）
	emptyClass := &SlaveNode{Class: ""}
	assert.True(t, AuthorizeNodeAccess(user, emptyClass, nil, now), "空 Class 应按 shared 处理")
}

func TestAuthorizeNodeAccess_PrivateNode(t *testing.T) {
	now := int64(1_000_000)
	owner := &User{ID: 7, ExpiredAt: now - 9999} // 注意：专属节点不看 User.ExpiredAt
	node := &SlaveNode{Class: NodeClassPrivate, PrivateOwnerUserID: ptrU64(7)}
	activeSub := &PrivateNodeSubscription{Status: PNStatusActive}

	assert.True(t, AuthorizeNodeAccess(owner, node, activeSub, now), "主人 + active 订阅应放行")

	stranger := &User{ID: 99, ExpiredAt: now + 3600}
	assert.False(t, AuthorizeNodeAccess(stranger, node, activeSub, now), "非主人必须拒绝")

	deprovisioned := &PrivateNodeSubscription{Status: PNStatusDeprovisioned}
	assert.False(t, AuthorizeNodeAccess(owner, node, deprovisioned, now), "已销毁订阅必须拒绝")

	grace := &PrivateNodeSubscription{Status: PNStatusGrace, GraceUntil: now + 100}
	assert.True(t, AuthorizeNodeAccess(owner, node, grace, now), "宽限期内应放行")

	graceExpired := &PrivateNodeSubscription{Status: PNStatusGrace, GraceUntil: now - 1}
	assert.False(t, AuthorizeNodeAccess(owner, node, graceExpired, now), "宽限期过应拒绝")

	noOwner := &SlaveNode{Class: NodeClassPrivate, PrivateOwnerUserID: nil}
	assert.False(t, AuthorizeNodeAccess(owner, noOwner, activeSub, now), "owner 缺失必须拒绝")

	assert.False(t, AuthorizeNodeAccess(owner, node, nil, now), "专属节点缺订阅必须拒绝")
}

func TestPrivateNodeSubscription_IsServiceable(t *testing.T) {
	now := int64(1_000_000)
	cases := []struct {
		name string
		sub  PrivateNodeSubscription
		want bool
	}{
		{"active", PrivateNodeSubscription{Status: PNStatusActive}, true},
		{"grace within", PrivateNodeSubscription{Status: PNStatusGrace, GraceUntil: now + 1}, true},
		{"grace expired", PrivateNodeSubscription{Status: PNStatusGrace, GraceUntil: now}, false},
		{"pending", PrivateNodeSubscription{Status: PNStatusPending}, false},
		{"provisioning", PrivateNodeSubscription{Status: PNStatusProvisioning}, false},
		{"suspended", PrivateNodeSubscription{Status: PNStatusSuspended}, false},
		{"deprovisioned", PrivateNodeSubscription{Status: PNStatusDeprovisioned}, false},
		{"failed", PrivateNodeSubscription{Status: PNStatusFailed}, false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.sub.IsServiceable(now), tc.name)
	}
}
