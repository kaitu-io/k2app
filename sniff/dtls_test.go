package sniff

import "testing"

// makeDTLS builds a minimal 13-byte DTLS record with the given content type
// and version bytes. Remaining bytes are zero-filled.
func makeDTLS(contentType, versionMajor, versionMinor byte) []byte {
	pkt := make([]byte, 13)
	pkt[0] = contentType
	pkt[1] = versionMajor
	pkt[2] = versionMinor
	return pkt
}

func TestIsDTLS_HandshakeDTLS10(t *testing.T) {
	t.Parallel()
	if !IsDTLS(makeDTLS(22, 0xFE, 0xFF)) {
		t.Error("handshake DTLS 1.0 should be detected")
	}
}

func TestIsDTLS_HandshakeDTLS12(t *testing.T) {
	t.Parallel()
	if !IsDTLS(makeDTLS(22, 0xFE, 0xFD)) {
		t.Error("handshake DTLS 1.2 should be detected")
	}
}

func TestIsDTLS_ApplicationData(t *testing.T) {
	t.Parallel()
	if !IsDTLS(makeDTLS(23, 0xFE, 0xFD)) {
		t.Error("application data should be detected")
	}
}

func TestIsDTLS_Alert(t *testing.T) {
	t.Parallel()
	if !IsDTLS(makeDTLS(21, 0xFE, 0xFF)) {
		t.Error("alert should be detected")
	}
}

func TestIsDTLS_ChangeCipherSpec(t *testing.T) {
	t.Parallel()
	if !IsDTLS(makeDTLS(20, 0xFE, 0xFD)) {
		t.Error("change_cipher_spec should be detected")
	}
}

func TestIsDTLS_Heartbeat(t *testing.T) {
	t.Parallel()
	if !IsDTLS(makeDTLS(25, 0xFE, 0xFF)) {
		t.Error("heartbeat should be detected")
	}
}

func TestIsDTLS_TooShort(t *testing.T) {
	t.Parallel()
	if IsDTLS(make([]byte, 12)) {
		t.Error("packet shorter than 13 bytes should not be detected")
	}
}

func TestIsDTLS_WrongContentType(t *testing.T) {
	t.Parallel()
	if IsDTLS(makeDTLS(99, 0xFE, 0xFD)) {
		t.Error("wrong content type should not be detected")
	}
}

func TestIsDTLS_WrongVersionMajor(t *testing.T) {
	t.Parallel()
	// TLS version 0x0303 instead of DTLS 0xFExx
	if IsDTLS(makeDTLS(22, 0x03, 0x03)) {
		t.Error("TLS version major should not be detected as DTLS")
	}
}

func TestIsDTLS_WrongVersionMinor(t *testing.T) {
	t.Parallel()
	// 0xFE, 0xFE is not a valid DTLS version
	if IsDTLS(makeDTLS(22, 0xFE, 0xFE)) {
		t.Error("wrong version minor should not be detected")
	}
}

func TestIsDTLS_NilEmpty(t *testing.T) {
	t.Parallel()
	if IsDTLS(nil) {
		t.Error("nil packet should not be detected")
	}
	if IsDTLS([]byte{}) {
		t.Error("empty packet should not be detected")
	}
}
