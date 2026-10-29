package wire

import (
	"bufio"
	"bytes"
	"testing"
)

// TestAttack_ReadStreamHeader_HugeAddrLen proves that a QUIC varint encoding
// a huge addr_len value causes ReadStreamHeader to panic via make([]byte, huge).
// This is reachable from the network: any authenticated client (or anyone with
// the ECH config) can send a crafted stream header through QUIC or TCP-WS.
func TestAttack_ReadStreamHeader_HugeAddrLen(t *testing.T) {
	// Craft a stream header: [type:0x01][addr_len: max QUIC varint]
	// QUIC varint 0xFF 0xFF 0xFF 0xFF 0xFF 0xFF 0xFF 0xFF = 2^62 - 1 = 4611686018427387903
	buf := []byte{
		0x01,                                                       // StreamTypeTCP
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,           // addr_len = 2^62 - 1
	}

	r := bufio.NewReader(bytes.NewReader(buf))

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ReadStreamHeader PANICKED on huge addr_len varint: %v\n"+
				"This would crash k2s when a client sends a crafted stream header.\n"+
				"Root cause: no bounds check on addr_len before make([]byte, addrLen)", r)
		}
	}()

	_, _, err := ReadStreamHeader(r)
	if err != nil {
		t.Logf("ReadStreamHeader returned error (expected): %v", err)
		return
	}
	t.Log("ReadStreamHeader succeeded (unexpected)")
}

// TestAttack_ReadSmuxUDPFrame_HugeFrameLen proves that a huge frame_len varint
// causes ReadSmuxUDPFrame to panic via make([]byte, frameLen) when > 4096.
func TestAttack_ReadSmuxUDPFrame_HugeFrameLen(t *testing.T) {
	// Craft a smux UDP frame: [frame_len: huge varint]
	// Use a value > 4096 to bypass the pool path, but make([]byte, huge) panics.
	// QUIC varint 0xC0 0x00 0x00 0x01 0x00 0x00 0x00 0x00 = 4294967296 (4 GB)
	buf := []byte{
		0xC0, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, // frame_len = 4GB
	}

	r := bufio.NewReader(bytes.NewReader(buf))

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ReadSmuxUDPFrame PANICKED on huge frame_len: %v\n"+
				"This would crash k2s when a client sends a crafted UDP frame.\n"+
				"Root cause: no bounds check on frame_len before make([]byte, frameLen)", r)
		}
	}()

	_, err := ReadSmuxUDPFrame(r)
	if err != nil {
		t.Logf("ReadSmuxUDPFrame returned error (expected): %v", err)
		return
	}
	t.Log("ReadSmuxUDPFrame succeeded (unexpected)")
}

// TestAttack_ReadUDPFrame_HugeAddrLen proves that a huge addr_len varint
// causes ReadUDPFrame to panic via make([]byte, addrLen).
func TestAttack_ReadUDPFrame_HugeAddrLen(t *testing.T) {
	// Craft a UDP frame: [session_id:4][addr_len: huge varint]
	buf := []byte{
		0x00, 0x00, 0x00, 0x01,                                   // session_id = 1
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,           // addr_len = 2^62 - 1
	}

	r := bufio.NewReader(bytes.NewReader(buf))

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ReadUDPFrame PANICKED on huge addr_len: %v\n"+
				"Root cause: no bounds check on addr_len before make([]byte, addrLen)", r)
		}
	}()

	_, err := ReadUDPFrame(r)
	if err != nil {
		t.Logf("ReadUDPFrame returned error (expected): %v", err)
		return
	}
	t.Log("ReadUDPFrame succeeded (unexpected)")
}
