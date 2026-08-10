package wire

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
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
//
// Every timeout must be excluded here, and identifying one is not as simple as
// comparing against context.DeadlineExceeded. EchoProbe arms the response read
// with stream.SetReadDeadline, and quic-go answers an expired stream deadline
// with its own unexported deadlineError, which unwraps to os.ErrDeadlineExceeded
// — never to context.DeadlineExceeded. Checking only the latter made a
// black-holed data path (handshake passes, Short Header packets dropped) look
// like a pre-echo server, so trust grew, strikes stayed at zero, and neither
// health.go's re-race nor race.go's transport selection ever noticed the wire
// was dead. Because the fail-open default below turns any unrecognized error
// into "benign", a check that misses a real failure mode fails silently: prefer
// over-matching timeouts here. See TestIsEchoUnsupported_RealStreamDeadlineIsNotUnsupported,
// which drives a real quic-go connection rather than a synthetic error value.
func IsEchoUnsupported(err error) bool {
	// Blocked path, not an old server — the wire is genuinely dead.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return false
	}
	// Catch-all for timeouts that carry neither sentinel (quic-go's stream
	// deadline error reports Timeout() through the net.Error interface).
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return false
	}
	// Stream reset or unexpected EOF from server = likely old server.
	return true
}
