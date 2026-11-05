package quic

import (
	"github.com/kaitu-io/kuic/internal/protocol"
	"github.com/kaitu-io/kuic/internal/utils"
)

// recvPNStats counts what the peer sent us in the 1-RTT packet number space
// against what actually arrived, so the receiver can estimate the loss of
// the direction it does not send in (kuic addition, see MAINTAINING.md).
//
// Why the receiver needs it: loss in quic-go is only ever detected by the
// SENDER (sent-packet handler, congestion controller). A client that mostly
// downloads sends almost nothing ack-eliciting, so its own loss estimate never
// moves while the peer's packets toward it are being dropped — the client is
// blind to the one direction its user cares about.
//
// The peer numbers its 1-RTT packets with consecutive packet numbers, so every
// unique packet number that never arrives is a lost (or still-delayed) packet:
// span = largest − first + 1 packet numbers were sent, unique of them
// arrived. The sender's optimistic-ACK defence skips a packet number at most
// once per SkipPacketInitialPeriod (256) packets, so the estimate's bias is
// below 0.4%. A reordered packet below the largest seen counts as received
// and does not grow the span.
//
// record is only called from the connection's run loop (single goroutine);
// the two counters are atomics in utils.ConnectionStats so ConnectionStats()
// can be read from any goroutine.
type recvPNStats struct {
	started bool
	largest protocol.PacketNumber
}

// record accounts one newly received (decrypted, non-duplicate) 1-RTT packet.
func (r *recvPNStats) record(pn protocol.PacketNumber, stats *utils.ConnectionStats) {
	stats.PacketsReceived1RTT.Add(1)
	if !r.started {
		r.started = true
		r.largest = pn
		stats.PacketNumberSpan1RTT.Add(1)
		return
	}
	if pn > r.largest {
		stats.PacketNumberSpan1RTT.Add(uint64(pn - r.largest))
		r.largest = pn
	}
}
