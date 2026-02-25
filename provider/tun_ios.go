//go:build ios

package provider

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/sagernet/sing-tun"
)

// TUNProvider wraps a TUN file descriptor from NEPacketTunnelProvider (iOS).
type TUNProvider struct {
	cfg   ProviderConfig
	tunIf tun.Tun
	stack tun.Stack
	name  string
}

var _ Provider = (*TUNProvider)(nil)

// NewTUNProvider creates a TUN-based provider using an existing FD from iOS.
func NewTUNProvider(cfg ProviderConfig) *TUNProvider {
	cfg.SetDefaults()
	return &TUNProvider{cfg: cfg}
}

func (p *TUNProvider) Start(ctx context.Context, handler ConnectionHandler) error {
	if p.cfg.FileDescriptor <= 0 {
		return fmt.Errorf("provider: iOS requires FileDescriptor > 0")
	}

	ipv4Prefix, err := netip.ParsePrefix(p.cfg.IPv4Address)
	if err != nil {
		return fmt.Errorf("provider: parse IPv4: %w", err)
	}
	ipv6Prefix, err := netip.ParsePrefix(p.cfg.IPv6Address)
	if err != nil {
		return fmt.Errorf("provider: parse IPv6: %w", err)
	}

	tunOpts := tun.Options{
		MTU:                     uint32(p.cfg.MTU),
		FileDescriptor:          p.cfg.FileDescriptor,
		Inet4Address:            []netip.Prefix{ipv4Prefix},
		Inet6Address:            []netip.Prefix{ipv6Prefix},
		EXP_MultiPendingPackets: p.cfg.MTU < 32768,
	}

	tunIf, err := tun.New(tunOpts)
	if err != nil {
		return fmt.Errorf("provider: create TUN from FD: %w", err)
	}

	// iOS NE: gVisor stack via IncludeAllNetworks (mirrors sing-box pattern).
	// System stack's net.Listen() may not work reliably in NE sandbox.
	stack, err := tun.NewStack("", tun.StackOptions{
		Context:            ctx,
		Tun:                tunIf,
		TunOptions:         tunOpts,
		UDPTimeout:         5 * time.Minute,
		Handler:            &handlerAdapter{handler: handler},
		Logger:             &slogTunLogger{},
		IncludeAllNetworks: true,
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
	if name, err := tunIf.Name(); err == nil {
		p.name = name
	} else {
		p.name = "utun-ios"
	}
	return nil
}

func (p *TUNProvider) Close() error {
	if p.stack != nil {
		p.stack.Close()
		p.stack = nil
	}
	if p.tunIf != nil {
		err := p.tunIf.Close()
		p.tunIf = nil
		return err
	}
	return nil
}

func (p *TUNProvider) Name() string { return p.name }
