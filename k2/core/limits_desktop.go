//go:build (darwin && !ios) || (linux && !android) || windows

package core

// maxTunnelConns is the maximum number of concurrent tunnel connections
// (HandleTCP + HandleUDP goroutines). Desktop has ample memory.
const maxTunnelConns = 4096

// nonBlockingAdmit=false keeps blocking semaphore on desktop (ample memory).
const nonBlockingAdmit = false

// udpIdleTimeoutSec is the max seconds a UDP pipe can be idle before cleanup.
// 60s on all platforms to prevent session leaks on desktop.
const udpIdleTimeoutSec = 60

// tcpBufSize is the buffer size for TCP io.CopyBuffer (per direction).
const tcpBufSize = 32 * 1024
