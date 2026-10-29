# k2 patches to smux v2.1.0

Vendored from `github.com/xtaci/smux/v2@v2.1.0` (MIT, see LICENSE) and wired
in with a `replace` in k2's `go.mod`. Images and README dropped; every other
file is upstream byte-for-byte except the patches below.

**This is an independent fork; it does not track upstream.** The `smux/v2`
module line ended at v2.1.0 (2019-12-26) and its tags are gone from the
upstream repo; upstream development continues as `github.com/xtaci/smux`
v1.5.x, which merged this protocol as "version 2" and has diverged by ~160
commits (closest code point: v1.5.5). Upstream's own half-close (v1.5.58,
2026-10) is not wire-compatible in meaning with patch 2: it sends one FIN for
both `CloseWrite` and `Close`, and a writer whose peer has fully closed blocks
until its deadline. Port individual upstream fixes when they apply; don't
re-vendor.

## 1. `waitRead`: don't report EOF while data is still buffered (stream.go)

`Read` calls `TryRead`, finds the buffer empty, then blocks in `waitRead`. If the
peer's last data frame and its FIN both land before the `select` runs, `select`
picks one of the two ready cases at random; picking `chFinEvent` returned
`io.EOF` and the caller dropped the buffered bytes. Peers that write and close
right away (TCP-WS metadata response, any proxied stream's tail) lost their last
chunk: ~4 in 220k write+close round-trips under CPU load, 0 in 540k patched.
CI symptom: `wire.TestTCPWSCookie_SecondConnectIsWarm` "client did not store the
issued cookie".

Same fix upstream applied on the v1 line (xtaci/smux#82): on FIN, return nil
(re-read) when the buffer is non-empty. Pinned by `fin_tail_test.go`.


## 2. Half-close: `Stream.CloseWrite`, two FINs per side (stream.go, session.go)

Upstream has no half-close. `Close()` sends one FIN and deregisters the
stream, so anything the peer sends afterwards is dropped; and a writer that
sees the peer's FIN returns `io.EOF` once the window runs out ("future window
update is impossible"). k2's `smuxStreamConn.CloseWrite` was a full `Close`,
so on TCP-WS a client that half-closed after its request lost the response,
and an upload to a server that had already FIN'd ended after one window.
QUIC streams never had either problem.

The patch:

- `CloseWrite()` sends the first FIN, makes further `Write`s fail with
  `io.ErrClosedPipe`, and leaves the stream registered and readable.
- `Close()` brings the FINs sent for the stream to **two**: the first closes
  our write direction, the second our read direction. A peer running
  upstream code treats every FIN after the first as a no-op (`fin()` used a
  `sync.Once`; a FIN for a deregistered stream is ignored by `recvLoop`).
- On receive, `fin()` counts: the first FIN closes `chFinEvent` (reads drain
  and then return EOF, as before), the second closes `chPeerClosed`, later
  ones are no-ops. The count saturates at 3: a uint32 that kept counting
  would wrap after 2^32 FINs from a hostile peer and close `chFinEvent`
  twice, a panic in `recvLoop` that kills the process
  (`TestHalfClose_FINCounterNeverWraps`).
- `Write` ends with `io.EOF` on the peer's second FIN, and on its first FIN
  **only if the peer is not known to support half-close**. An upstream
  peer's single FIN is a full close, and it will never send another window
  update, so waiting for one would hang the writer.
  `Session.SetPeerHalfClose(true)` marks the peer as capable. smux has no
  handshake of its own, so k2 learns it from the metadata stream (`smux_hc`
  kv key: the TCP-WS client offers it in the request, the server in the
  response; see `wire/tcpws.go`).
- `Write` now returns the bytes actually sent alongside `io.EOF` (upstream
  returned 0), so `io.Copy` byte counts stay right.

Mixed versions: a new client with an old server keeps upstream write
semantics for the session (no `smux_hc` in the response). Its `CloseWrite`
still keeps reading, which can only receive more of the response than
before. An old client with a new server never offers `smux_hc`, so the
server keeps upstream semantics too. Pinned by `halfclose_test.go` and
`wire/tcpws_halfclose_test.go`.

The opener-speaks-first contract below still applies: an acceptor must not
FIN before the opener's first byte, or the FIN can land before `OpenStream`
registers the stream and be dropped.

## 3. `WriteTo` returns nil at EOF (stream.go)

`io.WriterTo` reports success at EOF, and `io.Copy` returns `WriteTo`'s error
unchanged. Upstream returned `io.EOF`, so every `io.Copy` out of a stream
that ended with a clean FIN returned an error. k2's client `pipe`
(`core/tunnel.go`) treats a copy error as a broken stream and fully closes
both conns, which cancelled half-close for the direction still running.
Upstream v1.5.x made the same change (92b1bfc). Pinned by
`TestHalfClose_WriteToEndsWithNilAtFIN` and
`wire.TestTCPWS_HalfClose_CopyFromStreamEndsCleanlyOnFIN`.

Related, in k2 not here: `wire.smuxStreamConn` embeds `*Stream`, so `io.Copy`
used the promoted `WriteTo` and skipped the bytes its header `bufio.Reader`
had buffered. It now shadows `WriteTo` with `br.WriteTo`
(`wire.TestSmuxStreamConn_CopyIncludesBufferedBytes`).

## Contract k2 relies on (not patched): the opener speaks first

`OpenStream` writes the SYN and only then registers the stream in
`s.streams`. A PSH or FIN that the peer sends for that id in between finds no
stream in `recvLoop` and is dropped silently. Upstream v1 (v1.5.57) has the
same ordering, so this is smux's design assumption, not a v2 regression. k2
never trips it: only the client opens streams (`wire/tcpws.go`,
`wire/metadata_client.go`), it writes the stream header right after
`OpenStream` returns, and `TCPWSServer.handleStream` reads that header before
writing anything — so no server byte can precede registration. A new stream
type where the accepting side writes first, or the server starting to open
streams, would break this; keep the opener-speaks-first order.
`TestWriteThenCloseDeliversEverything` follows the same order (the acceptor
reads one byte before replying); without that read it loses data on upstream
code too.
