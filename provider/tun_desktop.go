//go:build (darwin && !ios) || (linux && !android) || windows

package provider

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"runtime"
	"time"

	"github.com/sagernet/sing/common/control"

	"github.com/sagernet/sing-tun"
)

// defaultTunName returns a platform-appropriate TUN device name.
// On macOS, sing-tun uses the parsed index from "utun%d" as the kernel unit number.
// We must pick an index not already in use, otherwise Connect returns "resource busy".
func defaultTunName() string {
	switch runtime.GOOS {
	case "darwin":
		return nextFreeDarwinUtun()
	case "linux":
		return "tun0"
	default: // windows
		return "k2tun"
	}
}

// nextFreeDarwinUtun finds the first unused utun index on macOS.
func nextFreeDarwinUtun() string {
	for i := 0; i < 256; i++ {
		name := fmt.Sprintf("utun%d", i)
		if _, err := net.InterfaceByName(name); err != nil {
			return name
		}
	}
	return "utun255"
}

// TUNProvider creates a TUN device and runs a sing-tun stack for L3→L4 conversion.
//
// Lifecycle (Start/Close) is serialized by the engine — no internal locking.
type TUNProvider struct {
	cfg         ProviderConfig
	tunIf       tun.Tun
	stack       tun.Stack
	name        string
	dnsOverride *dnsOverride // macOS: system DNS → TUN IP (nil on other platforms)
	// fdConsumed is set once tun.New takes ownership of cfg.FileDescriptor
	// (macOS sysext FD path; tunIf.Close then releases it). Until then Close
	// must release the raw fd itself. Irrelevant on the desktop self-create
	// path (FileDescriptor <= 0).
	fdConsumed bool
}

var _ Provider = (*TUNProvider)(nil)

// NewTUNProvider creates a TUN-based provider.
func NewTUNProvider(cfg ProviderConfig) *TUNProvider {
	cfg.SetDefaults()
	return &TUNProvider{cfg: cfg}
}

