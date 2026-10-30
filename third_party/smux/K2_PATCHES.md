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

Mixed versions: a session whose peer is not known to half-close (an old
peer never offers `smux_hc`) keeps upstream semantics entirely: writes end
at the peer's first FIN, and **`CloseWrite` is a full `Close`**, as k2's
`CloseWrite` was before this patch. Half-closing toward such a peer would
be worse than either: it reads our FIN as a full close and stops sending
once its window runs out, so we would read a response cut at an arbitrary
point that still ends in a clean EOF
(`TestHalfClose_CloseWriteToUnknownPeerClosesFully`). Half-close is used
only when both ends have said so. Pinned by `halfclose_test.go` and
`wire/tcpws_halfclose_test.go`.


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

## 4. Edge-triggered poller removed (session.go, stream.go)

`pushBytes` put a stream into `Session.pollEvents` whenever its buffer went
from empty to non-empty, and only `PollWait` ever took it out. k2 never
calls `PollWait`, so every stream that ever received data stayed referenced
until its session ended: ~1.9 KB per stream across both ends, 94 MB after
50k short streams on one session (0.2 MB without it) — enough to push an
iOS Network Extension past its memory limit on a long TCP-WS session.
`PollWait`, `pollEvents` and `TestPoll` are gone; upstream dropped the
poller too (e7525af).

## 5. Hardening against hostile or stuck peers (session.go, stream.go)

Each item is pinned by a `hardening_test.go` test that fails on the code
before it.

- **Register before SYN.** `OpenStream` used to write the SYN and only then
  add the stream to `s.streams`; a PSH or FIN the acceptor sent at once
  could reach `recvLoop` first and be dropped as a frame for an unknown
  stream. k2 got by on an "opener speaks first" ordering contract; the
  test without it loses the acceptor's reply within a few rounds. Upstream
  fixed the same in 8291a86.
- **Frame lengths validated.** NOP/SYN/FIN must carry length 0 and UPD
  length 8; otherwise the payload was parsed as the next header. Now a
  protocol error (upstream 5c3cd51).
- **UPD window bounded.** A window above MaxInt32 went negative in
  `Write`'s int32 arithmetic and blocked the writer forever. Now a
  protocol error.
- **A dead loop closes the session.** A read, write or protocol error ends
  `recvLoop`/`sendLoop`; the session used to stay open, so `OpenStream`
  still succeeded and writers waited for window updates that could not
  come. `notify*Error` now also call `Close` (upstream 292d504 covered
  only `OpenStream`).
- **Writes are bounded.** `sendLoop` sets a write deadline of
  `KeepAliveTimeout` on the conn (refreshed when less than half remains).
  A peer that keeps sending NOPs but stops reading used to block it
  forever, and keepalive, which watches only the receive side, never
  fired. With `coder/websocket`'s net.Conn an expired deadline only fails
  a write in progress; an idle conn is re-armed before its next write.
- **Accept hand-off outside `streamLock`.** A full accept backlog blocks
  `recvLoop` on `chAccepts` (backpressure); it used to do so holding
  `streamLock`, which also blocked every `OpenStream` and `Stream.Close`.
- **Deadlines wake blocked calls.** `SetReadDeadline`/`SetWriteDeadline`
  now wake a `Read`/`Write` already waiting, as net.Conn specifies; `Write`
  re-reads its deadline on every pass (upstream d2d7690 did the read side).
- A PSH for an unknown stream returns its buffer to the allocator.
- **Short writes are errors.** `WriteTo` returned success when its
  destination took only part of a buffer without an error, dropping the
  rest (upstream 9374ca7); a conn write that came back short without an
  error left a partial frame on the wire (upstream 148c01b). Both now
  report `io.ErrShortWrite`, the second closing the session.

## 6. Receive-window auto-tuning (`Config.MinStreamBuffer`; stream.go, session.go, mux.go)

Upstream advertises a fixed window of `MaxStreamBuffer` for every stream,
and stops reading the conn once `MaxReceiveBuffer` bytes are buffered. With
k2's old TCP-WS config (64 KB / 256 KB) that meant two failures, both
reproduced in tests:

- **One stream capped at 64 KB per round trip**: 5 Mbps at 100 ms RTT,
  2.6 Mbps at 200 ms, whatever the link could do.
- **Four stalled readers froze the session.** A stream whose reader stops
  (a paused video, an app in the background) keeps its window full. Four
  such streams filled the session buffer, `recvLoop` stopped, every other
  stream froze, and keepalive killed the session 30 s later.

With `MinStreamBuffer` > 0:

- A stream's window starts at `MinStreamBuffer` and doubles, up to
  `MaxStreamBuffer`, each time its reader consumes a whole window within
  `tuneInterval` (1 s). That needs no RTT estimate: a window-limited
  sender delivers one window per round trip, so any path under 1 s RTT
  grows; a stream that is not window-limited stops at about rate × 1 s.
- Growth over all live streams is capped at `MaxReceiveBuffer/2` (the
  pool), and one stream grows to at most `MinStreamBuffer` + half the
  pool, so a single fast stream leaves room for another
  (`TestAutoTune_PerStreamCeiling`).
