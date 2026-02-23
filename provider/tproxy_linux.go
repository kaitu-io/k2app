//go:build linux

package provider

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// TProxyProvider implements Provider using nftables TPROXY transparent proxy.
// No TUN device is created — nftables redirects traffic to a local tproxy
// listener that extracts the original destination and forwards connections.
//
// Designed for OpenWRT/Linux headless deployments where TUN is unavailable.
// Requires: nftables (nft), root privileges.
type TProxyProvider struct {
	cfg        ProviderConfig
	listener   net.Listener
	udpConn    *net.UDPConn
	listenPort int
	done       chan struct{}
}

var _ Provider = (*TProxyProvider)(nil)

// DefaultTProxyPort is the local port for nftables to redirect traffic to.
const DefaultTProxyPort = 12345

// NewTProxyProvider creates a tproxy-based provider.
func NewTProxyProvider(cfg ProviderConfig) *TProxyProvider {
	cfg.SetDefaults()
	return &TProxyProvider{cfg: cfg, done: make(chan struct{})}
}

func (p *TProxyProvider) Start(ctx context.Context, handler ConnectionHandler) error {
	p.listenPort = DefaultTProxyPort
	if p.cfg.TProxyPort > 0 {
		p.listenPort = p.cfg.TProxyPort
	}

	// Create TCP TPROXY listener with IP_TRANSPARENT socket option.
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_TRANSPARENT, 1)
			})
		},
	}

	addr := fmt.Sprintf("0.0.0.0:%d", p.listenPort)

	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("provider: tproxy listen TCP %s: %w", addr, err)
	}
	p.listener = ln

	// Create UDP TPROXY listener with IP_TRANSPARENT.
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		ln.Close()
		return fmt.Errorf("provider: resolve tproxy UDP addr: %w", err)
	}
	udpConn, err := listenUDPTransparent(udpAddr)
	if err != nil {
		ln.Close()
		return fmt.Errorf("provider: tproxy listen UDP %s: %w", addr, err)
	}
	p.udpConn = udpConn

	// Setup nftables TPROXY rules.
	if err := p.setupNftables(); err != nil {
		ln.Close()
		udpConn.Close()
		return fmt.Errorf("provider: setup nftables: %w", err)
	}

	// Accept TCP connections.
	go func() {
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
			// Original destination is preserved by TPROXY — the conn's
			// local address IS the original destination.
			dest := conn.LocalAddr().String()
			go handler.HandleTCP(conn, dest)
		}
	}()

	// Handle UDP packets.
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

	slog.Info("tproxy provider started", "port", p.listenPort)
	return nil
}

func (p *TProxyProvider) Close() error {
	select {
	case <-p.done:
		return nil
	default:
		close(p.done)
	}
	p.cleanupNftables()
	if p.listener != nil {
		p.listener.Close()
	}
	if p.udpConn != nil {
		p.udpConn.Close()
	}
	return nil
}

func (p *TProxyProvider) Name() string { return "tproxy" }

// setupNftables creates the nftables rules for TPROXY redirect.
// Uses a dedicated table "k2-tproxy" to avoid conflicts with existing rules.
func (p *TProxyProvider) setupNftables() error {
	portStr := strconv.Itoa(p.listenPort)

	// Build nftables ruleset as a single atomic script.
	script := strings.Join([]string{
		"add table inet k2-tproxy",
		// Prerouting chain for TPROXY.
		"add chain inet k2-tproxy prerouting { type filter hook prerouting priority mangle ; }",
		// Skip traffic from/to loopback.
		"add rule inet k2-tproxy prerouting iifname lo return",
		// TPROXY TCP.
		"add rule inet k2-tproxy prerouting meta l4proto tcp tproxy to :" + portStr + " meta mark set 1",
		// TPROXY UDP.
		"add rule inet k2-tproxy prerouting meta l4proto udp tproxy to :" + portStr + " meta mark set 1",
	}, "\n")

	if err := nftCmd("-f", "-", script); err != nil {
		return fmt.Errorf("nftables setup: %w", err)
	}

	// ip rule: route marked packets to local.
	_ = runCmd("ip", "rule", "add", "fwmark", "1", "table", "100")
	_ = runCmd("ip", "route", "add", "local", "0.0.0.0/0", "dev", "lo", "table", "100")

	return nil
}

