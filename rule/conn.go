package rule

import "net/netip"

// ConnMeta carries connection metadata for rule matching.
// Populated by the tunnel from connection source addr + process lookup.
// Zero value = no metadata available (all criteria pass).
type ConnMeta struct {
	ProcessName      string // OS process name (empty = unknown)
	PackageName      string // Android package name (empty = not Android or unknown)
	InstallerPackage string // Android only: package that installed PackageName (empty otherwise)
	Network          string // "tcp" or "udp" (empty = unknown)
	Port             uint16 // destination port (0 = unknown)

	// Protocol is the sniffed application protocol ("stun", "dtls", "quic",
	// "bittorrent"). Empty = not sniffed or unknown; routes with Protocols
	// criteria then never match.
	Protocol string

	// SrcIP is the connection's LAN source address (multi-slot gateway
	// routing pins each slot's bridge subnet to one outbound).
	// Zero value = unknown; routes with SrcCIDRs then never match.
	SrcIP netip.Addr
}
