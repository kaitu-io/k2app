package center

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	mathrand "math/rand"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
)

// SlaveNodeUpsertRequest 物理节点注册/更新请求结构体
type SlaveNodeUpsertRequest struct {
	Country      string              `json:"country" binding:"required" example:"US"`            // 国家代码（ISO 3166-1 alpha-2）
	Region       string              `json:"region" example:"us-west-1"`                         // 服务器机房位置/区域（可选，默认使用Country）
	Name         string              `json:"name" binding:"required" example:"US West Node 1"`   // 节点名称
	IPv6         string              `json:"ipv6" example:"2001:db8::1"`                         // 节点IPv6地址
	SecretToken  string              `json:"secretToken" binding:"required" example:"abc123..."` // 节点认证令牌（必需，客户端持久化保存）
	Tunnels      []TunnelConfigInput `json:"tunnels"`                                            // 隧道配置列表（可选，支持批量注册）
	Meta         json.RawMessage     `json:"meta,omitempty"`                                     // 节点元数据（可选JSON，如架构类型）
	IPType       string              `json:"ipType"`                                             // residential|non_residential|unknown，sidecar 始终上报（未配置=unknown）
	PrivateClaim string              `json:"privateClaim,omitempty"`                             // 专属节点认领令牌（cloud-init 注入，sidecar 回传）
}

// TunnelConfigInput 隧道配置输入
type TunnelConfigInput struct {
	Domain       string `json:"domain" binding:"required" example:"*.example.com"` // 隧道域名
	Protocol     string `json:"protocol" example:"k2v4"`                           // 隧道协议（k2v4, k2v5, k2wss）
	Port         int    `json:"port" binding:"required" example:"443"`             // 隧道端口
	HopPortStart int    `json:"hopPortStart" example:"10000"`                      // Port hopping start (0 = disabled)
	HopPortEnd   int    `json:"hopPortEnd" example:"20000"`                        // Port hopping end
	IsTest       bool   `json:"isTest" example:"false"`                            // 是否为测试节点（测试节点仅对 admin 用户可见）
	HasRelay     bool   `json:"hasRelay" example:"false"`                          // Whether this tunnel provides relay capability
	HasTunnel    bool   `json:"hasTunnel" example:"true"`                          // Whether this tunnel provides direct tunnel capability (default: true)
	ServerURL    string `json:"serverUrl,omitempty"`                               // k2v5 connection URL
}

// TunnelConfigOutput 隧道配置输出（含证书）
type TunnelConfigOutput struct {
	Domain       string `json:"domain" example:"*.example.com"` // 隧道域名
	Protocol     string `json:"protocol" example:"k2v4"`        // 隧道协议（k2v4, k2v5, k2wss）
	Port         int    `json:"port" example:"443"`             // 隧道端口
	HopPortStart int    `json:"hopPortStart" example:"10000"`   // Port hopping start
	HopPortEnd   int    `json:"hopPortEnd" example:"20000"`     // Port hopping end
	SSLCert      string `json:"sslCert"`                        // SSL证书（PEM格式）
	SSLKey       string `json:"sslKey"`                         // SSL私钥（PEM格式）
	Created      bool   `json:"created" example:"true"`         // 是否为新创建
	HasRelay     bool   `json:"hasRelay" example:"false"`       // Whether this tunnel provides relay capability
	HasTunnel    bool   `json:"hasTunnel" example:"true"`       // Whether this tunnel provides direct tunnel capability
}

// SlaveNodeUpsertResponse 物理节点注册/更新响应结构体
type SlaveNodeUpsertResponse struct {
	IPv4        string               `json:"ipv4" example:"1.2.3.4"`          // 节点IPv4地址（唯一标识）
	SecretToken string               `json:"secretToken" example:"abc123..."` // 节点认证令牌
	Created     bool                 `json:"created" example:"true"`          // 是否为新创建（true=创建，false=更新）
	Tunnels     []TunnelConfigOutput `json:"tunnels,omitempty"`               // 已注册的隧道列表（含证书）
}

// classForRegistration enforces Invariant 1: a node presenting a private claim is
// ALWAYS private-class (never shared), even before/without a successful sub binding —
// a claim-carrying VPS is someone's dedicated line and must never enter the shared
// pool. No claim → preservedClass ("" on fresh create → GORM default 'shared').
func classForRegistration(privateClaim, preservedClass string) string {
	if privateClaim != "" {
		return NodeClassPrivate
	}
	return preservedClass
}

