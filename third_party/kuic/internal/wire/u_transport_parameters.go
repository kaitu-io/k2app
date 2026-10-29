package wire

// Ported from refraction-networking/uquic (BSD-3-Clause), adapted to
// metacubex/utls types.

import (
	"time"

	tls "github.com/metacubex/utls"

	"github.com/kaitu-io/kuic/internal/protocol"
)

// PopulateFromClientHelloSpec makes the ClientHelloSpec's transport parameters
// authoritative for this connection: it copies the spec's values into the
// fields quic-go reasons about, and records the spec's exact serialization in
// ClientOverride so Marshal reproduces it byte for byte.
//
// The direction matters and is the opposite of the obvious one. Letting
// quic-go's own parameters win would put quic-go's set on the wire — no
// version_information, no google_initial_rtt, no google_connection_options, a
// max_ack_delay Chrome does not send, and quic-go's canonical ordering instead
// of Chrome's per-connection shuffle. A ClientHello shaped like Chrome whose
// transport parameters are unmistakably quic-go's is its own fingerprint, and a
// rarer one than either half alone.
//
// The single exception is initial_source_connection_id. RFC 9000 §7.3 requires
// it to equal the packet's Source Connection ID, which no spec author can know
// in advance, so a spec carries a zero-length placeholder and this fills in the
// real value — writing it back into the slice, because that slice is what goes
// on the wire. A spec that does specify one is left alone: that is a caller
// deliberately pinning the connection ID.
//
// tp is expected to arrive with InitialSourceConnectionID already set and
// everything else zero; the spec is the source for the rest. Parameters quic-go
// has no field for stay in ClientOverride only, which is where they need to be.
func (tp *TransportParameters) PopulateFromClientHelloSpec(params tls.TransportParameters) {
	for i, param := range params {
		switch param.ID() {
		case uint64(maxIdleTimeoutParameterID):
			tp.MaxIdleTimeout = time.Duration(param.(tls.MaxIdleTimeout)) * time.Millisecond
		case uint64(initialMaxDataParameterID):
			tp.InitialMaxData = protocol.ByteCount(param.(tls.InitialMaxData))
		case uint64(initialMaxStreamDataBidiLocalParameterID):
			tp.InitialMaxStreamDataBidiLocal = protocol.ByteCount(param.(tls.InitialMaxStreamDataBidiLocal))
		case uint64(initialMaxStreamDataBidiRemoteParameterID):
			tp.InitialMaxStreamDataBidiRemote = protocol.ByteCount(param.(tls.InitialMaxStreamDataBidiRemote))
		case uint64(initialMaxStreamDataUniParameterID):
			tp.InitialMaxStreamDataUni = protocol.ByteCount(param.(tls.InitialMaxStreamDataUni))
		case uint64(initialMaxStreamsBidiParameterID):
			tp.MaxBidiStreamNum = protocol.StreamNum(param.(tls.InitialMaxStreamsBidi))
		case uint64(initialMaxStreamsUniParameterID):
			tp.MaxUniStreamNum = protocol.StreamNum(param.(tls.InitialMaxStreamsUni))
		case uint64(maxAckDelayParameterID):
			tp.MaxAckDelay = time.Duration(param.(tls.MaxAckDelay)) * time.Millisecond
		case uint64(disableActiveMigrationParameterID):
			tp.DisableActiveMigration = true
		case uint64(activeConnectionIDLimitParameterID):
			tp.ActiveConnectionIDLimit = uint64(param.(tls.ActiveConnectionIDLimit))
		case uint64(maxUDPPayloadSizeParameterID):
			tp.MaxUDPPayloadSize = protocol.ByteCount(param.(tls.MaxUDPPayloadSize))
		case uint64(maxDatagramFrameSizeParameterID):
			tp.MaxDatagramFrameSize = protocol.ByteCount(param.(tls.MaxDatagramFrameSize))
		case uint64(initialSourceConnectionIDParameterID):
			scid, ok := param.(tls.InitialSourceConnectionID)
			if !ok {
				continue
			}
			if len(scid) > 0 {
				// The spec pins a connection ID. Honour it.
				tp.InitialSourceConnectionID = protocol.ParseConnectionID(scid)
				continue
			}
			// The placeholder case: write this connection's real SCID back into
			// the slice, which is the copy that gets marshaled.
			params[i] = tls.InitialSourceConnectionID(tp.InitialSourceConnectionID.Bytes())
		default:
			// Unknown to quic-go — Google-private parameters, GREASE,
			// version_information. They have no field here and need none: they
			// travel in ClientOverride below.
		}
	}

	tp.ClientOverride = params.Marshal()
}
