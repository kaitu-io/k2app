package center

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/viper"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"

	"github.com/kaitu-io/k2app/api/cloudprovider"
)

// 开通就绪轮询参数：每 2s 轮询一次，最多 ~5 分钟。
const (
	provisionPollInterval  = 2 * time.Second
	provisionReadyMaxPolls = 150
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

// centerCallbackURL 返回 Center 自身的回调基址（节点自注册回传用）。
func centerCallbackURL() string {
	return "https://" + viper.GetString("server.domain")
}

// waitInstanceReady 轮询 provider 直到实例 running 且拿到 IPv4，或超时。
func waitInstanceReady(ctx context.Context, provider cloudprovider.Provider, name string) (*cloudprovider.InstanceStatus, error) {
	for i := 0; i < provisionReadyMaxPolls; i++ {
		st, err := provider.GetInstanceStatus(ctx, name)
		if err == nil && st != nil && st.State == "running" && st.IPAddress != "" {
			return st, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(provisionPollInterval):
		}
	}
	return nil, fmt.Errorf("instance %s not ready after %d polls", name, provisionReadyMaxPolls)
}

// provisionPrivateNode 执行一次开通：原子门控 → 建实例 → 等就绪 → upsert CloudInstance → 回填 sub。
// 状态停在 provisioning（NOT active）——节点 cloud-init 自注册回传 claim 后才转 active（Plan 4）。
func provisionPrivateNode(ctx context.Context, sub *PrivateNodeSubscription, spec *PrivateNodePlanSpec, account CloudInstanceAccount, provider cloudprovider.Provider) error {
	// 1. 原子门控：只允许从 pending → provisioning。RowsAffected==0 说明已被其它 worker 抢占或非 pending。
	gate := db.Get().Model(&PrivateNodeSubscription{}).
		Where("id = ? AND status = ?", sub.ID, PNStatusPending).
		Update("status", PNStatusProvisioning)
	if gate.Error != nil {
		return gate.Error
	}
	if gate.RowsAffected == 0 {
		log.Debugf(ctx, "private node sub=%d not pending, skipping provision", sub.ID)
		return nil
	}

	// 2. 身份 + cloud-init。
	nodeSecret := generateSecret()
	userData := renderProvisionUserData(provisionParams{
		NodeSecret: nodeSecret,
		ClaimToken: sub.ProvisionClaimToken,
		CenterURL:  centerCallbackURL(),
		Domain:     "",
	})

	// 3. 建实例。
	name := fmt.Sprintf("pn-%d", sub.ID)
	if _, err := provider.CreateInstance(ctx, cloudprovider.CreateInstanceOptions{
		Region:   sub.Region,
		Plan:     spec.BundleID,
		ImageID:  spec.ImageID,
		Name:     name,
		UserData: userData,
	}); err != nil {
		return fmt.Errorf("create instance: %w", err)
	}

	// 4. 等就绪，回填配额。
	inst, err := waitInstanceReady(ctx, provider, name)
	if err != nil {
		return err
	}
	inst.TrafficTotalBytes = spec.TrafficTotalBytes

	// 5. upsert + reload。
	if err := upsertCloudInstance(ctx, account, inst); err != nil {
		return fmt.Errorf("upsert cloud instance: %w", err)
	}
	var ci CloudInstance
	if err := db.Get().Where("provider = ? AND instance_id = ?", account.Provider, inst.InstanceID).First(&ci).Error; err != nil {
		return fmt.Errorf("reload cloud instance: %w", err)
	}

	// 6. 回填 sub.cloud_instance_id；状态仍停在 provisioning。
	if err := db.Get().Model(&PrivateNodeSubscription{}).Where("id = ?", sub.ID).
		Update("cloud_instance_id", ci.ID).Error; err != nil {
		return fmt.Errorf("link cloud instance to sub: %w", err)
	}
	log.Infof(ctx, "private node sub=%d provisioned: instance=%s ip=%s ci=%d (awaiting self-register)",
		sub.ID, inst.InstanceID, inst.IPAddress, ci.ID)
	return nil
}
