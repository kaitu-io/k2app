//go:build linux

package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/kaitu-io/k2/engine"
	"github.com/kaitu-io/k2/safego"
	"github.com/sasha-s/go-deadlock"
)

// Gateway is the k2r daemon lifecycle manager. It owns the engine, interceptor,
// provider, and sysctl state. HTTP API on DefaultGatewayAddr exposes control
// (up/down/status) and SSE event streaming.
//
// Lock ordering: opMu (serialization) -> mu (state) -> sseMu (state).
// opMu serializes doUp/doDown (may block on engine start/stop).
// mu protects field reads/writes only.
type Gateway struct {
	opMu deadlock.Mutex // serialization: doUp/doDown (never nested inside mu)
	mu   deadlock.Mutex // state: protects fields below

	eng         *engine.Engine
	lastStatus  engine.Status
	interceptor Interceptor
	provider    *gatewayProvider
	sysctl      sysctlState

	httpServer *http.Server
	sseClients map[chan []byte]struct{}
	sseMu      deadlock.RWMutex

	ctx    context.Context
	cancel context.CancelFunc

	storage *Storage
	version string
	commit  string
	arch    string
}

// New creates a Gateway ready to Run.
func New(version, commit, arch string) *Gateway {
	ctx, cancel := context.WithCancel(context.Background())
	return &Gateway{
		ctx:        ctx,
		cancel:     cancel,
		lastStatus: engine.Status{State: engine.StateDisconnected},
		sseClients: make(map[chan []byte]struct{}),
		storage:    NewDefaultStorage(),
		version:    version,
		commit:     commit,
		arch:       arch,
	}
}

