package rule

import "net/netip"

// ConnMeta carries connection metadata for rule matching.
// Populated by the tunnel from connection source addr + process lookup.
// Zero value = no metadata available (all criteria pass).
type ConnMeta struct {
	ProcessName      string // OS process name (empty = unknown)

	// ProcessPath is the full path of the owning process's executable, in
	// the platform's canonical form (see NormalizeProcessPath) — desktop
	// only, empty when the searcher cannot report it. Matched by
	// RouteEntry.ProcessPathPrefixes: "is this process inside that app's
	// bundle / install directory".
	ProcessPath string
	PackageName      string // Android package name (empty = not Android or unknown)
	InstallerPackage string // Android only: package that installed PackageName (empty otherwise)

	// PackageNames lists ALL Android packages sharing the connection's owning
	// UID (PackageManager.getPackagesForUid). Android attribution is
	// UID-granular — Play Store and GMS share one UID via sharedUserId — so a
	// package rule matches when ANY shared package hits. When set, PackageName
	// holds the first entry (display/logging); matchers must consult this
	// list, not just PackageName. Nil on desktop and on the legacy /proc path.
	PackageNames []string
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
