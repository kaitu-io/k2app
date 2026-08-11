package wire

import (
	"testing"

	tls "github.com/metacubex/utls"
)

// These tests guard buildChromeH3Spec against the ways it can break without
// anything going red on its own.
//
// A wrong spec does not fail to compile and usually does not fail to connect —
// it connects while looking like something other than Chrome, which is the one
// outcome the fingerprint exists to prevent and the one no functional test
// notices. The expected values below are transcribed from
// refraction-networking/uquic's QUICChrome_146_IPv4 parrot, which is the
// authority this file was ported from; they are deliberately literal rather
// than derived from the spec under test.

// chrome146TransportParameterIDs is Chrome 146's fixed transport parameter set.
// The GREASE parameter is not here: its ID is drawn per connection and is
// checked separately.
//
// Transcribed from uquic's QUICChrome_146_IPv4. version_information appears as
// 0xff73db, not the RFC 9368 codepoint 0x11, because the parameter is sent with
// LegacyID set — as Chrome does.
var chrome146TransportParameterIDs = map[uint64]string{
	0x01:     "max_idle_timeout",
	0x03:     "max_udp_payload_size",
	0x04:     "initial_max_data",
	0x05:     "initial_max_stream_data_bidi_local",
	0x06:     "initial_max_stream_data_bidi_remote",
	0x07:     "initial_max_stream_data_uni",
	0x08:     "initial_max_streams_bidi",
	0x09:     "initial_max_streams_uni",
	0x0f:     "initial_source_connection_id",
	0x20:     "max_datagram_frame_size",
	0xff73db: "version_information (legacy codepoint)",
	0x3127:   "google_initial_rtt",
	0x3128:   "google_connection_options",
}

// isGREASETransportParameterID reports whether id is one of the IDs RFC 9000
// §18.1 reserves for GREASE: 31*N+27.
func isGREASETransportParameterID(id uint64) bool {
	return id >= 27 && (id-27)%31 == 0
}

func quicTransportParametersExtension(t *testing.T, spec *tls.ClientHelloSpec) *tls.QUICTransportParametersExtension {
	t.Helper()
	var found *tls.QUICTransportParametersExtension
	for _, ext := range spec.Extensions {
		if tp, ok := ext.(*tls.QUICTransportParametersExtension); ok {
			if found != nil {
				t.Fatal("spec has more than one quic_transport_parameters extension; kuic writes into the first it finds, so the other would go out with the spec author's values")
			}
			found = tp
		}
	}
	if found == nil {
		t.Fatal("spec has no quic_transport_parameters extension; kuic rejects the dial (ErrNoQUICTransportParametersExtension)")
	}
	return found
}

// TestChromeH3Spec_TransportParameterSet pins the parameter set against the
// uquic parrot. A dropped Google-private parameter, or an extra one, changes
// the QUIC fingerprint while leaving every connection working.
func TestChromeH3Spec_TransportParameterSet(t *testing.T) {
	spec, err := buildChromeH3Spec()
	if err != nil {
		t.Fatalf("buildChromeH3Spec: %v", err)
	}

	seen := make(map[uint64]int)
	greaseCount := 0
	for _, p := range quicTransportParametersExtension(t, spec).TransportParameters {
		id := p.ID()
		if isGREASETransportParameterID(id) {
			greaseCount++
			continue
		}
		seen[id]++
	}

	if greaseCount != 1 {
		t.Errorf("GREASE transport parameters = %d, want exactly 1; Chrome sends one and a fingerprinter counts them", greaseCount)
	}
	for id, name := range chrome146TransportParameterIDs {
		switch seen[id] {
		case 1:
		case 0:
			t.Errorf("missing transport parameter 0x%x (%s)", id, name)
		default:
			t.Errorf("transport parameter 0x%x (%s) appears %d times, want 1", id, name, seen[id])
		}
	}
	for id := range seen {
		if _, ok := chrome146TransportParameterIDs[id]; !ok {
			t.Errorf("unexpected transport parameter 0x%x — Chrome 146 does not send it", id)
		}
	}
}

