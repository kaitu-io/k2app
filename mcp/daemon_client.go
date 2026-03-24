package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// DaemonStatusError is the nested error object in DaemonStatus.
type DaemonStatusError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// DaemonStatus is the response from POST /api/core with action=status.
type DaemonStatus struct {
	State         string             `json:"state"`
	ConnectedAt   time.Time          `json:"connected_at,omitempty"`
	UptimeSeconds int                `json:"uptime_seconds,omitempty"`
	Config        *DaemonConfig      `json:"config,omitempty"`
	Error         *DaemonStatusError `json:"error,omitempty"`
}

// DaemonConfig holds the minimal config fields returned by the daemon.
type DaemonConfig struct {
	Server string `json:"server"`
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
func (d *DaemonClient) Up(serverURL string) error {
	return d.postCore(map[string]any{
		"action": "up",
		"params": map[string]any{
			"config": map[string]string{
				"server": serverURL,
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

	var status DaemonStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("daemon status decode: %w", err)
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
