package center

import (
	"testing"
	"time"
)

func TestStrategyRulesModel(t *testing.T) {
	rules := StrategyRules{
		Version:   "2026.01.18.1",
		UpdatedAt: time.Now(),
		Content:   `{"rules":[],"protocols":{}}`,
		IsActive:  BoolPtr(true),
	}
	if rules.Version == "" {
		t.Error("Version should not be empty")
	}
}

func TestTelemetryEventModel(t *testing.T) {
	event := TelemetryEvent{
		EventID:   "evt-001",
		Timestamp: time.Now().UnixMilli(),
		EventType: "connection",
		DeviceID:  1,
		Context:   `{}`,
	}
	if event.EventID == "" {
		t.Error("EventID should not be empty")
	}
}

func TestTelemetryRateLimitModel(t *testing.T) {
	rateLimit := TelemetryRateLimit{
		DeviceID:   1,
		HourBucket: time.Now().Unix() / 3600,
		EventCount: 10,
	}
	if rateLimit.DeviceID == 0 {
		t.Error("DeviceID should not be zero")
	}
}
