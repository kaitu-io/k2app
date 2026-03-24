package k2p2p

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/kaitu-io/k2/safego"
)

const (
	punchMagic    = "k2p2p-punch"
	punchInterval = 100 * time.Millisecond
	punchTimeout  = 5 * time.Second
)

// punchStandard sends UDP punch packets to peerAddr from conn at regular
// intervals while simultaneously listening for incoming punch packets.
// Returns the confirmed peer address on success.
func punchStandard(ctx context.Context, conn *net.UDPConn, peerAddr *net.UDPAddr) (*net.UDPAddr, error) {
	ctx, cancel := context.WithTimeout(ctx, punchTimeout)
	defer cancel()

	// Send punch packets in background.
	safego.Go(func() {
		ticker := time.NewTicker(punchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				conn.WriteToUDP([]byte(punchMagic), peerAddr)
			}
		}
	})

	buf := make([]byte, 64)
	for {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("k2p2p: punch timeout: %w", ctx.Err())
		}
		conn.SetReadDeadline(time.Now().Add(punchInterval * 2))
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return nil, fmt.Errorf("k2p2p: punch read: %w", err)
		}
		if n >= len(punchMagic) && string(buf[:len(punchMagic)]) == punchMagic {
			slog.Info("DIAG: punch-ok", "from", from.String(), "method", "standard")
			return from, nil
		}
	}
}

// BirthdayResult holds the outcome of a birthday attack punch.
type BirthdayResult struct {
	Conn     *net.UDPConn
	PeerAddr *net.UDPAddr
}

// punchBirthday opens multiple UDP sockets on localIP and sends punch packets
// to peerIP across a port range. Returns the first socket that receives a
// valid punch reply. If localStart == 0 && localEnd == 0, uses OS-assigned ports.
func punchBirthday(ctx context.Context, localIP, peerIP net.IP,
	localStart, localEnd, peerStart, peerEnd int) (*BirthdayResult, error) {

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if localStart == 0 && localEnd == 0 {
		localStart = 0
		localEnd = 255
	}
	numLocal := localEnd - localStart + 1

	conns := make([]*net.UDPConn, 0, numLocal)
	for i := 0; i < numLocal; i++ {
		port := 0
		if localStart > 0 {
			port = localStart + i
		}
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: localIP, Port: port})
		if err != nil {
			continue
		}
		conns = append(conns, c)
	}
	if len(conns) == 0 {
		return nil, fmt.Errorf("k2p2p: birthday: no local sockets opened")
	}

	type winResult struct {
		conn *net.UDPConn
		peer *net.UDPAddr
	}
	winCh := make(chan winResult, 1)

	// Send punch packets from all sockets to all peer ports.
	safego.Go(func() {
		for round := 0; round < 3; round++ {
			for _, c := range conns {
				for port := peerStart; port <= peerEnd; port++ {
					if ctx.Err() != nil {
						return
					}
					target := &net.UDPAddr{IP: peerIP, Port: port}
					c.WriteToUDP([]byte(punchMagic), target)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
	})

	// Listen on all sockets concurrently.
	for _, c := range conns {
		c := c
		safego.Go(func() {
			buf := make([]byte, 64)
			for {
				if ctx.Err() != nil {
					return
				}
				c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				n, from, err := c.ReadFromUDP(buf)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					continue
				}
				if n >= len(punchMagic) && string(buf[:len(punchMagic)]) == punchMagic {
					select {
					case winCh <- winResult{conn: c, peer: from}:
						cancel()
					default:
					}
					return
				}
			}
		})
	}

	select {
	case w := <-winCh:
		slog.Info("DIAG: punch-ok", "from", w.peer.String(), "method", "birthday",
			"socketsOpened", len(conns))
		for _, c := range conns {
			if c != w.conn {
				c.Close()
			}
		}
		return &BirthdayResult{Conn: w.conn, PeerAddr: w.peer}, nil
	case <-ctx.Done():
		for _, c := range conns {
			c.Close()
		}
		return nil, fmt.Errorf("k2p2p: birthday punch timeout")
	}
}
