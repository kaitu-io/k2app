package engine

import (
	"errors"
	"testing"
)

// Real errno strings as Go renders them per platform. The historical pattern
// "network unreachable" (no "is") matches none of these — every no-network
// connect attempt was misclassified as 503 server/ServerUnreachable, so the
// UI blamed the server when the user simply had no network (observed on
// Android 2026-07-16: Wi-Fi disconnected → banner showed a server error).
func TestClassifyError_ENETUNREACH_RealErrnoStrings(t *testing.T) {
	tests := []struct {
		name string
		msg  string
	}{
		// Verbatim from Android device log (transport race combined message).
		{"android transport race", "outbound setup: transport race 35-88-216-55.sslip.io: transport race: quic-443: <nil>; quic-hop: <nil>; tcpws: wire: TCP dial 35.88.216.55:443: dial tcp 35.88.216.55:443: connect: network is unreachable"},
		// Linux/Android/Darwin ENETUNREACH.
		{"unix ENETUNREACH", "wire: TCP dial 1.2.3.4:443: dial tcp 1.2.3.4:443: connect: network is unreachable"},
		// Windows WSAENETUNREACH.
		{"windows WSAENETUNREACH", "wire: TCP dial 1.2.3.4:443: dial tcp 1.2.3.4:443: connectex: A socket operation was attempted to an unreachable network."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ee := ClassifyError(errors.New(tt.msg))
			if ee.Code != ErrCodeNetworkUnavailable {
				t.Errorf("Code = %d, want %d (network unavailable)", ee.Code, ErrCodeNetworkUnavailable)
			}
			if ee.Category != CategoryNetwork {
				t.Errorf("Category = %q, want %q", ee.Category, CategoryNetwork)
			}
		})
	}
}
