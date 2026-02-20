//go:build (darwin && !ios) || (linux && !android) || windows

package provider

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/sagernet/sing-tun"
)

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
		Name:         "",
		MTU:          uint32(p.cfg.MTU),
		Inet4Address: []netip.Prefix{ipv4Prefix},
		Inet6Address: []netip.Prefix{ipv6Prefix},
		AutoRoute:    true,
		DNSServers:   p.cfg.DNSServers,
	}

	// Exclude DNS servers from routing to prevent loops.
	for _, dns := range p.cfg.DNSServers {
		if dns.Is4() {
			tunOpts.Inet4RouteExcludeAddress = append(tunOpts.Inet4RouteExcludeAddress,
				netip.PrefixFrom(dns, 32))
		} else {
			tunOpts.Inet6RouteExcludeAddress = append(tunOpts.Inet6RouteExcludeAddress,
				netip.PrefixFrom(dns, 128))
		}
	}

	// Pass interface monitor for TUN self-exclusion (prevents self-triggering).
	if m, ok := p.cfg.InterfaceMonitor.(tun.DefaultInterfaceMonitor); ok {
		tunOpts.InterfaceMonitor = m
	}

	if p.cfg.FileDescriptor > 0 {
		tunOpts.FileDescriptor = p.cfg.FileDescriptor
	}

	tunIf, err := tun.New(tunOpts)
	if err != nil {
		return fmt.Errorf("provider: create TUN: %w", err)
	}

	stack, err := tun.NewSystem(tun.StackOptions{
		Context:    ctx,
		Tun:        tunIf,
		TunOptions: tunOpts,
		Handler:    &handlerAdapter{handler: handler},
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
