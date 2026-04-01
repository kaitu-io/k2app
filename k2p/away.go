package k2p

import (
	"context"
	"fmt"

	"github.com/kaitu-io/k2/wire"
)

// ConnectResult is the output of a successful Dial().
type ConnectResult struct {
	Dialer        wire.Dialer
	TransportType string // "p2p-direct" or "p2p-relay"
}

// Dial connects to a home peer via ICE connectivity checks.
// Steps: lookup home → exchange ICE candidates via signal → QUIC dial over ice.Conn.
//
// TODO(task-8): reimplement with pion/ice.
func Dial(ctx context.Context, cfg Config) (*ConnectResult, error) {
	return nil, fmt.Errorf("k2p: Dial not yet reimplemented with ICE")
}
