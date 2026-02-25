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

// udpFramePool pools buffers for WriteUDPFrame/WriteSmuxUDPFrame to avoid
// per-packet heap allocations on the data plane hot path.
var udpFramePool = sync.Pool{
	New: func() any { b := make([]byte, 0, 4+10+4096); return &b },
}

// smuxReadPool pools buffers for ReadSmuxUDPFrame frame reads.
var smuxReadPool = sync.Pool{
	New: func() any { b := make([]byte, 4096); return &b },
}

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
	bp := udpFramePool.Get().(*[]byte)
	buf := (*bp)[:0]

	// session_id: uint32 BE (fixed offset for fast parsing)
	buf = binary.BigEndian.AppendUint32(buf, sessionID)

	// addr_len: varint
	buf = AppendVarint(buf, uint64(len(addr)))

	// addr (if present) — append from string avoids []byte(addr) allocation
	buf = append(buf, addr...)

	// payload
	buf = append(buf, payload...)

	_, err := w.Write(buf)
	*bp = buf // preserve grown capacity
	udpFramePool.Put(bp)
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

	// payload: everything remaining.
	// When r is a *bytes.Reader (common case from ReadSmuxUDPFrame),
	// use exact-length ReadFull to avoid io.ReadAll's grow-and-copy overhead.
	var payload []byte
	if br, ok := r.(*bytes.Reader); ok {
		if n := br.Len(); n > 0 {
			payload = make([]byte, n)
			if _, err := io.ReadFull(br, payload); err != nil {
				return nil, fmt.Errorf("wire: read UDP payload: %w", err)
			}
		}
	} else {
		var err error
		payload, err = io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("wire: read UDP payload: %w", err)
		}
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
	// Pre-compute inner frame length to write prefix first, avoiding shifts.
	addrVarintLen := varintLen(uint64(len(addr)))
	innerLen := 4 + addrVarintLen + len(addr) + len(payload)

	bp := udpFramePool.Get().(*[]byte)
	buf := (*bp)[:0]

	// frame_len prefix
	buf = AppendVarint(buf, uint64(innerLen))

	// inner frame: session_id + addr_len + addr + payload
	buf = binary.BigEndian.AppendUint32(buf, sessionID)
	buf = AppendVarint(buf, uint64(len(addr)))
	buf = append(buf, addr...)
	buf = append(buf, payload...)

	_, err := w.Write(buf)
	*bp = buf
	udpFramePool.Put(bp)
	return err
}

// varintLen returns the encoded byte length of a QUIC varint value.
func varintLen(v uint64) int {
	switch {
	case v <= 63:
		return 1
	case v <= 16383:
		return 2
	case v <= 1073741823:
		return 4
	default:
		return 8
	}
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

	// Read exactly frame_len bytes into pooled buffer.
	// frameBuf is safe to pool: ReadUDPFrame's io.ReadAll creates an
	// independent payload slice, so frameBuf has no external references.
	var pooled bool
	var bp *[]byte
	var frameBuf []byte
	if frameLen <= 4096 {
		bp = smuxReadPool.Get().(*[]byte)
		frameBuf = (*bp)[:frameLen]
		pooled = true
	} else {
		frameBuf = make([]byte, frameLen)
	}

	if _, err := io.ReadFull(r, frameBuf); err != nil {
		if pooled {
			smuxReadPool.Put(bp)
		}
		return nil, fmt.Errorf("wire: read smux UDP frame: %w", err)
	}

	result, err := ReadUDPFrame(bytes.NewReader(frameBuf))
	if pooled {
		smuxReadPool.Put(bp)
	}
	return result, err
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
	mu         sync.RWMutex
	sessions   map[uint32]*udpSession
	maxSize    int
	idleTTL    time.Duration
	stopCleanup chan struct{}
}

// NewUDPSessionMap creates a session map with max size and idle timeout.
func NewUDPSessionMap(maxSize int, idleTTL time.Duration) *UDPSessionMap {
	return &UDPSessionMap{
		sessions:    make(map[uint32]*udpSession),
		maxSize:     maxSize,
		idleTTL:     idleTTL,
		stopCleanup: make(chan struct{}),
	}
}

// Store adds or updates a session.
// Returns false if at maxSize capacity and the session is new (not an update).
func (m *UDPSessionMap) Store(sessionID uint32, addr string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if this is an update to existing session or a new one
	_, exists := m.sessions[sessionID]

	// Reject new sessions when at capacity
	if !exists && len(m.sessions) >= m.maxSize {
		return false
	}

	m.sessions[sessionID] = &udpSession{
		addr:     addr,
		lastSeen: time.Now(),
	}
	return true
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

// StartCleanup launches a background goroutine that periodically removes idle sessions.
// The cleanup goroutine stops when Close() is called.
func (m *UDPSessionMap) StartCleanup(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				m.Cleanup()
			case <-m.stopCleanup:
				return
			}
		}
	}()
}

// Len returns the number of active sessions.
func (m *UDPSessionMap) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Close cleans up all sessions and stops the cleanup goroutine.
func (m *UDPSessionMap) Close() {
	// Stop cleanup goroutine
	select {
	case <-m.stopCleanup:
		// Already closed
	default:
		close(m.stopCleanup)
	}

	// Clear all sessions
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = make(map[uint32]*udpSession)
}
