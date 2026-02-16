package center

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// TelemetryRateLimitPerHour max events per device per hour
	TelemetryRateLimitPerHour = 1000
)

// api_strategy_get_rules returns latest strategy rules
//
// GET /api/strategy/rules
// Supports ETag for conditional requests
func api_strategy_get_rules(c *gin.Context) {
	log.Infof(c, "fetching strategy rules")

	// Check If-None-Match header for caching
	clientETag := c.GetHeader("If-None-Match")

	// Find active rules
	var rules StrategyRules
	err := db.Get().Where(&StrategyRules{IsActive: BoolPtr(true)}).First(&rules).Error
	if err != nil {
		// No active rules - return default
		log.Infof(c, "no active rules found, returning defaults")
		defaultRules := StrategyRulesResponse{
			Version:   "default",
			UpdatedAt: time.Now().Format(time.RFC3339),
			ETag:      "\"default\"",
			Rules:     []map[string]any{},
			Protocols: map[string]any{},
			Default: map[string]any{
				"protocol_chain": []string{"k2:quic_bbr", "k2:tcp_ws"},
				"timeout_ms":     5000,
			},
		}
		Success(c, &defaultRules)
		return
	}

	// Generate ETag from version
	etag := fmt.Sprintf("\"%s\"", rules.Version)

	// Check if client has current version
	if clientETag == etag {
		c.Status(304)
		return
	}

	// Parse stored JSON content
	var content struct {
		Rules     []map[string]any `json:"rules"`
		Protocols map[string]any   `json:"protocols"`
		Default   map[string]any   `json:"default"`
	}
	if err := json.Unmarshal([]byte(rules.Content), &content); err != nil {
		log.Errorf(c, "failed to parse rules content: %v", err)
		Error(c, ErrorSystemError, "failed to parse rules")
		return
	}

	// Set ETag header
	c.Header("ETag", etag)

	response := StrategyRulesResponse{
		Version:   rules.Version,
		UpdatedAt: rules.UpdatedAt.Format(time.RFC3339),
		ETag:      etag,
		Rules:     content.Rules,
		Protocols: content.Protocols,
		Default:   content.Default,
	}

	log.Infof(c, "returning rules version %s", rules.Version)
	Success(c, &response)
}

// api_strategy_telemetry_batch accepts batch telemetry events
//
// POST /api/telemetry/batch
// Rate limited to 1000 events per hour per device
// Idempotent - duplicates are detected by event_id and ignored
func api_strategy_telemetry_batch(c *gin.Context) {
	var req TelemetryBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid telemetry request: %v", err)
		Error(c, ErrorInvalidArgument, "invalid request format")
		return
	}

	log.Infof(c, "receiving %d telemetry events from device %s", len(req.Events), req.DeviceID)

	// Find device by UDID
	var device Device
	err := db.Get().Where(&Device{UDID: req.DeviceID}).First(&device).Error
	if err != nil {
		log.Warnf(c, "device not found: %s", req.DeviceID)
		Error(c, ErrorNotFound, "device not found")
		return
	}

	// Check rate limit
	hourBucket := time.Now().Unix() / 3600
	var rateLimit TelemetryRateLimit
	db.Get().Where(&TelemetryRateLimit{
		DeviceID:   device.ID,
		HourBucket: hourBucket,
	}).FirstOrCreate(&rateLimit, TelemetryRateLimit{
		DeviceID:   device.ID,
		HourBucket: hourBucket,
		EventCount: 0,
	})

	remaining := TelemetryRateLimitPerHour - rateLimit.EventCount
	if remaining <= 0 {
		log.Warnf(c, "rate limit exceeded for device %s", req.DeviceID)
		Error(c, ErrorTooManyRequests, "rate limit exceeded")
		return
	}

	// Process events (up to remaining quota)
	var errors []string
	rateLimitRejected := 0

	eventsToProcess := req.Events
	if len(eventsToProcess) > remaining {
		eventsToProcess = eventsToProcess[:remaining]
		rateLimitRejected = len(req.Events) - remaining
		errors = append(errors, fmt.Sprintf("rate limit: only %d events accepted", remaining))
	}

	// Build batch of events
	var events []TelemetryEvent
	for _, evt := range eventsToProcess {
		contextJSON, _ := json.Marshal(evt.Context)
		decisionJSON, _ := json.Marshal(evt.Decision)
		outcomeJSON, _ := json.Marshal(evt.Outcome)

		events = append(events, TelemetryEvent{
			EventID:      evt.EventID,
			Timestamp:    evt.Timestamp,
			EventType:    evt.EventType,
			DeviceID:     device.ID,
			Context:      string(contextJSON),
			Decision:     string(decisionJSON),
			Outcome:      string(outcomeJSON),
			AppVersion:   req.AppVersion,
			Satisfaction: evt.Satisfaction,
		})
	}

	// Batch insert with INSERT IGNORE (duplicates silently ignored)
	var accepted int64
	if len(events) > 0 {
		result := db.Get().Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "event_id"}},
			DoNothing: true,
		}).Create(&events)
		if result.Error != nil {
			log.Errorf(c, "failed to batch insert telemetry: %v", result.Error)
			Error(c, ErrorSystemError, "failed to save events")
			return
		}
		accepted = result.RowsAffected
	}

	rejected := len(eventsToProcess) - int(accepted) + rateLimitRejected

	// Update rate limit counter (atomic increment to prevent race conditions)
	if accepted > 0 {
		db.Get().Model(&rateLimit).Update("event_count", gorm.Expr("event_count + ?", accepted))
	}

	log.Infof(c, "processed telemetry: %d accepted, %d rejected", accepted, rejected)

	response := TelemetryBatchResponse{
		Accepted: int(accepted),
		Rejected: rejected,
		Errors:   errors,
	}

	Success(c, &response)
}