// TestChromeH3Spec_CarriesECHPlaceholder pins the requirement with the quietest
// failure of all: utls needs an extension implementing
// EncryptedClientHelloExtension to have somewhere to put the real ECH
// extension. Without one it does not error — it sends a ClientHello with no
// ECH, and the server answers missing_extension. Every k2v5 tunnel uses ECH.
func TestChromeH3Spec_CarriesECHPlaceholder(t *testing.T) {
	spec, err := buildChromeH3Spec()
	if err != nil {
		t.Fatalf("buildChromeH3Spec: %v", err)
	}
	for _, ext := range spec.Extensions {
		if _, ok := ext.(tls.EncryptedClientHelloExtension); ok {
			return
		}
	}
	t.Fatal("spec has no utls.EncryptedClientHelloExtension; ECH would be silently dropped from the ClientHello")
}

// TestChromeH3Spec_IsAFreshSpecPerCall pins the property that makes the field a
// factory rather than a value. kuic writes this connection's transport
// parameters — including initial_source_connection_id — into the returned
// spec. Two dials sharing one spec would overwrite each other's connection ID,
// and RFC 9000 §7.3 makes that a TRANSPORT_PARAMETER_ERROR for whichever loses
// the race: a corruption that only appears under concurrency.
func TestChromeH3Spec_IsAFreshSpecPerCall(t *testing.T) {
	first, err := buildChromeH3Spec()
	if err != nil {
		t.Fatalf("buildChromeH3Spec: %v", err)
	}
	second, err := buildChromeH3Spec()
	if err != nil {
		t.Fatalf("buildChromeH3Spec: %v", err)
	}
	if first == second {
		t.Fatal("buildChromeH3Spec returned the same spec twice")
	}

	firstTP := quicTransportParametersExtension(t, first)
	secondTP := quicTransportParametersExtension(t, second)
	if firstTP == secondTP {
		t.Fatal("both specs share one quic_transport_parameters extension; concurrent dials would overwrite each other's initial_source_connection_id")
	}

	// Write through one the way kuic does, and require the other unmoved.
	firstTP.TransportParameters = nil
	if len(secondTP.TransportParameters) == 0 {
		t.Fatal("clearing one spec's transport parameters emptied the other's — the specs share backing state")
	}
}

// TestChromeH3Spec_CipherSuitesAreTLS13Only pins the difference between
// Chrome's QUIC ClientHello and its TCP one. The TCP hello leads with a GREASE
// cipher and offers TLS 1.2 suites; the QUIC hello offers three TLS 1.3 suites
// and nothing else. Reaching for one of utls's built-in Chrome presets here —
// they are all TCP — would trip this.
func TestChromeH3Spec_CipherSuitesAreTLS13Only(t *testing.T) {
	spec, err := buildChromeH3Spec()
	if err != nil {
		t.Fatalf("buildChromeH3Spec: %v", err)
	}
	want := []uint16{
		tls.TLS_AES_128_GCM_SHA256,
		tls.TLS_AES_256_GCM_SHA384,
		tls.TLS_CHACHA20_POLY1305_SHA256,
	}
	if len(spec.CipherSuites) != len(want) {
		t.Fatalf("cipher suites = %#x, want %#x", spec.CipherSuites, want)
	}
	for i, id := range want {
		if spec.CipherSuites[i] != id {
			t.Errorf("cipher suite %d = %#x, want %#x", i, spec.CipherSuites[i], id)
		}
	}
	if spec.TLSVersMin != tls.VersionTLS13 || spec.TLSVersMax != tls.VersionTLS13 {
		t.Errorf("TLS version range = [%#x, %#x], want TLS 1.3 only", spec.TLSVersMin, spec.TLSVersMax)
	}
}
