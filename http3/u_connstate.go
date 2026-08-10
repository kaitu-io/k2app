package http3

import (
	stdtls "crypto/tls"

	tls "github.com/metacubex/utls"
)

// stdConnectionState converts kuic's utls-typed TLS connection state into the
// standard library type.
//
// It exists because kuic runs its whole TLS stack on github.com/metacubex/utls
// (so the client ClientHello can be shaped to a browser fingerprint), while
// net/http's Response.TLS, Request.TLS and httptrace.TLSHandshakeDone are
// hard-typed to *crypto/tls.ConnectionState and cannot be changed.
//
// KNOWN GAP: stdtls.ConnectionState.CurveID has no source. metacubex/utls
// tracks the negotiated group in an unexported testingOnlyCurveID field, so it
// is left at its zero value here. Everything else round-trips exactly.
//
// Any field added to either struct must be handled here — TestStdConnectionState_FieldCoverage
// fails when either side grows a field this function does not name.
func stdConnectionState(s tls.ConnectionState) stdtls.ConnectionState {
	return stdtls.ConnectionState{
		Version:                     s.Version,
		HandshakeComplete:           s.HandshakeComplete,
		DidResume:                   s.DidResume,
		CipherSuite:                 s.CipherSuite,
		NegotiatedProtocol:          s.NegotiatedProtocol,
		NegotiatedProtocolIsMutual:  s.NegotiatedProtocolIsMutual,
		ServerName:                  s.ServerName,
		PeerCertificates:            s.PeerCertificates,
		VerifiedChains:              s.VerifiedChains,
		SignedCertificateTimestamps: s.SignedCertificateTimestamps,
		OCSPResponse:                s.OCSPResponse,
		TLSUnique:                   s.TLSUnique,
		ECHAccepted:                 s.ECHAccepted,
		// CurveID: see KNOWN GAP above.
		// PeerApplicationSettings (utls-only) has no destination field.
	}
}
