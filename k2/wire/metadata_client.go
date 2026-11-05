package wire

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
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

// ErrMetadataTimeout marks a metadata response that never arrived although
// the transport handshake to the node completed: the node is alive, its auth
// verdict is stuck — typically k2s waiting on an unreachable / hanging Center
// (PVE S15). Producer-classified like ErrMetadataRejected, and it fails
// connect() the same way, but it deliberately does NOT wrap
// ErrMetadataRejected: ClassifyError maps a code-less reject to 401 at
// initial connect, and a stuck Center is not this device's credentials. In
// the replace loop it is treated exactly like a code-less reject (engine
// isCodeLessReject); user-dial evidence records no opinion (dial_failure.go).
var ErrMetadataTimeout = errors.New("wire: metadata response timed out (node reached, auth verdict pending)")

// metaReadTimeout bounds the wait for the server's metadata response. It must
// outlive k2s's own remote-auth timeout (auth_remote.go remoteAuthClient, 5s)
// with margin, so a node that gives up on Center answers its code-less reject
// before we stop listening (PVE S15: both were 5s and the client lost by 2ms).
// A var only so tests can shorten it.
var metaReadTimeout = 8 * time.Second

// transportUpKey carries RaceTransport's per-candidate hook: connect() calls
// it once the transport handshake is done and only the metadata exchange is
// left, so the race's transport deadline stops applying (race.go).
type transportUpKey struct{}

func withTransportUpHook(ctx context.Context, fn func()) context.Context {
	return context.WithValue(ctx, transportUpKey{}, fn)
}

// transportUp signals the hook, if any (lazy user-dial connects have none).
func transportUp(ctx context.Context) {
	if fn, ok := ctx.Value(transportUpKey{}).(func()); ok && fn != nil {
		fn()
	}
}

// isMetadataTimeout reports a sendMetadata error that is the response read
// hitting its deadline (ErrMetadataTimeout).
func isMetadataTimeout(err error) bool { return errors.Is(err, ErrMetadataTimeout) }

// sendMetadata is the single metadata-stream orchestration shared by both
// transports. It replaces two hand-copied implementations that had already
// diverged (QUIC returned errors; TCP-WS returned void and dropped ok/errCode
// — the exact drift this merge exists to prevent by construction: both
// transports now call this one function instead of maintaining parallel
// copies).
//
// Error contract:
//   - transport-layer failure (open/write, a read that ends without an
//     answer) → plain error; the caller treats it as best-effort and must
//     NOT fail connect().
//   - the response read hitting metaReadTimeout after a successful write →
//     ErrMetadataTimeout; the caller MUST fail connect() (ProxyLayerError):
//     the node answered the handshake and is stuck on its auth verdict.
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
		// quic-go / net deadlines report net.Error.Timeout(); smux's stream
		// deadline returns its plain smux.ErrTimeout.
		var nErr net.Error
		if (errors.As(err, &nErr) && nErr.Timeout()) || errors.Is(err, smux.ErrTimeout) {
			// The request went out and the node never answered within
			// metaReadTimeout: alive, verdict stuck (ErrMetadataTimeout).
			return MetadataFrame{}, fmt.Errorf("%w: read response: %w", ErrMetadataTimeout, err)
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