// reconcilePrivateIdentity re-derives a claim-carrying node's private ownership from
// the authoritative PrivateNodeSubscription on EVERY registration (单一权威源):
//  1. First activation — token matches a pending/provisioning sub → activate, bind,
//     record BoundIpv4 (the durable re-claim key) and blank the one-time token.
//  2. Re-establishment — token already consumed; match a *serviceable* sub by
//     BoundIpv4 == node IP (IP = durable key + anti-hijack guard) → refresh both
//     sub.SlaveNodeID (fixes vanished-line) and node ownership.
//  3. Neither — node stays private-unowned (class already private via Invariant 1):
//     excluded from the shared pool, and device-auth denies on nil PrivateSubID.
//
// best-effort: errors are logged, never block the registration response (防探测：不暴露
// token 有效性). node is updated in-memory so the response reflects the truth.
func reconcilePrivateIdentity(c *gin.Context, node *SlaveNode, claim string) {
	now := time.Now().Unix()

	// (1) 首次激活：token 命中 pending/provisioning。原子 CAS 记录 BoundIpv4 并置空一次性 token。
	var pnSub PrivateNodeSubscription
	if err := db.Get().Where(&PrivateNodeSubscription{ProvisionClaimToken: claim}).First(&pnSub).Error; err == nil {
		activated := false
		txErr := db.Get().Transaction(func(tx *gorm.DB) error {
			res := tx.Model(&PrivateNodeSubscription{}).
				Where("id = ? AND provision_claim_token = ? AND status IN ?",
					pnSub.ID, claim, []string{PNStatusPending, PNStatusProvisioning}).
				Updates(map[string]any{
					"status": PNStatusActive, "slave_node_id": node.ID,
					"bound_ipv4": node.Ipv4, "provision_claim_token": "",
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return nil // 已认领/失败/token 失效 → 落到 (2) 按 IP 重认领
			}
			activated = true
			if err := bindNodePrivate(tx, node, &pnSub); err != nil {
				return err
			}
			linkCloudInstanceQuota(c, tx, node, &pnSub) // best-effort，首次激活专用
			markProvisionDone(c, tx, node, &pnSub)      // best-effort
			log.Infof(c, "node %s claimed as private by sub %d (owner %d)", node.Ipv4, pnSub.ID, pnSub.UserID)
			return nil
		})
		if txErr != nil {
			log.Errorf(c, "private first-activation tx sub=%d: %v", pnSub.ID, txErr)
		}
		if activated {
			return
		}
	}

	// (2) 重认领：token 已消费，按 BoundIpv4 找*可服务*订阅（IP=持久键+防劫持闸）。
	//     刷新 sub.SlaveNodeID 到当前活节点（修网关线消失），并回填节点归属。
	var sub2 PrivateNodeSubscription
	if err := db.Get().Where("bound_ipv4 = ? AND status IN ?",
		node.Ipv4, []string{PNStatusActive, PNStatusGrace}).
		Order("id DESC").First(&sub2).Error; err == nil && sub2.IsServiceable(now) {
		txErr := db.Get().Transaction(func(tx *gorm.DB) error {
			if e := tx.Model(&PrivateNodeSubscription{}).Where("id = ?", sub2.ID).
				Update("slave_node_id", node.ID).Error; e != nil {
				return e
			}
			return bindNodePrivate(tx, node, &sub2)
		})
		if txErr != nil {
			log.Errorf(c, "private re-establish tx sub=%d: %v", sub2.ID, txErr)
			return
		}
		log.Infof(c, "node %s re-established private for sub %d (owner %d)", node.Ipv4, sub2.ID, sub2.UserID)
		return
	}

	// (3) 带 token 但无可绑订阅 → 保持 private-unowned（class 已 private = 共享池排除 +
	//     device-auth 因 nil PrivateSubID 拒绝 = 谁都不服务，安全）。
	log.Infof(c, "node %s carries claim but no serviceable sub; left private-unowned", node.Ipv4)
}

// bindNodePrivate 把节点归属列定向写为 private（勿全量 Save），并同步内存字段供响应序列化。
func bindNodePrivate(tx *gorm.DB, node *SlaveNode, sub *PrivateNodeSubscription) error {
	if err := tx.Model(&SlaveNode{}).Where("id = ?", node.ID).Updates(map[string]any{
		"class": NodeClassPrivate, "private_owner_user_id": sub.UserID, "private_sub_id": sub.ID,
	}).Error; err != nil {
		return err
	}
	owner, sid := sub.UserID, sub.ID
	node.Class = NodeClassPrivate
	node.PrivateOwnerUserID = &owner
	node.PrivateSubID = &sid
	return nil
}

// linkCloudInstanceQuota 回填 CloudInstance 链路 + 写卖出配额（首次激活专用，best-effort）。
// provider sync 报的是 VPS bundle，绝不可当卖出额（upsertCloudInstance 对 private 已跳过覆盖）。
// ip_address 非唯一：IP 回收后可能有多条历史行，取最新一条（id DESC）。
func linkCloudInstanceQuota(c *gin.Context, tx *gorm.DB, node *SlaveNode, sub *PrivateNodeSubscription) {
	var ci CloudInstance
	if e := tx.Where("ip_address = ?", node.Ipv4).Order("id DESC").First(&ci).Error; e != nil {
		log.Debugf(c, "no cloud instance found for ip=%s (sub=%d), skip link", node.Ipv4, sub.ID)
		return
	}
	if e := tx.Model(&PrivateNodeSubscription{}).Where("id = ?", sub.ID).
		Update("cloud_instance_id", ci.ID).Error; e != nil {
		log.Errorf(c, "link cloud instance to sub=%d: %v", sub.ID, e)
	}
	ciUpdates := map[string]any{"traffic_total_bytes": sub.TrafficTotalBytes}
	if ci.TrafficResetAt == 0 {
		ciUpdates["traffic_reset_at"] = time.Now().Unix() + trafficEpochPeriodSec
	}
	if e := tx.Model(&CloudInstance{}).Where("id = ?", ci.ID).Updates(ciUpdates).Error; e != nil {
		log.Errorf(c, "write sold quota to cloud instance %d (sub=%d): %v", ci.ID, sub.ID, e)
	}
}

// markProvisionDone 翻 provision 运维任务 → done（权威完成只走自注册，best-effort）。
func markProvisionDone(c *gin.Context, tx *gorm.DB, node *SlaveNode, sub *PrivateNodeSubscription) {
	if e := tx.Model(&NodeOperation{}).
		Where("sub_id = ? AND action = ?", sub.ID, NodeOpProvision).
		Updates(map[string]any{
			"status":       NodeOpDone,
			"completed_at": time.Now().Unix(),
			"result":       mustJSON(map[string]any{"ipv4": node.Ipv4}),
		}).Error; e != nil {
		log.Errorf(c, "mark provision operation done sub=%d: %v", sub.ID, e)
	}
}

func api_slave_node_upsert(c *gin.Context) {
	ipv4Param := c.Param("ipv4")
	if ipv4Param == "" {
		Error(c, ErrorInvalidArgument, "ipv4 parameter is required")
		return
	}

	var req SlaveNodeUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 如果没有指定 Region，使用 Country 作为默认值
	region := req.Region
	if region == "" {
		region = req.Country
	}

	// SecretToken is required
	if req.SecretToken == "" {
		Error(c, ErrorInvalidArgument, "secretToken is required")
		return
	}

	// Check if node exists (including soft-deleted)
	var existingNode SlaveNode
	err := db.Get().Unscoped().Where("ipv4 = ?", ipv4Param).First(&existingNode).Error

	nodeCreated := false
	var node SlaveNode

	if err == nil {
		// Node exists - verify SecretToken and replace
		if existingNode.SecretToken != req.SecretToken {
			log.Warnf(c, "invalid secretToken for node: %s", ipv4Param)
			Error(c, ErrorForbidden, "invalid secretToken")
			return
		}

		// 保全专属归属：sidecar 重注册不发 Class，捕获旧值在重建时带过去，
		// 否则专属节点重启后会被重置成 shared。
		preservedClass := existingNode.Class
		preservedOwner := existingNode.PrivateOwnerUserID
		preservedSubID := existingNode.PrivateSubID

		// Delete existing node and its tunnels, then create new
		tx := db.Get().Begin()
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}()

		// Hard delete associated tunnels
		if err := tx.Unscoped().Where("node_id = ?", existingNode.ID).Delete(&SlaveTunnel{}).Error; err != nil {
			tx.Rollback()
			log.Errorf(c, "failed to delete tunnels for node %s: %v", ipv4Param, err)
			Error(c, ErrorSystemError, "failed to delete existing tunnels")
			return
		}

		// Hard delete existing node
		if err := tx.Unscoped().Delete(&existingNode).Error; err != nil {
			tx.Rollback()
			log.Errorf(c, "failed to delete existing node %s: %v", ipv4Param, err)
			Error(c, ErrorSystemError, "failed to delete existing node")
			return
		}

		// Create new node（带过保全的专属归属字段）
		node = SlaveNode{
			Ipv4:               ipv4Param,
			SecretToken:        req.SecretToken,
			Country:            req.Country,
			Region:             region,
			Name:               req.Name,
			Ipv6:               req.IPv6,
			Meta:               string(req.Meta),
			IPType:             NormalizeIPType(req.IPType),
			Class:              classForRegistration(req.PrivateClaim, preservedClass),
			PrivateOwnerUserID: preservedOwner,
			PrivateSubID:       preservedSubID,
		}
		if err := tx.Create(&node).Error; err != nil {
			tx.Rollback()
			log.Errorf(c, "failed to create node: %v", err)
			Error(c, ErrorSystemError, "failed to create node")
			return
		}

		if err := tx.Commit().Error; err != nil {
			log.Errorf(c, "failed to commit transaction: %v", err)
			Error(c, ErrorSystemError, "failed to commit transaction")
			return
		}

		log.Infof(c, "node replaced successfully: ipv4=%s", ipv4Param)
	} else if util.DbIsNotFoundErr(err) {
		// Node doesn't exist - create new
		node = SlaveNode{
			Ipv4:        ipv4Param,
			SecretToken: req.SecretToken,
			Country:     req.Country,
			Region:      region,
			Name:        req.Name,
			Ipv6:        req.IPv6,
			Meta:        string(req.Meta),
			IPType:      NormalizeIPType(req.IPType),
			Class:       classForRegistration(req.PrivateClaim, ""),
		}
		if err := db.Get().Create(&node).Error; err != nil {
			log.Errorf(c, "failed to create node: %v", err)
			Error(c, ErrorSystemError, "failed to create node")
			return
		}

		nodeCreated = true
		log.Infof(c, "node created successfully: ipv4=%s", ipv4Param)

		// Trigger outbound diagnosis for new node (async)
		go func() {
			if _, err := EnqueueDiagnosisOutbound(c.Request.Context(), node.Ipv4); err != nil {
				log.Warnf(c, "failed to enqueue diagnosis for new node %s: %v", node.Ipv4, err)
			}
		}()
	} else {
		log.Errorf(c, "failed to query node: %v, ipv4: %s", err, ipv4Param)
		Error(c, ErrorSystemError, fmt.Sprintf("failed to query node: %v", err))
		return
	}

	// 批量注册隧道（如果有）
	var tunnelOutputs []TunnelConfigOutput
	if len(req.Tunnels) > 0 {
		log.Infof(c, "registering %d tunnels for node: %s", len(req.Tunnels), ipv4Param)
		for _, tunnelInput := range req.Tunnels {
			tunnelOutput, err := upsertTunnelForNode(c, &node, tunnelInput)
			if err != nil {
				log.Errorf(c, "failed to register tunnel %s: %v", tunnelInput.Domain, err)
				Error(c, ErrorSystemError, fmt.Sprintf("failed to register tunnel %s: %v", tunnelInput.Domain, err))
				return
			}
			tunnelOutputs = append(tunnelOutputs, *tunnelOutput)
		}
		log.Infof(c, "successfully registered %d tunnels for node: %s", len(tunnelOutputs), ipv4Param)
	}

	// 专属节点身份对账（单一权威源 = PrivateNodeSubscription，spec §7.4 +
	// docs/superpowers/plans/2026-06-19-private-node-identity-single-source.md）：
	// 每次注册都从订阅重新推导归属，使节点身份扛过 unregister→recreate / 重启 /
	// 一次性 token 消费。缺 token = 共享节点，不进此分支。
	if req.PrivateClaim != "" {
		reconcilePrivateIdentity(c, &node, req.PrivateClaim)
	}

	Success(c, &SlaveNodeUpsertResponse{
		IPv4:        node.Ipv4,
		SecretToken: node.SecretToken,
		Created:     nodeCreated,
		Tunnels:     tunnelOutputs,
	})
}

