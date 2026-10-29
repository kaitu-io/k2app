package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/rule"
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

// TestClassifyError_MetadataRejectionNodeFault: a metadata rejection carrying
// a 5xx code means the NODE could not authenticate (no NodeSecret,
// unresolvable node IPv4 — wire.AuthCodeNodeFault), not that the device was
// judged. It must stay in the server category so the engine keeps retrying and
// the transport race routes around the broken node; collapsing it into 401
// would both blame the user's account and stop the retry that would have
// rescued them.
func TestClassifyError_MetadataRejectionNodeFault(t *testing.T) {
	for _, code := range []int{502, 503, 570} {
		err := fmt.Errorf("%w: %w", wire.ErrMetadataRejected, &wire.StreamError{Code: code, Message: "auth unavailable"})
		got := ClassifyError(err)
		if got.Code != code {
			t.Errorf("code %d: got %d, want it preserved", code, got.Code)
		}
		if got.Category != CategoryServer {
			t.Errorf("code %d: category = %s, want %s (must stay retryable)", code, got.Category, CategoryServer)
		}
	}
}

// TestClassifyError_MetadataRejectionUnknownCodeStaysClient: an unrecognised
// code still defaults to the fail-closed 401. Only the explicitly enumerated
// server codes escape the client category.
func TestClassifyError_MetadataRejectionUnknownCodeStaysClient(t *testing.T) {
	err := fmt.Errorf("%w: %w", wire.ErrMetadataRejected, &wire.StreamError{Code: 418, Message: "teapot"})
	got := ClassifyError(err)
	if got.Code != ErrCodeAuthRejected || got.Category != CategoryClient {
		t.Errorf("got {%d %s}, want {%d %s}", got.Code, got.Category, ErrCodeAuthRejected, CategoryClient)
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

// provider.Start failures (Windows wintun adapter creation, gVisor stack,
// proxy-mode listen) happen on the user's own machine after the wire has
// already connected — they are never server fault. Before this guard they
// fell through classifyByPatterns to fallback 570/CategoryServer, and the UI
// told the user to "switch node" (ticket #3849: six attempts across five
// nodes, every one failing at wintun CreateAdapter with ERROR_FILE_NOT_FOUND).
func TestProviderStartError_ClassifiedAsEnvironmentSetup(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"windows wintun create", fmt.Errorf("provider: create TUN: %w", errors.New("The system cannot find the file specified."))},
		{"create stack", fmt.Errorf("provider: create stack: %w", errors.New("gvisor: init failed"))},
		{"start TUN", fmt.Errorf("provider: start TUN: %w", errors.New("set route: Access is denied."))},
		{"proxy listen in use", fmt.Errorf("provider: listen 127.0.0.1:1080: %w", errors.New("bind: address already in use"))},
		// A provider error whose text happens to match a server pattern must
		// still be local: the call site, not the wording, decides.
		{"text resembles server pattern", fmt.Errorf("provider: start TUN: %w", errors.New("connection refused"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyError(providerStartError(tt.err))
			if result.Code != ErrCodeEnvironmentSetupFailed {
				t.Errorf("code = %d, want %d (ErrCodeEnvironmentSetupFailed)", result.Code, ErrCodeEnvironmentSetupFailed)
			}
			if result.Category != CategoryClient {
				t.Errorf("category = %q, want %q (local host problem, not server)", result.Category, CategoryClient)
			}
			if result.Message != tt.err.Error() {
				t.Errorf("message = %q, want original %q preserved for logs", result.Message, tt.err.Error())
			}
		})
	}
}

// Stop() during Start() cancels the provider's context; that is not an
// environment fault and must not be relabelled as one.
func TestProviderStartError_CancellationPassesThrough(t *testing.T) {
	for _, base := range []error{context.Canceled, context.DeadlineExceeded} {
		err := fmt.Errorf("provider: start stack: %w", base)
		if got := providerStartError(err); got != err {
			t.Errorf("providerStartError(%v) = %v, want the original error untouched", err, got)
		}
	}
	if providerStartError(nil) != nil {
		t.Error("providerStartError(nil) must be nil")
	}
}

// failingProvider simulates a local-host provider failure (e.g. wintun
// CreateAdapter returning ERROR_FILE_NOT_FOUND).
type failingProvider struct{ err error }

func (p *failingProvider) Start(ctx context.Context, handler provider.ConnectionHandler) error {
	return p.err
}
func (p *failingProvider) Close() error { return nil }
func (p *failingProvider) Name() string { return "failing-test" }

// Drives the real Engine.Start() to its provider-start call site (proxy mode
// needs no k2v5 outbound, so no wire is dialled) and asserts the surfaced
// error is 412/client. Guards the call-site wiring, which the helper-level
// tests above cannot see.
func TestEngineStart_ProviderFailureIsEnvironmentSetup(t *testing.T) {
	e := New()
	provErr := fmt.Errorf("provider: listen 127.0.0.1:1080: %w", errors.New("bind: address already in use"))
	err := e.Start(Config{
		Client: &config.ClientConfig{
			Mode:   "proxy",
			Routes: []config.RouteConfig{{Via: "direct", Match: rule.MatchConfig{All: true}}},
		},
		FileDescriptor: -1,
		Provider:       &failingProvider{err: provErr},
	})
	if err == nil {
		t.Fatal("Start with failing provider should return error")
	}
	var ee *EngineError
	if !errors.As(err, &ee) {
		t.Fatalf("Start error = %T %v, want *EngineError", err, err)
	}
	if ee.Code != ErrCodeEnvironmentSetupFailed || ee.Category != CategoryClient {
		t.Fatalf("Start error = %d/%s, want %d/%s (message %q)", ee.Code, ee.Category, ErrCodeEnvironmentSetupFailed, CategoryClient, ee.Message)
	}
	if !strings.Contains(e.StatusJSON(), `"code":412`) {
		t.Errorf("StatusJSON = %s, want code 412 surfaced to the UI", e.StatusJSON())
	}
}
