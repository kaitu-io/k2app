//go:build android

package provider

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/sagernet/sing-tun"
)

// TUNProvider wraps a TUN file descriptor from Android VpnService.Builder.
//
// Lifecycle (Start/Close) is serialized by the engine — no internal locking.
type TUNProvider struct {
	cfg   ProviderConfig
	tunIf tun.Tun
	stack tun.Stack
	name  string
	// fdConsumed is set once tun.New takes ownership of cfg.FileDescriptor
	// (tunIf.Close then releases it). Until then Close must release the raw
	// fd itself: the fd is the engine's dup of the VpnService tun — leaking
	// it keeps the Android VPN alive routing into a dead tunnel, blackholing
	// the whole device (bypassable=false default route).
	fdConsumed bool
}

var _ Provider = (*TUNProvider)(nil)

// NewTUNProvider creates a TUN-based provider using an existing FD from Android.
func NewTUNProvider(cfg ProviderConfig) *TUNProvider {
	cfg.SetDefaults()
	return &TUNProvider{cfg: cfg}
}

func (p *TUNProvider) Start(ctx context.Context, handler ConnectionHandler) error {
	if p.cfg.FileDescriptor <= 0 {
		return fmt.Errorf("provider: Android requires FileDescriptor > 0")
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
		MTU:            uint32(p.cfg.MTU),
		FileDescriptor: p.cfg.FileDescriptor,
		Inet4Address:   []netip.Prefix{ipv4Prefix},
		Inet6Address:   []netip.Prefix{ipv6Prefix},
	}

	tunIf, err := tun.New(tunOpts)
	if err != nil {
		return fmt.Errorf("provider: create TUN from FD: %w", err)
	}
	p.fdConsumed = true // tunIf owns the fd from here (tunIf.Close releases it)

	// Android VpnService: gVisor stack via IncludeAllNetworks (mirrors sing-box pattern).
	// System stack's net.Listen() may not work reliably in VPN service sandbox.
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
		p.name = "tun-android"
	}
	return nil
}

func (p *TUNProvider) Close() error {
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
	return err
}

func (p *TUNProvider) Name() string { return p.name }
