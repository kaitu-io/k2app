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

	// DNS-port listeners (REDIRECT target for K2R_DNS / K2R_DNS_OUT chains).
	// Plain sockets — no TPROXY/IP_TRANSPARENT — since REDIRECT delivers
	// packets to the local IP normally. Empty when DNSRedirect is off.
	dnsTCPLn4 net.Listener
	dnsTCPLn6 net.Listener
	dnsUDP4   *net.UDPConn
	dnsUDP6   *net.UDPConn

	// replies hands the UDP read loops a transparent socket per original
	// destination so TPROXY'd replies carry that destination as their
	// source (see tproxyUDPConn.WriteTo).
	replies *replySocketCache

	done chan struct{}
}

var _ provider.Provider = (*gatewayProvider)(nil)

func newGatewayProvider(interceptor Interceptor, cfg InterceptConfig) *gatewayProvider {
	return &gatewayProvider{
		interceptor: interceptor,
		cfg:         cfg,
		replies:     newReplySocketCache(transparentBind),
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

	// Optional DNS listener on DNSPort — REDIRECT target for OUTPUT-chain
	// (and PREROUTING-chain) DNS interception. Bound BEFORE interceptor.Install
	// to avoid a race where iptables rules send packets at a closed port.
	if p.cfg.DNSRedirect && p.cfg.DNSPort > 0 {
		dnsTCPLn4, dnsTCPLn6, err := newDNSTCPListeners(ctx, p.cfg.DNSPort)
		if err != nil {
			tcpLn4.Close()
			tcpLn6.Close()
			udpConn4.Close()
			udpConn6.Close()
			return fmt.Errorf("gateway: dns tproxy listen: %w", err)
		}
		p.dnsTCPLn4 = dnsTCPLn4
		p.dnsTCPLn6 = dnsTCPLn6

		dnsUDP4, dnsUDP6, err := newDNSUDPListeners(ctx, p.cfg.DNSPort)
		if err != nil {
			tcpLn4.Close()
			tcpLn6.Close()
			udpConn4.Close()
			udpConn6.Close()
			dnsTCPLn4.Close()
			dnsTCPLn6.Close()
			return fmt.Errorf("gateway: dns udp listen: %w", err)
		}
		p.dnsUDP4 = dnsUDP4
		p.dnsUDP6 = dnsUDP6
	}

	if err := p.interceptor.Install(p.cfg); err != nil {
		tcpLn4.Close()
		tcpLn6.Close()
		udpConn4.Close()
		udpConn6.Close()
		if p.dnsTCPLn4 != nil {
			p.dnsTCPLn4.Close()
			p.dnsTCPLn6.Close()
			p.dnsUDP4.Close()
			p.dnsUDP6.Close()
		}
		return fmt.Errorf("gateway: install intercept: %w", err)
	}

	// TCP accept loops (IPv4 + IPv6).
	p.startTCPAccept(tcpLn4, handler)
	p.startTCPAccept(tcpLn6, handler)

	// UDP read loops (IPv4 + IPv6).
	p.startUDPRead(udpConn4, handler)
	p.startUDPRead(udpConn6, handler)
	safego.Go(func() { p.replies.runSweeper(p.done) })

	// DNS-port loops: TCP accept + UDP read. Each conn/packet gets a
	// synthetic dest ":53" so dnsHandler short-circuits via port-53 match.
	if p.dnsTCPLn4 != nil {
		p.startDNSTCPAccept(p.dnsTCPLn4, handler)
		p.startDNSTCPAccept(p.dnsTCPLn6, handler)
		p.startDNSUDPRead(p.dnsUDP4, handler)
		p.startDNSUDPRead(p.dnsUDP6, handler)
	}

	slog.Info("gateway provider started", "port", port, "dnsPort", p.cfg.DNSPort,
		"dnsRedirect", p.cfg.DNSRedirect, "interceptor", p.interceptor.Name())
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
			pconn := newTProxyUDPConn(udpConn, p.replies, src, dst, buf[:n])
			go handler.HandleUDP(pconn, dst.String())
		}
	})
}

// startDNSTCPAccept loops over Accept on the DNS TCP listener and dispatches
// each conn to the handler with a synthetic ":53" dest, so dnsHandler.HandleTCP
// short-circuits into serveDNSStream regardless of where the connection
// actually landed (e.g. the REDIRECT-rewritten 127.0.0.1:DNSPort).
func (p *gatewayProvider) startDNSTCPAccept(ln net.Listener, handler provider.ConnectionHandler) {
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
			go handler.HandleTCP(conn, ":53")
		}
	})
}

// startDNSUDPRead loops over packets on the DNS UDP listener and dispatches
// each one to a fresh single-shot dnsUDPConn so dnsHandler.serveDNS handles
// it in its own goroutine — concurrent DNS query processing without per-flow
// state in the listener.
func (p *gatewayProvider) startDNSUDPRead(udpConn *net.UDPConn, handler provider.ConnectionHandler) {
	safego.Go(func() {
		buf := make([]byte, 4096)
		for {
			n, src, err := udpConn.ReadFrom(buf)
			if err != nil {
				select {
				case <-p.done:
					return
				default:
					continue
				}
			}
			pconn := newDNSUDPConn(udpConn, src, buf[:n])
			go handler.HandleUDP(pconn, ":53")
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
	if p.replies != nil {
		p.replies.close()
	}
	if p.dnsTCPLn4 != nil {
		p.dnsTCPLn4.Close()
	}
	if p.dnsTCPLn6 != nil {
		p.dnsTCPLn6.Close()
	}
	if p.dnsUDP4 != nil {
		p.dnsUDP4.Close()
	}
	if p.dnsUDP6 != nil {
		p.dnsUDP6.Close()
	}
	return nil
}

func (p *gatewayProvider) Name() string { return "gateway" }
