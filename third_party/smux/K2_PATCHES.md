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