func (p *TUNProvider) Start(ctx context.Context, handler ConnectionHandler) error {
	ipv4Prefix, err := netip.ParsePrefix(p.cfg.IPv4Address)
	if err != nil {
		return fmt.Errorf("provider: parse IPv4: %w", err)
	}
	ipv6Prefix, err := netip.ParsePrefix(p.cfg.IPv6Address)
	if err != nil {
		return fmt.Errorf("provider: parse IPv6: %w", err)
	}

	tunOpts := tun.Options{
		Name:                    defaultTunName(),
		MTU:                     uint32(p.cfg.MTU),
		Inet4Address:            []netip.Prefix{ipv4Prefix},
		Inet6Address:            []netip.Prefix{ipv6Prefix},
		AutoRoute:               true,
		StrictRoute:             true,
		EXP_MultiPendingPackets: runtime.GOOS == "darwin" && p.cfg.MTU < 32768,
		// DNSServers intentionally NOT set — sing-tun auto-derives TUN_IP.Next()
		// which tells the OS to send DNS through TUN for Handler-level interception.
		// On Windows: luid.SetDNS(TUN_IP.Next())
		// On Linux:   resolvectl dns <tun> TUN_IP.Next()
		// On macOS:   dns_darwin.go overrides system DNS → TUN IP via SCDynamicStore
	}

	// Exclude direct DNS upstream IPs and wire server IPs from TUN routing.
	// This prevents DirectDNSClient queries from looping back into the TUN.
	for _, addr := range p.cfg.RouteExcludeAddrs {
		if addr.Is4() {
			tunOpts.Inet4RouteExcludeAddress = append(tunOpts.Inet4RouteExcludeAddress,
				netip.PrefixFrom(addr, 32))
		} else {
			tunOpts.Inet6RouteExcludeAddress = append(tunOpts.Inet6RouteExcludeAddress,
				netip.PrefixFrom(addr, 128))
		}
	}

	// Pass interface monitor for TUN self-exclusion (prevents self-triggering).
	// Desktop: daemon provides via engine.Config.InterfaceMonitor.
	// macOS NE: appext/nePlatform provides via engine.Config.InterfaceMonitor.
	if m, ok := p.cfg.InterfaceMonitor.(tun.DefaultInterfaceMonitor); ok {
		tunOpts.InterfaceMonitor = m
	}

	if p.cfg.FileDescriptor > 0 {
		// NE / mobile FD path: platform already created TUN and manages routes.
		// Skip Name (don't create new device) and AutoRoute (NE handles routing).
		tunOpts.FileDescriptor = p.cfg.FileDescriptor
		tunOpts.Name = ""
		tunOpts.AutoRoute = false
	}

	// Step 1: Create TUN device (FD ready, but routes not yet set).
	tunIf, err := tun.New(tunOpts)
	if err != nil {
		return fmt.Errorf("provider: create TUN: %w", err)
	}
	p.fdConsumed = true // FD path: tunIf owns the fd from here (tunIf.Close releases it)

	// Step 2: Create stack (TCP listener not yet bound).
	// Use sing-tun's NewStack dispatcher (mirrors sing-box pattern):
	//   IncludeAllNetworks=true (NE FD path) → gVisor (full userspace TCP/IP)
	//   IncludeAllNetworks=false (daemon)     → Mixed (gVisor UDP + System TCP)
	stack, err := tun.NewStack("", tun.StackOptions{
		Context:            ctx,
		Tun:                tunIf,
		TunOptions:         tunOpts,
		UDPTimeout:         5 * time.Minute,
		Handler:            &handlerAdapter{handler: handler},
		Logger:             &slogTunLogger{},
		InterfaceFinder:    control.NewDefaultInterfaceFinder(),
		IncludeAllNetworks: p.cfg.FileDescriptor > 0,
	})
	if err != nil {
		tunIf.Close()
		return fmt.Errorf("provider: create stack: %w", err)
	}

	// Step 3: Start stack (TCP listener binds, tunLoop starts reading packets).
	if err := stack.Start(); err != nil {
		tunIf.Close()
		return fmt.Errorf("provider: start stack: %w", err)
	}

	// Step 4: Start TUN interface (sets system routes → traffic enters TUN).
	// Must be AFTER stack.Start() so incoming packets have a ready handler.
	if err := tunIf.Start(); err != nil {
		stack.Close()
		tunIf.Close()
		return fmt.Errorf("provider: start TUN: %w", err)
	}

	p.tunIf = tunIf
	p.stack = stack

	name, _ := tunIf.Name()
	p.name = name

	// macOS desktop: override system DNS → TUN_IP.Next() for DNS hijack.
	// TUN_IP itself is LOCAL (kernel delivers without entering TUN data plane).
	// TUN_IP.Next() routes through TUN gateway → gVisor intercepts port 53.
	// Must be AFTER TUN is up so dnsHandler can receive DNS queries.
	// Skip for NE/mobile FD path (NE manages DNS via NEDNSSettings).
	if p.cfg.FileDescriptor <= 0 {
		p.dnsOverride = newDNSOverride(ipv4Prefix.Addr().Next().String())
		if p.dnsOverride != nil {
			if err := p.dnsOverride.set(); err != nil {
				slog.Warn("provider: DNS override failed (user can set DNS manually)", "err", err)
			} else {
				go p.dnsOverride.monitor()
			}
		}
	}

	// Save DNS state marker for crash recovery. On Windows, sing-tun sets DNS
	// directly via luid.SetDNS — we track state externally so the next daemon
	// startup can detect and clean up after a crash. No-op on other platforms.
	saveDNSState(p.name, ipv4Prefix.Addr().Next().String())

	return nil
}

func (p *TUNProvider) Close() error {
	// Restore system DNS FIRST (while TUN still has routes).
	if p.dnsOverride != nil {
		p.dnsOverride.stop()
		p.dnsOverride = nil
	}
	if p.stack != nil {
		p.stack.Close()
		p.stack = nil
	}
	var err error
	if p.tunIf != nil {
		err = p.tunIf.Close()
		p.tunIf = nil
	}
	if !p.fdConsumed {
		closeRawFD(p.cfg.FileDescriptor)
		p.fdConsumed = true // idempotent: never close the (reusable) fd number twice
	}
	// Remove DNS state marker AFTER TUN is fully closed.
	// If we crash before this point, the marker survives and
	// CleanupStaleDNSOverride() will clean up on next startup.
	removeDNSState()
	return err
}

func (p *TUNProvider) Name() string {
	if p.name != "" {
		return p.name
	}
	return "tun"
}
