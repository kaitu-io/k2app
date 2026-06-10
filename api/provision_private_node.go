package center

import (
	"fmt"
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
