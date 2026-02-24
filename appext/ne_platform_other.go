//go:build !darwin

package appext

import (
	"context"
	"net"
	"syscall"
)

// nePlatform stub for non-Darwin platforms. All methods are unreachable
// (isNetworkExtension is false) but must exist for type checking.
type nePlatform struct{}

func newNEPlatform() (*nePlatform, error) { return nil, nil }

func (p *nePlatform) DialContext(_ context.Context, _, _ string) (net.Conn, error) { return nil, nil }
func (p *nePlatform) ListenPacket(_, _ string) (net.PacketConn, error)            { return nil, nil }
func (p *nePlatform) ControlFunc() func(string, string, syscall.RawConn) error    { return nil }
func (p *nePlatform) Start(_ func()) error                                        { return nil }
func (p *nePlatform) Close() error                                                { return nil }
func (p *nePlatform) InterfaceMonitor() any                                       { return nil }
func (p *nePlatform) interfaceName() string                                       { return "" }
