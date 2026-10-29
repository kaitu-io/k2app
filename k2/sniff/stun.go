package sniff

import "encoding/binary"

// STUN message types (method 0x001 Binding + class bits).
// RFC 5389 §18.1, RFC 3489 §11.1.
var stunMessageTypes = map[uint16]bool{
	0x0001: true, // Binding Request
	0x0101: true, // Binding Response
	0x0111: true, // Binding Error Response
	0x0002: true, // Shared Secret Request (RFC 3489 only)
	0x0102: true, // Shared Secret Response (RFC 3489 only)
	0x0112: true, // Shared Secret Error Response (RFC 3489 only)
}

// IsSTUN checks if a UDP packet is a STUN message.
//
// Supports both modern STUN (RFC 5389/8489 with magic cookie 0x2112A442)
// and classic STUN (RFC 3489 without magic cookie, used by Zoom).
//
// RFC 5389 §6: first 2 bits of message type MUST be 0.
// Body length MUST be a multiple of 4.
func IsSTUN(packet []byte) bool {
	if len(packet) < 20 {
		return false
	}

	// First 2 bits must be 0 (distinguishes STUN from RTP/DTLS per RFC 5764 §5.1.2).
	if packet[0]&0xC0 != 0 {
		return false
	}

	bodyLen := int(binary.BigEndian.Uint16(packet[2:4]))

	// Body length must be 4-byte aligned (RFC 5389 §6).
	if bodyLen%4 != 0 {
		return false
	}

	// Packet must contain full message.
	if len(packet) < 20+bodyLen {
		return false
	}

	// RFC 5389/8489: magic cookie present — modern STUN.
	if binary.BigEndian.Uint32(packet[4:8]) == 0x2112A442 {
		return true
	}

	// RFC 3489: no magic cookie. Validate by message type.
	msgType := binary.BigEndian.Uint16(packet[0:2])
	return stunMessageTypes[msgType]
}
