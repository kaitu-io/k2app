package sniff

import "encoding/binary"

// IsSTUN checks if a UDP packet is a STUN message.
// Requires at least 20 bytes (STUN header) and the magic cookie 0x2112A442 at bytes 4-7.
// Reference: https://www.rfc-editor.org/rfc/rfc8489#section-5
func IsSTUN(packet []byte) bool {
	if len(packet) < 20 {
		return false
	}
	if binary.BigEndian.Uint32(packet[4:8]) != 0x2112A442 {
		return false
	}
	bodyLen := int(binary.BigEndian.Uint16(packet[2:4]))
	return len(packet) >= 20+bodyLen
}
