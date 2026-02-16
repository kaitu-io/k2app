// Package center 提供中心服务 API
//
package center

import (
	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// AcceleratePath 加速路径结构体
//
type AcceleratePath struct {
	Domain    string         `json:"domain" example:"node1.example.com"` // 节点域名
	Ip        string         `json:"ip" example:"192.168.1.100"`         // 节点IP地址
	Port      int            `json:"port" example:"443"`                 // 节点端口
	Protocol  TunnelProtocol `json:"protocol" example:"k2wss"`           // 隧道协议
	IsCurrent bool           `json:"is_current" example:"false"`         // 是否为当前节点
}

func api_slave_accelerate_tunnels(c *gin.Context) {
	node := ReqSlaveNode(c)
	tunnels := []SlaveTunnel{}
	err := db.Get().Model(&SlaveTunnel{}).Preload("Node").Find(&tunnels).Error
	if err != nil {
		log.Errorf(c, "[ERROR] failed to get tunnels: %v", err)
		Error(c, ErrorSystemError, "failed to get tunnels")
		return
	}
	paths := []AcceleratePath{}
	for _, tunnel := range tunnels {
		paths = append(paths, AcceleratePath{
			Domain:    tunnel.Domain,
			Ip:        tunnel.Node.Ipv4,
			Port:      int(tunnel.Port),
			Protocol:  tunnel.Protocol,
			IsCurrent: node != nil && tunnel.NodeID == node.ID,
		})
	}

	log.Tracef(c, "[DEBUG] api_slave_accelerate_tunnels: %v", paths)
	List(c, paths, nil)
}

// ResolveDomainResponse 域名解析响应结构体
//
type ResolveDomainResponse struct {
	Found     bool           `json:"found" example:"true"`                 // 是否找到匹配
	Domain    string         `json:"domain" example:"*.node1.example.com"` // 匹配的域名模式
	Ip        string         `json:"ip" example:"192.168.1.100"`           // 节点IP地址
	Port      int            `json:"port" example:"443"`                   // 节点端口
	Protocol  TunnelProtocol `json:"protocol" example:"k2wss"`             // 隧道协议
	IsCurrent bool           `json:"is_current" example:"false"`           // 是否为当前节点
}

func api_slave_resolve_domain(c *gin.Context) {
	domain := c.Query("domain")
	if domain == "" {
		Error(c, ErrorInvalidArgument, "domain is required")
		return
	}

	node := ReqSlaveNode(c)

	// Find all tunnels and match
	tunnels := []SlaveTunnel{}
	err := db.Get().Model(&SlaveTunnel{}).Preload("Node").Find(&tunnels).Error
	if err != nil {
		log.Errorf(c, "[ERROR] failed to get tunnels: %v", err)
		Error(c, ErrorSystemError, "failed to get tunnels")
		return
	}

	for _, tunnel := range tunnels {
		if matchDomainPattern(domain, tunnel.Domain) {
			Success(c, &ResolveDomainResponse{
				Found:     true,
				Domain:    tunnel.Domain,
				Ip:        tunnel.Node.Ipv4,
				Port:      int(tunnel.Port),
				Protocol:  tunnel.Protocol,
				IsCurrent: node != nil && tunnel.NodeID == node.ID,
			})
			return
		}
	}

	// 未找到匹配
	Success(c, &ResolveDomainResponse{
		Found: false,
	})
}

// matchDomainPattern 匹配域名模式
// 支持精确匹配和通配符匹配
func matchDomainPattern(sni, pattern string) bool {
	if pattern == "" {
		return false
	}

	// 精确匹配
	if pattern == sni {
		return true
	}

	// 通配符匹配：*.example.com
	if len(pattern) > 2 && pattern[0] == '*' && pattern[1] == '.' {
		suffix := pattern[1:] // .example.com
		if len(sni) > len(suffix) && sni[len(sni)-len(suffix):] == suffix {
			return true
		}
	}

	// 前缀通配符匹配：*example.com
	if len(pattern) > 1 && pattern[0] == '*' {
		suffix := pattern[1:] // example.com
		if len(sni) >= len(suffix) && sni[len(sni)-len(suffix):] == suffix {
			return true
		}
	}

	return false
}
