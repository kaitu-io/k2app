//go:build android

package provider

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/sagernet/sing-tun"
)

// Android VPN builder addresses — must match K2VpnService.Builder config.
var (
	androidIPv4Prefix = netip.MustParsePrefix("10.0.0.2/24")
	androidIPv6Prefix = netip.MustParsePrefix("fd00::2/64")
)

// TUNProvider wraps a TUN file descriptor from Android VpnService.Builder.
type TUNProvider struct {
	cfg   ProviderConfig
	tunIf tun.Tun
	stack tun.Stack
	name  string
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

	tunOpts := tun.Options{
		MTU:            uint32(p.cfg.MTU),
		FileDescriptor: p.cfg.FileDescriptor,
		Inet4Address:   []netip.Prefix{androidIPv4Prefix},
		Inet6Address:   []netip.Prefix{androidIPv6Prefix},
	}

	tunIf, err := tun.New(tunOpts)
	if err != nil {
		return fmt.Errorf("provider: create TUN from FD: %w", err)
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
	p.name = "tun-android"
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

func (p *TUNProvider) Name() string { return p.name }
