package wire

import (
	"testing"
)

// TestPeekClientHello_InflatedExtsLen proves that a malformed ClientHello
// with inflated extensions length causes PeekClientHello to panic with
// "index out of range". This is the #1 suspected crash cause for k2s.
func TestPeekClientHello_InflatedExtsLen(t *testing.T) {
	// Build a minimal TLS ClientHello with inflated extensions_length.
	//
	// Layout:
	//   TLS Record:    [0x16][0x03 0x01][recordLen:2]
	//   Handshake:     [0x01][bodyLen:3]
	//   ClientHello:   [version:2][random:32][sid_len:1=0]
	//                  [cipher_suites_len:2=2][suite:2]
	//                  [compression_len:1=1][method:1]
	//                  [extensions_len:2=0xFF 0xFF]  ← inflated!
	//                  [actual ext data: just a few bytes]

	body := make([]byte, 0, 256)

	// Version (TLS 1.2)
	body = append(body, 0x03, 0x03)
	// Random (32 bytes)
	body = append(body, make([]byte, 32)...)
	// Session ID length = 0
	body = append(body, 0x00)
	// Cipher suites length = 2 (one suite)
	body = append(body, 0x00, 0x02)
	body = append(body, 0x13, 0x01) // TLS_AES_128_GCM_SHA256
	// Compression methods length = 1
	body = append(body, 0x01)
	body = append(body, 0x00) // null compression

	// Extensions length = 0xFFFF (65535!) but we only have a few bytes after
	body = append(body, 0xFF, 0xFF)

	// Add a small SNI extension (just enough to make pos+4 fit if extsEnd were correct)
	// ext type = 0x0000 (SNI), ext len = 5
	body = append(body, 0x00, 0x00, 0x00, 0x05)
	body = append(body, 0x00, 0x03, 0x00, 0x00, 0x01) // minimal SNI data
	// A few more bytes of junk
	body = append(body, 0xDE, 0xAD, 0xBE, 0xEF)

	// Build handshake wrapper
	bodyLen := len(body)
	handshake := make([]byte, 0, 4+bodyLen)
	handshake = append(handshake, 0x01) // ClientHello
	handshake = append(handshake, byte(bodyLen>>16), byte(bodyLen>>8), byte(bodyLen))
	handshake = append(handshake, body...)

	// Build TLS record wrapper
	recordLen := len(handshake)
	record := make([]byte, 0, 5+recordLen)
	record = append(record, 0x16)       // Handshake
	record = append(record, 0x03, 0x01) // TLS 1.0
	record = append(record, byte(recordLen>>8), byte(recordLen))
	record = append(record, handshake...)

	// This should NOT panic, but it does before the fix.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PeekClientHello PANICKED on malformed input: %v\n"+
				"This would crash k2s when receiving malformed TLS from scanners", r)
		}
	}()

	_, err := PeekClientHello(record)
	if err != nil {
		t.Logf("PeekClientHello returned error (expected): %v", err)
		return
	}
	t.Log("PeekClientHello succeeded (unexpected for malformed input)")
}

// TestPeekClientHello_InflatedExtLen proves that a single extension with
// inflated ext_len causes a panic when slicing body[pos : pos+extLen].
func TestPeekClientHello_InflatedExtLen(t *testing.T) {
	body := make([]byte, 0, 256)

	// Version + Random + Session ID
	body = append(body, 0x03, 0x03)
	body = append(body, make([]byte, 32)...)
	body = append(body, 0x00)
	// Cipher suites
	body = append(body, 0x00, 0x02, 0x13, 0x01)
	// Compression
	body = append(body, 0x01, 0x00)

	// Extensions length = 8 (just enough for one extension header + 0 data)
	body = append(body, 0x00, 0x08)

	// SNI extension with inflated length
	// ext type = 0x0000, ext len = 0x00FF (255!) but only 0 bytes follow
	body = append(body, 0x00, 0x00, 0x00, 0xFF)
	// Only 4 bytes of actual data (not 255)
	body = append(body, 0x00, 0x03, 0x00, 0x00)

	// Handshake + TLS record wrapping
	bodyLen := len(body)
	handshake := make([]byte, 0, 4+bodyLen)
	handshake = append(handshake, 0x01)
	handshake = append(handshake, byte(bodyLen>>16), byte(bodyLen>>8), byte(bodyLen))
	handshake = append(handshake, body...)

	recordLen := len(handshake)
	record := make([]byte, 0, 5+recordLen)
	record = append(record, 0x16, 0x03, 0x01)
	record = append(record, byte(recordLen>>8), byte(recordLen))
	record = append(record, handshake...)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PeekClientHello PANICKED on inflated ext_len: %v\n"+
				"This would crash k2s when receiving crafted TLS packets", r)
		}
	}()

	_, err := PeekClientHello(record)
	if err != nil {
		t.Logf("PeekClientHello returned error (expected): %v", err)
		return
	}
}

// TestPeekClientHello_InflatedSessionIDLen proves inflated session_id_len
// causes pos to jump past cipher_suites, hitting wrong bytes.
func TestPeekClientHello_InflatedSessionIDLen(t *testing.T) {
	body := make([]byte, 0, 256)

	body = append(body, 0x03, 0x03)
	body = append(body, make([]byte, 32)...)
	// Session ID length = 0xFF (255!) but body is small
	body = append(body, 0xFF)
	// Only 10 bytes of padding (not 255)
	body = append(body, make([]byte, 10)...)

	bodyLen := len(body)
	handshake := make([]byte, 0, 4+bodyLen)
	handshake = append(handshake, 0x01)
	handshake = append(handshake, byte(bodyLen>>16), byte(bodyLen>>8), byte(bodyLen))
	handshake = append(handshake, body...)

	recordLen := len(handshake)
	record := make([]byte, 0, 5+recordLen)
	record = append(record, 0x16, 0x03, 0x01)
	record = append(record, byte(recordLen>>8), byte(recordLen))
	record = append(record, handshake...)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PeekClientHello PANICKED on inflated session_id_len: %v", r)
		}
	}()

	_, err := PeekClientHello(record)
	if err != nil {
		t.Logf("Expected error: %v", err)
		return
	}
}
