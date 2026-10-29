package http3

import (
	stdtls "crypto/tls"
	"reflect"
	"testing"

	tls "github.com/metacubex/utls"
)

// handledFields are the ConnectionState fields stdConnectionState explicitly
// accounts for — either by copying them or by documenting why it cannot.
//
// This list is the whole point of the test: a hand-written struct copy silently
// drops any field added later, and the compiler never complains. Growing either
// ConnectionState must break this test so a human decides what the new field
// means for the std bridge, rather than shipping a state that quietly reads as
// the zero value.
var handledFields = map[string]string{
	"Version":                     "copied",
	"HandshakeComplete":           "copied",
	"DidResume":                   "copied",
	"CipherSuite":                 "copied",
	"NegotiatedProtocol":          "copied",
	"NegotiatedProtocolIsMutual":  "copied",
	"ServerName":                  "copied",
	"PeerCertificates":            "copied",
	"VerifiedChains":              "copied",
	"SignedCertificateTimestamps": "copied",
	"OCSPResponse":                "copied",
	"TLSUnique":                   "copied",
	"ECHAccepted":                 "copied",
	"CurveID":                     "std-only: utls keeps the negotiated group unexported",
	"PeerApplicationSettings":     "utls-only: no destination field in crypto/tls",
}

func TestStdConnectionState_FieldCoverage(t *testing.T) {
	for _, tc := range []struct {
		side string
		typ  reflect.Type
	}{
		{"crypto/tls", reflect.TypeOf(stdtls.ConnectionState{})},
		{"metacubex/utls", reflect.TypeOf(tls.ConnectionState{})},
	} {
		for i := 0; i < tc.typ.NumField(); i++ {
			f := tc.typ.Field(i)
			if !f.IsExported() {
				continue
			}
			if _, ok := handledFields[f.Name]; !ok {
				t.Errorf("%s.ConnectionState.%s is not handled by stdConnectionState — "+
					"copy it (or document why it cannot be copied) and add it to handledFields",
					tc.side, f.Name)
			}
		}
	}
}

// TestStdConnectionState_CopiesValues is the mutation check for the guard above:
// a field listed as "copied" that is not actually assigned would pass the
// coverage test but drop data at runtime.
func TestStdConnectionState_CopiesValues(t *testing.T) {
	src := tls.ConnectionState{
		Version:                     tls.VersionTLS13,
		HandshakeComplete:           true,
		DidResume:                   true,
		CipherSuite:                 tls.TLS_AES_128_GCM_SHA256,
		NegotiatedProtocol:          "h3",
		NegotiatedProtocolIsMutual:  true,
		ServerName:                  "example.com",
		SignedCertificateTimestamps: [][]byte{{1, 2, 3}},
		OCSPResponse:                []byte{4, 5, 6},
		TLSUnique:                   []byte{7, 8, 9},
		ECHAccepted:                 true,
	}

	got := stdConnectionState(src)

	if got.Version != src.Version ||
		got.HandshakeComplete != src.HandshakeComplete ||
		got.DidResume != src.DidResume ||
		got.CipherSuite != src.CipherSuite ||
		got.NegotiatedProtocol != src.NegotiatedProtocol ||
		got.NegotiatedProtocolIsMutual != src.NegotiatedProtocolIsMutual ||
		got.ServerName != src.ServerName ||
		got.ECHAccepted != src.ECHAccepted {
		t.Fatalf("scalar fields lost in conversion: %+v", got)
	}
	if !reflect.DeepEqual(got.SignedCertificateTimestamps, src.SignedCertificateTimestamps) ||
		!reflect.DeepEqual(got.OCSPResponse, src.OCSPResponse) ||
		!reflect.DeepEqual(got.TLSUnique, src.TLSUnique) {
		t.Fatalf("byte-slice fields lost in conversion: %+v", got)
	}
}
