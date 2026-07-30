package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/kaitu-io/k2/wire"
)

// timeoutError is a mock net.Error that returns Timeout()=true.
type timeoutError struct{ msg string }

func (e *timeoutError) Error() string   { return e.msg }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return false }

// Verify timeoutError satisfies net.Error.
var _ net.Error = (*timeoutError)(nil)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
	}{
		// 400 Bad Config
		{"parse URL", fmt.Errorf("wire: parse URL: invalid scheme"), 400},
		{"missing auth", errors.New("wire: missing auth (udid:token@)"), 400},
		{"unsupported scheme", fmt.Errorf("wire: unsupported scheme %q, want k2v5", "http"), 400},
		{"missing port", errors.New("wire: missing port"), 400},

		// 401/402 Auth Rejected (structured sentinels, not string matching)
		{"stream rejected sentinel", fmt.Errorf("%w: invalid token", wire.ErrStreamRejected), 401},
		{"metadata rejected 402", fmt.Errorf("%w: %w", wire.ErrMetadataRejected, &wire.StreamError{Code: 402, Message: "expired"}), 402},

		// 403 Forbidden
		{"pin mismatch", errors.New("wire: public key pin mismatch and not signed by Kaitu CA"), 403},
		{"blocked CA", fmt.Errorf("wire: certificate issued by blocked CA: %s", "BadCA"), 403},

		// 108 Timeout (via net.Error interface)
		{"dial timeout", fmt.Errorf("wire: TCP dial: %w", &timeoutError{msg: "i/o timeout"}), 108},
		{"wrapped timeout", fmt.Errorf("start engine: %w", &timeoutError{msg: "connection timed out"}), 108},

		// 502 TLS/Protocol Error
		{"uTLS handshake", errors.New("wire: uTLS handshake: certificate verify failed"), 502},
		{"certificate error", errors.New("wire: certificate issued by unknown authority"), 502},
		{"QUIC dial", fmt.Errorf("wire: QUIC dial: %w", errors.New("crypto handshake failed")), 502},

		// 101 Client Network Unavailable
		{"network unreachable", errors.New("wire: TCP dial: network unreachable"), 101},
		{"no route to host", errors.New("wire: TCP dial: no route to host"), 101},

		// 503 Server Unreachable
		{"TCP dial refused", fmt.Errorf("wire: TCP dial 10.0.0.1:443: %w", errors.New("connection refused")), 503},
		{"listen UDP", errors.New("wire: listen UDP: address already in use"), 503},

		// MultiTransportError — classifies by TCP-WS (primary)
		{"multi transport TCP refused", &wire.MultiTransportError{
			QUICErr:  errors.New("wire: QUIC dial: crypto handshake failed"),
			TCPWSErr: errors.New("wire: TCP dial: connection refused"),
		}, 503},
		{"multi transport TLS error", &wire.MultiTransportError{
			QUICErr:  errors.New("wire: QUIC dial: crypto handshake failed"),
			TCPWSErr: errors.New("wire: uTLS handshake: certificate verify failed"),
		}, 502},

		// context.DeadlineExceeded
		{"context deadline exceeded", context.DeadlineExceeded, 108},
		{"wrapped deadline exceeded", fmt.Errorf("start: %w", context.DeadlineExceeded), 108},

		// 570 Fallback
		{"unknown error", errors.New("wire: something completely unknown"), 570},
		{"smux error", errors.New("wire: smux client: broken pipe"), 570},
		{"generic engine error", errors.New("engine: already started"), 570},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyError(tt.err)
			if result.Code != tt.wantCode {
				t.Errorf("ClassifyError(%q) code = %d, want %d", tt.err, result.Code, tt.wantCode)
			}
			if result.Message != tt.err.Error() {
				t.Errorf("ClassifyError(%q) message = %q, want %q", tt.err, result.Message, tt.err.Error())
			}
		})
	}
}

func TestClassifyError_MultiTransportMessage(t *testing.T) {
	mte := &wire.MultiTransportError{
		QUICErr:  errors.New("QUIC fail"),
		TCPWSErr: errors.New("TCP fail"),
	}
	result := ClassifyError(mte)
	// Message should contain BOTH errors for UI display.
	if !strings.Contains(result.Message, "QUIC fail") {
		t.Errorf("message missing QUIC error: %s", result.Message)
	}
	if !strings.Contains(result.Message, "TCP fail") {
		t.Errorf("message missing TCP error: %s", result.Message)
	}
}

func TestClassifyError_MultiTransportNilTCPWS(t *testing.T) {
	// TCPWSErr nil — should classify by QUICErr without panicking.
	mte := &wire.MultiTransportError{
		QUICErr:  errors.New("wire: QUIC dial: crypto handshake failed"),
		TCPWSErr: nil,
	}
	result := ClassifyError(mte)
	if result.Code != 502 {
		t.Errorf("code = %d, want 502", result.Code)
	}
}

