package handshake

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	tls "github.com/metacubex/utls"

	"github.com/kaitu-io/kuic/quicvarint"
)

// quicTLSConn is the slice of the TLS QUIC API that cryptoSetup drives. It
// exists so the client can run on either the plain utls QUIC connection or the
// fingerprint-shaping one, which are separate concrete types with no common
// ancestor.
type quicTLSConn interface {
	Start(context.Context) error
	NextEvent() tls.QUICEvent
	HandleData(tls.QUICEncryptionLevel, []byte) error
	SetTransportParameters([]byte)
	SendSessionTicket(tls.QUICSessionTicketOptions) error
	StoreSession(*tls.SessionState) error
	ConnectionState() tls.ConnectionState
	Close() error
}

var (
	_ quicTLSConn = (*tls.QUICConn)(nil)
	_ quicTLSConn = (*fingerprintedConn)(nil)
)

// ErrNoQUICTransportParametersExtension is returned when a ClientHelloSpec has
// no quic_transport_parameters extension to carry the connection's real
// parameters. Such a spec cannot produce a usable QUIC ClientHello.
var ErrNoQUICTransportParametersExtension = errors.New(
	"kuic: ClientHelloSpec has no *utls.QUICTransportParametersExtension; " +
		"a QUIC ClientHello must carry quic_transport_parameters (id 57)")

// fingerprintedConn adapts utls's fingerprint-shaping QUIC connection to
// quicTLSConn.
//
// It exists to solve one specific problem. utls builds the ClientHello from the
// ClientHelloSpec's extension list, and its QUICTransportParametersExtension
// marshals from its own structured TransportParameters field —
// UQUICConn.SetTransportParameters only records the bytes for internal use and
// deliberately does NOT write them into the ClientHello (see the commented-out
// block in utls u_quic.go SetTransportParameters).
//
// Left alone, that means the ClientHello advertises whatever transport
// parameters the spec was authored with instead of the ones this connection
// actually negotiated — including initial_source_connection_id, which RFC 9000
// section 7.3 requires to equal the packet's Source Connection ID. A mismatch
// is a TRANSPORT_PARAMETER_ERROR from any conformant server, and shows up as a
// handshake that dies right after the ClientHello.
//
// So SetTransportParameters below also decodes quic-go's marshaled parameters
// and writes them into the spec's extension, which is the copy the ClientHello
// is actually built from.
type fingerprintedConn struct {
	*tls.UQUICConn

	// tpExt is the spec's quic_transport_parameters extension. ApplyPreset
	// copies the spec's extension list by interface value, so this pointer is
	// the same object the ClientHello marshals from.
	tpExt *tls.QUICTransportParametersExtension
}

// newFingerprintedConn builds a QUIC client connection whose ClientHello is
// shaped to spec.
//
// spec must be freshly built for this connection and not shared with any other:
// the transport parameters written into it are connection-specific, so a shared
// spec would have concurrent dials overwrite each other's
// initial_source_connection_id. Config.ClientHelloSpec is a factory for exactly
// this reason.
func newFingerprintedConn(qconf *tls.QUICConfig, spec *tls.ClientHelloSpec) (*fingerprintedConn, error) {
	tpExt := findQUICTransportParametersExtension(spec)
	if tpExt == nil {
		return nil, ErrNoQUICTransportParametersExtension
	}

	uconn := tls.UQUICClient(qconf, tls.HelloCustom)
	if err := uconn.ApplyPreset(spec); err != nil {
		return nil, fmt.Errorf("kuic: apply ClientHelloSpec: %w", err)
	}
	return &fingerprintedConn{UQUICConn: uconn, tpExt: tpExt}, nil
}

// SetTransportParameters records params for utls's internal use and, crucially,
// writes them into the ClientHello's quic_transport_parameters extension. See
// the type doc for why both are needed.
func (c *fingerprintedConn) SetTransportParameters(params []byte) {
	c.tpExt.TransportParameters = decodeTransportParameters(params)
	c.UQUICConn.SetTransportParameters(params)
}

// StoreSession is never reached: utls's UQUICClient ignores
// QUICConfig.EnableSessionEvents, so the fingerprinted handshake emits no
// QUICStoreSession event. utls also offers no way to store a session on this
// path — ClientSessionCache insertion needs package-internal state.
func (c *fingerprintedConn) StoreSession(*tls.SessionState) error {
	return errors.New("kuic: session storage is unavailable on the uTLS client path")
}

// findQUICTransportParametersExtension returns the spec's
// quic_transport_parameters extension, or nil if it has none.
func findQUICTransportParametersExtension(spec *tls.ClientHelloSpec) *tls.QUICTransportParametersExtension {
	if spec == nil {
		return nil
	}
	for _, ext := range spec.Extensions {
		if tpExt, ok := ext.(*tls.QUICTransportParametersExtension); ok {
			return tpExt
		}
	}
	return nil
}

// rawTransportParameter carries one already-encoded transport parameter through
// utls's TransportParameter interface, whose Marshal writes
// varint(ID) || varint(len(Value)) || Value.
type rawTransportParameter struct {
	id    uint64
	value []byte
}

func (p rawTransportParameter) ID() uint64    { return p.id }
func (p rawTransportParameter) Value() []byte { return p.value }

// decodeTransportParameters splits quic-go's marshaled transport parameters
// back into individual (id, value) pairs so utls can re-marshal them in the
// same order with the same bytes.
//
// It is deliberately total: a parameter list quic-go produced is well-formed by
// construction, and a truncated tail would mean a bug upstream of here, not a
// condition worth failing the dial over. Anything unparseable is dropped, which
// surfaces as a transport parameter the server never sees — loud, and far
// easier to diagnose than a silently reordered list.
func decodeTransportParameters(b []byte) tls.TransportParameters {
	var params tls.TransportParameters
	r := bytes.NewReader(b)
	for {
		id, err := quicvarint.Read(r)
		if err != nil {
			return params
		}
		l, err := quicvarint.Read(r)
		if err != nil {
			return params
		}
		val := make([]byte, l)
		if _, err := io.ReadFull(r, val); err != nil {
			return params
		}
		params = append(params, rawTransportParameter{id: id, value: val})
	}
}
