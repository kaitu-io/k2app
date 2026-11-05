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

	"github.com/kaitu-io/kuic"
	smux "github.com/xtaci/smux/v2"
)

const echoNonceLen = 8

// EchoProber is an optional capability interface for transports that need
// data-path verification. Implemented by QUICClient (quic.go's EchoProbe)
// and TCPWSClient (tcpws.go's RunEchoProbe). Engine type-asserts on this
// interface (not concrete type) per layer boundary rules.
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

// handleEcho reads echoNonceLen bytes from br and reflects them to w.
// Shared by QUIC (HandleEchoStream) and TCP-WS (handleStream StreamTypeEcho).
func handleEcho(w io.Writer, br *bufio.Reader) {
	var buf [echoNonceLen]byte
	if _, err := io.ReadFull(br, buf[:]); err != nil {
		slog.Debug("echo: read failed", "err", err)
		return
	}
	if _, err := w.Write(buf[:]); err != nil {
		slog.Debug("echo: write failed", "err", err)
	}
}

// HandleEchoStream handles a server-side echo probe stream.
// Reads echoNonceLen bytes from br (buffered reader) and writes them back to stream.
func HandleEchoStream(stream *quic.Stream, br *bufio.Reader) {
	defer func() {
		stream.CancelRead(0)
		stream.Close()
	}()
	handleEcho(stream, br)
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
	// Never reached a live connection: no verdict about the server at all.
	if errors.Is(err, ErrEchoNoConnection) {
		return false
	}
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
	// smux (the TCP-WS leg) answers an expired stream read/write deadline
	// with smux.ErrTimeout — a bare errors.New("timeout") carrying neither
	// sentinel above nor a Timeout() method. Missing it made a dead tcpws data
	// path read as "pre-echo server": no strike, no re-race, ~70s to notice
	// (2026-11-05 field logs). TestTCPWSEcho_RealSmuxReadTimeoutIsFailure.
	if errors.Is(err, smux.ErrTimeout) {
		return false
	}
	// Stream reset or unexpected EOF from server = likely old server.
	return true
}

// ErrEchoNoConnection marks an echo probe that never ran on a live
// connection: the client had no cached connection (Reset / ResetConnections
// drop it and the next dial rebuilds it lazily), the cached one was already
// closed, or a tcpws connect failed with a non-timeout error. Such a probe
// says nothing about the server's echo support or about the data path, so it
// is neither "unsupported" nor a failure on its own — engine's echoStep gives
// it the unsupported arithmetic and RecordEchoResult records nothing. It
// strikes only when user dials are failing to connect at the same time
// (TransportManager.ConnectFailures, dial_failure.go; engine echoStepDial).
//
// Before this marker every such error fell through IsEchoUnsupported's
// fail-open default: on Android the connectivity change raised right after
// the VPN comes up resets the transport, the 1s probe found no connection,
// and `DIAG: echo-probe-unsupported` fired on almost every connect against
// servers that do implement echo — growing trust and zeroing the strike
// counter each time.
var ErrEchoNoConnection = errors.New("echo: no live connection")

// IsEchoNotProbed reports whether err is an ErrEchoNoConnection — the probe
// never reached a live connection. Callers check it before IsEchoUnsupported.
func IsEchoNotProbed(err error) bool {
	return errors.Is(err, ErrEchoNoConnection)
}

// isQUICConnClosedErr reports whether err is a QUIC CONNECTION-level close
// (the whole connection is gone), as opposed to a stream-level outcome. A
// pre-echo server only ever ends the echo STREAM — handleStream's `default:`
// calls stream.Close(), which the client reads as EOF — and never closes the
// connection over an unknown stream type, so none of these can be its
// signature. *quic.StreamError (a stream reset) is deliberately absent.
// IdleTimeoutError and HandshakeTimeoutError are listed for completeness but
// report Timeout(), so echoNotProbed still passes them through as failures.
func isQUICConnClosedErr(err error) bool {
	var (
		appErr       *quic.ApplicationError
		transportErr *quic.TransportError
		resetErr     *quic.StatelessResetError
		vnErr        *quic.VersionNegotiationError
		hsErr        *quic.HandshakeTimeoutError
		idleErr      *quic.IdleTimeoutError
	)
	return errors.As(err, &appErr) ||
		errors.As(err, &transportErr) ||
		errors.As(err, &resetErr) ||
		errors.As(err, &vnErr) ||
		errors.As(err, &hsErr) ||
		errors.As(err, &idleErr)
}

// echoNotProbed relabels err as ErrEchoNoConnection — but only an error that
// IsEchoUnsupported would otherwise have read as "pre-echo server". Timeouts
// pass through untouched: they stay failures that strike toward a re-race
// (ticket #3463). So relabelling can only turn "unsupported" into "not
// probed", never hide a failure.
func echoNotProbed(err error) error {
	if err == nil || !IsEchoUnsupported(err) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrEchoNoConnection, err)
}
