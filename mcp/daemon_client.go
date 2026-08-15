package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DaemonStatusError is the nested error object in DaemonStatus.
type DaemonStatusError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// DaemonStatus is the response from POST /api/core with action=status.
//
// Field names are the daemon's, not ours: statusInfo() hand-builds a
// map[string]any with camelCase keys ("startAt", "uptimeSeconds"), which the
// SSE payload example in k2/daemon/sse.go documents verbatim. This struct used
// to declare `connected_at` / `uptime_seconds`, so both fields silently stayed
// at their zero values and the status tool reported uptime 0 on a live tunnel.
type DaemonStatus struct {
	State         string             `json:"state"`
	StartAt       int64              `json:"startAt,omitempty"` // unix seconds, not RFC3339
	UptimeSeconds int                `json:"uptimeSeconds,omitempty"`
	Config        *DaemonConfig      `json:"config,omitempty"`
	Error         *DaemonStatusError `json:"error,omitempty"`
}

// DaemonConfig holds the minimal config fields returned by the daemon.
//
// The daemon marshals its live config.ClientConfig straight into the status
// payload, so this must mirror that shape. It previously declared a
// `server` string, which no ClientConfig ever emits (the field is
// json:"-"): the status tool always resolved an empty server name.
type DaemonConfig struct {
	Routes []DaemonRoute `json:"routes"`
}

// DaemonRoute is one entry of ClientConfig.Routes. Only Via is needed here;
// match criteria are irrelevant to naming the active server.
type DaemonRoute struct {
	Via string `json:"via"`
}

// Server returns the first wire outbound URL in the route table, or "" when
// there is none. Sentinel targets ("direct", "reject") are not outbounds and
// are skipped — only a scheme-bearing Via names a server.
func (c *DaemonConfig) Server() string {
	if c == nil {
		return ""
	}
	for _, r := range c.Routes {
		if strings.Contains(r.Via, "://") {
			return r.Via
		}
	}
	return ""
}

// DaemonError is returned when the daemon responds with a non-2xx status.
type DaemonError struct {
	Code    int
	Message string
}

func (e *DaemonError) Error() string {
	return fmt.Sprintf("daemon error %d: %s", e.Code, e.Message)
}

// DaemonClient is an HTTP client for the k2 daemon local API.
type DaemonClient struct {
	Addr string
	http *http.Client
}

// httpClient returns the client's HTTP client, or a default one if nil.
// This lazy init avoids nil panics when tests use struct literals.
func (d *DaemonClient) httpClient() *http.Client {
	if d.http != nil {
		return d.http
	}
	return &http.Client{Timeout: 5 * time.Second}
}

// Ping sends GET /ping and returns an error if the daemon is unreachable.
func (d *DaemonClient) Ping() error {
	resp, err := d.httpClient().Get(d.Addr + "/ping")
	if err != nil {
		return fmt.Errorf("daemon ping: %w", err)
	}
	resp.Body.Close()
	return nil
}

// Up sends an "up" action to the daemon with the given server config URL.
//
// The outbound goes in routes[].via. ClientConfig has no top-level "server"
// field — it carries a derived Server tagged json:"-" — so a {"server": url}
// body unmarshals into a config with zero routes. The daemon still answers
// "connecting" (doUp is async), and the engine fails 570 "no k2v5 outbound
// configured" afterwards, out of band: a silent failure with a success reply.
func (d *DaemonClient) Up(serverURL string) error {
	return d.postCore(map[string]any{
		"action": "up",
		"params": map[string]any{
			"config": map[string]any{
				"mode":   "tun",
				"routes": []map[string]string{{"via": serverURL}},
			},
		},
	})
}

// Down sends a "down" action to the daemon.
func (d *DaemonClient) Down() error {
	return d.postCore(map[string]any{
		"action": "down",
	})
}

// daemonEnvelope is the JSON envelope returned by daemon API endpoints.
type daemonEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Status sends POST /api/core with action=status and returns the parsed DaemonStatus.
func (d *DaemonClient) Status() (*DaemonStatus, error) {
	b, err := json.Marshal(map[string]any{"action": "status"})
	if err != nil {
		return nil, fmt.Errorf("daemon status marshal: %w", err)
	}
	resp, err := d.httpClient().Post(d.Addr+"/api/core", "application/json", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("daemon status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &DaemonError{Code: resp.StatusCode, Message: resp.Status}
	}

	var envelope daemonEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("daemon status decode: %w", err)
	}
	if envelope.Code != 0 {
		return nil, &DaemonError{Code: envelope.Code, Message: envelope.Message}
	}

	var status DaemonStatus
	if err := json.Unmarshal(envelope.Data, &status); err != nil {
		return nil, fmt.Errorf("daemon status unmarshal data: %w", err)
	}
	return &status, nil
}

// postCore sends a POST /api/core with the given body payload.
func (d *DaemonClient) postCore(body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("daemon post core marshal: %w", err)
	}
	resp, err := d.httpClient().Post(d.Addr+"/api/core", "application/json", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("daemon post core: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &DaemonError{Code: resp.StatusCode, Message: resp.Status}
	}
	return nil
}
