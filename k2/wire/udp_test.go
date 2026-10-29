package wire

import (
	"bytes"
	"testing"
)

// ============================================================================
// UDP Message Frame
// ============================================================================

func TestWriteUDPFrame_WithAddr(t *testing.T) {
	var buf bytes.Buffer
	addr := "8.8.8.8:53"
	payload := []byte("dns-query")

	err := WriteUDPFrame(&buf, 1, addr, payload)
	if err != nil {
		t.Fatalf("WriteUDPFrame: %v", err)
	}

	// Read it back
	f, err := ReadUDPFrame(&buf)
	if err != nil {
		t.Fatalf("ReadUDPFrame: %v", err)
	}
	if f.SessionID != 1 {
		t.Errorf("session_id = %d, want 1", f.SessionID)
	}
	if f.Addr != addr {
		t.Errorf("addr = %q, want %q", f.Addr, addr)
	}
	if !bytes.Equal(f.Payload, payload) {
		t.Errorf("payload = %x, want %x", f.Payload, payload)
	}
}

func TestWriteUDPFrame_NoAddr(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("dns-query-2")

	// addr_len = 0, no address (subsequent packet)
	err := WriteUDPFrame(&buf, 42, "", payload)
	if err != nil {
		t.Fatalf("WriteUDPFrame: %v", err)
	}

	f, err := ReadUDPFrame(&buf)
	if err != nil {
		t.Fatalf("ReadUDPFrame: %v", err)
	}
	if f.SessionID != 42 {
		t.Errorf("session_id = %d, want 42", f.SessionID)
	}
	if f.Addr != "" {
		t.Errorf("addr = %q, want empty", f.Addr)
	}
	if !bytes.Equal(f.Payload, payload) {
		t.Errorf("payload mismatch")
	}
}

func TestUDPFrame_Roundtrip(t *testing.T) {
	cases := []UDPFrame{
		{SessionID: 1, Addr: "8.8.8.8:53", Payload: []byte("hello")},
		{SessionID: 2, Addr: "", Payload: []byte("world")},
		{SessionID: 0xFFFFFFFF, Addr: "[::1]:443", Payload: []byte{0x01}},
		{SessionID: 100, Addr: "10.0.0.1:5353", Payload: make([]byte, 1200)},
	}

	for _, c := range cases {
		var buf bytes.Buffer
		if err := WriteUDPFrame(&buf, c.SessionID, c.Addr, c.Payload); err != nil {
			t.Fatalf("WriteUDPFrame: %v", err)
		}
		got, err := ReadUDPFrame(&buf)
		if err != nil {
			t.Fatalf("ReadUDPFrame: %v", err)
		}
		if got.SessionID != c.SessionID {
			t.Errorf("session_id = %d, want %d", got.SessionID, c.SessionID)
		}
		if got.Addr != c.Addr {
			t.Errorf("addr = %q, want %q", got.Addr, c.Addr)
		}
		if !bytes.Equal(got.Payload, c.Payload) {
			t.Errorf("payload length = %d, want %d", len(got.Payload), len(c.Payload))
		}
	}
}

// ============================================================================
// Smux-framed UDP (with frame_len prefix)
// ============================================================================

func TestWriteReadSmuxUDPFrame(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("dns-response")

	err := WriteSmuxUDPFrame(&buf, 5, "8.8.8.8:53", payload)
	if err != nil {
		t.Fatalf("WriteSmuxUDPFrame: %v", err)
	}

	f, err := ReadSmuxUDPFrame(&buf)
	if err != nil {
		t.Fatalf("ReadSmuxUDPFrame: %v", err)
	}
	if f.SessionID != 5 {
		t.Errorf("session_id = %d, want 5", f.SessionID)
	}
	if f.Addr != "8.8.8.8:53" {
		t.Errorf("addr = %q", f.Addr)
	}
	if !bytes.Equal(f.Payload, payload) {
		t.Errorf("payload mismatch")
	}
}

func TestSmuxUDPFrame_MultipleFrames(t *testing.T) {
	var buf bytes.Buffer

	// Write multiple frames to same stream
	WriteSmuxUDPFrame(&buf, 1, "8.8.8.8:53", []byte("query1"))
	WriteSmuxUDPFrame(&buf, 2, "1.1.1.1:53", []byte("query2"))
	WriteSmuxUDPFrame(&buf, 1, "", []byte("query3")) // no addr

	// Read them back in order
	f1, _ := ReadSmuxUDPFrame(&buf)
	f2, _ := ReadSmuxUDPFrame(&buf)
	f3, _ := ReadSmuxUDPFrame(&buf)

	if f1.SessionID != 1 || f1.Addr != "8.8.8.8:53" {
		t.Errorf("frame1: session=%d addr=%q", f1.SessionID, f1.Addr)
	}
	if f2.SessionID != 2 || f2.Addr != "1.1.1.1:53" {
		t.Errorf("frame2: session=%d addr=%q", f2.SessionID, f2.Addr)
	}
	if f3.SessionID != 1 || f3.Addr != "" {
		t.Errorf("frame3: session=%d addr=%q", f3.SessionID, f3.Addr)
	}
}

// ============================================================================
// Session ID Allocator
// ============================================================================

func TestSessionIDAllocator(t *testing.T) {
	alloc := NewSessionIDAllocator()

	id1 := alloc.Next()
	id2 := alloc.Next()
	id3 := alloc.Next()

	if id1 != 1 {
		t.Errorf("first id = %d, want 1", id1)
	}
	if id2 != 2 {
		t.Errorf("second id = %d, want 2", id2)
	}
	if id3 != 3 {
		t.Errorf("third id = %d, want 3", id3)
	}
}

