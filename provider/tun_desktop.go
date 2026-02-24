//go:build (darwin && !ios) || (linux && !android) || windows

package provider

import (
	"context"
	"fmt"
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
		return "sing-tun"
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
type TUNProvider struct {
	cfg   ProviderConfig
	tunIf tun.Tun
	stack tun.Stack
	name  string
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
		EXP_MultiPendingPackets: runtime.GOOS == "darwin" && p.cfg.MTU < 32768,
		// DNSServers intentionally NOT set — sing-tun auto-derives TUN_IP.Next()
		// which tells the OS to send DNS through TUN for Handler-level interception.
		// On Windows: luid.SetDNS(TUN_IP.Next())
		// On Linux:   resolvectl dns <tun> TUN_IP.Next()
		// On macOS:   unused (AutoRoute captures all traffic)
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

	tunIf, err := tun.New(tunOpts)
	if err != nil {
		return fmt.Errorf("provider: create TUN: %w", err)
	}

	if err := tunIf.Start(); err != nil {
		tunIf.Close()
		return fmt.Errorf("provider: start TUN: %w", err)
	}

	// Use sing-tun's NewStack dispatcher (mirrors sing-box pattern):
	//   IncludeAllNetworks=true (NE FD path) → gVisor (full userspace TCP/IP)
	//   IncludeAllNetworks=false (daemon)     → Mixed (gVisor UDP + System TCP) or System
	stack, err := tun.NewStack("", tun.StackOptions{
		Context:            ctx,
		Tun:                tunIf,
		TunOptions:         tunOpts,
		UDPTimeout:         5 * time.Minute,
		Handler:            &handlerAdapter{handler: handler},
		InterfaceFinder:    control.NewDefaultInterfaceFinder(),
		IncludeAllNetworks: p.cfg.FileDescriptor > 0,
	})
	if err != nil {
		tunIf.Close()
		return fmt.Errorf("provider: create stack: %w", err)
	}

	if err := stack.Start(); err != nil {
		tunIf.Close()
		return fmt.Errorf("provider: start stack: %w", err)
	}

	p.tunIf = tunIf
	p.stack = stack

	name, _ := tunIf.Name()
	p.name = name
	return nil
}

func (p *TUNProvider) Close() error {
	if p.stack != nil {
		p.stack.Close()
	}
	if p.tunIf != nil {
		return p.tunIf.Close()
	}
	return nil
}

func (p *TUNProvider) Name() string {
	if p.name != "" {
		return p.name
	}
	return "tun"
}
