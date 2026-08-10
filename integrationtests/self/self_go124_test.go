//go:build !go1.25

package self_test

import tls "github.com/metacubex/utls"

// getCurveID always reports 0 because kuic runs its TLS stack on
// github.com/metacubex/utls, which keeps the negotiated key exchange group in
// an unexported field. Upstream quic-go reads crypto/tls's exported
// ConnectionState.CurveID here on Go 1.25+; that field has no counterpart in
// utls, so tests that would assert on the negotiated group cannot do so in this
// fork. Same gap as http3.stdConnectionState.
func getCurveID(connState tls.ConnectionState) tls.CurveID {
	return 0
}
