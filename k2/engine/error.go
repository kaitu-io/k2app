package engine

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/kaitu-io/k2/wire"
)

// Error codes: 10x client network, 40x client action, 50x server/node.
// Used by StatusJSON() and exposed to frontend via daemon API.
const (
	ErrCodeNetworkUnavailable = 101 // Client network unreachable, no route to host
	ErrCodeTimeout            = 108 // Connection or handshake timeout
	ErrCodeBadConfig              = 400 // Invalid wire URL, missing auth, bad scheme
	ErrCodeAuthRejected           = 401 // Server rejected authentication (stream rejected)
	ErrCodePaymentRequired        = 402 // Subscription expired or payment required
	ErrCodeForbidden              = 403 // Certificate pin mismatch, blocked CA
	ErrCodeEnvironmentSetupFailed = 412 // Local host setup failed (iptables, sysctl, file perms, TUN/wintun adapter) — operator must fix env. HTTP 412 Precondition Failed semantics
	ErrCodeProtocolError          = 502 // TLS handshake failure, QUIC dial failure
	ErrCodeServerUnreachable      = 503 // TCP dial failed, connection refused
	ErrCodeRuleBundlesUnavailable = 504 // Dependency unreachable: rule bundle CDN sources all failed (transient, retryable)
	ErrCodeConnectionFatal        = 570 // Unclassified connection error (fallback)
)

// Error categories — three-party blame attribution.
//
//	k2 (client) ◄──wire──► k2s (server) ──dial──► target
//	     │                      │                    │
//	 client error          server error         target error
//
// client:  k2's responsibility — local network, bad config, auth, payment, pin mismatch.
// network: k2's responsibility — transient local network issues (auto-retry subset of client).
// server:  k2s's responsibility — unreachable, protocol error, connection fatal.
// target:  destination's responsibility — blocked IP, target DNS fail, target connection refused.
//          Per-request, does NOT affect tunnel health.
const (
	CategoryClient  = "client"  // User action needed (re-auth, reconfigure, renew)
	CategoryNetwork = "network" // Transient, auto-retry or wait
	CategoryServer  = "server"  // Server-side issue (suggest update)
	CategoryTarget  = "target"  // Target-specific (k2s→target failed, wire is healthy)
	CategoryUnknown = "unknown" // Unclassified
)