// upsertTunnelForNode 为节点注册/更新单个隧道（内部函数）
// 简化逻辑：先删除再创建，避免 GORM Update 的各种问题
func upsertTunnelForNode(c *gin.Context, node *SlaveNode, input TunnelConfigInput) (*TunnelConfigOutput, error) {
	// 规范化协议字符串
	protocolStr := strings.ReplaceAll(input.Protocol, "+", "")
	protocolStr = strings.ReplaceAll(protocolStr, " ", "")
	protocol := TunnelProtocol(protocolStr)
	if protocol == "" {
		protocol = TunnelProtocolK2V4
	}

	// 生成隧道名称
	tunnelName := fmt.Sprintf("%s %04d", node.Country, mathrand.Intn(10000))
	if input.IsTest {
		tunnelName = fmt.Sprintf("%s (test)", tunnelName)
	}

	// 查找现有隧道（包括软删除的）
	var existing SlaveTunnel
	err := db.Get().Unscoped().Where(&SlaveTunnel{Domain: input.Domain}).First(&existing).Error
	tunnelCreated := util.DbIsNotFoundErr(err)

	// 保留原有的 SecretToken（如果存在）
	secretToken := generateSecret()
	if err == nil {
		secretToken = existing.SecretToken
		// 硬删除旧记录
		if err := db.Get().Unscoped().Delete(&existing).Error; err != nil {
			return nil, fmt.Errorf("failed to delete existing tunnel: %w", err)
		}
	}

	// 创建新隧道
	tunnel := SlaveTunnel{
		NodeID:       node.ID,
		Domain:       input.Domain,
		SecretToken:  secretToken,
		Name:         tunnelName,
		Protocol:     protocol,
		Port:         int64(input.Port),
		HopPortStart: int64(input.HopPortStart),
		HopPortEnd:   int64(input.HopPortEnd),
		IsTest:       BoolPtr(input.IsTest),
		HasRelay:     BoolPtr(input.HasRelay),
		HasTunnel:    BoolPtr(input.HasTunnel || (!input.HasRelay && !input.HasTunnel)),
		ServerURL:    input.ServerURL,
	}
	if err := db.Get().Create(&tunnel).Error; err != nil {
		return nil, fmt.Errorf("failed to create tunnel: %w", err)
	}

	// 生成 SSL 证书（ECDSA CA 签名，所有协议统一）
	certPEM, keyPEM, err := GetDomainCert(c, input.Domain, false)
	if err != nil {
		return nil, fmt.Errorf("failed to generate SSL certificate: %w", err)
	}

	return &TunnelConfigOutput{
		Domain:       tunnel.Domain,
		Protocol:     string(protocol),
		Port:         input.Port,
		HopPortStart: int(tunnel.HopPortStart),
		HopPortEnd:   int(tunnel.HopPortEnd),
		SSLCert:      string(certPEM),
		SSLKey:       string(keyPEM),
		Created:      tunnelCreated,
		HasRelay:     tunnel.HasRelay != nil && *tunnel.HasRelay,
		HasTunnel:    tunnel.HasTunnel == nil || *tunnel.HasTunnel,
	}, nil
}

