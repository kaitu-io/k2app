package center

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// OutboundRouteResponse represents the response for outbound route diagnosis
type OutboundRouteResponse struct {
	Carrier     string  `json:"carrier"`               // china_telecom, china_unicom, china_mobile
	Province    string  `json:"province"`              // guangdong, beijing, etc.
	RouteType   string  `json:"routeType"`             // cn2_gia, cmi, as9929, as4837, 163, unknown
	DiagnosedAt *string `json:"diagnosedAt,omitempty"` // RFC3339 timestamp, null if no diagnosis data
}

// api_outbound_route returns outbound route diagnosis for a specific IP
//
// GET /api/diagnosis/outbound-route?ip=x.x.x.x&carrier=china_telecom&province=guangdong
//
// Parameters:
// - ip (required): Target IP address
// - carrier (required): china_telecom, china_unicom, china_mobile, china_education
// - province (required): Province name in pinyin (e.g., guangdong, beijing)
//
// Returns:
// - code 0: Success with route info (may trigger background refresh if stale)
// - code 425: Diagnosis not ready, triggered detection, please retry
// - code 422: Invalid parameters
func api_outbound_route(c *gin.Context) {
	// Get required parameters
	ip := c.Query("ip")
	carrier := c.Query("carrier")
	province := c.Query("province")

	// Validate required parameters
	if ip == "" {
		log.Warnf(c, "missing required parameter: ip")
		Error(c, ErrorInvalidArgument, "missing required parameter: ip")
		return
	}
	if carrier == "" {
		log.Warnf(c, "missing required parameter: carrier")
		Error(c, ErrorInvalidArgument, "missing required parameter: carrier")
		return
	}
	if province == "" {
		log.Warnf(c, "missing required parameter: province")
		Error(c, ErrorInvalidArgument, "missing required parameter: province")
		return
	}

	// Validate carrier value
	validCarriers := map[string]bool{
		"china_telecom":   true,
		"china_unicom":    true,
		"china_mobile":    true,
		"china_education": true,
	}
	if !validCarriers[carrier] {
		log.Warnf(c, "invalid carrier: %s", carrier)
		Error(c, ErrorInvalidArgument, fmt.Sprintf("invalid carrier: %s", carrier))
		return
	}

	log.Infof(c, "fetching outbound route for IP %s, carrier %s, province %s", ip, carrier, province)

	// Get diagnosis config for stale check
	cfg := configDiagnosis()
	staleDuration := time.Duration(cfg.StaleDays) * 24 * time.Hour

	// Find IPRouteInfo for this IP (outbound direction)
	var routeInfo IPRouteInfo
	err := db.Get().Where(&IPRouteInfo{
		IP:        ip,
		Direction: DiagnosisDirectionOutbound,
	}).First(&routeInfo).Error

	if err == gorm.ErrRecordNotFound {
		// No data exists - trigger diagnosis and return 425 (Too Early)
		log.Infof(c, "no diagnosis data for IP %s, triggering detection", ip)
		if _, enqErr := EnqueueDiagnosisOutbound(c, ip); enqErr != nil {
			log.Warnf(c, "failed to enqueue diagnosis for IP %s: %v", ip, enqErr)
		}
		Error(c, ErrorTooEarly, "diagnosis not ready, detection triggered, please retry")
		return
	}
	if err != nil {
		log.Errorf(c, "failed to query diagnosis for IP %s: %v", ip, err)
		Error(c, ErrorSystemError, "failed to query diagnosis")
		return
	}

	// Check if data is stale and trigger background refresh
	if time.Since(routeInfo.DiagnosedAt) > staleDuration {
		log.Infof(c, "diagnosis data for IP %s is stale (diagnosed at %s), triggering refresh",
			ip, routeInfo.DiagnosedAt.Format(time.RFC3339))
		if _, enqErr := EnqueueDiagnosisOutbound(c, ip); enqErr != nil {
			log.Warnf(c, "failed to enqueue refresh for IP %s: %v", ip, enqErr)
		}
		// Continue to return existing data
	}

	// Parse route matrix
	routeMap, err := routeInfo.GetRouteMap()
	if err != nil {
		log.Errorf(c, "failed to parse route matrix for IP %s: %v", ip, err)
		Error(c, ErrorSystemError, "failed to parse route matrix")
		return
	}

	// Build lookup key
	key := fmt.Sprintf("%s:%s", carrier, province)
	routeType, found := routeMap[key]
	if !found {
		routeType = "unknown"
	}

	// Format diagnosed at time
	diagnosedAt := routeInfo.DiagnosedAt.Format("2006-01-02T15:04:05Z07:00")

	log.Infof(c, "outbound route for IP %s, %s: %s (diagnosed at %s)", ip, key, routeType, diagnosedAt)

	response := OutboundRouteResponse{
		Carrier:     carrier,
		Province:    province,
		RouteType:   routeType,
		DiagnosedAt: &diagnosedAt,
	}

	Success(c, &response)
}