func TestStatusJSON_StructuredError(t *testing.T) {
	e := New()
	// Simulate fail() setting lastError.
	e.fail(fmt.Errorf("wire: TCP dial: connection refused"))

	status := e.StatusJSON()
	if !strings.Contains(status, `"code":503`) {
		t.Errorf("StatusJSON missing structured error code, got: %s", status)
	}
	if !strings.Contains(status, `"category":"server"`) {
		t.Errorf("StatusJSON missing category, got: %s", status)
	}
	if !strings.Contains(status, `"message":"wire: TCP dial: connection refused"`) {
		t.Errorf("StatusJSON missing error message, got: %s", status)
	}
}

func TestStatusJSON_NoError(t *testing.T) {
	e := New()
	status := e.StatusJSON()
	if strings.Contains(status, "error") {
		t.Errorf("StatusJSON should not contain error when none set, got: %s", status)
	}
}

func TestClassifyError_Category(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantCode     int
		wantCategory string
	}{
		{"101 network", errors.New("wire: network unreachable"), 101, CategoryNetwork},
		{"108 network", context.DeadlineExceeded, 108, CategoryNetwork},
		{"400 client", fmt.Errorf("wire: parse URL: bad"), 400, CategoryClient},
		{"401 client", fmt.Errorf("%w: expired", wire.ErrStreamRejected), 401, CategoryClient},
		{"402 client", errors.New("payment required"), 402, CategoryClient},
		{"403 client", errors.New("wire: public key pin mismatch and not signed by Kaitu CA"), 403, CategoryClient},
		{"502 server", errors.New("wire: uTLS handshake: verify failed"), 502, CategoryServer},
		{"503 server", errors.New("wire: TCP dial: connection refused"), 503, CategoryServer},
		{"570 server", errors.New("wire: something unknown"), 570, CategoryServer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyError(tt.err)
			if result.Code != tt.wantCode {
				t.Errorf("code = %d, want %d", result.Code, tt.wantCode)
			}
			if result.Category != tt.wantCategory {
				t.Errorf("category = %q, want %q", result.Category, tt.wantCategory)
			}
		})
	}
}

func TestClassifyError_StreamErrorPriority(t *testing.T) {
	// StreamError = server responded to target request → always CategoryTarget.
	se := &wire.StreamError{Code: 403, Message: "blocked: 10.0.0.1 is a reserved IP"}
	result := ClassifyError(se)
	if result.Code != 403 {
		t.Errorf("code = %d, want 403", result.Code)
	}
	if result.Category != CategoryTarget {
		t.Errorf("category = %q, want %q", result.Category, CategoryTarget)
	}
	if result.Message != se.Message {
		t.Errorf("message = %q, want %q", result.Message, se.Message)
	}
}

func TestClassifyError_PaymentRequired(t *testing.T) {
	result := ClassifyError(errors.New("subscription expired"))
	if result.Code != 402 {
		t.Errorf("code = %d, want 402", result.Code)
	}
	if result.Category != CategoryClient {
		t.Errorf("category = %q, want %q", result.Category, CategoryClient)
	}
}

func TestClassifyError_NotConnected(t *testing.T) {
	result := ClassifyError(errors.New("wire: not connected"))
	if result.Code != 503 {
		t.Errorf("code = %d, want 503", result.Code)
	}
	if result.Category != CategoryServer {
		t.Errorf("category = %q, want %q", result.Category, CategoryServer)
	}
}

func TestClassifyError_StreamErrorInsideMTE(t *testing.T) {
	// StreamError inside MultiTransportError should be found via errors.As
	// thanks to Unwrap() []error. StreamError → always CategoryTarget.
	mte := &wire.MultiTransportError{
		QUICErr:  &wire.StreamError{Code: 401, Message: "token expired"},
		TCPWSErr: errors.New("wire: TCP dial: connection refused"),
	}
	result := ClassifyError(mte)
	// StreamError priority 0 should find the 401 inside QUICErr.
	if result.Code != 401 {
		t.Errorf("code = %d, want 401 (StreamError found via MTE.Unwrap)", result.Code)
	}
	if result.Category != CategoryTarget {
		t.Errorf("category = %q, want %q", result.Category, CategoryTarget)
	}
}

func TestFail_OnStatusReceivesError(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	testErr := fmt.Errorf("wire: TCP dial: connection refused")
	e.fail(testErr)

	errs := h.getErrors()
	if len(errs) == 0 {
		t.Fatal("expected OnStatus with error to be called")
	}
	if errs[0] != testErr.Error() {
		t.Errorf("OnStatus error message = %q, want %q", errs[0], testErr.Error())
	}
}

