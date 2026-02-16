package center

import (
	"fmt"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// DataRelay represents a relay node in the API response
// Matches the format specified in rust/docs/design/detection-strategy-evaluation.md
// Note: All nodes in DB are active by design - no IsAlive field needed.
type DataRelay struct {
	ID         string `json:"id"`         // Unique relay identifier (e.g., "relay-tokyo-1")
	Name       string `json:"name"`       // Human-readable name
	Ipv4       string `json:"ipv4"`       // IPv4 address
	Ipv6       string `json:"ipv6"`       // IPv6 address (optional)
	HopPortMin int64  `json:"hopPortMin"` // Port hopping range start (0 if disabled)
	HopPortMax int64  `json:"hopPortMax"` // Port hopping range end (0 if disabled)
	Region     string `json:"region"`     // Geographic region
}

// DataRelayListResponse relay list response
type DataRelayListResponse struct {
	Relays []DataRelay `json:"relays"`
}

// api_k2_relays returns all tunnels with has_relay=true as available relay list
// Route: GET /k2/relays
func api_k2_relays(c *gin.Context) {
	log.Infof(c, "request to get k2 relays")

	// Check if user is admin
	user := ReqUser(c)
	isAdmin := user != nil && user.IsAdmin != nil && *user.IsAdmin

	var tunnels []SlaveTunnel
	q := db.Get().Model(&SlaveTunnel{}).
		Preload("Node"). // All nodes in DB are alive by design
		Where("has_relay = ?", true).
		Where("node_id IS NOT NULL")

	// Filter out test nodes for non-admin users
	if !isAdmin {
		q = q.Where(&SlaveTunnel{IsTest: BoolPtr(false)})
	}

	if err := q.Find(&tunnels).Error; err != nil {
		log.Errorf(c, "failed to get k2 relays, err: %v", err)
		Error(c, ErrorSystemError, "failed to get relays")
		return
	}

	log.Debugf(c, "found %d relay tunnels from database", len(tunnels))

	// Convert to relay response format
	relays := make([]DataRelay, 0, len(tunnels))
	for _, tunnel := range tunnels {
		// Skip tunnels without associated node
		if tunnel.Node == nil || tunnel.Node.ID == 0 {
			continue
		}

		relay := DataRelay{
			ID:         fmt.Sprintf("relay-%s-%d", tunnel.Node.Region, tunnel.ID),
			Name:       tunnel.Name,
			Ipv4:       tunnel.Node.Ipv4,
			Ipv6:       tunnel.Node.Ipv6,
			HopPortMin: tunnel.HopPortStart,
			HopPortMax: tunnel.HopPortEnd,
			Region:     tunnel.Node.Region,
		}
		relays = append(relays, relay)
	}

	log.Infof(c, "successfully retrieved %d k2 relays", len(relays))
	Success(c, &DataRelayListResponse{
		Relays: relays,
	})
}