// SlaveNodeUpsertTunnelRequest 添加/更新隧道请求结构体
type SlaveNodeUpsertTunnelRequest struct {
	Name        string `json:"name" binding:"required" example:"Example Tunnel"` // Tunnel name
	Protocol    string `json:"protocol" example:"k2v4"`                          // Tunnel protocol (k2v4, k2v5, k2wss)
	Port        int    `json:"port" binding:"required" example:"443"`            // Tunnel port
	SecretToken string `json:"secretToken" example:"xyz789..."`                  // Tunnel auth token (optional, generates new if not provided)
}

// SlaveNodeUpsertTunnelResponse 添加/更新隧道响应结构体
type SlaveNodeUpsertTunnelResponse struct {
	TunnelID    uint64 `json:"tunnelId" example:"123"`          // 隧道ID
	Domain      string `json:"domain" example:"*.example.com"`  // 隧道域名
	SecretToken string `json:"secretToken" example:"xyz789..."` // 隧道认证令牌
	SSLCert     string `json:"sslCert"`                         // SSL证书
	SSLKey      string `json:"sslKey"`                          // SSL私钥
	Created     bool   `json:"created" example:"true"`          // 是否为新创建（true=创建，false=更新）
}

func api_slave_node_upsert_tunnel(c *gin.Context) {
	// 验证节点认证
	node := ReqSlaveNode(c)
	if node == nil {
		Error(c, ErrorNotLogin, "node authentication required")
		return
	}

	// 验证 IPv4 参数与认证的节点匹配
	ipv4Param := c.Param("ipv4")
	if ipv4Param != node.Ipv4 {
		Error(c, ErrorForbidden, "ipv4 mismatch with authenticated node")
		return
	}

	// 获取域名参数
	domain := c.Param("domain")
	if domain == "" {
		Error(c, ErrorInvalidArgument, "domain parameter is required")
		return
	}

	var req SlaveNodeUpsertTunnelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 规范化协议字符串
	protocolStr := strings.ReplaceAll(req.Protocol, "+", "")
	protocolStr = strings.ReplaceAll(protocolStr, " ", "")
	protocol := TunnelProtocol(protocolStr)
	if protocol == "" {
		protocol = TunnelProtocolK2V4
	}

	// 查找现有隧道（包括软删除的）
	var existing SlaveTunnel
	err := db.Get().Unscoped().Where(&SlaveTunnel{Domain: domain}).First(&existing).Error
	created := util.DbIsNotFoundErr(err)

	// 保留原有的 SecretToken（如果存在且未提供新的）
	secretToken := req.SecretToken
	if secretToken == "" {
		if err == nil {
			secretToken = existing.SecretToken
		} else {
			secretToken = generateSecret()
		}
	}

	// 硬删除旧记录（如果存在）
	if err == nil {
		if err := db.Get().Unscoped().Delete(&existing).Error; err != nil {
			log.Errorf(c, "failed to delete existing tunnel: %v", err)
			Error(c, ErrorSystemError, "failed to delete existing tunnel")
			return
		}
	}

	// 创建新隧道
	tunnel := SlaveTunnel{
		NodeID:      node.ID,
		Domain:      domain,
		SecretToken: secretToken,
		Name:        req.Name,
		Protocol:    protocol,
		Port:        int64(req.Port),
	}
	if err := db.Get().Create(&tunnel).Error; err != nil {
		log.Errorf(c, "failed to create tunnel: %v", err)
		Error(c, ErrorSystemError, "failed to create tunnel")
		return
	}

	// 生成 SSL 证书（ECDSA CA 签名，所有协议统一）
	certPEM, keyPEM, err := GetDomainCert(c, domain, false)
	if err != nil {
		Error(c, ErrorSystemError, "failed to generate SSL certificate")
		return
	}

	Success(c, &SlaveNodeUpsertTunnelResponse{
		TunnelID:    tunnel.ID,
		Domain:      tunnel.Domain,
		SecretToken: tunnel.SecretToken,
		SSLCert:     string(certPEM),
		SSLKey:      string(keyPEM),
		Created:     created,
	})
}

