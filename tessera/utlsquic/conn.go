// Package utlsquic drives a QUIC handshake with uTLS instead of crypto/tls, so
// that the ClientHello inside the Initial packet carries a real browser
// fingerprint.
//
// # Why
//
// On QUIC the ClientHello rides in the Initial packet, and Initial packets are
// protected with a key derived from the cleartext Destination Connection ID
// (RFC 9001 §5.2). Any on-path observer can therefore decrypt them and read the
// ClientHello -- the GFW has been doing this at scale since 2024-04. Go's
// standard library emits a ClientHello that no browser produces, so a QUIC
// client built on it is identifiable regardless of what SNI it presents.
//
// # How it plugs in
//
// quic-go builds its TLS engine as a concrete *tls.QUICConn, so there is no
// stock seam to hook. This package targets the patched fork (see ../patches),
// which names that dependency as an interface and lets a caller supply it per
// connection via quic.Config.ClientTLSConnFactory.
//
// # Deliberate limitations
//
//   - Session resumption is off. utls.UQUICConn has no StoreSession, so this
//     package leaves EnableSessionEvents unset; QUICStoreSession never fires.
//     0-RTT is therefore unavailable.
//   - Client certificates are not translated.
//   - Config fields that the ClientHelloID would silently override (CipherSuites,
//     CurvePreferences) are rejected rather than ignored -- see translateConfig.
package utlsquic

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

	quic "github.com/apernet/quic-go"
	"github.com/apernet/quic-go/quicvarint"
	utls "github.com/metacubex/utls"
)

// Factory returns a quic.ClientTLSConnFactory that hands each client handshake
// to uTLS using the given browser preset. Install it on quic.Config:
//
//	&quic.Config{ClientTLSConnFactory: utlsquic.Factory(utls.HelloChrome_120)}
func Factory(id utls.ClientHelloID) quic.ClientTLSConnFactory {
	return func(qc *tls.QUICConfig) quic.TLSConn { return New(qc, id) }
}

// Conn adapts *utls.UQUICConn to the stdlib-typed interface quic-go drives.
type Conn struct {
	inner *utls.UQUICConn
	// tp is the quic_transport_parameters extension inside the ClientHello.
	// uTLS wires SetTransportParameters only to its own QUIC path, not to a
	// preset-built hello, so this package fills the extension itself.
	tp *utls.QUICTransportParametersExtension
	// err defers a construction failure to Start. The factory signature has
	// nowhere to return one, and reporting it at Start is better than handing
	// back a nil engine that panics later.
	err error
}

// New builds a uTLS-backed engine from the QUIC config quic-go assembled.
func New(qc *tls.QUICConfig, id utls.ClientHelloID) *Conn {
	if qc == nil || qc.TLSConfig == nil {
		return &Conn{err: errors.New("utlsquic: nil TLS config")}
	}
	uc, err := translateConfig(qc.TLSConfig)
	if err != nil {
		return &Conn{err: err}
	}
	spec, tp, err := specFor(id, uc.NextProtos)
	if err != nil {
		return &Conn{err: err}
	}
	// EnableSessionEvents stays false: utls.UQUICConn cannot StoreSession.
	inner := utls.UQUICClient(&utls.QUICConfig{TLSConfig: uc}, utls.HelloCustom)
	if err := inner.ApplyPreset(spec); err != nil {
		return &Conn{err: fmt.Errorf("utlsquic: apply %s preset: %w", id.Client, err)}
	}
	return &Conn{inner: inner, tp: tp}
}

