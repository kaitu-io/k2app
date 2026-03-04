//go:build (darwin && !ios) || (linux && !android) || windows

package core

// maxTunnelConns is the maximum number of concurrent tunnel connections
// (HandleTCP + HandleUDP goroutines). Desktop has ample memory.
const maxTunnelConns = 4096

// nonBlockingAdmit=false keeps blocking semaphore on desktop (ample memory).
const nonBlockingAdmit = false

// udpIdleTimeoutSec disables UDP idle timeout on desktop (0 = no timeout).
const udpIdleTimeoutSec = 0

// tcpBufSize is the buffer size for TCP io.CopyBuffer (per direction).
const tcpBufSize = 32 * 1024
