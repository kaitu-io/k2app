package wire

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/kaitu-io/kuic"
	smux "github.com/xtaci/smux/v2"
)

// ErrMetadataRejected marks a server's explicit rejection of the metadata
// (device-auth) stream, as opposed to a transport-layer failure. connect()
// wraps the underlying *StreamError / ErrStreamRejected with this sentinel so
// engine.ClassifyError can tell an auth rejection (CategoryClient) apart from
// a proxy-request StreamError (CategoryTarget — see server/handler.go's 403
// for blocked addresses) without string matching. Producer classifies,
// consumer asks — Error Ownership Rules, k2/CLAUDE.md.
var ErrMetadataRejected = errors.New("wire: metadata auth rejected")

// metadataTransport is what sendMetadata needs from a transport: a way to
// open the metadata stream, and whether this transport writes the H3
// frame-type marker (QUIC does; TCP-WS/smux does not — a pre-existing
// protocol difference, not an omission). The returned stream must come with a
// read deadline armed, and its Close must release BOTH directions (QUIC:
// CancelRead + Close — see quicMetaStream).
type metadataTransport interface {
	openMetaStream(ctx context.Context) (io.ReadWriteCloser, error)
	writesH3FrameType() bool
}

// metaReadTimeout bounds the wait for the server's metadata response.
const metaReadTimeout = 5 * time.Second

// sendMetadata is the single metadata-stream orchestration shared by both
// transports. It replaces two hand-copied implementations that had already
// diverged (QUIC returned errors; TCP-WS returned void and dropped ok/errCode
// — the exact drift this merge exists to prevent by construction: both
// transports now call this one function instead of maintaining parallel
// copies).
//
// Error contract:
//   - transport-layer failure (open/write/read timeout) → plain error; the
//     caller treats it as best-effort and must NOT fail connect().
//   - server explicit rejection → *StreamError{Code} (coded 0x02) or an
//     ErrStreamRejected-wrapped error (legacy 0x01); the caller MUST fail
//     connect(), wrapping with ErrMetadataRejected. Check via isMetadataReject.
//
// On success returns the server-issued cookie — "" when the server sent none
// (old server, or Phase 1 servers before cookie issuance lands in Phase 2).
func sendMetadata(ctx context.Context, t metadataTransport, meta MetadataFrame) (resp MetadataFrame, err error) {
	stream, err := t.openMetaStream(ctx)
	if err != nil {
		return MetadataFrame{}, fmt.Errorf("open stream: %w", err)
	}
	defer stream.Close()

	if t.writesH3FrameType() {
		if err := WriteH3ProxyFrameType(stream); err != nil {
			return MetadataFrame{}, fmt.Errorf("write frame type: %w", err)
		}
	}
	if err := WriteStreamHeader(stream, StreamTypeMeta, ""); err != nil {
		return MetadataFrame{}, fmt.Errorf("write stream header: %w", err)
	}
	// A server that rejects us answers and then closes the connection, and
	// the close can reach our write before its success does. The rejection
	// is already buffered on the stream, so read it before trusting a write
	// error: reporting a 402 as a transport failure would let connect() go
	// on as if nothing were wrong.
	werr := WriteMetadataFrame(stream, meta)

	br := bufio.NewReader(stream)
	ok, errCode, errMsg, err := ReadStreamResponse(br)
	if err != nil {
		if werr != nil {
			return MetadataFrame{}, fmt.Errorf("write metadata: %w", werr)
		}
		return MetadataFrame{}, fmt.Errorf("read response: %w", err)
	}
	if !ok {
		if errCode > 0 {
			return MetadataFrame{}, &StreamError{Code: errCode, Message: errMsg}
		}
		return MetadataFrame{}, fmt.Errorf("%w: %s", ErrStreamRejected, errMsg)
	}
	resp, err = ReadMetadataResponse(br)
	if err != nil {
		// Malformed kv after a clean OK: log and treat as "no kv" — the
		// auth verdict (OK) already arrived intact.
		slog.Debug("wire: metadata response kv malformed", "err", err)
		return MetadataFrame{}, nil
	}
	hasCookie := resp.Cookie != ""
	slog.Debug("wire: metadata auth OK", "mode", meta.Mode, "hasCookie", hasCookie)
	return resp, nil
}

// isMetadataReject reports whether a sendMetadata error is the server's
// explicit rejection (0x01/0x02 response) rather than a transport failure.
func isMetadataReject(err error) bool {
	var se *StreamError
	return errors.As(err, &se) || errors.Is(err, ErrStreamRejected)
}

// quicMetaTransport adapts a QUIC connection to metadataTransport.
type quicMetaTransport struct{ conn *quic.Conn }

func (q quicMetaTransport) openMetaStream(ctx context.Context) (io.ReadWriteCloser, error) {
	stream, err := q.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	stream.SetReadDeadline(time.Now().Add(metaReadTimeout))
	return &quicMetaStream{stream}, nil
}
func (q quicMetaTransport) writesH3FrameType() bool { return true }

// quicMetaStream releases BOTH stream directions on Close. quic-go's
// Stream.Close only sends FIN (write side); without CancelRead the receive
// half never completes and the stream slot leaks (wire/CLAUDE.md — the same
// leak quicStreamConn.Close fixes for proxy streams).
type quicMetaStream struct{ *quic.Stream }

func (s *quicMetaStream) Close() error {
	s.CancelRead(0)
	return s.Stream.Close()
}

// smuxMetaTransport adapts a smux session to metadataTransport.
type smuxMetaTransport struct{ sess *smux.Session }

func (m smuxMetaTransport) openMetaStream(ctx context.Context) (io.ReadWriteCloser, error) {
	stream, err := m.sess.OpenStream()
	if err != nil {
		return nil, err
	}
	stream.SetReadDeadline(time.Now().Add(metaReadTimeout))
	return stream, nil
}
func (m smuxMetaTransport) writesH3FrameType() bool { return false }
