package sniff

// IsDTLS checks if a UDP packet starts with a valid DTLS record header.
// Requires at least 13 bytes (DTLS record header size). Validates content type,
// version major (0xFE), and version minor (0xFF = DTLS 1.0, 0xFD = DTLS 1.2).
// Reference: RFC 6347 (DTLS 1.2), RFC 4347 (DTLS 1.0).
func IsDTLS(packet []byte) bool {
	if len(packet) < 13 {
		return false
	}
	switch packet[0] {
	case 20, 21, 22, 23, 25: // change_cipher_spec, alert, handshake, application_data, heartbeat
	default:
		return false
	}
	if packet[1] != 0xFE {
		return false
	}
	return packet[2] == 0xFF || packet[2] == 0xFD
}
