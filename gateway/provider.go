//go:build linux

package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/safego"
)

type gatewayProvider struct {
	interceptor Interceptor
	cfg         InterceptConfig
	tcpLn4      net.Listener
	tcpLn6      net.Listener
	udpConn4    *net.UDPConn
	udpConn6    *net.UDPConn
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

	tcpLn4, tcpLn6, err := newTPROXYTCPListeners(ctx, port)
	if err != nil {
		return fmt.Errorf("gateway: tproxy TCP listen: %w", err)
	}
	p.tcpLn4 = tcpLn4
	p.tcpLn6 = tcpLn6

	udpConn4, udpConn6, err := newTPROXYUDPListeners(ctx, port)
	if err != nil {
		tcpLn4.Close()
		tcpLn6.Close()
		return fmt.Errorf("gateway: tproxy UDP listen: %w", err)
	}
	p.udpConn4 = udpConn4
	p.udpConn6 = udpConn6

	if err := p.interceptor.Install(p.cfg); err != nil {
		tcpLn4.Close()
		tcpLn6.Close()
		udpConn4.Close()
		udpConn6.Close()
		return fmt.Errorf("gateway: install intercept: %w", err)
	}

	// TCP accept loops (IPv4 + IPv6).
	p.startTCPAccept(tcpLn4, handler)
	p.startTCPAccept(tcpLn6, handler)

	// UDP read loops (IPv4 + IPv6).
	p.startUDPRead(udpConn4, handler)
	p.startUDPRead(udpConn6, handler)

	slog.Info("gateway provider started", "port", port, "interceptor", p.interceptor.Name())
	return nil
}

func (p *gatewayProvider) startTCPAccept(ln net.Listener, handler provider.ConnectionHandler) {
	safego.Go(func() {
		for {
			conn, err := ln.Accept()
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
	})
}

func (p *gatewayProvider) startUDPRead(udpConn *net.UDPConn, handler provider.ConnectionHandler) {
	safego.Go(func() {
		buf := make([]byte, 4096)
		for {
			n, src, dst, err := readUDPOrigDst(udpConn, buf)
			if err != nil {
				select {
				case <-p.done:
					return
				default:
					continue
				}
			}
			pconn := newTProxyUDPConn(udpConn, src, dst, buf[:n])
			go handler.HandleUDP(pconn, dst.String())
		}
	})
}

func (p *gatewayProvider) Close() error {
	select {
	case <-p.done:
		return nil
	default:
		close(p.done)
	}
	// Note: interceptor lifecycle is owned by Gateway, not provider.
	// Provider only closes its own resources (TCP listeners + UDP conns).
	if p.tcpLn4 != nil {
		p.tcpLn4.Close()
	}
	if p.tcpLn6 != nil {
		p.tcpLn6.Close()
	}
	if p.udpConn4 != nil {
		p.udpConn4.Close()
	}
	if p.udpConn6 != nil {
		p.udpConn6.Close()
	}
	return nil
}

func (p *gatewayProvider) Name() string { return "gateway" }
