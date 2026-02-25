package engine

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/kaitu-io/k2/wire"
)

// Error codes aligned with HTTP status codes.
// Used by StatusJSON() and exposed to frontend via daemon API.
const (
	ErrCodeBadConfig         = 400 // Invalid wire URL, missing auth, bad scheme
	ErrCodeAuthRejected      = 401 // Server rejected authentication (stream rejected)
	ErrCodeForbidden         = 403 // Certificate pin mismatch, blocked CA
	ErrCodeTimeout           = 408 // Connection or handshake timeout
	ErrCodeProtocolError     = 502 // TLS handshake failure, QUIC dial failure
	ErrCodeServerUnreachable = 503 // TCP dial failed, connection refused, network unreachable
	ErrCodeConnectionFatal   = 570 // Unclassified connection error (fallback)
)

// EngineError is a structured error with an HTTP-aligned code.
type EngineError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *EngineError) Error() string {
	return e.Message
}

// ClassifyError maps a wire/engine error to an HTTP-aligned error code.
// Priority: MultiTransportError → timeout → string pattern match → fallback 570.
func ClassifyError(err error) *EngineError {
	// Priority 1: MultiTransportError — classify by primary (TCP-WS) error,
	// but keep full combined message for UI display.
	var mte *wire.MultiTransportError
	if errors.As(err, &mte) {
		classified := classifyByPatterns(mte.TCPWSErr.Error())
		classified.Message = mte.Error()
		return classified
	}

	msg := err.Error()

	// Priority 2: context.DeadlineExceeded (not a net.Error, so check explicitly).
	if errors.Is(err, context.DeadlineExceeded) {
		return &EngineError{Code: ErrCodeTimeout, Message: msg}
	}

	// Priority 3: net.Error.Timeout() interface check.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &EngineError{Code: ErrCodeTimeout, Message: msg}
	}

	// Priority 4: String pattern matching.
	return classifyByPatterns(msg)
}

// classifyByPatterns matches error message text to an error code.
func classifyByPatterns(msg string) *EngineError {
	// 401: Auth rejected (server explicitly rejected credentials).
	if strings.Contains(msg, "stream rejected") {
		return &EngineError{Code: ErrCodeAuthRejected, Message: msg}
	}

	// 403: Forbidden (certificate/pin verification failure).
	if strings.Contains(msg, "pin mismatch") || strings.Contains(msg, "blocked CA") {
		return &EngineError{Code: ErrCodeForbidden, Message: msg}
	}

	// 400: Bad config (URL parse errors, missing fields).
	if strings.Contains(msg, "parse URL") ||
		strings.Contains(msg, "missing auth") ||
		strings.Contains(msg, "unsupported scheme") ||
		strings.Contains(msg, "missing port") {
		return &EngineError{Code: ErrCodeBadConfig, Message: msg}
	}

	// 502: TLS/Protocol error (handshake failures).
	if strings.Contains(msg, "uTLS handshake") ||
		strings.Contains(msg, "certificate") ||
		strings.Contains(msg, "QUIC dial") {
		return &EngineError{Code: ErrCodeProtocolError, Message: msg}
	}

	// 503: Server unreachable (network-level failures).
	if strings.Contains(msg, "TCP dial") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "network unreachable") ||
		strings.Contains(msg, "no route to host") ||
		strings.Contains(msg, "listen UDP") {
		return &EngineError{Code: ErrCodeServerUnreachable, Message: msg}
	}

	// 570: Fallback for all unclassified errors.
	return &EngineError{Code: ErrCodeConnectionFatal, Message: msg}
}