- A stream whose reader has consumed nothing for `tuneInterval` shrinks
  back to `MinStreamBuffer` and tells its peer (`tuneLoop`, its own
  goroutine so a slow conn can't delay keepalive). Without it, a stream
  that grew and then paused (a paused video, a kept-alive HTTP/2 conn) kept
  its growth until it closed; on the iOS budget one such stream left every
  new stream at 16 KB (`TestAutoTune_IdleStreamGivesGrowthBack`).
- The pool is charged for what a stream may actually hold:
  max(window, bytes buffered) − `MinStreamBuffer`. Bytes that a sender
  still on the old window delivers after a shrink are charged on arrival,
  past the pool if need be, and released as they are read or at close
  (`TestAutoTune_BytesPastShrunkWindowAreCharged`). A stalled reader holds
  about `MinStreamBuffer`, so blocking the session takes
  `MaxReceiveBuffer/2 / MinStreamBuffer` of them, and conforming peers
  can't reach the buffer limit.
- Each side sends a window update as soon as it starts using a stream: the
  opener right behind its SYN, ahead of any data; the acceptor on the
  stream's first `Read`, `Write` or `WriteTo`, from that caller's
  goroutine, not in `AcceptStream`, so an accept loop never waits on the
  conn (`TestAutoTune_AcceptDoesNotWaitOnConn`). Until then the peer
  assumes `initialPeerWindow` (256 KB): an opener may send up to that much
  before the acceptor's update lands, as before the patch, still bounded
  by the session buffer. (An old opener registers its stream only after its
  SYN write, so it can drop an acceptor's early update; it then runs on
  256 KB until our first read, as before.)
- All of this is receiver-local: a window travels in the receiver's own
  updates, so either end can run it against any peer
  (`TestAutoTune_InteropWithFixedWindowPeer`, plus a run against the
  pre-patch code).

Not changed: keepalive still closes a session whose `recvLoop` is blocked
on the buffer, unlike upstream 80072d9. Once conforming peers can't
block it, a blocked buffer means a misbehaving peer or a pathological
stall. Closing lets the client reconnect, and a server reclaims the
session; leaving it frozen would do neither.

k2's budgets live in `wire.DefaultSmuxConfig` (client, per platform) and
`wire.ServerSmuxConfig`, with `MaxStreamBuffer` at or below the per-stream
ceiling (`wire.TestDefaultSmuxConfig`). Pinned by `autotune_test.go`.

Also added: `Stream.PeerClosed()`, closed once the peer will read nothing
more (its second FIN with half-close, its first otherwise). k2's server
`pipe` uses it to end a stream whose client has closed while the target
stays silent (`server.TestPipe_ClientFullCloseEndsSilentTarget`). And
`Write` returns the bytes sent with `ErrConsumed`, as it does with `io.EOF`.

Window updates are ordered (review of patch 6). The reader and tuneLoop's
idle shrink both send a stream's window updates, and the peer — old or new
— keeps whichever arrives last. A shrink built before the reader consumed
more, landing after the reader's update, sent "consumed" backwards: the
sender then counted bytes as in flight that had been read, and with the
buffer drained below half a window the receiver had no reason to send
another update — a stream stalled for good while keepalive kept the
session up. Now `Stream.sendLatestWindow` sends every update under the
per-stream `updMu`, reading consumed/window at send time, and the shaper
releases equal-priority frames first-in, first-out (`writeRequest.seq`; a
heap is not stable). The shrink holds `updMu` for at most `tuneInterval`,
so a reader queued behind it overruns its read deadline by at most that;
the reader itself never gives up on `updMu` (its consumed count is already
reset, so a dropped update could be the stall itself). Pinned by
`TestWindowUpdates_NeverGoBackwards` (a test hook pauses the shrink between
deciding and sending), `TestWindowUpdates_StaleShrinkDoesNotStallSender`
(end to end: on the old order the sender stays stuck past 15 s; it needs
the reader's catch-up to be under a window, so the window does not grow
back and re-arm the shrink, and the sender to pause past `tuneInterval`)
and `TestShaper_EqualPriorityIsFIFO`.

## 7. A write's own result wins over the session closing (session.go)

A conn write that fails closes the session right after delivering the
frame's result, so the writer's `select` saw both ready and picked at
random: `io.ErrClosedPipe` about a third of the time instead of the real
error (`TestHardening_ShortConnWriteIsFatal` failed 84 / 500 runs).
`writeFrameInternal` now takes the result when it is there.

## Fuzzing

`FuzzSession` (`fuzz_test.go`) drives a server session with arbitrary peer
frames, well-formed or not, interleaved with local stream calls, and
checks: no panic or hang, receive accounting in bounds (`bucket <=
MaxReceiveBuffer`, `grown >= 0`), and closing the conn ends the session and
every goroutine. Its seeds run with the normal tests; to fuzz,
`go test -run '^$' -fuzz FuzzSession -fuzztime 10m ./`.

## Removed contract: the opener no longer has to speak first

Before patch 5 an acceptor could not write (FIN included) before the
opener's first byte. That ordering is no longer required; k2 still
follows it (the client opens every stream and writes the header first),
which costs nothing.
