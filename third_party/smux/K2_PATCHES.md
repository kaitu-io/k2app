# k2 patches to smux v2.1.0

Vendored from `github.com/xtaci/smux/v2@v2.1.0` (latest v2 tag; MIT, see LICENSE)
and wired in with a `replace` in k2's `go.mod`. Images and README dropped; every
other file is upstream byte-for-byte except the patch below.

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

Upgrading smux: re-check whether upstream v2 carries this fix; if it does, drop
this directory and the `replace`.

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
