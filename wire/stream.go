package wire

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

// streamHeaderPool pools small buffers for WriteStreamHeader/WriteStreamResponse.
var streamHeaderPool = sync.Pool{
	New: func() any { b := make([]byte, 0, 300); return &b },
}

// Stream type constants.
const (
	StreamTypeTCP byte = 0x01
	StreamTypeUDP byte = 0x02
)

// H3FrameTypeK2Proxy is the custom H3 frame type for k2v5 proxy streams.
// "K2" in ASCII = 0x4B 0x32.
const H3FrameTypeK2Proxy uint64 = 0x4B32

// ============================================================================
// QUIC Varint Encoding (RFC 9000 §16)
// ============================================================================

// AppendVarint appends a QUIC varint-encoded value to buf and returns the result.
func AppendVarint(buf []byte, v uint64) []byte {
	switch {
	case v <= 63:
		return append(buf, byte(v))
	case v <= 16383:
		return append(buf, byte(0x40|v>>8), byte(v))
	case v <= 1073741823:
		return append(buf, byte(0x80|v>>24), byte(v>>16), byte(v>>8), byte(v))
	default:
		return append(buf,
			byte(0xc0|v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
			byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
	}
}

// ReadVarint reads a QUIC varint from r. Returns value, bytes consumed, and error.
func ReadVarint(r io.ByteReader) (uint64, int, error) {
	first, err := r.ReadByte()
	if err != nil {
		return 0, 0, err
	}

	prefix := first >> 6
	length := 1 << prefix // 1, 2, 4, or 8 bytes
	v := uint64(first & 0x3f)

	for i := 1; i < length; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, 0, fmt.Errorf("wire: varint truncated at byte %d: %w", i, err)
		}
		v = v<<8 | uint64(b)
	}

	return v, length, nil
}

// ============================================================================
// Stream Header: [type:1][addr_len:varint][addr]
// ============================================================================

// WriteStreamHeader writes a stream header to w.
func WriteStreamHeader(w io.Writer, streamType byte, addr string) error {
	bp := streamHeaderPool.Get().(*[]byte)
	buf := (*bp)[:0]
	buf = append(buf, streamType)
	buf = AppendVarint(buf, uint64(len(addr)))
	buf = append(buf, addr...)
	_, err := w.Write(buf)
	*bp = buf
	streamHeaderPool.Put(bp)
	return err
}

// ReadStreamHeader reads a stream header from r.
// r must implement io.Reader and io.ByteReader (bytes.Reader, bufio.Reader, etc.)
func ReadStreamHeader(r interface {
	io.Reader
	io.ByteReader
}) (streamType byte, addr string, err error) {
	streamType, err = r.ReadByte()
	if err != nil {
		return 0, "", fmt.Errorf("wire: read stream type: %w", err)
	}

	addrLen, _, err := ReadVarint(r)
	if err != nil {
		return 0, "", fmt.Errorf("wire: read addr_len: %w", err)
	}

	addrBuf := make([]byte, addrLen)
	if _, err := io.ReadFull(r, addrBuf); err != nil {
		return 0, "", fmt.Errorf("wire: read addr: %w", err)
	}

	return streamType, string(addrBuf), nil
}

// ============================================================================
// TCP Stream Response: [0x00] OK or [0x01][err_len:varint][err_msg]
// ============================================================================

// WriteStreamResponse writes a TCP stream response (success or error).
func WriteStreamResponse(w io.Writer, respErr error) error {
	if respErr == nil {
		_, err := w.Write([]byte{0x00})
		return err
	}

	errMsg := respErr.Error()
	bp := streamHeaderPool.Get().(*[]byte)
	buf := (*bp)[:0]
	buf = append(buf, 0x01)
	buf = AppendVarint(buf, uint64(len(errMsg)))
	buf = append(buf, errMsg...)
	_, err := w.Write(buf)
	*bp = buf
	streamHeaderPool.Put(bp)
	return err
}

// ReadStreamResponse reads a TCP stream response.
// Returns (true, "", nil) on success or (false, errMsg, nil) on server error.
func ReadStreamResponse(r interface {
	io.Reader
	io.ByteReader
}) (ok bool, errMsg string, err error) {
	status, err := r.ReadByte()
	if err != nil {
		return false, "", fmt.Errorf("wire: read response status: %w", err)
	}

	if status == 0x00 {
		return true, "", nil
	}

	if status != 0x01 {
		return false, "", fmt.Errorf("wire: unknown response status 0x%02x", status)
	}

	errLen, _, err := ReadVarint(r)
	if err != nil {
		return false, "", fmt.Errorf("wire: read error length: %w", err)
	}

	msgBuf := make([]byte, errLen)
	if _, err := io.ReadFull(r, msgBuf); err != nil {
		return false, "", fmt.Errorf("wire: read error message: %w", err)
	}

	return false, string(msgBuf), nil
}

// ============================================================================
// H3 Custom Frame Type (0x4B32)
// ============================================================================

// WriteH3ProxyFrameType writes the k2v5 custom H3 frame type marker.
// Used by the client when opening an H3 proxy stream on QUIC.
func WriteH3ProxyFrameType(w io.Writer) error {
	buf := AppendVarint(nil, H3FrameTypeK2Proxy)
	_, err := w.Write(buf)
	return err
}

// ErrStreamRejected is returned when the server rejects a stream request.
var ErrStreamRejected = errors.New("wire: stream rejected by server")