// cleanupNftables removes the k2-tproxy nftables table and ip rules.
func (p *TProxyProvider) cleanupNftables() {
	_ = runCmd("nft", "delete", "table", "inet", "k2-tproxy")
	_ = runCmd("ip", "rule", "del", "fwmark", "1", "table", "100")
	_ = runCmd("ip", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", "100")
	slog.Info("tproxy: nftables rules cleaned up")
}

// nftCmd runs nft with stdin input for atomic ruleset loading.
func nftCmd(args ...string) error {
	if len(args) >= 2 && args[0] == "-f" && args[1] == "-" {
		cmd := exec.Command("nft", "-f", "-")
		cmd.Stdin = strings.NewReader(args[2])
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("nft -f -: %s: %w", out, err)
		}
		return nil
	}
	return runCmd("nft", args...)
}

// runCmd runs a command and returns an error if it fails.
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v: %s: %w", name, args, out, err)
	}
	return nil
}

// listenUDPTransparent creates a UDP listener with IP_TRANSPARENT set.
func listenUDPTransparent(addr *net.UDPAddr) (*net.UDPConn, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_TRANSPARENT, 1)
				syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_RECVORIGDSTADDR, 1)
			})
		},
	}
	pc, err := lc.ListenPacket(context.Background(), "udp", addr.String())
	if err != nil {
		return nil, err
	}
	return pc.(*net.UDPConn), nil
}

// readUDPOrigDst reads a UDP packet and extracts the original destination
// from the IP_RECVORIGDSTADDR ancillary message.
func readUDPOrigDst(conn *net.UDPConn, buf []byte) (n int, src, dst *net.UDPAddr, err error) {
	oob := make([]byte, 64)
	n, oobn, _, srcAddr, err := conn.ReadMsgUDP(buf, oob)
	if err != nil {
		return 0, nil, nil, err
	}

	msgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return 0, nil, nil, fmt.Errorf("parse control message: %w", err)
	}

	for _, msg := range msgs {
		if msg.Header.Level == syscall.SOL_IP && msg.Header.Type == syscall.IP_RECVORIGDSTADDR {
			// struct sockaddr_in: family(2) + port(2) + addr(4)
			if len(msg.Data) >= 8 {
				port := int(msg.Data[2])<<8 | int(msg.Data[3])
				ip := net.IPv4(msg.Data[4], msg.Data[5], msg.Data[6], msg.Data[7])
				return n, srcAddr, &net.UDPAddr{IP: ip, Port: port}, nil
			}
		}
	}

	return n, srcAddr, srcAddr, nil // fallback: use source as dest
}

// tproxyUDPConn wraps a single UDP packet as a net.PacketConn for the handler.
type tproxyUDPConn struct {
	conn    *net.UDPConn
	src     *net.UDPAddr
	dst     *net.UDPAddr
	initial []byte
	read    bool
	done    chan struct{}
}

func newTProxyUDPConn(conn *net.UDPConn, src, dst *net.UDPAddr, initial []byte) *tproxyUDPConn {
	data := make([]byte, len(initial))
	copy(data, initial)
	return &tproxyUDPConn{
		conn:    conn,
		src:     src,
		dst:     dst,
		initial: data,
		done:    make(chan struct{}),
	}
}

func (c *tproxyUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if !c.read {
		c.read = true
		n := copy(b, c.initial)
		return n, c.src, nil
	}
	// Block until closed — single-packet conn.
	<-c.done
	return 0, nil, net.ErrClosed
}

func (c *tproxyUDPConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	return c.conn.WriteTo(b, c.src)
}

func (c *tproxyUDPConn) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return nil
}

func (c *tproxyUDPConn) LocalAddr() net.Addr                        { return c.dst }
func (c *tproxyUDPConn) SetDeadline(t time.Time) error              { return nil }
func (c *tproxyUDPConn) SetReadDeadline(t time.Time) error          { return nil }
func (c *tproxyUDPConn) SetWriteDeadline(t time.Time) error         { return nil }

// isOpenWRT checks if running on OpenWRT by looking for fw4 or br-lan.
func isOpenWRT() bool {
	if _, err := exec.LookPath("fw4"); err == nil {
		return true
	}
	if _, err := net.InterfaceByName("br-lan"); err == nil {
		return true
	}
	return false
}
