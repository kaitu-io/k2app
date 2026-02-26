//go:build ios

package core

// maxTunnelConns is the maximum number of concurrent tunnel connections.
// iOS Network Extension has ~50 MB memory limit.
const maxTunnelConns = 512
