package center

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	mathrand "math/rand"
	"strings"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
)

// SlaveNodeUpsertRequest 物理节点注册/更新请求结构体
//
type SlaveNodeUpsertRequest struct {
	Country     string              `json:"country" binding:"required" example:"US"`            // 国家代码（ISO 3166-1 alpha-2）
	Region      string              `json:"region" example:"us-west-1"`                         // 服务器机房位置/区域（可选，默认使用Country）
	Name        string              `json:"name" binding:"required" example:"US West Node 1"`   // 节点名称
	IPv6        string              `json:"ipv6" example:"2001:db8::1"`                         // 节点IPv6地址
	SecretToken string              `json:"secretToken" binding:"required" example:"abc123..."` // 节点认证令牌（必需，客户端持久化保存）
	Tunnels     []TunnelConfigInput `json:"tunnels"`                                            // 隧道配置列表（可选，支持批量注册）
}

// TunnelConfigInput 隧道配置输入
//
type TunnelConfigInput struct {
	Domain       string `json:"domain" binding:"required" example:"*.example.com"` // 隧道域名
	Protocol     string `json:"protocol" example:"k2v4"`                           // 隧道协议（k2v4, k2wss, k2oc）
	Port         int    `json:"port" binding:"required" example:"443"`             // 隧道端口
	HopPortStart int    `json:"hopPortStart" example:"10000"`                      // Port hopping start (0 = disabled)
	HopPortEnd   int    `json:"hopPortEnd" example:"20000"`                        // Port hopping end
	IsTest       bool   `json:"isTest" example:"false"`                            // 是否为测试节点（测试节点仅对 admin 用户可见）
	HasRelay     bool   `json:"hasRelay" example:"false"`                          // Whether this tunnel provides relay capability
	HasTunnel    bool   `json:"hasTunnel" example:"true"`                          // Whether this tunnel provides direct tunnel capability (default: true)
	CertPin       string `json:"certPin,omitempty"`       // k2v5 cert pin (e.g. "sha256:base64...")
	ECHConfigList string `json:"echConfigList,omitempty"` // k2v5 ECH config list (base64url encoded)
}

// TunnelConfigOutput 隧道配置输出（含证书）
//
type TunnelConfigOutput struct {
	Domain       string `json:"domain" example:"*.example.com"` // 隧道域名
	Protocol     string `json:"protocol" example:"k2v4"`        // 隧道协议（k2v4, k2wss, k2oc）
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
//
type SlaveNodeUpsertResponse struct {
	IPv4        string               `json:"ipv4" example:"1.2.3.4"`          // 节点IPv4地址（唯一标识）
	SecretToken string               `json:"secretToken" example:"abc123..."` // 节点认证令牌
	Created     bool                 `json:"created" example:"true"`          // 是否为新创建（true=创建，false=更新）
	Tunnels     []TunnelConfigOutput `json:"tunnels,omitempty"`               // 已注册的隧道列表（含证书）
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

		// Create new node
		node = SlaveNode{
			Ipv4:        ipv4Param,
			SecretToken: req.SecretToken,
			Country:     req.Country,
			Region:      region,
			Name:        req.Name,
			Ipv6:        req.IPv6,
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
		NodeID:        node.ID,
		Domain:        input.Domain,
		SecretToken:   secretToken,
		Name:          tunnelName,
		Protocol:      protocol,
		Port:          int64(input.Port),
		HopPortStart:  int64(input.HopPortStart),
		HopPortEnd:    int64(input.HopPortEnd),
		IsTest:        BoolPtr(input.IsTest),
		HasRelay:      BoolPtr(input.HasRelay),
		HasTunnel:     BoolPtr(input.HasTunnel || (!input.HasRelay && !input.HasTunnel)),
		CertPin:       input.CertPin,
		ECHConfigList: input.ECHConfigList,
	}
	if err := db.Get().Create(&tunnel).Error; err != nil {
		return nil, fmt.Errorf("failed to create tunnel: %w", err)
	}

	// 根据协议生成 SSL 证书
	certPEM, keyPEM, err := GetDomainCertForProtocol(c, input.Domain, protocol)
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
//
type SlaveNodeUpsertTunnelRequest struct {
	Name        string `json:"name" binding:"required" example:"Example Tunnel"` // Tunnel name
	Protocol    string `json:"protocol" example:"k2v4"`                          // Tunnel protocol (k2v4, k2wss, k2oc)
	Port        int    `json:"port" binding:"required" example:"443"`            // Tunnel port
	SecretToken string `json:"secretToken" example:"xyz789..."`                  // Tunnel auth token (optional, generates new if not provided)
}

// SlaveNodeUpsertTunnelResponse 添加/更新隧道响应结构体
//
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

	// 根据协议生成 SSL 证书
	certPEM, keyPEM, err := GetDomainCertForProtocol(c, domain, protocol)
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

// generateSecret 生成随机密钥
func generateSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ========================= Route Diagnosis API =========================

// SlaveRouteDiagnosisRequest inbound route diagnosis report from Slave
type SlaveRouteDiagnosisRequest struct {
	Direction    string            `json:"direction" binding:"required"` // "inbound"
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
