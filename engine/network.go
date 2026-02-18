package engine

// NetworkChangeNotifier detects network interface changes and notifies the engine.
// Implementations are platform-specific: sing-tun DefaultInterfaceMonitor on desktop,
// NWPathMonitor (iOS) and ConnectivityManager (Android) call OnNetworkChanged directly.
type NetworkChangeNotifier interface {
	// Start begins monitoring. Calls callback when default interface changes.
	Start(callback func()) error
	// Close stops monitoring and releases resources.
	Close() error
}
