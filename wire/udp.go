package wire

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// UDPFrame represents a single UDP message frame.
// Used on all UDP paths (QUIC datagram, smux stream, QUIC stream).
type UDPFrame struct {
	SessionID uint32
	Addr      string // empty if addr_len=0
	Payload   []byte
}

// ============================================================================
// UDP Message Frame: [session_id:4 BE][addr_len:varint][addr][payload]
// ============================================================================

// WriteUDPFrame writes a UDP message frame to w.
// If addr is empty, addr_len is written as 0 (subsequent packet optimization).
// payload_length is implicit (= remaining bytes in datagram, or frame_len - header for smux).
func WriteUDPFrame(w io.Writer, sessionID uint32, addr string, payload []byte) error {
	addrBytes := []byte(addr)
	buf := make([]byte, 0, 4+2+len(addrBytes)+len(payload))

	// session_id: uint32 BE (fixed offset for fast parsing)
	buf = binary.BigEndian.AppendUint32(buf, sessionID)

	// addr_len: varint
	buf = AppendVarint(buf, uint64(len(addrBytes)))

	// addr (if present)
	buf = append(buf, addrBytes...)

	// payload
	buf = append(buf, payload...)

	_, err := w.Write(buf)
	return err
}

// ReadUDPFrame reads a UDP message frame from a datagram (known-length) or buffer.
// For QUIC datagrams, pass the full datagram bytes as a bytes.Buffer.
// For smux/stream, the caller must first read the frame_len prefix.
func ReadUDPFrame(r interface {
	io.Reader
	io.ByteReader
}) (*UDPFrame, error) {
	// session_id: 4 bytes BE
	var sidBuf [4]byte
	if _, err := io.ReadFull(r, sidBuf[:]); err != nil {
		return nil, fmt.Errorf("wire: read UDP session_id: %w", err)
	}
	sessionID := binary.BigEndian.Uint32(sidBuf[:])

	// addr_len: varint
	addrLen, _, err := ReadVarint(r)
	if err != nil {
		return nil, fmt.Errorf("wire: read UDP addr_len: %w", err)
	}

	// addr (if present)
	var addr string
	if addrLen > 0 {
		addrBuf := make([]byte, addrLen)
		if _, err := io.ReadFull(r, addrBuf); err != nil {
			return nil, fmt.Errorf("wire: read UDP addr: %w", err)
		}
		addr = string(addrBuf)
	}

	// payload: everything remaining
	payload, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("wire: read UDP payload: %w", err)
	}

	return &UDPFrame{
		SessionID: sessionID,
		Addr:      addr,
		Payload:   payload,
	}, nil
}

// ============================================================================
// Smux-framed UDP: [frame_len:varint][UDP message frame]
// ============================================================================

// WriteSmuxUDPFrame writes a length-prefixed UDP frame for smux byte streams.
func WriteSmuxUDPFrame(w io.Writer, sessionID uint32, addr string, payload []byte) error {
	// Build the inner frame first to calculate length
	addrBytes := []byte(addr)
	inner := make([]byte, 0, 4+2+len(addrBytes)+len(payload))
	inner = binary.BigEndian.AppendUint32(inner, sessionID)
	inner = AppendVarint(inner, uint64(len(addrBytes)))
	inner = append(inner, addrBytes...)
	inner = append(inner, payload...)

	// Write frame_len prefix + inner
	buf := AppendVarint(nil, uint64(len(inner)))
	buf = append(buf, inner...)
	_, err := w.Write(buf)
	return err
}

// ReadSmuxUDPFrame reads a length-prefixed UDP frame from a smux byte stream.
func ReadSmuxUDPFrame(r interface {
	io.Reader
	io.ByteReader
}) (*UDPFrame, error) {
	// frame_len: varint
	frameLen, _, err := ReadVarint(r)
	if err != nil {
		return nil, fmt.Errorf("wire: read smux UDP frame_len: %w", err)
	}

	// Read exactly frame_len bytes
	frameBuf := make([]byte, frameLen)
	if _, err := io.ReadFull(r, frameBuf); err != nil {
		return nil, fmt.Errorf("wire: read smux UDP frame: %w", err)
	}

	return ReadUDPFrame(bytes.NewReader(frameBuf))
}

// ============================================================================
// Session ID Allocator (client-side, sequential uint32)
// ============================================================================

// SessionIDAllocator generates sequential session IDs per connection.
type SessionIDAllocator struct {
	next atomic.Uint32
}

// NewSessionIDAllocator creates a new allocator starting at 1.
func NewSessionIDAllocator() *SessionIDAllocator {
	return &SessionIDAllocator{}
}

// Next returns the next session ID.
func (a *SessionIDAllocator) Next() uint32 {
	return a.next.Add(1)
}

// ============================================================================
// Server-side UDP Session Map
// ============================================================================

type udpSession struct {
	addr     string
	lastSeen time.Time
}

// UDPSessionMap manages server-side UDP session state.
type UDPSessionMap struct {
	mu       sync.RWMutex
	sessions map[uint32]*udpSession
	maxSize  int
	idleTTL  time.Duration
}

// NewUDPSessionMap creates a session map with max size and idle timeout.
func NewUDPSessionMap(maxSize int, idleTTL time.Duration) *UDPSessionMap {
	return &UDPSessionMap{
		sessions: make(map[uint32]*udpSession),
		maxSize:  maxSize,
		idleTTL:  idleTTL,
	}
}

// Store adds or updates a session.
func (m *UDPSessionMap) Store(sessionID uint32, addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionID] = &udpSession{
		addr:     addr,
		lastSeen: time.Now(),
	}
}

// Load retrieves a session's address. Does not update last-seen time; use Touch for that.
func (m *UDPSessionMap) Load(sessionID uint32) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return "", false
	}
	return s.addr, true
}

// Touch updates the last-seen time for a session.
func (m *UDPSessionMap) Touch(sessionID uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[sessionID]; ok {
		s.lastSeen = time.Now()
	}
}

// Cleanup removes sessions that have been idle longer than idleTTL.
func (m *UDPSessionMap) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-m.idleTTL)
	for id, s := range m.sessions {
		if s.lastSeen.Before(cutoff) {
			delete(m.sessions, id)
		}
	}
}

// Len returns the number of active sessions.
func (m *UDPSessionMap) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Close cleans up all sessions.
func (m *UDPSessionMap) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = make(map[uint32]*udpSession)
}
