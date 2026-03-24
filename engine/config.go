package engine

import (
	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/wire"
)

// Config holds all parameters needed to assemble and start a tunnel.
// It unifies the desktop (daemon/) and appext (mobile/sysext) paths.
//
// Client is the single source of truth for user configuration.
// Engine reads Server, Mode, Tun, Proxy, DNS, Rule fields directly from Client.
// Fields not used by engine (Listen, PIDFile, Log) are ignored.
//
// The remaining fields are platform runtime resources (not user-configurable).
type Config struct {
	// Client is the user configuration (YAML/JSON single source of truth).
	Client *config.ClientConfig

	// FileDescriptor controls TUN device source:
	//   -1  = self-create TUN (desktop)
	//   >=0 = platform-provided FD (appext: iOS NEPacketTunnelProvider / Android VpnService)
	FileDescriptor int

	// DirectDialer binds transport connections to the physical interface,
	// bypassing TUN. Nil = system default dialing.
	//
	// Desktop: directdial.Dialer (sing-tun interface binding)
	// Android: protectedDialer (VpnService.protect via syscall.RawConn)
	// iOS/macOS sysext/OpenWRT: nil
	DirectDialer wire.DirectDialer

	// NetworkMonitor detects network interface changes (desktop only).
	// When non-nil, engine registers a callback that calls OnNetworkChanged().
	// On mobile, this is nil — platform calls OnNetworkChanged() directly.
	NetworkMonitor NetworkChangeNotifier

	// InterfaceMonitor is passed to the TUN provider for self-exclusion.
	// On desktop, this should be the same sing-tun DefaultInterfaceMonitor instance
	// used by NetworkMonitor, so the TUN device is excluded from triggering
	// network change events. Opaque to engine — provider does type assertion.
	InterfaceMonitor any

	// NetworkExtension indicates the tunnel runs under Apple's NEPacketTunnelProvider.
	// When true, TUN MTU is capped at 4064 to avoid a known Apple kernel performance bug.
	// Set by appext on Darwin platforms. Always false on daemon path.
	NetworkExtension bool

	// HealthCallback receives 1Hz health snapshots from the HealthMonitor.
	// Nil = no callback (appext typically doesn't need this).
	HealthCallback func(HealthSnapshot)

	// Provider is an externally-created provider. If non-nil, engine uses it
	// instead of creating a provider from Client.Mode. Used by gateway/.
	Provider provider.Provider
}