func api_slave_node_delete_tunnel(c *gin.Context) {
	// 验证节点认证
	node := ReqSlaveNode(c)
	if node == nil {
		Error(c, ErrorNotLogin, "node authentication required")
		return
	}

	// 验证 IPv4 参数与认证的节点匹配
	ipv4Param := c.Param("ipv4")
	if ipv4Param != node.Ipv4 {
		Error(c, ErrorForbidden, "ipv4 mismatch with authenticated node")
		return
	}

	// 获取域名参数
	domain := c.Param("domain")
	if domain == "" {
		Error(c, ErrorInvalidArgument, "domain parameter is required")
		return
	}

	// 查找隧道（包括软删除的记录）
	var tunnel SlaveTunnel
	err := db.Get().Unscoped().Where(&SlaveTunnel{
		NodeID: node.ID,
		Domain: domain,
	}).First(&tunnel).Error
	if err != nil {
		if util.DbIsNotFoundErr(err) {
			// 隧道不存在或已被永久删除，DELETE 请求幂等，返回成功
			log.Infof(c, "tunnel not found, treating DELETE as successful (idempotent)")
			SuccessEmpty(c)
			return
		}
		Error(c, ErrorSystemError, "failed to find tunnel")
		return
	}

	// 删除隧道（软删除或永久删除）
	if err := db.Get().Delete(&tunnel).Error; err != nil {
		Error(c, ErrorSystemError, "failed to delete tunnel")
		return
	}

	SuccessEmpty(c)
}