// specFor loads a browser preset and retargets its ALPN list at the protocols
// the caller asked for.
//
// The presets are captures of a browser's TCP handshake, so their ALPN says
// "h2, http/1.1". Left alone that is not merely wrong-looking: a QUIC server
// offering only h3 aborts the handshake with "no application protocol". Real
// Chrome over QUIC sends h3, so overriding here is both the working choice and
// the faithful one.
//
// What this does NOT fix: the rest of the preset is still a TCP capture. A real
// Chrome QUIC ClientHello also differs in extension set and ordering, which no
// uTLS preset currently models. Closing that gap needs a capture of Chrome's own
// QUIC handshake to diff against -- see docs/tessera/spec.md §10.
// It also installs the quic_transport_parameters extension, which the TCP
// presets have no reason to carry and whose absence a QUIC server rejects
// outright ("missing extension"). The returned pointer is that extension;
// SetTransportParameters fills it in later, once quic-go supplies the values.
func specFor(id utls.ClientHelloID, alpn []string) (*utls.ClientHelloSpec, *utls.QUICTransportParametersExtension, error) {
	spec, err := utls.UTLSIdToSpec(id)
	if err != nil {
		return nil, nil, fmt.Errorf("utlsquic: no spec for %s %s: %w", id.Client, id.Version, err)
	}
	var sawALPN, sawVersions bool
	for _, ext := range spec.Extensions {
		switch e := ext.(type) {
		case *utls.ALPNExtension:
			e.AlpnProtocols = alpn
			sawALPN = true
		case *utls.SupportedVersionsExtension:
			e.Versions = tls13Only(e.Versions)
			sawVersions = true
		}
	}
	if !sawALPN {
		// Proceeding would send the preset's own ALPN, and the failure would
		// surface much later as an opaque handshake error.
		return nil, nil, fmt.Errorf("utlsquic: %s preset has no ALPN extension to retarget", id.Client)
	}
	if !sawVersions {
		return nil, nil, fmt.Errorf("utlsquic: %s preset has no supported_versions extension", id.Client)
	}
	spec.TLSVersMin = utls.VersionTLS13
	spec.TLSVersMax = utls.VersionTLS13

	tp := &utls.QUICTransportParametersExtension{}
	spec.Extensions = insertBeforePadding(spec.Extensions, tp)
	return &spec, tp, nil
}

// insertBeforePadding places ext last, except that a trailing padding extension
// stays last -- padding measures everything ahead of it, so anything appended
// after it would push the ClientHello past the size the preset was padding to.
func insertBeforePadding(exts []utls.TLSExtension, ext utls.TLSExtension) []utls.TLSExtension {
	if n := len(exts); n > 0 {
		if _, isPad := exts[n-1].(*utls.UtlsPaddingExtension); isPad {
			out := make([]utls.TLSExtension, 0, n+1)
			out = append(out, exts[:n-1]...)
			out = append(out, ext, exts[n-1])
			return out
		}
	}
	return append(exts, ext)
}

// rawTransportParameter re-emits one already-encoded QUIC transport parameter.
//
// quic-go hands over the whole parameter block pre-marshaled, while uTLS wants a
// list it can marshal itself. Splitting the block and handing each piece back
// verbatim keeps quic-go the sole author of the values -- inventing them here
// would mean two sources of truth for the connection's limits.
type rawTransportParameter struct {
	id    uint64
	value []byte
}

func (p rawTransportParameter) ID() uint64    { return p.id }
func (p rawTransportParameter) Value() []byte { return p.value }

// parseTransportParameters splits a marshaled parameter block into its
// id/length/value triples (RFC 9000 §18).
func parseTransportParameters(b []byte) (utls.TransportParameters, error) {
	var out utls.TransportParameters
	for len(b) > 0 {
		id, n, err := quicvarint.Parse(b)
		if err != nil {
			return nil, fmt.Errorf("utlsquic: transport parameter id: %w", err)
		}
		b = b[n:]
		size, n, err := quicvarint.Parse(b)
		if err != nil {
			return nil, fmt.Errorf("utlsquic: transport parameter 0x%x length: %w", id, err)
		}
		b = b[n:]
		if uint64(len(b)) < size {
			return nil, fmt.Errorf("utlsquic: transport parameter 0x%x truncated: want %d, have %d",
				id, size, len(b))
		}
		out = append(out, rawTransportParameter{id: id, value: b[:size]})
		b = b[size:]
	}
	return out, nil
}

