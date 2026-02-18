package engine

import (
	"net/netip"

	"github.com/kaitu-io/k2/directdial"
	"github.com/kaitu-io/k2rule"
)

// Config holds all parameters needed to assemble and start a tunnel.
// It unifies the desktop (daemon/tunnel.go) and mobile (mobile/mobile.go) paths.
type Config struct {
	// WireURL is the k2v5:// connection URL (required).
	WireURL string

	// FileDescriptor controls TUN device source:
	//   -1  = self-create TUN (desktop)
	//   >=0 = platform-provided FD (mobile: iOS NEPacketTunnelProvider / Android VpnService)
	FileDescriptor int

	// DataDir is the k2rule cache directory. Empty uses default (~/.cache/k2rule/).
	DataDir string

	// RuleMode selects routing mode: "global" or "smart". Default is "global".
	// Ignored when RuleConfig is non-nil.
	RuleMode string

	// DirectDialer binds transport connections to the physical interface,
	// bypassing TUN. Desktop only; nil on mobile.
	DirectDialer *directdial.Dialer

	// PreferIPv6 enables IPv6 transport when both server and client support it.
	// Desktop only.
	PreferIPv6 bool

	// Mode selects the provider type: "tun" (default) or "proxy".
	// Only applies when FileDescriptor < 0 (desktop).
	Mode string

	// ProxyListen is the proxy mode listen address (e.g. "127.0.0.1:1080").
	// Only used when Mode is "proxy".
	ProxyListen string

	// DNSExclude lists DNS server IPs to exclude from TUN routing (anti-loop).
	// Desktop only.
	DNSExclude []netip.Addr

	// RuleConfig provides full k2rule configuration. When non-nil, it takes
	// priority over RuleMode and DataDir. Typically used by the desktop daemon
	// which has richer rule configuration. Nil = auto-build from RuleMode + DataDir.
	RuleConfig *k2rule.Config

	// NetworkMonitor detects network interface changes (desktop only).
	// When non-nil, engine registers a callback that calls OnNetworkChanged().
	// On mobile, this is nil — platform calls OnNetworkChanged() directly.
	NetworkMonitor NetworkChangeNotifier

	// InterfaceMonitor is passed to the TUN provider for self-exclusion.
	// On desktop, this should be the same sing-tun DefaultInterfaceMonitor instance
	// used by NetworkMonitor, so the TUN device is excluded from triggering
	// network change events. Opaque to engine — provider does type assertion.
	InterfaceMonitor any
}
