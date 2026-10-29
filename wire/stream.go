package wire

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// streamHeaderPool pools small buffers for WriteStreamHeader/WriteStreamResponse.
var streamHeaderPool = sync.Pool{
	New: func() any { b := make([]byte, 0, 300); return &b },
}

// Stream type constants.
const (
	StreamTypeTCP         byte = 0x01
	StreamTypeUDP         byte = 0x02
	StreamTypeUDPOverflow byte = 0x03
	StreamTypeEcho        byte = 0x04
	StreamTypeMeta        byte = 0x10 // connection metadata (auth + mode)
)

// Limits for varint-decoded lengths to prevent OOM from malicious input.
const (
	maxAddrLen     = 4096           // max host:port string length
	maxErrorMsgLen = 8192           // max error message length
	maxFrameLen    = 16 * 1024 * 1024 // 16MB max smux UDP frame
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

// streamHeaderTimeout bounds how long a server waits for a new stream's
// header (and, for a metadata stream, its metadata frame). Streams can be
// opened before the peer authenticates, and without a bound a peer that
// opens streams and sends nothing pins a goroutine for each until the
// connection dies. A k2 client writes the header right after opening.
// var, not const, so tests can shorten it.
var streamHeaderTimeout = 10 * time.Second

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
	if addrLen > maxAddrLen {
		return 0, "", fmt.Errorf("wire: addr_len %d exceeds max %d", addrLen, maxAddrLen)
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
// If respErr is a *StreamError with Code > 0, writes the 0x02 coded format.
// Otherwise writes the legacy 0x01 format.
func WriteStreamResponse(w io.Writer, respErr error) error {
	if respErr == nil {
		_, err := w.Write([]byte{0x00})
		return err
	}

	bp := streamHeaderPool.Get().(*[]byte)
	buf := (*bp)[:0]

	var se *StreamError
	if errors.As(respErr, &se) && se.Code > 0 {
		// 0x02: error with numeric code
		buf = append(buf, 0x02)
		buf = AppendVarint(buf, uint64(se.Code))
		buf = AppendVarint(buf, uint64(len(se.Message)))
		buf = append(buf, se.Message...)
	} else {
		// 0x01: legacy error (message only)
		errMsg := respErr.Error()
		buf = append(buf, 0x01)
		buf = AppendVarint(buf, uint64(len(errMsg)))
		buf = append(buf, errMsg...)
	}

	_, err := w.Write(buf)
	*bp = buf
	streamHeaderPool.Put(bp)
	return err
}

// ReadStreamResponse reads a TCP stream response.
// Returns (true, 0, "", nil) on success, or (false, code, errMsg, nil) on server error.
// code is 0 for legacy 0x01 format, or the server-provided code for 0x02 format.
func ReadStreamResponse(r interface {
	io.Reader
	io.ByteReader
}) (ok bool, errCode int, errMsg string, err error) {
	status, err := r.ReadByte()
	if err != nil {
		return false, 0, "", fmt.Errorf("wire: read response status: %w", err)
	}

	switch status {
	case 0x00:
		return true, 0, "", nil

	case 0x01:
		// Legacy error: message only
		errLen, _, err := ReadVarint(r)
		if err != nil {
			return false, 0, "", fmt.Errorf("wire: read error length: %w", err)
		}
		if errLen > maxErrorMsgLen {
			return false, 0, "", fmt.Errorf("wire: error length %d exceeds max %d", errLen, maxErrorMsgLen)
		}
		msgBuf := make([]byte, errLen)
		if _, err := io.ReadFull(r, msgBuf); err != nil {
			return false, 0, "", fmt.Errorf("wire: read error message: %w", err)
		}
		return false, 0, string(msgBuf), nil

	case 0x02:
		// Coded error: [code:varint][err_len:varint][err_msg]
		code, _, err := ReadVarint(r)
		if err != nil {
			return false, 0, "", fmt.Errorf("wire: read error code: %w", err)
		}
		errLen, _, err := ReadVarint(r)
		if err != nil {
			return false, 0, "", fmt.Errorf("wire: read error length: %w", err)
		}
		if errLen > maxErrorMsgLen {
			return false, 0, "", fmt.Errorf("wire: error length %d exceeds max %d", errLen, maxErrorMsgLen)
		}
		msgBuf := make([]byte, errLen)
		if _, err := io.ReadFull(r, msgBuf); err != nil {
			return false, 0, "", fmt.Errorf("wire: read error message: %w", err)
		}
		return false, int(code), string(msgBuf), nil

	default:
		return false, 0, "", fmt.Errorf("wire: unknown response status 0x%02x", status)
	}
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

// StreamError is a server rejection with a numeric error code.
// Used by ClassifyError to extract server-provided codes without pattern matching.
type StreamError struct {
	Code    int
	Message string
}

func (e *StreamError) Error() string      { return e.Message }
func (e *StreamError) IsProxyLayer() bool  { return true }

// ============================================================================
// Metadata Frame: [StreamType:0x10][field_count:varint] { [key_len:varint][key][val_len:varint][val] }...
// ============================================================================

// MetadataFrame holds key-value metadata for a connection (auth + mode).
type MetadataFrame struct {
	UDID    string
	Token   string
	Mode    string // "gateway" or "" (normal)
	Version string // client app version (e.g. "0.4.8"); "" = legacy/unknown. Observation only — never an auth-exemption signal (self-reported, spoofable).
	Cookie  string // node-issued connection cookie (Phase 2); kv key "cookie". Empty = cold auth.
	// SmuxHalfClose: the sender's smux keeps a stream readable after its
	// first FIN (third_party/smux K2 patch 2); kv key "smux_hc" = "1". Sent
	// by TCP-WS clients in the request and TCP-WS servers in the response;
	// each side enables half-close only for a peer that advertised it.
	SmuxHalfClose bool
}

// WriteMetadataFrame writes a metadata frame payload to w.
// The StreamType byte (0x10) is NOT included — it's written by WriteStreamHeader.
// Format: [field_count:varint] then for each field: [key_len:varint][key][val_len:varint][val]
func WriteMetadataFrame(w io.Writer, meta MetadataFrame) error {
	bp := streamHeaderPool.Get().(*[]byte)
	buf := (*bp)[:0]

	fields := [][2]string{
		{"udid", meta.UDID},
		{"token", meta.Token},
	}
	if meta.Mode != "" {
		fields = append(fields, [2]string{"mode", meta.Mode})
	}
	if meta.Version != "" {
		fields = append(fields, [2]string{"version", meta.Version})
	}
	if meta.Cookie != "" {
		fields = append(fields, [2]string{"cookie", meta.Cookie})
	}
	if meta.SmuxHalfClose {
		fields = append(fields, [2]string{"smux_hc", "1"})
	}

	buf = AppendVarint(buf, uint64(len(fields)))
	for _, f := range fields {
		buf = AppendVarint(buf, uint64(len(f[0])))
		buf = append(buf, f[0]...)
		buf = AppendVarint(buf, uint64(len(f[1])))
		buf = append(buf, f[1]...)
	}

	_, err := w.Write(buf)
	*bp = buf
	streamHeaderPool.Put(bp)
	return err
}

// ReadMetadataFrame reads a metadata frame from r.
// The StreamType byte (0x10) must have already been consumed by the caller.
func ReadMetadataFrame(r interface {
	io.Reader
	io.ByteReader
}) (MetadataFrame, error) {
	fieldCount, _, err := ReadVarint(r)
	if err != nil {
		return MetadataFrame{}, fmt.Errorf("wire: read metadata field count: %w", err)
	}
	if fieldCount > 16 {
		return MetadataFrame{}, fmt.Errorf("wire: metadata field count %d exceeds max 16", fieldCount)
	}
	return readMetadataFields(r, fieldCount)
}

// readMetadataFields reads fieldCount [key][val] pairs. Shared by
// ReadMetadataFrame (request) and ReadMetadataResponse (response kv).
func readMetadataFields(r interface {
	io.Reader
	io.ByteReader
}, fieldCount uint64) (MetadataFrame, error) {
	var meta MetadataFrame
	for i := uint64(0); i < fieldCount; i++ {
		keyLen, _, err := ReadVarint(r)
		if err != nil {
			return MetadataFrame{}, fmt.Errorf("wire: read metadata key length: %w", err)
		}
		if keyLen > 256 {
			return MetadataFrame{}, fmt.Errorf("wire: metadata key length %d exceeds max", keyLen)
		}
		keyBuf := make([]byte, keyLen)
		if _, err := io.ReadFull(r, keyBuf); err != nil {
			return MetadataFrame{}, fmt.Errorf("wire: read metadata key: %w", err)
		}

		valLen, _, err := ReadVarint(r)
		if err != nil {
			return MetadataFrame{}, fmt.Errorf("wire: read metadata value length: %w", err)
		}
		if valLen > 4096 {
			return MetadataFrame{}, fmt.Errorf("wire: metadata value length %d exceeds max", valLen)
		}
		valBuf := make([]byte, valLen)
		if _, err := io.ReadFull(r, valBuf); err != nil {
			return MetadataFrame{}, fmt.Errorf("wire: read metadata value: %w", err)
		}

		switch string(keyBuf) {
		case "udid":
			meta.UDID = string(valBuf)
		case "token":
			meta.Token = string(valBuf)
		case "mode":
			meta.Mode = string(valBuf)
		case "version":
			meta.Version = string(valBuf)
		case "cookie":
			meta.Cookie = string(valBuf)
		case "smux_hc":
			meta.SmuxHalfClose = string(valBuf) == "1"
		}
	}
	return meta, nil
}

// ============================================================================
// Metadata Stream Response: [0x00][MetadataFrame kv]  (success)
//                           [0x01]/[0x02]…            (rejection — WriteStreamResponse)
// ============================================================================

// WriteMetadataResponse writes a successful metadata-stream response: the
// 0x00 status byte followed by a MetadataFrame-format kv (cookie and/or
// smux_hc, or an empty kv when there is neither). 0x02 is already taken by the coded
// error format, so success-with-payload rides BEHIND 0x00 instead of using a
// new status byte. Old clients read the single 0x00 and Close(): on QUIC the
// unread kv stays in the receive buffer until the stream is released; on
// smux Close() discards it. Neither surfaces an error — pinned by
// TestMetadataResponse_OldClientSafe_{QUIC,TCPWS}.
func WriteMetadataResponse(w io.Writer, cookie string, smuxHalfClose bool) error {
	var fields [][2]string
	if cookie != "" {
		fields = append(fields, [2]string{"cookie", cookie})
	}
	if smuxHalfClose {
		fields = append(fields, [2]string{"smux_hc", "1"})
	}
	buf := []byte{0x00}
	buf = AppendVarint(buf, uint64(len(fields)))
	for _, f := range fields {
		buf = AppendVarint(buf, uint64(len(f[0])))
		buf = append(buf, f[0]...)
		buf = AppendVarint(buf, uint64(len(f[1])))
		buf = append(buf, f[1]...)
	}
	_, err := w.Write(buf)
	return err
}

// ReadMetadataResponse reads the kv payload that follows a 0x00 success
// status. The caller must already have consumed the status byte (via
// ReadStreamResponse). An old server sends a bare [0x00] and closes, so ANY
// failure to read the leading field count (EOF, reset, deadline) means
// "no kv" and returns a zero MetadataFrame. A malformed kv after a successful
// field-count read is a protocol error. Only Cookie and SmuxHalfClose are
// meaningful in a response.
func ReadMetadataResponse(r interface {
	io.Reader
	io.ByteReader
}) (MetadataFrame, error) {
	fieldCount, _, err := ReadVarint(r)
	if err != nil {
		return MetadataFrame{}, nil // old server: nothing after the bare [0x00]
	}
	if fieldCount > 16 {
		return MetadataFrame{}, fmt.Errorf("wire: metadata response field count %d exceeds max 16", fieldCount)
	}
	return readMetadataFields(r, fieldCount)
}
