package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
	"time"

	"github.com/kaitu-io/k2rule"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/directdial"
	"github.com/kaitu-io/k2/engine"
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
	lastConfig *config.ClientConfig

	// Error from last connection attempt (orthogonal to state).
	lastError string

	// Connected at timestamp.
	connectedAt time.Time

	// Process monitor PID.
	monitorPID int

	// Active engine and its cancel function for PID monitoring.
	engine    *engine.Engine
	engineCtx context.Context
	engineCnl context.CancelFunc

	// EngineStarter allows overriding engine creation for testing.
	// It should create an engine, start it with the given config, and return it.
	// When nil, the default production path (engine.New + Start) is used.
	EngineStarter func(cfg engine.Config) (*engine.Engine, error)
}

// New creates a new daemon.
func New() *Daemon {
	return &Daemon{
		state: StateStopped,
	}
}

// Run starts the daemon HTTP server and blocks until ctx is cancelled.
// If listenAddr is empty, DefaultAddr is used.
func (d *Daemon) Run(ctx context.Context, listenAddr string) error {
	if listenAddr == "" {
		listenAddr = DefaultAddr
	}
	log.Printf("k2 daemon starting on %s", listenAddr)

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", d.handlePing)
	mux.HandleFunc("/metrics", d.handleMetrics)
	mux.HandleFunc("/api/core", d.handleCore)
	mux.HandleFunc("/api/device/udid", d.handleDeviceUDID)
	mux.Handle("/", webappHandler())

	srv := &http.Server{
		Addr:    listenAddr,
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

// closeTunnel tears down the active engine. Caller must hold opMu.
// Idempotent: no-op if engine is nil.
func (d *Daemon) closeTunnel() {
	d.mu.Lock()
	eng := d.engine
	cancel := d.engineCnl
	d.engine = nil
	d.engineCnl = nil
	d.engineCtx = nil
	d.monitorPID = 0
	d.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if eng != nil {
		eng.Stop()
	}

	d.mu.Lock()
	d.setStateLocked(StateStopped)
	d.mu.Unlock()

	saveState(persistedState{State: StateStopped, Timestamp: time.Now()})
}

// doUp connects the tunnel with the given ClientConfig.
func (d *Daemon) doUp(cfg *config.ClientConfig, pid int) error {
	d.opMu.Lock()
	defer d.opMu.Unlock()

	// Unconditionally tear down any existing tunnel (idempotent).
	d.closeTunnel()

	d.mu.Lock()
	d.setStateLocked(StateConnecting)
	d.lastConfig = cfg
	d.monitorPID = pid
	d.lastError = ""
	d.mu.Unlock()

	// Persist state for auto-reconnect across restarts.
	saveState(persistedState{
		Config:    cfg,
		State:     StateConnecting,
		Timestamp: time.Now(),
	})

	// Build engine config from ClientConfig.
	dd := directdial.New()
	if dd.InterfaceName() != "" {
		log.Printf("tunnel: direct dialer bound to %s", dd.InterfaceName())
	}
	engineCfg := d.engineConfigFromClientConfig(cfg, dd)

	// Create and start engine.
	starter := d.EngineStarter
	if starter == nil {
		starter = defaultEngineStarter
	}
	eng, err := starter(engineCfg)
	if err != nil {
		msg := fmt.Sprintf("start engine: %v", err)
		d.mu.Lock()
		d.lastError = msg
		d.setStateLocked(StateStopped)
		d.mu.Unlock()
		log.Printf("Error: %s", msg)
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())

	d.mu.Lock()
	d.engine = eng
	d.engineCtx = ctx
	d.engineCnl = cancel
	d.connectedAt = time.Now()
	d.setStateLocked(StateConnected)
	d.mu.Unlock()

	// Start PID monitoring if requested.
	// Uses engineCtx so the goroutine stops when closeTunnel() cancels the context.
	if pid > 0 {
		go d.monitorProcess(ctx, pid)
	}

	// Update persisted state.
	saveState(persistedState{
		Config:    cfg,
		State:     StateConnected,
		Timestamp: time.Now(),
	})

	return nil
}

// defaultEngineStarter is the production engine creation path.
func defaultEngineStarter(cfg engine.Config) (*engine.Engine, error) {
	eng := engine.New()
	if err := eng.Start(cfg); err != nil {
		return nil, err
	}
	return eng, nil
}

// engineConfigFromClientConfig maps a config.ClientConfig to an engine.Config.
func (d *Daemon) engineConfigFromClientConfig(cfg *config.ClientConfig, dd *directdial.Dialer) engine.Config {
	ecfg := engine.Config{
		WireURL:        cfg.Server,
		FileDescriptor: -1, // Desktop: engine creates its own TUN.
		DirectDialer:   dd,
		PreferIPv6:     true,
		Mode:           cfg.Mode,
		ProxyListen:    cfg.Proxy.Listen,
	}

	// Compute DNSExclude from DNS config + server host resolution.
	ecfg.DNSExclude = d.computeDNSExclude(cfg)

	// Map rule config if present.
	if cfg.Rule.RuleURL != "" || cfg.Rule.GeoIPURL != "" || cfg.Rule.PornURL != "" ||
		cfg.Rule.CacheDir != "" || cfg.Rule.Global {
		ecfg.RuleConfig = d.buildRuleConfig(cfg)
	}

	return ecfg
}

// computeDNSExclude builds the list of IPs to exclude from TUN routing.
func (d *Daemon) computeDNSExclude(cfg *config.ClientConfig) []netip.Addr {
	var addrs []netip.Addr

	// Parse DNS server IPs.
	for _, s := range cfg.DNS.Direct {
		host, _, err := splitHostPort(s)
		if err != nil {
			continue
		}
		if addr, err := netip.ParseAddr(host); err == nil {
			addrs = append(addrs, addr)
		}
	}

	// Resolve server host to IPs for route exclusion.
	if serverHost := extractServerHost(cfg.Server); serverHost != "" {
		if ips, err := net.LookupHost(serverHost); err == nil {
			for _, ip := range ips {
				if addr, err := netip.ParseAddr(ip); err == nil {
					addrs = append(addrs, addr)
				}
			}
		}
	}

	return addrs
}

// extractServerHost extracts the hostname from a k2v5:// URL.
func extractServerHost(wireURL string) string {
	// The wire URL format is: k2v5://user:pass@host:port
	u, err := url.Parse(wireURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// buildRuleConfig creates a k2rule.Config from client config.
func (d *Daemon) buildRuleConfig(cfg *config.ClientConfig) *k2rule.Config {
	ruleCfg := &k2rule.Config{
		RuleURL:  cfg.Rule.RuleURL,
		GeoIPURL: cfg.Rule.GeoIPURL,
		PornURL:  cfg.Rule.PornURL,
		CacheDir: cfg.Rule.CacheDir,
		IsGlobal: cfg.Rule.Global,
	}
	if cfg.Rule.Global {
		ruleCfg.GlobalTarget = k2rule.TargetProxy
	}
	return ruleCfg
}

// splitHostPort splits a host:port string. Unlike net.SplitHostPort, it
// doesn't require brackets around IPv6 addresses.
func splitHostPort(hostport string) (string, string, error) {
	for i := len(hostport) - 1; i >= 0; i-- {
		if hostport[i] == ':' {
			return hostport[:i], hostport[i+1:], nil
		}
	}
	return hostport, "", fmt.Errorf("missing port in %q", hostport)
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

	if st.Config == nil {
		log.Println("Auto-reconnect: no config in saved state, skipping")
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
		if err := d.doUp(st.Config, 0); err != nil {
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
	if d.lastConfig != nil {
		info["config"] = d.lastConfig
	}
	return info
}