func TestCategoryForCode_RuleBundlesUnavailable(t *testing.T) {
	if got := categoryForCode(ErrCodeRuleBundlesUnavailable); got != CategoryServer {
		t.Errorf("expected CategoryServer for code %d, got %q", ErrCodeRuleBundlesUnavailable, got)
	}
}

// Gateway environment setup failures (iptables, sysctl, etc.) are local-host
// problems — never server fault. Must classify as 4xx client, not 570 server.
// Regression guard for misclassification observed on OpenWrt MIPS where
// "gateway: install intercept: iptables: Chain already exists" fell through
// to fallback 570 + CategoryServer.
func TestClassifyError_GatewayIptablesFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			"install intercept chain exists",
			errors.New("gateway: install intercept: gateway: iptables [-t mangle -N K2R]: iptables -t mangle -N K2R: iptables: Chain already exists.\n: exit status 1"),
		},
		{
			"iptables generic failure",
			errors.New("gateway: iptables [-t mangle -A K2R -j TPROXY]: Operation not permitted"),
		},
		{
			"ip6tables failure",
			errors.New("gateway: ip6tables [-t mangle -N K2R]: command not found"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyError(tt.err)
			if result.Code != ErrCodeEnvironmentSetupFailed {
				t.Errorf("code = %d, want %d (ErrCodeEnvironmentSetupFailed)", result.Code, ErrCodeEnvironmentSetupFailed)
			}
			if result.Category != CategoryClient {
				t.Errorf("category = %q, want %q (gateway-host problem, not server)", result.Category, CategoryClient)
			}
		})
	}
}

// TestClassifyError_MetadataRejection: the producer (wire connect) classifies
// via the ErrMetadataRejected sentinel; the embedded StreamError carries the
// server code. All three land in CategoryClient — retrying the same
// credentials cannot succeed, so the state machine must stop retrying.
func TestClassifyError_MetadataRejection(t *testing.T) {
	for _, code := range []int{401, 402, 403} {
		err := fmt.Errorf("%w: %w", wire.ErrMetadataRejected, &wire.StreamError{Code: code, Message: "auth rejected"})
		got := ClassifyError(err)
		if got.Code != code || got.Category != CategoryClient {
			t.Errorf("code %d: got {%d %s}, want {%d %s}", code, got.Code, got.Category, code, CategoryClient)
		}
	}
}

// TestClassifyError_MetadataRejectionThroughMultiTransport: the same rejection
// arriving inside a MultiTransportError (TransportManager fallback path) must
// classify identically — MultiTransportError.Unwrap() []error carries it.
func TestClassifyError_MetadataRejectionThroughMultiTransport(t *testing.T) {
	inner := fmt.Errorf("%w: %w", wire.ErrMetadataRejected, &wire.StreamError{Code: 402, Message: "subscription expired"})
	mte := &wire.MultiTransportError{QUICErr: errors.New("quic: handshake timeout"), TCPWSErr: inner}
	got := ClassifyError(mte)
	if got.Code != 402 || got.Category != CategoryClient {
		t.Fatalf("got {%d %s}, want {402 client}", got.Code, got.Category)
	}
}

// TestClassifyError_LegacyStreamRejectedSentinel: an old enforce server (or
// the proxy-stream admission gate) answers the uncoded 0x01 "unauthorized" —
// classified 401 via the errors.Is sentinel, not via string matching.
func TestClassifyError_LegacyStreamRejectedSentinel(t *testing.T) {
	got := ClassifyError(fmt.Errorf("connect: %w: unauthorized", wire.ErrStreamRejected))
	if got.Code != ErrCodeAuthRejected || got.Category != CategoryClient {
		t.Fatalf("got {%d %s}, want {401 client}", got.Code, got.Category)
	}
}

// TestClassifyError_FabricatedStringIsNotAuth: string matching is gone — a
// message that merely CONTAINS "stream rejected" no longer classifies as 401.
func TestClassifyError_FabricatedStringIsNotAuth(t *testing.T) {
	got := ClassifyError(errors.New("wire: stream rejected by server: whatever"))
	if got.Code == ErrCodeAuthRejected {
		t.Fatal("string matching resurrected")
	}
}

// TestClassifyError_BareStreamError403StaysTarget: proxy-layer StreamError
// (server/handler.go writes 403 for blocked target addresses) must remain
// CategoryTarget — a user browsing to a blocked IP is not an auth failure.
func TestClassifyError_BareStreamError403StaysTarget(t *testing.T) {
	got := ClassifyError(&wire.StreamError{Code: 403, Message: "blocked: 10.0.0.1 is a reserved IP"})
	if got.Category != CategoryTarget {
		t.Fatalf("category = %s, want target", got.Category)
	}
}
