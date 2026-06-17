package center

import (
	"context"
	"encoding/base64"
	"time"

	db "github.com/wordgate/qtoolkit/db"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// tunnelProtocolsForQuery returns the set of DB protocols to query for a given
// requested protocol. k2v5 is the sole backend; it handles ECH traffic natively
// and all unknown-SNI connections via the camouflage reverse proxy.
// Legacy k2-family clients (k2, k2v4, k2wss) can connect through a k2v5 node,
// so queries for those protocols must also return k2v5 tunnels.
//
// Rules:
//   - k2, k2v4, k2wss → also include k2v5 tunnels
//   - k2v5 → exact match only
func tunnelProtocolsForQuery(requested TunnelProtocol) []TunnelProtocol {
	switch requested {
	case TunnelProtocolK2, TunnelProtocolK2V4, TunnelProtocolK2WSS:
		return []TunnelProtocol{requested, TunnelProtocolK2V5}
	default:
		return []TunnelProtocol{requested}
	}
}

// api_k2_tunnels get all tunnel list
// Routes:
//   - GET /tunnels - returns tunnels with protocol forced to "k2wss" for backward compatibility
//   - GET /tunnels/:protocol - returns tunnels with real protocol value
func api_k2_tunnels(c *gin.Context) {
	// Check if protocol is specified in path (new API) or not (legacy API)
	protocolParam := c.Param("protocol")
	useLegacyProtocol := protocolParam == ""
	log.Infof(c, "request to get k2 tunnels (legacy=%v, protocol=%s)", useLegacyProtocol, protocolParam)

	// Check if user is admin
	user := ReqUser(c)
	isAdmin := user != nil && user.IsAdmin != nil && *user.IsAdmin
	log.Debugf(c, "user is admin: %v", isAdmin)

	var tunnels []SlaveTunnel
	q := db.Get().Model(&SlaveTunnel{}).
		Preload("Node"). // All nodes in DB are alive by design
		Where("node_id IS NOT NULL")

	if protocolParam != "" {
		// Protocol-specific query: use tunnelProtocolsForQuery to expand the
		// requested protocol into the full set of DB protocols to include.
		// This handles k2v4 backward compatibility (also returns k2v5 tunnels).
		requestedProtocol := TunnelProtocol(protocolParam)
		queryProtocols := tunnelProtocolsForQuery(requestedProtocol)
		q = q.Where("protocol IN ?", queryProtocols)
		log.Debugf(c, "filtering by protocol set %v (requested: %s)", queryProtocols, protocolParam)
	}

	// Filter out test nodes for non-admin users
	if !isAdmin {
		q = q.Where(&SlaveTunnel{IsTest: BoolPtr(false)})
		log.Debugf(c, "filtering test tunnels for non-admin user")
	}

	// Parse optional capability filter parameters
	hasRelayParam := c.Query("hasRelay")
	hasTunnelParam := c.Query("hasTunnel")

	// Apply capability filters if specified
	if hasRelayParam != "" {
		hasRelay := hasRelayParam == "true" || hasRelayParam == "1"
		q = q.Where("has_relay = ?", hasRelay)
		log.Debugf(c, "filtering by hasRelay=%v", hasRelay)
	}
	if hasTunnelParam != "" {
		hasTunnel := hasTunnelParam == "true" || hasTunnelParam == "1"
		q = q.Where("COALESCE(has_tunnel, true) = ?", hasTunnel)
		log.Debugf(c, "filtering by hasTunnel=%v", hasTunnel)
	}

	if err := q.Find(&tunnels).Error; err != nil {
		log.Errorf(c, "failed to get k2 tunnels, err: %v", err)
		Error(c, ErrorSystemError, "failed to get tunnels")
		return
	}

	log.Debugf(c, "found %d tunnels from database before filtering", len(tunnels))

	// Collect all node IDs and IPs for batch queries
	nodeIDs := make([]uint64, 0, len(tunnels))
	nodeIPs := make([]string, 0, len(tunnels))
	for _, tunnel := range tunnels {
		if tunnel.Node != nil && tunnel.Node.ID != 0 {
			nodeIDs = append(nodeIDs, tunnel.Node.ID)
			if tunnel.Node.Ipv4 != "" {
				nodeIPs = append(nodeIPs, tunnel.Node.Ipv4)
			}
		}
	}

	// Batch query node load details to avoid N+1 query problem
	// Returns detailed metrics for evaluation (traffic, bandwidth, cloud tunnel status)
	nodeLoadDetails := GetNodeLoadDetails(c, nodeIDs)

	// Batch query CloudInstances by IP to get billing/traffic info
	instanceMap := getCloudInstancesByIPs(nodeIPs)

	// Convert to API response format, filter out tunnels without nodes
	items := make([]DataSlaveTunnel, 0, len(tunnels))
	for _, tunnel := range tunnels {
		// Skip tunnels without associated node
		if tunnel.Node == nil || tunnel.Node.ID == 0 {
			log.Debugf(c, "skipping tunnel %d (domain=%s): no associated node", tunnel.ID, tunnel.Domain)
			continue
		}

		// Capability matrix (App→private ❌): private nodes are single-owner and
		// reachable only through the gateway /api/subs path. They must never
		// surface in the shared-pool /api/tunnels list, or one user's dedicated
		// VPS (IP, country) would leak to every App user. This query does not
		// filter by class at the DB layer (the slave_nodes JOIN aliasing trap,
		// see 3e20b8e), so exclude in-memory here.
		if tunnel.Node.Class == NodeClassPrivate {
			continue
		}

		// Get load details from batch query result
		details := NodeLoadDetails{Load: 100} // Default full load
		if d, exists := nodeLoadDetails[tunnel.Node.ID]; exists {
			details = d
		}

		// Hard-exclude over-quota cloud instances for non-admin users.
		// Scoring already penalizes them, but pickWeighted can still land on
		// a low-score node — and once AWS billing has tipped into overage
		// every additional byte costs real money. Admin path stays open so
		// over-quota nodes remain visible for triage.
		instForFilter, hasInst := instanceMap[tunnel.Node.Ipv4]
		var instPtr *CloudInstance
		if hasInst {
			instPtr = &instForFilter
		}
		if shouldHideTunnelForUser(instPtr, isAdmin) {
			log.Warnf(c, "tunnel %d (node=%s, ip=%s) over quota, hiding from non-admin: used=%dB total=%dB",
				tunnel.ID, tunnel.Node.Name, tunnel.Node.Ipv4,
				instForFilter.TrafficUsedBytes, instForFilter.TrafficTotalBytes)
			continue
		}

		nodeData := buildDataSlaveNode(tunnel.Node, details)

		// Determine protocol for response
		// Legacy API (/tunnels): always return "k2wss" for backward compatibility
		// New API (/tunnels/:protocol): return real protocol value
		responseProtocol := tunnel.Protocol
		if useLegacyProtocol {
			responseProtocol = TunnelProtocolK2WSS
		}

		item := DataSlaveTunnel{
			ID:           tunnel.ID,
			Domain:       tunnel.Domain,
			Name:         tunnel.Name,
			Protocol:     responseProtocol,
			Port:         tunnel.Port,
			HopPortStart: tunnel.HopPortStart,
			HopPortEnd:   tunnel.HopPortEnd,
			Node:         nodeData,
		}

		// Add instance data if CloudInstance found by IP
		if hasInst {
			item.Instance = buildTunnelInstanceData(instPtr)
		}

		// Top-level recommendScore — non-cloud nodes get the neutral 0.5 default
		// from ComputeRecommendScore(nil), so UI doesn't need instance null checks.
		item.RecommendScore = ComputeRecommendScore(item.Instance)

		// Always pass through serverUrl for k2v5 tunnels (needed by client regardless of protocol param)
		if tunnel.Protocol == TunnelProtocolK2V5 && tunnel.ServerURL != "" {
			item.ServerUrl = tunnel.ServerURL
		}

		items = append(items, item)
	}

	log.Infof(c, "successfully retrieved %d k2 tunnels", len(items))

	// Fetch ECH config for K2v4 connections
	echConfigBase64 := getECHConfigForTunnelResponse(c)

	Success(c, &DataSlaveTunnelListResponse{
		Items:         items,
		ECHConfigList: echConfigBase64,
	})
}

// getECHConfigForTunnelResponse fetches the active ECH config and returns it as base64.
// Returns empty string if no active ECH key exists or on any error.
func getECHConfigForTunnelResponse(ctx context.Context) string {
	if err := EnsureActiveECHKeyExists(ctx); err != nil {
		return ""
	}

	activeKey, err := GetActiveECHKey(ctx)
	if err != nil {
		return ""
	}

	_, _, echConfig, err := DecryptECHKeyMaterial(ctx, activeKey)
	if err != nil {
		return ""
	}

	echConfigList := buildECHConfigList([][]byte{echConfig})
	return base64.StdEncoding.EncodeToString(echConfigList)
}

// getCloudInstancesByIPs batch queries CloudInstances by IP addresses
func getCloudInstancesByIPs(ips []string) map[string]CloudInstance {
	result := make(map[string]CloudInstance)
	if len(ips) == 0 {
		return result
	}

	var instances []CloudInstance
	if err := db.Get().Where("ip_address IN ?", ips).Find(&instances).Error; err != nil {
		return result
	}

	for _, inst := range instances {
		result[inst.IPAddress] = inst
	}
	return result
}

// buildTunnelInstanceData constructs DataTunnelInstance from CloudInstance
func buildTunnelInstanceData(inst *CloudInstance) *DataTunnelInstance {
	if inst == nil {
		return nil
	}

	// Calculate traffic ratio (0-1)
	trafficRatio := 0.0
	if inst.TrafficTotalBytes > 0 {
		trafficRatio = float64(inst.TrafficUsedBytes) / float64(inst.TrafficTotalBytes)
		if trafficRatio > 1 {
			trafficRatio = 1
		}
	}

	// Determine billing cycle end and calculate time ratio
	billingCycleEndAt := inst.TrafficResetAt
	if billingCycleEndAt == 0 {
		billingCycleEndAt = inst.ExpiresAt
	}

	timeRatio := calculateTimeRatio(billingCycleEndAt)

	d := &DataTunnelInstance{
		TrafficTotalBytes: inst.TrafficTotalBytes,
		TrafficRatio:      trafficRatio,
		BillingCycleEndAt: billingCycleEndAt,
		TimeRatio:         timeRatio,
		BudgetScore:       trafficRatio - timeRatio,
	}
	d.RecommendScore = ComputeRecommendScore(d)
	return d
}

// buildDataSlaveNode constructs the 8 common fields shared between the v1 and v2
// tunnel response shapes. It intentionally does NOT set IPType — callers that want
// it (v2, admin) set it explicitly after calling this helper. This keeps IPType
// absent from v1 responses (omitempty on the field suppresses the zero value).
func buildDataSlaveNode(node *SlaveNode, details NodeLoadDetails) DataSlaveNode {
	return DataSlaveNode{
		Name:                  node.Name,
		Country:               node.Country,
		Region:                node.Region,
		Ipv4:                  node.Ipv4,
		Ipv6:                  node.Ipv6,
		Load:                  details.Load,
		TrafficUsagePercent:   details.TrafficUsagePercent,
		BandwidthUsagePercent: details.BandwidthUsagePercent,
	}
}

// calculateTimeRatio calculates elapsed time ratio for a billing cycle
// Returns 0-1 representing how much of the billing period has elapsed
func calculateTimeRatio(billingCycleEndAt int64) float64 {
	if billingCycleEndAt == 0 {
		return 0
	}

	now := time.Now().Unix()
	endTime := billingCycleEndAt

	// Calculate period start (assume 30-day billing cycle)
	// We go back 30 days from the end date to find the period start
	periodLengthSeconds := int64(30 * 24 * 60 * 60) // 30 days in seconds
	periodStart := endTime - periodLengthSeconds

	// If we're before the period start, no time has elapsed
	if now < periodStart {
		return 0
	}

	// If we're past the end, 100% elapsed
	if now >= endTime {
		return 1
	}

	// Calculate elapsed ratio
	elapsed := now - periodStart
	ratio := float64(elapsed) / float64(periodLengthSeconds)
	if ratio > 1 {
		ratio = 1
	}
	return ratio
}
