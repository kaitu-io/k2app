package engine

import (
	"encoding/json"
	"time"
)

// Status is the engine's typed state snapshot.
// Used by EventHandler.OnStatus, daemon statusInfo, appext StatusJSON.
// Fields use json:"-" because MarshalJSON computes uptimeSeconds dynamically.
type Status struct {
	State       string       `json:"-"`
	Error       *EngineError `json:"-"`
	ConnectedAt time.Time    `json:"-"`
}

// statusJSON is the JSON wire format for Status.
// UptimeSeconds is computed dynamically from ConnectedAt.
type statusJSON struct {
	State         string       `json:"state"`
	Error         *EngineError `json:"error,omitempty"`
	StartAt       int64        `json:"startAt,omitempty"`
	UptimeSeconds int          `json:"uptimeSeconds,omitempty"`
}

func (s Status) MarshalJSON() ([]byte, error) {
	j := statusJSON{
		State: s.State,
		Error: s.Error,
	}
	if s.State == StateConnected && !s.ConnectedAt.IsZero() {
		j.StartAt = s.ConnectedAt.Unix()
		j.UptimeSeconds = int(time.Since(s.ConnectedAt).Seconds())
	}
	return json.Marshal(j)
}
