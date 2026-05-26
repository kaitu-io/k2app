package rule

// ConnMeta carries connection metadata for rule matching.
// Populated by the tunnel from connection source addr + process lookup.
// Zero value = no metadata available (all criteria pass).
type ConnMeta struct {
	ProcessName      string // OS process name (empty = unknown)
	PackageName      string // Android package name (empty = not Android or unknown)
	InstallerPackage string // Android only: package that installed PackageName (empty otherwise)
	Network          string // "tcp" or "udp" (empty = unknown)
	Port             uint16 // destination port (0 = unknown)
}
