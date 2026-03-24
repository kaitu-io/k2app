package k2p

import (
	"context"
	"fmt"
	"log/slog"
	"net"
)

// NATType classifies a peer's NAT behavior.
type NATType int

const (
	NATUnknown NATType = iota
	NATEasy            // Endpoint-independent mapping (full/restricted/port-restricted cone)
	NATHard            // Endpoint-dependent mapping (symmetric)
	NATOpen            // No NAT (reflexive == local)
)

func (t NATType) String() string {
	switch t {
	case NATEasy:
		return "easy"
	case NATHard:
		return "hard"
	case NATOpen:
		return "open"
	default:
		return "unknown"
	}
}

// parseNATType converts a string to NATType.
func parseNATType(s string) NATType {
	switch s {
	case "easy":
		return NATEasy
	case "hard":
		return NATHard
	case "open":
		return NATOpen
	default:
		return NATUnknown
	}
}

// NATResult holds the outcome of NAT classification.
type NATResult struct {
	Type      NATType
	Reflexive *net.UDPAddr
	Local     *net.UDPAddr
}

// ClassifyNAT probes multiple STUN servers from the same socket to determine
// NAT type. Requires at least 2 servers. Returns NATEasy if all servers
// return the same reflexive address, NATHard if they differ.
func ClassifyNAT(ctx context.Context, servers []string) (NATResult, error) {
	if len(servers) < 2 {
		return NATResult{}, fmt.Errorf("k2p: need at least 2 STUN servers, got %d", len(servers))
	}

	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return NATResult{}, fmt.Errorf("k2p: listen UDP: %w", err)
	}
	defer conn.Close()

	var results []stunResult
	for _, server := range servers[:2] {
		r, err := stunProbe(ctx, server, conn)
		if err != nil {
			slog.Debug("k2p: STUN probe failed, trying next", "server", server, "err", err)
			continue
		}
		results = append(results, r)
	}

	if len(results) < 2 {
		if len(results) == 1 {
			return NATResult{
				Type:      NATEasy,
				Reflexive: results[0].reflexive,
				Local:     conn.LocalAddr().(*net.UDPAddr),
			}, nil
		}
		return NATResult{}, fmt.Errorf("k2p: all STUN servers unreachable")
	}

	local := conn.LocalAddr().(*net.UDPAddr)
	first := results[0].reflexive

	if first.IP.Equal(local.IP) && first.Port == local.Port {
		return NATResult{Type: NATOpen, Reflexive: first, Local: local}, nil
	}

	if first.IP.Equal(results[1].reflexive.IP) && first.Port == results[1].reflexive.Port {
		slog.Info("DIAG: nat-classify", "type", "easy", "reflexive", first.String(), "serversUsed", 2)
		return NATResult{Type: NATEasy, Reflexive: first, Local: local}, nil
	}

	slog.Info("DIAG: nat-classify", "type", "hard", "reflexive", first.String(), "serversUsed", 2)
	return NATResult{Type: NATHard, Reflexive: first, Local: local}, nil
}
