package mobile

// SocketProtector marks sockets for VPN routing exclusion.
// On Android: wraps VpnService.protect(fd) to prevent routing loops.
// On iOS: no-op (PacketTunnelProvider self-excludes at kernel level).
//
// gomobile exports this as MobileSocketProtector (ObjC) / mobile.SocketProtector (Java).
type SocketProtector interface {
	Protect(fd int32) bool
}
