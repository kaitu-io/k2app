package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"

	"github.com/gin-gonic/gin"
)

// DataSlaveTunnelV20260717 is the clean response shape for GET /api/v20260717/tunnels (C2/C3).
//   - protocol:  display name via ProtocolDisplay — k2v5 shows as "k2s"
//   - serverUrl: wire scheme preserved (k2v5://)
//   - ipType:    from node.ip_type column
//   - echConfigList: deliberately absent (C3)
type DataSlaveTunnelV20260717 struct {
	ID             uint64        `json:"id"`
	Domain         string        `json:"domain"`
	Name           string        `json:"name"`
	Protocol       string        `json:"protocol"` // ProtocolDisplay output: k2v5 → "k2s"
	Port           int64         `json:"port"`
	HopPortStart   int64         `json:"hopPortStart"`
	HopPortEnd     int64         `json:"hopPortEnd"`
	Node           DataSlaveNode `json:"node"`
	IPType         string        `json:"ipType"`         // node.ip_type (residential|non_residential|unknown)
	RecommendScore float64       `json:"recommendScore"` // [0,1] via ComputeRecommendScore
	ServerUrl      string        `json:"serverUrl"`      // k2v5:// wire scheme kept as-is
}

// api_v20260717_tunnels returns all serviceable shared-pool tunnels.
// No :protocol path param (C3). No echConfigList (C3). Mirrors the query
// and scoring logic of api_k2_tunnels without the legacy/admin branches.
func api_v20260717_tunnels(c *gin.Context) {
	var tunnels []SlaveTunnel
	if err := db.Get().Model(&SlaveTunnel{}).
		Preload("Node").
		Where("node_id IS NOT NULL").
		Where("protocol IN ?", tunnelProtocolsForQuery(TunnelProtocolK2V5)).
		Where(&SlaveTunnel{IsTest: BoolPtr(false)}).
		Find(&tunnels).Error; err != nil {
		log.Errorf(c, "v20260717 tunnels query failed: %v", err)
		Error(c, ErrorSystemError, "query failed")
		return
	}

	// Collect node IPs for batch CloudInstance lookup (same as api_k2_tunnels).
	nodeIPs := make([]string, 0, len(tunnels))
	nodeIDs := make([]uint64, 0, len(tunnels))
	for _, tunnel := range tunnels {
		if tunnel.Node != nil && tunnel.Node.ID != 0 {
			nodeIDs = append(nodeIDs, tunnel.Node.ID)
			if tunnel.Node.Ipv4 != "" {
				nodeIPs = append(nodeIPs, tunnel.Node.Ipv4)
			}
		}
	}

	nodeLoadDetails := GetNodeLoadDetails(c, nodeIDs)
	instanceMap := getCloudInstancesByIPs(nodeIPs)

	items := make([]DataSlaveTunnelV20260717, 0, len(tunnels))
	for _, tunnel := range tunnels {
		if tunnel.Node == nil || tunnel.Node.ID == 0 {
			continue
		}
		// Private nodes must not surface in the shared pool (same guard as v1).
		if tunnel.Node.Class == NodeClassPrivate {
			continue
		}

		instForFilter, hasInst := instanceMap[tunnel.Node.Ipv4]
		var instPtr *CloudInstance
		if hasInst {
			instPtr = &instForFilter
		}
		if shouldHideTunnelForUser(instPtr, false) {
			continue
		}

		details := NodeLoadDetails{Load: 100}
		if d, exists := nodeLoadDetails[tunnel.Node.ID]; exists {
			details = d
		}

		nodeData := DataSlaveNode{
			Name:                  tunnel.Node.Name,
			Country:               tunnel.Node.Country,
			Region:                tunnel.Node.Region,
			Ipv4:                  tunnel.Node.Ipv4,
			Ipv6:                  tunnel.Node.Ipv6,
			IPType:                tunnel.Node.IPType,
			Load:                  details.Load,
			TrafficUsagePercent:   details.TrafficUsagePercent,
			BandwidthUsagePercent: details.BandwidthUsagePercent,
		}

		item := DataSlaveTunnelV20260717{
			ID:             tunnel.ID,
			Domain:         tunnel.Domain,
			Name:           tunnel.Name,
			Protocol:       ProtocolDisplay(tunnel.Protocol),
			Port:           tunnel.Port,
			HopPortStart:   tunnel.HopPortStart,
			HopPortEnd:     tunnel.HopPortEnd,
			Node:           nodeData,
			IPType:         tunnel.Node.IPType,
			RecommendScore: ComputeRecommendScore(buildTunnelInstanceData(instPtr)),
			ServerUrl:      tunnel.ServerURL,
		}
		items = append(items, item)
	}

	log.Infof(c, "v20260717 tunnels: returned %d items", len(items))
	type response struct {
		Items []DataSlaveTunnelV20260717 `json:"items"`
	}
	Success(c, &response{Items: items})
}
