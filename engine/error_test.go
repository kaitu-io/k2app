package engine

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
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

		// 401 Auth Rejected
		{"stream rejected", errors.New("wire: stream rejected by server: invalid token"), 401},
		{"stream rejected unauthorized", errors.New("wire: stream rejected by server: unauthorized"), 401},

		// 403 Forbidden
		{"pin mismatch", errors.New("wire: public key pin mismatch and not signed by Kaitu CA"), 403},
		{"blocked CA", fmt.Errorf("wire: certificate issued by blocked CA: %s", "BadCA"), 403},

		// 408 Timeout (via net.Error interface)
		{"dial timeout", fmt.Errorf("wire: TCP dial: %w", &timeoutError{msg: "i/o timeout"}), 408},
		{"wrapped timeout", fmt.Errorf("start engine: %w", &timeoutError{msg: "connection timed out"}), 408},

		// 502 TLS/Protocol Error
		{"uTLS handshake", errors.New("wire: uTLS handshake: certificate verify failed"), 502},
		{"certificate error", errors.New("wire: certificate issued by unknown authority"), 502},
		{"QUIC dial", fmt.Errorf("wire: QUIC dial: %w", errors.New("crypto handshake failed")), 502},

		// 503 Server Unreachable
		{"TCP dial refused", fmt.Errorf("wire: TCP dial 10.0.0.1:443: %w", errors.New("connection refused")), 503},
		{"network unreachable", errors.New("wire: TCP dial: network unreachable"), 503},
		{"listen UDP", errors.New("wire: listen UDP: address already in use"), 503},

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

func TestStatusJSON_StructuredError(t *testing.T) {
	e := New()
	// Simulate fail() setting lastError.
	e.fail(fmt.Errorf("wire: TCP dial: connection refused"))

	status := e.StatusJSON()
	// Should contain "error":{"code":503,"message":"wire: TCP dial: connection refused"}
	if !strings.Contains(status, `"code":503`) {
		t.Errorf("StatusJSON missing structured error code, got: %s", status)
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