// api_slave_node_unregister handles node self-unregistration during sidecar graceful shutdown.
// Cascading hard-delete: SlaveTunnel + SlaveNodeLoad + SlaveNode.
// Idempotent: returns success even if the node doesn't exist.
// DELETE /slave/nodes/:ipv4
func api_slave_node_unregister(c *gin.Context) {
	node := ReqSlaveNode(c)
	if node == nil {
		Error(c, ErrorNotLogin, "node authentication required")
		return
	}

	ipv4Param := c.Param("ipv4")
	if ipv4Param != node.Ipv4 {
		Error(c, ErrorForbidden, "ipv4 mismatch with authenticated node")
		return
	}

	log.Infof(c, "node self-unregister: ipv4=%s", ipv4Param)

	tx := db.Get().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err := tx.Unscoped().Where("node_id = ?", node.ID).Delete(&SlaveTunnel{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to delete tunnels for node %s: %v", ipv4Param, err)
		Error(c, ErrorSystemError, "failed to delete tunnels")
		return
	}

	if err := tx.Unscoped().Where("node_id = ?", node.ID).Delete(&SlaveNodeLoad{}).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to delete load records for node %s: %v", ipv4Param, err)
		Error(c, ErrorSystemError, "failed to delete load records")
		return
	}

	if err := tx.Unscoped().Delete(&node).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to delete node %s: %v", ipv4Param, err)
		Error(c, ErrorSystemError, "failed to delete node")
		return
	}

	if err := tx.Commit().Error; err != nil {
		log.Errorf(c, "failed to commit unregister for node %s: %v", ipv4Param, err)
		Error(c, ErrorSystemError, "failed to commit unregister")
		return
	}

	log.Infof(c, "node unregistered successfully: ipv4=%s", ipv4Param)
	SuccessEmpty(c)
}

