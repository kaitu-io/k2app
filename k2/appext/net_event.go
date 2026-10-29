package appext

// NetEvent carries a platform network state change to the engine.
// Defined here (not in engine/) so gomobile bind ./appext/ exports it.
// gomobile generates: AppextNetEvent (iOS/ObjC), appext.NetEvent (Android/Java).
// Fields mirror engine.NetEvent exactly — keep in sync if engine.NetEvent changes.
type NetEvent struct {
	Signal        string // "available", "unavailable", "changed"
	InterfaceName string // e.g. "en0", "pdp_ip0"
	InterfaceIndex int
	IsWifi        bool
	IsCellular    bool
	HasIPv4       bool
	HasIPv6       bool
	Source        string // e.g. "sing-tun", "NWPathMonitor", "ConnectivityManager"
}

// NewNetEvent creates a NetEvent with zero values.
// gomobile requires a constructor for Java/ObjC instantiation.
func NewNetEvent() *NetEvent {
	return &NetEvent{}
}
