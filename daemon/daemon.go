package daemon

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/core"
)

// DefaultAddr is the daemon HTTP listen address.
const DefaultAddr = "127.0.0.1:1777"

// Connection states.
const (
	StateStopped    = "stopped"
	StateConnecting = "connecting"
	StateConnected  = "connected"
)

// Daemon is the k2 background service.
// It manages the VPN tunnel lifecycle and exposes an HTTP API.
//
// Concurrency model:
//   - opMu serializes doUp/doDown (coarse lifecycle lock)
//   - mu protects field reads/writes (fine-grained data lock)
type Daemon struct {
	opMu sync.Mutex   // serializes doUp/doDown operations
	mu   sync.RWMutex // protects field access

	state string

	// Last config used for auto-reconnect.
	lastWireURL    string
	lastConfigPath string

	// Error from last connection attempt (orthogonal to state).
	lastError string

	// Connected at timestamp.
	connectedAt time.Time

	// Process monitor PID.
	monitorPID int

	// Active tunnel.
	tunnel    *core.ClientTunnel
	tunnelCtx context.Context
	tunnelCnl context.CancelFunc

	// TunnelBuilder allows overriding tunnel creation for testing.
	TunnelBuilder func(ctx context.Context, cfg *config.ClientConfig) (*core.ClientTunnel, error)
}

// New creates a new daemon.
func New() *Daemon {
	return &Daemon{
		state: StateStopped,
	}
}

// Run starts the daemon HTTP server and blocks until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	log.Printf("k2 daemon starting on %s", DefaultAddr)

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", d.handlePing)
	mux.HandleFunc("/metrics", d.handleMetrics)
	mux.HandleFunc("/api/core", d.handleCore)
	mux.HandleFunc("/api/device/udid", d.handleDeviceUDID)
	mux.Handle("/", webappHandler())

	srv := &http.Server{
		Addr:    DefaultAddr,
		Handler: corsMiddleware(mux),
	}

	// Load persisted state and auto-reconnect if applicable.
	d.tryAutoReconnect(ctx)

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

// setStateLocked sets state. Caller must hold mu.
func (d *Daemon) setStateLocked(s string) {
	log.Printf("State: %s → %s", d.state, s)
	d.state = s
}

// closeTunnel tears down the active tunnel. Caller must hold opMu.
// Idempotent: no-op if tunnel is nil.
func (d *Daemon) closeTunnel() {
	d.mu.Lock()
	tunnel := d.tunnel
	cancel := d.tunnelCnl
	d.tunnel = nil
	d.tunnelCnl = nil
	d.tunnelCtx = nil
	d.monitorPID = 0
	d.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if tunnel != nil {
		tunnel.Close()
	}

	d.mu.Lock()
	d.setStateLocked(StateStopped)
	d.mu.Unlock()

	saveState(persistedState{State: StateStopped, Timestamp: time.Now()})
}

// doUp connects the tunnel with the given wire URL or config path.
func (d *Daemon) doUp(wireURL, configPath string, pid int) error {
	d.opMu.Lock()
	defer d.opMu.Unlock()

	// Unconditionally tear down any existing tunnel (idempotent).
	d.closeTunnel()

	d.mu.Lock()
	d.setStateLocked(StateConnecting)
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

	// Build config from wire URL or config file.
	var cfg *config.ClientConfig
	if configPath != "" {
		var err error
		cfg, err = config.LoadClient(configPath)
		if err != nil {
			msg := fmt.Sprintf("load config: %v", err)
			d.mu.Lock()
			d.lastError = msg
			d.setStateLocked(StateStopped)
			d.mu.Unlock()
			log.Printf("Error: %s", msg)
			return err
		}
	} else {
		cfg = config.ClientFromURL(wireURL)
	}

	// Create and start tunnel.
	builder := d.TunnelBuilder
	if builder == nil {
		builder = BuildTunnel
	}
	ctx, cancel := context.WithCancel(context.Background())
	tunnel, err := builder(ctx, cfg)
	if err != nil {
		cancel()
		msg := fmt.Sprintf("build tunnel: %v", err)
		d.mu.Lock()
		d.lastError = msg
		d.setStateLocked(StateStopped)
		d.mu.Unlock()
		log.Printf("Error: %s", msg)
		return err
	}

	d.mu.Lock()
	d.tunnel = tunnel
	d.tunnelCtx = ctx
	d.tunnelCnl = cancel
	d.connectedAt = time.Now()
	d.setStateLocked(StateConnected)
	d.mu.Unlock()

	// Start PID monitoring if requested.
	// Uses tunnelCtx so the goroutine stops when closeTunnel() cancels the context.
	if pid > 0 {
		go d.monitorProcess(ctx, pid)
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
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.closeTunnel()
}

// tryAutoReconnect checks persisted state and reconnects if applicable.
// ctx is the daemon's lifecycle context — reconnect is aborted if ctx is cancelled.
func (d *Daemon) tryAutoReconnect(ctx context.Context) {
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
		// Delay 5s to let daemon fully start; abort if daemon shuts down.
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			log.Println("Auto-reconnect: cancelled during startup delay")
			return
		}
		// Skip if already connected (e.g. user called up during delay).
		if d.State() == StateConnected {
			return
		}
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
