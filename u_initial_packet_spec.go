package quic

// Ported from refraction-networking/uquic (BSD-3-Clause).

import (
	"github.com/kaitu-io/kuic/internal/protocol"
)

// PacketNumberLen is the number of bytes a packet number is encoded in.
type PacketNumberLen = protocol.PacketNumberLen

const (
	// PacketNumberLen1 is a 1 byte packet number
	PacketNumberLen1 PacketNumberLen = 1
	// PacketNumberLen2 is a 2 byte packet number
	PacketNumberLen2 PacketNumberLen = 2
	// PacketNumberLen3 is a 3 byte packet number
	PacketNumberLen3 PacketNumberLen = 3
	// PacketNumberLen4 is a 4 byte packet number
	PacketNumberLen4 PacketNumberLen = 4
)

// InitialPacketSpec describes the QUIC Initial flight's shape — everything
// outside the ClientHello itself. A ClientHelloSpec covers the TLS layer; this
// covers the QUIC layer around it, which a fingerprinter can read just as
// easily and which quic-go otherwise fills in with its own choices.
//
// Only the client sends a shaped Initial, so this is client-side only. The zero
// value changes nothing.
//
// A field is live only where the dial path reads it. Adding a field here
// without teaching the dial path to read it leaves it silently ignored on the
// wire — the failure mode this type exists to prevent.
type InitialPacketSpec struct {
	// DestConnIDLength is the length in bytes of the client-chosen destination
	// connection ID in the Initial packet.
	//
	// Zero means quic-go's default, which is a *random* length between 8 and 20
	// (protocol.GenerateConnectionIDForInitial). That randomness is itself a
	// discriminator: real clients are consistent. Chrome always uses 8.
	DestConnIDLength int

	// InitPacketNumber is the packet number of the first Initial packet.
	// Subsequent Initials increment from it.
	//
	// quic-go starts at 0. Chrome starts at 1 — a one-byte tell available to
	// anyone parsing the long header, no decryption needed.
	InitPacketNumber uint64

	// InitPacketNumberLengths is the packet-number encoding length for each
	// successive Initial packet: entry [0] applies to InitPacketNumber, [1] to
	// the next Initial, and so on, with the last entry repeating past the end
	// of the slice. Empty means quic-go's own choice.
	//
	// quic-go derives the length from the packet number and what has been
	// acknowledged, which for a first flight means 2 bytes. Chrome 146 sends
	// {1, 2}: one byte for PN=1, two for PN=2.
	InitPacketNumberLengths []PacketNumberLen

	// FrameBuilder re-cuts the Initial CRYPTO stream into the frame layout a real
	// client sends: many small CRYPTO frames interleaved with PING and PADDING,
	// rather than quic-go's single CRYPTO frame per packet. Applied per Initial
	// datagram of the fresh ClientHello flight only (see uPacketPacker); nil leaves
	// quic-go's framing untouched.
	//
	// Chrome 146:
	//
	//	&QUICRandomFrames{MinPING: 1, MaxPING: 4, MinCRYPTO: 6, MaxCRYPTO: 14,
	//	                  MinPADDING: 2, MaxPADDING: 6, Length: 1231 - 16}
	FrameBuilder QUICFrameBuilder
}

// initialPacketNumber returns the packet number the first Initial should carry.
func (s *InitialPacketSpec) initialPacketNumber() protocol.PacketNumber {
	if s == nil {
		return 0
	}
	return protocol.PacketNumber(s.InitPacketNumber)
}

// packetNumberLenFor returns the encoding length for the Initial packet at
// index idx (0 for the first), or 0 if the spec does not pin one — in which
// case quic-go's own calculation stands.
func (s *InitialPacketSpec) packetNumberLenFor(idx int) PacketNumberLen {
	if s == nil || len(s.InitPacketNumberLengths) == 0 || idx < 0 {
		return 0
	}
	if idx >= len(s.InitPacketNumberLengths) {
		return s.InitPacketNumberLengths[len(s.InitPacketNumberLengths)-1]
	}
	return s.InitPacketNumberLengths[idx]
}

// destConnIDLength returns the pinned Initial destination connection ID length,
// or 0 for quic-go's random-length default.
func (s *InitialPacketSpec) destConnIDLength() int {
	if s == nil {
		return 0
	}
	return s.DestConnIDLength
}
