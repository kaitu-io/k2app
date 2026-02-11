package daemon

import (
	"log"
	"sync"
)

// SpeedtestState tracks the state of an async speed test.
type SpeedtestState struct {
	mu           sync.Mutex
	State        string  `json:"state"` // idle, running, completed, error
	DownloadMbps float64 `json:"download_mbps"`
	UploadMbps   float64 `json:"upload_mbps"`
	LatencyMs    float64 `json:"latency_ms"`
	Error        string  `json:"error,omitempty"`
}

// NewSpeedtestState creates a new speedtest state tracker.
func NewSpeedtestState() *SpeedtestState {
	return &SpeedtestState{State: "idle"}
}

// IsRunning returns true if a speedtest is in progress.
func (s *SpeedtestState) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.State == "running"
}

// GetStatus returns a copy of the current status.
func (s *SpeedtestState) GetStatus() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{
		"state":         s.State,
		"download_mbps": s.DownloadMbps,
		"upload_mbps":   s.UploadMbps,
		"latency_ms":    s.LatencyMs,
		"error":         s.Error,
	}
}

func (s *SpeedtestState) setRunning() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = "running"
	s.DownloadMbps = 0
	s.UploadMbps = 0
	s.LatencyMs = 0
	s.Error = ""
}

func (s *SpeedtestState) setCompleted(download, upload, latency float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = "completed"
	s.DownloadMbps = download
	s.UploadMbps = upload
	s.LatencyMs = latency
}

func (s *SpeedtestState) setError(err string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = "error"
	s.Error = err
}

// runSpeedtest runs a speed test asynchronously.
// TODO: Implement actual speedtest logic.
func (d *Daemon) runSpeedtest(serverID string) {
	d.speedtest.setRunning()
	log.Printf("Speedtest started (server: %s)", serverID)

	// TODO: Implement actual speed test.
	// For now, set a placeholder result.
	d.speedtest.setCompleted(0, 0, 0)
	log.Println("Speedtest completed (stub)")
}