// tls13Only strips pre-1.3 offers from a supported_versions list, preserving
// GREASE placeholders and order.
//
// RFC 9001 §4.2 requires it -- "clients MUST NOT offer TLS versions older than
// 1.3" on QUIC -- and the browser presets violate it, because over TCP Chrome
// really does offer 1.2. Leaving it in is a double cost: uTLS refuses to start a
// QUIC handshake whose MinVersion is below 1.3, and an offer no QUIC client
// makes would stand out in exactly the packet a censor decrypts.
func tls13Only(vs []uint16) []uint16 {
	out := make([]uint16, 0, len(vs))
	for _, v := range vs {
		if v >= utls.VersionTLS13 || isGREASE(v) {
			out = append(out, v)
		}
	}
	return out
}

// isGREASE reports whether v is a reserved GREASE value (RFC 8701): 0x?A?A with
// both bytes equal.
func isGREASE(v uint16) bool { return v&0x0f0f == 0x0a0a && byte(v>>8) == byte(v) }

// translateConfig converts a crypto/tls client config into the uTLS one.
//
// utls is a fork of crypto/tls, so the two Config types are structurally alike
// but formally unrelated; every field has to be carried across by hand. The
// danger in hand-copying is not a compile error, it is a silent security
// downgrade -- dropping RootCAs or InsecureSkipVerify changes what gets
// accepted without changing what compiles. So anything a caller set that this
// function would silently violate is rejected instead of ignored.
func translateConfig(c *tls.Config) (*utls.Config, error) {
	switch {
	case len(c.CipherSuites) > 0:
		// The ClientHelloID dictates the cipher list; honoring both is impossible.
		return nil, errors.New("utlsquic: CipherSuites is set but the browser preset governs it")
	case len(c.CurvePreferences) > 0:
		return nil, errors.New("utlsquic: CurvePreferences is set but the browser preset governs it")
	case c.VerifyConnection != nil:
		// Takes a tls.ConnectionState; uTLS would hand it a utls.ConnectionState.
		return nil, errors.New("utlsquic: VerifyConnection is not translatable")
	case c.GetClientCertificate != nil || len(c.Certificates) > 0:
		return nil, errors.New("utlsquic: client certificates are not supported")
	case c.ClientSessionCache != nil:
		return nil, errors.New("utlsquic: session resumption is not supported")
	case len(c.EncryptedClientHelloConfigList) > 0:
		return nil, errors.New("utlsquic: ECH is not supported")
	}
	return &utls.Config{
		Rand:                  c.Rand,
		Time:                  c.Time,
		ServerName:            c.ServerName,
		NextProtos:            c.NextProtos,
		RootCAs:               c.RootCAs,
		InsecureSkipVerify:    c.InsecureSkipVerify,
		VerifyPeerCertificate: c.VerifyPeerCertificate,
		KeyLogWriter:          c.KeyLogWriter,
		MinVersion:            c.MinVersion,
		MaxVersion:            c.MaxVersion,
	}, nil
}

func (c *Conn) Start(ctx context.Context) error {
	if c.err != nil {
		return c.err
	}
	return c.inner.Start(ctx)
}

