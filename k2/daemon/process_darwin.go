//go:build darwin && !ios

package daemon

import "github.com/kaitu-io/k2/provider"

// newProcessSearcher returns a Darwin-specific ProcessSearcher using lsof
// to look up the process owning each connection by (network, srcIP, srcPort).
func newProcessSearcher() provider.ProcessSearcher {
	return provider.NewDarwinProcessSearcher()
}
