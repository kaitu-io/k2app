# Maintaining kuic

kuic is a thin fork of [apernet/quic-go](https://github.com/apernet/quic-go)
(itself a fork of quic-go, carrying pluggable congestion control) whose only
purpose is to let the **client** shape its TLS ClientHello to a chosen
fingerprint while keeping real ECH and pluggable congestion control.

Everything else should stay as close to upstream as possible. Every line that
diverges is a line to re-apply on every rebase.

## What actually differs from apernet/quic-go

1. **The TLS engine is `github.com/metacubex/utls`, not `crypto/tls`.**
   The import was swapped wholesale rather than bridged. utls is a crypto/tls
   fork with `QUICClient`, `QUICServer` and both sides of ECH, so one type
   universe covers client, server, relay and P2P. The alternative — keeping
   stdlib at the API boundary and converting to `utls.Config` inside the
   handshake — needs a hand-written struct copy, which silently drops any
   field either side adds later. Don't reintroduce it.

2. **`Config.NewClientHelloSpec`** (interface.go). Non-nil makes the client
   handshake go through `utls.UQUICClient` + `ApplyPreset`. Nil is the ordinary
   path.

3. **`internal/handshake/u_clienthello.go`** — the seam. See the next section.

4. **`http3/u_connstate.go`** — the only place that must still speak stdlib
   `crypto/tls`, because `net/http` types `Response.TLS`, `Request.TLS` and
   `httptrace.TLSHandshakeDone` and cannot be changed. Guarded by a reflection
   test.

5. **`internal/qtls/cipher_suite.go` and `internal/handshake/hkdf_test.go`**
   linknames point at `github.com/metacubex/utls`, not `crypto/tls`. After the
   engine swap the crypto/tls targets were both a link error and a meaningless
   assertion.

6. **`integrationtests/self/handshake_drop_test.go`** no longer asserts which
   key exchange group was negotiated. utls keeps it in an unexported field with
   no equivalent of crypto/tls's `ConnectionState.CurveID`. Upstream's
   `self_go12{4,5}_test.go` helpers were deleted rather than stubbed — a stub
   returning 0 just made the caller fail.

## The spec's transport parameters are authoritative

When a dial carries a `ClientHelloSpec`, the spec's `quic_transport_parameters`
are what goes on the wire — values, order, and all. `Config`'s flow-control
settings do not override them. `wire.PopulateFromClientHelloSpec` copies the
spec's values into quic-go's own `TransportParameters` so its flow control
agrees with what it advertised, and stashes the exact serialization in
`ClientOverride`, which `Marshal` returns verbatim.

The direction is deliberate and was reversed once. Letting quic-go win puts
quic-go's parameter set on the wire under a Chrome-shaped ClientHello: no
`version_information`, no `google_initial_rtt`, no `google_connection_options`,
a `max_ack_delay` Chrome never sends, and quic-go's canonical ordering instead
of Chrome's per-connection shuffle. That combination is a rarer fingerprint than
either half alone.

**Consequences for anyone writing a spec:**

- **A spec must carry real transport parameter values.** An empty
  `&utls.QUICTransportParametersExtension{}` used to be normal — kuic filled it
  in. Now it means a connection advertising no flow control at all.
- **`initial_source_connection_id` is the one exception**, and the only one
  kuic still writes. RFC 9000 §7.3 requires it to equal the packet's Source
  Connection ID, which no spec author can know. Include it with a zero-length
  value as a placeholder; a non-empty value is honoured as a deliberate pin.
- **Parameters quic-go has no field for pass through untouched** — that is the
  point. They live only in `ClientOverride`.
- **Flow control comes from the spec.** A Chrome spec brings Chrome's windows
  (6 MB stream / 15 MB connection), overriding whatever the caller tuned. On
  memory-constrained platforms that is a real change, not a cosmetic one.

The default client transport hides the connection-ID back-fill: quic-go dials
with a zero-length source connection ID (as does Chrome), so a back-fill that
never ran looks identical to one that did.
`TestFingerprintSpecBackfillsSourceConnectionID` forces an 8-byte connection ID
precisely to separate them. Don't delete it.

## The transport-parameters trap

This is the part that is easy to get wrong and produces a handshake that dies
right after the ClientHello.

This section describes the mechanism the section above rests on, and the trap
an earlier version of this fork fell into.

utls's `UQUICConn.SetTransportParameters` records the parameters internally but
deliberately does **not** write them into the ClientHello when a preset is
applied — there is a commented-out block in utls `u_quic.go` where that used to
happen. The ClientHello's `quic_transport_parameters` extension instead marshals
from the spec's own `QUICTransportParametersExtension.TransportParameters`.

Left alone, the server reads the spec author's parameters rather than this
connection's, including `initial_source_connection_id`, which RFC 9000 §7.3
requires to equal the packet's Source Connection ID. Any conformant server
answers `TRANSPORT_PARAMETER_ERROR`.

That is exactly why the spec is authoritative: the ClientHello is built from
the spec's extension, so the spec is where the parameters have to be correct.
`ApplyPreset` copies the extension list by interface value, so writing the real
connection ID into the spec's extension before the dial reaches it — which
`PopulateFromClientHelloSpec` does — reaches the object the ClientHello
marshals from.

kuic briefly did the opposite, decoding quic-go's marshaled parameters and
overwriting the spec's extension with them. That handshakes perfectly well and
throws Chrome's parameters away. Don't.

**ECH takes a different route to the parameters, so keep both cases.** With
ECH the server reads them from the decrypted *inner* ClientHello, which utls
builds from the bytes `UQUICConn.SetTransportParameters` stashed internally;
without ECH the spec's extension is the only source. Both paths have to work,
and a suite covering only one has repeatedly gone green on a broken other. Back
when kuic overwrote the spec instead, deleting that overwrite left every ECH
test passing and only `TestFingerprintSpecWithoutECH_Handshakes` red.

There is also a hazard with no alarm of its own:
`QUICTransportParametersExtension` caches its marshaled bytes in an unexported
`marshalResult` on the first `Len()` call, and kuic cannot reset it
(`UQUICConn.conn` is unexported, so the extension list is out of reach). Today
`Len()` runs after `PopulateFromClientHelloSpec` and all is well. If a utls
bump ever measured the ClientHello first, the connection ID back-fill would
stop reaching the wire. If the fingerprint tests go red after a utls bump, look
here before looking anywhere else.

Two consequences to preserve:

- **`NewClientHelloSpec` is a factory, not a value.** This connection's source
  connection ID is written into the spec. A shared spec has concurrent dials
  overwrite each other's.
- **The spec must contain a `*utls.QUICTransportParametersExtension`.** Missing
  it fails the dial (`ErrNoQUICTransportParametersExtension`) rather than
  falling back to an unshaped handshake.

## The Chrome-146 Initial framing port

`Config.InitialPacketSpec` shapes the QUIC packet layer around the ClientHello:
the destination connection ID length, the first packet number and its per-packet
encoding lengths, and — the packet-layer half — a `FrameBuilder` that re-cuts the
Initial CRYPTO stream. Chrome sends its ClientHello as many small CRYPTO frames
interleaved with PING and PADDING, padded to ~1231 bytes per datagram; quic-go
sends one big CRYPTO frame per packet. `&QUICRandomFrames{...}` reproduces the
former (`u_quic_frames.go`, ported from uQUIC minus the `clienthellod`
dependency and the flight-layout builders Chrome does not need).

`uPacketPacker` (`u_packet_packer.go`) is where the builder is applied. It is
**not** a re-implementation of `PackCoalescedPacket` the way uQUIC's is. It
embeds the base packer and overrides `PackCoalescedPacket` for one case only:
the fresh ClientHello flight — a client Initial carrying new CRYPTO popped from
the initial stream. That flight has no handshake keys yet, so it never coalesces,
which is what lets the override own the whole datagram and delegate everything
else — ACK-only Initials, Initial retransmissions (which come from the
retransmission queue, so `initialStream.HasData()` is false), PTO probes, and
every later encryption level — to the base packer unchanged. **Consequence to
keep:** a retransmitted or PTO-probed Initial goes out as quic-go's single CRYPTO
frame, distinguishable from Chrome but only under packet loss. Re-framing those
would risk desyncing the Initial ack handler (which tracks the contiguous range
it popped) against a re-cut wire layout; the ClientHello, which is what a
first-packet classifier reads, is covered.

Two deliberate divergences from uQUIC, both driven by the wire-capture test
(`TestChrome146Initial_ReframedOnTheWire`), which decrypts the real Initial
datagrams and asserts on their frames:

- **`QUICRandomFrames` measures its PADDING top-up at the real base offset**, not
  at 0. A CRYPTO frame's offset varint widens from 1 to 2 bytes as the value
  crosses 64, so measuring at offset 0 undercounts datagrams past the first and
  lands the payload a few bytes over `Length`. uQUIC has this latent.
- **The CRYPTO-pop cap reserves room by builder parameters**, not a flat 16
  bytes. Splitting into up to `MaxCRYPTO` frames adds ~5 bytes of header each;
  16 is smaller than Chrome's `MaxCRYPTO`=14 worth, so uQUIC's first datagram
  ends up with no PADDING at all — itself a tell, since Chrome's does.

The per-packet PN encoding lengths live in the ack handler
(`internal/ackhandler/u_sent_packet_handler.go` + the two fields and the
`PeekPacketNumber` branch in `sent_packet_handler.go`). kuic stores them on the
concrete handler rather than wrapping it in a `uSentPacketHandler` as uQUIC does;
the handler is fork-local, so there is no constructor to shadow. Empty leaves
quic-go's own length calculation untouched, and a pinned length only ever forces
one that decodes correctly, never a shorter one.

**Backward-compat gate:** `TestNewClientOldServer_ReframedInitial` in
`integrationtests/apernetcompat` dials the un-upgraded apernet server with the
full Chrome-146 `InitialPacketSpec`. A re-framing bug that dropped or duplicated
a CRYPTO byte would leave the server unable to reassemble the ClientHello, so a
green handshake there is the proof the re-cut stream is faithful.

## Writing a ClientHelloSpec

Two requirements beyond ordinary utls spec authoring:

- a `*utls.QUICTransportParametersExtension` — see above;
- an extension satisfying `utls.EncryptedClientHelloExtension` (in practice
  `*utls.GREASEEncryptedClientHelloExtension`) if ECH is used. utls swaps that
  placeholder for the real `encrypted_client_hello` extension. Without it the
  server replies `missing_extension` and the ECH config is silently unused.

metacubex/utls ships **no QUIC parrots** — its built-in presets
(`HelloChrome_120_PQ` and friends) are TCP ClientHellos. Chrome's HTTP/3
ClientHello differs from its TCP one in extension set and order, so a TCP preset
is not a Chrome-over-QUIC fingerprint. A real one has to be authored from a
capture or from published QUIC spec data. `u_fingerprint_test.go` says the same
thing at the point of use.

## Session resumption is unavailable on the fingerprinted path

`utls.UQUICClient` ignores `QUICConfig.EnableSessionEvents`, so no
`QUICStoreSession` / `QUICResumeSession` events are emitted and `UQUICConn` has
no `StoreSession`. Consequence: **no TLS session resumption or 0-RTT** when a
spec is set. Every connection does a full handshake.

Relevant to fingerprinting too — a real browser reconnecting to the same origin
offers a session ticket, and this client never will.

## Rebasing on upstream

The baseline is tagged `apernet-base-<commit>` (currently
`apernet-base-eec823306178`). To take new upstream work:

1. Fetch apernet/quic-go, pick the new commit, tag it `apernet-base-<commit>`.
2. Rebase kuic's commits onto it.
3. Expect conflicts only in the files listed under "What actually differs".
   The import swap is mechanical:
   `"crypto/tls"` → `tls "github.com/metacubex/utls"` everywhere except
   `integrationtests/gomodvendor` and the three http3 boundary sites.
   Two base files also carry small Initial-framing edits that can conflict:
   `sent_packet_handler.go` (the two `initialPacketNumber*` fields and the
   `PeekPacketNumber` branch) and `connection.go` (the client wiring near
   `newPacketPacker`). Everything else for that feature is in its own `u_*.go`
   files, and `uPacketPacker` delegates to — rather than copies —
   `PackCoalescedPacket`, so upstream changes to the packing loop need no
   re-sync. See "The Chrome-146 Initial framing port".
4. `go build ./... && go vet ./... && go test ./... -count=1`.
5. The gate in `u_fingerprint_test.go` must pass. It is mutation-verified —
   if it goes green after a change that should have broken it, distrust it and
   re-run the mutations described in its commit message.
6. `cd integrationtests/apernetcompat && go test ./... -count=1`. Separate
   module, so `./...` at the root does not reach it. See below.

## The apernet compatibility matrix

`integrationtests/apernetcompat/` is its own module requiring **both** forks at
once — kuic via `replace ../..`, apernet/quic-go at the version kuic was forked
from. It answers the question kuic's own suite structurally cannot: does a
fingerprinted kuic client interoperate with the un-upgraded apernet server that
is already deployed, and does an un-upgraded apernet client still reach a kuic
server? Both directions matter, because the fork rolls out client-first and the
rollout window contains both mixed pairings.

The peer in the first case is stdlib `crypto/tls`, which has never seen a utls
type. That is the only place a malformed shaped ClientHello shows up as a dead
handshake rather than as nothing at all.

Bump the apernet version in its `go.mod` whenever the rebase baseline moves —
a matrix pinned to a fork point the fleet no longer runs proves nothing.

## utls version

Pinned to the version k2 uses. Bumping utls is the single riskiest maintenance
action here, because the spec/preset internals and the ECH code are exactly
what this fork depends on. Bump both repos together and re-run the gate.
