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

	// ProcessSearcher finds the process that owns a network connection.
	// Nil = no process matching (NopProcessSearcher used internally).
	ProcessSearcher provider.ProcessSearcher

	// PackageResolver resolves Android UID to package name.
	// Passed to LinuxProcessSearcher on Android. Nil on other platforms.
	PackageResolver provider.PackageResolver

	// WireMode is the identity sent to the server via metadata stream.
	// "gateway" for k2r, "" for all other binaries.
	// Hardcoded by the platform shell — not user-configurable.
	WireMode string

	// OutboundProvider supplies replacement k2v5:// URLs when a running
	// outbound's wire transport fails persistently (RaceTransport fails
	// repeatedly for the same URL). Nil = engine falls back to today's
	// behavior (reports fatal after wire-level re-race exhaustion; no
	// server-level rotation).
	//
	// Desktop daemon implements this by delegating to the k2subs://
	// Subscription.Pick(exclude) API. Mobile/appext and gateway leave it
	// nil — they don't have a Subscription concept at the platform shell.
	//
	// The engine calls NextURL from its outboundReplaceLoop goroutine,
	// OUTSIDE engine.mu (read-copy-act pattern). The provider must not
	// acquire any lock the caller also holds.
	OutboundProvider OutboundProvider

	// DNSForwardUpstream, when non-empty ("host:port"), disables the local
	// DNS middleware for provider port-53 flows and forwards raw queries to
	// this upstream through the rule engine (multi-slot gateway mode).
	DNSForwardUpstream string
}

// OutboundProvider supplies replacement k2v5:// URLs when an existing
// outbound's wire transport has persistently failed.
//
// Semantics:
//   - The engine calls NextURL with the URL that just failed (currentURL)
//     and the classified error (reason).
//   - The provider is expected to return a DIFFERENT URL each time for the
//     same logical subscription session — i.e. it maintains its own
//     "already-tried-and-failed" exclude set and filters accordingly.
//   - When the provider exhausts its candidates, it returns (""/wrapped-err).
//     The engine then fires EventHandler.OnOutboundFatal and calls
//     e.fail to terminate the tunnel.
//
// Concurrency: the engine invokes NextURL from a single
// outboundReplaceLoop goroutine and calls are therefore serialized across
// all outbounds owned by a given Engine. A provider that is shared between
// multiple Engine instances (unusual — daemon owns at most one) must
// synchronize internally.
type OutboundProvider interface {
	NextURL(currentURL string, reason error) (string, error)
}
