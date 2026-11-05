package quic

import (
	"testing"

	"github.com/kaitu-io/kuic/internal/protocol"
	"github.com/kaitu-io/kuic/internal/utils"
)

func recvPNFeed(pns ...protocol.PacketNumber) (received, span uint64) {
	var r recvPNStats
	var s utils.ConnectionStats
	for _, pn := range pns {
		r.record(pn, &s)
	}
	return s.PacketsReceived1RTT.Load(), s.PacketNumberSpan1RTT.Load()
}

func TestRecvPNStats_InOrderIsLossless(t *testing.T) {
	rcv, span := recvPNFeed(0, 1, 2, 3, 4, 5, 6, 7, 8, 9)
	if rcv != 10 || span != 10 {
		t.Fatalf("received=%d span=%d, want 10/10", rcv, span)
	}
}

// The first packet number need not be 0 (the 1-RTT space starts wherever the
// peer's 0-RTT/1-RTT numbering is): the span counts from the first one seen.
func TestRecvPNStats_SpanStartsAtFirstPacket(t *testing.T) {
	rcv, span := recvPNFeed(1000, 1001, 1002)
	if rcv != 3 || span != 3 {
		t.Fatalf("received=%d span=%d, want 3/3", rcv, span)
	}
}

func TestRecvPNStats_GapsCountAsLost(t *testing.T) {
	// 0..9 sent, 3 of them (2, 5, 6) never arrive.
	rcv, span := recvPNFeed(0, 1, 3, 4, 7, 8, 9)
	if rcv != 7 || span != 10 {
		t.Fatalf("received=%d span=%d, want 7/10", rcv, span)
	}
}

// A packet arriving after a higher one fills its gap: it counts as received
// and must not grow the span a second time.
func TestRecvPNStats_ReorderedFillsGap(t *testing.T) {
	rcv, span := recvPNFeed(0, 1, 3, 2, 4)
	if rcv != 5 || span != 5 {
		t.Fatalf("received=%d span=%d, want 5/5", rcv, span)
	}
}

// Trailing loss (the newest packets have not arrived yet) is invisible until a
// later packet arrives — the estimate never over-reports.
func TestRecvPNStats_TrailingLossInvisibleUntilNextPacket(t *testing.T) {
	rcv, span := recvPNFeed(0, 1, 2)
	if rcv != span {
		t.Fatalf("received=%d span=%d: trailing loss must not be counted", rcv, span)
	}
	rcv, span = recvPNFeed(0, 1, 2, 9)
	if rcv != 4 || span != 10 {
		t.Fatalf("received=%d span=%d, want 4/10 once 9 arrives", rcv, span)
	}
}
