package appext

// SocketProtector marks sockets for VPN routing exclusion.
// On Android: wraps VpnService.protect(fd) to prevent routing loops.
// On iOS: no-op (PacketTunnelProvider self-excludes at kernel level).
//
// gomobile exports this as AppextSocketProtector (ObjC) / appext.SocketProtector (Java).
type SocketProtector interface {
	Protect(fd int32) bool
}
