package center

import (
	"context"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

const TaskTypePrivateNodeLifecycleSweep = "private_node:lifecycle_sweep"

// handlePrivateNodeLifecycleSweep 每日推进专属节点订阅生命周期标签。
// 服务硬切点由 IsServiceable 以时间戳派生（Task 1）；本 cron 只重贴标签 + grace→suspended
// 硬切 + 续费回收。
//
// 单步推进保证：每条订阅每次扫描最多推进一级（active→grace 或 grace→suspended 或
// suspended→deprovisioned），绝不在一次扫描里级联跳多级。实现靠在任何写入之前先快照各
// cohort 的候选行（按推进前的 status 查询），后续 CAS 更新只针对快照里的行；这样某行在第 2
// 步被 active→grace 后，不会被第 3 步重新选中又推进到 suspended。例如一条已过期 22 天仍标
// active 的订阅，本次只 active→grace，下次扫描才 grace→suspended，再下次才 deprovisioned。
func handlePrivateNodeLifecycleSweep(ctx context.Context, _ []byte) error {
	now := time.Now().Unix()
	graceCutoff := now - privateNodeGraceSeconds                               // expires_at <= 此值 ⇔ 已过宽限期
	suspendCutoff := now - privateNodeGraceSeconds - privateNodeSuspendSeconds // expires_at <= 此值 ⇔ 已过停机期

	// 在任何写入之前快照三个 cohort 的候选行，确保单步推进（见函数注释）。
	var expiring, graceEnded, suspendEnded []PrivateNodeSubscription
	if err := db.Get().Where("status = ? AND expires_at <= ?", PNStatusActive, now).Find(&expiring).Error; err != nil {
		return fmt.Errorf("query expiring active subs: %w", err)
	}
	if err := db.Get().Where("status = ? AND expires_at <= ?", PNStatusGrace, graceCutoff).Find(&graceEnded).Error; err != nil {
		return fmt.Errorf("query grace-ended subs: %w", err)
	}
	if err := db.Get().Where("status = ? AND expires_at <= ?", PNStatusSuspended, suspendCutoff).Find(&suspendEnded).Error; err != nil {
		return fmt.Errorf("query suspend-ended subs: %w", err)
	}

	// 1. 续费回收:grace/suspended 但已续费(now < ExpiresAt)→ active,并取消其未结
	//    stop/destroy(防已续费机器被操作员照单停机/销毁)。放最前,避免后续步骤把刚续费的
	//    订阅误推进到下一级。(cohort 查询用 expires_at <= cutoff,续费过的订阅 expires_at
	//    已未来,本就不会入选 graceEnded/suspendEnded。)
	var recovered []PrivateNodeSubscription
	if err := db.Get().Select("id").
		Where("status IN ? AND expires_at > ?", []string{PNStatusGrace, PNStatusSuspended}, now).
		Find(&recovered).Error; err != nil {
		return fmt.Errorf("lifecycle sweep: query renewed subs: %w", err)
	}
	if len(recovered) > 0 {
		recoveredIDs := make([]uint64, len(recovered))
		for i := range recovered {
			recoveredIDs[i] = recovered[i].ID
		}
		// status 守卫与 cohort 循环对称:仅更新仍处 grace/suspended 的行,避免快照与更新
		// 之间被其它写者改状态时误覆盖(单 cron 写者下基本不可达,守卫纯为防御)。
		if err := db.Get().Model(&PrivateNodeSubscription{}).
			Where("id IN ? AND status IN ?", recoveredIDs, []string{PNStatusGrace, PNStatusSuspended}).
			Updates(map[string]any{"status": PNStatusActive, "grace_until": 0, "suspend_until": 0}).Error; err != nil {
			return fmt.Errorf("lifecycle sweep: recover renewed subs: %w", err)
		}
		if err := cancelOpenNodeOperations(db.Get(), recoveredIDs, []string{NodeOpStop, NodeOpDestroy}); err != nil {
			log.Errorf(ctx, "[PRIVATE-NODE-LIFECYCLE] cancel open stop/destroy for renewed subs: %v", err)
		}
	}

	// 2. 期满：active 且 now >= ExpiresAt → grace。
	for i := range expiring {
		s := &expiring[i]
		graceUntil := s.ExpiresAt + privateNodeGraceSeconds
		if err := db.Get().Model(&PrivateNodeSubscription{}).
			Where("id = ? AND status = ?", s.ID, PNStatusActive).
			Updates(map[string]any{"status": PNStatusGrace, "grace_until": graceUntil}).Error; err != nil {
			log.Errorf(ctx, "[PRIVATE-NODE-LIFECYCLE] active->grace sub=%d: %v", s.ID, err)
		}
	}

	// 3. 宽限结束：grace 且 now >= ExpiresAt+grace → suspended（路由器断连）。
	for i := range graceEnded {
		s := &graceEnded[i]
		suspendUntil := s.ExpiresAt + privateNodeGraceSeconds + privateNodeSuspendSeconds
		if err := db.Get().Model(&PrivateNodeSubscription{}).
			Where("id = ? AND status = ?", s.ID, PNStatusGrace).
			Updates(map[string]any{"status": PNStatusSuspended, "suspend_until": suspendUntil}).Error; err != nil {
			log.Errorf(ctx, "[PRIVATE-NODE-LIFECYCLE] grace->suspended sub=%d: %v", s.ID, err)
			continue
		}
		sendCloudSlackNotification(ctx, "Private Node Suspended",
			fmt.Sprintf("sub=%d user=%d order=%d grace ended → suspended (stop VPS, keep IP)", s.ID, s.UserID, s.OrderID))
		if err := dispatchNodeOperation(ctx, s.ID, s.CloudInstanceID, NodeOpStop, "system:lifecycle", StopParams{Reason: "grace ended"}); err != nil {
			log.Errorf(ctx, "[PRIVATE-NODE-LIFECYCLE] dispatch stop sub=%d: %v", s.ID, err)
		}
	}

	// 4. 停机结束：suspended 且 now >= ExpiresAt+grace+suspend → deprovisioned（终态）。
	for i := range suspendEnded {
		s := &suspendEnded[i]
		if err := db.Get().Model(&PrivateNodeSubscription{}).
			Where("id = ? AND status = ?", s.ID, PNStatusSuspended).
			Updates(map[string]any{"status": PNStatusDeprovisioned}).Error; err != nil {
			log.Errorf(ctx, "[PRIVATE-NODE-LIFECYCLE] suspended->deprovisioned sub=%d: %v", s.ID, err)
			continue
		}
		sendCloudSlackNotification(ctx, "Private Node Deprovisioned",
			fmt.Sprintf("sub=%d user=%d order=%d suspend ended → deprovisioned (destroy VPS, release IP)", s.ID, s.UserID, s.OrderID))
		if err := dispatchNodeOperation(ctx, s.ID, s.CloudInstanceID, NodeOpDestroy, "system:lifecycle", DestroyParams{Reason: "suspend ended"}); err != nil {
			log.Errorf(ctx, "[PRIVATE-NODE-LIFECYCLE] dispatch destroy sub=%d: %v", s.ID, err)
		}
	}

	return nil
}
