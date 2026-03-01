package sniff

import (
	"encoding/binary"
	"testing"
)

// makeSTUN builds a minimal STUN packet with the given message type and body.
// STUN header: 2-byte type + 2-byte body length + 4-byte magic cookie + 12-byte transaction ID.
func makeSTUN(msgType uint16, body []byte) []byte {
	pkt := make([]byte, 20+len(body))
	binary.BigEndian.PutUint16(pkt[0:2], msgType)
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(body)))
	binary.BigEndian.PutUint32(pkt[4:8], 0x2112A442) // magic cookie
	// bytes 8-19: transaction ID (zero is fine for tests)
	copy(pkt[20:], body)
	return pkt
}

func TestIsSTUN_BindingRequest(t *testing.T) {
	t.Parallel()
	pkt := makeSTUN(0x0001, nil)
	if !IsSTUN(pkt) {
		t.Error("binding request should be detected as STUN")
	}
}

func TestIsSTUN_BindingResponse(t *testing.T) {
	t.Parallel()
	pkt := makeSTUN(0x0101, nil)
	if !IsSTUN(pkt) {
		t.Error("binding response should be detected as STUN")
	}
}

func TestIsSTUN_WithBody(t *testing.T) {
	t.Parallel()
	body := make([]byte, 8)
	pkt := makeSTUN(0x0001, body)
	if !IsSTUN(pkt) {
		t.Error("STUN packet with body should be detected")
	}
}

func TestIsSTUN_TooShort(t *testing.T) {
	t.Parallel()
	if IsSTUN([]byte{0x00, 0x01, 0x00, 0x00, 0x21, 0x12, 0xA4, 0x42}) {
		t.Error("packet shorter than 20 bytes should not be detected as STUN")
	}
}

func TestIsSTUN_WrongMagic(t *testing.T) {
	t.Parallel()
	pkt := makeSTUN(0x0001, nil)
	binary.BigEndian.PutUint32(pkt[4:8], 0xDEADBEEF) // corrupt magic
	if IsSTUN(pkt) {
		t.Error("wrong magic cookie should not be detected as STUN")
	}
}

func TestIsSTUN_BodyTruncated(t *testing.T) {
	t.Parallel()
	pkt := makeSTUN(0x0001, nil)
	binary.BigEndian.PutUint16(pkt[2:4], 100) // claims 100 bytes body but only 20 total
	if IsSTUN(pkt) {
		t.Error("truncated body should not be detected as STUN")
	}
}

func TestIsSTUN_Nil(t *testing.T) {
	t.Parallel()
	if IsSTUN(nil) {
		t.Error("nil packet should not be detected as STUN")
	}
}

func TestIsSTUN_Empty(t *testing.T) {
	t.Parallel()
	if IsSTUN([]byte{}) {
		t.Error("empty packet should not be detected as STUN")
	}
}
