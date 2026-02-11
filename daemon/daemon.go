package daemon

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// DefaultAddr is the daemon HTTP listen address.
const DefaultAddr = "127.0.0.1:1777"

// Connection states.
const (
	StateStopped       = "stopped"
	StateConnecting    = "connecting"
	StateConnected     = "connected"
	StateReconnecting  = "reconnecting"
	StateDisconnecting = "disconnecting"
	StateError         = "error"
)

// Daemon is the k2 background service.
// It manages the VPN tunnel lifecycle and exposes an HTTP API.
type Daemon struct {
	mu    sync.RWMutex
	state string

	// Last config used for auto-reconnect.
	lastWireURL    string
	lastConfigPath string

	// Error from last connection attempt.
	lastError string

	// Connected at timestamp.
	connectedAt time.Time

	// Process monitor PID.
	monitorPID int

	// Speedtest state.
	speedtest *SpeedtestState
}

// New creates a new daemon.
func New() *Daemon {
	return &Daemon{
		state:     StateStopped,
		speedtest: NewSpeedtestState(),
	}
}

// Run starts the daemon HTTP server and blocks until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	log.Printf("k2 daemon starting on %s", DefaultAddr)

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", d.handlePing)
	mux.HandleFunc("/metrics", d.handleMetrics)
	mux.HandleFunc("/api/core", d.handleCore)

	srv := &http.Server{
		Addr:    DefaultAddr,
		Handler: corsMiddleware(mux),
	}

	// Load persisted state and auto-reconnect if applicable.
	d.tryAutoReconnect()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	log.Printf("k2 daemon ready")

	select {
	case <-ctx.Done():
		log.Println("Daemon shutting down...")
		d.doDown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	}
}

// State returns the current connection state.
func (d *Daemon) State() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.state
}

func (d *Daemon) setState(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	log.Printf("State: %s → %s", d.state, s)
	d.state = s
}

// doUp connects the tunnel with the given wire URL or config path.
func (d *Daemon) doUp(wireURL, configPath string, pid int) error {
	d.mu.Lock()
	current := d.state
	d.mu.Unlock()

	// If already connected, disconnect first.
	if current == StateConnected || current == StateConnecting || current == StateReconnecting {
		d.doDown()
	}

	d.setState(StateConnecting)

	// Save config for auto-reconnect.
	d.mu.Lock()
	d.lastWireURL = wireURL
	d.lastConfigPath = configPath
	d.monitorPID = pid
	d.lastError = ""
	d.mu.Unlock()

	// Persist state for auto-reconnect across restarts.
	saveState(persistedState{
		WireURL:    wireURL,
		ConfigPath: configPath,
		State:      StateConnecting,
		Timestamp:  time.Now(),
	})

	// TODO: Actually start the tunnel here.
	// This requires the L4 tunnel rewrite (core/tunnel.go).
	// For now, we just set state to connected.
	d.setState(StateConnected)
	d.mu.Lock()
	d.connectedAt = time.Now()
	d.mu.Unlock()

	// Start PID monitoring if requested.
	if pid > 0 {
		go d.monitorProcess(pid)
	}

	// Update persisted state.
	saveState(persistedState{
		WireURL:    wireURL,
		ConfigPath: configPath,
		State:      StateConnected,
		Timestamp:  time.Now(),
	})

	return nil
}

// doDown disconnects the tunnel.
func (d *Daemon) doDown() {
	d.setState(StateDisconnecting)

	// TODO: Actually stop the tunnel here.
	// Requires L4 tunnel rewrite.

	d.setState(StateStopped)

	d.mu.Lock()
	d.monitorPID = 0
	d.mu.Unlock()

	saveState(persistedState{State: StateStopped, Timestamp: time.Now()})
}

// tryAutoReconnect checks persisted state and reconnects if applicable.
func (d *Daemon) tryAutoReconnect() {
	st, err := loadState()
	if err != nil || st.State != StateConnected {
		return
	}

	// Only auto-reconnect if disconnected less than 1 hour ago.
	if time.Since(st.Timestamp) > 1*time.Hour {
		log.Println("Auto-reconnect: stale state (>1h), skipping")
		return
	}

	log.Println("Auto-reconnect: reconnecting with saved config...")
	go func() {
		// Delay 5s to let daemon fully start.
		time.Sleep(5 * time.Second)
		if err := d.doUp(st.WireURL, st.ConfigPath, 0); err != nil {
			log.Printf("Auto-reconnect failed: %v", err)
		}
	}()
}

// statusInfo returns current status as a map.
func (d *Daemon) statusInfo() map[string]any {
	d.mu.RLock()
	defer d.mu.RUnlock()

	info := map[string]any{
		"state": d.state,
	}
	if d.state == StateConnected {
		info["connected_at"] = d.connectedAt.Format(time.RFC3339)
		info["uptime_seconds"] = int(time.Since(d.connectedAt).Seconds())
	}
	if d.lastError != "" {
		info["error"] = d.lastError
	}
	if d.lastWireURL != "" {
		info["wire_url"] = d.lastWireURL
	}
	if d.lastConfigPath != "" {
		info["config_path"] = d.lastConfigPath
	}
	return info
}
