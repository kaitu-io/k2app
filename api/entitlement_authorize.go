package center

// AuthorizeNodeAccess 是节点访问授权的唯一决策点（能力矩阵的授权侧）。
// k2s 保持"哑"：只转发节点身份 + 用户凭证；这里集中判断 shared/private 差异。
//
//   - 共享节点：任意未过期会员用户放行。
//   - 专属节点：仅"主人"且其专属订阅处于可服务态（active/宽限期内）放行；
//     不看 User.ExpiredAt（专属节点与共享池会员是两条独立时钟）。
//
// sub 为该节点对应的专属订阅（共享节点传 nil）。now 为 Unix 秒。
func AuthorizeNodeAccess(user *User, node *SlaveNode, sub *PrivateNodeSubscription, now int64) bool {
	switch node.Class {
	case NodeClassPrivate:
		if node.PrivateOwnerUserID == nil || *node.PrivateOwnerUserID != user.ID {
			return false
		}
		return sub != nil && sub.IsServiceable(now)
	case NodeClassShared, "":
		return user.ExpiredAt > now
	}
	return false
}
