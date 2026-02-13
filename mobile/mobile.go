package mobile

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/kaitu-io/k2rule"

	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/core/dns"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/wire"
)

// Engine is the mobile VPN engine. It assembles the tunnel stack
// (wire transports, router, DNS middleware, provider) and manages
// the connection lifecycle. Exported as a struct for gomobile binding.
type Engine struct {
	mu      sync.Mutex
	state   string
	handler EventHandler

	cancel context.CancelFunc
	tunnel *core.ClientTunnel
	tm     *wire.TransportManager
	prov   provider.Provider
}

// NewEngine creates a new Engine in the disconnected state.
func NewEngine() *Engine {
	return &Engine{
		state: StateDisconnected,
	}
}

// SetEventHandler registers a callback receiver for state changes,
// errors, and traffic stats. Pass nil to clear.
func (e *Engine) SetEventHandler(h EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handler = h
}

// Status returns the current engine state.
func (e *Engine) Status() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

// Start assembles the tunnel stack and begins forwarding traffic.
// url is a k2v5:// connection URL; fd is the TUN file descriptor
// provided by the platform (iOS NEPacketTunnelProvider / Android VpnService).
func (e *Engine) Start(url string, fd int) error {
	e.mu.Lock()
	if e.state != StateDisconnected {
		e.mu.Unlock()
		return fmt.Errorf("engine: already started")
	}
	e.setState(StateConnecting)
	e.mu.Unlock()

	// 1. Parse k2v5:// URL.
	wireCfg, err := wire.ParseURL(url)
	if err != nil {
		return e.fail(err)
	}

	// 2. Init k2rule (global mode for mobile v1).
	k2rule.Init(&k2rule.Config{IsGlobal: true, GlobalTarget: k2rule.TargetProxy})

	// 3. Build transports.
	quicClient := wire.NewQUICClient(wireCfg, nil)
	tcpwsClient := wire.NewTCPWSClient(wireCfg, nil)
	tm := wire.NewTransportManager()
	switch wireCfg.Transport {
	case "quic":
		tm.SetQUIC(quicClient)
	case "tcpws":
		tm.SetTCPWS(tcpwsClient)
	default:
		tm.SetQUIC(quicClient)
		tm.SetTCPWS(tcpwsClient)
	}

	// 4. Router.
	router := core.NewRouter()

	// 5. DNS middleware (L4 mode -- no writer needed).
	dnsMW := dns.NewMiddleware(dns.MiddlewareConfig{
		ProxyDNS: dns.NewProxyDNSClient(dns.ProxyDNSClientConfig{Dialer: tm}),
		DNSCache: dns.NewDNSCache(dns.DNSCacheConfig{}),
		Filter:   dns.NewFilter(dns.FilterConfig{}),
	})

	// 6. TUN provider (from platform FD).
	prov := provider.NewTUNProvider(provider.ProviderConfig{
		FileDescriptor: fd,
	})

	// 7. Client tunnel.
	ctx, cancel := context.WithCancel(context.Background())
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{
		Provider: prov,
		Dialer:   tm,
		Router:   router,
	})

	// 8. Start with DNS-intercepting handler wrapper.
	// We call prov.Start() directly (instead of tunnel.Start()) to insert
	// the dnsHandler wrapper between provider and tunnel.
	if err := prov.Start(ctx, &dnsHandler{inner: tunnel, dns: dnsMW}); err != nil {
		cancel()
		tm.Close()
		return e.fail(err)
	}

	e.mu.Lock()
	e.cancel = cancel
	e.tunnel = tunnel
	e.tm = tm
	e.prov = prov
	e.setState(StateConnected)
	e.mu.Unlock()

	return nil
}

// Stop tears down the tunnel and returns the engine to disconnected.
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state == StateDisconnected {
		return nil
	}

	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.prov != nil {
		e.prov.Close()
		e.prov = nil
	}
	if e.tm != nil {
		e.tm.Close()
		e.tm = nil
	}
	e.tunnel = nil

	e.setState(StateDisconnected)
	return nil
}

// setState updates state and notifies the EventHandler.
// Must be called with e.mu held.
func (e *Engine) setState(s string) {
	e.state = s
	if e.handler != nil {
		e.handler.OnStateChange(s)
	}
}

// fail records an error, resets to disconnected, and returns the error.
func (e *Engine) fail(err error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.handler != nil {
		e.handler.OnError(err.Error())
	}
	e.setState(StateDisconnected)
	return err
}

// =========================================================================
// dnsHandler wraps a ConnectionHandler to intercept DNS queries (port 53).
// =========================================================================

type dnsHandler struct {
	inner provider.ConnectionHandler
	dns   *dns.Middleware
}

func (h *dnsHandler) HandleTCP(conn net.Conn, dest string) {
	h.inner.HandleTCP(conn, dest)
}

func (h *dnsHandler) HandleUDP(conn net.PacketConn, dest string) {
	_, port, _ := net.SplitHostPort(dest)
	if port == "53" && h.dns != nil {
		h.serveDNS(conn)
		return
	}
	h.inner.HandleUDP(conn, dest)
}

func (h *dnsHandler) serveDNS(conn net.PacketConn) {
	defer conn.Close()
	buf := make([]byte, 4096)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		resp, err := h.dns.HandleQuery(buf[:n])
		if err != nil {
			continue
		}
		conn.WriteTo(resp, addr)
	}
}
