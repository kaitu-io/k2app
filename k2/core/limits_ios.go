//go:build ios

package core

// maxTunnelConns is the maximum number of concurrent tunnel connections.
// iOS NE jetsam ~50MB, GOMEMLIMIT=35MB, base overhead ~12MB.
// 512 full × 40KB/conn = 20MB + 12MB base = 32MB < GOMEMLIMIT.
// GOMEMLIMIT is the real safety net; generous limit avoids dropping connections.
const maxTunnelConns = 512

// nonBlockingAdmit makes HandleTCP/HandleUDP drop connections immediately
// when at capacity instead of blocking. Prevents goroutine pile-up from
// gVisor spawning waiters that each consume 2-8KB stack.
const nonBlockingAdmit = true

// udpIdleTimeoutSec is the max seconds a UDP pipe can be idle before cleanup.
// 60s unified across all platforms. Previous 15s was too short for DNS-over-QUIC.
// iOS memory impact to be tuned separately if needed.
const udpIdleTimeoutSec = 60

// tcpBufSize is the buffer size for TCP io.CopyBuffer (per direction).
// 8KB on iOS: 24KB/conn total (8KB stacks + 16KB bufs). Negligible throughput
// impact — YouTube 1080p needs only ~156 read cycles/sec at 8KB.
const tcpBufSize = 8 * 1024
