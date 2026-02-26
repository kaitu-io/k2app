//go:build (darwin && !ios) || (linux && !android) || windows

package core

// maxTunnelConns is the maximum number of concurrent tunnel connections
// (HandleTCP + HandleUDP goroutines). Desktop has ample memory.
const maxTunnelConns = 4096
