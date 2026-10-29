//go:build android

package core

// maxTunnelConns is the maximum number of concurrent tunnel connections.
// Android has moderate memory available.
const maxTunnelConns = 2048

// nonBlockingAdmit makes HandleTCP/HandleUDP drop connections immediately
// when at capacity instead of blocking.
const nonBlockingAdmit = true

// udpIdleTimeoutSec is the max seconds a UDP pipe can be idle before cleanup.
const udpIdleTimeoutSec = 60

// tcpBufSize is the buffer size for TCP io.CopyBuffer (per direction).
// 8KB on Android: reduces per-connection footprint under memory pressure.
const tcpBufSize = 8 * 1024
