package sniff

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// makeSTUN builds a minimal RFC 5389 STUN packet with magic cookie.
func makeSTUN(msgType uint16, body []byte) []byte {
	pkt := make([]byte, 20+len(body))
	binary.BigEndian.PutUint16(pkt[0:2], msgType)
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(body)))
	binary.BigEndian.PutUint32(pkt[4:8], 0x2112A442) // magic cookie
	copy(pkt[20:], body)
	return pkt
}

// makeClassicSTUN builds an RFC 3489 STUN packet (no magic cookie).
func makeClassicSTUN(msgType uint16, body []byte) []byte {
	pkt := make([]byte, 20+len(body))
	binary.BigEndian.PutUint16(pkt[0:2], msgType)
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(body)))
	// bytes 4-19: transaction ID (no magic cookie)
	pkt[4] = 0x48
	pkt[5] = 0x6f
	pkt[6] = 0xf5
	pkt[7] = 0x08
	copy(pkt[20:], body)
	return pkt
}

// --- RFC 5389/8489 (modern STUN with magic cookie) ---

func TestIsSTUN_BindingRequest(t *testing.T) {
	t.Parallel()
	if !IsSTUN(makeSTUN(0x0001, nil)) {
		t.Error("RFC 5389 binding request should be detected")
	}
}

func TestIsSTUN_BindingResponse(t *testing.T) {
	t.Parallel()
	if !IsSTUN(makeSTUN(0x0101, nil)) {
		t.Error("RFC 5389 binding response should be detected")
	}
}

func TestIsSTUN_WithBody(t *testing.T) {
	t.Parallel()
	if !IsSTUN(makeSTUN(0x0001, make([]byte, 8))) {
		t.Error("STUN packet with body should be detected")
	}
}

// --- RFC 3489 (classic STUN, no magic cookie) ---

func TestIsSTUN_Classic_BindingRequest(t *testing.T) {
	t.Parallel()
	if !IsSTUN(makeClassicSTUN(0x0001, nil)) {
		t.Error("RFC 3489 binding request should be detected")
	}
}

func TestIsSTUN_Classic_BindingResponse(t *testing.T) {
	t.Parallel()
	if !IsSTUN(makeClassicSTUN(0x0101, nil)) {
		t.Error("RFC 3489 binding response should be detected")
	}
}

func TestIsSTUN_Classic_BindingErrorResponse(t *testing.T) {
	t.Parallel()
	if !IsSTUN(makeClassicSTUN(0x0111, nil)) {
		t.Error("RFC 3489 binding error response should be detected")
	}
}

func TestIsSTUN_Classic_SharedSecretRequest(t *testing.T) {
	t.Parallel()
	if !IsSTUN(makeClassicSTUN(0x0002, nil)) {
		t.Error("RFC 3489 shared secret request should be detected")
	}
}

func TestIsSTUN_Classic_WithBody(t *testing.T) {
	t.Parallel()
	if !IsSTUN(makeClassicSTUN(0x0001, make([]byte, 24))) {
		t.Error("RFC 3489 binding request with body should be detected")
	}
}

// --- Real Zoom packet from captured logs ---

func TestIsSTUN_ZoomPacket(t *testing.T) {
	t.Parallel()
	// Actual Zoom :3478 packet: type=0x0001 (Binding Request), length=0x0018 (24 bytes body),
	// no magic cookie (RFC 3489), transaction ID starts with 486ff508...
	// 20-byte header + 24-byte body = 44 bytes total
	raw, _ := hex.DecodeString(
		"0001001848" + "6ff508" + // type=0x0001, len=0x0018, txn ID start
			"000000000000000000000000" + // rest of 20-byte header
			"000000000000000000000000" + // 12 bytes body
			"000000000000000000000000") // 12 bytes body (total 24)
	if !IsSTUN(raw) {
		t.Errorf("real Zoom STUN packet should be detected, len=%d", len(raw))
	}
}

// --- Negative cases ---

func TestIsSTUN_TooShort(t *testing.T) {
	t.Parallel()
	if IsSTUN([]byte{0x00, 0x01, 0x00, 0x00, 0x21, 0x12, 0xA4, 0x42}) {
		t.Error("packet shorter than 20 bytes should not be detected")
	}
}

func TestIsSTUN_Nil(t *testing.T) {
	t.Parallel()
	if IsSTUN(nil) {
		t.Error("nil should not be detected")
	}
}

func TestIsSTUN_Empty(t *testing.T) {
	t.Parallel()
	if IsSTUN([]byte{}) {
		t.Error("empty should not be detected")
	}
}

func TestIsSTUN_BodyTruncated(t *testing.T) {
	t.Parallel()
	pkt := makeSTUN(0x0001, nil)
	binary.BigEndian.PutUint16(pkt[2:4], 100) // claims 100 bytes but only 20 total
	if IsSTUN(pkt) {
		t.Error("truncated body should not be detected")
	}
}

func TestIsSTUN_BodyNotAligned(t *testing.T) {
	t.Parallel()
	// RFC 5389 magic cookie but body length not 4-byte aligned
	pkt := makeSTUN(0x0001, make([]byte, 5))
	binary.BigEndian.PutUint16(pkt[2:4], 5) // 5 is not multiple of 4
	if IsSTUN(pkt) {
		t.Error("non-aligned body length should not be detected")
	}
}

func TestIsSTUN_HighBitsSet(t *testing.T) {
	t.Parallel()
	// First 2 bits != 0 → not STUN (could be RTP/DTLS)
	pkt := makeSTUN(0x0001, nil)
	pkt[0] = 0x80 // set high bit
	if IsSTUN(pkt) {
		t.Error("high bits set should not be detected as STUN")
	}
}

func TestIsSTUN_UnknownType_NoMagic(t *testing.T) {
	t.Parallel()
	// No magic cookie + unknown message type → not STUN
	pkt := makeClassicSTUN(0x0099, nil)
	if IsSTUN(pkt) {
		t.Error("unknown type without magic cookie should not be detected")
	}
}

func TestIsSTUN_WrongMagic_UnknownType(t *testing.T) {
	t.Parallel()
	// No magic cookie + unknown type → reject
	pkt := make([]byte, 20)
	binary.BigEndian.PutUint16(pkt[0:2], 0x0055)
	binary.BigEndian.PutUint32(pkt[4:8], 0xDEADBEEF)
	if IsSTUN(pkt) {
		t.Error("wrong magic + unknown type should not be detected")
	}
}

// --- Benchmark ---

func BenchmarkIsSTUN_Modern(b *testing.B) {
	pkt := makeSTUN(0x0001, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsSTUN(pkt)
	}
}

func BenchmarkIsSTUN_Classic(b *testing.B) {
	pkt := makeClassicSTUN(0x0001, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsSTUN(pkt)
	}
}
