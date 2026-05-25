package wire

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/apernet/quic-go"
)

const echoNonceLen = 8

// EchoProber is an optional capability interface for transports that need
// data-path verification (QUIC). TCPWSClient does not implement this.
// Engine type-asserts on this interface (not concrete type) per layer boundary rules.
type EchoProber interface {
	RunEchoProbe(ctx context.Context) error
}

// EchoProbe verifies the QUIC data path (Short Header packets) by opening a
// stream, sending a random nonce, and reading the echo response. Returns the
// round-trip time on success.
func EchoProbe(ctx context.Context, conn *quic.Conn) (time.Duration, error) {
	start := time.Now()

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return 0, fmt.Errorf("echo: open stream: %w", err)
	}
	defer func() {
		stream.CancelRead(0)
		stream.Close()
	}()

	// Write H3 frame type marker (consistent with DialTCP).
	if err := WriteH3ProxyFrameType(stream); err != nil {
		return 0, fmt.Errorf("echo: write H3 frame: %w", err)
	}

	// Write stream header: StreamTypeEcho + empty addr.
	if err := WriteStreamHeader(stream, StreamTypeEcho, ""); err != nil {
		return 0, fmt.Errorf("echo: write header: %w", err)
	}

	// Write random nonce.
	var nonce [echoNonceLen]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return 0, fmt.Errorf("echo: generate nonce: %w", err)
	}
	if _, err := stream.Write(nonce[:]); err != nil {
		return 0, fmt.Errorf("echo: write nonce: %w", err)
	}

	// Set read deadline from context so reads respect cancellation.
	// QUIC stream reads don't cancel via context — only via deadline.
	if deadline, ok := ctx.Deadline(); ok {
		stream.SetReadDeadline(deadline)
	}

	// Read echo response.
	var resp [echoNonceLen]byte
	if _, err := io.ReadFull(stream, resp[:]); err != nil {
		return 0, fmt.Errorf("echo: read response: %w", err)
	}

	if nonce != resp {
		return 0, errors.New("echo: nonce mismatch")
	}

	rtt := time.Since(start)
	slog.Debug("DIAG: echo-probe-ok", "rttMs", rtt.Milliseconds())
	return rtt, nil
}

// HandleEchoStream handles a server-side echo probe stream.
// Reads echoNonceLen bytes from br (buffered reader) and writes them back to stream.
func HandleEchoStream(stream *quic.Stream, br *bufio.Reader) {
	defer func() {
		stream.CancelRead(0)
		stream.Close()
	}()

	var buf [echoNonceLen]byte
	if _, err := io.ReadFull(br, buf[:]); err != nil {
		slog.Debug("echo: read failed", "err", err)
		return
	}
	if _, err := stream.Write(buf[:]); err != nil {
		slog.Debug("echo: write failed", "err", err)
		return
	}
}

// IsEchoUnsupported returns true if the error indicates the server doesn't
// support echo (old server). Used for backward compatibility degradation.
//
// Deliberately broad (P3, not fixed): treats all non-timeout errors as "old
// server" — including theoretical nonce mismatch. This is correct in practice:
// QUIC encryption prevents data tampering, so the only realistic non-timeout
// errors are stream reset / EOF from servers without echo handler. Narrowing
// to explicit error types would add quic-go type coupling for zero real gain.
//
// Exported so engine/health.go can distinguish "wire dead" from "server can't
// validate"; only the former should accumulate strikes toward rerace.
func IsEchoUnsupported(err error) bool {
	// Old servers close the stream with "unknown stream type" or similar.
	// A context.DeadlineExceeded means timeout (blocked path, not old server).
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Stream reset or unexpected EOF from server = likely old server.
	return true
}
