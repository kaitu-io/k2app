//go:build android

package core

// maxTunnelConns is the maximum number of concurrent tunnel connections.
// Android has moderate memory available.
const maxTunnelConns = 2048
