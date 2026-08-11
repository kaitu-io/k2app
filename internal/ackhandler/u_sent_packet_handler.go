package ackhandler

// Ported from refraction-networking/uquic (BSD-3-Clause).

import "github.com/kaitu-io/kuic/internal/protocol"

// SetInitialPacketNumberLengths pins the packet-number encoding length of each successive
// Initial packet, so a fingerprinting client can match a real one's long-header packet
// numbers. base is the packet number of the first Initial (InitialPacketSpec's
// InitPacketNumber); pnLens[0] applies to it, pnLens[1] to the next Initial, and so on,
// with the last entry repeating past the end of the slice.
//
// It is a no-op on a handler that isn't the concrete *sentPacketHandler (the mock in
// tests), and on an empty pnLens (which leaves quic-go's own length calculation in
// place). Unlike upstream uQUIC, kuic stores the lengths on the handler directly rather
// than wrapping it in a uSentPacketHandler — the handler is a fork-local type, so there
// is no reason to shadow its constructor.
func SetInitialPacketNumberLengths(h SentPacketHandler, base protocol.PacketNumber, pnLens []protocol.PacketNumberLen) {
	sph, ok := h.(*sentPacketHandler)
	if !ok {
		return
	}
	sph.initialPacketNumberBase = base
	sph.initialPacketNumberLengths = pnLens
}
