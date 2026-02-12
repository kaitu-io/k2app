//go:build ios

package provider

import (
	"context"
	"fmt"

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

	tunOpts := tun.Options{
		MTU:            uint32(p.cfg.MTU),
		FileDescriptor: p.cfg.FileDescriptor,
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
	p.name = "utun-ios"
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