// generateSecret 生成随机密钥
func generateSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ========================= Route Diagnosis API =========================

// SlaveRouteDiagnosisRequest inbound route diagnosis report from Slave
type SlaveRouteDiagnosisRequest struct {
	Direction    string            `json:"direction" binding:"required"`   // "inbound"
	RouteMatrix  map[string]string `json:"routeMatrix" binding:"required"` // {"carrier:province": "route_type", ...}
	ProbeCount   int               `json:"probeCount" binding:"required"`
	SuccessCount int               `json:"successCount" binding:"required"`
}

// api_slave_report_route_diagnosis handles inbound diagnosis report from Slave
// POST /slave/nodes/{ipv4}/route-diagnosis
func api_slave_report_route_diagnosis(c *gin.Context) {
	// Validate node authentication
	node := ReqSlaveNode(c)
	if node == nil {
		Error(c, ErrorNotLogin, "node authentication required")
		return
	}

	// Validate IPv4 parameter matches authenticated node
	ipv4Param := c.Param("ipv4")
	if ipv4Param != node.Ipv4 {
		Error(c, ErrorForbidden, "ipv4 mismatch with authenticated node")
		return
	}

	var req SlaveRouteDiagnosisRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Only accept inbound diagnosis from Slave
	if req.Direction != string(DiagnosisDirectionInbound) {
		Error(c, ErrorInvalidArgument, "only inbound diagnosis is accepted from Slave")
		return
	}

	log.Infof(c, "[DIAGNOSIS] Received inbound diagnosis report: ip=%s, probes=%d, success=%d",
		node.Ipv4, req.ProbeCount, req.SuccessCount)

	// Save inbound diagnosis
	if err := SaveInboundDiagnosis(c, node.Ipv4, req.RouteMatrix, req.ProbeCount, req.SuccessCount); err != nil {
		log.Errorf(c, "[DIAGNOSIS] Failed to save inbound diagnosis: %v", err)
		Error(c, ErrorSystemError, "failed to save diagnosis")
		return
	}

	SuccessEmpty(c)
}