// Run starts the HTTP API server and blocks until Stop is called or the
// listener fails. It cleans stale firewall rules on startup and attempts
// auto-reconnect from saved state.
func (g *Gateway) Run(addr string) error {
	defer g.handlePanic()

	g.cleanStaleRules()

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", g.handlePing)
	mux.HandleFunc("/api/core", g.handleCore)
	mux.HandleFunc("/api/events", g.handleEvents)
	mux.HandleFunc("/api/log-level", g.handleLogLevel)
	mux.HandleFunc("/api/storage", g.handleStorage)
	mux.HandleFunc("/api/platform", g.handlePlatform)
	mux.HandleFunc("/api/upgrade", g.handleUpgrade)

	if webapp := newWebappHandler(g.version, g.commit, g.arch); webapp != nil {
		mux.Handle("/", webapp)
	}

	g.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	safego.Go(g.tryAutoReconnect)

	slog.Info("gateway: HTTP API listening", "addr", addr)

	errCh := make(chan error, 1)
	safego.Go(func() {
		errCh <- g.httpServer.ListenAndServe()
	})

	select {
	case <-g.ctx.Done():
		g.doDown()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		g.httpServer.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// Stop cancels the gateway context, triggering graceful shutdown of the
// HTTP server and tunnel.
func (g *Gateway) Stop() {
	g.cancel()
}

// doUp connects with the given config. Acquires opMu to serialize.
func (g *Gateway) doUp(cfg *GatewayConfig) error {
	g.opMu.Lock()
	defer g.opMu.Unlock()

	g.closeTunnel()

	var subnets []string
	var err error
	if len(cfg.LANSubnets) > 0 {
		subnets = cfg.LANSubnets
	} else {
		subnets, err = DiscoverLANSubnets()
		if err != nil {
			return err
		}
	}
	slog.Info("DIAG: gw-lan-discover", "subnets", subnets, "ifaceCount", len(subnets))

	excludeIPs, err := resolveExcludeIPs(cfg.Server, cfg.DNS.Direct)
	if err != nil {
		return err
	}

	var sc sysctlState
	if err := sc.Enable(); err != nil {
		return err
	}

	interceptor, err := NewInterceptor()
	if err != nil {
		sc.Restore()
		return err
	}

	icfg := InterceptConfig{
		ListenPort: cfg.ListenPort,
		LANSubnets: subnets,
		ExcludeIPs: excludeIPs,
	}
	prov := newGatewayProvider(interceptor, icfg)

	eng := engine.New()
	eng.SetEventHandler(g)
	ecfg := engineConfig(cfg, prov, g.broadcastSnapshot)
	if err := eng.Start(ecfg); err != nil {
		prov.Close()
		sc.Restore()
		return err
	}

	g.mu.Lock()
	g.eng = eng
	g.interceptor = interceptor
	g.provider = prov
	g.sysctl = sc
	g.mu.Unlock()

	saveState(&persistedState{Config: cfg, State: "connected", Timestamp: time.Now()})
	return nil
}

// doDown disconnects and cleans up. Acquires opMu to serialize.
func (g *Gateway) doDown() {
	g.opMu.Lock()
	defer g.opMu.Unlock()
	g.closeTunnel()
	saveState(&persistedState{State: "disconnected", Timestamp: time.Now()})
}

// closeTunnel extracts resources under mu, then closes outside lock.
// Caller must hold opMu.
func (g *Gateway) closeTunnel() {
	g.mu.Lock()
	eng := g.eng
	g.eng = nil
	interceptor := g.interceptor
	g.interceptor = nil
	g.provider = nil
	sc := g.sysctl
	g.sysctl = sysctlState{}
	g.mu.Unlock()

	if eng != nil {
		eng.Stop()
	}
	if interceptor != nil {
		interceptor.Remove()
	}
	sc.Restore()
}

// --- EventHandler impl (engine.EventHandler) ---

// OnStatus implements engine.EventHandler.
func (g *Gateway) OnStatus(status engine.Status) {
	g.mu.Lock()
	g.lastStatus = status
	g.mu.Unlock()
	g.broadcastStatus(status)
}

// OnStats implements engine.EventHandler.
func (g *Gateway) OnStats(tx, rx int64) {}

func (g *Gateway) broadcastSnapshot(snap engine.HealthSnapshot) {
	data, _ := json.Marshal(snap)
	g.broadcastSSE("stats", data)
}

func (g *Gateway) broadcastStatus(status engine.Status) {
	data, _ := json.Marshal(status)
	g.broadcastSSE("status", data)
}

// --- Recovery ---

// cleanStaleRules removes any leftover firewall rules from a previous crash.
func (g *Gateway) cleanStaleRules() {
	if i, err := NewInterceptor(); err == nil {
		i.Remove()
	}
}

// handlePanic is a deferred recovery handler that cleans up firewall rules
// and sysctl on panic.
func (g *Gateway) handlePanic() {
	if r := recover(); r != nil {
		slog.Error("gateway: panic", "err", r, "stack", string(debug.Stack()))
		g.mu.Lock()
		interceptor := g.interceptor
		g.interceptor = nil
		sc := g.sysctl
		g.sysctl = sysctlState{}
		g.mu.Unlock()
		if interceptor != nil {
			interceptor.Remove()
		}
		sc.Restore()
	}
}

// tryAutoReconnect loads persisted state and reconnects if the gateway was
// previously connected within the last 7 days.
func (g *Gateway) tryAutoReconnect() {
	st := loadState()
	if st == nil || st.State != "connected" || st.Config == nil {
		return
	}
	if time.Since(st.Timestamp) > 7*24*time.Hour {
		return
	}
	// Derive Server from routes (Server is json:"-", not persisted).
	SetGatewayDefaults(st.Config)
	time.Sleep(3 * time.Second) // wait for HTTP server + network stack to be ready
	select {
	case <-g.ctx.Done():
		return
	default:
	}
	slog.Info("gateway: auto-reconnecting from saved state")
	if err := g.doUp(st.Config); err != nil {
		slog.Warn("gateway: auto-reconnect failed", "err", err)
	}
}