// NextEvent maps uTLS events onto the stdlib ones.
//
// The two enums are currently identical in value, so a numeric cast would work
// today. It is not used: if uTLS ever inserts a kind, a cast would silently
// relabel one event as another -- and these events carry the traffic secrets,
// so a mislabel is a silent key mix-up. An explicit switch fails loudly instead.
func (c *Conn) NextEvent() tls.QUICEvent {
	ev := c.inner.NextEvent()
	out := tls.QUICEvent{Data: ev.Data, Suite: ev.Suite}
	switch ev.Kind {
	case utls.QUICNoEvent:
		return tls.QUICEvent{Kind: tls.QUICNoEvent}
	case utls.QUICSetReadSecret:
		out.Kind = tls.QUICSetReadSecret
	case utls.QUICSetWriteSecret:
		out.Kind = tls.QUICSetWriteSecret
	case utls.QUICWriteData:
		out.Kind = tls.QUICWriteData
	case utls.QUICTransportParameters:
		out.Kind = tls.QUICTransportParameters
	case utls.QUICTransportParametersRequired:
		out.Kind = tls.QUICTransportParametersRequired
	case utls.QUICRejectedEarlyData:
		out.Kind = tls.QUICRejectedEarlyData
	case utls.QUICHandshakeDone:
		out.Kind = tls.QUICHandshakeDone
	default:
		// Includes QUICResumeSession/QUICStoreSession, which carry a
		// *utls.SessionState that has no stdlib counterpart. They cannot fire
		// while EnableSessionEvents is unset; reaching here means uTLS changed.
		panic(fmt.Sprintf("utlsquic: unmapped uTLS event kind %d", ev.Kind))
	}
	out.Level = level(ev.Level)
	return out
}

func level(l utls.QUICEncryptionLevel) tls.QUICEncryptionLevel {
	switch l {
	case utls.QUICEncryptionLevelInitial:
		return tls.QUICEncryptionLevelInitial
	case utls.QUICEncryptionLevelEarly:
		return tls.QUICEncryptionLevelEarly
	case utls.QUICEncryptionLevelHandshake:
		return tls.QUICEncryptionLevelHandshake
	case utls.QUICEncryptionLevelApplication:
		return tls.QUICEncryptionLevelApplication
	default:
		panic(fmt.Sprintf("utlsquic: unmapped uTLS encryption level %d", l))
	}
}

func (c *Conn) HandleData(l tls.QUICEncryptionLevel, data []byte) error {
	var ul utls.QUICEncryptionLevel
	switch l {
	case tls.QUICEncryptionLevelInitial:
		ul = utls.QUICEncryptionLevelInitial
	case tls.QUICEncryptionLevelEarly:
		ul = utls.QUICEncryptionLevelEarly
	case tls.QUICEncryptionLevelHandshake:
		ul = utls.QUICEncryptionLevelHandshake
	case tls.QUICEncryptionLevelApplication:
		ul = utls.QUICEncryptionLevelApplication
	default:
		return fmt.Errorf("utlsquic: unmapped encryption level %d", l)
	}
	return c.inner.HandleData(ul, data)
}

// SetTransportParameters records quic-go's parameters both in uTLS's QUIC state
// and in the ClientHello extension, because uTLS only does the former when the
// hello came from a preset (see the Conn.tp comment). A parse failure is held
// until Start, which is the first method that can report one; quic-go always
// calls this before Start.
func (c *Conn) SetTransportParameters(p []byte) {
	if c.inner != nil {
		c.inner.SetTransportParameters(p)
	}
	if c.tp == nil || c.err != nil {
		return
	}
	params, err := parseTransportParameters(p)
	if err != nil {
		c.err = err
		return
	}
	c.tp.TransportParameters = params
}

func (c *Conn) ConnectionState() tls.ConnectionState {
	s := c.inner.ConnectionState()
	// CurveID has no uTLS counterpart and is left zero; quic-go does not read it.
	return tls.ConnectionState{
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
	}
}

// SendSessionTicket is server-only; this engine is a client.
func (c *Conn) SendSessionTicket(tls.QUICSessionTicketOptions) error {
	return errors.New("utlsquic: SendSessionTicket on a client connection")
}

// StoreSession is unreachable while EnableSessionEvents is unset (see New).
func (c *Conn) StoreSession(*tls.SessionState) error {
	return errors.New("utlsquic: session resumption is disabled")
}

func (c *Conn) Close() error {
	if c.inner == nil {
		return nil
	}
	return c.inner.Close()
}

// Compile-time proof that the adapter satisfies what quic-go drives.
var _ quic.TLSConn = (*Conn)(nil)
