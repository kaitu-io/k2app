//go:build linux

package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/kaitu-io/k2/provider"
)

type gatewayProvider struct {
	interceptor Interceptor
	cfg         InterceptConfig
	tcpLn       net.Listener
	udpConn     *net.UDPConn
	done        chan struct{}
}

var _ provider.Provider = (*gatewayProvider)(nil)

func newGatewayProvider(interceptor Interceptor, cfg InterceptConfig) *gatewayProvider {
	return &gatewayProvider{
		interceptor: interceptor,
		cfg:         cfg,
		done:        make(chan struct{}),
	}
}

func (p *gatewayProvider) Start(ctx context.Context, handler provider.ConnectionHandler) error {
	port := p.cfg.ListenPort
	if port == 0 {
		port = DefaultListenPort
	}

	tcpLn, err := newTPROXYTCPListener(ctx, port)
	if err != nil {
		return fmt.Errorf("gateway: tproxy TCP listen: %w", err)
	}
	p.tcpLn = tcpLn

	udpConn, err := newTPROXYUDPListener(ctx, port)
	if err != nil {
		tcpLn.Close()
		return fmt.Errorf("gateway: tproxy UDP listen: %w", err)
	}
	p.udpConn = udpConn

	if err := p.interceptor.Install(p.cfg); err != nil {
		tcpLn.Close()
		udpConn.Close()
		return fmt.Errorf("gateway: install intercept: %w", err)
	}

	go func() {
		for {
			conn, err := tcpLn.Accept()
			if err != nil {
				select {
				case <-p.done:
					return
				default:
					continue
				}
			}
			dest := conn.LocalAddr().String()
			go handler.HandleTCP(conn, dest)
		}
	}()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, src, dst, err := readUDPOrigDst(p.udpConn, buf)
			if err != nil {
				select {
				case <-p.done:
					return
				default:
					continue
				}
			}
			pconn := newTProxyUDPConn(p.udpConn, src, dst, buf[:n])
			go handler.HandleUDP(pconn, dst.String())
		}
	}()

	slog.Info("gateway provider started", "port", port, "interceptor", p.interceptor.Name())
	return nil
}

func (p *gatewayProvider) Close() error {
	select {
	case <-p.done:
		return nil
	default:
		close(p.done)
	}
	if p.interceptor != nil {
		p.interceptor.Remove()
	}
	if p.tcpLn != nil {
		p.tcpLn.Close()
	}
	if p.udpConn != nil {
		p.udpConn.Close()
	}
	return nil
}

func (p *gatewayProvider) Name() string { return "gateway" }
