package wire

import (
	"bytes"
	"testing"
	"time"
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

// ============================================================================
// Server-side UDP Session Map
// ============================================================================

func TestUDPSessionMap_AddAndGet(t *testing.T) {
	m := NewUDPSessionMap(1024, 60*time.Second)
	defer m.Close()

	if ok := m.Store(1, "8.8.8.8:53"); !ok {
		t.Fatal("Store should succeed")
	}
	addr, ok := m.Load(1)
	if !ok {
		t.Fatal("session 1 should exist")
	}
	if addr != "8.8.8.8:53" {
		t.Errorf("addr = %q, want %q", addr, "8.8.8.8:53")
	}

	// Non-existent session
	_, ok = m.Load(999)
	if ok {
		t.Error("session 999 should not exist")
	}
}

func TestUDPSessionMap_IdleCleanup(t *testing.T) {
	m := NewUDPSessionMap(1024, 50*time.Millisecond)
	defer m.Close()

	if ok := m.Store(1, "8.8.8.8:53"); !ok {
		t.Fatal("Store should succeed")
	}

	// Should exist immediately
	_, ok := m.Load(1)
	if !ok {
		t.Fatal("session should exist")
	}

	time.Sleep(60 * time.Millisecond)
	m.Cleanup()

	_, ok = m.Load(1)
	if ok {
		t.Error("session should be cleaned up after idle timeout")
	}
}

func TestUDPSessionMap_Touch(t *testing.T) {
	m := NewUDPSessionMap(1024, 80*time.Millisecond)
	defer m.Close()

	if ok := m.Store(1, "8.8.8.8:53"); !ok {
		t.Fatal("Store should succeed")
	}

	// Touch before idle timeout
	time.Sleep(40 * time.Millisecond)
	m.Touch(1)

	time.Sleep(50 * time.Millisecond)
	m.Cleanup()

	// Should still exist (touched recently)
	_, ok := m.Load(1)
	if !ok {
		t.Error("session should still exist after touch")
	}
}

// ============================================================================
// MaxSize Enforcement (AC19)
// ============================================================================

func TestUDPSessionMap_MaxSize_Enforced(t *testing.T) {
	m := NewUDPSessionMap(2, 60*time.Second)
	defer m.Close()

	// Store first two sessions - should succeed
	ok1 := m.Store(1, "8.8.8.8:53")
	ok2 := m.Store(2, "1.1.1.1:53")

	if !ok1 {
		t.Error("first Store should succeed")
	}
	if !ok2 {
		t.Error("second Store should succeed")
	}
	if m.Len() != 2 {
		t.Errorf("len = %d, want 2", m.Len())
	}

	// Third store should be rejected (at capacity)
	ok3 := m.Store(3, "9.9.9.9:53")
	if ok3 {
		t.Error("third Store should fail (at maxSize)")
	}
	if m.Len() != 2 {
		t.Errorf("len = %d, want 2 (third session should not be added)", m.Len())
	}

	// Verify session 3 was not added
	_, exists := m.Load(3)
	if exists {
		t.Error("session 3 should not exist")
	}
}

func TestUDPSessionMap_MaxSize_UpdateExisting(t *testing.T) {
	m := NewUDPSessionMap(2, 60*time.Second)
	defer m.Close()

	// Fill to capacity
	if ok := m.Store(1, "8.8.8.8:53"); !ok {
		t.Fatal("first Store should succeed")
	}
	if ok := m.Store(2, "1.1.1.1:53"); !ok {
		t.Fatal("second Store should succeed")
	}

	// Update existing session should succeed even at capacity
	ok := m.Store(1, "8.8.4.4:53")
	if !ok {
		t.Error("updating existing session should succeed at capacity")
	}

	addr, _ := m.Load(1)
	if addr != "8.8.4.4:53" {
		t.Errorf("addr = %q, want %q", addr, "8.8.4.4:53")
	}
	if m.Len() != 2 {
		t.Errorf("len = %d, want 2", m.Len())
	}
}

// ============================================================================
// Background Cleanup (AC20)
// ============================================================================

func TestUDPSessionMap_StartCleanup(t *testing.T) {
	m := NewUDPSessionMap(1024, 50*time.Millisecond)
	defer m.Close()

	// Store session with short idle TTL
	if ok := m.Store(1, "8.8.8.8:53"); !ok {
		t.Fatal("Store should succeed")
	}

	// Start cleanup goroutine with short interval
	m.StartCleanup(100 * time.Millisecond)

	// Session should exist initially
	_, ok := m.Load(1)
	if !ok {
		t.Fatal("session should exist initially")
	}

	// Wait for idle timeout + cleanup interval
	// Session idle at t=0, idle TTL=50ms, cleanup runs at t=100ms
	time.Sleep(200 * time.Millisecond)

	// Session should be removed by background cleanup
	_, ok = m.Load(1)
	if ok {
		t.Error("session should be cleaned up by background goroutine")
	}
}
