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

6. **`integrationtests/self/self_go12{4,5}_test.go`** both return 0 from
   `getCurveID`. utls keeps the negotiated group unexported.

## The transport-parameters trap

This is the part that is easy to get wrong and produces a handshake that dies
right after the ClientHello.

utls's `UQUICConn.SetTransportParameters` records the parameters internally but
deliberately does **not** write them into the ClientHello when a preset is
applied — there is a commented-out block in utls `u_quic.go` where that used to
happen. The ClientHello's `quic_transport_parameters` extension instead marshals
from the spec's own `QUICTransportParametersExtension.TransportParameters`.

Left alone, the server reads the spec author's parameters rather than this
connection's, including `initial_source_connection_id`, which RFC 9000 §7.3
requires to equal the packet's Source Connection ID. Any conformant server
answers `TRANSPORT_PARAMETER_ERROR`.

`fingerprintedConn.SetTransportParameters` fixes this by decoding quic-go's
marshaled parameters back into `(id, value)` pairs and writing them into the
spec's extension — `ApplyPreset` copies the spec's extension list by interface
value, so that object is the one the ClientHello is built from.

**ECH hides this bug.** Measured, not reasoned: disable the injection and
`TestFingerprintSpecWithoutECH_Handshakes` fails with exactly the error above,
while every ECH test still passes. With ECH the server reads the parameters
from the decrypted *inner* ClientHello, which utls builds from the bytes
`UQUICConn.SetTransportParameters` stashed internally — the spec's extension
only shapes the *outer* hello. So on an ECH deployment the injection is not a
correctness fix at all; it is what keeps the outer ClientHello from advertising
an empty `quic_transport_parameters`, which no browser does and which is
therefore a discriminator all by itself.

Consequence for testing: **a no-ECH case is not optional coverage.** An all-ECH
suite goes green with the injection deleted.

That matters more than it looks, because of a second hazard with no other
alarm: `QUICTransportParametersExtension` caches its marshaled bytes in an
unexported `marshalResult` on the first `Len()` call, and kuic cannot reset it
(`UQUICConn.conn` is unexported, so the extension list is out of reach). Today
`Len()` runs after the injection and all is well. If a utls bump ever measured
the ClientHello first, the injection would silently stop reaching the wire —
red in the no-ECH tests, invisible everywhere else. If those go red after a
utls bump, look here before looking anywhere else.

Two consequences to preserve:

- **`NewClientHelloSpec` is a factory, not a value.** The parameters written in
  are connection-specific. A shared spec has concurrent dials overwrite each
  other's connection ID.
- **The spec must contain a `*utls.QUICTransportParametersExtension`.** Missing
  it fails the dial (`ErrNoQUICTransportParametersExtension`) rather than
  falling back to an unshaped handshake.

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
