//go:build !linux

package provider

import (
	"context"
	"fmt"
)

// TProxyProvider is only supported on Linux.
type TProxyProvider struct {
	cfg ProviderConfig
}

// NewTProxyProvider creates a tproxy-based provider.
// Returns a provider that always fails on non-Linux platforms.
func NewTProxyProvider(cfg ProviderConfig) *TProxyProvider {
	return &TProxyProvider{cfg: cfg}
}

func (p *TProxyProvider) Start(ctx context.Context, handler ConnectionHandler) error {
	return fmt.Errorf("provider: tproxy mode is only supported on Linux")
}

func (p *TProxyProvider) Close() error { return nil }
func (p *TProxyProvider) Name() string { return "tproxy" }
