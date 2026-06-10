package center

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// provisionParams 注入 VPS cloud-init 的身份与回调信息。
type provisionParams struct {
	NodeSecret string // 节点 SecretToken（随机，存 sub；sidecar 注册用作 Basic Auth 密码）
	ClaimToken string // 认领令牌（节点自注册回传，Center 据此置 Class=private+owner）
	CenterURL  string // Center 回调地址
	Domain     string // 隧道域名（空则 sidecar 用 sslip.io 自生成）
}

// renderProvisionUserData 生成 VPS 首启 cloud-init 脚本：写 /apps/kaitu-slave/.env
// 注入身份 + claim，然后跑现有 docker/ 部署链。首版复用 provision-node.sh + docker-compose，
// 不做自定义镜像（见 spec §7.3）。具体拉取部署物的步骤在发布前真机 smoke 阶段对齐。
func renderProvisionUserData(p provisionParams) string {
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail
mkdir -p /apps/kaitu-slave
cat > /apps/kaitu-slave/.env <<'ENVEOF'
K2_NODE_SECRET=%s
K2_PRIVATE_CLAIM=%s
K2_CENTER_URL=%s
K2_DOMAIN=%s
ENVEOF
# 复用现有部署链（provision-node.sh + docker-compose）。具体拉取在真机 smoke 对齐。
`, p.NodeSecret, p.ClaimToken, p.CenterURL, p.Domain)
}

// loadPrivateNodePlanSpec 按 PlanID 取开通参数。
func loadPrivateNodePlanSpec(tx *gorm.DB, planID uint64) (*PrivateNodePlanSpec, error) {
	var spec PrivateNodePlanSpec
	if err := tx.Where(&PrivateNodePlanSpec{PlanID: planID}).First(&spec).Error; err != nil {
		return nil, fmt.Errorf("private node plan spec not found for plan %d: %w", planID, err)
	}
	return &spec, nil
}

// firstAllowedRegion 取 spec.AllowedRegions(JSON 数组) 的首个；首版默认第一个，
// 购买时选 region 的 UI 在 Plan 5。空则返回空串。
func firstAllowedRegion(allowedRegionsJSON string) string {
	var regions []string
	_ = json.Unmarshal([]byte(allowedRegionsJSON), &regions)
	if len(regions) > 0 {
		return regions[0]
	}
	return ""
}

// createPrivateNodeSubscription 在订单事务内建一条 pending 订阅（独立时钟）。
// 幂等由 OrderID uniqueIndex 保证：重复 webhook 第二次 Create 撞唯一键 → 调用方忽略。
func createPrivateNodeSubscription(ctx context.Context, tx *gorm.DB, order *Order, plan *Plan, now int64) (*PrivateNodeSubscription, error) {
	spec, err := loadPrivateNodePlanSpec(tx, plan.ID)
	if err != nil {
		return nil, err
	}
	expiresAt := time.Unix(now, 0).AddDate(0, plan.Month, 0).Unix()
	sub := &PrivateNodeSubscription{
		UserID:              order.UserID,
		PlanID:              plan.ID,
		OrderID:             order.ID,
		Region:              firstAllowedRegion(spec.AllowedRegions),
		IPType:              spec.IPType,
		TrafficTotalBytes:   spec.TrafficTotalBytes,
		Status:              PNStatusPending,
		PurchasedAt:         now,
		ExpiresAt:           expiresAt,
		ProvisionClaimToken: generateSecret(),
	}
	if err := tx.Create(sub).Error; err != nil {
		return nil, err
	}
	return sub, nil
}
