package k2p2p

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/pion/stun/v3"
)

// stunResult holds the outcome of a single STUN probe.
type stunResult struct {
	reflexive *net.UDPAddr
	local     *net.UDPAddr
	server    string
}

// stunProbe sends a STUN Binding Request to the given server and returns
// the reflexive (mapped) address. If conn is nil, a new UDP socket is created.
func stunProbe(ctx context.Context, server string, conn *net.UDPConn) (stunResult, error) {
	var ownConn bool
	if conn == nil {
		c, err := net.ListenUDP("udp4", nil)
		if err != nil {
			return stunResult{}, fmt.Errorf("k2p2p: listen UDP: %w", err)
		}
		conn = c
		ownConn = true
		defer conn.Close()
	}
	_ = ownConn

	serverAddr, err := net.ResolveUDPAddr("udp4", server)
	if err != nil {
		return stunResult{}, fmt.Errorf("k2p2p: resolve STUN server %q: %w", server, err)
	}

	msg, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		return stunResult{}, fmt.Errorf("k2p2p: build STUN request: %w", err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
		defer conn.SetDeadline(time.Time{})
	}

	if _, err := conn.WriteToUDP(msg.Raw, serverAddr); err != nil {
		return stunResult{}, fmt.Errorf("k2p2p: send STUN request: %w", err)
	}

	buf := make([]byte, 1500)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return stunResult{}, fmt.Errorf("k2p2p: read STUN response: %w", err)
	}

	resp := new(stun.Message)
	resp.Raw = buf[:n]
	if err := resp.Decode(); err != nil {
		return stunResult{}, fmt.Errorf("k2p2p: decode STUN response: %w", err)
	}

	var xorAddr stun.XORMappedAddress
	if err := xorAddr.GetFrom(resp); err != nil {
		return stunResult{}, fmt.Errorf("k2p2p: extract XOR-MAPPED-ADDRESS: %w", err)
	}

	return stunResult{
		reflexive: &net.UDPAddr{IP: xorAddr.IP, Port: xorAddr.Port},
		local:     conn.LocalAddr().(*net.UDPAddr),
		server:    server,
	}, nil
}
