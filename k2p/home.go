package k2p

import (
	"context"
	"fmt"

	"github.com/kaitu-io/k2/wire"
)

// Listen starts the home peer: registers with signal server, waits for
// away peers, performs ICE connectivity checks, and accepts QUIC connections.
//
// Returns a channel of wire.Listener — one per successfully connected peer.
// Channel is closed when ctx is cancelled.
//
// TODO(task-9): reimplement with pion/ice.
func Listen(ctx context.Context, cfg Config) (<-chan wire.Listener, error) {
	return nil, fmt.Errorf("k2p: Listen not yet reimplemented with ICE")
}
