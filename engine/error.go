package engine

import (
	"errors"
	"net"
	"strings"
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

// classifyError maps a wire/engine error to an HTTP-aligned error code.
// Priority: net.Error.Timeout() → string pattern match → fallback 570.
func classifyError(err error) *EngineError {
	msg := err.Error()

	// Priority 1: Check for timeout via net.Error interface (most specific).
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &EngineError{Code: ErrCodeTimeout, Message: msg}
	}

	// Priority 2: String pattern matching.
	// Order matters — check more specific patterns first.

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
