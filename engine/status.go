package engine

import (
	"encoding/json"
	"time"
)

// Status is the engine's typed state snapshot.
// Used by EventHandler.OnStatus, daemon statusInfo, appext StatusJSON.
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
	ConnectedAt   string       `json:"connected_at,omitempty"`
	UptimeSeconds int          `json:"uptime_seconds,omitempty"`
}

func (s Status) MarshalJSON() ([]byte, error) {
	j := statusJSON{
		State: s.State,
		Error: s.Error,
	}
	if s.State == StateConnected && !s.ConnectedAt.IsZero() {
		j.ConnectedAt = s.ConnectedAt.Format(time.RFC3339)
		j.UptimeSeconds = int(time.Since(s.ConnectedAt).Seconds())
	}
	return json.Marshal(j)
}