// EngineError is a structured error with an HTTP-aligned code and blame category.
type EngineError struct {
	Code     int    `json:"code"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

// categoryForCode maps an error code to its blame category.
func categoryForCode(code int) string {
	switch code {
	case ErrCodeBadConfig, ErrCodeAuthRejected, ErrCodePaymentRequired, ErrCodeForbidden, ErrCodeEnvironmentSetupFailed:
		return CategoryClient
	case ErrCodeNetworkUnavailable, ErrCodeTimeout:
		return CategoryNetwork
	case ErrCodeProtocolError, ErrCodeServerUnreachable, ErrCodeRuleBundlesUnavailable, ErrCodeConnectionFatal:
		return CategoryServer
	default:
		return CategoryUnknown
	}
}

// newEngineError creates an EngineError with auto-derived category.
func newEngineError(code int, msg string) *EngineError {
	return &EngineError{Code: code, Category: categoryForCode(code), Message: msg}
}

func (e *EngineError) Error() string {
	return e.Message
}

// RequiresUserAction reports whether this error stays true even after
// something else proves the wire works — i.e. whether it must survive
// ClearWireError (a successful proxy dial) and the network-change clear.
//
// It answers exactly one question, and it is the producer's answer, not a
// category test at the call site. The distinction matters because "client
// category" and "the user must do something" are not the same set:
//
//	401 / 402  the tunnel cannot work until the user re-authenticates or pays.
//	           No dial can succeed while they hold, so surviving a "successful
//	           dial" is vacuous — but keeping them is what makes the UI stable
//	           across a reconnect storm.
//	403        a pin mismatch or blocked CA is the *node's* certificate, not
//	           anything the user owns. It is also per-channel: TCP-WS can fail
//	           pin verification while QUIC carries traffic normally, and a
//	           MultiTransportError is classified by the TCP-WS leg alone. A
//	           later successful dial went through pin verification too, so it
//	           is positive proof the verdict is stale.
//	400 / 412  a dial that succeeded necessarily parsed the config and had a
//	           working local environment.
//
// Scope note: this governs *clearing* only. ReportWireError's overwrite guard
// still asks CategoryClient, so a fresh 403 may still displace a network error
// while the tunnel is down — that is "which error to show while broken", a
// different question from "is this error still true".
func (e *EngineError) RequiresUserAction() bool {
	if e == nil {
		return false
	}
	switch e.Code {
	case ErrCodeAuthRejected, ErrCodePaymentRequired:
		return true
	default:
		return false
	}
}

// ClassifyError maps a wire/engine error to an HTTP-aligned error code with category.
// Priority: already-classified EngineError → StreamError (server-provided code) → MultiTransportError → timeout → string pattern → fallback 570.
func ClassifyError(err error) *EngineError {
	// Priority 0a: already-classified EngineError passes through untouched.
	// Allows call sites to pre-classify (e.g. rule-bundle gate, protocol-specific
	// errors) and still go through Engine.fail() for proper state transition.
	var ee *EngineError
	if errors.As(err, &ee) {
		return ee
	}

	// Priority 0b: metadata (device-auth) rejection — the server rejected this
	// connection on the metadata stream. Producer-classified via
	// wire.ErrMetadataRejected; the embedded StreamError carries the server's
	// code. Two disjoint families, and keeping them apart is the whole point:
	//
	//   4xx (401 invalid credentials / 402 subscription expired / 403 device
	//        class or ban) → CategoryClient. Center judged THIS device;
	//        retrying the same credentials — here or on another node — cannot
	//        succeed, so the engine must stop rather than race on.
	//   5xx (503 the node itself cannot authenticate: no NodeSecret,
	//        unresolvable node IPv4 — wire.AuthCodeNodeFault) → CategoryServer.
	//        The device is fine and a healthy node will admit it, so this must
	//        stay retryable. Collapsing it into 401 would tell the user their
	//        account failed AND stop the transport race that would otherwise
	//        route around one misconfigured node.
	//
	// Must run BEFORE the bare-StreamError check — errors.As would otherwise
	// classify the embedded StreamError as CategoryTarget. Traverses
	// MultiTransportError via its Unwrap() []error.
	if errors.Is(err, wire.ErrMetadataRejected) {
		var se *wire.StreamError
		if errors.As(err, &se) {
			switch se.Code {
			case ErrCodeAuthRejected, ErrCodePaymentRequired, ErrCodeForbidden,
				ErrCodeProtocolError, ErrCodeServerUnreachable, ErrCodeConnectionFatal:
				return newEngineError(se.Code, err.Error())
			}
		}
		return newEngineError(ErrCodeAuthRejected, err.Error())
	}

	// Priority 0c: legacy stream-rejection sentinel — old enforce servers and
	// the proxy-stream admission gate answer the uncoded 0x01 "unauthorized".
	// Structured errors.Is on the producer's sentinel, never string matching.
	if errors.Is(err, wire.ErrStreamRejected) {
		return newEngineError(ErrCodeAuthRejected, err.Error())
	}

	// Priority 0: StreamError — server responded to a proxy request about a
	// specific target. The wire is healthy (server received and responded).
	// Always CategoryTarget regardless of code. Auth/payment errors arrive
	// wrapped in ErrMetadataRejected / ErrStreamRejected (handled above),
	// never as a bare StreamError.
	var se *wire.StreamError
	if errors.As(err, &se) && se.Code > 0 {
		return &EngineError{Code: se.Code, Category: CategoryTarget, Message: se.Message}
	}

	// Priority 1: MultiTransportError — classify by primary (TCP-WS) error,
	// but keep full combined message for UI display.
	var mte *wire.MultiTransportError
	if errors.As(err, &mte) {
		if mte.TCPWSErr != nil {
			classified := classifyByPatterns(mte.TCPWSErr.Error())
			classified.Message = mte.Error()
			return classified
		}
		if mte.QUICErr != nil {
			classified := classifyByPatterns(mte.QUICErr.Error())
			classified.Message = mte.Error()
			return classified
		}
		// Both nil (shouldn't happen) — fall through to pattern matching.
	}

	msg := err.Error()

	// Priority 2: context.DeadlineExceeded (not a net.Error, so check explicitly).
	if errors.Is(err, context.DeadlineExceeded) {
		return newEngineError(ErrCodeTimeout, msg)
	}

	// Priority 3: net.Error.Timeout() interface check.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return newEngineError(ErrCodeTimeout, msg)
	}

	// Priority 4: String pattern matching.
	return classifyByPatterns(msg)
}

// providerStartError pre-classifies a provider.Start failure as 412
// EnvironmentSetupFailed. provider.Start runs after the wire has connected and
// only touches the local host (TUN / wintun adapter, routes, gVisor stack,
// proxy-mode listen socket), so its failures are never server fault. Left to
// classifyByPatterns they fall through to fallback 570/CategoryServer and the
// UI tells the user to switch node — which cannot help. Classified at the call
// site rather than by message text: sing-tun/wintun wording is OS-localized.
//
// Cancellation passes through untouched — Stop() during Start() is not an
// environment fault.
func providerStartError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return newEngineError(ErrCodeEnvironmentSetupFailed, err.Error())
}

// classifyByPatterns matches error message text to an error code with category.
func classifyByPatterns(msg string) *EngineError {
	// 402: Payment required / subscription expired.
	if strings.Contains(msg, "payment required") || strings.Contains(msg, "subscription") {
		return newEngineError(ErrCodePaymentRequired, msg)
	}

	// 403: Forbidden (certificate/pin verification failure).
	if strings.Contains(msg, "pin mismatch") || strings.Contains(msg, "blocked CA") {
		return newEngineError(ErrCodeForbidden, msg)
	}

	// 400: Bad config (URL parse errors, missing fields).
	if strings.Contains(msg, "parse URL") ||
		strings.Contains(msg, "missing auth") ||
		strings.Contains(msg, "unsupported scheme") ||
		strings.Contains(msg, "missing port") {
		return newEngineError(ErrCodeBadConfig, msg)
	}

	// 406: Local host setup failed (gateway iptables/ip6tables/sysctl). The
	// gateway daemon runs these commands on its own host; failures here are
	// operator-actionable (missing kernel module, permission denied, stale
	// chain). Must be client-category — never server. Checked before the
	// generic "TCP dial" / "listen UDP" patterns so the leading "gateway:"
	// prefix is matched first.
	if strings.Contains(msg, "install intercept") ||
		strings.Contains(msg, "iptables") ||
		strings.Contains(msg, "ip6tables") {
		return newEngineError(ErrCodeEnvironmentSetupFailed, msg)
	}

	// 101: Client network unavailable (checked BEFORE 503 — a local dial that
	// can't even route is client-side, not server-side). Real errno strings:
	// Linux/Android/Darwin ENETUNREACH renders as "connect: network is
	// unreachable" (with "is" — the historical "network unreachable" pattern
	// matched no real errno and every no-network attempt fell through to 503),
	// EHOSTUNREACH as "no route to host", Windows WSAENETUNREACH as "A socket
	// operation was attempted to an unreachable network".
	if strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "network unreachable") ||
		strings.Contains(msg, "unreachable network") ||
		strings.Contains(msg, "no route to host") {
		return newEngineError(ErrCodeNetworkUnavailable, msg)
	}

	// 502: TLS/Protocol error (handshake failures).
	if strings.Contains(msg, "uTLS handshake") ||
		strings.Contains(msg, "certificate") ||
		strings.Contains(msg, "QUIC dial") {
		return newEngineError(ErrCodeProtocolError, msg)
	}

	// 503: Server unreachable (connection refused, dial failed).
	if strings.Contains(msg, "TCP dial") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "listen UDP") ||
		strings.Contains(msg, "not connected") {
		return newEngineError(ErrCodeServerUnreachable, msg)
	}

	// 570: Fallback for all unclassified errors.
	return newEngineError(ErrCodeConnectionFatal, msg)
}
