package sniff

import (
	"encoding/binary"
	"testing"
)

func TestParseQUICClientHello_ValidTLS(t *testing.T) {
	// Build a minimal TLS record with ClientHello
	ch := buildMinimalClientHello("example.com", []uint16{0x1301, 0x1302}, []uint16{0x001d})
	result := parseQUICClientHello(ch)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ServerName != "example.com" {
		t.Errorf("ServerName = %q, want example.com", result.ServerName)
	}
	if len(result.CipherSuites) != 2 {
		t.Errorf("CipherSuites len = %d, want 2", len(result.CipherSuites))
	}
	if result.CipherSuites[0] != 0x1301 {
		t.Errorf("CipherSuites[0] = 0x%04x, want 0x1301", result.CipherSuites[0])
	}
	if len(result.EllipticCurves) != 1 || result.EllipticCurves[0] != 0x001d {
		t.Errorf("EllipticCurves = %v, want [0x001d]", result.EllipticCurves)
	}
}

func TestParseQUICClientHello_NoSNI(t *testing.T) {
	ch := buildMinimalClientHelloNoSNI([]uint16{0x1301})
	result := parseQUICClientHello(ch)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ServerName != "" {
		t.Errorf("ServerName = %q, want empty", result.ServerName)
	}
}

func TestParseQUICClientHello_Invalid(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"too short", []byte{0x16, 0x03, 0x03}},
		{"wrong type", []byte{0x17, 0x03, 0x03, 0x00, 0x00}},
		{"truncated record", append([]byte{0x16, 0x03, 0x03, 0x00, 0xFF}, make([]byte, 10)...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := parseQUICClientHello(tt.data); result != nil {
				t.Errorf("expected nil for %s", tt.name)
			}
		})
	}
}

func TestParseSupportedVersions(t *testing.T) {
	// [length=4, 0x0304 (TLS 1.3), 0x0303 (TLS 1.2)]
	data := []byte{0x04, 0x03, 0x04, 0x03, 0x03}
	versions := parseSupportedVersions(data)
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	if versions[0] != 0x0304 {
		t.Errorf("versions[0] = 0x%04x, want 0x0304", versions[0])
	}
	if versions[1] != 0x0303 {
		t.Errorf("versions[1] = 0x%04x, want 0x0303", versions[1])
	}
}

func TestParseUint16List(t *testing.T) {
	// [length=4, 0x001d (X25519), 0x0017 (secp256r1)]
	data := []byte{0x00, 0x04, 0x00, 0x1d, 0x00, 0x17}
	result := parseUint16List(data)
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	if result[0] != 0x001d {
		t.Errorf("result[0] = 0x%04x, want 0x001d", result[0])
	}
}

// buildMinimalClientHello constructs a TLS record containing a ClientHello
// with the specified SNI, cipher suites, and curves.
func buildMinimalClientHello(sni string, ciphers, curves []uint16) []byte {
	body := buildClientHelloBody(sni, ciphers, curves)
	// Handshake header: type=1 (ClientHello), length=3 bytes
	hs := make([]byte, 4+len(body))
	hs[0] = 1 // ClientHello
	hs[1] = byte(len(body) >> 16)
	hs[2] = byte(len(body) >> 8)
	hs[3] = byte(len(body))
	copy(hs[4:], body)

	// TLS record header
	record := make([]byte, 5+len(hs))
	record[0] = 0x16 // Handshake
	record[1] = 0x03
	record[2] = 0x03 // TLS 1.2
	binary.BigEndian.PutUint16(record[3:], uint16(len(hs)))
	copy(record[5:], hs)
	return record
}

// buildMinimalClientHelloNoSNI builds a ClientHello without SNI extension.
func buildMinimalClientHelloNoSNI(ciphers []uint16) []byte {
	return buildMinimalClientHello("", ciphers, nil)
}

func buildClientHelloBody(sni string, ciphers, curves []uint16) []byte {
	var b []byte
	// Client version (TLS 1.2)
	b = append(b, 0x03, 0x03)
	// Random (32 bytes)
	b = append(b, make([]byte, 32)...)
	// Session ID (empty)
	b = append(b, 0x00)

	// Cipher suites
	csLen := len(ciphers) * 2
	b = append(b, byte(csLen>>8), byte(csLen))
	for _, cs := range ciphers {
		b = append(b, byte(cs>>8), byte(cs))
	}

	// Compression methods (null only)
	b = append(b, 0x01, 0x00)

	// Extensions
	var exts []byte

	// SNI extension
	if sni != "" {
		sniData := buildSNIExtension(sni)
		exts = appendExtension(exts, 0x0000, sniData)
	}

	// Supported groups
	if len(curves) > 0 {
		listData := make([]byte, 2+len(curves)*2)
		binary.BigEndian.PutUint16(listData, uint16(len(curves)*2))
		for i, c := range curves {
			binary.BigEndian.PutUint16(listData[2+i*2:], c)
		}
		exts = appendExtension(exts, 0x000a, listData)
	}

	// Supported versions (TLS 1.3)
	exts = appendExtension(exts, 0x002b, []byte{0x02, 0x03, 0x04})

	// Signature algorithms (ECDSA-P256-SHA256)
	sigData := []byte{0x00, 0x02, 0x04, 0x03}
	exts = appendExtension(exts, 0x000d, sigData)

	// Extensions length
	b = append(b, byte(len(exts)>>8), byte(len(exts)))
	b = append(b, exts...)
	return b
}

func buildSNIExtension(name string) []byte {
	// server_name_list_length (2) + name_type (1) + name_length (2) + name
	nameLen := len(name)
	listLen := 3 + nameLen
	data := make([]byte, 2+listLen)
	binary.BigEndian.PutUint16(data, uint16(listLen))
	data[2] = 0 // host_name
	binary.BigEndian.PutUint16(data[3:], uint16(nameLen))
	copy(data[5:], name)
	return data
}

func appendExtension(buf []byte, extType uint16, data []byte) []byte {
	buf = append(buf, byte(extType>>8), byte(extType))
	buf = append(buf, byte(len(data)>>8), byte(len(data)))
	buf = append(buf, data...)
	return buf
}
