// Package sniff JA3 ClientHello parser.
// Adapted from github.com/sagernet/sing-box/common/ja3/parser.go (BSD 3-Clause License).
package sniff

import "encoding/binary"

// QUICClientHello holds parsed TLS ClientHello fields for client fingerprinting.
type QUICClientHello struct {
	ServerName          string
	CipherSuites        []uint16
	Extensions          []uint16
	EllipticCurves      []uint16
	SignatureAlgorithms []uint16
	Versions            []uint16
}

// parseQUICClientHello parses a TLS record (with 5-byte header) containing a
// ClientHello handshake message and extracts fingerprinting fields.
// Returns nil if the data is not a valid ClientHello.
func parseQUICClientHello(data []byte) *QUICClientHello {
	// Need at least TLS record header (5) + handshake header (4) + client version (2) + random (32)
	if len(data) < 5+4+2+32 {
		return nil
	}
	// Verify TLS Handshake record
	if data[0] != 0x16 {
		return nil
	}
	recordLen := int(binary.BigEndian.Uint16(data[3:5]))
	if 5+recordLen > len(data) {
		return nil
	}

	hs := data[5 : 5+recordLen]
	return parseHandshake(hs)
}

// parseHandshake parses a TLS Handshake message (after record header).
func parseHandshake(data []byte) *QUICClientHello {
	if len(data) < 4 {
		return nil
	}
	// Handshake type must be ClientHello (1)
	if data[0] != 1 {
		return nil
	}
	// Handshake length (3 bytes)
	hsLen := int(data[1])<<16 | int(data[2])<<8 | int(data[3])
	if hsLen+4 > len(data) {
		return nil
	}
	body := data[4 : 4+hsLen]
	return parseClientHello(body)
}

// parseClientHello parses the ClientHello body (after handshake header).
func parseClientHello(data []byte) *QUICClientHello {
	if len(data) < 2+32 {
		return nil
	}
	ch := &QUICClientHello{}

	pos := 0
	// Client version (2 bytes) — skip
	pos += 2
	// Random (32 bytes) — skip
	pos += 32

	// Session ID
	if pos >= len(data) {
		return nil
	}
	sessionIDLen := int(data[pos])
	pos++
	if pos+sessionIDLen > len(data) {
		return nil
	}
	pos += sessionIDLen

	// Cipher suites
	if pos+2 > len(data) {
		return nil
	}
	csLen := int(binary.BigEndian.Uint16(data[pos:]))
	pos += 2
	if pos+csLen > len(data) || csLen%2 != 0 {
		return nil
	}
	ch.CipherSuites = make([]uint16, csLen/2)
	for i := 0; i < csLen; i += 2 {
		ch.CipherSuites[i/2] = binary.BigEndian.Uint16(data[pos+i:])
	}
	pos += csLen

	// Compression methods — skip
	if pos >= len(data) {
		return nil
	}
	compLen := int(data[pos])
	pos++
	if pos+compLen > len(data) {
		return nil
	}
	pos += compLen

	// Extensions
	if pos+2 > len(data) {
		return ch // No extensions, still valid
	}
	extTotalLen := int(binary.BigEndian.Uint16(data[pos:]))
	pos += 2
	if pos+extTotalLen > len(data) {
		return nil
	}
	extEnd := pos + extTotalLen

	// Pre-size: each extension is at least 4 bytes (type + length), estimate count.
	ch.Extensions = make([]uint16, 0, extTotalLen/4)

	for pos+4 <= extEnd {
		extType := binary.BigEndian.Uint16(data[pos:])
		extLen := int(binary.BigEndian.Uint16(data[pos+2:]))
		pos += 4
		if pos+extLen > extEnd {
			return nil
		}
		extData := data[pos : pos+extLen]
		ch.Extensions = append(ch.Extensions, extType)

		switch extType {
		case 0x0000: // server_name
			ch.ServerName = parseSNIExtension(extData)
		case 0x000a: // supported_groups (elliptic_curves)
			ch.EllipticCurves = parseUint16List(extData)
		case 0x000d: // signature_algorithms
			ch.SignatureAlgorithms = parseUint16List(extData)
		case 0x002b: // supported_versions
			ch.Versions = parseSupportedVersions(extData)
		}

		pos += extLen
	}

	return ch
}

// parseSNIExtension extracts the hostname from a server_name extension.
func parseSNIExtension(data []byte) string {
	if len(data) < 2 {
		return ""
	}
	listLen := int(binary.BigEndian.Uint16(data))
	if 2+listLen > len(data) {
		return ""
	}
	pos := 2
	end := 2 + listLen
	for pos+3 <= end {
		nameType := data[pos]
		nameLen := int(binary.BigEndian.Uint16(data[pos+1:]))
		pos += 3
		if pos+nameLen > end {
			return ""
		}
		if nameType == 0 { // host_name
			return string(data[pos : pos+nameLen])
		}
		pos += nameLen
	}
	return ""
}

// parseUint16List parses a 2-byte length-prefixed list of uint16 values.
func parseUint16List(data []byte) []uint16 {
	if len(data) < 2 {
		return nil
	}
	listLen := int(binary.BigEndian.Uint16(data))
	if 2+listLen > len(data) || listLen%2 != 0 {
		return nil
	}
	result := make([]uint16, listLen/2)
	for i := 0; i < listLen; i += 2 {
		result[i/2] = binary.BigEndian.Uint16(data[2+i:])
	}
	return result
}

// parseSupportedVersions extracts supported TLS versions from the extension.
// ClientHello uses a 1-byte length prefix for the version list.
func parseSupportedVersions(data []byte) []uint16 {
	if len(data) < 1 {
		return nil
	}
	listLen := int(data[0])
	if 1+listLen > len(data) || listLen%2 != 0 {
		return nil
	}
	result := make([]uint16, listLen/2)
	for i := 0; i < listLen; i += 2 {
		result[i/2] = binary.BigEndian.Uint16(data[1+i:])
	}
	return result
}
