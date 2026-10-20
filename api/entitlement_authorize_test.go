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
	activeSub := &PrivateNodeSubscription{Status: PNStatusActive, ExpiresAt: now + 3600}

	assert.True(t, AuthorizeNodeAccess(owner, node, activeSub, now), "主人 + active 订阅应放行")

	stranger := &User{ID: 99, ExpiredAt: now + 3600}
	assert.False(t, AuthorizeNodeAccess(stranger, node, activeSub, now), "非主人必须拒绝")

	deprovisioned := &PrivateNodeSubscription{Status: PNStatusDeprovisioned}
	assert.False(t, AuthorizeNodeAccess(owner, node, deprovisioned, now), "已销毁订阅必须拒绝")

	grace := &PrivateNodeSubscription{Status: PNStatusGrace, ExpiresAt: now - 86400} // 期满 1 天，仍在 7d 宽限内
	assert.True(t, AuthorizeNodeAccess(owner, node, grace, now), "宽限期内应放行")

	graceExpired := &PrivateNodeSubscription{Status: PNStatusGrace, ExpiresAt: now - 8*86400} // 超 7d 宽限
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
		// 时间戳权威：active/grace 服务到 ExpiresAt + 7d 宽限期为止。
		{"active not yet expired", PrivateNodeSubscription{Status: PNStatusActive, ExpiresAt: now + 3600}, true},
		{"active 1h past expiry within grace", PrivateNodeSubscription{Status: PNStatusActive, ExpiresAt: now - 3600}, true},
		// 关键新断言：active 但已过期超过宽限期 —— 旧代码无条件返回 true（永久免费漏洞）。
		{"active 8d past expiry beyond grace", PrivateNodeSubscription{Status: PNStatusActive, ExpiresAt: now - 8*86400}, false},
		{"grace 1d past expiry within grace", PrivateNodeSubscription{Status: PNStatusGrace, ExpiresAt: now - 86400}, true},
		{"grace 8d past expiry beyond grace", PrivateNodeSubscription{Status: PNStatusGrace, ExpiresAt: now - 8*86400}, false},
		{"pending", PrivateNodeSubscription{Status: PNStatusPending, ExpiresAt: now + 3600}, false},
		{"provisioning", PrivateNodeSubscription{Status: PNStatusProvisioning, ExpiresAt: now + 3600}, false},
		{"suspended", PrivateNodeSubscription{Status: PNStatusSuspended, ExpiresAt: now + 3600}, false},
		{"deprovisioned", PrivateNodeSubscription{Status: PNStatusDeprovisioned, ExpiresAt: now + 3600}, false},
		{"failed", PrivateNodeSubscription{Status: PNStatusFailed, ExpiresAt: now + 3600}, false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.sub.IsServiceable(now), tc.name)
	}
}

// TestAuthorizeNodeAccess_TrialUser —— 节点鉴权是体验线路隔离真正的门（spec §3.7）：
// 客户端缓存里的会员节点地址只能靠这里挡。
func TestAuthorizeNodeAccess_TrialUser(t *testing.T) {
	now := int64(1_000_000)
	trialUser := &User{ID: 7, ExpiredAt: now + 3600, InTrial: true}
	regular := &SlaveNode{Class: NodeClassShared}
	trialPool := &SlaveNode{Class: NodeClassShared, Pool: NodePoolTrial}

	assert.False(t, AuthorizeNodeAccess(trialUser, regular, nil, now), "试用用户不能连会员节点")
	assert.True(t, AuthorizeNodeAccess(trialUser, trialPool, nil, now), "试用用户能连体验线路")

	expiredTrial := &User{ID: 7, ExpiredAt: now - 1, InTrial: true}
	assert.False(t, AuthorizeNodeAccess(expiredTrial, trialPool, nil, now), "过期试用用户连体验线路也拒绝")

	paid := &User{ID: 8, ExpiredAt: now + 3600, IsFirstOrderDone: BoolPtr(true)}
	assert.True(t, AuthorizeNodeAccess(paid, regular, nil, now), "付费用户能连会员节点")
	assert.True(t, AuthorizeNodeAccess(paid, trialPool, nil, now), "付费用户也能连体验线路（容量不浪费）")
}
