package center

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
)

// ========================= Request Types =========================

type StatsEventRequest struct {
	AppOpens    []StatsAppOpenEvent    `json:"app_opens"`
	Connections []StatsConnectionEvent `json:"connections"`
}

type StatsAppOpenEvent struct {
	DeviceHash string    `json:"device_hash" binding:"required"`
	OS         string    `json:"os" binding:"required"`
	AppVersion string    `json:"app_version" binding:"required"`
	Locale     string    `json:"locale"`
	CreatedAt  time.Time `json:"created_at" binding:"required"`
}

type StatsConnectionEvent struct {
	DeviceHash       string    `json:"device_hash" binding:"required"`
	OS               string    `json:"os" binding:"required"`
	AppVersion       string    `json:"app_version" binding:"required"`
	Event            string    `json:"event" binding:"required"`
	NodeType         string    `json:"node_type" binding:"required"`
	NodeIPv4         string    `json:"node_ipv4"`
	NodeRegion       string    `json:"node_region"`
	RuleMode         string    `json:"rule_mode"`
	DurationSec      int       `json:"duration_sec"`
	DisconnectReason string    `json:"disconnect_reason"`
	CreatedAt        time.Time `json:"created_at" binding:"required"`
}

// ========================= k2s Download Request =========================

type StatsK2sDownloadRequest struct {
	IPRaw string `json:"ip_raw" binding:"required"`
	UA    string `json:"ua"`
}

// ========================= Handlers =========================

const maxEventsPerRequest = 100

// api_stats_ingest handles POST /api/stats/events
func api_stats_ingest(c *gin.Context) {
	var req StatsEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request body")
		return
	}

	totalEvents := len(req.AppOpens) + len(req.Connections)
	if totalEvents == 0 {
		SuccessEmpty(c)
		return
	}
	if totalEvents > maxEventsPerRequest {
		Error(c, ErrorInvalidArgument, fmt.Sprintf("too many events: %d, max %d", totalEvents, maxEventsPerRequest))
		return
	}

	tx := db.Get()

	// Insert app opens
	if len(req.AppOpens) > 0 {
		records := make([]StatAppOpen, len(req.AppOpens))
		for i, e := range req.AppOpens {
			records[i] = StatAppOpen{
				CreatedAt:  e.CreatedAt,
				DeviceHash: e.DeviceHash,
				OS:         e.OS,
				AppVersion: e.AppVersion,
				Locale:     e.Locale,
			}
		}
		if err := tx.Create(&records).Error; err != nil {
			log.Errorf(c, "failed to insert app opens: %v", err)
			Error(c, ErrorSystemError, "failed to save events")
			return
		}
	}

	// Insert connections
	if len(req.Connections) > 0 {
		records := make([]StatConnection, len(req.Connections))
		for i, e := range req.Connections {
			records[i] = StatConnection{
				CreatedAt:        e.CreatedAt,
				DeviceHash:       e.DeviceHash,
				OS:               e.OS,
				AppVersion:       e.AppVersion,
				Event:            e.Event,
				NodeType:         e.NodeType,
				NodeIPv4:         e.NodeIPv4,
				NodeRegion:       e.NodeRegion,
				RuleMode:         e.RuleMode,
				DurationSec:      e.DurationSec,
				DisconnectReason: e.DisconnectReason,
			}
		}
		if err := tx.Create(&records).Error; err != nil {
			log.Errorf(c, "failed to insert connections: %v", err)
			Error(c, ErrorSystemError, "failed to save events")
			return
		}
	}

	log.Debugf(c, "ingested %d stats events (app_opens=%d, connections=%d)",
		totalEvents, len(req.AppOpens), len(req.Connections))
	SuccessEmpty(c)
}

// api_stats_k2s_download handles POST /api/stats/k2s-download (internal, called by Next.js middleware)
func api_stats_k2s_download(c *gin.Context) {
	var req StatsK2sDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request body")
		return
	}

	ipHash, err := hashIPWithDailySalt(c, req.IPRaw)
	if err != nil {
		log.Errorf(c, "failed to hash IP: %v", err)
		Error(c, ErrorSystemError, "internal error")
		return
	}

	record := StatK2sDownload{
		IPHash: ipHash,
		IPRaw:  req.IPRaw,
		UA:     req.UA,
	}
	if err := db.Get().Create(&record).Error; err != nil {
		log.Errorf(c, "failed to insert k2s download: %v", err)
		Error(c, ErrorSystemError, "failed to save download")
		return
	}

	SuccessEmpty(c)
}

// ========================= Daily Salt =========================

func hashIPWithDailySalt(c *gin.Context, ip string) (string, error) {
	today := time.Now().UTC().Format("2006-01-02")
	cacheKey := fmt.Sprintf("stats:daily_salt:%s", today)

	// Try to get existing salt
	var salt string
	exists, err := redis.CacheGet(cacheKey, &salt)
	if err != nil {
		return "", fmt.Errorf("redis get salt: %w", err)
	}

	if !exists {
		// Generate new salt
		saltBytes := make([]byte, 32)
		if _, err := rand.Read(saltBytes); err != nil {
			return "", fmt.Errorf("generate salt: %w", err)
		}
		salt = hex.EncodeToString(saltBytes)
		if err := redis.CacheSet(cacheKey, salt, 48*3600); err != nil {
			log.Warnf(c, "failed to set daily salt, using generated: %v", err)
		}
		// Re-read in case another process set it first
		if exists2, err2 := redis.CacheGet(cacheKey, &salt); err2 == nil && exists2 {
			// Use the one from Redis (may be from another process)
		}
	}

	h := sha256.Sum256([]byte(ip + salt))
	return hex.EncodeToString(h[:]), nil
}
