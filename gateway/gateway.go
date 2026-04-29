//go:build linux

package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/subscription"
	"github.com/kaitu-io/k2/webui"
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

	storage         *webui.Storage
	version         string
	commit          string
	arch            string
	routerDeviceMgr *RouterDeviceManager
	updater         *webui.Upgrader

	// Subscription manager — handles k2subs:// routes, OutboundProvider
	// hot-swap, exclude state. Cleared on doDown via CloseAll.
	subsMgr *subscription.Manager
}

// k2rReleaseEndpoints lists the CDN base URLs used by k2r's OTA upgrader.
// Tried in order; first successful LATEST + checksums.txt fetch wins.
var k2rReleaseEndpoints = []string{
	"https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2r",
	"https://dl.kaitu.io/kaitu/k2r",
}

// newK2rUpgrader wires a webui.Upgrader with k2r-specific CDN, state, and
// restart configuration. RestartFunc is detected at call time so OpenWRT
// hosts use procd while systemd hosts use systemctl.
func newK2rUpgrader(version string) *webui.Upgrader {
	return webui.NewUpgrader(version, webui.UpgradeConfig{
		BinaryName:       "k2r",
		StateDir:         stateDir(),
		ReleaseEndpoints: k2rReleaseEndpoints,
		RestartFunc: func() {
			if webui.IsOpenWRT() {
				exec.Command("/etc/init.d/k2r", "restart").Run()
			} else {
				exec.Command("systemctl", "restart", "k2r").Run()
			}
		},
	})
}

// New creates a Gateway ready to Run.
func New(version, commit, arch string) *Gateway {
	ctx, cancel := context.WithCancel(context.Background())
	g := &Gateway{
		ctx:        ctx,
		cancel:     cancel,
		lastStatus: engine.Status{State: engine.StateDisconnected},
		sseClients: make(map[chan []byte]struct{}),
		storage:    webui.NewStorage(filepath.Join(stateDir(), "storage.json")),
		version:    version,
		commit:     commit,
		arch:       arch,
		updater:    newK2rUpgrader(version),
	}
	g.subsMgr = subscription.NewManager(
		filepath.Join(stateDir(), "subs"),
		nil,
		nil, // probe.Registry is daemon-only for now; gateway can wire later
	)
	return g
}

// Run starts the HTTP API server and blocks until Stop is called or the
// listener fails. It cleans stale firewall rules on startup and attempts
// auto-reconnect from saved state.
func (g *Gateway) Run(addr string) error {
	defer g.handlePanic()

	g.cleanStaleRules()

	if g.updater != nil {
		g.updater.RecoverOnStartup()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", g.handlePing)
	mux.HandleFunc("/api/core", g.handleCore)
	mux.HandleFunc("/api/events", g.handleEvents)
	mux.HandleFunc("/api/log-level", g.handleLogLevel)

	// Shared webui endpoints (storage, platform, upgrade) — same handlers
	// as cmd/k2 (Linux desktop) uses via the daemon package.
	mux.HandleFunc("/api/storage", webui.NewStorageHandler(g.storage))
	mux.HandleFunc("/api/platform", webui.NewPlatformHandler(webui.PlatformInfo{
		PlatformType: "gateway",
		Version:      g.version,
		Commit:       g.commit,
		Arch:         g.arch,
	}))
	mux.HandleFunc("/api/upgrade", webui.NewUpgradeHandler(g.updater))

	// Router device management (state resolved per-request — nil until doUp).
	mux.HandleFunc("/api/router-devices", g.handleRouterDevices)
	mux.HandleFunc("/api/router-devices/allow", g.handleRouterDeviceAllow)
	mux.HandleFunc("/api/router-devices/remove", g.handleRouterDeviceRemove)
	mux.HandleFunc("/api/router-devices/mode", g.handleRouterDeviceMode)

	if webapp := webui.NewWebappHandler(g.version, g.commit, g.arch); webapp != nil {
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

	// Resolve k2subs:// routes BEFORE building the interceptor or engine so the
	// engine sees a fully-resolved (k2v5://) ClientConfig. Sessions are owned
	// by g.subsMgr; cleanup is deferred until either an early-return path or
	// (on success) doDown via closeTunnel/CloseAll.
	subs, resolved, subsErr := g.subsMgr.Resolve(&cfg.ClientConfig)
	if subsErr != nil {
		sc.Restore()
		return fmt.Errorf("gateway: subscription resolve: %w", subsErr)
	}
	subsCleanup := true
	defer func() {
		if subsCleanup {
			g.subsMgr.CloseAll()
		}
	}()
	_ = subs // sessions captured on g.subsMgr; reference kept for log/debug parity

	interceptor, err := NewInterceptor()
	if err != nil {
		sc.Restore()
		return err
	}

	// Initialize RouterDeviceManager with quota from user Tier
	rdm := &RouterDeviceManager{
		quota:       cfg.MaxLanClient,
		lanSubnets:  subnets,
		interceptor: interceptor,
		storage:     g.storage,
	}
	rdm.loadFromStorage()

	// Build extended InterceptConfig with all router features
	icfg := InterceptConfig{
		ListenPort:    cfg.ListenPort,
		LANSubnets:    subnets,
		ExcludeIPs:    excludeIPs,
		DNSRedirect:   cfg.DNSRedirect,
		DNSPort:       cfg.DNSPort,
		AllowlistMode: rdm.mode == "allowlist",
		AllowedMACs:   rdm.getAllowedMACs(),
	}

	// Set mode change callback — triggers interceptor reinstall
	rdm.onModeChange = func() {
		rdm.mu.RLock()
		newICfg := icfg
		newICfg.AllowlistMode = rdm.mode == "allowlist"
		newICfg.AllowedMACs = make([]string, len(rdm.entries))
		for i, e := range rdm.entries {
			newICfg.AllowedMACs[i] = e.MAC
		}
		rdm.mu.RUnlock()

		g.mu.Lock()
		interceptor := g.interceptor
		g.mu.Unlock()

		if interceptor != nil {
			interceptor.Remove()
			if err := interceptor.Install(newICfg); err != nil {
				slog.Error("gateway: failed to reinstall interceptor after mode change", "err", err)
			}
		}
	}

	prov := newGatewayProvider(interceptor, icfg)

	// Engine runs against the resolved client (k2subs:// → k2v5://). The
	// original cfg.ClientConfig keeps the k2subs:// route for persistence so
	// a restart re-fetches the subscription.
	engineClient := subscription.ApplyResolved(&cfg.ClientConfig, resolved)

	eng := engine.New()
	eng.SetEventHandler(g)
	ecfg := engineConfig(engineClient, prov, g.broadcastSnapshot, g.subsMgr)
	if err := eng.Start(ecfg); err != nil {
		prov.Close()
		interceptor.Remove()
		sc.Restore()
		return err
	}
	subsCleanup = false // engine owns sessions until doDown/closeTunnel

	g.mu.Lock()
	g.eng = eng
	g.interceptor = interceptor
	g.provider = prov
	g.sysctl = sc
	g.routerDeviceMgr = rdm
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
	g.routerDeviceMgr = nil
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

// OnOutboundFatal implements engine.EventHandler. Fired when the
// OutboundProvider (subsMgr) returns no further candidates and the engine
// has terminated the tunnel. Gateway logs and updates lastStatus; the SSE
// broadcast happens via OnStatus's normal path.
func (g *Gateway) OnOutboundFatal(outbound string, err error) {
	slog.Warn("gateway: outbound fatal", "url", config.TruncateVia(outbound), "err", err)
}

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
